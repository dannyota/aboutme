package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// alwaysReadyPinger is a trivial api.DBPinger for tests that need a working
// router but no real database.
type alwaysReadyPinger struct{}

func (alwaysReadyPinger) Ping(context.Context) error { return nil }

// testTOTPActiveKey is the unpadded base64url spelling of 32 zero bytes, a
// valid TOTP_ACTIVE_KEY value (docs/design/totp-key-management.md "Key
// ring").
const testTOTPActiveKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// validSecondFactorConfig returns a config that satisfies every dependency
// newSecondFactorRoutes needs to construct (relying party, mail key ring,
// TOTP key ring), without a live database: newMailKeyRing and every
// constructor here only validate and store their inputs, never dial out.
func validSecondFactorConfig(publicOrigin string, enrollment bool) config.Config {
	cfg := config.Config{PublicOrigin: publicOrigin, PasskeyEnrollment: enrollment, TOTPActiveKey: testTOTPActiveKey}
	cfg.AuthEmail.ActiveKeyID = "k1"
	return cfg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNewSecondFactorRoutes_RegistersEveryContractRoute proves every pending,
// state, passkey, recovery, and TOTP route in
// docs/design/totp-second-factor-contract.md's "Release surface" table is
// reachable.
func TestNewSecondFactorRoutes_RegistersEveryContractRoute(t *testing.T) {
	t.Parallel()
	cfg := validSecondFactorConfig("https://aboutme.example", true)
	cfg.TOTPEnrollment = true
	runtime, err := newSecondFactorRoutes(discardLogger(), cfg, &store.Pool{})
	if err != nil {
		t.Fatalf("newSecondFactorRoutes() error = %v", err)
	}
	mux := http.NewServeMux()
	runtime.RegisterRoutes(mux)

	// Every route below requires the pending or session cookie this test
	// never sends, so each must be reachable (401, not 404) to prove
	// registration — the specific 401 body is internal/auth's own concern.
	// The TOTP enrollment path answers 404 while TOTP_ENROLLMENT_ENABLED is
	// false (see TestNewSecondFactorRoutes_TOTPEnrollmentIgnoresFlagOnlyAtRoute),
	// so this table uses an enrollment-enabled config.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/second-factor"},
		{http.MethodPost, "/api/v1/auth/second-factor/passkey/options"},
		{http.MethodPost, "/api/v1/auth/second-factor/passkey/verify"},
		{http.MethodPost, "/api/v1/auth/second-factor/recovery/verify"},
		{http.MethodPost, "/api/v1/auth/second-factor/totp/verify"},
		{http.MethodGet, "/api/v1/me/second-factor"},
		{http.MethodPost, "/api/v1/me/second-factor/passkeys"},
		{http.MethodDelete, "/api/v1/me/second-factor/passkeys/018f5b6a-9a3e-7c21-8b1e-000000000001"},
		{http.MethodPost, "/api/v1/me/second-factor/recovery-codes"},
		{http.MethodPost, "/api/v1/me/second-factor/totp/enrollment"},
		{http.MethodPut, "/api/v1/me/second-factor/totp/enrollment"},
		{http.MethodDelete, "/api/v1/me/second-factor/totp"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s %s: status = 404, want the route registered", tc.method, tc.path)
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
			runtime, err := newSecondFactorRoutes(discardLogger(), validSecondFactorConfig("https://aboutme.example", tc.enrollment), &store.Pool{})
			if err != nil {
				t.Fatalf("newSecondFactorRoutes() error = %v", err)
			}
			mux := http.NewServeMux()
			runtime.RegisterRoutes(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/me/second-factor/passkeys/options", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestNewSecondFactorRoutes_TOTPEnrollmentIgnoresFlagOnlyAtRoute proves TOTP
// enrollment start answers the uniform not-found response while
// TOTP_ENROLLMENT_ENABLED is false, before any session check, and proceeds to
// ordinary session enforcement once enabled — the same shape as the passkey
// registration options flag check, for
// docs/design/totp-second-factor-contract.md "Enrollment start and
// completion both recheck TOTP_ENROLLMENT_ENABLED".
func TestNewSecondFactorRoutes_TOTPEnrollmentIgnoresFlagOnlyAtRoute(t *testing.T) {
	t.Parallel()
	cfg := config.Config{PublicOrigin: "https://aboutme.example", TOTPActiveKey: testTOTPActiveKey}
	cfg.AuthEmail.ActiveKeyID = "k1"
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
			c := cfg
			c.TOTPEnrollment = tc.enrollment
			runtime, err := newSecondFactorRoutes(discardLogger(), c, &store.Pool{})
			if err != nil {
				t.Fatalf("newSecondFactorRoutes() error = %v", err)
			}
			mux := http.NewServeMux()
			runtime.RegisterRoutes(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/me/second-factor/totp/enrollment", nil))
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

// TestNewSecondFactorRoutes_RejectsMalformedTOTPKeyRing is the regression
// test for failing startup closed on a malformed TOTP key ring, regardless
// of TOTP_ENROLLMENT_ENABLED: verification of an existing credential must
// keep working when enrollment is off, so the ring is always built
// (docs/design/totp-key-management.md "Key ring": "Startup fails when the
// active key is missing or malformed").
func TestNewSecondFactorRoutes_RejectsMalformedTOTPKeyRing(t *testing.T) {
	t.Parallel()
	for _, enrollment := range []bool{false, true} {
		cfg := config.Config{PublicOrigin: "https://aboutme.example", TOTPEnrollment: enrollment, TOTPActiveKey: "not-a-valid-key"}
		cfg.AuthEmail.ActiveKeyID = "k1"
		_, err := newSecondFactorRoutes(discardLogger(), cfg, &store.Pool{})
		if err == nil {
			t.Fatalf("newSecondFactorRoutes(enrollment=%t) error = nil, want a malformed totp key ring rejection", enrollment)
		}
	}
}

// TestNewSecondFactorRoutes_TOTPIssuerDerivedFromValidOriginStartsCleanly
// proves a PUBLIC_ORIGIN host that secondfactor.NewRelyingParty accepts also
// satisfies ValidateTOTPIssuer when TOTP enrollment is enabled, so a normal
// production origin never fails startup on the TOTP issuer check.
// ValidateTOTPIssuer itself is exercised directly in
// internal/secondfactor/totp_provisioning_test.go; the relying-party check
// in newSecondFactorRoutes runs first and is strictly narrower (it requires
// at least two DNS labels or exactly "localhost", where ValidateTOTPIssuer
// also accepts a single label), so no origin this composition accepts can
// reach a rejected TOTP issuer.
func TestNewSecondFactorRoutes_TOTPIssuerDerivedFromValidOriginStartsCleanly(t *testing.T) {
	t.Parallel()
	cfg := config.Config{PublicOrigin: "https://aboutme.example", TOTPActiveKey: testTOTPActiveKey}
	cfg.AuthEmail.ActiveKeyID = "k1"
	for _, enrollment := range []bool{false, true} {
		c := cfg
		c.TOTPEnrollment = enrollment
		_, err := newSecondFactorRoutes(discardLogger(), c, &store.Pool{})
		if err != nil {
			t.Fatalf("newSecondFactorRoutes(enrollment=%t) error = %v, want a valid two-label host to start cleanly either way", enrollment, err)
		}
	}
}

// TestSecondFactorTOTPRoutes_KeepTheRouterExactCacheControl proves every
// TOTP route carries the router's exact `Cache-Control: no-store,
// no-transform` (internal/api/cache_policy.go), success and error alike, and
// that no TOTP handler sets its own route-specific cache policy: composing
// the real api.New router around newSecondFactorRoutes's registrar is the
// only layer that could add one.
func TestSecondFactorTOTPRoutes_KeepTheRouterExactCacheControl(t *testing.T) {
	t.Parallel()
	runtime, err := newSecondFactorRoutes(discardLogger(), validSecondFactorConfig("https://aboutme.example", true), &store.Pool{})
	if err != nil {
		t.Fatalf("newSecondFactorRoutes() error = %v", err)
	}
	handler := api.New(discardLogger(), alwaysReadyPinger{}, api.Options{}, noPublicRoutes{}, runtime.RegisterRoutes)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/second-factor/totp/verify"},
		{http.MethodPost, "/api/v1/me/second-factor/totp/enrollment"},
		{http.MethodPut, "/api/v1/me/second-factor/totp/enrollment"},
		{http.MethodDelete, "/api/v1/me/second-factor/totp"},
	} {
		rec := httptest.NewRecorder()
		// Every case below is an error response (no cookie, no session, no
		// body): the cache-policy proof is meant to hold on failure too, not
		// only on the success path this harness cannot reach without a live
		// database.
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
			t.Errorf("%s %s: Cache-Control = %q, want the router's no-store policy %q", tc.method, tc.path, got, api.CacheControlNoStore)
		}
	}
}
