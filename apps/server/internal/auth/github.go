package auth

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

	"danny.vn/aboutme/apps/server/internal/accountemail"
	"danny.vn/aboutme/apps/server/internal/api"
)

const (
	GitHubStartPath    = "/api/v1/auth/github/start"
	GitHubCallbackPath = "/api/v1/auth/github/callback"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubAPIBaseURL   = "https://api.github.com"
)

func githubScopes() []string {
	return []string{"user:email"}
}

type githubProviderConfig struct {
	clientID     string
	clientSecret string
}

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
		Scopes:      githubScopes(),
	}
}

func (s *Service) githubAPIBaseURLFor() string {
	if s.githubEndpointOverride != "" {
		return s.githubEndpointOverride
	}
	return s.githubAPIBaseURL
}

func (s *Service) githubRedirectURL() string {
	return s.publicOrigin + GitHubCallbackPath
}

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

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

func primaryVerifiedGitHubEmail(emails []githubEmail) (email string, ok bool) {
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true
		}
	}
	return "", false
}

func githubDisplayName(user githubUser) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Login
}

func (s *Service) buildGitHubAuthorizeURL(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID, returnPath string) (handle, authURL, op string, err error) {
	redirectURI := s.githubRedirectURL()
	handle, tx, err := s.tx.begin(ctx, ProviderGitHub, purpose, linkingUserID, redirectURI, returnPath)
	if err != nil {
		return "", "", "begin_transaction", err
	}

	oauth2Cfg := s.githubOAuth2Config(redirectURI)
	return handle, oauth2Cfg.AuthCodeURL(tx.State, oauth2.S256ChallengeOption(tx.PKCEVerifier)), "", nil
}

func (s *Service) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	ctx := withProviderHTTPClient(r.Context())
	ctx = s.withGitHubProviderHTTPClient(ctx)

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, Transaction{Purpose: PurposeLogin}, reasonTxCookieMissing)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGitHub)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderGitHub, Transaction{Purpose: PurposeLogin}, reasonTxInvalid)
			return
		}
		s.writeInternalError(w, r, ProviderGitHub, "consume_transaction", err)
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx, reasonStateMismatch)
		return
	}

	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderGitHub, tx, cancelledErrorCode, reasonConsentDenied)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx, reasonAuthorizationCodeMissing)
		return
	}

	oauth2Cfg := s.githubOAuth2Config(tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx, reasonTokenExchangeFailed)
		return
	}

	client := oauth2Cfg.Client(ctx, token)

	var user githubUser
	if err = s.githubAPIGet(ctx, client, "/user", &user); err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx, reasonGitHubUserAPIFailed)
		return
	}
	if user.ID == 0 {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx, reasonGitHubUserIDMissing)
		return
	}
	providerUserID := strconv.FormatInt(user.ID, 10)

	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		pendingRaw, linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderGitHub, providerUserID)
		if linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderGitHub, tx, linkErr)
			return
		}
		s.finishLinkOrReauth(w, r, tx, pendingRaw)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies)
	ua := r.UserAgent()

	login, found, err := s.resolveProviderLogin(ctx, ProviderSubject{
		Provider: ProviderGitHub,
		Subject:  providerUserID,
	}, ua, clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderGitHub, "resolve_provider_login", err)
		return
	}
	if found {
		s.finishProviderLogin(w, r, ProviderGitHub, tx, login)
		return
	}

	var emails []githubEmail
	if err = s.githubAPIGet(ctx, client, "/user/emails", &emails); err != nil {
		s.redirectAuthFailed(w, r, ProviderGitHub, tx, reasonGitHubUserEmailsAPIFailed)
		return
	}
	email, ok := primaryVerifiedGitHubEmail(emails)
	if !ok {
		s.redirectWithError(w, r, ProviderGitHub, tx, emailNotVerifiedErrorCode, reasonGitHubNoVerifiedPrimaryEmail)
		return
	}
	canonicalEmail, err := accountemail.Canonicalize(email)
	if err != nil {
		s.redirectWithError(w, r, ProviderGitHub, tx, emailNotVerifiedErrorCode, reasonGitHubNoVerifiedPrimaryEmail)
		return
	}

	login, err = s.createProviderLogin(ctx, NewProviderAccount{
		Subject:       ProviderSubject{Provider: ProviderGitHub, Subject: providerUserID},
		VerifiedEmail: canonicalEmail,
		Name:          githubDisplayName(user),
	}, ua, clientIP)
	if err != nil {
		if errors.Is(err, errEmailAlreadyRegistered) {
			s.redirectEmailAlreadyRegistered(w, r, ProviderGitHub)
			return
		}
		s.writeInternalError(w, r, ProviderGitHub, "create_provider_login", err)
		return
	}

	s.finishProviderLogin(w, r, ProviderGitHub, tx, login)
}
