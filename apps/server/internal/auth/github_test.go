// Package auth_test's GitHub coverage (task-6-brief.md Step 1): the
// happy-path login test, and newGitHubStub, its in-process test double for
// GitHub's REST surface. Unlike Google/LinkedIn (google_test.go,
// oidctest), GitHub login is plain OAuth2 with no OIDC discovery, no
// signed ID token, and no nonce (design spec §3, AC-AUTH-003) -- so this
// stub needs no signing key or JWKS, just three plain JSON endpoints:
// POST /login/oauth/access_token (token exchange), GET /user (numeric id
// + login), and GET /user/emails (the array handleGitHubCallback scans
// for the primary+verified entry). golang.org/x/oauth2's own
// Exchange/Client code -- the same library the production code uses --
// runs its real token-exchange and bearer-auth logic against this server,
// not a hand-rolled stub of oauth2's internals.
//
// The adversarial matrix (unverified/no-verified-primary email, the
// cross-provider mix-up defense, and the static no-OIDC-import check) is a
// separate, independently authored suite per the phase's review workflow
// (task-6-brief.md Step 2) and is not duplicated here.
package auth_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ghEmail is one entry of a stubbed GET /user/emails response. Field names
// and JSON tags mirror GitHub's real API response shape (email, primary,
// verified) exactly, so github.go's own decoding is exercised faithfully
// rather than against a convenient test-only shape.
type ghEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// gitHubStubConfig is newGitHubStub's own scratch options struct.
type gitHubStubConfig struct {
	code        string
	accessToken string
	userID      int64
	userLogin   string
	userName    string
	emails      []ghEmail

	// codeChallenge is the PKCE (RFC 7636) S256 code_challenge a real
	// authorization server would have remembered from the /authorize
	// request that issued code. Empty (the default) means PKCE is not
	// enforced for this code -- /login/oauth/access_token accepts an
	// exchange with no code_verifier at all. It is set post-construction
	// via SetCodeChallenge, not a gitHubStubOption, because the real
	// value is only known after /start actually runs (Begin generates
	// a fresh PKCEVerifier per transaction) -- see
	// TestGitHubCallback_SendsPKCEVerifier_MatchesStartCodeChallenge.
	codeChallenge string

	// userAPIStatus, when non-zero, makes GET /user respond with this
	// HTTP status and an empty body instead of a normal stubGitHubUser --
	// fix round 2 item 2's DD-C10 regression coverage (a provider-side
	// GitHub REST failure, e.g. GitHub's own /user endpoint returning 500).
	userAPIStatus int

	// emailsAPIMalformedJSON, when true, makes GET /user/emails respond
	// 200 with a body that is not valid JSON at all -- fix round 2 item
	// 2's other DD-C10 regression case (a malformed/undecodable response,
	// as distinct from a bad status code).
	emailsAPIMalformedJSON bool
}

// gitHubStubOption configures newGitHubStub.
type gitHubStubOption func(*gitHubStubConfig)

// withTokenResponse registers the single (code, accessToken) pair the
// stub's POST /login/oauth/access_token endpoint accepts: exchanging any
// other code is rejected with a 400 invalid_grant response, matching a
// real authorization server's behavior for an unrecognized authorization
// code.
func withTokenResponse(code, accessToken string) gitHubStubOption {
	return func(c *gitHubStubConfig) {
		c.code = code
		c.accessToken = accessToken
	}
}

// withUser registers the stub's GET /user response: id (githubUser's
// numeric "id" claim -- github.go stringifies this into
// identities.provider_user_id) and login (GitHub's always-present
// username). The response deliberately carries no "name" field at all, so
// every test using this option exercises github.go's fallback to login
// for the created user's display name, the same way oidctest's Claims
// having no Name field forces every Google test through
// resolveGoogleUser's own email-local-part fallback (google.go).
func withUser(id int64, login string) gitHubStubOption {
	return func(c *gitHubStubConfig) {
		c.userID = id
		c.userLogin = login
	}
}

// withEmails registers the stub's GET /user/emails response.
func withEmails(emails []ghEmail) gitHubStubOption {
	return func(c *gitHubStubConfig) { c.emails = emails }
}

// withUserAPIStatus makes GET /user respond with status and an empty body
// -- fix round 2 item 2's DD-C10 regression coverage: a non-200 from
// GitHub's own /user endpoint (e.g. a transient 500) must funnel through
// redirectAuthFailed (302 ?error=auth_failed), not writeInternalError's
// 500, since this is a provider-side failure, not a local one.
func withUserAPIStatus(status int) gitHubStubOption {
	return func(c *gitHubStubConfig) { c.userAPIStatus = status }
}

// withEmailsAPIMalformedJSON makes GET /user/emails respond 200 with a
// body that fails to decode as JSON at all -- fix round 2 item 2's other
// DD-C10 regression case: a malformed response is exactly as much a
// provider-side failure as a bad status code, so it must funnel through
// the same redirectAuthFailed path, not writeInternalError.
func withEmailsAPIMalformedJSON() gitHubStubOption {
	return func(c *gitHubStubConfig) { c.emailsAPIMalformedJSON = true }
}

// gitHubStub is an in-process, GitHub-shaped HTTP server backing a single
// test's login attempt.
type gitHubStub struct {
	// URL is the server's own base URL -- pass to withGitHubEndpoint
	// (handlers_test.go) so the Service under test dials this stub
	// instead of the real https://github.com/api.github.com.
	URL string

	t *testing.T

	mu  sync.Mutex
	cfg gitHubStubConfig
}

// newGitHubStub starts an in-process GitHub-shaped test server and
// registers t.Cleanup to shut it down, so no server outlives the test
// that created it.
func newGitHubStub(t *testing.T, opts ...gitHubStubOption) *gitHubStub {
	t.Helper()

	var cfg gitHubStubConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	gh := &gitHubStub{t: t, cfg: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", gh.serveToken)
	mux.HandleFunc("/user", gh.serveUser)
	mux.HandleFunc("/user/emails", gh.serveEmails)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	gh.URL = server.URL
	return gh
}

// SetCodeChallenge registers challenge as the PKCE (RFC 7636) S256
// code_challenge the stub's /login/oauth/access_token endpoint requires a
// matching code_verifier for -- see gitHubStubConfig.codeChallenge's doc
// comment for why this is a post-construction setter rather than a
// gitHubStubOption.
func (gh *gitHubStub) SetCodeChallenge(challenge string) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	gh.cfg.codeChallenge = challenge
}

// tokenErrorResponse is the token endpoint's RFC 6749 §5.2 error body:
// {"error": "..."}.
type tokenErrorResponse struct {
	Error string `json:"error"`
}

// writeTokenError writes a token-endpoint error response as JSON, so
// golang.org/x/oauth2's own error decoding (doTokenRoundTrip's
// Content-Type switch) parses it as JSON rather than
// application/x-www-form-urlencoded -- mirroring oidctest.Provider's own
// writeTokenError (oidctest.go), whose doc comment explains why a bare
// http.Error's text/plain Content-Type would silently lose the error
// code.
func (gh *gitHubStub) writeTokenError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(tokenErrorResponse{Error: code}); err != nil {
		gh.t.Errorf("github stub: encoding token error response: %v", err)
	}
}

// tokenResponse is the token endpoint's success body: the subset of
// GitHub's real response github.go's token exchange consumes via
// golang.org/x/oauth2.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func (gh *gitHubStub) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		gh.writeTokenError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	code := r.PostFormValue("code")

	gh.mu.Lock()
	cfg := gh.cfg
	gh.mu.Unlock()

	if cfg.code == "" || code != cfg.code {
		gh.writeTokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// PKCE (RFC 7636): a code registered with a code_challenge (via
	// SetCodeChallenge) requires a matching code_verifier on this exchange
	// -- mirrors oidctest.Provider.serveToken's own PKCE check
	// (oidctest.go). Checked after the code lookup above so a PKCE-failed
	// exchange still consumes the attempt the same way a real
	// authorization server's behavior would for any failed exchange.
	if cfg.codeChallenge != "" {
		verifier := r.PostFormValue("code_verifier")
		if verifier == "" || oauth2.S256ChallengeFromVerifier(verifier) != cfg.codeChallenge {
			gh.writeTokenError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
	}

	resp := tokenResponse{
		AccessToken: cfg.accessToken,
		TokenType:   "bearer",
		Scope:       "user:email",
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil { //nolint:gosec // access_token here is this stub's own fixed placeholder, not a real credential
		gh.t.Errorf("github stub: encoding token response: %v", err)
	}
}

// requireBearerToken reports whether r carries the exact "Authorization:
// Bearer <accessToken>" header golang.org/x/oauth2.Config.Client attaches
// automatically once Exchange has returned a token -- writing 401 and
// returning false otherwise, so a /user or /user/emails call made without
// a (or with the wrong) access token is rejected the same way the real
// API would reject it, rather than silently succeeding.
func (gh *gitHubStub) requireBearerToken(w http.ResponseWriter, r *http.Request) bool {
	gh.mu.Lock()
	want := "Bearer " + gh.cfg.accessToken
	gh.mu.Unlock()

	if r.Header.Get("Authorization") != want {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

// stubGitHubUser is the GET /user response body: GitHub's real response
// has many more fields, but id/login/name are the only ones github.go
// reads.
type stubGitHubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
}

func (gh *gitHubStub) serveUser(w http.ResponseWriter, r *http.Request) {
	if !gh.requireBearerToken(w, r) {
		return
	}

	gh.mu.Lock()
	cfg := gh.cfg
	gh.mu.Unlock()

	if cfg.userAPIStatus != 0 {
		w.WriteHeader(cfg.userAPIStatus)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	body := stubGitHubUser{ID: cfg.userID, Login: cfg.userLogin, Name: cfg.userName}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		gh.t.Errorf("github stub: encoding user response: %v", err)
	}
}

func (gh *gitHubStub) serveEmails(w http.ResponseWriter, r *http.Request) {
	if !gh.requireBearerToken(w, r) {
		return
	}

	gh.mu.Lock()
	cfg := gh.cfg
	gh.mu.Unlock()

	if cfg.emailsAPIMalformedJSON {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("{not valid json")); err != nil {
			gh.t.Errorf("github stub: writing malformed emails response: %v", err)
		}
		return
	}

	emails := cfg.emails
	w.Header().Set("Content-Type", "application/json")
	if emails == nil {
		emails = []ghEmail{} // GitHub's real API returns "[]", never "null"
	}
	if err := json.NewEncoder(w).Encode(emails); err != nil {
		gh.t.Errorf("github stub: encoding emails response: %v", err)
	}
}

// uniqueGitHubUserID returns a collision-proof positive int64 for this
// test run. github.go stringifies this into
// identities.provider_user_id, so a fixed literal (unlike this file's
// deliberately fixed "octocat" login/name, which carries no uniqueness
// constraint) would silently reuse a row a previous run already created
// against the same shared TEST_DATABASE_URL (no per-test reset -- see
// google_adversarial_test.go's uniqueSubject/uniqueEmail doc comment for
// the established convention this mirrors).
func uniqueGitHubUserID(t *testing.T) int64 {
	t.Helper()
	id := rand.Int64N(1 << 62) //nolint:gosec // test-only row-collision avoidance, not a security-sensitive value
	if id == 0 {
		id = 1
	}
	return id
}

// TestGitHubCallback_NewUser_UsesVerifiedPrimaryEmail is task-6-brief.md
// Step 1's required happy-path test: a first-ever GitHub login creates a
// users row using the ONE email entry that is both primary and verified
// (never the unverified or the non-primary-but-verified entries also
// present in the stubbed /user/emails response) and an identities row
// keyed by the numeric /user id, stringified. It drives /start then
// /callback exactly as a browser would (capturing the Set-Cookie and
// state/code_challenge from /start's redirect, then presenting them back
// to /callback), proving the PKCE send (oauth2.Config.Exchange with
// oauth2.VerifierOption) the same way google_test.go's own happy-path test
// does for Google -- github.go's stub doesn't enforce PKCE itself (unlike
// oidctest, which validates code_challenge/code_verifier), but the
// exchange still exercises the real client-side send.
func TestGitHubCallback_NewUser_UsesVerifiedPrimaryEmail(t *testing.T) {
	t.Parallel()

	userID := uniqueGitHubUserID(t)
	email := uniqueEmail(t) // google_adversarial_test.go: uuid.NewString()+"@example.com", collision-proof

	gh := newGitHubStub(t,
		withTokenResponse("code-1", "access-token-1"),
		withUser(userID, "octocat"),
		withEmails([]ghEmail{
			{Email: "unverified@example.com", Primary: false, Verified: false},
			{Email: "secondary@example.com", Primary: false, Verified: true},
			{Email: email, Primary: true, Verified: true},
		}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	startResp := doGet(t, handler, auth.GitHubStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d", auth.GitHubStartPath, startResp.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("start redirect Location missing state param")
	}
	codeChallenge := loc.Query().Get("code_challenge")
	if codeChallenge == "" {
		t.Fatal("start redirect Location missing code_challenge param (PKCE)")
	}
	if method := loc.Query().Get("code_challenge_method"); method != "S256" {
		t.Errorf("code_challenge_method = %q, want %q", method, "S256")
	}
	if loc.Query().Has("nonce") {
		t.Error("start redirect Location has a nonce param, want none -- GitHub is plain OAuth2, no OIDC (AC-AUTH-003)")
	}
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("start response missing __Host-oauth-tx cookie")
	}

	cbPath := auth.GitHubCallbackPath + "?code=code-1&state=" + url.QueryEscape(state)
	cbResp := doGet(t, handler, cbPath, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != http.StatusFound {
		t.Fatalf("GET callback status = %d, want %d", cbResp.StatusCode, http.StatusFound)
	}

	sessionCookie := extractCookie(cbResp, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("callback response missing a non-empty __Host-session cookie")
	}
	if !sessionCookie.Secure || !sessionCookie.HttpOnly {
		t.Errorf("__Host-session cookie Secure=%v HttpOnly=%v, want both true", sessionCookie.Secure, sessionCookie.HttpOnly)
	}

	clearedTxCookie := extractCookie(cbResp, auth.OAuthTxCookieName)
	if clearedTxCookie == nil {
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it (ruling 1)")
	}
	if clearedTxCookie.MaxAge >= 0 {
		t.Errorf("callback __Host-oauth-tx MaxAge = %d, want negative (cleared)", clearedTxCookie.MaxAge)
	}

	ctx := context.Background()
	usr, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row using the primary+verified email "+
			"(not unverified@example.com or secondary@example.com)", email, err)
	}
	if usr.Email != email {
		t.Errorf("user.Email = %q, want %q", usr.Email, email)
	}
	if usr.Name != "octocat" {
		t.Errorf("user.Name = %q, want %q (fallback to login -- the stubbed /user response has no name)", usr.Name, "octocat")
	}

	providerUserID := strconv.FormatInt(userID, 10)
	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       "github",
		ProviderUserID: providerUserID,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(github, %q) error = %v, want a created identity row", providerUserID, err)
	}
	if identity.UserID != usr.ID {
		t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
	}
}

// TestGitHubCallback_SendsPKCEVerifier_MatchesStartCodeChallenge is fix
// round 2 item 4's PKCE proof: design spec §3 requires "authorization-code
// + PKCE S256 even as confidential client" for every provider, GitHub
// included, but TestGitHubCallback_NewUser_UsesVerifiedPrimaryEmail above
// never actually EXERCISES that -- its stub (no SetCodeChallenge call)
// accepts any exchange, code_verifier or not, so it would keep passing
// even if github.go's Exchange call silently dropped
// oauth2.VerifierOption(tx.PKCEVerifier) entirely. This test closes that
// gap: it captures the REAL code_challenge /start generated (exactly the
// way google_test.go's happy path captures Google's), registers it on the
// stub via SetCodeChallenge, and only then completes the callback --
// serveToken (github_test.go) now requires a code_verifier that
// S256-hashes to this exact code_challenge, so a callback that succeeds
// here is direct evidence the client actually sent one derived from the
// same PKCEVerifier Begin generated for this transaction, not merely that
// oauth2.Config.Exchange was called at all.
//
// Regression evidence (fix round 2 item 4's required RED transcript, run
// against a live TEST_DATABASE_URL): with github.go's Exchange call
// temporarily changed from
//
//	oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
//
// to
//
//	oauth2Cfg.Exchange(ctx, code)
//
// (dropping the PKCE verifier option), running
//
//	TEST_DATABASE_URL=postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable \
//	  go test ./internal/auth/... -run TestGitHubCallback_SendsPKCEVerifier -v -count=1
//
// produced:
//
//	=== RUN   TestGitHubCallback_SendsPKCEVerifier_MatchesStartCodeChallenge
//	    github_test.go:496: callback response missing a non-empty __Host-session cookie -- the token exchange must have failed the stub's PKCE check (no code_verifier sent, or one that doesn't match)
//	--- FAIL: TestGitHubCallback_SendsPKCEVerifier_MatchesStartCodeChallenge (0.04s)
//	FAIL
//
// (the callback still redirected 302, per DD-C3/DD-C4's generic
// rejection contract, but with no session cookie -- github.go's token
// exchange failed against the stub's now-enforced PKCE check, exactly
// the failure this test exists to catch). Restoring the VerifierOption
// call was then re-verified to pass again (see this dispatch's fix
// report for the exact commands run both ways).
func TestGitHubCallback_SendsPKCEVerifier_MatchesStartCodeChallenge(t *testing.T) {
	t.Parallel()

	userID := uniqueGitHubUserID(t)
	email := uniqueEmail(t)

	gh := newGitHubStub(t,
		withTokenResponse("code-pkce", "access-token-pkce"),
		withUser(userID, "octocat"),
		withEmails([]ghEmail{{Email: email, Primary: true, Verified: true}}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	startResp := doGet(t, handler, auth.GitHubStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d", auth.GitHubStartPath, startResp.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	state := loc.Query().Get("state")
	codeChallenge := loc.Query().Get("code_challenge")
	if codeChallenge == "" {
		t.Fatal("start redirect Location missing code_challenge param (PKCE) -- cannot exercise the PKCE proof without it")
	}
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("start response missing __Host-oauth-tx cookie")
	}

	// Only NOW does the stub require a matching code_verifier: registering
	// this before /start ran would be impossible (the real code_challenge
	// doesn't exist yet -- Begin generates a fresh PKCEVerifier per
	// transaction), and registering it not at all (as the happy-path test
	// above does) would never force github.go's Exchange call to actually
	// send one.
	gh.SetCodeChallenge(codeChallenge)

	cbPath := auth.GitHubCallbackPath + "?code=code-pkce&state=" + url.QueryEscape(state)
	cbResp := doGet(t, handler, cbPath, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != http.StatusFound {
		t.Fatalf("GET callback status = %d, want %d", cbResp.StatusCode, http.StatusFound)
	}

	sessionCookie := extractCookie(cbResp, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("callback response missing a non-empty __Host-session cookie -- the token exchange must have failed the stub's PKCE check (no code_verifier sent, or one that doesn't match)")
	}

	usr, err := q.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row (the PKCE-checked exchange must still complete the login)", email, err)
	}
	if usr.Email != email {
		t.Errorf("user.Email = %q, want %q", usr.Email, email)
	}
}

// ==== fix round 2 item 2: DD-C10 (provider-side GitHub REST failures) ====

// beginGitHubFlow drives GET auth.GitHubStartPath (via the shared doGet)
// and returns the __Host-oauth-tx cookie it set and the state query
// param -- no nonce (GitHub has no ID token; see transaction.go's Begin,
// which leaves Transaction.Nonce empty for ProviderGitHub). Mirrors
// google_adversarial_test.go's beginGoogle for the Google flow; factored
// out here because the two DD-C10 tests below both need it and
// duplicating it a third time (on top of the two happy-path tests above,
// which predate this helper and are left as-is) was no longer worth it.
func beginGitHubFlow(t *testing.T, handler http.Handler) (txCookie *http.Cookie, state string) {
	t.Helper()

	start := doGet(t, handler, auth.GitHubStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
	if start.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d (a redirect to the provider)", auth.GitHubStartPath, start.StatusCode, http.StatusFound)
	}

	txCookie = extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatalf("GET %s did not set the %s cookie", auth.GitHubStartPath, auth.OAuthTxCookieName)
	}

	loc, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	state = loc.Query().Get("state")
	if state == "" {
		t.Fatal("start redirect Location missing state param")
	}

	return txCookie, state
}

// doGitHubCallback drives GET auth.GitHubCallbackPath?code=...&state=...
// (via the shared doGet).
func doGitHubCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.GitHubCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

// assertGitHubRejectedAuthFailed asserts the DD-C3/DD-C4 shape every
// /callback rejection must have (mirrors google_adversarial_test.go's
// assertRejected, narrowed to the one error code both DD-C10 tests below
// produce): a 302 redirect to the login page with ?error=auth_failed
// (never a raw JSON error or a 200), the __Host-oauth-tx cookie cleared,
// and no __Host-session cookie -- proving a provider-side GitHub REST
// failure is treated as an ordinary rejected login, never an internal
// server error the visitor can do nothing about.
func assertGitHubRejectedAuthFailed(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (DD-C10: a provider-side GitHub REST failure is a rejection, not a 500)", resp.StatusCode, http.StatusFound)
	}
	if got := mustQueryParam(t, resp.Header.Get("Location"), "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie on a rejected callback (value=%q), want none", auth.SessionCookieName, sc.Value)
	}
	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil {
		t.Fatalf("response did not clear the %s cookie on a rejected callback (no Set-Cookie for it at all)", auth.OAuthTxCookieName)
	}
	if tx.MaxAge >= 0 {
		t.Errorf("%s cookie MaxAge = %d on a rejected callback, want negative (cleared)", auth.OAuthTxCookieName, tx.MaxAge)
	}
}

// TestGitHubCallback_UserAPINon200_RedirectsAuthFailed is fix round 2
// item 2's first required DD-C10 case: GitHub's own GET /user endpoint
// returning a non-200 status (a transient 500, here) is a provider-side
// failure -- GitHub being briefly unavailable or erroring is an ordinary,
// retryable condition, not a defect in this server -- so it must funnel
// through redirectAuthFailed (302 ?error=auth_failed), never
// writeInternalError's 500.
func TestGitHubCallback_UserAPINon200_RedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t,
		withTokenResponse("code-user-500", "access-token-user-500"),
		withUserAPIStatus(http.StatusInternalServerError),
	)
	handler, _ := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-user-500", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	assertGitHubRejectedAuthFailed(t, resp)
}

// TestGitHubCallback_EmailsAPIMalformedJSON_RedirectsAuthFailed is fix
// round 2 item 2's second required DD-C10 case: GitHub's GET
// /user/emails responding 200 with a body that isn't valid JSON at all
// (a malformed/undecodable response, distinct from a bad status code) is
// equally a provider-side failure, and must funnel through the same
// redirectAuthFailed path.
func TestGitHubCallback_EmailsAPIMalformedJSON_RedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	userID := uniqueGitHubUserID(t)

	gh := newGitHubStub(t,
		withTokenResponse("code-emails-malformed", "access-token-emails-malformed"),
		withUser(userID, "octocat"),
		withEmailsAPIMalformedJSON(),
	)
	handler, _ := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-emails-malformed", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	assertGitHubRejectedAuthFailed(t, resp)
}

// ==== fix round 2 item 5: log attribution ====

// TestGitHubCallback_RejectionLogsProviderAttribute mirrors
// TestGoogleCallback_RejectionLogsProviderAttribute (handlers_test.go):
// proves logRejection's output identifies GitHub, specifically, as the
// provider whose callback was rejected -- the shared funnel
// (redirectWithError/redirectAuthFailed) is reused by every provider
// Service registers, so its log message is deliberately provider-neutral
// ("auth: callback rejected"), and the "provider" attribute is the only
// thing that still lets an operator filter or correlate by provider in a
// multi-provider log stream. This is also the only test in this package
// that can catch a specific, easy-to-make mistake: passing the WRONG
// Provider constant (e.g. ProviderGoogle) into one of GitHub's own
// redirectWithError/redirectAuthFailed/writeInternalError call sites --
// every other GitHub test in this file asserts on status code and cookie
// shape, which are IDENTICAL regardless of which Provider constant was
// logged, so only a log assertion like this one discriminates a swapped
// constant (the same reasoning LinkedIn's own equivalent test documents).
func TestGitHubCallback_RejectionLogsProviderAttribute(t *testing.T) {
	t.Parallel()

	logger, logBuf := newCapturingLogger()
	// This request never reaches a real GitHub call (it's rejected at the
	// missing-tx-cookie check, the very first line of handleGitHubCallback),
	// so any non-empty override satisfies newTestService's guard --
	// auth.UnroutableTestSentinel documents that explicitly rather than a
	// bare, unexplained literal.
	handler, _ := newTestService(t, withGitHubEndpoint(auth.UnroutableTestSentinel), withLogger(logger))

	resp := doGet(t, handler, auth.GitHubCallbackPath+"?code=whatever&state=whatever") //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `"provider":"github"`) {
		t.Errorf("log record = %q, want a provider attribute identifying GitHub as the callback that was rejected", logged)
	}
}
