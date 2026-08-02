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
	"sync"
	"testing"

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
	emails := gh.cfg.emails
	gh.mu.Unlock()

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
