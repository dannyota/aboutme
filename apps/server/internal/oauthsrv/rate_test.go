package oauthsrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

func testOAuthRateConfig() RateConfig {
	return RateConfig{
		TrustedProxies:         api.TrustedProxies{netip.MustParsePrefix("127.0.0.1/32")},
		RegisterRequests:       5,
		RegisterRangeRequests:  120,
		RegisterGlobalRequests: 600,
		RegisterWindow:         time.Hour,
		TokenRequests:          30,
		TokenWindow:            time.Minute,
		FailedGrantLimit:       10,
		FailedGrantWindow:      15 * time.Minute,
		MaxKeys:                10_000,
	}
}

func rateRequest(path, contentType, body, viewerIP string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example"+path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:443"
	req.Header.Set(api.TrustedClientIPHeader, viewerIP)
	req.Header.Set("Content-Type", contentType)
	return req
}

func TestRatePolicies_RegisterAndTokenExactBudgetsAndRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	policies, err := NewRatePolicies(testOAuthRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}

	registration := &Service{
		clock:             func() time.Time { return now },
		registerAdmission: policies,
	}
	for i := 1; i <= 6; i++ {
		recorder := httptest.NewRecorder()
		registration.HandleRegister(recorder, rateRequest("/oauth/register", "application/json", `{}`, "198.51.100.10"))
		if i <= 5 && recorder.Code == http.StatusTooManyRequests {
			t.Fatalf("register request %d was rate limited within the 5/hour budget", i)
		}
		if i == 6 {
			if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "720" {
				t.Fatalf("register limit+1 = %d Retry-After %q, want 429 and 720", recorder.Code, recorder.Header().Get("Retry-After"))
			}
			if got := recorder.Body.String(); got != `{"error":"invalid_request","error_description":"The request is invalid."}` {
				t.Fatalf("register 429 body = %q", got)
			}
		}
	}

	tokens := &Service{
		clock:          func() time.Time { return now },
		tokenAdmission: policies,
	}
	for i := 1; i <= 31; i++ {
		recorder := httptest.NewRecorder()
		tokens.HandleToken(recorder, rateRequest("/oauth/token", "application/x-www-form-urlencoded", "grant_type=unsupported", "198.51.100.11"))
		if i <= 30 && recorder.Code == http.StatusTooManyRequests {
			t.Fatalf("token request %d was rate limited within the 30/minute budget", i)
		}
		if i == 31 {
			if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "2" {
				t.Fatalf("token limit+1 = %d Retry-After %q, want 429 and 2", recorder.Code, recorder.Header().Get("Retry-After"))
			}
		}
	}
}

func TestRatePolicies_UsesBoundedOverflowStore(t *testing.T) {
	cfg := testOAuthRateConfig()
	cfg.RegisterRequests = 1
	cfg.RegisterWindow = time.Minute
	cfg.MaxKeys = 1
	policies, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	now := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		ip      string
		allowed bool
	}{
		{"198.51.100.1", true},
		{"198.51.100.2", true},
		{"198.51.100.3", false},
		{"198.51.100.1", false},
	} {
		allowed, _ := policies.AdmitRegister(now, rateRequest("/oauth/register", "application/json", `{}`, tc.ip))
		if allowed != tc.allowed {
			t.Fatalf("AdmitRegister(%s) = %t, want %t", tc.ip, allowed, tc.allowed)
		}
	}
}

func TestRatePolicies_FailedGrantBudgetClearsOnlyOnSuccess(t *testing.T) {
	cfg := testOAuthRateConfig()
	cfg.MaxKeys = 1
	policies, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	client := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000030")

	for i := 1; i <= 2; i++ {
		attempt, allowed, _ := policies.AdmitGrant(client, now)
		if !allowed {
			t.Fatalf("pre-success failed grant %d was denied", i)
		}
		policies.FinishGrant(attempt, grantAttemptFailure)
	}
	success, successAllowed, _ := policies.AdmitGrant(client, now)
	if !successAllowed {
		t.Fatal("success attempt was denied before the failure budget was full")
	}
	policies.FinishGrant(success, grantAttemptSuccess)
	for i := 1; i <= 10; i++ {
		attempt, attemptAllowed, _ := policies.AdmitGrant(client, now)
		if !attemptAllowed {
			t.Fatalf("post-success failed grant %d denied before the reset budget was consumed", i)
		}
		policies.FinishGrant(attempt, grantAttemptFailure)
	}
	if _, limitAllowed, retry := policies.AdmitGrant(client, now); limitAllowed || retry != 900 {
		t.Fatalf("failed grant limit+1 = (%t,%d), want (false,900)", limitAllowed, retry)
	}

	// Once the bounded store is full, new client IDs share one overflow
	// bucket. A success for one overflow client must not clear every other
	// overflow client's debt.
	overflowA := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000031")
	overflowB := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000032")
	for i := 1; i <= 9; i++ {
		attempt, attemptAllowed, _ := policies.AdmitGrant(overflowA, now)
		if !attemptAllowed {
			t.Fatalf("overflow failure %d was denied", i)
		}
		policies.FinishGrant(attempt, grantAttemptFailure)
	}
	overflowSuccess, overflowSuccessAllowed, _ := policies.AdmitGrant(overflowB, now)
	if !overflowSuccessAllowed {
		t.Fatal("overflow success reservation was denied")
	}
	policies.FinishGrant(overflowSuccess, grantAttemptSuccess)
	overflowFailure, overflowFailureAllowed, _ := policies.AdmitGrant(overflowA, now)
	if !overflowFailureAllowed {
		t.Fatal("overflow success did not release its own reservation")
	}
	policies.FinishGrant(overflowFailure, grantAttemptFailure)
	if _, limitAllowed, _ := policies.AdmitGrant(overflowA, now); limitAllowed {
		t.Fatal("overflow success cleared another client's shared failure debt")
	}
}

func TestRatePolicies_FailedGrantAdmissionReservesConcurrentBudget(t *testing.T) {
	cfg := testOAuthRateConfig()
	cfg.FailedGrantLimit = 3
	policies, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	now := time.Date(2026, 9, 2, 12, 30, 0, 0, time.UTC)
	client := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000034")

	attempts := make([]grantAttempt, 0, 3)
	for i := 1; i <= 3; i++ {
		attempt, allowed, _ := policies.AdmitGrant(client, now)
		if !allowed {
			t.Fatalf("concurrent reservation %d denied within the budget", i)
		}
		attempts = append(attempts, attempt)
	}
	if _, allowed, retry := policies.AdmitGrant(client, now); allowed || retry != 900 {
		t.Fatalf("concurrent reservation limit+1 = (%t,%d), want (false,900)", allowed, retry)
	}
	policies.FinishGrant(attempts[0], grantAttemptRelease)
	replacement, allowed, _ := policies.AdmitGrant(client, now)
	if !allowed {
		t.Fatal("a non-failure outcome did not release its reservation")
	}
	_ = replacement
	policies.FinishGrant(attempts[1], grantAttemptSuccess)
	for i := 1; i <= 3; i++ {
		if _, allowed, _ := policies.AdmitGrant(client, now); !allowed {
			t.Fatalf("reservation %d denied after success clear", i)
		}
	}
}

func TestRatePolicies_InvalidCanonicalClientAddressFailsClosed(t *testing.T) {
	policies, err := NewRatePolicies(testOAuthRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	req := rateRequest("/oauth/register", "application/json", `{}`, "not-an-ip")
	if allowed, retry := policies.AdmitRegister(time.Now(), req); allowed || retry != 1 {
		t.Fatalf("invalid canonical client address = (%t,%d), want closed denial", allowed, retry)
	}
}

func TestHandleToken_FailedGrantBudgetIsClearedBySuccess(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	fixture := newCodeFixture(t, now)
	cfg := testOAuthRateConfig()
	cfg.TokenRequests = 100
	cfg.FailedGrantLimit = 3
	policies, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	fixture.s.tokenAdmission = policies

	for i := 0; i < 2; i++ {
		response := fixture.exchange(t, fixture.clientID, "http://127.0.0.1:20090/callback", "wrong-verifier-value-with-a-valid-enough-shape-abcdefghijklmnopqrstuvwxyz")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("pre-success failure %d status = %d", i+1, response.Code)
		}
	}
	if response := fixture.exchange(t, fixture.clientID, "http://127.0.0.1:20090/callback", fixture.verifier); response.Code != http.StatusOK {
		t.Fatalf("successful exchange status = %d, want 200", response.Code)
	}
	for i := 1; i <= 3; i++ {
		response := fixture.exchange(t, fixture.clientID, "http://127.0.0.1:20090/callback", fixture.verifier)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("post-success failed grant %d status = %d, want 400 after success reset", i, response.Code)
		}
	}
	limited := fixture.exchange(t, fixture.clientID, "http://127.0.0.1:20090/callback", fixture.verifier)
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "900" {
		t.Fatalf("post-reset limit+1 = %d Retry-After %q", limited.Code, limited.Header().Get("Retry-After"))
	}
}

// testEgressRange is a published client egress range used only by these
// tests; production takes its list from OAUTH_REGISTER_EGRESS_CIDRS.
var testEgressRange = netip.MustParsePrefix("160.79.104.0/21")

func testEgressRatePolicies(t *testing.T) *RatePolicies {
	t.Helper()
	cfg := testOAuthRateConfig()
	cfg.RegisterEgressRanges = []netip.Prefix{testEgressRange}
	policies, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	return policies
}

// peerRequest is a registration request straight from an untrusted socket
// peer, so its own address is the client address.
func peerRequest(remoteAddr string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/register", strings.NewReader(`{}`))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	return req
}

func registerFrom(p *RatePolicies, now time.Time, ip string) (bool, int) {
	return p.AdmitRegister(now, rateRequest("/oauth/register", "application/json", `{}`, ip))
}

func TestNewRatePolicies_RejectsInvalidRegisterRangeAndGlobalBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*RateConfig)
	}{
		{"zero range budget", func(c *RateConfig) { c.RegisterRangeRequests = 0 }},
		{"zero global budget", func(c *RateConfig) { c.RegisterGlobalRequests = 0 }},
		{"invalid prefix", func(c *RateConfig) { c.RegisterEgressRanges = []netip.Prefix{{}} }},
		{"non-canonical prefix", func(c *RateConfig) {
			c.RegisterEgressRanges = []netip.Prefix{netip.MustParsePrefix("160.79.104.1/21")}
		}},
		{"mapped prefix", func(c *RateConfig) {
			c.RegisterEgressRanges = []netip.Prefix{netip.MustParsePrefix("::ffff:160.79.104.0/117")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testOAuthRateConfig()
			tc.mutate(&cfg)
			if _, err := NewRatePolicies(cfg); err == nil {
				t.Fatal("NewRatePolicies error = nil")
			}
		})
	}
}

// A header naming an address inside a configured range is honored only
// through the trust boundary: from a trusted proxy, as exactly one X-Real-IP.
func TestRatePolicies_RegisterRangeCannotBeSpoofedByHeaders(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

	t.Run("untrusted peer keeps its own five an hour", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		for i := 1; i <= 6; i++ {
			req := peerRequest("203.0.113.7:5555")
			req.Header.Set(api.TrustedClientIPHeader, "160.79.104.10")
			req.Header.Set("X-Forwarded-For", "160.79.104.10")
			allowed, retry := policies.AdmitRegister(now, req)
			if i <= 5 && !allowed {
				t.Fatalf("spoofing request %d denied within the per-address budget", i)
			}
			if i == 6 && (allowed || retry < 1) {
				t.Fatalf("spoofing request 6 = (%t,%d), want the per-address refusal", allowed, retry)
			}
		}
		// The range bucket was never touched by the spoofing peer.
		for i := 1; i <= 120; i++ {
			if allowed, _ := registerFrom(policies, now, "160.79.104.10"); !allowed {
				t.Fatalf("range registration %d denied after a spoofing peer", i)
			}
		}
	})

	t.Run("trusted proxy with only X-Forwarded-For fails closed", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		req := peerRequest("127.0.0.1:443")
		req.Header.Set("X-Forwarded-For", "160.79.104.10")
		if allowed, retry := policies.AdmitRegister(now, req); allowed || retry != 1 {
			t.Fatalf("X-Forwarded-For only = (%t,%d), want (false,1)", allowed, retry)
		}
	})

	t.Run("repeated X-Real-IP fails closed", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		req := peerRequest("127.0.0.1:443")
		req.Header.Add(api.TrustedClientIPHeader, "160.79.104.10")
		req.Header.Add(api.TrustedClientIPHeader, "160.79.104.11")
		if allowed, retry := policies.AdmitRegister(now, req); allowed || retry != 1 {
			t.Fatalf("repeated X-Real-IP = (%t,%d), want (false,1)", allowed, retry)
		}
	})

	t.Run("trusted proxy with one in-range X-Real-IP uses the range bucket", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		for i := 1; i <= 6; i++ {
			if allowed, _ := registerFrom(policies, now, "160.79.104.10"); !allowed {
				t.Fatalf("in-range registration %d denied by a per-address budget", i)
			}
		}
	})
}

func TestRatePolicies_RegisterRangeSharesOneBucketAndRefills(t *testing.T) {
	now := time.Date(2026, 9, 27, 11, 0, 0, 0, time.UTC)
	policies := testEgressRatePolicies(t)

	first := netip.MustParseAddr("160.79.104.0")
	addr := first
	for i := 1; i <= 120; i++ {
		if allowed, _ := registerFrom(policies, now, addr.String()); !allowed {
			t.Fatalf("range registration %d from %s denied within 120 an hour", i, addr)
		}
		addr = addr.Next()
	}
	for _, ip := range []string{"160.79.111.255", "160.79.104.0", addr.String()} {
		allowed, retry := registerFrom(policies, now, ip)
		if allowed || retry < 1 {
			t.Fatalf("range registration 121 from %s = (%t,%d), want a refusal with Retry-After", ip, allowed, retry)
		}
	}

	// Neighbors just outside the range keep the per-address five an hour.
	for _, ip := range []string{"160.79.112.0", "160.79.103.255"} {
		for i := 1; i <= 6; i++ {
			allowed, retry := registerFrom(policies, now, ip)
			if i <= 5 && !allowed {
				t.Fatalf("%s registration %d denied within five an hour", ip, i)
			}
			if i == 6 && (allowed || retry < 1) {
				t.Fatalf("%s registration 6 = (%t,%d), want the per-address refusal", ip, allowed, retry)
			}
		}
	}

	// The bucket refills continuously; one window plus a second is past any
	// rounding of the refill rate.
	later := now.Add(time.Hour + time.Second)
	for i := 1; i <= 120; i++ {
		if allowed, _ := registerFrom(policies, later, "160.79.105.1"); !allowed {
			t.Fatalf("range registration %d after the window denied", i)
		}
	}
	if allowed, _ := registerFrom(policies, later, "160.79.105.1"); allowed {
		t.Fatal("range registration 121 after the refill was admitted")
	}
}

func TestRatePolicies_RegisterRangeMatchesIPv4MappedAddresses(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	t.Run("trusted proxy header", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		for i := 1; i <= 120; i++ {
			if allowed, _ := registerFrom(policies, now, "::ffff:160.79.104.9"); !allowed {
				t.Fatalf("mapped header registration %d denied within the range budget", i)
			}
		}
		if allowed, _ := registerFrom(policies, now, "160.79.104.10"); allowed {
			t.Fatal("mapped header addresses did not share the range bucket")
		}
	})

	t.Run("socket peer", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		for i := 1; i <= 120; i++ {
			if allowed, _ := policies.AdmitRegister(now, peerRequest("[::ffff:160.79.104.9]:443")); !allowed {
				t.Fatalf("mapped peer registration %d denied within the range budget", i)
			}
		}
		if allowed, _ := registerFrom(policies, now, "160.79.104.10"); allowed {
			t.Fatal("mapped peer did not share the range bucket")
		}
	})
}

func TestRatePolicies_RegisterGlobalCeiling(t *testing.T) {
	now := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)

	t.Run("601st distinct address is refused", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		addr := netip.MustParseAddr("10.0.0.1")
		for i := 1; i <= 600; i++ {
			if allowed, _ := registerFrom(policies, now, addr.String()); !allowed {
				t.Fatalf("registration %d from %s denied under the global ceiling", i, addr)
			}
			addr = addr.Next()
		}
		allowed, retry := registerFrom(policies, now, addr.String())
		if allowed || retry < 1 {
			t.Fatalf("registration 601 = (%t,%d), want a global refusal with Retry-After", allowed, retry)
		}
		if allowed, _ := registerFrom(policies, now, "160.79.104.10"); allowed {
			t.Fatal("the global ceiling did not bound the range bucket")
		}
	})

	t.Run("per-address refusals do not consume the ceiling", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		admitted := 0
		for i := 0; i < 1000; i++ {
			if allowed, _ := registerFrom(policies, now, "198.51.100.77"); allowed {
				admitted++
			}
		}
		if admitted != 5 {
			t.Fatalf("one address admitted %d of 1,000 attempts, want 5", admitted)
		}
		addr := netip.MustParseAddr("10.1.0.1")
		for i := 1; i <= 595; i++ {
			if allowed, _ := registerFrom(policies, now, addr.String()); !allowed {
				t.Fatalf("registration %d from another address denied; the ceiling was drained", i)
			}
			addr = addr.Next()
		}
		if allowed, _ := registerFrom(policies, now, addr.String()); allowed {
			t.Fatal("registration beyond the 600 ceiling was admitted")
		}
	})

	t.Run("range refusals do not consume the ceiling", func(t *testing.T) {
		policies := testEgressRatePolicies(t)
		admitted := 0
		for i := 0; i < 1000; i++ {
			if allowed, _ := registerFrom(policies, now, "160.79.104.10"); allowed {
				admitted++
			}
		}
		if admitted != 120 {
			t.Fatalf("the range admitted %d of 1,000 attempts, want 120", admitted)
		}
		addr := netip.MustParseAddr("10.2.0.1")
		for i := 1; i <= 480; i++ {
			if allowed, _ := registerFrom(policies, now, addr.String()); !allowed {
				t.Fatalf("registration %d from another address denied; the ceiling was drained", i)
			}
			addr = addr.Next()
		}
	})
}
