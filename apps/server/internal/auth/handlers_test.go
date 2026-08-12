// Package auth_test exercises shared auth routes, identity resolution, and
// error funnels against live Postgres.
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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// testLogger discards JSON logs.
func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// newCapturingLogger returns a logger and the buffer that receives its JSON.
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

// assertRedirectPath fails unless rawURL parses and has wantPath. Callback
// rejection and success tests pass distinct paths so they cannot collapse to
// one redirect target unnoticed.
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

// testServiceConfig holds test-only Service overrides.
type testServiceConfig struct {
	googleIssuer    string
	githubEndpoint  string
	linkedinIssuer  string
	logger          *slog.Logger
	sessionIssuer   sessionIssuerForTest
	startRateLimit  int
	startRateWindow time.Duration
}

// testServiceOption configures newTestService.
type testServiceOption func(*testServiceConfig)

// withGoogleIssuer directs discovery to a test issuer.
func withGoogleIssuer(issuer string) testServiceOption {
	return func(c *testServiceConfig) { c.googleIssuer = issuer }
}

// withGitHubEndpoint directs every GitHub OAuth2 and API call to a test server.
func withGitHubEndpoint(endpoint string) testServiceOption {
	return func(c *testServiceConfig) { c.githubEndpoint = endpoint }
}

// withLinkedInIssuer directs LinkedIn discovery to a local issuer. An omitted
// override resolves to NewServiceForTest's unroutable sentinel, so tests cannot
// contact the real provider.
func withLinkedInIssuer(issuer string) testServiceOption {
	return func(c *testServiceConfig) { c.linkedinIssuer = issuer }
}

// withLogger captures Service rejection and internal-error logs.
func withLogger(logger *slog.Logger) testServiceOption {
	return func(c *testServiceConfig) { c.logger = logger }
}

// withStartRateLimit sets a small budget before route registration.
func withStartRateLimit(requests int, window time.Duration) testServiceOption {
	return func(c *testServiceConfig) { c.startRateLimit, c.startRateWindow = requests, window }
}

// sessionIssuerForTest mirrors the unexported issuance seam structurally.
type sessionIssuerForTest interface {
	Issue(ctx context.Context, userID uuid.UUID, ua, ip string) (rawToken string, sess store.Session, err error)
}

// withSessionIssuer injects deterministic issuance failures.
func withSessionIssuer(si sessionIssuerForTest) testServiceOption {
	return func(c *testServiceConfig) { c.sessionIssuer = si }
}

// newTestService builds the production router over live queries. It requires a
// Google or GitHub test endpoint; all omitted endpoints become unroutable.
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
	if sc.linkedinIssuer != "" {
		cfg.LinkedInClientID = oidctest.DefaultClientID
		cfg.LinkedInClientSecret = "test-linkedin-client-secret"
	}

	logger := sc.logger
	if logger == nil {
		logger = testLogger()
	}

	// Empty overrides pass through so NewServiceForTest applies the same
	// unroutable sentinel for every provider.
	svc, err := auth.NewServiceForTest(logger, cfg, q, sc.googleIssuer, sc.githubEndpoint, sc.linkedinIssuer)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	if sc.sessionIssuer != nil {
		auth.SetSessionIssuerForTest(svc, sc.sessionIssuer)
	}
	if sc.startRateLimit > 0 {
		auth.SetStartRateLimitForTest(svc, sc.startRateLimit, sc.startRateWindow)
	}

	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)
	return handler, q
}

// doGet returns ResponseRecorder.Result so headers written after WriteHeader
// stay invisible as they would on the wire. Its in-memory body remains readable
// after Close.
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

// requestCookie builds a Cookie request-header value. Secure, HttpOnly, and
// SameSite are response attributes with no request representation; the inline
// suppression applies only to this centralized request helper.
func requestCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value} // nosemgrep: go.lang.security.audit.net.cookie-missing-httponly.cookie-missing-httponly,go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- request-side cookie (see this function's own doc comment): Secure/HttpOnly/SameSite are response-cookie attributes with no meaning on a Cookie header this helper builds for req.AddCookie.
}

// failingSessionIssuer reaches the internal-error funnel deterministically.
// Its distinctive error text must not reach clients or logs.
type failingSessionIssuer struct{}

func (failingSessionIssuer) Issue(context.Context, uuid.UUID, string, string) (string, store.Session, error) {
	return "", store.Session{}, errors.New("stub: session issuance deliberately failed for a test -- must never leak")
}

// pgErrorSessionIssuer fails with a *pgconn.PgError shaped exactly like a real
// users_email_key unique-violation (the same SQLSTATE/message/detail/
// constraint shape a genuine concurrent-registration race would produce),
// deterministically exercising SQLSTATE extraction and redaction without a
// database race.
type pgErrorSessionIssuer struct{}

// These fields model a unique violation whose detail contains bound PII.
const (
	pgUniqueViolationMessage    = `duplicate key value violates unique constraint "users_email_key"`
	pgUniqueViolationDetail     = "Key (email)=(pg-error-test@example.com) already exists."
	pgUniqueViolationConstraint = "users_email_key"
)

func (pgErrorSessionIssuer) Issue(context.Context, uuid.UUID, string, string) (string, store.Session, error) {
	return "", store.Session{}, &pgconn.PgError{
		Code:           "23505",
		Message:        pgUniqueViolationMessage,
		Detail:         pgUniqueViolationDetail,
		ConstraintName: pgUniqueViolationConstraint,
	}
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

// ---- shared login identity resolution ------------------------------------

// TestResolveLoginIdentity_NewThenExisting_AcrossProviders checks new and
// existing identity outcomes for each provider. Row counts distinguish reuse
// from a failed duplicate insert.
func TestResolveLoginIdentity_NewThenExisting_AcrossProviders(t *testing.T) {
	t.Parallel()

	providers := []auth.Provider{auth.ProviderGoogle, auth.ProviderGitHub, auth.ProviderLinkedIn}

	for _, provider := range providers {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()

			q := newTestQueries(t)
			svc, err := auth.NewServiceForTest(testLogger(), config.Config{
				PublicOrigin:         testPublicOrigin,
				GoogleClientID:       oidctest.DefaultClientID,
				GoogleClientSecret:   "test-google-client-secret",
				LinkedInClientID:     oidctest.DefaultClientID,
				LinkedInClientSecret: "test-linkedin-client-secret",
			}, q, "", "", "")
			if err != nil {
				t.Fatalf("NewServiceForTest() error = %v", err)
			}

			providerUserID := uuid.NewString()
			email := uniqueEmail(t)
			ctx := context.Background()

			first, err := auth.ResolveLoginIdentityForTest(ctx, svc, provider, providerUserID, email, "Test User")
			if err != nil {
				t.Fatalf("resolveLoginIdentity() (first login) error = %v", err)
			}
			if first.Kind != auth.LoginResultNewUserForTest {
				t.Fatalf("first login Kind = %d, want LoginResultNewUserForTest (%d)", first.Kind, auth.LoginResultNewUserForTest)
			}
			if first.User.Email != email {
				t.Errorf("first login User.Email = %q, want %q", first.User.Email, email)
			}

			second, err := auth.ResolveLoginIdentityForTest(ctx, svc, provider, providerUserID, email, "Test User")
			if err != nil {
				t.Fatalf("resolveLoginIdentity() (second login) error = %v", err)
			}
			if second.Kind != auth.LoginResultExistingIdentityForTest {
				t.Fatalf("second login Kind = %d, want LoginResultExistingIdentityForTest (%d)", second.Kind, auth.LoginResultExistingIdentityForTest)
			}
			if second.User.ID != first.User.ID {
				t.Errorf("second login User.ID = %v, want %v (the SAME user, not a new one)", second.User.ID, first.User.ID)
			}

			inspector := newRowInspectorPool(t)
			var identityCount int
			if err := inspector.QueryRow(ctx,
				`SELECT count(*) FROM identities WHERE provider = $1 AND provider_user_id = $2`, string(provider), providerUserID,
			).Scan(&identityCount); err != nil {
				t.Fatalf("count identities: %v", err)
			}
			if identityCount != 1 {
				t.Errorf("identities row count for (%s, %q) = %d, want exactly 1 (no duplicate row from the second login)", provider, providerUserID, identityCount)
			}

			var userCount int
			if err := inspector.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE email = $1`, email,
			).Scan(&userCount); err != nil {
				t.Fatalf("count users: %v", err)
			}
			if userCount != 1 {
				t.Errorf("users row count for %q = %d, want exactly 1 (no duplicate user from the second login)", email, userCount)
			}
		})
	}
}

// TestGoogleProvider_DiscoveryDoesNotHoldCacheMutex blocks discovery without a
// timing race, then proves another goroutine can acquire the cache mutex.
func TestGoogleProvider_DiscoveryDoesNotHoldCacheMutex(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)
	cfg := config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(testLogger(), cfg, q, p.URL, "", "")
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

// TestDefaultTestOverride_DefaultsToUnroutableSentinelWhenOmitted proves an
// empty provider override cannot leave a Service pointed at a real endpoint.
func TestDefaultTestOverride_DefaultsToUnroutableSentinelWhenOmitted(t *testing.T) {
	t.Parallel()

	if got := auth.DefaultTestOverrideForTest(""); got != auth.UnroutableTestSentinel {
		t.Errorf("DefaultTestOverrideForTest(\"\") = %q, want the unroutable sentinel %q", got, auth.UnroutableTestSentinel)
	}
}

// TestDefaultTestOverride_UsesExplicitOverrideWhenSupplied proves an explicit
// test endpoint wins over the sentinel default.
func TestDefaultTestOverride_UsesExplicitOverrideWhenSupplied(t *testing.T) {
	t.Parallel()

	if got := auth.DefaultTestOverrideForTest("http://example.invalid"); got != "http://example.invalid" {
		t.Errorf("DefaultTestOverrideForTest(explicit override) = %q, want %q", got, "http://example.invalid")
	}
}

// TestService_LinkedInRoute_OmittedIssuerOverride_FailsFastOffline proves an
// omitted LinkedIn issuer reaches only the loopback sentinel and returns an
// opaque internal error.
func TestService_LinkedInRoute_OmittedIssuerOverride_FailsFastOffline(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL)) // no withLinkedInIssuer

	resp := doGet(t, handler, auth.LinkedInStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("GET %s (no LinkedIn issuer override) status = %d, want %d (discovery against the unroutable sentinel must fail fast, not silently succeed against the real network)",
			auth.LinkedInStartPath, resp.StatusCode, http.StatusInternalServerError)
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
	assertRedirectPath(t, cbResp.Header.Get("Location"), "/login") // Rejected callbacks return to login.
}

// TestService_RegisterRoutes_WrongMethod_Returns405 pins exact methods. HEAD
// must not inherit GET because a crawler could create or consume a transaction.
func TestService_RegisterRoutes_WrongMethod_Returns405(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	cases := []struct {
		path    string
		methods []string
	}{
		{auth.GoogleStartPath, []string{http.MethodHead, http.MethodPatch}},
		{auth.LinkedInStartPath, []string{http.MethodHead, http.MethodPatch}},
		{auth.GoogleCallbackPath, []string{http.MethodPost, http.MethodHead}},
		{auth.LinkedInCallbackPath, []string{http.MethodPost, http.MethodHead}},
	}
	for _, c := range cases {
		for _, method := range c.methods {
			req := httptest.NewRequestWithContext(context.Background(), method, c.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status = %d, want %d", method, c.path, rec.Code, http.StatusMethodNotAllowed)
			}
		}
	}
}

// TestService_HeadRequest_DoesNotBeginTransaction proves method rejection
// occurs before the start handler by asserting no transaction cookie.
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

// TestService_HeadRequest_DoesNotConsumeTransaction sends HEAD with a real
// transaction, then proves a GET can still consume the single-use handle.
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

// ---- __Host-oauth-tx cookie lifecycle -------------------------------------

// TestGoogleCallback_MissingTxCookie_RedirectsAuthFailed checks the earliest
// rejection path before any provider identity exists.
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
	assertRedirectPath(t, loc, "/login") // Rejected callbacks return to login.
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set for a rejected callback, want none")
	}
}

// TestGoogleCallback_StaleOrUnknownTxCookie_ClearsCookieAndRedirectsAuthFailed
// proves an unknown handle emits an explicit deletion cookie before provider
// identity lookup.
func TestGoogleCallback_StaleOrUnknownTxCookie_ClearsCookieAndRedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	fakeTxCookie := requestCookie(auth.OAuthTxCookieName, "never-issued-handle-shaped-value")
	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=c&state=s", fakeTxCookie) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	assertRedirectPath(t, loc, "/login") // Rejected callbacks return to login.

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

// TestGoogleCallback_StateMismatch_ClearsCookieAndRedirectsAuthFailed proves a
// valid transaction cookie cannot bind an attacker-controlled code. Scoped row
// checks prove rejection precedes account creation.
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
	assertRedirectPath(t, loc, "/login") // Rejected callbacks return to login.
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

// ---- internal-error funnel -----------------------------------------------

// TestGoogleCallback_SessionIssuanceFails_Returns500ClearsCookieAndLogs injects
// failure after a valid OIDC exchange. It checks the opaque response, cookie
// effects, request ID, and absence of secrets or raw error text in logs.
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

// TestGoogleCallback_SessionIssuanceUniqueViolation_LogsSQLSTATEOnly is
// TestGoogleCallback_SessionIssuanceUniqueViolation_LogsSQLSTATEOnly proves
// logInternalError records only PgError.Code, never Message, Detail, or
// ConstraintName. A synthetic unique violation at session issuance exercises
// the HTTP error path deterministically without relying on a database race.
func TestGoogleCallback_SessionIssuanceUniqueViolation_LogsSQLSTATEOnly(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	logger, logBuf := newCapturingLogger()
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger), withSessionIssuer(pgErrorSessionIssuer{}))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-sqlstate-session-issuance", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})
	resp := doCallback(t, handler, "code-sqlstate-session-issuance", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (session issuance failed with a unique-violation-shaped error)", resp.StatusCode, http.StatusInternalServerError)
	}
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set despite session issuance itself failing")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"internal_error"`) {
		t.Errorf("response body = %q, want it to contain the generic internal_error code", bodyStr)
	}
	if strings.Contains(bodyStr, pgUniqueViolationMessage) || strings.Contains(bodyStr, pgUniqueViolationDetail) || strings.Contains(bodyStr, pgUniqueViolationConstraint) {
		t.Errorf("response body = %q, leaked the underlying Postgres error text -- must be opaque", bodyStr)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `"sqlstate":"23505"`) {
		t.Errorf("log record = %q, want it to contain the unique_violation SQLSTATE (\"sqlstate\":\"23505\")", logged)
	}
	if !strings.Contains(logged, `"provider":"google"`) {
		t.Errorf("log record = %q, want a provider attribute identifying which provider's callback failed", logged)
	}
	if strings.Contains(logged, pgUniqueViolationMessage) {
		t.Errorf("log record = %q, leaked the raw postgres error message -- logInternalError must log only the SQLSTATE code, never message text (which for a real DB error can embed PII)", logged)
	}
	if strings.Contains(logged, pgUniqueViolationDetail) {
		t.Errorf("log record = %q, leaked the raw postgres detail text (which embeds a bound email value for a real violation)", logged)
	}
	if strings.Contains(logged, pgUniqueViolationConstraint) {
		t.Errorf("log record = %q, leaked the constraint name -- want the SQLSTATE code only", logged)
	}
	if strings.Contains(logged, email) {
		t.Errorf("log record = %q, leaked the visitor's email", logged)
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

// ---- provider-signaled cancellation --------------------------------------

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
	const wantErrorCode = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != wantErrorCode {
		t.Errorf("error param = %q, want %q (RFC 6749 access_denied maps to its own distinct code)", got, wantErrorCode)
	}
	assertRedirectPath(t, loc, "/login") // Rejected callbacks return to login.
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set on a canceled login, want none")
	}
	cleared := extractCookie(resp, auth.OAuthTxCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-oauth-tx not cleared on a canceled login")
	}
}

// ---- injected session-manager consistency --------------------------------

// TestGoogleCallback_LoginIssuesSessionUsingInjectedSessionManagerClock proves
// SetSessionManagerForTest updates both authentication and issuance paths. The
// issued session must use the injected epoch clock, not wall time.
func TestGoogleCallback_LoginIssuesSessionUsingInjectedSessionManagerClock(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)
	svc, err := auth.NewServiceForTest(testLogger(), config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}, q, p.URL, "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)

	txCookie, state, nonce := beginGoogle(t, handler)
	subject := uniqueSubject(t)
	code := "code-clock-seam-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+state, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	sessionCookie := extractCookie(resp, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("login did not issue a session cookie")
	}

	row, err := q.GetSessionByTokenHash(context.Background(), sessionTokenHash(sessionCookie.Value))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error = %v", err)
	}
	if !row.CreatedAt.Equal(clk.Now()) {
		t.Errorf("issued session CreatedAt = %v, want %v (the injected SessionManager's fake clock -- SetSessionManagerForTest must point svc.sessions at it too, not just svc.sessionMgr)", row.CreatedAt, clk.Now())
	}
}
