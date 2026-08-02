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
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// testLogger is a *slog.Logger that discards output -- these tests assert
// on HTTP responses and database rows, not log lines (the one test that
// DOES assert on log output, TestGoogleCallback_SessionIssuanceFails_...,
// builds its own capturing logger instead -- see newCapturingLogger).
func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// newCapturingLogger returns a *slog.Logger whose JSON output lands in
// the returned *bytes.Buffer, for fix-round Important 2's log-emission
// test.
func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
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
// fields; only each provider's own endpoint-override seam is needed here,
// provided by auth.NewServiceForTest (Google) and
// auth.SetGitHubEndpointForTest (GitHub).
type testServiceConfig struct {
	googleIssuer   string
	githubEndpoint string
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

// withGitHubEndpoint arranges for the returned Service to dial endpoint
// (e.g. a newGitHubStub's httptest.Server URL, github_test.go) instead of
// the real "https://github.com" / "https://api.github.com" for every
// GitHub OAuth2/API call -- see auth.SetGitHubEndpointForTest's doc
// comment. Task 6's own copy of the issuer-override guard below: GitHub
// has no OIDC discovery to omit, but the same "a test that forgot the
// override would dial the real provider" risk applies to its OAuth2
// endpoints and REST API base URL.
func withGitHubEndpoint(endpoint string) testServiceOption {
	return func(c *testServiceConfig) { c.githubEndpoint = endpoint }
}

// newTestService builds a Service backed by a fresh live-database
// connection (see newTestQueries) and wraps it in the exact same router
// wiring cmd/server/main.go uses (api.New + Service.RegisterRoutes), so
// these tests exercise the real registration/middleware path, not a
// bypass. It returns the resulting http.Handler and the *store.Queries
// backing it, for direct row assertions.
//
// At least one provider endpoint override (withGoogleIssuer or
// withGitHubEndpoint) is REQUIRED, not optional: fix-round Critical
// finding was exactly a test that called this with no override and so
// performed live OIDC discovery against the real
// https://accounts.google.com on every hermetic-looking test run. Forcing
// at least one override here, once, makes that mistake impossible to
// repeat for either provider -- a test that supplies only
// withGitHubEndpoint never dials Google (it never calls a Google route at
// all), and vice versa, so this stays a genuine per-provider seam rather
// than a blanket requirement to supply both.
func newTestService(t *testing.T, opts ...testServiceOption) (http.Handler, *store.Queries) {
	t.Helper()

	q := newTestQueries(t)

	var sc testServiceConfig
	for _, opt := range opts {
		opt(&sc)
	}
	if sc.googleIssuer == "" && sc.githubEndpoint == "" {
		t.Fatal("newTestService: no provider endpoint override supplied (withGoogleIssuer or " +
			"withGitHubEndpoint) -- every test Service must be pointed at a local stub, never " +
			"the real https://accounts.google.com or https://github.com/api.github.com, or a " +
			"request to /start or /callback would perform live network I/O against the real provider")
	}

	cfg := config.Config{PublicOrigin: testPublicOrigin}
	if sc.googleIssuer != "" {
		cfg.GoogleClientID = oidctest.DefaultClientID
		cfg.GoogleClientSecret = "test-google-client-secret"
	}
	if sc.githubEndpoint != "" {
		cfg.GitHubClientID = "test-github-client-id"
		cfg.GitHubClientSecret = "test-github-client-secret"
	}

	svc, err := auth.NewServiceForTest(testLogger(), cfg, q, sc.googleIssuer)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	if sc.githubEndpoint != "" {
		auth.SetGitHubEndpointForTest(svc, sc.githubEndpoint)
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
// The response Body is Close()'d here before returning, but remains
// readable afterward -- ResponseRecorder.Result() wraps an in-memory
// bytes.Reader in io.NopCloser, whose Close is a genuine no-op, so a
// caller that does need the body (e.g. an internal-error envelope
// assertion) can still read it. Every other caller in this package only
// reads headers/cookies, and this Close() satisfies bodyclose for all of
// them without repeating a defer/nolint at each call site.
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

// failingSessionIssuer structurally satisfies handlers.go's unexported
// sessionIssuer interface (Go interface satisfaction needs no shared
// name across packages) -- Issue always fails, letting a test force
// Service into its writeInternalError funnel deterministically
// (fix-round Important 1: there is no realistic way to fail a real
// *SessionManager.Issue without corrupting a live database mid-request).
// The error text is deliberately distinctive so a test can also prove it
// never reaches the client response or (per Important 2) the log.
type failingSessionIssuer struct{}

func (failingSessionIssuer) Issue(context.Context, uuid.UUID, string, string) (string, store.Session, error) {
	return "", store.Session{}, errors.New("stub: session issuance deliberately failed for a test -- must never leak")
}

// ---- Service construction / route registration -----------------------

func TestNewService_MissingPublicOrigin_ReturnsError(t *testing.T) {
	t.Parallel()

	q := newTestQueries(t)
	_, err := auth.NewService(testLogger(), config.Config{}, q)
	if err == nil {
		t.Fatal("NewService() error = nil, want error for a config with no PublicOrigin")
	}
}

func TestService_RegisterRoutes_GoogleStartAndCallback_RespondToGET(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

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

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

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
// This request never establishes any specific email/subject (it is
// rejected before any provider interaction at all), so there is no
// meaningful scoped row to assert against beyond "no session cookie" --
// see google_adversarial_test.go's TestGoogleCallback_RejectsMissingTxCookie
// for the version of this scenario that DOES drive a real registered code
// through a real oidctest provider and asserts scoped no-user/no-identity.
func TestGoogleCallback_MissingTxCookie_RedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

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
}

// TestGoogleCallback_StaleOrUnknownTxCookie_ClearsCookieAndRedirectsAuthFailed
// covers a syntactically-present but invalid transaction handle (never
// issued by Begin) -- ErrTransactionInvalid's path through Consume.
// Ruling 1 requires ClearOAuthTxCookie to run even here: this test proves
// it by inspecting the callback response's own Set-Cookie header for
// __Host-oauth-tx with a negative Max-Age, not just its absence. Like
// TestGoogleCallback_MissingTxCookie_RedirectsAuthFailed above, this
// request never reaches a point where any specific email/subject is
// established (Consume itself rejects the handle before any provider
// interaction), so there is no scoped row to check beyond "no session
// cookie".
func TestGoogleCallback_StaleOrUnknownTxCookie_ClearsCookieAndRedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

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
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set for a rejected callback, want none")
	}
}

// TestGoogleCallback_StateMismatch_ClearsCookieAndRedirectsAuthFailed
// starts a real transaction (so Consume succeeds) but presents a ?state=
// that does not match what Begin generated -- the OAuth `state` parameter
// CSRF defense, independent of PKCE and independent of the
// __Host-oauth-tx cookie itself (RFC 6749 §10.12): without this check, an
// attacker who can get a victim's browser to visit
// .../callback?code=<attacker's code>&state=<anything> while the victim
// holds a legitimate pending transaction cookie could splice their own
// authorization code into the victim's transaction. Unlike the two tests
// above, this flow DOES have a real transaction and a real registered
// code, so (fix-round Important 3) it asserts a scoped
// assertNoUser/assertNoIdentity instead of a global row count --
// uniqueSubject/uniqueEmail/assertNoUser/assertNoIdentity are
// google_adversarial_test.go's helpers, shared here since both files are
// package auth_test.
func TestGoogleCallback_StateMismatch_ClearsCookieAndRedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginGoogle(t, handler) // beginGoogle: google_adversarial_test.go; state deliberately discarded below
	p.RegisterCode("code-state-mismatch-handlers", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=code-state-mismatch-handlers&state=not-the-real-state", txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
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
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set for a rejected callback, want none")
	}

	assertNoUser(t, q, email)
	assertNoIdentity(t, q, subject)
}

// ---- fix-round Important 1 + 2: internal-error (500) funnel -----------

// TestGoogleCallback_SessionIssuanceFails_Returns500ClearsCookieAndLogs
// drives a FULLY successful OIDC round trip (real provider, real PKCE,
// real nonce, verified email) all the way to the last step -- session
// issuance -- then, via SetSessionIssuerForTest's seam, forces that one
// step to fail. This is the only realistic way to reach handleGoogleCallback's
// writeInternalError funnel deterministically (every other internal-error
// call site requires an actual database failure). It asserts, together,
// both fix-round obligations that funnel must satisfy:
//
//   - Important 1: __Host-oauth-tx is cleared, the response body is the
//     generic {"error":{"code":"internal_error",...}} envelope with no
//     trace of the underlying stub error's text, and no __Host-session
//     cookie is set.
//   - Important 2: at least one log record is emitted, carrying a
//     request id, and containing neither the authorization code used in
//     this exchange nor the underlying stub error's own message (proving
//     logInternalError's deliberate choice not to log raw error text).
func TestGoogleCallback_SessionIssuanceFails_Returns500ClearsCookieAndLogs(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)
	logger, logBuf := newCapturingLogger()

	cfg := config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(logger, cfg, q, p.URL)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	auth.SetSessionIssuerForTest(svc, failingSessionIssuer{})

	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)

	const secretCode = "code-session-issue-fails-must-not-leak-into-body-or-log"
	p.RegisterCode(secretCode, oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, secretCode, state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning (still readable; see doGet's doc comment).

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set despite Issue failing, want none")
	}
	cleared := extractCookie(resp, auth.OAuthTxCookieName)
	if cleared == nil {
		t.Fatal("response missing a __Host-oauth-tx Set-Cookie header on a 500, want one clearing it (Important 1)")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("__Host-oauth-tx MaxAge = %d, want negative (cleared)", cleared.MaxAge)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"internal_error"`) {
		t.Errorf("response body = %q, want it to contain the generic internal_error code", bodyStr)
	}
	if strings.Contains(bodyStr, "deliberately failed") || strings.Contains(bodyStr, secretCode) {
		t.Errorf("response body = %q, leaked the underlying stub error or authorization code -- must be opaque", bodyStr)
	}

	logged := logBuf.String()
	if logged == "" {
		t.Fatal("no log record was emitted for an internal-error (500) failure, want at least one (Important 2)")
	}
	if !strings.Contains(logged, `"request_id"`) {
		t.Errorf("log record = %q, want a request_id field", logged)
	}
	if strings.Contains(logged, "deliberately failed") {
		t.Errorf("log record = %q, contains the underlying stub error's own text -- logInternalError must log only a fixed op string, never raw error text (which for a real DB error can embed PII)", logged)
	}
	if strings.Contains(logged, secretCode) {
		t.Errorf("log record = %q, leaked the authorization code -- must never log OAuth codes/tokens/secrets", logged)
	}
	if strings.Contains(logged, email) {
		t.Errorf("log record = %q, leaked the visitor's email -- must never log emails", logged)
	}
}

// ---- fix-round ruling b2: provider-signaled cancel ---------------------

// TestGoogleCallback_ProviderAccessDenied_RedirectsCancelled proves a
// callback carrying ?error=access_denied (RFC 6749 §4.1.2.1's signal that
// the visitor declined consent at the provider) redirects with its own
// distinct error code (handlers.go's cancelledErrorCode -- not the
// generic auth_failed) while still clearing the transaction cookie and
// never authenticating the visitor.
func TestGoogleCallback_ProviderAccessDenied_RedirectsCancelled(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	txCookie, state, _ := beginGoogle(t, handler)

	path := auth.GoogleCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	const wantErrorCode = "cancelled" //nolint:misspell // exact, ruling-specified wire value (double-L "cancelled"), not a typo for "canceled"
	if got := mustQueryParam(t, resp.Header.Get("Location"), "error"); got != wantErrorCode {
		t.Errorf("error param = %q, want %q (RFC 6749 access_denied maps to its own distinct code)", got, wantErrorCode)
	}
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set on a canceled login, want none")
	}
	cleared := extractCookie(resp, auth.OAuthTxCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-oauth-tx not cleared on a canceled login")
	}
}
