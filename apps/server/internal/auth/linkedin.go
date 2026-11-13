package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/accountemail"
	"danny.vn/aboutme/apps/server/internal/api"
)

const linkedinIssuer = "https://www.linkedin.com/oauth"

func linkedinScopes() []string {
	return []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}
}

var linkedinCancelErrors = map[string]bool{
	"user_cancelled_login":     true, //nolint:misspell // LinkedIn's exact wire value.
	"user_cancelled_authorize": true, //nolint:misspell // LinkedIn's exact wire value.
	"access_denied":            true,
}

type linkedinClaims struct {
	Email         string        `json:"email"`
	EmailVerified *linkedinBool `json:"email_verified"`
	Name          string        `json:"name"`
}

// LinkedIn ID tokens send email_verified as the JSON string "true" or "false", not a JSON boolean.
type linkedinBool bool

func (b *linkedinBool) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch v := raw.(type) {
	case bool:
		*b = linkedinBool(v)
	case string:
		*b = linkedinBool(v == "true")
	default:
		*b = false
	}
	return nil
}

type linkedinProviderConfig struct {
	clientID     string
	clientSecret string

	cache oidcProviderCache
}

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

func (s *Service) linkedinOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return oauth2.Config{
		ClientID:     s.linkedin.clientID,
		ClientSecret: s.linkedin.clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       linkedinScopes(),
	}
}

func (s *Service) linkedinRedirectURL() string {
	return s.publicOrigin + LinkedInCallbackPath
}

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

func (s *Service) handleLinkedInCallback(w http.ResponseWriter, r *http.Request) {
	ctx := withProviderHTTPClient(r.Context())
	if s.linkedinLocalOIDC && s.linkedinIssuerOverride == "" {
		ctx = withLocalProviderHTTPClient(ctx)
	}

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, Transaction{Purpose: PurposeLogin}, reasonTxCookieMissing)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderLinkedIn)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderLinkedIn, Transaction{Purpose: PurposeLogin}, reasonTxInvalid)
			return
		}
		s.writeInternalError(w, r, ProviderLinkedIn, "consume_transaction", err)
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonStateMismatch)
		return
	}

	if providerError := r.URL.Query().Get("error"); providerError != "" {
		if linkedinCancelErrors[providerError] {
			s.redirectWithError(w, r, ProviderLinkedIn, tx, cancelledErrorCode, reasonConsentDenied)
			return
		}
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonLinkedInProviderError)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonAuthorizationCodeMissing)
		return
	}

	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "linkedin_provider_discovery", err)
		return
	}

	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonTokenExchangeFailed)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonIDTokenMissing)
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.linkedin.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonIDTokenVerificationFailed)
		return
	}

	// go-oidc's IDTokenVerifier.Verify does not validate the nonce; that is the caller's job (go-oidc docs).
	if idToken.Nonce != "" && idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonNonceMismatch)
		return
	}

	if tx.Purpose == PurposeView {
		s.handleViewCallback(w, r, ProviderLinkedIn, tx)
		return
	}

	var claims linkedinClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx, reasonIDTokenClaimsDecodeFailed)
		return
	}

	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		pendingRaw, linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderLinkedIn, idToken.Subject)
		if linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderLinkedIn, tx, linkErr)
			return
		}
		s.finishLinkOrReauth(w, r, tx, pendingRaw)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies)
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

	if claims.Email == "" || claims.EmailVerified == nil || !bool(*claims.EmailVerified) {
		s.redirectWithError(w, r, ProviderLinkedIn, tx, emailNotVerifiedErrorCode,
			reasonLinkedInRegistrationEmailUnverified)
		return
	}
	canonicalEmail, err := accountemail.Canonicalize(claims.Email)
	if err != nil {
		s.redirectWithError(w, r, ProviderLinkedIn, tx, emailNotVerifiedErrorCode,
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
