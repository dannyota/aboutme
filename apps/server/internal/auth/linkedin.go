package auth

// LinkedIn follows its documented confidential web flow: no PKCE, client
// credentials in the token request body, and the OIDC nonce as the
// code-injection defense (RFC 9700 section 2.1.1). Its email and
// email_verified claims are optional, so EmailVerified is a *bool and a missing
// claim never counts as verified for registration. See
// docs/design/linkedin-sign-in.md and ADR 0058.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// linkedinIssuer is LinkedIn's OIDC discovery issuer. Tests override only the
// issuer URL and exercise the same flow.
const linkedinIssuer = "https://www.linkedin.com/oauth"

// linkedinScopes are the OAuth2 scopes requested for LinkedIn login:
// "openid profile email". See docs/design/security.md.
var linkedinScopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

// linkedinCancelErrors are the callback error values that mean the person
// canceled: LinkedIn's two documented values and the RFC 6749 access_denied.
var linkedinCancelErrors = map[string]bool{
	"user_cancelled_login":     true, //nolint:misspell // LinkedIn's exact wire value.
	"user_cancelled_authorize": true, //nolint:misspell // LinkedIn's exact wire value.
	"access_denied":            true,
}

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

// linkedinOAuth2Config builds a request-local configuration. LinkedIn documents
// client_id and client_secret as token request form parameters, so the auth
// style is fixed: auto-detection would first send HTTP Basic credentials.
func (s *Service) linkedinOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	endpoint.AuthStyle = oauth2.AuthStyleInParams
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

// buildLinkedInAuthorizeURL binds state and an OIDC nonce and sends no PKCE
// challenge. The transaction still stores a verifier that LinkedIn never
// receives, so storage matches the other providers.
func (s *Service) buildLinkedInAuthorizeURL(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID, returnPath string) (handle, authURL, op string, err error) {
	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		return "", "", "linkedin_provider_discovery", err
	}

	redirectURI := s.linkedinRedirectURL()
	handle, tx, err := s.tx.begin(ctx, ProviderLinkedIn, purpose, linkingUserID, redirectURI, returnPath)
	if err != nil {
		return "", "", "begin_transaction", err
	}

	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), redirectURI)
	return handle, oauth2Cfg.AuthCodeURL(tx.State, oidc.Nonce(tx.Nonce)), "", nil
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

	// Check the provider's error only after validating state. Any error, even
	// with a code beside it, stops the flow before the token exchange.
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		if linkedinCancelErrors[providerError] {
			s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, cancelledErrorCode, reasonConsentDenied)
			return
		}
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, reasonLinkedInProviderError)
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

	// Use the transaction's exact authorization-time redirect URI. No
	// code_verifier: LinkedIn's token endpoint rejects it from a confidential
	// client, and the nonce check below binds the code to this transaction.
	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code)
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

	// go-oidc exposes the nonce but does not validate it. For LinkedIn this
	// check is the code-injection defense, so it runs before any claim is used.
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
		pendingRaw, linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderLinkedIn, idToken.Subject)
		if linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderLinkedIn, tx.Purpose, linkErr)
			return
		}
		s.finishLinkOrReauth(w, r, tx, pendingRaw)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: IssueTx tolerates an empty ip
	ua := r.UserAgent()

	login, found, err := s.resolveProviderLogin(ctx, ProviderSubject{
		Provider: ProviderLinkedIn,
		Subject:  idToken.Subject,
	}, ua, clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "resolve_provider_login", err)
		return
	}
	if found {
		s.finishProviderLogin(w, r, ProviderLinkedIn, tx, login)
		return
	}

	// Only a new identity requires a present, true, canonical email.
	// Existing identities do not re-evaluate email. See docs/design/security.md.
	if claims.Email == "" || claims.EmailVerified == nil || !*claims.EmailVerified {
		s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, emailNotVerifiedErrorCode,
			reasonLinkedInRegistrationEmailUnverified)
		return
	}
	canonicalEmail, err := accountemail.Canonicalize(claims.Email)
	if err != nil {
		s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, emailNotVerifiedErrorCode,
			reasonLinkedInRegistrationEmailUnverified)
		return
	}

	login, err = s.createProviderLogin(ctx, NewProviderAccount{
		Subject:       ProviderSubject{Provider: ProviderLinkedIn, Subject: idToken.Subject},
		VerifiedEmail: canonicalEmail,
		Name:          claims.Name,
	}, ua, clientIP)
	if err != nil {
		if errors.Is(err, errEmailAlreadyRegistered) {
			s.redirectEmailAlreadyRegistered(w, r, ProviderLinkedIn)
			return
		}
		s.writeInternalError(w, r, ProviderLinkedIn, "create_provider_login", err)
		return
	}

	s.finishProviderLogin(w, r, ProviderLinkedIn, tx, login)
}
