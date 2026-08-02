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

// assertRedirectPath fails the test unless rawURL parses and its path
// equals wantPath -- DD-C7 (integration-owner ruling, task-4-brief.md's
// re-review) pins every /callback REJECTION's redirect target to
// PublicOrigin+"/login" specifically so the frontend's login screen is
// what renders the ?error= code, while a SUCCESSFUL callback keeps
// redirecting to the bare PublicOrigin+"/" -- these two must never be
// pinned to the same literal, or a future edit could silently merge them
// back together.
func assertRedirectPath(t *testing.T, rawURL, wantPath string) {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse redirect URL %q: %v", rawURL, err)
	}
	if u.Path != wantPath {
		t.Errorf("redirect Location path = %q, want %q (rawURL=%q)", u.Path, wantPath, rawURL)
	}
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
// fields; the Google/LinkedIn issuer overrides need a seam, provided by
// auth.NewServiceForTest.
type testServiceConfig struct {
	googleIssuer   string
	linkedinIssuer string
	logger         *slog.Logger
	sessionIssuer  sessionIssuerForTest
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

// withLinkedInIssuer is withGoogleIssuer's LinkedIn counterpart: arranges
// for the returned Service to run LinkedIn OIDC discovery against issuer
// instead of the real "https://www.linkedin.com/oauth". Unlike
// withGoogleIssuer, newTestService does NOT force every caller to supply
// this (see newTestService's own doc comment for why) -- only a test that
// actually drives a LinkedIn route needs it.
func withLinkedInIssuer(issuer string) testServiceOption {
	return func(c *testServiceConfig) { c.linkedinIssuer = issuer }
}

// withLogger arranges for the returned Service's own logger (the one
// logRejection/logInternalError write to -- NOT api.New's separate
// request-logging middleware, which stays discarded regardless) to be
// logger instead of a discarding testLogger(). It exists so a test that
// needs to capture this Service's own log output (e.g.
// newCapturingLogger's buffer) can still go through newTestService's
// guarded construction instead of hand-building a Service directly and
// bypassing the "every test Service must have a real issuer override"
// guard below.
func withLogger(logger *slog.Logger) testServiceOption {
	return func(c *testServiceConfig) { c.logger = logger }
}

// sessionIssuerForTest structurally mirrors handlers.go's unexported
// sessionIssuer interface (Go interface satisfaction needs no shared name
// across packages -- failingSessionIssuer below already relies on this),
// so withSessionIssuer can accept anything satisfying it without
// importing the unexported type itself.
type sessionIssuerForTest interface {
	Issue(ctx context.Context, userID uuid.UUID, ua, ip string) (rawToken string, sess store.Session, err error)
}

// withSessionIssuer arranges for the returned Service's session-issuance
// seam (auth.SetSessionIssuerForTest) to be si instead of a real
// *SessionManager -- e.g. failingSessionIssuer, to deterministically force
// the writeInternalError funnel (there is no realistic way to fail a real
// *SessionManager.Issue without corrupting a live database mid-request).
func withSessionIssuer(si sessionIssuerForTest) testServiceOption {
	return func(c *testServiceConfig) { c.sessionIssuer = si }
}

// newTestService builds a Service backed by a fresh live-database
// connection (see newTestQueries) and wraps it in the exact same router
// wiring cmd/server/main.go uses (api.New + Service.RegisterRoutes), so
// these tests exercise the real registration/middleware path, not a
// bypass. It returns the resulting http.Handler and the *store.Queries
// backing it, for direct row assertions.
//
// A non-empty Google issuer override (withGoogleIssuer) is REQUIRED, not
// optional: fix-round Critical finding was exactly a test that called
// this with no override and so performed live OIDC discovery against the
// real https://accounts.google.com on every hermetic-looking test run.
// Forcing this here, once, makes that mistake impossible to repeat.
//
// A LinkedIn issuer override (withLinkedInIssuer) is NOT forced the same
// way: unlike Google, no test in this package drives a LinkedIn route by
// default, so an omitted override is inert (linkedin_test.go's own tests
// always supply one via withLinkedInIssuer, the same discipline Task 4
// established for Google).
func newTestService(t *testing.T, opts ...testServiceOption) (http.Handler, *store.Queries) {
	t.Helper()

	q := newTestQueries(t)

	var sc testServiceConfig
	for _, opt := range opts {
		opt(&sc)
	}
	if sc.googleIssuer == "" {
		t.Fatal("newTestService: no Google issuer override supplied (withGoogleIssuer) -- " +
			"every test Service must be pointed at an oidctest.Provider, never the real " +
			"https://accounts.google.com, or a request to /start or /callback would " +
			"perform live OIDC discovery against the real network")
	}

	cfg := config.Config{
		PublicOrigin:         testPublicOrigin,
		GoogleClientID:       oidctest.DefaultClientID,
		GoogleClientSecret:   "test-google-client-secret",
		LinkedInClientID:     oidctest.DefaultClientID,
		LinkedInClientSecret: "test-linkedin-client-secret",
	}

	logger := sc.logger
	if logger == nil {
		logger = testLogger()
	}

	svc, err := auth.NewServiceForTest(logger, cfg, q, sc.googleIssuer, sc.linkedinIssuer)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	if sc.sessionIssuer != nil {
		auth.SetSessionIssuerForTest(svc, sc.sessionIssuer)
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

// TestGoogleProvider_DiscoveryDoesNotHoldCacheMutex proves the lazy OIDC
// provider-discovery cache (provider_cache.go's oidcProviderCache, shared
// by google.go and linkedin.go) does NOT hold its own mutex for the
// duration of the discovery network call -- an earlier version of this
// pattern did, which meant every OTHER concurrent /start or /callback
// request (for ANY purpose, not just ones needing this same provider)
// blocked behind a single slow or hung discovery attempt against a real,
// uncontrolled external dependency.
//
// oidctest.Provider.BlockDiscoveryForTest deterministically holds the
// discovery HTTP response open (no sleep/timing race: `entered` closes
// the instant the handler is reached, proving the dial is genuinely in
// flight) while this test tries to acquire the SAME cache's mutex from a
// second, independent goroutine via GoogleProviderCacheTryLockForTest --
// under the old (buggy) pattern that acquisition would block until
// discovery completed; under the fixed pattern it succeeds immediately.
func TestGoogleProvider_DiscoveryDoesNotHoldCacheMutex(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)
	cfg := config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(testLogger(), cfg, q, p.URL, "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)

	entered, release := p.BlockDiscoveryForTest()
	t.Cleanup(release) // in case the test fails before reaching the explicit release() below

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	}()

	select {
	case <-entered:
		// Discovery is now genuinely blocked mid-flight (inside
		// oidc.NewProvider's HTTP round trip); the assertion below
		// happens exactly while that is true.
	case <-requestDone:
		t.Fatal("GET /start finished before discovery's blocked endpoint was ever entered -- test setup is broken, not the code under test")
	}

	unlock, ok := auth.GoogleProviderCacheTryLockForTest(svc)
	if !ok {
		t.Error("google provider cache mutex is held while discovery's network call is still in flight -- discover must not hold its mutex across oidc.NewProvider")
	} else {
		unlock()
	}

	release()
	<-requestDone
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
	assertRedirectPath(t, cbResp.Header.Get("Location"), "/login") // DD-C7
}

// TestService_RegisterRoutes_WrongMethod_Returns405 covers POST (any
// method net/http's own "GET /path" mux pattern would never let through
// regardless) and, separately, HEAD -- DD-C8 (integration-owner ruling):
// unlike internal/api's own route helper (and Go's stdlib ServeMux
// pattern matching), which both let HEAD satisfy a registered GET route
// per RFC 9110 §9.3.2, this package's own routes deliberately do NOT: see
// TestService_HeadRequest_DoesNotBeginTransaction and
// TestService_HeadRequest_DoesNotConsumeTransaction below for why (a HEAD
// prefetcher must never create or burn a real OAuth transaction).
func TestService_RegisterRoutes_WrongMethod_Returns405(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	paths := []string{auth.GoogleStartPath, auth.GoogleCallbackPath, auth.LinkedInStartPath, auth.LinkedInCallbackPath}
	for _, path := range paths {
		for _, method := range []string{http.MethodPost, http.MethodHead} {
			req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status = %d, want %d", method, path, rec.Code, http.StatusMethodNotAllowed)
			}
		}
	}
}

// TestService_HeadRequest_DoesNotBeginTransaction is DD-C8's first half:
// a HEAD /start request -- the shape a link-preview/prefetch crawler
// sends without ever intending to complete the flow -- must be rejected
// by the method check BEFORE handleGoogleStart ever runs, so it never
// performs handleGoogleStart's real side effect (a database INSERT via
// TransactionStore.Begin) or sets a real __Host-oauth-tx cookie. Proven
// here by the absence of any Set-Cookie header for it at all -- a
// well-formed GET always sets one (see beginGoogle).
func TestService_HeadRequest_DoesNotBeginTransaction(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodHead, auth.GoogleStartPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD %s status = %d, want %d", auth.GoogleStartPath, rec.Code, http.StatusMethodNotAllowed)
	}
	if extractCookie(rec.Result(), auth.OAuthTxCookieName) != nil { //nolint:bodyclose // ResponseRecorder.Result()'s Body is an in-memory NopCloser; nothing to leak.
		t.Error("HEAD /start set a __Host-oauth-tx cookie, want none -- it must never begin a real transaction")
	}
}

// TestService_HeadRequest_DoesNotConsumeTransaction is DD-C8's second
// half: begins a REAL transaction via a genuine GET /start, then sends a
// HEAD /callback carrying that transaction's own cookie and a
// plausible-looking code/state -- proving the method check rejects it
// (405) BEFORE handleGoogleCallback ever calls TransactionStore.Consume,
// by then completing the SAME transaction with a real GET /callback and
// observing it still succeeds. TransactionStore.Consume is single-use
// (transaction_test.go's own replay tests): if the earlier HEAD had
// reached Consume, this second, real completion would fail with
// ErrTransactionInvalid instead of succeeding.
func TestService_HeadRequest_DoesNotConsumeTransaction(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-head-does-not-consume", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	headPath := auth.GoogleCallbackPath + "?code=code-head-does-not-consume&state=" + state
	headReq := httptest.NewRequestWithContext(context.Background(), http.MethodHead, headPath, nil)
	headReq.AddCookie(txCookie)
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD %s status = %d, want %d", headPath, headRec.Code, http.StatusMethodNotAllowed)
	}

	realResp := doCallback(t, handler, "code-head-does-not-consume", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if realResp.StatusCode != http.StatusFound {
		t.Fatalf("GET callback (after an earlier HEAD) status = %d, want %d", realResp.StatusCode, http.StatusFound)
	}
	if extractCookie(realResp, auth.SessionCookieName) == nil {
		t.Error("GET callback (after an earlier HEAD) did not authenticate -- the earlier HEAD must not have burned the transaction")
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
	assertRedirectPath(t, loc, "/login") // DD-C7
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
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	assertRedirectPath(t, loc, "/login") // DD-C7

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
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	assertRedirectPath(t, loc, "/login") // DD-C7
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
	logger, logBuf := newCapturingLogger()
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger), withSessionIssuer(failingSessionIssuer{}))

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
	if !strings.Contains(logged, `"provider":"google"`) {
		t.Errorf("log record = %q, want a provider attribute identifying which provider's callback failed", logged)
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

// TestGoogleCallback_ResolveUserConflict_LogsSQLSTATE forces a REAL
// Postgres constraint violation (not a stub) through resolveGoogleUser's
// CreateUser call: a first login seeds a users row for email, then a
// SECOND login with a different subject but the SAME email hits
// users_email_key's UNIQUE constraint -- resolveGoogleUser is still
// task-4-brief.md's documented stub (no pre-check against an existing
// email; Task 10 replaces this), so this is a completely ordinary way to
// reach it. logInternalError must log the five-character SQLSTATE class
// code (pgconn.PgError.Code -- "23505" for unique_violation) so an
// operator can distinguish a real constraint failure from any other
// internal error, but NEVER the raw postgres message/detail text or the
// constraint name, both of which can embed the bound email value
// (Postgres's own "Key (email)=(...) already exists" detail).
func TestGoogleCallback_ResolveUserConflict_LogsSQLSTATE(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	logger, logBuf := newCapturingLogger()
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger))

	email := uniqueEmail(t)

	// Seed: an ordinary successful login creates the users row that the
	// second login below collides with.
	seedSubject := uniqueSubject(t)
	seedTxCookie, seedState, seedNonce := beginGoogle(t, handler)
	p.RegisterCode("code-sqlstate-seed", oidctest.Claims{
		Subject:       seedSubject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         seedNonce,
	})
	seedResp := doCallback(t, handler, "code-sqlstate-seed", seedState, seedTxCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if seedResp.StatusCode != http.StatusFound || extractCookie(seedResp, auth.SessionCookieName) == nil {
		t.Fatalf("seed login did not succeed (status=%d): setup is broken, not the code under test", seedResp.StatusCode)
	}

	// Collision: a different provider subject, but the SAME email --
	// resolveGoogleUser's CreateUser hits users_email_key.
	conflictSubject := uniqueSubject(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-sqlstate-conflict", oidctest.Claims{
		Subject:       conflictSubject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})
	resp := doCallback(t, handler, "code-sqlstate-conflict", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (a unique-constraint violation is an internal error, not a rejected callback)", resp.StatusCode, http.StatusInternalServerError)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `"sqlstate":"23505"`) {
		t.Errorf("log record = %q, want it to contain the unique_violation SQLSTATE (\"sqlstate\":\"23505\")", logged)
	}
	if !strings.Contains(logged, `"provider":"google"`) {
		t.Errorf("log record = %q, want a provider attribute identifying which provider's callback failed", logged)
	}
	if strings.Contains(logged, email) {
		t.Errorf("log record = %q, leaked the colliding email -- SQLSTATE logging must never include raw postgres message/detail text", logged)
	}
	if strings.Contains(logged, "duplicate key") || strings.Contains(logged, "users_email_key") {
		t.Errorf("log record = %q, leaked the raw postgres error message or constraint name -- want the SQLSTATE code only", logged)
	}
}

// TestGoogleCallback_RejectionLogsProviderAttribute proves logRejection's
// output identifies WHICH provider's callback was rejected: the shared
// funnel (redirectWithError/redirectAuthFailed) is reused by every
// provider Service registers, so its log message is deliberately
// provider-neutral ("auth: callback rejected", not "auth: google callback
// rejected") -- the provider attribute is what still lets an operator
// filter or correlate by provider in a multi-provider log stream.
func TestGoogleCallback_RejectionLogsProviderAttribute(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	logger, logBuf := newCapturingLogger()
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger))

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=whatever&state=whatever") //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `"provider":"google"`) {
		t.Errorf("log record = %q, want a provider attribute identifying which provider's callback was rejected", logged)
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
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != wantErrorCode {
		t.Errorf("error param = %q, want %q (RFC 6749 access_denied maps to its own distinct code)", got, wantErrorCode)
	}
	assertRedirectPath(t, loc, "/login") // DD-C7
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set on a canceled login, want none")
	}
	cleared := extractCookie(resp, auth.OAuthTxCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-oauth-tx not cleared on a canceled login")
	}
}
