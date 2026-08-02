package auth

// github.go implements "Sign in with GitHub" (design spec §3's OAuth
// table; AC-AUTH-003): plain OAuth2, deliberately NOT OIDC. GitHub has no
// ID token, no issuer/audience/signature to verify, and no nonce --
// transaction.go's Begin already leaves Transaction.Nonce empty for
// ProviderGitHub. This file must never import coreos/go-oidc (nor
// anything JWT-shaped): that absence is AC-AUTH-003's enforceable
// invariant, guarded by TestGitHubCallback_NoOIDCImportInPackage
// (task-6-brief.md Step 2). GitHub's own defense against a cross-provider
// mix-up is instead the distinct /api/v1/auth/github/callback endpoint
// plus TransactionStore.Consume's provider check (transaction.go) -- see
// that function's own doc comment for the RFC 9700 §4.4 reasoning.
//
// handleGitHubStart/handleGitHubCallback otherwise follow the exact same
// routing/funnel pattern handlers.go's Google handlers established:
// Begin/Consume for the transaction, the shared __Host-oauth-tx cookie
// helpers (cookie.go), and the shared redirectWithError/redirectAuthFailed/
// writeInternalError funnel (handlers.go) for every rejection and
// internal failure -- so DD-C3's closed ?error= vocabulary (including
// cancelledErrorCode for a provider-signaled access_denied) and DD-C4's
// "always clear the tx cookie, always 302" contract apply identically to
// GitHub, even though the underlying verification is entirely different.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// GitHubStartPath and GitHubCallbackPath are the literal routes Service
// registers for "Sign in with GitHub" (design spec §3's OAuth table).
// Exported for the same reason GoogleStartPath/GoogleCallbackPath
// (handlers.go) are: callers (tests, and later phases wiring the same
// paths into docs/api/openapi.yaml) never need to hand-copy the literal
// strings.
const (
	GitHubStartPath    = "/api/v1/auth/github/start"
	GitHubCallbackPath = "/api/v1/auth/github/callback"
)

// githubAuthorizeURL, githubTokenURL, and githubAPIBaseURL are GitHub's
// real endpoints. Production dials these directly; tests override all
// three at once via githubEndpointOverride (see githubOAuth2Config and
// githubAPIBaseURLFor below) -- see NewServiceForTest's doc comment
// (export_test.go).
const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubAPIBaseURL   = "https://api.github.com"
)

// githubScopes requests "user:email": without it, an account with
// "Keep my email addresses private" enabled (GitHub's own setting) omits
// private addresses from GET /user/emails entirely, which would make a
// login for such an account always fail the verified-primary-email
// requirement even though the account genuinely has one.
var githubScopes = []string{"user:email"}

// githubProviderConfig holds GitHub's OAuth2 client credentials --
// distinct type (rather than fields directly on Service), mirroring
// googleProviderConfig's own reasoning (google.go), even though GitHub
// needs no lazily-discovered provider to protect with a mutex: keeping
// the same shape across providers is itself the point, not an
// accident.
type githubProviderConfig struct {
	clientID     string
	clientSecret string
}

// githubOAuth2Config builds the oauth2.Config for a single request, from
// this Service's GitHub credentials, redirect URL, and (in production)
// GitHub's real authorize/token endpoints -- or, when
// githubEndpointOverride is set (tests only), that override's endpoints
// instead.
func (s *Service) githubOAuth2Config(redirectURL string) oauth2.Config {
	authorizeURL, tokenURL := githubAuthorizeURL, githubTokenURL
	if s.githubEndpointOverride != "" {
		authorizeURL = s.githubEndpointOverride + "/login/oauth/authorize"
		tokenURL = s.githubEndpointOverride + "/login/oauth/access_token"
	}
	return oauth2.Config{
		ClientID:     s.github.clientID,
		ClientSecret: s.github.clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authorizeURL,
			TokenURL: tokenURL,
		},
		RedirectURL: redirectURL,
		Scopes:      githubScopes,
	}
}

// githubAPIBaseURLFor returns the GitHub REST API base URL to call:
// githubAPIBaseURL ("https://api.github.com") in production, or
// githubEndpointOverride (tests only) when set.
func (s *Service) githubAPIBaseURLFor() string {
	if s.githubEndpointOverride != "" {
		return s.githubEndpointOverride
	}
	return githubAPIBaseURL
}

// githubRedirectURL is the absolute callback URL registered with GitHub
// and sent as this flow's redirect_uri -- must be byte-identical between
// the /start authorize request and the /callback token exchange, per
// OAuth2's redirect_uri-must-match requirement (mirrors
// googleRedirectURL, google.go).
func (s *Service) githubRedirectURL() string {
	return s.publicOrigin + GitHubCallbackPath
}

// githubUser is the subset of GET /user's response this package reads:
// ID becomes identities.provider_user_id (stringified -- the schema
// column is text, matching every provider); Login is GitHub's
// always-present username; Name is the account's optional display name.
type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// githubEmail is one entry of GET /user/emails's response.
type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// githubAPIGet issues an authenticated GET to path (e.g. "/user") against
// this Service's configured GitHub API base URL, using client (an
// *http.Client from oauth2.Config.Client, which attaches the exchanged
// access token's Authorization header automatically), and decodes a JSON
// response body into out.
//
// The response body is wrapped in io.LimitReader(maxProviderResponseBytes)
// before decoding (security-relevant cheap-win fix): json.Decoder.Decode
// otherwise reads the ENTIRE body into memory with no cap at all, however
// large GitHub's (or a misconfigured githubEndpointOverride's) response
// happens to be. A body that hits the cap fails json.Decode with a
// truncation/syntax error -- ordinary decode-failure handling, no special
// case needed -- rather than the read itself ever completing unbounded.
func (s *Service) githubAPIGet(ctx context.Context, client *http.Client, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.githubAPIBaseURLFor()+path, nil)
	if err != nil {
		return fmt.Errorf("auth: build github api request %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: github api call %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only GET response; nothing meaningful to do with a close error here

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: github api call %s: unexpected status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProviderResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("auth: github api call %s: decode response: %w", path, err)
	}
	return nil
}

// primaryVerifiedGitHubEmail returns the entry of emails with Primary &&
// Verified both true (design spec §3's GitHub rule: "email from
// /user/emails -- verified primary only"), or ("", false) when no such
// entry exists.
func primaryVerifiedGitHubEmail(emails []githubEmail) (email string, ok bool) {
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true
		}
	}
	return "", false
}

// githubDisplayName returns user's display name for a newly created
// users row: user.Name when GitHub supplies one, else user.Login (always
// present on a genuine GitHub account, unlike Name, which an account may
// leave blank) -- never empty, since users.name is NOT NULL.
func githubDisplayName(user githubUser) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Login
}

// handleGitHubStart begins a GitHub login transaction and redirects the
// browser to GitHub's own authorize endpoint, with PKCE (S256) bound to
// the transaction -- no oidc.Nonce option (unlike handleGoogleStart):
// GitHub issues no ID token to bind one to.
func (s *Service) handleGitHubStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	purpose, linkingUserID, ok := s.startPurposeAndLinkingUser(w, r)
	if !ok {
		return
	}

	redirectURI := s.githubRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderGitHub, purpose, linkingUserID, redirectURI)
	if err != nil {
		s.writeInternalError(w, r, ProviderGitHub, "begin_transaction", err)
		return
	}

	oauth2Cfg := s.githubOAuth2Config(redirectURI)
	authURL := oauth2Cfg.AuthCodeURL(tx.State, oauth2.S256ChallengeOption(tx.PKCEVerifier))

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleGitHubCallback completes a GitHub login: consumes the OAuth
// transaction, exchanges the authorization code (with PKCE, plain
// OAuth2 -- no ID token, no signature/issuer/audience/nonce
// verification), fetches the authenticated user's profile and email
// list, resolves or creates the local user, and issues a session.
//
// DD-C10 (owner ruling, fix round 2 item 2): a failure fetching /user or
// /user/emails -- a non-200 status, a network error reaching
// api.github.com, or a malformed/undecodable response body -- is a
// PROVIDER-side failure, not a local one, and funnels through
// redirectAuthFailed (302 ?error=auth_failed) exactly like a failed
// token exchange already does, NOT writeInternalError's 500.
// writeInternalError is reserved for genuinely local failures this
// server's own database/session machinery can produce
// (begin_transaction, consume_transaction's non-ErrTransactionInvalid
// path, resolve_user, issue_session) -- GitHub being briefly unreachable
// or misbehaving is an ordinary, expected external-dependency failure a
// visitor can retry, not an internal server defect worth a 500 and an
// error-level log line. See TestGitHubCallback_UserAPINon200_RedirectsAuthFailed
// and TestGitHubCallback_EmailsAPIMalformedJSON_RedirectsAuthFailed
// (github_test.go) for the regression coverage.
//
// ClearOAuthTxCookie is called on every exit path, exactly like
// handleGoogleCallback (see that function's doc comment for why it is
// deliberately not a deferred call).
func (s *Service) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	// withProviderHTTPClient (provider_http.go): every outbound call below
	// (token exchange, /user, /user/emails) shares one bounded client
	// (timeout + this file's own maxProviderResponseBytes cap on the
	// bodies githubAPIGet decodes).
	ctx := withProviderHTTPClient(r.Context())

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, PurposeLogin, "missing or malformed __Host-oauth-tx cookie")
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGitHub)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderGitHub, PurposeLogin, "oauth transaction invalid (unknown, expired, replayed, or wrong provider)")
			return
		}
		s.writeInternalError(w, r, ProviderGitHub, "consume_transaction", err)
		return
	}

	// The OAuth `state` parameter (RFC 6749 §10.12) -- identical
	// reasoning to handleGoogleCallback's own check.
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, "state parameter mismatch")
		return
	}

	// Ruling b2 (handlers.go): the provider's own signal that the
	// visitor declined consent, checked only after state has already
	// been validated -- identical to handleGoogleCallback.
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderGitHub, tx.Purpose, cancelledErrorCode, "user denied consent (access_denied)")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, "callback missing code and no recognized error parameter")
		return
	}

	// tx.RedirectURI (not s.githubRedirectURL()): the exact redirect_uri
	// Begin stored for THIS transaction, not one rebuilt from this
	// Service's current PublicOrigin config -- mirrors
	// handleGoogleCallback's own hardening (google.go/handlers.go) and
	// TestGoogleCallback_UsesStoredRedirectURI_NotCurrentPublicOrigin's
	// reasoning: a real provider enforces redirect_uri as an exact match
	// against what it issued the authorization code for.
	oauth2Cfg := s.githubOAuth2Config(tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, "token exchange failed")
		return
	}

	client := oauth2Cfg.Client(ctx, token)

	var user githubUser
	if err = s.githubAPIGet(ctx, client, "/user", &user); err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, "github /user api call failed (non-200 status, network error, or malformed response)")
		return
	}
	if user.ID == 0 {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, "github /user response missing id")
		return
	}
	providerUserID := strconv.FormatInt(user.ID, 10)

	// purpose=link/reauth: resolved entirely off (provider, providerUserID)
	// by link.go's shared algorithm -- no email check at all (Task 10),
	// so GET /user/emails below is never even called for this branch
	// (fewer GitHub API calls, and a link never blocked merely because the
	// visitor has since made their GitHub email private).
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		if linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderGitHub, providerUserID); linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderGitHub, tx.Purpose, linkErr)
			return
		}
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
		return
	}

	var emails []githubEmail
	if err = s.githubAPIGet(ctx, client, "/user/emails", &emails); err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, "github /user/emails api call failed (non-200 status, network error, or malformed response)")
		return
	}
	email, ok := primaryVerifiedGitHubEmail(emails)
	if !ok {
		s.redirectWithError(w, r, ProviderGitHub, tx.Purpose, emailNotVerifiedErrorCode, "no verified primary email in /user/emails")
		return
	}

	result, err := s.resolveLoginIdentity(ctx, ProviderGitHub, providerUserID, email, githubDisplayName(user))
	if err != nil {
		s.writeInternalError(w, r, ProviderGitHub, "resolve_login_identity", err)
		return
	}
	if result.Kind == loginResultEmailCollision {
		s.redirectEmailAlreadyRegistered(w, r, ProviderGitHub)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: Issue tolerates an empty ip
	rawSession, _, err := s.sessions.Issue(ctx, result.User.ID, r.UserAgent(), clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderGitHub, "issue_session", err)
		return
	}

	SetSessionCookie(w, rawSession)
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
}
