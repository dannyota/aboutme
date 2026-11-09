package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

// RegisterRoutes attaches the authentication and session routes to mux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	// A disabled provider's paths stay unregistered and return the uniform
	// not-found response.
	if s.providerLogin.Any() {
		// All enabled providers share one bounded start-route limiter.
		startLimit := s.startRateLimit()
		if s.providerLogin.Google {
			mux.Handle(GoogleStartPath, s.startRoute(ProviderGoogle, s.buildGoogleAuthorizeURL, startLimit))
			mux.Handle(GoogleCallbackPath, route(http.MethodGet, s.handleGoogleCallback))
		}
		if s.providerLogin.GitHub {
			mux.Handle(GitHubStartPath, s.startRoute(ProviderGitHub, s.buildGitHubAuthorizeURL, startLimit))
			mux.Handle(GitHubCallbackPath, route(http.MethodGet, s.handleGitHubCallback))
		}
		if s.providerLogin.LinkedIn {
			mux.Handle(LinkedInStartPath, s.startRoute(ProviderLinkedIn, s.buildLinkedInAuthorizeURL, startLimit))
			mux.Handle(LinkedInCallbackPath, route(http.MethodGet, s.handleLinkedInCallback))
		}
	}

	// Session routes authenticate before CSRF enforcement.
	mux.Handle(MePath, http.HandlerFunc(s.handleAccount))
	mux.Handle(LogoutPath, route(http.MethodPost, s.sessionChain(s.handleLogout)))
	// Collection method dispatch runs before authentication, matching route.
	mux.Handle(SessionsPath, http.HandlerFunc(s.handleSessionsCollection))
	mux.Handle(SessionsPath+"/{id}", route(http.MethodDelete, s.sessionChain(s.handleRevokeSession)))
	// Unlinking works for every provider, enabled or not, so a user can remove
	// a link to a provider that has been turned off.
	mux.Handle(MeIdentitiesPath+"/{id}", s.unlinkRoute())
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

// OptionalSession authenticates a browser session when one is present and
// otherwise continues anonymously. It is used only by routes such as the OAuth
// authorize endpoint that must preserve their own unauthenticated redirect
// behavior. Invalid cookies are cleared and treated as absent; internal store
// failures remain closed 500s.
func OptionalSession(m *SessionManager) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(sessionCookieName); err != nil {
				next.ServeHTTP(w, r)
				return
			}
			sess, rotated, err := readAndAuthenticateSession(r, m)
			if err != nil {
				if errors.Is(err, ErrSessionInvalid) {
					ClearSessionCookie(w)
					next.ServeHTTP(w, r)
					return
				}
				api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
				return
			}
			if rotated != "" {
				SetSessionCookie(w, rotated)
			}
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
