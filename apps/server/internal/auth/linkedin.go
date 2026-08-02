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
	"github.com/google/uuid"
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
// specifically so resolveLinkedInUser's registration check can tell
// "claim absent" apart from "claim present and false", both of which
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
	ctx := r.Context()

	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "linkedin_provider_discovery", err)
		return
	}

	redirectURI := s.linkedinRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderLinkedIn, PurposeLogin, uuid.Nil, redirectURI)
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
// registration-only optional-email rule -- see resolveLinkedInUser), and
// issues a session. Structurally identical to handleGoogleCallback except
// for the two points this doc comment calls out; see that function's own
// doc comment for the shared exit-path/cookie-clearing obligations both
// funnel through (redirectWithError/redirectAuthFailed/writeInternalError).
func (s *Service) handleLinkedInCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "missing or malformed __Host-oauth-tx cookie")
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderLinkedIn)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderLinkedIn, "oauth transaction invalid (unknown, expired, replayed, or wrong provider)")
			return
		}
		s.writeInternalError(w, r, ProviderLinkedIn, "consume_transaction", err)
		return
	}

	// See handleGoogleCallback's identical state-parameter check (RFC
	// 6749 §10.12).
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "state parameter mismatch")
		return
	}

	// See handleGoogleCallback's identical ruling b2 ordering (checked
	// only after state has already been validated above).
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderLinkedIn, cancelledErrorCode, "user denied consent (access_denied)")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "callback missing code and no recognized error parameter")
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
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "token exchange failed")
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "token response missing id_token")
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.linkedin.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "id_token verification failed (issuer, audience, signature, or expiry)")
		return
	}

	// go-oidc does NOT check nonce automatically -- see
	// handleGoogleCallback's identical comment.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "nonce mismatch")
		return
	}

	var claims linkedinClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderLinkedIn, "id_token claims decode failed")
		return
	}

	// Unlike handleGoogleCallback, the email-verified check is NOT
	// applied here unconditionally: it's registration-only (design spec
	// §3's LinkedIn carve-out), so it lives inside resolveLinkedInUser,
	// which is the one place that already knows whether this callback is
	// about to create a brand-new user or not.
	usr, err := s.resolveLinkedInUser(ctx, tx.Purpose, tx.LinkingUserID, idToken.Subject, claims.Email, claims.EmailVerified, claims.Name)
	if err != nil {
		if errors.Is(err, errLinkedInRegistrationEmailNotVerified) {
			s.redirectWithError(w, r, ProviderLinkedIn, emailNotVerifiedErrorCode,
				"linkedin registration requires a verified email (unknown identity, not a link)")
			return
		}
		s.writeInternalError(w, r, ProviderLinkedIn, "resolve_user", err)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: Issue tolerates an empty ip
	rawSession, _, err := s.sessions.Issue(ctx, usr.ID, r.UserAgent(), clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderLinkedIn, "issue_session", err)
		return
	}

	SetSessionCookie(w, rawSession)
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.publicOrigin+"/", http.StatusFound)
}

// errLinkedInRegistrationEmailNotVerified is resolveLinkedInUser's
// sentinel for THE registration-only rejection (task-5-brief.md,
// AC-AUTH-002, design spec §3): an unknown LinkedIn identity (about to
// register a brand-new user, not attach to an already-authenticated one
// via purpose=link) whose email is absent, whose email_verified claim is
// absent, or whose email_verified claim is present but false.
var errLinkedInRegistrationEmailNotVerified = errors.New("auth: linkedin registration requires a verified email")

// resolveLinkedInUser resolves the local user for a LinkedIn login,
// mirroring resolveGoogleUser's TODO(Task 10) stub scope (an unknown
// identity unconditionally creates a new user -- Task 10 replaces this
// with the real three-way NewUser|ExistingIdentity|EmailCollision branch
// shared by every provider) with two LinkedIn-specific additions:
//
//   - purpose == PurposeLink attaches the new identity to the ALREADY
//     authenticated user linkingUserID names (Task 10's HTTP surface is
//     what establishes that precondition; this package trusts
//     tx.Purpose/tx.LinkingUserID as already-validated once a
//     transaction has been Begin'd with them), rather than creating a
//     brand-new user -- design spec §3's "linking to an existing account
//     still allowed" carve-out.
//   - otherwise (registration), THE optional-email rule applies:
//     `email == "" || emailVerified == nil || !*emailVerified` rejects
//     with errLinkedInRegistrationEmailNotVerified. This is deliberately
//     NOT `emailVerified == nil || *emailVerified` (which reads almost
//     identical but is backwards -- it would treat an ABSENT claim as
//     verified, exactly the bug design spec §3's "absent email_verified
//     is never treated as true" rule exists to forbid) and deliberately
//     NOT applied when an identity already exists (a returning LinkedIn
//     login reuses its user without re-checking email at all, the same
//     way resolveGoogleUser's existing-identity branch does).
func (s *Service) resolveLinkedInUser(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID, providerUserID, email string, emailVerified *bool, name string) (store.User, error) {
	identity, err := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(ProviderLinkedIn),
		ProviderUserID: providerUserID,
	})
	if err == nil {
		usr, getErr := s.q.GetUserByID(ctx, identity.UserID)
		if getErr != nil {
			return store.User{}, fmt.Errorf("auth: resolve linkedin user: get user: %w", getErr)
		}
		return usr, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, fmt.Errorf("auth: resolve linkedin user: get identity: %w", err)
	}

	// Unknown identity: purpose == PurposeLink attaches it to the
	// already-authenticated user linkingUserID names -- NOT registration,
	// so the email rule below never applies to this branch.
	if purpose == PurposeLink {
		usr, getErr := s.q.GetUserByID(ctx, linkingUserID)
		if getErr != nil {
			return store.User{}, fmt.Errorf("auth: resolve linkedin user: get linking user: %w", getErr)
		}
		if _, createErr := s.q.CreateIdentity(ctx, store.CreateIdentityParams{
			UserID:         usr.ID,
			Provider:       string(ProviderLinkedIn),
			ProviderUserID: providerUserID,
		}); createErr != nil {
			return store.User{}, fmt.Errorf("auth: resolve linkedin user: create identity: %w", createErr)
		}
		return usr, nil
	}

	// Registration path: THE rule (see this function's own doc comment
	// for why the exact predicate below, and not its inverted-nil look-
	// alike, is correct).
	if email == "" || emailVerified == nil || !*emailVerified {
		return store.User{}, errLinkedInRegistrationEmailNotVerified
	}

	if name == "" {
		// See resolveGoogleUser's identical fallback and its reasoning
		// (fix-round ruling b1): the LOCAL PART of the email, never the
		// full address.
		name = emailLocalPart(email)
	}

	usr, err := s.q.CreateUser(ctx, store.CreateUserParams{Email: email, Name: name})
	if err != nil {
		return store.User{}, fmt.Errorf("auth: resolve linkedin user: create user: %w", err)
	}
	if _, err := s.q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         usr.ID,
		Provider:       string(ProviderLinkedIn),
		ProviderUserID: providerUserID,
	}); err != nil {
		return store.User{}, fmt.Errorf("auth: resolve linkedin user: create identity: %w", err)
	}
	return usr, nil
}
