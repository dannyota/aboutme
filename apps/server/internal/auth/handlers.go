package auth

// handlers.go implements the OAuth login HTTP surface (design spec §3):
// GET /api/v1/auth/{provider}/start and its matching .../callback. See
// transaction.go for this package's own doc comment.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// LinkedInStartPath and LinkedInCallbackPath are the literal routes
// Service registers for "Sign in with LinkedIn" (design spec §3's OAuth
// table), mirroring GoogleStartPath/GoogleCallbackPath's naming.
const (
	LinkedInStartPath    = "/api/v1/auth/linkedin/start"
	LinkedInCallbackPath = "/api/v1/auth/linkedin/callback"
)

// MePath, LogoutPath, and SessionsPath are the literal routes Service
// registers for Task 9's session-authenticated JSON API (design spec §3,
// AC-AUTH-005): GET /me, POST /auth/logout, and GET+DELETE /sessions.
// DELETE /sessions/{id} (per-session revoke) has no exported constant of
// its own -- it is SessionsPath with a "/{id}" wildcard segment appended
// where it is registered (RegisterRoutes) -- callers that need the literal
// URL for a specific session build it themselves (SessionsPath + "/" +
// id.String()).
const (
	MePath       = "/api/v1/me"
	LogoutPath   = "/api/v1/auth/logout"
	SessionsPath = "/api/v1/sessions"
)

// The full, closed vocabulary of ?error= codes a provider callback can
// ever redirect the browser with (fix-round ruling b2). A new distinct
// code is a deliberate, reviewed decision -- never something to add ad
// hoc at a new call site -- because DD-C3's whole point is that a caller
// gets no finer-grained oracle than this list:
//
//   - auth_failed:              DD-C3's generic code for every rejection
//     not explicitly listed below (authFailedErrorCode).
//   - email_not_verified:       design spec §3's Google/LinkedIn rule
//     (emailNotVerifiedErrorCode).
//   - the visitor declined consent at the provider --
//     ?error=access_denied echoed back on the callback, RFC 6749
//     §4.1.2.1 -- gets its own distinct code too: see cancelledErrorCode
//     for the exact (ruling-specified, double-L) wire value.
//   - email_already_registered: Task 10's cross-provider email-collision
//     rejection -- not produced by this file yet; resolveGoogleUser is
//     still Task 4's documented stub.

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

// cancelledErrorCode is the ?error= value for a callback where the
// provider itself reports the visitor declined consent
// (?error=access_denied on the callback query string, RFC 6749
// §4.1.2.1) -- fix-round ruling b2: this reflects the provider's own
// signal rather than an internal classification, leaks nothing about our
// own defenses, and lets the frontend show "you canceled" instead of a
// generic failure message.
const cancelledErrorCode = "cancelled" //nolint:misspell // exact, ruling-specified wire value (double-L "cancelled"), not a typo for "canceled"

// sessionIssuer is the subset of *SessionManager handleGoogleCallback
// needs to issue a session. Extracted as a seam (fix-round Important 1)
// so a test can inject a deterministic failure at this exact point --
// there is no realistic way to force a *SessionManager.Issue failure
// through the real database mid-request -- and prove writeInternalError's
// obligations end to end: __Host-oauth-tx cleared, a generic body with no
// wrapped-error text, and no session cookie set.
type sessionIssuer interface {
	Issue(ctx context.Context, userID uuid.UUID, ua, ip string) (rawToken string, sess store.Session, err error)
}

// Service implements the OAuth login HTTP surface for every provider
// (Google and LinkedIn are wired as of this phase task; GitHub follows
// the same pattern in a later task). It holds the shared OAuth
// transaction and session machinery plus each provider's own client
// credentials.
type Service struct {
	tx       *TransactionStore
	q        *store.Queries
	sessions sessionIssuer
	// sessionMgr is the same *SessionManager instance as sessions, kept as
	// its own concrete-typed field (rather than widening the sessions
	// interface itself) because Task 9's RequireSession middleware and
	// session-management handlers (me.go, sessions_handlers.go) need the
	// SessionManager's full surface -- Authenticate, Revoke, RevokeForUser,
	// RevokeAll -- not just sessionIssuer's single Issue method that
	// SetSessionIssuerForTest is deliberately scoped to fault-inject.
	sessionMgr *SessionManager
	logger     *slog.Logger

	publicOrigin   string
	trustedProxies api.TrustedProxies

	google   googleProviderConfig
	github   githubProviderConfig
	linkedin linkedinProviderConfig

	// googleIssuerOverride replaces the real googleIssuer
	// ("https://accounts.google.com") used for discovery. Empty in
	// production; set only by tests -- see NewServiceForTest -- so the
	// callback exercises exactly the same discovery/verify code path
	// against an in-process oidctest.Provider instead of the real Google
	// endpoint.
	googleIssuerOverride string

	// githubEndpointOverride replaces GitHub's real OAuth2 endpoints
	// (https://github.com/login/oauth/...) and REST API base
	// (https://api.github.com) alike. Empty in production; set only by
	// tests -- see NewServiceForTest (export_test.go), which also
	// defaults this to an unroutable sentinel when the caller passes an
	// empty string -- so a bare *Service can never be pointed at
	// anything but the real network for GitHub, mirroring
	// googleIssuerOverride's own guard.
	githubEndpointOverride string

	// linkedinIssuerOverride is googleIssuerOverride's LinkedIn
	// counterpart, replacing the real linkedinIssuer
	// ("https://www.linkedin.com/oauth").
	linkedinIssuerOverride string
}

// NewService builds a Service backed by q, wiring per-provider OAuth2
// configuration from cfg. logger receives this Service's own operational
// logging (fix-round Important 2) -- error class and request id on every
// rejected or failed callback, matching internal/api's own
// constructor-injected logger style (see api.New); a nil logger is valid
// and simply disables logging (tests that don't care pass one that
// discards output, but nil is accepted too so a caller never needs a
// throwaway).
//
// NewService performs no network I/O of its own (e.g. no OIDC discovery):
// each provider's discovery document is fetched lazily, and cached, the
// first time a request actually needs it (see (*Service).googleProvider)
// -- a server with no login traffic yet, or a dev environment with empty
// GOOGLE_CLIENT_ID, never blocks startup on it.
//
// NewService does not hold an internal/user.Store: that package's own
// constructor (user.New) takes a store.DBTX (pool/connection) and builds
// its own internal *store.Queries, which cannot be recovered from the
// *store.Queries this constructor is pinned to receive. Service instead
// calls q's CreateUser/GetUserByEmail/GetIdentityByProviderSubject/
// CreateIdentity methods directly -- the same store.Queries surface
// user.Store itself wraps, with equivalent pgx.ErrNoRows handling done
// inline (see resolveGoogleUser).
func NewService(logger *slog.Logger, cfg config.Config, q *store.Queries) (*Service, error) {
	if cfg.PublicOrigin == "" {
		return nil, fmt.Errorf("auth: NewService: config.PublicOrigin is required")
	}
	sessionMgr := NewSessionManager(q)
	return &Service{
		tx:             NewTransactionStore(q),
		q:              q,
		sessions:       sessionMgr,
		sessionMgr:     sessionMgr,
		logger:         logger,
		publicOrigin:   cfg.PublicOrigin,
		trustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
		google: googleProviderConfig{
			clientID:     cfg.GoogleClientID,
			clientSecret: cfg.GoogleClientSecret,
		},
		github: githubProviderConfig{
			clientID:     cfg.GitHubClientID,
			clientSecret: cfg.GitHubClientSecret,
		},
		linkedin: linkedinProviderConfig{
			clientID:     cfg.LinkedInClientID,
			clientSecret: cfg.LinkedInClientSecret,
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
	mux.Handle(GitHubStartPath, route(http.MethodGet, s.handleGitHubStart))
	mux.Handle(GitHubCallbackPath, route(http.MethodGet, s.handleGitHubCallback))
	mux.Handle(LinkedInStartPath, route(http.MethodGet, s.handleLinkedInStart))
	mux.Handle(LinkedInCallbackPath, route(http.MethodGet, s.handleLinkedInCallback))

	// Session-authenticated JSON API (Task 9, AC-AUTH-005). Every route
	// below is wired through sessionChain, so it always runs the same two
	// layers, in order, before the handler: RequireSession, then
	// RequireCSRF. Combined with api.New's own outer chain (applied
	// identically to every route RegisterRoutes adds -- see router.go's
	// New), the FULL order every request here passes through is (fix
	// round 1, M1: the previous version of this comment omitted
	// SecurityHeaders, NoStoreCache, and RateLimit):
	//
	//	RequestID -> SecurityHeaders -> Logging -> NoStoreCache ->
	//	RateLimit -> BodyLimit -> RequireSession -> RequireCSRF -> handler
	//
	// RequireCSRF only enforces on mutating methods (GET/HEAD/OPTIONS pass
	// through untouched -- csrf.go's isMutatingMethod), so applying it
	// unconditionally via sessionChain, even to GET /me and GET /sessions,
	// costs nothing on those routes and keeps every protected route behind
	// the identical two-layer stack rather than hand-picking per route.
	mux.Handle(MePath, route(http.MethodGet, s.sessionChain(s.handleMe)))
	mux.Handle(LogoutPath, route(http.MethodPost, s.sessionChain(s.handleLogout)))
	// GET (device list) and DELETE (logout-everywhere) share one path, so
	// they can't both go through route()'s single-method wrapper the way
	// every other route here does; handleSessionsCollection does its own
	// method dispatch, checking the method BEFORE either branch's
	// sessionChain runs -- an unsupported method never reaches
	// RequireSession, so it always cleanly 405s regardless of the caller's
	// auth state, matching route()'s own method-before-side-effect
	// ordering.
	mux.Handle(SessionsPath, http.HandlerFunc(s.handleSessionsCollection))
	mux.Handle(SessionsPath+"/{id}", route(http.MethodDelete, s.sessionChain(s.handleRevokeSession)))
}

// The closed vocabulary of JSON API error codes this package ITSELF
// introduces for its session-authenticated surface (distinct from the
// OAuth callback's own ?error= redirect vocabulary above, which is a
// different mechanism entirely -- a query parameter on a 302, never a
// JSON body). Deliberately scoped to codes this package defines, not an
// exhaustive list of every code a response from this surface can ever
// carry: internal_error, method_not_allowed, body_too_large, and other
// generic infrastructure codes are internal/api's own (api.WriteError
// call sites in this package reuse them verbatim, e.g.
// writeSessionAPIInternalError/handleSessionsCollection's 405) and are
// documented at their own definition, not repeated here. A new distinct
// code IN THIS LIST is a deliberate, reviewed decision, not something to
// invent ad hoc at a new call site:
//
//   - session_required: RequireSession's own single rejection code (401)
//     -- adjudicated 2026-08-02 (fix round 1) as THE canonical code for
//     "no valid session," to be reused verbatim by every later
//     session-authenticated endpoint this package or Task 10 adds
//     (provider link/unlink, account deletion, email change, slug
//     release, ...), never reinvented under a different literal.
//   - csrf_rejected: RequireCSRF's own single rejection code (403,
//     csrf.go's csrfRejectedCode) -- established at Task 8.
//   - reauth_required: RequireRecentReauth's own rejection code (403,
//     reauthRequiredCode below) -- shared with the phase's other
//     sensitive-operation flows (provider link/reauth).
//   - not_found: DELETE /sessions/{id}'s uniform, no-oracle 404
//     (sessions_handlers.go's notFoundCode).

// sessionRequiredCode is the single error code every RequireSession
// rejection returns: no __Host-session cookie at all, and a cookie that
// fails to authenticate (unknown, revoked, idle/absolute-expired, or an
// old token past its rotation grace window) are indistinguishable from the
// response alone -- the same no-oracle reasoning ErrSessionInvalid itself
// already collapses those five cases behind.
const sessionRequiredCode = "session_required"

// reauthRequiredCode is the error code Task 9's two recent-reauth-gated
// endpoints (DELETE /sessions/{id}, DELETE /sessions) return when
// RequireRecentReauth rejects the caller's session -- the same wire value
// the phase's other sensitive-operation flows (provider link/reauth) use,
// so a shared frontend prompt can key off one literal regardless of which
// endpoint produced it.
const reauthRequiredCode = "reauth_required"

// RequireSession wraps a handler that requires an authenticated session
// (Task 9; the counterpart to Task 8's RequireCSRF, which documents this
// exact ordering requirement): it reads the __Host-session cookie,
// authenticates it via m, and -- on success -- stores the governing
// session in the request context (ContextWithSession) before calling
// next, so next (and, per the documented chain, RequireCSRF running
// between this and next) can read it back via SessionFromContext.
//
// If Authenticate rotated the session (task-7-brief.md's >24h rotation:
// rotatedToken is non-empty), the new session cookie is set on the
// response before next runs, so this request's own response already
// carries the caller's next credential -- no extra round trip.
//
// On ErrSessionInvalid -- no cookie, or one that fails to authenticate for
// any reason -- responds 401 with sessionRequiredCode and clears the
// __Host-session cookie (ClearSessionCookie), so a browser holding a
// dead/invalid token doesn't keep resending it forever. Any OTHER error
// (e.g. the database itself is unreachable) is a genuine internal failure,
// not a verdict on the credential's own validity, and gets the standard
// opaque 500 instead -- it must never be conflated with "session invalid"
// or clear a cookie that might still be perfectly good once the outage
// clears.
//
// Ordering (RegisterRoutes' sessionChain, matching csrf.go's own
// documented chain): RequestID -> SecurityHeaders -> Logging ->
// NoStoreCache -> RateLimit -> BodyLimit -> RequireSession -> RequireCSRF
// -> handler (fix round 1, M1: the previous version of this comment
// omitted SecurityHeaders, NoStoreCache, and RateLimit -- see router.go's
// New for the authoritative composition this mirrors). RequireCSRF
// depends on RequireSession having already populated the session in
// context.
func RequireSession(m *SessionManager) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				rejectSession(w)
				return
			}

			sess, rotated, err := m.Authenticate(r.Context(), cookie.Value)
			if err != nil {
				if errors.Is(err, ErrSessionInvalid) {
					rejectSession(w)
					return
				}
				api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
				return
			}

			if rotated != "" {
				SetSessionCookie(w, rotated)
			}

			next.ServeHTTP(w, r.WithContext(ContextWithSession(r.Context(), sess)))
		})
	}
}

// rejectSession writes RequireSession's single 401 rejection response and
// clears the __Host-session cookie -- factored out so both of
// RequireSession's own call sites and every Task 9 handler's
// defense-in-depth "session missing from context" branch (which should
// never be reachable in production wiring, but must still fail closed
// rather than panic if it ever were) produce the exact same response.
func rejectSession(w http.ResponseWriter) {
	ClearSessionCookie(w)
	api.WriteError(w, http.StatusUnauthorized, sessionRequiredCode, "a valid session is required")
}

// sessionChain wraps handler with the two layers every one of Task 9's
// session-authenticated routes needs, in the documented order:
// RequireSession, then RequireCSRF. See RegisterRoutes' own comment for
// why applying RequireCSRF unconditionally (even to a read-only route) is
// safe and deliberate.
func (s *Service) sessionChain(handler http.HandlerFunc) http.HandlerFunc {
	wrapped := RequireSession(s.sessionMgr)(RequireCSRF(s.publicOrigin)(handler))
	return wrapped.ServeHTTP
}

// writeSessionAPIInternalError writes the standard opaque 500 for one of
// Task 9's session-authenticated JSON endpoints and logs the failing
// operation server-side -- me.go/sessions_handlers.go's counterpart to
// this file's own writeInternalError, minus that funnel's OAuth-specific
// concerns (no provider to log, no __Host-oauth-tx cookie to clear: these
// routes never set one). Like logInternalError below, this logs err's
// SQLSTATE class code when it is a *pgconn.PgError, and ONLY that --
// deliberately never err's own message/detail text, which for a real
// database error can embed a bound parameter value (e.g. an email
// address).
func (s *Service) writeSessionAPIInternalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	if s.logger != nil {
		attrs := []any{
			"request_id", api.RequestIDFromContext(r.Context()),
			"op", op,
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			attrs = append(attrs, "sqlstate", pgErr.Code)
		}
		s.logger.ErrorContext(r.Context(), "auth: session api internal error", attrs...)
	}
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

// handleGoogleStart begins a Google login transaction and redirects the
// browser to Google's own authorize endpoint, with PKCE (S256) and an
// OIDC nonce bound to the transaction.
func (s *Service) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	provider, err := s.googleProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "google_provider_discovery", err)
		return
	}

	redirectURI := s.googleRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderGoogle, PurposeLogin, uuid.Nil, redirectURI)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "begin_transaction", err)
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
// rejection funnels through redirectWithError/redirectAuthFailed and
// every internal failure funnels through writeInternalError, both of
// which call it before writing their response, and the success path
// calls it explicitly in the same place. It is deliberately NOT a
// deferred call at the top of this function: a deferred ClearOAuthTxCookie
// would run after the response's WriteHeader has already been called on
// every exit path, and a Set-Cookie header added after WriteHeader has
// been called never reaches a real client (net/http headers must be set
// before the first WriteHeader/Write) -- only ResponseRecorder-based
// tests that read .Header() instead of .Result() would fail to notice.
func (s *Service) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	handle, err := ReadOAuthTxCookie(r)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, "missing or malformed __Host-oauth-tx cookie")
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGoogle)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderGoogle, "oauth transaction invalid (unknown, expired, replayed, or wrong provider)")
			return
		}
		s.writeInternalError(w, r, ProviderGoogle, "consume_transaction", err)
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
		s.redirectAuthFailed(w, r, ProviderGoogle, "state parameter mismatch")
		return
	}

	// Ruling b2: the provider's own signal that the visitor declined
	// consent (RFC 6749 §4.1.2.1's error=access_denied) is checked only
	// after state has already been validated above, so a forged
	// access_denied carrying the wrong state still gets the generic
	// rejection, never this friendlier one.
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderGoogle, cancelledErrorCode, "user denied consent (access_denied)")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderGoogle, "callback missing code and no recognized error parameter")
		return
	}

	provider, err := s.googleProvider(ctx)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "google_provider_discovery", err)
		return
	}

	// tx.RedirectURI (not s.googleRedirectURL()): the exact redirect_uri
	// Begin stored for THIS transaction, not one rebuilt from this
	// Service's current PublicOrigin config -- see provider_cache.go's
	// sibling hardening note and TestGoogleCallback_UsesStoredRedirectURI_
	// NotCurrentPublicOrigin (google_test.go). A real provider enforces
	// redirect_uri as an exact match against what it issued the
	// authorization code for; rebuilding it from current config would
	// desync the two if PUBLIC_ORIGIN changes between /start and
	// /callback (the oauth_transactions.redirect_uri column would
	// otherwise be a vestigial, never-read field).
	oauth2Cfg := s.googleOAuth2Config(provider.Endpoint(), tx.RedirectURI)
	token, err := oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(tx.PKCEVerifier))
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, "token exchange failed")
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderGoogle, "token response missing id_token")
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.google.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, "id_token verification failed (issuer, audience, signature, or expiry)")
		return
	}

	// go-oidc does NOT check nonce automatically (see its own docs on
	// IDToken.Nonce) -- this is an application-level check and an easy
	// one to accidentally skip.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderGoogle, "nonce mismatch")
		return
	}

	var claims googleClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, "id_token claims decode failed")
		return
	}
	if claims.Email == "" || !claims.EmailVerified {
		s.redirectWithError(w, r, ProviderGoogle, emailNotVerifiedErrorCode, "email absent or email_verified != true")
		return
	}

	usr, err := s.resolveGoogleUser(ctx, idToken.Subject, claims.Email, claims.Name)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "resolve_user", err)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: Issue tolerates an empty ip
	rawSession, _, err := s.sessions.Issue(ctx, usr.ID, r.UserAgent(), clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "issue_session", err)
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
		// Google's "name" claim is requested (googleScopes includes
		// oidc.ScopeProfile) but still not guaranteed -- the consent
		// screen or the account itself can omit it -- and oidctest's
		// Claims has no Name field at all (test infrastructure), so this
		// fallback is exercised by every test. users.name is NOT NULL, so
		// a real value is required; email is always present here (the
		// caller already rejected an unverified/absent one). Fix-round
		// ruling b1: fall back to the LOCAL PART of the email (before
		// "@"), never the full address -- a later phase renders this
		// value as a display name, and the full email would leak the
		// visitor's address to anyone who can see it (their own resume
		// page, an account-settings view shared with others, etc.).
		name = emailLocalPart(email)
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

// emailLocalPart returns the portion of email before "@", or email
// unchanged if it contains no "@" at all (defensive; email is already
// validated non-empty by every caller). See resolveGoogleUser's
// display-name fallback.
func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}

// redirectWithError clears the __Host-oauth-tx cookie (ruling 1), logs
// the rejection server-side (fix-round Important 2), and redirects the
// browser to the app's login page with ?error=code (DD-C7,
// integration-owner ruling: distinct from the bare "/" a SUCCESSFUL
// callback redirects to below -- a rejection's ?error= code is meant to
// render on the login screen, not the app's post-login landing page) --
// /callback is a top-level browser navigation, so every rejection is a
// redirect the user actually sees, never a raw JSON error body. provider
// identifies which provider's callback this is (see logRejection) -- it
// is never echoed to the client, only logged.
func (s *Service) redirectWithError(w http.ResponseWriter, r *http.Request, provider Provider, code, reason string) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, code, reason)
	http.Redirect(w, r, s.publicOrigin+"/login?error="+url.QueryEscape(code), http.StatusFound)
}

// redirectAuthFailed is redirectWithError's shorthand for the generic,
// no-oracle authFailedErrorCode (DD-C3/DD-C4). reason is an operator-
// facing detail logged server-side only (see logRejection) -- it never
// reaches the client, which always sees the same generic
// authFailedErrorCode regardless of reason's value.
func (s *Service) redirectAuthFailed(w http.ResponseWriter, r *http.Request, provider Provider, reason string) {
	s.redirectWithError(w, r, provider, authFailedErrorCode, reason)
}

// writeInternalError clears the __Host-oauth-tx cookie (ruling 1: applies
// to every exit path, including this server's own internal failures, not
// only a rejected callback), logs the failing operation server-side
// (fix-round Important 2) -- including err's SQLSTATE class code when it
// is a *pgconn.PgError (see logInternalError) -- and writes an opaque 500
// via the standard api error envelope -- integration-owner ruling 2:
// never leak a wrapped error's text to the client. Clearing the cookie
// here is a safe no-op when handleGoogleStart's own two internal-error
// paths call this before any tx cookie for the current attempt has been
// set at all; it still tidies up a stale cookie left over from an
// earlier, abandoned attempt.
func (s *Service) writeInternalError(w http.ResponseWriter, r *http.Request, provider Provider, op string, err error) {
	ClearOAuthTxCookie(w)
	s.logInternalError(r, provider, op, err)
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

// logRejection logs a rejected /callback attempt at Warn level: which
// provider it was for (this funnel is shared by every provider Service
// registers, not just Google -- a provider-neutral message plus this
// attribute is what still lets an operator filter or correlate by
// provider in a shared log stream), the outward error code (never more
// specific than what the browser itself already sees -- DD-C3 already
// permits the client to know this much), a short, fixed reason string
// this package writes itself at each call site (never user- or
// provider-supplied data: no OAuth authorization code, no token, no
// email, no raw state/nonce value), and the request id, so an operator
// can correlate a specific rejected request in the access log with why
// it was rejected. A nil logger (see NewService) is a silent no-op.
func (s *Service) logRejection(r *http.Request, provider Provider, code, reason string) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(r.Context(), "auth: callback rejected",
		"request_id", api.RequestIDFromContext(r.Context()),
		"provider", provider,
		"error_code", code,
		"reason", reason,
	)
}

// logInternalError logs this server's own failure (never the browser's
// fault) at Error level: which provider it was for (see logRejection's
// same reasoning), which operation failed (op, a short fixed string),
// the request id, and -- when err is a *pgconn.PgError -- its SQLSTATE
// class code (pgconn.PgError.Code, e.g. "23505" for unique_violation)
// ONLY. Deliberately NOT err's own message/detail text (unlike a plain
// %w wrap, a database constraint-violation message can itself embed a
// bound parameter value, e.g. Postgres's own "Key (email)=(...) already
// exists" detail on users_email_key; logging that verbatim would leak an
// email address into the server log, which fix-round Important 2
// explicitly forbids) -- the SQLSTATE class code alone is enough for an
// operator to distinguish, say, a constraint violation from a connection
// failure, without risking that leak. A nil logger is a silent no-op.
func (s *Service) logInternalError(r *http.Request, provider Provider, op string, err error) {
	if s.logger == nil {
		return
	}
	attrs := []any{
		"request_id", api.RequestIDFromContext(r.Context()),
		"provider", provider,
		"op", op,
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		attrs = append(attrs, "sqlstate", pgErr.Code)
	}
	s.logger.ErrorContext(r.Context(), "auth: callback internal error", attrs...)
}

// route restricts handler to the given HTTP method, responding 405 Method
// Not Allowed with the standard error envelope (and an Allow header) for
// any other method on the same registered path. Deliberately NOT
// internal/api's own route helper (which this package cannot reach --
// api.route is unexported -- and would not want here regardless, see
// methodMatches below): every /start and /callback route this package
// registers has a real server-side side effect on every request that
// reaches its handler (TransactionStore.Begin's database INSERT, or
// Consume's atomic single-use claim), so this is deliberately stricter
// than internal/api/router.go's route, not merely a copy of it.
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
// registered for routeMethod: exact match only (DD-C8, integration-owner
// ruling). This is a deliberate divergence from internal/api's own
// route/methodMatches (and from Go's stdlib ServeMux "GET /pattern"
// registration syntax), both of which let HEAD satisfy a registered GET
// route per RFC 9110 §9.3.2 ("HEAD is identical to GET but without a
// body") -- the right default for an idempotent, side-effect-free GET,
// but wrong here: handleGoogleStart/handleLinkedInStart's GET has a real
// side effect (TransactionStore.Begin's database INSERT and a real
// __Host-oauth-tx Set-Cookie) on every request that reaches it, and
// handleGoogleCallback/handleLinkedInCallback's GET atomically consumes a
// single-use transaction. A HEAD request is exactly the shape a
// link-preview/prefetch crawler sends without ever intending to complete
// the flow -- letting it fall through to either handler would mean a
// prefetcher hitting every link on a page burns a real transaction (and
// sets a real cookie) for a request the visitor never made.
func methodMatches(routeMethod, requestMethod string) bool {
	return requestMethod == routeMethod
}
