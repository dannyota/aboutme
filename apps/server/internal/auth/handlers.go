package auth

// OAuth callbacks share the no-oracle and identity rules in
// docs/design/security.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// GoogleStartPath and GoogleCallbackPath are the registered Google routes.
const (
	GoogleStartPath    = "/api/v1/auth/google/start"
	GoogleCallbackPath = "/api/v1/auth/google/callback"
)

// LinkedInStartPath and LinkedInCallbackPath are the registered LinkedIn routes.
const (
	LinkedInStartPath    = "/api/v1/auth/linkedin/start"
	LinkedInCallbackPath = "/api/v1/auth/linkedin/callback"
)

// MePath, LogoutPath, and SessionsPath are the session API routes.
const (
	MePath       = "/api/v1/me"
	LogoutPath   = "/api/v1/auth/logout"
	SessionsPath = "/api/v1/sessions"
)

// Callback error codes are closed. Security-sensitive failures collapse to
// auth_failed; user-actionable outcomes remain distinct. See
// docs/design/security.md.

// authFailedErrorCode collapses security-sensitive callback failures.
const authFailedErrorCode = "auth_failed"

// emailNotVerifiedErrorCode identifies a failed registration email check.
const emailNotVerifiedErrorCode = "email_not_verified"

// cancelledErrorCode identifies provider-reported consent denial.
const cancelledErrorCode = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".

// emailAlreadyRegisteredErrorCode identifies a verified-email collision.
const emailAlreadyRegisteredErrorCode = "email_already_registered"

// identityAlreadyLinkedErrorCode identifies an identity owned by another user.
const identityAlreadyLinkedErrorCode = "identity_already_linked"

// pgUniqueViolationCode is PostgreSQL's unique-violation SQLSTATE.
const pgUniqueViolationCode = "23505"

// isUniqueViolation matches only PostgreSQL unique violations.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode
}

// settingsSessionsPath is the success and error target for privileged callbacks.
const settingsSessionsPath = "/app/settings/sessions"

// sessionIssuer is the callback's injectable session-issuance seam. It exposes
// the transaction-scoped primitive only: the caller already holds the user row
// lock in the same transaction.
type sessionIssuer interface {
	IssueTx(ctx context.Context, qtx *store.Queries, user store.User, ua, ip string) (SessionIssue, error)
}

// Service implements the OAuth login HTTP surface for Google, GitHub, and
// LinkedIn. It holds shared transaction and session machinery plus each
// provider's credentials.
type Service struct {
	tx   *TransactionStore
	q    *store.Queries
	pool *store.Pool
	// sessions is the injectable issuance seam; sessionMgr supplies the full
	// session surface so tests can inject failures without replacing
	// authentication.
	sessions   sessionIssuer
	sessionMgr *SessionManager
	logger     *slog.Logger

	publicOrigin            string
	trustedProxies          api.TrustedProxies
	googleIssuerURL         string
	linkedinIssuerURL       string
	githubOAuthAuthorizeURL string
	githubOAuthTokenURL     string
	githubAPIBaseURL        string
	googleLocalOIDC         bool
	linkedinLocalOIDC       bool
	githubLocalOAuth        bool

	// These fields let tests reduce the shared start-route budget.
	startRateLimitRequests int
	startRateLimitWindow   time.Duration

	google   googleProviderConfig
	github   githubProviderConfig
	linkedin linkedinProviderConfig

	// Provider overrides are empty in production and route test traffic to
	// in-process providers through the production verification paths.
	googleIssuerOverride string

	// GitHub uses one override for both OAuth and API endpoints.
	githubEndpointOverride string

	linkedinIssuerOverride string
}

// NewService builds a Service without network I/O. Provider discovery is lazy
// and cached. A nil logger disables auth logging. pool may be nil for callers
// that only exercise route registration or provider discovery, never the login
// callback; every callback requires it to open the D4 user-lock transaction.
func NewService(logger *slog.Logger, cfg config.Config, pool *store.Pool) (*Service, error) {
	if cfg.PublicOrigin == "" {
		return nil, fmt.Errorf("auth: NewService: config.PublicOrigin is required")
	}
	q := store.New(pool)
	sessionMgr := NewSessionManagerWithPool(pool)
	return &Service{
		tx:                      NewTransactionStore(q),
		q:                       q,
		pool:                    pool,
		sessions:                sessionMgr,
		sessionMgr:              sessionMgr,
		logger:                  logger,
		publicOrigin:            cfg.PublicOrigin,
		trustedProxies:          api.TrustedProxies(cfg.TrustedProxyCIDRs),
		googleIssuerURL:         endpointOrDefault(cfg.GoogleOIDCIssuerURL, googleIssuer),
		linkedinIssuerURL:       endpointOrDefault(cfg.LinkedInOIDCIssuerURL, linkedinIssuer),
		githubOAuthAuthorizeURL: endpointOrDefault(cfg.GitHubOAuthAuthorizeURL, githubAuthorizeURL),
		githubOAuthTokenURL:     endpointOrDefault(cfg.GitHubOAuthTokenURL, githubTokenURL),
		githubAPIBaseURL:        endpointOrDefault(cfg.GitHubAPIBaseURL, githubAPIBaseURL),
		googleLocalOIDC:         cfg.GoogleOIDCIssuerURL != "",
		linkedinLocalOIDC:       cfg.LinkedInOIDCIssuerURL != "",
		githubLocalOAuth:        cfg.GitHubOAuthAuthorizeURL != "",

		startRateLimitRequests: startRateLimitRequests,
		startRateLimitWindow:   startRateLimitWindow,
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

func endpointOrDefault(configured, fallback string) string {
	if configured != "" {
		return configured
	}
	return fallback
}

// RegisterRoutes attaches the authentication and session routes to mux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	// All providers share one bounded start-route limiter.
	startLimit := s.startRateLimit()
	mux.Handle(GoogleStartPath, s.startRoute(ProviderGoogle, s.buildGoogleAuthorizeURL, startLimit))
	mux.Handle(GoogleCallbackPath, route(http.MethodGet, s.handleGoogleCallback))
	mux.Handle(GitHubStartPath, s.startRoute(ProviderGitHub, s.buildGitHubAuthorizeURL, startLimit))
	mux.Handle(GitHubCallbackPath, route(http.MethodGet, s.handleGitHubCallback))
	mux.Handle(LinkedInStartPath, s.startRoute(ProviderLinkedIn, s.buildLinkedInAuthorizeURL, startLimit))
	mux.Handle(LinkedInCallbackPath, route(http.MethodGet, s.handleLinkedInCallback))

	// Session routes authenticate before CSRF enforcement.
	mux.Handle(MePath, route(http.MethodGet, s.sessionChain(s.handleMe)))
	mux.Handle(LogoutPath, route(http.MethodPost, s.sessionChain(s.handleLogout)))
	// Collection method dispatch runs before authentication, matching route.
	mux.Handle(SessionsPath, http.HandlerFunc(s.handleSessionsCollection))
	mux.Handle(SessionsPath+"/{id}", route(http.MethodDelete, s.sessionChain(s.handleRevokeSession)))
}

// sessionRequiredCode collapses every invalid-session cause.
const sessionRequiredCode = "session_required"

// reauthRequiredCode is shared by all recent-reauth gates.
const reauthRequiredCode = "reauth_required"

// RequireSession authenticates the session, delivers any rotated cookie, and
// stores the session and account ID in context. Invalid credentials return the
// same 401 and clear the cookie; internal failures return an opaque 500 without
// clearing it. RequireCSRF must run after this middleware.
func RequireSession(m *SessionManager) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, rotated, err := readAndAuthenticateSession(r, m)
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

			// Account context lets inner limiters key by account and IP.
			ctx := ContextWithSession(r.Context(), sess)
			ctx = api.WithAccountID(ctx, sess.UserID.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// readAndAuthenticateSession lets public callbacks reuse session
// authentication after learning the transaction purpose. It returns
// ErrSessionInvalid unchanged so callers choose JSON or redirect handling.
func readAndAuthenticateSession(r *http.Request, m *SessionManager) (sess store.Session, rotated string, err error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return store.Session{}, "", ErrSessionInvalid
	}
	return m.Authenticate(r.Context(), cookie.Value)
}

// rejectSession writes the uniform 401 response and clears the session cookie.
func rejectSession(w http.ResponseWriter) {
	ClearSessionCookie(w)
	api.WriteError(w, http.StatusUnauthorized, sessionRequiredCode, "a valid session is required")
}

// sessionChain authenticates before CSRF enforcement.
func (s *Service) sessionChain(handler http.HandlerFunc) http.HandlerFunc {
	wrapped := RequireSession(s.sessionMgr)(RequireCSRF(s.publicOrigin)(handler))
	return wrapped.ServeHTTP
}

// writeSessionAPIInternalError logs only fixed fields and an optional SQLSTATE,
// never database error text, then writes an opaque 500.
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

// buildGoogleAuthorizeURL binds PKCE S256 and an OIDC nonce to the stored
// transaction. start.go owns the GET-versus-POST response policy.
func (s *Service) buildGoogleAuthorizeURL(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID) (handle, authURL, op string, err error) {
	provider, err := s.googleProvider(ctx)
	if err != nil {
		return "", "", "google_provider_discovery", err
	}

	redirectURI := s.googleRedirectURL()
	handle, tx, err := s.tx.Begin(ctx, ProviderGoogle, purpose, linkingUserID, redirectURI)
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
		s.redirectAuthFailed(w, r, ProviderGoogle, PurposeLogin, reasonTxCookieMissing)
		return
	}

	tx, err := s.tx.Consume(ctx, handle, ProviderGoogle)
	if err != nil {
		if errors.Is(err, ErrTransactionInvalid) {
			s.redirectAuthFailed(w, r, ProviderGoogle, PurposeLogin, reasonTxInvalid)
			return
		}
		s.writeInternalError(w, r, ProviderGoogle, "consume_transaction", err)
		return
	}

	// State prevents an attacker from splicing their authorization code into
	// a victim's pending transaction; the cookie and PKCE do not replace it.
	state := r.URL.Query().Get("state")
	if state == "" || state != tx.State {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonStateMismatch)
		return
	}

	// Validate state before exposing the friendlier consent-denied result.
	if r.URL.Query().Get("error") == "access_denied" {
		s.redirectWithError(w, r, ProviderGoogle, tx.Purpose, cancelledErrorCode, reasonConsentDenied)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonAuthorizationCodeMissing)
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
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonTokenExchangeFailed)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonIDTokenMissing)
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: s.google.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonIDTokenVerificationFailed)
		return
	}

	// go-oidc exposes the nonce but does not validate it.
	if idToken.Nonce == "" || idToken.Nonce != tx.Nonce {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonNonceMismatch)
		return
	}

	var claims googleClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		s.redirectAuthFailed(w, r, ProviderGoogle, tx.Purpose, reasonIDTokenClaimsDecodeFailed)
		return
	}

	// Link and reauth use provider identity without an email check.
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		if linkErr := s.resolveLinkOrReauth(ctx, r, w, tx, ProviderGoogle, idToken.Subject); linkErr != nil {
			s.redirectLinkOrReauthError(w, r, ProviderGoogle, tx.Purpose, linkErr)
			return
		}
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
		return
	}

	clientIP, _ := api.ClientIP(r, s.trustedProxies) // best-effort: IssueTx tolerates an empty ip
	ua := r.UserAgent()

	rawSession, found, err := s.resolveProviderLogin(ctx, ProviderSubject{
		Provider: ProviderGoogle,
		Subject:  idToken.Subject,
	}, ua, clientIP)
	if err != nil {
		s.writeInternalError(w, r, ProviderGoogle, "resolve_provider_login", err)
		return
	}
	if found {
		SetSessionCookie(w, rawSession)
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
		return
	}

	// A new subject requires a verified, canonical email.
	if claims.Email == "" || !claims.EmailVerified {
		s.redirectWithError(w, r, ProviderGoogle, tx.Purpose, emailNotVerifiedErrorCode, reasonEmailNotVerified)
		return
	}
	canonicalEmail, err := accountemail.Canonicalize(claims.Email)
	if err != nil {
		s.redirectWithError(w, r, ProviderGoogle, tx.Purpose, emailNotVerifiedErrorCode, reasonEmailNotVerified)
		return
	}

	rawSession, err = s.createProviderLogin(ctx, NewProviderAccount{
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

	SetSessionCookie(w, rawSession)
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.callbackSuccessRedirect(tx.Purpose), http.StatusFound)
}

// emailLocalPart returns the text before "@", or the input when absent.
func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}

// redirectWithError clears the transaction cookie, logs the rejection, and
// redirects to the purpose-specific error page. Unknown-purpose failures use
// the login page.
func (s *Service) redirectWithError(w http.ResponseWriter, r *http.Request, provider Provider, purpose Purpose, code string, reason rejectReason) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, code, reason)
	http.Redirect(w, r, s.callbackErrorRedirectBase(purpose)+"?error="+url.QueryEscape(code), http.StatusFound)
}

// redirectAuthFailed exposes one code while retaining a typed, server-only
// reason for operators.
func (s *Service) redirectAuthFailed(w http.ResponseWriter, r *http.Request, provider Provider, purpose Purpose, reason rejectReason) {
	s.redirectWithError(w, r, provider, purpose, authFailedErrorCode, reason)
}

// redirectEmailAlreadyRegistered names only the attempted provider, never the
// provider already attached to the account. See docs/design/security.md.
func (s *Service) redirectEmailAlreadyRegistered(w http.ResponseWriter, r *http.Request, provider Provider) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, emailAlreadyRegisteredErrorCode, reasonEmailAlreadyRegistered)
	target := s.publicOrigin + "/login?error=" + url.QueryEscape(emailAlreadyRegisteredErrorCode) +
		"&provider=" + url.QueryEscape(string(provider))
	http.Redirect(w, r, target, http.StatusFound)
}

// callbackErrorRedirectBase returns the PublicOrigin-prefixed page a
// callback rejection redirects to before ?error=code is appended.
func (s *Service) callbackErrorRedirectBase(purpose Purpose) string {
	if purpose == PurposeLink || purpose == PurposeReauth {
		return s.publicOrigin + settingsSessionsPath
	}
	return s.publicOrigin + "/login"
}

// callbackSuccessRedirect selects the purpose-specific success page.
func (s *Service) callbackSuccessRedirect(purpose Purpose) string {
	if purpose == PurposeLink || purpose == PurposeReauth {
		return s.publicOrigin + settingsSessionsPath
	}
	return s.publicOrigin + "/"
}

// writeInternalError clears any transaction cookie, logs fixed failure data,
// and writes an opaque 500.
func (s *Service) writeInternalError(w http.ResponseWriter, r *http.Request, provider Provider, op string, err error) {
	ClearOAuthTxCookie(w)
	s.logInternalError(r, provider, op, err)
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

// logRejection logs only fixed vocabulary and request metadata, never OAuth or
// identity values. String converts the integer-backed reason to its stable token.
func (s *Service) logRejection(r *http.Request, provider Provider, code string, reason rejectReason) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(r.Context(), "auth: callback rejected",
		"request_id", api.RequestIDFromContext(r.Context()),
		"provider", provider,
		"error_code", code,
		"reason", reason.String(),
	)
}

// logInternalError records fixed metadata and an optional SQLSTATE. Database
// error text may contain bound values and must never be logged.
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

// route rejects every method except the exact configured method before the
// side-effecting handler runs.
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

// methodMatches is stricter than ServeMux's GET matching: HEAD must not start
// or consume an OAuth transaction. This prevents preview and prefetch requests
// from creating or burning credentials.
func methodMatches(routeMethod, requestMethod string) bool {
	return requestMethod == routeMethod
}
