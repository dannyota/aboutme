package oauthsrv

import (
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
		TrustedProxies:    api.TrustedProxies{netip.MustParsePrefix("127.0.0.1/32")},
		RegisterRequests:  5,
		RegisterWindow:    time.Hour,
		TokenRequests:     30,
		TokenWindow:       time.Minute,
		FailedGrantLimit:  10,
		FailedGrantWindow: 15 * time.Minute,
		MaxKeys:           10_000,
	}
}

func rateRequest(path, contentType, body, viewerIP string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://aboutme.example"+path, strings.NewReader(body))
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
	success, allowed, _ := policies.AdmitGrant(client, now)
	if !allowed {
		t.Fatal("success attempt was denied before the failure budget was full")
	}
	policies.FinishGrant(success, grantAttemptSuccess)
	for i := 1; i <= 10; i++ {
		attempt, allowed, _ := policies.AdmitGrant(client, now)
		if !allowed {
			t.Fatalf("post-success failed grant %d denied before the reset budget was consumed", i)
		}
		policies.FinishGrant(attempt, grantAttemptFailure)
	}
	if _, allowed, retry := policies.AdmitGrant(client, now); allowed || retry != 900 {
		t.Fatalf("failed grant limit+1 = (%t,%d), want (false,900)", allowed, retry)
	}

	// Once the bounded store is full, new client IDs share one overflow
	// bucket. A success for one overflow client must not clear every other
	// overflow client's debt.
	overflowA := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000031")
	overflowB := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000032")
	for i := 1; i <= 9; i++ {
		attempt, allowed, _ := policies.AdmitGrant(overflowA, now)
		if !allowed {
			t.Fatalf("overflow failure %d was denied", i)
		}
		policies.FinishGrant(attempt, grantAttemptFailure)
	}
	overflowSuccess, allowed, _ := policies.AdmitGrant(overflowB, now)
	if !allowed {
		t.Fatal("overflow success reservation was denied")
	}
	policies.FinishGrant(overflowSuccess, grantAttemptSuccess)
	overflowFailure, allowed, _ := policies.AdmitGrant(overflowA, now)
	if !allowed {
		t.Fatal("overflow success did not release its own reservation")
	}
	policies.FinishGrant(overflowFailure, grantAttemptFailure)
	if _, allowed, _ := policies.AdmitGrant(overflowA, now); allowed {
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
