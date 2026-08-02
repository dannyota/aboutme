package auth

// linkedin.go mirrors google.go/handlers.go's Google login shape for
// "Sign in with LinkedIn" (design spec §3's OAuth table), with LinkedIn's
// one real mechanical difference: `email`/`email_verified` are OPTIONAL
// OIDC claims (google.go's Google claims always carry them), so
// linkedinClaims.EmailVerified is `*bool` (nil = claim absent) rather
// than a plain bool, and the registration-blocking email rule below is
// this file's whole reason to exist (task-5-brief.md, AC-AUTH-002).

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// linkedinIssuer is the real LinkedIn OIDC discovery issuer (LinkedIn's
// "Sign In with LinkedIn using OpenID Connect" product publishes its
// discovery document at
// https://www.linkedin.com/oauth/.well-known/openid-configuration, whose
// "issuer" claim is this value). Tests point Service at a different
// issuer instead (an oidctest.Provider's URL) via
// NewServiceForTest/linkedinIssuerOverride, the same seam google.go's
// googleIssuer uses.
const linkedinIssuer = "https://www.linkedin.com/oauth"

// linkedinScopes are the OAuth2 scopes requested for LinkedIn login:
// design spec §3 pins these exactly ("openid profile email") for
// LinkedIn's OIDC product.
var linkedinScopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

// linkedinClaims is the subset of a LinkedIn ID token's claims this
// package reads. Unlike googleClaims, Email and EmailVerified are both
// OPTIONAL here (design spec §3: "email/email_verified are optional in
// LinkedIn's OIDC") -- EmailVerified is `*bool` (nil = claim absent)
// specifically so handleLinkedInCallback's registration check (below) can
// tell "claim absent" apart from "claim present and false", both of which
// must reject registration (never treat an absent claim as verified).
// Name, like googleClaims.Name, is optional and has no dedicated
// presence signal (LinkedIn's OIDC "name" claim, when granted, is a
// plain string like Google's).
type linkedinClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

// linkedinProviderConfig holds LinkedIn's OAuth2 client credentials and
// the lazily-discovered, cached *oidc.Provider backing them, via the
// shared oidcProviderCache (provider_cache.go) -- see
// googleProviderConfig's identical shape.
type linkedinProviderConfig struct {
	clientID     string
	clientSecret string

	cache oidcProviderCache
}

// linkedinProvider returns the discovered LinkedIn OIDC provider,
// discovering (and caching) it on first use -- see googleProvider's
// identical reasoning (NewService performs no network I/O of its own)
// and oidcProviderCache.discover for the caching/concurrency contract.
func (s *Service) linkedinProvider(ctx context.Context) (*oidc.Provider, error) {
	issuer := linkedinIssuer
	if s.linkedinIssuerOverride != "" {
		issuer = s.linkedinIssuerOverride
	}

	p, err := s.linkedin.cache.discover(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover linkedin oidc provider: %w", err)
	}
	return p, nil
}

// linkedinOAuth2Config builds the oauth2.Config for a single request, from
// the discovered provider's endpoint and this Service's redirect URL --
// see googleOAuth2Config's identical shape.
func (s *Service) linkedinOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	return oauth2.Config{
		ClientID:     s.linkedin.clientID,
		ClientSecret: s.linkedin.clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       linkedinScopes,
	}
}

// linkedinRedirectURL is the absolute callback URL registered with
// LinkedIn and sent as this flow's redirect_uri -- see
// googleRedirectURL's identical shape and reasoning.
func (s *Service) linkedinRedirectURL() string {
	return s.publicOrigin + LinkedInCallbackPath
}

// handleLinkedInStart begins a LinkedIn login transaction and redirects
// the browser to LinkedIn's own authorize endpoint, with PKCE (S256) and
// an OIDC nonce bound to the transaction -- see handleGoogleStart's
// identical shape.
func (s *Service) handleLinkedInStart(w http.ResponseWriter, r *http.Request) {
	// withProviderHTTPClient (provider_http.go): bounds the OIDC discovery
	// call below with a timeout, same as every other outbound provider
	// call this package makes.
	ctx := withProviderHTTPClient(r.Context())

	purpose, linkingUserID, ok := s.startPurposeAndLinkingUser(w, r)
	if !ok {
		return
	}

	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "linkedin_provider_discovery", err)
		return
	}

	redirectURI := s.linkedinRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderLinkedIn, purpose, linkingUserID, redirectURI)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "begin_transaction", err)
		return
	}

	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), redirectURI)
	authURL := oauth2Cfg.AuthCodeURL(tx.State,
		oauth2.S256ChallengeOption(tx.PKCEVerifier),
		oidc.Nonce(tx.Nonce),
	)

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleLinkedInCallback completes a LinkedIn login: consumes the OAuth
// transaction, exchanges the authorization code (with PKCE), verifies the
// ID token, resolves or creates the local user (applying the
// registration-only optional-email rule inline, below) or attaches/
// reauthenticates the identity for an already-authenticated caller
// (resolveLinkOrReauth, link.go, for purpose=link/reauth), and issues a
// session. Structurally identical to handleGoogleCallback except
// for the two points this doc comment calls out; see that function's own
// doc comment for the shared exit-path/cookie-clearing obligations both
// funnel through (redirectWithError/redirectAuthFailed/writeInternalError).
func (s *Service) handleLinkedInCallback(w http.ResponseWriter, r *http.Request) {
	// withProviderHTTPClient (provider_http.go): every outbound call below
	// (OIDC discovery, token exchange, ID token verification's JWKS fetch)
	// shares one bounded client.
	ctx := withProviderHTTPClient(r.Context())

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, PurposeLogin, "missing or malformed __Host-oauth-tx cookie")
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderLinkedIn)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderLinkedIn, PurposeLogin, "oauth transaction invalid (unknown, expired, replayed, or wrong provider)")
			return
		}
		s.writeInternalError(w, r, ProviderLinkedIn, "consume_transaction", err)
		return
	}

	// See handleGoogleCallback's identical state-parameter check (RFC
	// 6749 §10.12).
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "state parameter mismatch")
		return
	}

	// See handleGoogleCallback's identical ruling b2 ordering (checked
	// only after state has already been validated above).
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, cancelledErrorCode, "user denied consent (access_denied)")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "callback missing code and no recognized error parameter")
		return
	}

	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "linkedin_provider_discovery", err)
		return
	}

	// tx.RedirectURI, not s.linkedinRedirectURL() -- see
	// handleGoogleCallback's identical hardening note.
	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "token exchange failed")
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "token response missing id_token")
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.linkedin.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "id_token verification failed (issuer, audience, signature, or expiry)")
		return
	}

	// go-oidc does NOT check nonce automatically -- see
	// handleGoogleCallback's identical comment.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "nonce mismatch")
		return
	}

	var claims linkedinClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, tx.Purpose, "id_token claims decode failed")
		return
	}

	// purpose=link/reauth: resolved entirely by link.go's shared
	// algorithm, off tx.LinkingUserID -- no email check at all (Task 10's
	// link algorithm; design spec §3's "linking to an existing account
	// still allowed [without a verified email]" carve-out applies with no
	// exception here), so the registration-only email rule below never
	// runs for this branch.
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		if linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderLinkedIn, idToken.Subject); linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderLinkedIn, tx.Purpose, linkErr)
			return
		}
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
		return
	}

	// Registration-only verified-email carve-out (design spec §3,
	// AC-AUTH-002): checked ONLY when about to register a brand-new
	// identity -- an existing identity's repeat login never re-checks
	// email at all (resolveLoginIdentity's ExistingIdentity branch does
	// no email comparison whatsoever, the same as every other provider),
	// so this pre-check exists purely to decide whether THIS rule
	// applies before resolveLoginIdentity's own (email-agnostic-once-
	// identity-is-known) algorithm runs. Deliberately
	// `email == "" || emailVerified == nil || !*emailVerified`, NOT
	// `emailVerified == nil || *emailVerified` (which reads almost
	// identical but is backwards -- it would treat an ABSENT claim as
	// verified, exactly the bug design spec §3's "absent email_verified
	// is never treated as true" rule exists to forbid).
	if _, identityErr := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(ProviderLinkedIn),
		ProviderUserID: idToken.Subject,
	}); identityErr != nil {
		if !errors.Is(identityErr, pgx.ErrNoRows) {
			s.writeInternalError(w, r, ProviderLinkedIn, "check_existing_identity", identityErr)
			return
		}
		// Unknown identity: about to register. THE rule.
		if claims.Email == "" || claims.EmailVerified == nil || !*claims.EmailVerified {
			s.redirectWithError(w, r, ProviderLinkedIn, tx.Purpose, emailNotVerifiedErrorCode,
				"linkedin registration requires a verified email (unknown identity, not a link)")
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
