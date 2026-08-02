// Package auth_test exercises Service's HTTP surface: route registration,
// the login-vs-existing-user wiring, and the generic error-handling
// obligations (__Host-oauth-tx cleared on every exit path, opaque 500 on
// an internal failure, 302 auth_failed on a rejected transaction) that
// apply across every provider, not just Google. Provider-specific OIDC
// mechanics (issuer/audience/signature/nonce/expiry, unverified email)
// live in google_test.go instead.
//
// These tests run against a live Postgres database (spec §9), reusing
// this package's existing live-DB harness (newTestQueries, defined in
// transaction_test.go) -- Service ultimately writes real users/identities
// rows, which a hermetic in-memory fake would not exercise faithfully.
package auth_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// testLogger is a *slog.Logger that discards output -- these tests assert
// on HTTP responses and database rows, not log lines.
func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// mustQueryParam parses rawURL and returns its name query parameter,
// failing the test if rawURL doesn't parse or the parameter is absent.
func mustQueryParam(t *testing.T, rawURL, name string) string {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	if !u.Query().Has(name) {
		t.Fatalf("URL %q missing query param %q", rawURL, name)
	}
	return u.Query().Get(name)
}

// testPublicOrigin is the PublicOrigin every test Service is configured
// with -- an arbitrary, valid-per-config.loadPublicOrigin origin (scheme
// + host, no path/trailing slash) these tests never actually dial.
const testPublicOrigin = "https://aboutme.example"

// noopPinger satisfies api.DBPinger without touching a database -- these
// tests never hit /healthz or /readyz, but api.New requires a pinger to
// build the handler at all.
type noopPinger struct{}

func (noopPinger) Ping(context.Context) error { return nil }

// testServiceConfig is newTestService's own scratch options struct --
// deliberately not config.Config itself, which carries no test-only
// fields; only the Google issuer override needs a seam, provided by
// auth.NewServiceForTest.
type testServiceConfig struct {
	googleIssuer string
}

// testServiceOption configures newTestService.
type testServiceOption func(*testServiceConfig)

// withGoogleIssuer arranges for the returned Service to run Google OIDC
// discovery against issuer (e.g. an oidctest.Provider's URL) instead of
// the real "https://accounts.google.com" -- see
// auth.NewServiceForTest's doc comment.
func withGoogleIssuer(issuer string) testServiceOption {
	return func(c *testServiceConfig) { c.googleIssuer = issuer }
}

// newTestService builds a Service backed by a fresh live-database
// connection (see newTestQueries) and wraps it in the exact same router
// wiring cmd/server/main.go uses (api.New + Service.RegisterRoutes), so
// these tests exercise the real registration/middleware path, not a
// bypass. It returns the resulting http.Handler and the *store.Queries
// backing it, for direct row assertions.
func newTestService(t *testing.T, opts ...testServiceOption) (http.Handler, *store.Queries) {
	t.Helper()

	q := newTestQueries(t)

	var sc testServiceConfig
	for _, opt := range opts {
		opt(&sc)
	}

	cfg := config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}

	svc, err := auth.NewServiceForTest(cfg, q, sc.googleIssuer)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}

	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)
	return handler, q
}

// doGet issues a GET request for path against handler, optionally
// carrying cookies, and returns the recorded response -- using
// httptest.ResponseRecorder.Result() (not just .Header()) specifically so
// a Set-Cookie written AFTER WriteHeader was already called (a real bug:
// on a live connection that header would never reach the browser) is
// NOT visible here, the same way a real net/http client would never see
// it either.
//
// The response Body is closed here, inside doGet, before returning: every
// caller in this package only reads headers/cookies, never the body, and
// closing it here once satisfies bodyclose for every call site instead of
// repeating a defer/nolint at each one.
func doGet(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp
}

// extractCookie returns the cookie named name from resp's Set-Cookie
// headers, or nil if absent.
func extractCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ---- Service construction / route registration -----------------------

func TestNewService_MissingPublicOrigin_ReturnsError(t *testing.T) {
	t.Parallel()

	q := newTestQueries(t)
	_, err := auth.NewService(config.Config{}, q)
	if err == nil {
		t.Fatal("NewService() error = nil, want error for a config with no PublicOrigin")
	}
}

func TestService_RegisterRoutes_GoogleStartAndCallback_RespondToGET(t *testing.T) {
	t.Parallel()

	handler, _ := newTestService(t)

	// /start with no code/state at all still runs (it begins a fresh
	// transaction and redirects) -- proving the route is registered and
	// reachable is the point here, not the full OIDC round trip (that's
	// google_test.go's job).
	startResp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Errorf("GET %s status = %d, want %d", auth.GoogleStartPath, startResp.StatusCode, http.StatusFound)
	}

	// /callback with no transaction cookie at all is a well-defined
	// rejection (missing __Host-oauth-tx), not a 404 -- proving the route
	// itself is registered.
	cbResp := doGet(t, handler, auth.GoogleCallbackPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != http.StatusFound {
		t.Errorf("GET %s (no cookie) status = %d, want %d", auth.GoogleCallbackPath, cbResp.StatusCode, http.StatusFound)
	}
}

func TestService_RegisterRoutes_WrongMethod_Returns405(t *testing.T) {
	t.Parallel()

	handler, _ := newTestService(t)

	for _, path := range []string{auth.GoogleStartPath, auth.GoogleCallbackPath} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s status = %d, want %d", path, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}

// ---- __Host-oauth-tx cookie lifecycle (ruling 1: cleared on every exit) --

// TestGoogleCallback_MissingTxCookie_RedirectsAuthFailed covers the
// simplest rejection path: no __Host-oauth-tx cookie at all. DD-C3/DD-C4
// pin this to a 302 with the generic ?error=auth_failed code (no oracle
// distinguishing this from any other rejected-transaction failure mode).
func TestGoogleCallback_MissingTxCookie_RedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	handler, _ := newTestService(t)
	before := countUsers(t)

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=whatever&state=whatever") //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set for a rejected callback, want none")
	}

	assertUsersCountUnchanged(t, before)
}

// TestGoogleCallback_StaleOrUnknownTxCookie_ClearsCookieAndRedirectsAuthFailed
// covers a syntactically-present but invalid transaction handle (never
// issued by Begin) -- ErrTransactionInvalid's path through Consume.
// Ruling 1 requires ClearOAuthTxCookie to run even here: this test proves
// it by inspecting the callback response's own Set-Cookie header for
// __Host-oauth-tx with a negative Max-Age, not just its absence.
func TestGoogleCallback_StaleOrUnknownTxCookie_ClearsCookieAndRedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	handler, _ := newTestService(t)
	before := countUsers(t)

	fakeTxCookie := &http.Cookie{Name: auth.OAuthTxCookieName, Value: "never-issued-handle-shaped-value"}
	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=c&state=s", fakeTxCookie) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := mustQueryParam(t, resp.Header.Get("Location"), "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}

	cleared := extractCookie(resp, auth.OAuthTxCookieName)
	if cleared == nil {
		t.Fatal("response missing a __Host-oauth-tx Set-Cookie header, want one clearing it (ruling 1)")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("__Host-oauth-tx MaxAge = %d, want negative (cleared)", cleared.MaxAge)
	}

	assertUsersCountUnchanged(t, before)
}

// TestGoogleCallback_StateMismatch_ClearsCookieAndRedirectsAuthFailed
// starts a real transaction (so Consume succeeds) but presents a ?state=
// that does not match what Begin generated -- the OAuth `state` parameter
// CSRF defense, independent of PKCE and independent of the
// __Host-oauth-tx cookie itself (RFC 6749 §10.12): without this check, an
// attacker who can get a victim's browser to visit
// .../callback?code=<attacker's code>&state=<anything> while the victim
// holds a legitimate pending transaction cookie could splice their own
// authorization code into the victim's transaction.
func TestGoogleCallback_StateMismatch_ClearsCookieAndRedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))
	before := countUsers(t)

	startResp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("start response missing __Host-oauth-tx cookie")
	}

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=irrelevant&state=not-the-real-state", txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := mustQueryParam(t, resp.Header.Get("Location"), "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	cleared := extractCookie(resp, auth.OAuthTxCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-oauth-tx not cleared on a state-mismatch rejection")
	}

	assertUsersCountUnchanged(t, before)
}

// countUsers returns the current number of rows in users. The test
// database is shared across this whole package's live-DB test suite (no
// per-test reset), so an absolute "count == 0" assertion is meaningless --
// countUsers/assertUsersCountUnchanged instead compare a before/after
// snapshot, the only thing a single test can meaningfully own.
func countUsers(t *testing.T) int {
	t.Helper()

	inspector := newRowInspectorPool(t)
	var count int
	if err := inspector.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return count
}

// assertUsersCountUnchanged is a coarse guard for the rejection-path
// tests above: none of them should ever create a users row, checked as a
// before/after delta rather than an absolute count (see countUsers).
func assertUsersCountUnchanged(t *testing.T, before int) {
	t.Helper()

	if after := countUsers(t); after != before {
		t.Errorf("users row count changed from %d to %d, want unchanged (a rejected callback must create nothing)", before, after)
	}
}
