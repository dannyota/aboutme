package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// validSecondFactorConfig returns a config that satisfies every dependency
// newSecondFactorRoutes needs to construct (relying party, mail key ring),
// without a live database: newMailKeyRing and every constructor here only
// validate and store their inputs, never dial out.
func validSecondFactorConfig(publicOrigin string, enrollment bool) config.Config {
	cfg := config.Config{PublicOrigin: publicOrigin, PasskeyEnrollment: enrollment}
	cfg.AuthEmail.ActiveKeyID = "k1"
	return cfg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNewSecondFactorRoutes_RegistersEveryContractRouteAndNoTOTPRoute proves
// every v0.4.2 pending, state, passkey, and recovery route in
// docs/design/second-factor-authentication.md's API table is reachable, and
// that no TOTP route is registered — the contract's closed v0.4.2 surface
// (docs/design/passkey-second-factor-contract.md, "Release surface").
func TestNewSecondFactorRoutes_RegistersEveryContractRouteAndNoTOTPRoute(t *testing.T) {
	t.Parallel()
	register, err := newSecondFactorRoutes(discardLogger(), validSecondFactorConfig("https://aboutme.example", true), &store.Pool{})
	if err != nil {
		t.Fatalf("newSecondFactorRoutes() error = %v", err)
	}
	mux := http.NewServeMux()
	register(mux)

	// Every route below requires the pending or session cookie this test
	// never sends, so each must be reachable (401, not 404) to prove
	// registration — the specific 401 body is internal/auth's own concern.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/second-factor"},
		{http.MethodPost, "/api/v1/auth/second-factor/passkey/options"},
		{http.MethodPost, "/api/v1/auth/second-factor/passkey/verify"},
		{http.MethodPost, "/api/v1/auth/second-factor/recovery/verify"},
		{http.MethodGet, "/api/v1/me/second-factor"},
		{http.MethodPost, "/api/v1/me/second-factor/passkeys"},
		{http.MethodDelete, "/api/v1/me/second-factor/passkeys/018f5b6a-9a3e-7c21-8b1e-000000000001"},
		{http.MethodPost, "/api/v1/me/second-factor/recovery-codes"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s %s: status = 404, want the route registered", tc.method, tc.path)
		}
	}

	// No TOTP route exists at any v0.4.2 second-factor path shape.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/second-factor/totp/verify"},
		{http.MethodPost, "/api/v1/me/second-factor/totp/enrollment"},
		{http.MethodPut, "/api/v1/me/second-factor/totp/enrollment"},
		{http.MethodDelete, "/api/v1/me/second-factor/totp"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (no TOTP route in v0.4.2)", tc.method, tc.path, rec.Code)
		}
	}
}

// TestNewSecondFactorRoutes_RegistrationOptionsIgnoresEnrollmentFlagOnlyAtRoute
// proves registration options answers the uniform not-found response while
// enrollment is disabled, before any session check, and proceeds to ordinary
// session enforcement (401, not 404) once enrollment is enabled — both ends
// of docs/design/passkey-second-factor-contract.md's "Release surface" rule
// that only registration options and completion recheck the flag.
func TestNewSecondFactorRoutes_RegistrationOptionsIgnoresEnrollmentFlagOnlyAtRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		enrollment bool
		want       int
	}{
		{"disabled is not found", false, http.StatusNotFound},
		{"enabled requires a session", true, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			register, err := newSecondFactorRoutes(discardLogger(), validSecondFactorConfig("https://aboutme.example", tc.enrollment), &store.Pool{})
			if err != nil {
				t.Fatalf("newSecondFactorRoutes() error = %v", err)
			}
			mux := http.NewServeMux()
			register(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/me/second-factor/passkeys/options", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestNewSecondFactorRoutes_RejectsRelyingPartyOrigin is the regression test
// for failing startup closed when PUBLIC_ORIGIN cannot produce a WebAuthn
// relying party (docs/design/second-factor-authentication.md, "Passkeys").
// internal/secondfactor.NewRelyingParty is always constructed — every
// second-factor route (not only enrollment) needs a working relying party —
// so this fails regardless of PasskeyEnrollment; see newSecondFactorRoutes's
// own comment and internal/config/passkey_test.go for the narrower
// flag-gated startup check.
func TestNewSecondFactorRoutes_RejectsRelyingPartyOrigin(t *testing.T) {
	t.Parallel()
	for _, enrollment := range []bool{false, true} {
		_, err := newSecondFactorRoutes(discardLogger(), validSecondFactorConfig("https://203.0.113.10", enrollment), &store.Pool{})
		if err == nil {
			t.Fatalf("newSecondFactorRoutes(enrollment=%t) error = nil, want a relying-party rejection for an IP-literal origin", enrollment)
		}
	}
}
