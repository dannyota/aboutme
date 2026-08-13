package auth

// LinkedIn follows the Google OIDC flow, but its email and email_verified
// claims are optional. EmailVerified is therefore a *bool, and a missing claim
// never counts as verified for registration.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// linkedinIssuer is LinkedIn's OIDC discovery issuer. Tests override only the
// issuer URL and exercise the same flow.
const linkedinIssuer = "https://www.linkedin.com/oauth"

// linkedinScopes are the OAuth2 scopes requested for LinkedIn login:
// "openid profile email". See docs/design/security.md.
var linkedinScopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

// linkedinClaims keeps email verification nullable so an absent claim cannot
// count as verified. Name is optional.
type linkedinClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

// linkedinProviderConfig holds LinkedIn's credentials and lazy discovery cache.
type linkedinProviderConfig struct {
	clientID     string
	clientSecret string

	cache oidcProviderCache
}

// linkedinProvider discovers and caches LinkedIn's provider on first use.
func (s *Service) linkedinProvider(ctx context.Context) (*oidc.Provider, error) {
	issuer := s.linkedinIssuerURL
	local := s.linkedinLocalOIDC
	if s.linkedinIssuerOverride != "" {
		issuer = s.linkedinIssuerOverride
		local = false
	}
	if local {
		ctx = withLocalProviderHTTPClient(ctx)
	}

	p, err := s.linkedin.cache.discover(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover linkedin oidc provider: %w", err)
	}
	if local {
		if err := validateLocalOIDCProvider(p, s.publicOrigin, ProviderLinkedIn); err != nil {
			return nil, fmt.Errorf("auth: validate linkedin oidc provider: %w", err)
		}
	}
	return p, nil
}

// linkedinOAuth2Config builds a request-local configuration.
func (s *Service) linkedinOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	return oauth2.Config{
		ClientID:     s.linkedin.clientID,
		ClientSecret: s.linkedin.clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       linkedinScopes,
	}
}

// linkedinRedirectURL must match exactly at authorization and token exchange.
func (s *Service) linkedinRedirectURL() string {
	return s.publicOrigin + LinkedInCallbackPath
}

// buildLinkedInAuthorizeURL binds PKCE S256 and an OIDC nonce.
func (s *Service) buildLinkedInAuthorizeURL(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID) (handle, authURL, op string, err error) {
	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		return "", "", "linkedin_provider_discovery", err
	}

	redirectURI := s.linkedinRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderLinkedIn, purpose, linkingUserID, redirectURI)
	if err != nil {
		return "", "", "begin_transaction", err
	}

	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), redirectURI)
	return handle, oauth2Cfg.AuthCodeURL(tx.State,
		oauth2.S256ChallengeOption(tx.PKCEVerifier),
		oidc.Nonce(tx.Nonce),
	), "", nil
}

// handleLinkedInCallback verifies OIDC and applies LinkedIn's nullable-email
// registration rule. Every exit clears the transaction cookie before responding.
func (s *Service) handleLinkedInCallback(w http.ResponseWriter, r *http.Request) {
	// Discovery, token exchange, and JWKS fetch share one bounded client.
	ctx := withProviderHTTPClient(r.Context())
	if s.linkedinLocalOIDC && s.linkedinIssuerOverride == "" {
		ctx = withLocalProviderHTTPClient(ctx)
	}

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, PurposeLogin, reasonTxCookieMissing)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderLinkedIn)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderLinkedIn, PurposeLogin, reasonTxInvalid)
			return
		}
		s.writeInternalError(w, r, ProviderLinkedIn, "consume_transaction", err)
		return
	}

	// State prevents authorization-code splicing.
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonStateMismatch)
		return
	}

	// Check the provider's consent denial only after validating state.
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, cancelledErrorCode, reasonConsentDenied)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonAuthorizationCodeMissing)
		return
	}

	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "linkedin_provider_discovery", err)
		return
	}

	// Use the transaction's exact authorization-time redirect URI.
	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonTokenExchangeFailed)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonIDTokenMissing)
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.linkedin.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonIDTokenVerificationFailed)
		return
	}

	// go-oidc exposes the nonce but does not validate it.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonNonceMismatch)
		return
	}

	var claims linkedinClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonIDTokenClaimsDecodeFailed)
		return
	}

	// Link and reauth use provider identity without an email check.
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		if linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderLinkedIn, idToken.Subject); linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderLinkedIn, tx.Purpose, linkErr)
			return
		}
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
		return
	}

	// Only a new identity requires a present, true email_verified claim.
	// Existing identities do not re-evaluate email. See docs/design/security.md.
	if _, identityErr := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(ProviderLinkedIn),
		ProviderUserID: idToken.Subject,
	}); identityErr != nil {
		if !errors.Is(identityErr, pgx.ErrNoRows) {
			s.writeInternalError(w, r, ProviderLinkedIn, "check_existing_identity", identityErr)
			return
		}
		// A new identity requires a present, true email_verified claim.
		if claims.Email == "" || claims.EmailVerified == nil || !*claims.EmailVerified {
			s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, emailNotVerifiedErrorCode,
				reasonLinkedInRegistrationEmailUnverified)
			return
		}
	}

	result, err := s.resolveLoginIdentity(ctx, ProviderLinkedIn, idToken.Subject, claims.Email, claims.Name)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "resolve_login_identity", err)
		return
	}
	if result.Kind == loginResultEmailCollision {
		s.redirectEmailAlreadyRegistered(w, r, ProviderLinkedIn)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: Issue tolerates an empty ip
	rawSession, _, err := s.sessions.Issue(ctx, result.User.ID, r.UserAgent(), clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "issue_session", err)
		return
	}

	SetSessionCookie(w, rawSession)
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
}
