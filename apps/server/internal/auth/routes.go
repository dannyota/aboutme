package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	if s.providerLogin.Any() {
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

	mux.Handle(MePath, http.HandlerFunc(s.handleAccount))
	mux.Handle(LogoutPath, route(http.MethodPost, s.sessionChain(s.handleLogout)))
	mux.Handle(SessionsPath, http.HandlerFunc(s.handleSessionsCollection))
	mux.Handle(SessionsPath+"/{id}", route(http.MethodDelete, s.sessionChain(s.handleRevokeSession)))
	mux.Handle(MeIdentitiesPath+"/{id}", s.unlinkRoute())
}

const sessionRequiredCode = "session_required"

const reauthRequiredCode = "reauth_required"

const statusClientClosedRequest = 499

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

			ctx := ContextWithSession(r.Context(), sess)
			ctx = api.WithAccountID(ctx, sess.UserID.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

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

func readAndAuthenticateSession(r *http.Request, m *SessionManager) (sess store.Session, rotated string, err error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return store.Session{}, "", ErrSessionInvalid
	}
	return m.Authenticate(r.Context(), cookie.Value)
}

func rejectSession(w http.ResponseWriter) {
	ClearSessionCookie(w)
	api.WriteError(w, http.StatusUnauthorized, sessionRequiredCode, "a valid session is required")
}

func (s *Service) sessionChain(handler http.HandlerFunc) http.HandlerFunc {
	wrapped := RequireSession(s.sessionMgr)(RequireCSRF(s.publicOrigin)(handler))
	return wrapped.ServeHTTP
}

func (s *Service) writeSessionAPIInternalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	if errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled) {
		if s.logger != nil {
			s.logger.InfoContext(r.Context(), "auth: session api request canceled",
				"request_id", api.RequestIDFromContext(r.Context()), "op", op)
		}
		w.WriteHeader(statusClientClosedRequest)
		return
	}
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

func methodMatches(routeMethod, requestMethod string) bool {
	return requestMethod == routeMethod
}
