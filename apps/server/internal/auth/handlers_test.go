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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
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
// fields; each provider's own endpoint-override seam is provided by
// auth.NewServiceForTest (Google, GitHub, LinkedIn all three -- see that
// function's own doc comment for its sentinel-defaulting guarantee).
type testServiceConfig struct {
	googleIssuer   string
	githubEndpoint string
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

// withGitHubEndpoint arranges for the returned Service to dial endpoint
// (e.g. a newGitHubStub's httptest.Server URL, github_test.go) instead of
// the real "https://github.com" / "https://api.github.com" for every
// GitHub OAuth2/API call. Like withLinkedInIssuer below, newTestService
// does NOT t.Fatal when this is omitted on its own (the belt-and-
// suspenders guard only fires when EVERY override is empty) -- an omitted
// override instead silently defaults to auth.NewServiceForTest's own
// unroutable sentinel (see that function's doc comment), so a test that
// forgets this but never drives a GitHub route is unaffected, and one
// that does but forgot the override fails fast and OFFLINE, never against
// the real network.
func withGitHubEndpoint(endpoint string) testServiceOption {
	return func(c *testServiceConfig) { c.githubEndpoint = endpoint }
}

// withLinkedInIssuer is withGoogleIssuer's LinkedIn counterpart: arranges
// for the returned Service to run LinkedIn OIDC discovery against issuer
// instead of the real "https://www.linkedin.com/oauth". Unlike
// withGoogleIssuer, newTestService does NOT t.Fatal when this is omitted
// on its own (see newTestService's own doc comment for why) -- instead,
// an omitted override silently defaults to
// auth.NewServiceForTest's own unroutable sentinel (fix round 1 item 1,
// generalized to every provider by fix round 2 item 3: a forgotten
// override must still fail fast and OFFLINE, never silently reach the
// real network -- see NewServiceForTest's doc comment for why this
// defaulting now lives there instead of in a local resolvedXxxIssuer
// helper).
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
// At least one provider endpoint override (withGoogleIssuer or
// withGitHubEndpoint) is REQUIRED, not optional: fix-round Critical
// finding was exactly a test that called this with no override and so
// performed live OIDC discovery against the real
// https://accounts.google.com on every hermetic-looking test run. Forcing
// at least one override here, once, makes that mistake impossible to
// repeat for either provider -- a test that supplies only
// withGitHubEndpoint never dials Google (it never calls a Google route at
// all), and vice versa, so this stays a genuine per-provider seam rather
// than a blanket requirement to supply both. This is now belt-and-
// suspenders, not the only guard: auth.NewServiceForTest itself defaults
// every EMPTY override (including one this t.Fatal doesn't catch, e.g. a
// GitHub-focused test that never supplies withLinkedInIssuer) to its own
// unroutable sentinel, so even a caller that bypasses this guard entirely
// -- or a future provider added here without updating it -- still cannot
// reach a real provider endpoint by accident (fix round 2 item 3).
//
// A LinkedIn issuer override (withLinkedInIssuer) is NOT forced with a
// t.Fatal the same way -- unlike Google, no test in this package drives a
// LinkedIn route by default, so an omitted override being INERT would be
// fine on its own. Nor is a GitHub endpoint override forced on its own,
// for the same reason. Both rely entirely on auth.NewServiceForTest's own
// sentinel defaulting described above.
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

	// sc.githubEndpoint/sc.linkedinIssuer are passed straight through, even
	// when empty: auth.NewServiceForTest itself substitutes its own
	// unroutable sentinel for any empty override (fix round 2 item 3), so
	// this call site no longer pre-resolves one -- unlike the pre-fix
	// resolvedLinkedInIssuer helper this replaced.
	svc, err := auth.NewServiceForTest(logger, cfg, q, sc.googleIssuer, sc.githubEndpoint, sc.linkedinIssuer)
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

// requestCookie builds an *http.Cookie carrying only Name/Value, for
// attaching to an outbound test *http.Request -- via req.AddCookie, or
// one of doGet/doJSON/doCallback's own cookies... parameters, which every
// call site across this package's test files ultimately feeds into
// req.AddCookie -- never a real response Set-Cookie. Secure/HttpOnly/
// SameSite are response-cookie attributes: they tell a BROWSER how to
// treat a cookie a SERVER sets via Set-Cookie, and have no wire
// representation at all on a request's own Cookie header (RFC 6265
// §5.4) -- there is nothing for this helper to set, and setting them here
// would be simulating a shape a real request cookie can never have.
// Production Set-Cookie construction lives exclusively in cookie.go's
// oauthTxCookie and session_cookie.go's sessionCookie, both already
// covered by .semgrep.yml's go-cookie-missing-security-flags rule (which,
// as of fix round 1, excludes _test.go entirely -- see that rule's own
// updated paths.exclude comment for why request-side construction there
// can never be the regression it exists to catch).
//
// Centralized here (fix round 1, Task 10 review finding, semgrep) rather
// than one bare &http.Cookie{Name: ..., Value: ...} literal per call
// site specifically so semgrep's two remaining upstream registry rules
// (p/gosec/p/golang's cookie-missing-httponly and cookie-missing-secure --
// not this project's own rule, which the .semgrep.yml exclusion above
// already silences for every _test.go file) need exactly ONE nosemgrep
// pair, here, rather than one per call site.
func requestCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value} // nosemgrep: go.lang.security.audit.net.cookie-missing-httponly.cookie-missing-httponly,go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- request-side cookie (see this function's own doc comment): Secure/HttpOnly/SameSite are response-cookie attributes with no meaning on a Cookie header this helper builds for req.AddCookie.
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

// pgErrorSessionIssuer is failingSessionIssuer's sibling (fix round 1, I7):
// Issue always fails with a *pgconn.PgError shaped exactly like a real
// users_email_key unique-violation (the same SQLSTATE/message/detail/
// constraint shape a genuine concurrent-registration race would produce),
// deterministically forcing writeInternalError/logInternalError's
// SQLSTATE-extraction-and-redaction path (fix-round Important 2) without
// needing an actual database race. This restores the coverage
// TestGoogleCallback_ResolveUserConflict_LogsSQLSTATE's retirement note
// (below) flagged as lost: that test's own repro (a second registration
// racing a real UNIQUE constraint) no longer reaches the database at all
// now that resolveLoginIdentity checks GetUserByEmail first, so this seam
// -- already established by failingSessionIssuer for an unrelated
// purpose -- is reused instead of trying to force a genuine, inherently
// non-deterministic race.
type pgErrorSessionIssuer struct{}

// pgUniqueViolationMessage/pgUniqueViolationDetail/pgUniqueViolationConstraint
// are the exact fields a real users_email_key violation carries -- Detail
// deliberately embeds a bound email value (Postgres's own behavior,
// "Key (email)=(...) already exists"), which is precisely why
// logInternalError must never log this struct's Message/Detail/
// ConstraintName fields, only its Code.
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

// ---- Task 10 Step 1: resolveLoginIdentity's shared new-account /
// existing-identity happy path, exercised once per provider ------------

// TestResolveLoginIdentity_NewThenExisting_AcrossProviders is
// task-10-brief.md Step 1's required happy-path test: first-ever login
// creates a users row + an identities row (NewUser); a second login with
// the SAME (provider, providerUserID) reuses the existing user
// (ExistingIdentity) and creates NO second identities row -- asserted on
// the actual row count, not merely the absence of an error, since
// identities_provider_subject_key's own UNIQUE constraint would catch a
// naive re-insert bug just as well and this test must not be fooled by
// that into passing for the wrong reason.
//
// Calls resolveLoginIdentity directly (auth.ResolveLoginIdentityForTest)
// rather than driving three separate providers' worth of OIDC/OAuth2
// mechanics through the full HTTP surface -- Google's own /start-/callback
// round trip already covers the identical scenario
// (TestGoogleCallback_NewUser_CreatesUserAndSession,
// TestGoogleCallback_ExistingIdentity_ReusesUser_NoDuplicateRow,
// google_test.go, Task 4), and Task 5/6 already proved LinkedIn/GitHub's
// own provider mechanics -- so this test's whole job is confirming the
// ONE shared function every provider's /callback now calls behaves
// identically regardless of which Provider constant it's given, per
// task-10-brief.md's own framing ("exercised once per provider to
// confirm wiring, not re-proving provider mechanics").
//
// This is a genuine RED-before-GREEN test for Task 10 specifically (not
// merely a refactor-preserving regression): auth.ResolveLoginIdentityForTest
// and the loginResult/loginResultKind shape it exposes did not exist
// before this task, so this test fails to even COMPILE against Task
// 4/5/6's per-provider resolveXUser stubs -- see task-10-report.md for
// the literal `go test` transcript.
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

// TestDefaultTestOverride_DefaultsToUnroutableSentinelWhenOmitted is fix
// round 1 item 1's core regression test, updated for fix round 2 item 3's
// relocation: an empty override string (the shape ANY of
// NewServiceForTest's three provider-override parameters gets when a
// caller forgets to supply one -- not just LinkedIn's, generalized from
// the original LinkedIn-only handlers_test.go-local helper) must resolve
// to auth.UnroutableTestSentinel, never the empty string (which
// NewServiceForTest would otherwise store verbatim, leaving the real
// provider endpoint in place).
func TestDefaultTestOverride_DefaultsToUnroutableSentinelWhenOmitted(t *testing.T) {
	t.Parallel()

	if got := auth.DefaultTestOverrideForTest(""); got != auth.UnroutableTestSentinel {
		t.Errorf("DefaultTestOverrideForTest(\"\") = %q, want the unroutable sentinel %q", got, auth.UnroutableTestSentinel)
	}
}

// TestDefaultTestOverride_UsesExplicitOverrideWhenSupplied is the above
// test's sibling: an explicit override value must still win over the
// sentinel default -- otherwise google_test.go/linkedin_test.go's own
// tests (which all supply a real oidctest.Provider URL) would silently
// stop reaching it.
func TestDefaultTestOverride_UsesExplicitOverrideWhenSupplied(t *testing.T) {
	t.Parallel()

	if got := auth.DefaultTestOverrideForTest("http://example.invalid"); got != "http://example.invalid" {
		t.Errorf("DefaultTestOverrideForTest(explicit override) = %q, want %q", got, "http://example.invalid")
	}
}

// TestService_LinkedInRoute_OmittedIssuerOverride_FailsFastOffline is the
// end-to-end companion to TestDefaultTestOverride_..._Omitted above:
// proves the sentinel default holds in practice when a LinkedIn route is
// actually driven, not just in isolation. This is itself a SAFE,
// local-only assertion, never a real network dependency:
// auth.UnroutableTestSentinel (127.0.0.1:1) is loopback with no listener,
// so the discovery call fails immediately with connection-refused, which
// handleLinkedInStart surfaces as the standard opaque 500 via
// writeInternalError -- this test is deliberately only ever run AFTER the
// sentinel-default fix exists (see this dispatch's fix report for why it
// was not exercised against the pre-fix code: doing so would have meant a
// real external network attempt to https://www.linkedin.com from a
// test).
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

	fakeTxCookie := requestCookie(auth.OAuthTxCookieName, "never-issued-handle-shaped-value")
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

// TestGoogleCallback_ResolveUserConflict_LogsSQLSTATE (removed 2026-08-02,
// Task 10) used to force a REAL Postgres constraint violation through
// resolveGoogleUser's CreateUser call: a first login seeded a users row
// for email, then a SECOND login with a different subject but the SAME
// email hit users_email_key's UNIQUE constraint -- a completely ordinary
// way to reach it under Task 4's documented stub, which created a new
// user unconditionally on any unknown identity with no pre-check against
// an existing email.
//
// That repro no longer reaches the database at all: resolveLoginIdentity
// (handlers.go, Task 10, AC-AUTH-001) checks GetUserByEmail BEFORE ever
// calling CreateUser, so this exact scenario -- a second, different
// provider subject presenting an email that already belongs to another
// account -- is now the EmailCollision outcome: a 302
// ?error=email_already_registered redirect with zero new rows, never a
// 500. That is the intended fix, not a regression (Step 2's independent
// rejection-matrix suite asserts the "zero new rows" outcome directly for
// this and five sibling cases), but it also means this test's specific
// mechanism for reaching writeInternalError/logInternalError's SQLSTATE-
// extraction-and-redaction path (pgconn.PgError.Code logged, raw
// message/detail/constraint-name text never logged) no longer applies: a
// UNIQUE-constraint violation is now reachable only via a genuine
// concurrent race between resolveLoginIdentity's own check and its
// later write (two simultaneous first-ever registrations for the same
// brand-new email), which cannot be forced deterministically without
// either an artificial fault-injection seam this package doesn't have or
// a real, inherently non-deterministic goroutine race -- CLAUDE.md's
// "tests must be deterministic ... a flaky test is broken, not a
// candidate for retries" rules out the latter. Removed rather than left
// disabled or weakened to "eventually" pass; flagged in task-10-report.md
// for the integration owner to decide whether dedicated SQLSTATE-
// redaction coverage belongs at the internal/store layer instead, where
// a genuine unique-violation is trivial to force deterministically
// (two direct CreateUser calls with the same email, no HTTP layer or
// race required).
//
// Superseded (fix round 1, I7) by
// TestGoogleCallback_SessionIssuanceUniqueViolation_LogsSQLSTATEOnly
// below: SetSessionIssuerForTest/pgErrorSessionIssuer (an already-
// established seam, extended here rather than reinvented) forces the
// SAME writeInternalError/logInternalError path with a synthetic
// *pgconn.PgError shaped exactly like a real users_email_key violation,
// deterministically, downstream of a real successful resolveLoginIdentity
// call rather than trying to force the database's own constraint under a
// race.

// TestGoogleCallback_SessionIssuanceUniqueViolation_LogsSQLSTATEOnly is
// fix round 1's I7: restores this package's ONLY coverage of
// logInternalError's SQLSTATE-extraction-and-redaction behavior
// (pgconn.PgError.Code logged; raw Message/Detail/ConstraintName text,
// which for a real unique-violation embeds a bound value -- see
// pgUniqueViolationDetail's own literal email -- never logged), lost when
// TestGoogleCallback_ResolveUserConflict_LogsSQLSTATE was retired (see
// that test's own doc comment above for why its specific repro no longer
// reaches the database). Rather than forcing a genuine, inherently
// non-deterministic concurrent race, this drives a real, successful
// Google login all the way through resolveLoginIdentity's NewUser branch
// and fails ONLY at the very next step -- session issuance -- via
// pgErrorSessionIssuer, deterministically reaching the exact same
// writeInternalError/logInternalError code path a real CreateUser
// constraint violation would have, with a synthetic error shaped
// identically to a genuine one.
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

// ---- cheap win: SetSessionManagerForTest must also swap sessions -------

// TestGoogleCallback_LoginIssuesSessionUsingInjectedSessionManagerClock is
// regression coverage for a gap in the test seam itself (export_test.go's
// SetSessionManagerForTest): it used to set svc.sessionMgr only, leaving
// svc.sessions -- the seam a LOGIN callback's sessions.Issue call actually
// goes through (handleGoogleCallback/handleGitHubCallback/
// handleLinkedInCallback) -- still pointed at the ORIGINAL, real-wall-clock
// SessionManager NewService built internally. A test that injected a fake
// clock to drive session behavior deterministically therefore still got a
// freshly LOGGED-IN session timestamped off the real wall clock, silently
// defeating the very fake clock it just injected for exactly the
// timestamps a login issues (CreatedAt, AbsoluteExpiresAt,
// ReauthenticatedAt) -- session AUTHENTICATION (RequireSession, revoke/
// list/reauth-check) was never affected, only issuance. Proven here by
// setting the fake clock to testutil.Epoch (2024-01-01, nowhere near this
// test's real wall-clock run time) and asserting the freshly-issued
// session's own CreatedAt round-trips as exactly that instant, read back
// from the database -- not merely close to the real "now".
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
