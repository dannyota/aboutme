package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// buildGoogleAuthorizeURL binds PKCE S256 and an OIDC nonce to the stored
// transaction. start.go owns the GET-versus-POST response policy.
func (s *Service) buildGoogleAuthorizeURL(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID, returnPath string) (handle, authURL, op string, err error) {
	provider, err := s.googleProvider(ctx)
	if err != nil {
		return "", "", "google_provider_discovery", err
	}

	redirectURI := s.googleRedirectURL()
	handle, tx, err := s.tx.begin(ctx, ProviderGoogle, purpose, linkingUserID, redirectURI, returnPath)
	if err != nil {
		return "", "", "begin_transaction", err
	}

	oauth2Cfg := s.googleOAuth2Config(provider.Endpoint(), redirectURI)
	return handle, oauth2Cfg.AuthCodeURL(tx.State,
		oauth2.S256ChallengeOption(tx.PKCEVerifier),
		oidc.Nonce(tx.Nonce),
	), "", nil
}

// handleGoogleCallback consumes the transaction, verifies PKCE and OIDC, then
// resolves the provider identity. Every exit clears the transaction cookie
// before writing the response; deferring the clear would miss real clients.
func (s *Service) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	// Discovery, token exchange, and JWKS fetch share one bounded client.
	ctx := withProviderHTTPClient(r.Context())
	if s.googleLocalOIDC && s.googleIssuerOverride == "" {
		ctx = withLocalProviderHTTPClient(ctx)
	}

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, Transaction{Purpose: PurposeLogin}, reasonTxCookieMissing)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGoogle)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderGoogle, Transaction{Purpose: PurposeLogin}, reasonTxInvalid)
			return
		}
		s.writeInternalError(w, r, ProviderGoogle, "consume_transaction", err)
		return
	}

	// State prevents an attacker from splicing their authorization code into
	// a victim's pending transaction; the cookie and PKCE do not replace it.
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonStateMismatch)
		return
	}

	// Validate state before exposing the friendlier consent-denied result.
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderGoogle, tx, cancelledErrorCode, reasonConsentDenied)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonAuthorizationCodeMissing)
		return
	}

	provider, err := s.googleProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "google_provider_discovery", err)
		return
	}

	// Use the redirect URI stored for this transaction. The provider requires
	// it to match the authorization request exactly, even if configuration
	// changed before the callback.
	oauth2Cfg := s.googleOAuth2Config(provider.Endpoint(), tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonTokenExchangeFailed)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonIDTokenMissing)
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.google.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonIDTokenVerificationFailed)
		return
	}

	// go-oidc exposes the nonce but does not validate it.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonNonceMismatch)
		return
	}

	if tx.Purpose == PurposeView {
		s.handleViewCallback(w, r, ProviderGoogle, tx)
		return
	}

	var claims googleClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx, reasonIDTokenClaimsDecodeFailed)
		return
	}

	// Link and reauth use provider identity without an email check.
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		pendingRaw, linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderGoogle, idToken.Subject)
		if linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderGoogle, tx, linkErr)
			return
		}
		s.finishLinkOrReauth(w, r, tx, pendingRaw)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: IssueTx tolerates an empty ip
	ua := r.UserAgent()

	login, found, err := s.resolveProviderLogin(ctx, ProviderSubject{
		Provider: ProviderGoogle,
		Subject:  idToken.Subject,
	}, ua, clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "resolve_provider_login", err)
		return
	}
	if found {
		s.finishProviderLogin(w, r, ProviderGoogle, tx, login)
		return
	}

	// A new subject requires a verified, canonical email.
	if claims.Email == "" || !claims.EmailVerified {
		s.redirectWithError(w, r, ProviderGoogle, tx, emailNotVerifiedErrorCode, reasonEmailNotVerified)
		return
	}
	canonicalEmail, err := accountemail.Canonicalize(claims.Email)
	if err != nil {
		s.redirectWithError(w, r, ProviderGoogle, tx, emailNotVerifiedErrorCode, reasonEmailNotVerified)
		return
	}

	login, err = s.createProviderLogin(ctx, NewProviderAccount{
		Subject:       ProviderSubject{Provider: ProviderGoogle, Subject: idToken.Subject},
		VerifiedEmail: canonicalEmail,
		Name:          claims.Name,
	}, ua, clientIP)
	if err != nil {
		if errors.Is(err, errEmailAlreadyRegistered) {
			s.redirectEmailAlreadyRegistered(w, r, ProviderGoogle)
			return
		}
		s.writeInternalError(w, r, ProviderGoogle, "create_provider_login", err)
		return
	}

	s.finishProviderLogin(w, r, ProviderGoogle, tx, login)
}
