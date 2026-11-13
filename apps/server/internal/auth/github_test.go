package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

type ghEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type gitHubStubConfig struct {
	code        string
	accessToken string
	userID      int64
	userLogin   string
	userName    string
	emails      []ghEmail

	codeChallenge string

	userAPIStatus int

	emailsAPIMalformedJSON bool

	userAPIOversizedBody bool
}

type gitHubStubOption func(*gitHubStubConfig)

func withTokenResponse(code, accessToken string) gitHubStubOption {
	return func(c *gitHubStubConfig) {
		c.code = code
		c.accessToken = accessToken
	}
}

func withUser(id int64, login string) gitHubStubOption {
	return func(c *gitHubStubConfig) {
		c.userID = id
		c.userLogin = login
	}
}

func withEmails(emails []ghEmail) gitHubStubOption {
	return func(c *gitHubStubConfig) { c.emails = emails }
}

func withUserAPIStatus(status int) gitHubStubOption {
	return func(c *gitHubStubConfig) { c.userAPIStatus = status }
}

func withEmailsAPIMalformedJSON() gitHubStubOption {
	return func(c *gitHubStubConfig) { c.emailsAPIMalformedJSON = true }
}

func withUserAPIOversizedBody() gitHubStubOption {
	return func(c *gitHubStubConfig) { c.userAPIOversizedBody = true }
}

type gitHubStub struct {
	URL string

	t *testing.T

	mu  sync.Mutex
	cfg gitHubStubConfig

	emailsRequests int
}

func (gh *gitHubStub) emailsRequestCount() int {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	return gh.emailsRequests
}

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

func (gh *gitHubStub) SetCodeChallenge(challenge string) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	gh.cfg.codeChallenge = challenge
}

type tokenErrorResponse struct {
	Error string `json:"error"`
}

func (gh *gitHubStub) writeTokenError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(tokenErrorResponse{Error: code}); err != nil {
		gh.t.Errorf("github stub: encoding token error response: %v", err)
	}
}

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

	if cfg.userAPIOversizedBody {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"id":` + strconv.FormatInt(cfg.userID, 10) + `,"login":"` + cfg.userLogin + `","padding":"`)); err != nil {
			gh.t.Errorf("github stub: writing oversized user response prefix: %v", err)
			return
		}
		padding := strings.Repeat("x", auth.MaxProviderResponseBytesForTest+1)
		if _, err := io.WriteString(w, padding); err != nil {
			gh.t.Errorf("github stub: writing oversized user response padding: %v", err)
			return
		}
		if _, err := w.Write([]byte(`"}`)); err != nil {
			gh.t.Errorf("github stub: writing oversized user response suffix: %v", err)
		}
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
	gh.emailsRequests++
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

func uniqueGitHubUserID(t *testing.T) int64 {
	t.Helper()
	id := rand.Int64N(1 << 62) //nolint:gosec // test-only row-collision avoidance, not a security-sensitive value
	if id == 0 {
		id = 1
	}
	return id
}

func TestGitHubCallback_NewUser_UsesVerifiedPrimaryEmail(t *testing.T) {
	t.Parallel()

	userID := uniqueGitHubUserID(t)
	email := uniqueEmail(t)

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
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it")
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

func doGitHubCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.GitHubCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

func assertGitHubRejectedAuthFailed(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (a provider-side GitHub REST failure is a rejection, not a 500)", resp.StatusCode, http.StatusFound)
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

func TestGitHubCallback_UserAPIOversizedBody_RedirectsAuthFailed(t *testing.T) {
	t.Parallel()

	userID := uniqueGitHubUserID(t)

	gh := newGitHubStub(t,
		withTokenResponse("code-user-oversized", "access-token-user-oversized"),
		withUser(userID, "octocat"),
		withUserAPIOversizedBody(),
	)
	handler, _ := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-user-oversized", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	assertGitHubRejectedAuthFailed(t, resp)
}

func TestGitHubCallback_RejectionLogsProviderAttribute(t *testing.T) {
	t.Parallel()

	logger, logBuf := newCapturingLogger()
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

func TestGitHubCallback_EnrolledLogin_PendingNotSession(t *testing.T) {
	t.Parallel()

	githubID := uniqueGitHubUserID(t)
	gh := newGitHubStub(t, withTokenResponse("code-enrolled", "access-token-enrolled"), withUser(githubID, "octocat-enrolled"))
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))
	userID := createTestUser(t, q)
	if _, err := q.CreateIdentity(t.Context(), store.CreateIdentityParams{
		UserID: userID, Provider: string(auth.ProviderGitHub), ProviderUserID: strconv.FormatInt(githubID, 10),
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}
	enrollForTest(t, q, userID)

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-enrolled", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.
	pending := pendingForCookie(t, q, assertPendingRedirect(t, resp))
	if pending.UserID != userID || pending.Purpose != auth.PendingAuthenticationPurposeLogin {
		t.Errorf("pending row = %+v, want a login row for the account", pending)
	}
	if n := unrevokedSessionCount(t, userID); n != 0 {
		t.Errorf("enrolled GitHub login created %d sessions, want 0", n)
	}
	if n := gh.emailsRequestCount(); n != 0 {
		t.Errorf("returning enrolled identity fetched /user/emails %d times, want 0", n)
	}
}
