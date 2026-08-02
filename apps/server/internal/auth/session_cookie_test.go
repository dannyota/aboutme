// Package auth_test exercises the __Host-session cookie helpers. These
// tests are hermetic (no database): SetSessionCookie and
// ClearSessionCookie only touch net/http.ResponseWriter -- the same
// convention cookie_test.go already uses for __Host-oauth-tx.
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

// setSessionCookie calls fn (SetSessionCookie or ClearSessionCookie)
// against a fresh httptest.ResponseRecorder and returns the single cookie
// it wrote, parsed from the real "Set-Cookie" response header -- the same
// approach cookie_test.go's setCookie takes, so a bug in how the header
// text itself is rendered is caught too, not only a bug in which struct
// fields get set.
func setSessionCookie(t *testing.T, fn func(http.ResponseWriter)) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	fn(rec)

	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d Set-Cookie headers, want exactly 1 (raw: %q)", len(cookies), rec.Header().Values("Set-Cookie"))
	}
	return cookies[0]
}

// TestSetSessionCookie_EmitsPinnedAttributes pins the spec's literal
// contract (docs/specs/aboutme-design.md §3's sessions table):
// "__Host-session: Secure; HttpOnly; SameSite=Lax; Path=/ (no Domain)",
// with Max-Age equal to the absolute session timeout (90 days) -- ruling
// 5 of task-4-brief.md's dispatch. Every attribute is asserted
// individually, the same cookie_test.go precedent, so a regression in any
// one of them fails this test specifically.
func TestSetSessionCookie_EmitsPinnedAttributes(t *testing.T) {
	t.Parallel()

	const token = "session-token-value-under-test"
	c := setSessionCookie(t, func(w http.ResponseWriter) { auth.SetSessionCookie(w, token) })

	if c.Name != auth.SessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, auth.SessionCookieName)
	}
	if c.Value != token {
		t.Errorf("Value = %q, want %q", c.Value, token)
	}
	if !c.Secure {
		t.Error("Secure = false, want true (required for the __Host- prefix to mean anything)")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want SameSiteLaxMode", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
	const wantMaxAge = 90 * 24 * 60 * 60 // 90 days, in seconds
	if c.MaxAge != wantMaxAge {
		t.Errorf("MaxAge = %d, want %d (90 days, matching session.go's absoluteTimeout)", c.MaxAge, wantMaxAge)
	}
	// __Host- is only meaningful with no Domain attribute at all.
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty (a __Host- cookie must not set Domain)", c.Domain)
	}
}

// TestClearSessionCookie_ExpiresWithMatchingAttributes guards the other
// half of the __Host-session contract: a browser only overwrites/deletes a
// cookie when its Path (and, since this is __Host-, the implicit no-Domain
// and Secure) attributes match exactly what was set. If
// ClearSessionCookie's Path, Secure, HttpOnly, or SameSite ever drifted
// from SetSessionCookie's, the clear would silently stop working and a
// dead session cookie would linger in the browser after logout.
func TestClearSessionCookie_ExpiresWithMatchingAttributes(t *testing.T) {
	t.Parallel()

	c := setSessionCookie(t, auth.ClearSessionCookie)

	if c.Name != auth.SessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, auth.SessionCookieName)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative (delete now)", c.MaxAge)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q (must match SetSessionCookie's for the browser to actually delete it)", c.Path, "/")
	}
	if !c.Secure {
		t.Error("Secure = false, want true (must match SetSessionCookie's)")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true (must match SetSessionCookie's)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want SameSiteLaxMode (must match SetSessionCookie's)", c.SameSite)
	}
}
