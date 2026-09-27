package viewpass_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/viewpass"
)

func testKey() []byte {
	return []byte("01234567890123456789012345678901") // 32 bytes
}

func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid.NewRandom: %v", err)
	}
	return id
}

// AC-VIEW-007: a sealed pass round trips through Valid for its own resume
// and epoch, and does not validate for another resume, another epoch, or
// past its expiry.
func TestValid_RoundTrip(t *testing.T) {
	t.Parallel()
	key := testKey()
	resumeID := mustUUID(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token := viewpass.Seal(key, resumeID, 3, now.Add(viewpass.Lifetime))

	if !viewpass.Valid(token, key, resumeID, 3, now) {
		t.Fatal("Valid() = false, want true for the sealing resume and epoch")
	}
}

func TestValid_AnotherResumeRejected(t *testing.T) {
	t.Parallel()
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token := viewpass.Seal(key, mustUUID(t), 1, now.Add(viewpass.Lifetime))

	if viewpass.Valid(token, key, mustUUID(t), 1, now) {
		t.Fatal("Valid() = true for a different resume ID, want false")
	}
}

func TestValid_OldEpochRejected(t *testing.T) {
	t.Parallel()
	key := testKey()
	resumeID := mustUUID(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token := viewpass.Seal(key, resumeID, 1, now.Add(viewpass.Lifetime))

	if viewpass.Valid(token, key, resumeID, 2, now) {
		t.Fatal("Valid() = true for a raised epoch, want false: an earlier period's pass must stop working")
	}
}

func TestValid_ExpiredRejected(t *testing.T) {
	t.Parallel()
	key := testKey()
	resumeID := mustUUID(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token := viewpass.Seal(key, resumeID, 1, now.Add(-time.Second))

	if viewpass.Valid(token, key, resumeID, 1, now) {
		t.Fatal("Valid() = true for an expired pass, want false")
	}
	// Exactly at the expiry instant is also expired: Valid uses a strict
	// before-comparison, matching "expires 7 days after sign-in".
	tokenAtBoundary := viewpass.Seal(key, resumeID, 1, now)
	if viewpass.Valid(tokenAtBoundary, key, resumeID, 1, now) {
		t.Fatal("Valid() = true at the exact expiry instant, want false")
	}
}

func TestValid_BadTagRejected(t *testing.T) {
	t.Parallel()
	key := testKey()
	resumeID := mustUUID(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token := viewpass.Seal(key, resumeID, 1, now.Add(viewpass.Lifetime))

	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode sealed token: %v", err)
	}
	decoded[len(decoded)-1] ^= 0xFF // flip a tag bit
	tampered := base64.RawURLEncoding.EncodeToString(decoded)

	if viewpass.Valid(tampered, key, resumeID, 1, now) {
		t.Fatal("Valid() = true for a tampered tag, want false")
	}

	// A different key must also fail to validate the same otherwise-valid
	// token: proves the tag actually binds to the key, not just its length.
	otherKey := []byte("98765432109876543210987654321098")
	if viewpass.Valid(token, otherKey, resumeID, 1, now) {
		t.Fatal("Valid() = true under a different key, want false")
	}
}

func TestValid_TruncatedOrGarbageRejected(t *testing.T) {
	t.Parallel()
	key := testKey()
	resumeID := mustUUID(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	full := viewpass.Seal(key, resumeID, 1, now.Add(viewpass.Lifetime))

	for name, value := range map[string]string{
		"empty":                     "",
		"truncated":                 full[:len(full)-10],
		"not base64":                "!!!not-base64!!!",
		"wrong length valid base64": base64.RawURLEncoding.EncodeToString([]byte("too short")),
		"single dot":                ".",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if viewpass.Valid(value, key, resumeID, 1, now) {
				t.Fatalf("Valid(%q) = true, want false", value)
			}
		})
	}
}

// AC-VIEW-007: at most 10 passes, one per resume; an 11th drops whichever
// kept pass expires first, and Max-Age is the latest remaining expiry.
func TestAdd_EleventhPassDropsEarliestExpiring(t *testing.T) {
	t.Parallel()
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	earliest := mustUUID(t)
	cookieValue := ""
	for i := 0; i < viewpass.MaxPasses; i++ {
		id := mustUUID(t)
		expiry := now.Add(viewpass.Lifetime + time.Duration(i)*time.Hour)
		if i == 0 {
			id = earliest
		}
		token := viewpass.Seal(key, id, 1, expiry)
		cookieValue = viewpass.Add(key, cookieValue, token, now).Value
	}
	if !viewpass.Valid(cookieValue, key, earliest, 1, now) {
		t.Fatal("the earliest-expiring pass should still be present before an 11th pass arrives")
	}

	eleventh := mustUUID(t)
	latestExpiry := now.Add(viewpass.Lifetime + 100*time.Hour)
	eleventhToken := viewpass.Seal(key, eleventh, 1, latestExpiry)
	cookie := viewpass.Add(key, cookieValue, eleventhToken, now)

	if viewpass.Valid(cookie.Value, key, earliest, 1, now) {
		t.Fatal("the earliest-expiring pass should be dropped once an 11th pass is added")
	}
	if !viewpass.Valid(cookie.Value, key, eleventh, 1, now) {
		t.Fatal("the newly added 11th pass should be present")
	}
	if got := strings.Count(cookie.Value, "."); got != viewpass.MaxPasses-1 {
		t.Fatalf("kept passes = %d, want %d", got+1, viewpass.MaxPasses)
	}
	if want := int(latestExpiry.Sub(now).Seconds()); cookie.MaxAge != want {
		t.Fatalf("Max-Age = %d, want %d (the latest remaining expiry)", cookie.MaxAge, want)
	}
}

func TestAdd_ReplacesExistingPassForTheSameResume(t *testing.T) {
	t.Parallel()
	key := testKey()
	resumeID := mustUUID(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	first := viewpass.Seal(key, resumeID, 1, now.Add(viewpass.Lifetime))
	cookie := viewpass.Add(key, "", first, now)

	second := viewpass.Seal(key, resumeID, 2, now.Add(viewpass.Lifetime+time.Hour))
	cookie = viewpass.Add(key, cookie.Value, second, now)

	if strings.Contains(cookie.Value, ".") {
		t.Fatalf("cookie value = %q, want exactly one pass for the one resume", cookie.Value)
	}
	if viewpass.Valid(cookie.Value, key, resumeID, 1, now) {
		t.Fatal("the superseded epoch-1 pass must not still validate")
	}
	if !viewpass.Valid(cookie.Value, key, resumeID, 2, now) {
		t.Fatal("the fresh epoch-2 pass must validate")
	}
}

func TestAdd_DropsExpiredAndUnreadableOnRewrite(t *testing.T) {
	t.Parallel()
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expired := viewpass.Seal(key, mustUUID(t), 1, now.Add(-time.Minute))
	garbage := "not-a-real-pass"
	existing := expired + "." + garbage

	fresh := viewpass.Seal(key, mustUUID(t), 1, now.Add(viewpass.Lifetime))
	cookie := viewpass.Add(key, existing, fresh, now)

	if strings.Contains(cookie.Value, garbage) {
		t.Fatalf("cookie value = %q, must not keep an unreadable token", cookie.Value)
	}
	for _, token := range strings.Split(cookie.Value, ".") {
		if token == expired {
			t.Fatal("expired pass must be dropped on rewrite")
		}
	}
}

func TestAdd_MaxAgeIsLatestRemainingExpiry(t *testing.T) {
	t.Parallel()
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	shortLived := viewpass.Seal(key, mustUUID(t), 1, now.Add(time.Hour))
	cookie := viewpass.Add(key, "", shortLived, now)

	longLived := viewpass.Seal(key, mustUUID(t), 1, now.Add(48*time.Hour))
	cookie = viewpass.Add(key, cookie.Value, longLived, now)

	if want := int((48 * time.Hour).Seconds()); cookie.MaxAge != want {
		t.Fatalf("Max-Age = %d, want %d (the longer-lived pass's remaining expiry)", cookie.MaxAge, want)
	}
}

// AC-VIEW-007: the whole cookie, attributes included, stays under 1 KiB
// even holding the maximum 10 passes.
func TestAdd_TenPassesStayUnderOneKiB(t *testing.T) {
	t.Parallel()
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cookieValue := ""
	for i := 0; i < viewpass.MaxPasses; i++ {
		token := viewpass.Seal(key, mustUUID(t), 1, now.Add(viewpass.Lifetime+time.Duration(i)*time.Hour))
		cookieValue = viewpass.Add(key, cookieValue, token, now).Value
	}
	cookie := viewpass.Add(key, cookieValue, "", now)
	if got := len(cookie.String()); got >= 1024 {
		t.Fatalf("serialized cookie is %d bytes, want under 1024", got)
	}
}

func TestAdd_CookieAttributes(t *testing.T) {
	t.Parallel()
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token := viewpass.Seal(key, mustUUID(t), 1, now.Add(viewpass.Lifetime))
	cookie := viewpass.Add(key, "", token, now)

	if cookie.Name != "__Host-view-pass" {
		t.Fatalf("Name = %q, want __Host-view-pass", cookie.Name)
	}
	if !cookie.Secure || !cookie.HttpOnly {
		t.Fatalf("Secure=%t HttpOnly=%t, want both true", cookie.Secure, cookie.HttpOnly)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Fatalf("Path = %q, want /", cookie.Path)
	}
}
