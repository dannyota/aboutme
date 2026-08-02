package auth

// handlers.go implements the OAuth login HTTP surface (design spec §3):
// GET /api/v1/auth/{provider}/start and its matching .../callback. See
// transaction.go for this package's own doc comment.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// GoogleStartPath and GoogleCallbackPath are the literal routes Service
// registers for "Sign in with Google" (design spec §3's OAuth table).
// Exported so callers (tests, and later phases wiring the same paths into
// docs/api/openapi.yaml) never need to hand-copy the literal strings.
const (
	GoogleStartPath    = "/api/v1/auth/google/start"
	GoogleCallbackPath = "/api/v1/auth/google/callback"
)

// authFailedErrorCode is the generic ?error= value every /callback
// rejection not explicitly pinned a distinct code by the plan redirects
// with (DD-C3, integration-owner ruling 2026-08-02): every OIDC
// verification failure, every ErrTransactionInvalid outcome (unknown/
// expired/replayed/wrong-provider transaction handle), a missing
// __Host-oauth-tx cookie, and an OAuth `state` mismatch all collapse into
// this one code, so a caller -- and, in turn, an attacker probing the
// callback -- gets no oracle distinguishing which of them actually
// happened. email_not_verified (and, from Task 10,
// email_already_registered) stay distinct: they are plan-pinned,
// user-actionable outcomes, not security-sensitive failure classes.
const authFailedErrorCode = "auth_failed"

// emailNotVerifiedErrorCode is the plan-pinned ?error= value for a
// callback whose verified-email requirement failed (design spec §3's
// Google row: "require email_verified == true").
const emailNotVerifiedErrorCode = "email_not_verified"

// Service implements the OAuth login HTTP surface for every provider
// (only Google is wired in this phase task; LinkedIn/GitHub follow the
// same pattern in later tasks). It holds the shared OAuth transaction and
// session machinery plus each provider's own client credentials.
type Service struct {
	tx       *TransactionStore
	q        *store.Queries
	sessions *SessionManager

	publicOrigin   string
	trustedProxies api.TrustedProxies

	google googleProviderConfig

	// googleIssuerOverride replaces the real googleIssuer
	// ("https://accounts.google.com") used for discovery. Empty in
	// production; set only by tests -- see NewServiceForTest -- so the
	// callback exercises exactly the same discovery/verify code path
	// against an in-process oidctest.Provider instead of the real Google
	// endpoint.
	googleIssuerOverride string
}

// NewService builds a Service backed by q, wiring per-provider OAuth2
// configuration from cfg. It performs no network I/O of its own (e.g. no
// OIDC discovery): each provider's discovery document is fetched lazily,
// and cached, the first time a request actually needs it (see
// (*Service).googleProvider) -- a server with no login traffic yet, or a
// dev environment with empty GOOGLE_CLIENT_ID, never blocks startup on
// it.
//
// NewService does not hold an internal/user.Store: that package's own
// constructor (user.New) takes a store.DBTX (pool/connection) and builds
// its own internal *store.Queries, which cannot be recovered from the
// *store.Queries this constructor is pinned to receive. Service instead
// calls q's CreateUser/GetUserByEmail/GetIdentityByProviderSubject/
// CreateIdentity methods directly -- the same store.Queries surface
// user.Store itself wraps, with equivalent pgx.ErrNoRows handling done
// inline (see resolveGoogleUser).
func NewService(cfg config.Config, q *store.Queries) (*Service, error) {
	if cfg.PublicOrigin == "" {
		return nil, fmt.Errorf("auth: NewService: config.PublicOrigin is required")
	}
	return &Service{
		tx:             NewTransactionStore(q),
		q:              q,
		sessions:       NewSessionManager(q),
		publicOrigin:   cfg.PublicOrigin,
		trustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
		google: googleProviderConfig{
			clientID:     cfg.GoogleClientID,
			clientSecret: cfg.GoogleClientSecret,
		},
	}, nil
}

// RegisterRoutes matches api.New's register signature (design decision
// 7): it attaches this Service's routes to mux, so cmd/server/main.go can
// wire it in without internal/api importing internal/auth (which imports
// internal/api itself -- the reverse import would cycle).
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(GoogleStartPath, route(http.MethodGet, s.handleGoogleStart))
	mux.Handle(GoogleCallbackPath, route(http.MethodGet, s.handleGoogleCallback))
}

// handleGoogleStart begins a Google login transaction and redirects the
// browser to Google's own authorize endpoint, with PKCE (S256) and an
// OIDC nonce bound to the transaction.
func (s *Service) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	provider, err := s.googleProvider(ctx)
	if err != nil {
		writeInternalError(w)
		return
	}

	redirectURI := s.googleRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderGoogle, PurposeLogin, uuid.Nil, redirectURI)
	if err != nil {
		writeInternalError(w)
		return
	}

	oauth2Cfg := s.googleOAuth2Config(provider.Endpoint(), redirectURI)
	authURL := oauth2Cfg.AuthCodeURL(tx.State,
		oauth2.S256ChallengeOption(tx.PKCEVerifier),
		oidc.Nonce(tx.Nonce),
	)

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleGoogleCallback completes a Google login: consumes the OAuth
// transaction, exchanges the authorization code (with PKCE), verifies the
// ID token (signature/issuer/audience/expiry via go-oidc, plus the
// nonce check go-oidc does NOT perform itself), resolves or creates the
// local user, and issues a session.
//
// ClearOAuthTxCookie is called on every exit path (ruling 1): every
// rejection funnels through redirectWithError/redirectAuthFailed, which
// call it before writing the redirect response, and the success path
// calls it explicitly in the same place. It is deliberately NOT a
// deferred call at the top of this function: a deferred ClearOAuthTxCookie
// would run after http.Redirect has already called WriteHeader on every
// exit path, and a Set-Cookie header added after WriteHeader has been
// called never reaches a real client (net/http headers must be set before
// the first WriteHeader/Write) -- only ResponseRecorder-based tests that
// read .Header() instead of .Result() would fail to notice.
func (s *Service) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGoogle)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r)
			return
		}
		writeInternalError(w)
		return
	}

	// The OAuth `state` parameter (RFC 6749 §10.12): compared against
	// what Begin generated for this exact transaction, independent of
	// (and in addition to) the __Host-oauth-tx cookie and PKCE. Without
	// this check, an attacker who gets a victim's browser to visit
	// .../callback?code=<attacker's code>&state=<anything> while the
	// victim holds a legitimate pending transaction cookie could splice
	// their own authorization code into the victim's transaction.
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r)
		return
	}

	provider, err := s.googleProvider(ctx)
	if err != nil {
		writeInternalError(w)
		return
	}

	oauth2Cfg := s.googleOAuth2Config(provider.Endpoint(), s.googleRedirectURL())
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r)
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.google.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r)
		return
	}

	// go-oidc does NOT check nonce automatically (see its own docs on
	// IDToken.Nonce) -- this is an application-level check and an easy
	// one to accidentally skip.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r)
		return
	}

	var claims googleClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r)
		return
	}
	if claims.Email == "" || !claims.EmailVerified {
		s.redirectWithError(w, r, emailNotVerifiedErrorCode)
		return
	}

	usr, err := s.resolveGoogleUser(ctx, idToken.Subject, claims.Email, claims.Name)
	if err != nil {
		writeInternalError(w)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: Issue tolerates an empty ip
	rawSession, _, err := s.sessions.Issue(ctx, usr.ID, r.UserAgent(), clientIP)
	if err != nil {
		writeInternalError(w)
		return
	}

	SetSessionCookie(w, rawSession)
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.publicOrigin+"/", http.StatusFound)
}

// resolveGoogleUser resolves the local user for a validated Google login:
// an existing identity reuses its user; an unknown identity creates a new
// user + identity.
//
// TODO(Task 10): this is Task 4's stub of the shared login-resolution
// algorithm (task-10-brief.md's resolveLoginIdentity): on an unknown
// identity it unconditionally creates a new user, without checking
// whether email already belongs to a different account reached via
// another provider. Task 10 replaces this with the real three-way branch
// (NewUser | ExistingIdentity | EmailCollision) shared by all three
// providers, per design decision 5 and AC-AUTH-001 (no automatic email
// merge across providers).
func (s *Service) resolveGoogleUser(ctx context.Context, providerUserID, email, name string) (store.User, error) {
	identity, err := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(ProviderGoogle),
		ProviderUserID: providerUserID,
	})
	if err == nil {
		usr, getErr := s.q.GetUserByID(ctx, identity.UserID)
		if getErr != nil {
			return store.User{}, fmt.Errorf("auth: resolve google user: get user: %w", getErr)
		}
		return usr, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, fmt.Errorf("auth: resolve google user: get identity: %w", err)
	}

	if name == "" {
		// Google's "name" claim is optional in practice and absent
		// entirely from oidctest's Claims (test infrastructure has no
		// Name field) -- users.name is NOT NULL, so a real value is
		// required; the email is always present here (the caller already
		// rejected an unverified/absent one) and is a reasonable, always-
		// available fallback display name.
		name = email
	}

	usr, err := s.q.CreateUser(ctx, store.CreateUserParams{Email: email, Name: name})
	if err != nil {
		return store.User{}, fmt.Errorf("auth: resolve google user: create user: %w", err)
	}
	if _, err := s.q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         usr.ID,
		Provider:       string(ProviderGoogle),
		ProviderUserID: providerUserID,
	}); err != nil {
		return store.User{}, fmt.Errorf("auth: resolve google user: create identity: %w", err)
	}
	return usr, nil
}

// redirectWithError clears the __Host-oauth-tx cookie (ruling 1) and
// redirects the browser to the app's landing page with ?error=code --
// /callback is a top-level browser navigation, so every rejection is a
// redirect the user actually sees, never a raw JSON error body.
func (s *Service) redirectWithError(w http.ResponseWriter, r *http.Request, code string) {
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.publicOrigin+"/?error="+url.QueryEscape(code), http.StatusFound)
}

// redirectAuthFailed is redirectWithError's shorthand for the generic,
// no-oracle authFailedErrorCode (DD-C3/DD-C4).
func (s *Service) redirectAuthFailed(w http.ResponseWriter, r *http.Request) {
	s.redirectWithError(w, r, authFailedErrorCode)
}

// writeInternalError writes an opaque 500 via the standard api error
// envelope for a failure that is this server's own fault (a database
// error, a wrapped non-sentinel error) rather than a rejected callback --
// integration-owner ruling 2: never leak a wrapped error's text to the
// client.
func writeInternalError(w http.ResponseWriter) {
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

// route mirrors internal/api's own unexported route helper (same 405
// Method Not Allowed + standard error envelope treatment for a mismatched
// method, including HEAD satisfying a GET route). Duplicated here rather
// than imported because api.route is unexported and internal/auth cannot
// reach it; see internal/api/router.go's route for the shared behavior
// this mirrors.
func route(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !methodMatches(method, r.Method) {
			w.Header().Set("Allow", method)
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				fmt.Sprintf("method not allowed on %s; use %s", r.URL.Path, method))
			return
		}
		handler(w, r)
	}
}

// methodMatches reports whether requestMethod satisfies a route
// registered for routeMethod: every method matches itself, and HEAD
// additionally satisfies a GET route (RFC 9110 §9.3.2).
func methodMatches(routeMethod, requestMethod string) bool {
	if requestMethod == routeMethod {
		return true
	}
	return routeMethod == http.MethodGet && requestMethod == http.MethodHead
}
