package auth

// GitHub uses OAuth2, never OIDC or a nonce. Its distinct callback and the
// transaction provider check prevent mix-up. See docs/design/security.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// GitHubStartPath and GitHubCallbackPath are the registered GitHub routes.
const (
	GitHubStartPath    = "/api/v1/auth/github/start"
	GitHubCallbackPath = "/api/v1/auth/github/callback"
)

// Tests replace all GitHub endpoints through one service override.
const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubAPIBaseURL   = "https://api.github.com"
)

// githubScopes permits reading a private verified primary email.
var githubScopes = []string{"user:email"}

// githubProviderConfig holds GitHub's OAuth2 client credentials.
type githubProviderConfig struct {
	clientID     string
	clientSecret string
}

// githubOAuth2Config applies the production or test endpoints.
func (s *Service) githubOAuth2Config(redirectURL string) oauth2.Config {
	authorizeURL, tokenURL := s.githubOAuthAuthorizeURL, s.githubOAuthTokenURL
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
	return s.githubAPIBaseURL
}

// githubRedirectURL must match exactly at authorization and token exchange.
func (s *Service) githubRedirectURL() string {
	return s.publicOrigin + GitHubCallbackPath
}

// githubUser contains the profile fields needed for local identity and display.
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

// githubAPIGet uses the token-bearing client and bounds the response before JSON
// decoding.
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

// primaryVerifiedGitHubEmail applies the registration rule in
// docs/design/security.md.
func primaryVerifiedGitHubEmail(emails []githubEmail) (email string, ok bool) {
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true
		}
	}
	return "", false
}

// githubDisplayName falls back to the required GitHub login.
func githubDisplayName(user githubUser) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Login
}

// buildGitHubAuthorizeURL binds PKCE S256 without OIDC discovery or nonce.
func (s *Service) buildGitHubAuthorizeURL(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID) (handle, authURL, op string, err error) {
	redirectURI := s.githubRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderGitHub, purpose, linkingUserID, redirectURI)
	if err != nil {
		return "", "", "begin_transaction", err
	}

	oauth2Cfg := s.githubOAuth2Config(redirectURI)
	// GitHub issues no ID token to bind a nonce to.
	return handle, oauth2Cfg.AuthCodeURL(tx.State, oauth2.S256ChallengeOption(tx.PKCEVerifier)), "", nil
}

// handleGitHubCallback verifies OAuth2 and resolves the numeric GitHub identity.
// Provider API failures use the generic callback rejection; local failures use
// an opaque 500. Every exit clears the transaction cookie before responding.
func (s *Service) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	// Token exchange and API calls share one bounded client.
	ctx := withProviderHTTPClient(r.Context())
	ctx = s.withGitHubProviderHTTPClient(ctx)

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, PurposeLogin, reasonTxCookieMissing)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGitHub)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderGitHub, PurposeLogin, reasonTxInvalid)
			return
		}
		s.writeInternalError(w, r, ProviderGitHub, "consume_transaction", err)
		return
	}

	// State prevents authorization-code splicing.
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, reasonStateMismatch)
		return
	}

	// Check the provider's consent denial only after validating state.
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderGitHub, tx.Purpose, cancelledErrorCode, reasonConsentDenied)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, reasonAuthorizationCodeMissing)
		return
	}

	// Use the redirect URI stored for this transaction. The provider requires
	// it to match the authorization request exactly, even if configuration
	// changed before the callback.
	oauth2Cfg := s.githubOAuth2Config(tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, reasonTokenExchangeFailed)
		return
	}

	client := oauth2Cfg.Client(ctx, token)

	var user githubUser
	if err = s.githubAPIGet(ctx, client, "/user", &user); err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, reasonGitHubUserAPIFailed)
		return
	}
	if user.ID == 0 {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, reasonGitHubUserIDMissing)
		return
	}
	providerUserID := strconv.FormatInt(user.ID, 10)

	// Link and reauth use provider identity without fetching email.
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
		s.redirectAuthFailed(w, r, ProviderGitHub, tx.Purpose, reasonGitHubUserEmailsAPIFailed)
		return
	}
	email, ok := primaryVerifiedGitHubEmail(emails)
	if !ok {
		s.redirectWithError(w, r, ProviderGitHub, tx.Purpose, emailNotVerifiedErrorCode, reasonGitHubNoVerifiedPrimaryEmail)
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
