package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	startRateLimitRequests = 30
	startRateLimitWindow   = time.Minute
)

const startReapBatch = 200

type authorizeURLBuilder func(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID, returnPath string) (handle, authURL, op string, err error)

type startAuthorizeResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

func (s *Service) startRoute(provider Provider, build authorizeURLBuilder, limit api.Middleware) http.Handler {
	login := limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleLoginStart(w, r, provider, build)
	}))
	link := RequireSession(s.sessionMgr)(limit(RequireCSRF(s.publicOrigin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleLinkStart(w, r, provider, build)
	}))))

	return s.logStartRejections(provider)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			login.ServeHTTP(w, r)
		case http.MethodPost:
			link.ServeHTTP(w, r)
		default:
			markStartRejection(r.Context(), reasonStartMethodNotAllowed)
			w.Header().Set("Allow", "GET, POST")
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"method not allowed on "+r.URL.Path+"; use GET (login) or POST (link/reauth)")
		}
	}))
}

func (s *Service) startRateLimit() api.Middleware {
	return api.RateLimit(api.RateLimiterConfig{
		Requests:       s.startRateLimitRequests,
		Window:         s.startRateLimitWindow,
		TrustedProxies: s.trustedProxies,
		Key:            api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
		Logger:         s.logger,
	})
}

func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request, provider Provider, build authorizeURLBuilder) {
	switch Purpose(r.URL.Query().Get("purpose")) {
	case PurposeLink, PurposeReauth:
		markStartRejection(r.Context(), reasonStartMethodNotAllowed)
		w.Header().Set("Allow", http.MethodPost)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"purpose=link and purpose=reauth require POST on "+r.URL.Path)
		return
	case PurposeView:
		s.handleViewStart(w, r, provider)
		return
	}

	ctx := withProviderHTTPClient(r.Context())
	s.reapExpiredOAuthTransactions(ctx, r, provider)

	returnPath := validatedLoginReturnPath(r.URL.Query().Get("next"))
	handle, authURL, op, err := build(ctx, PurposeLogin, uuid.Nil, returnPath)
	if err != nil {
		s.writeInternalError(w, r, provider, op, err)
		return
	}

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Service) handleLinkStart(w http.ResponseWriter, r *http.Request, provider Provider, build authorizeURLBuilder) {
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		markStartRejection(r.Context(), reasonStartSessionRequired)
		rejectSession(w)
		return
	}

	purpose := Purpose(r.URL.Query().Get("purpose"))
	if purpose != PurposeLink && purpose != PurposeReauth {
		markStartRejection(r.Context(), reasonStartPurposeUnsupported)
		api.WriteError(w, http.StatusBadRequest, "bad_request",
			"purpose must be link or reauth on POST "+r.URL.Path)
		return
	}

	if purpose == PurposeLink {
		if err := s.recheckSensitiveSession(r.Context(), sess); err != nil {
			switch {
			case errors.Is(err, ErrSessionInvalid):
				markStartRejection(r.Context(), reasonStartSessionRequired)
				rejectSession(w)
			case errors.Is(err, ErrReauthRequired):
				markStartRejection(r.Context(), reasonStartReauthRequired)
				api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
			default:
				s.writeInternalError(w, r, provider, "recheck_link_session", err)
			}
			return
		}
	}

	ctx := withProviderHTTPClient(r.Context())
	s.reapExpiredOAuthTransactions(ctx, r, provider)

	handle, authURL, op, err := build(ctx, purpose, sess.UserID, defaultLoginReturnPath)
	if err != nil {
		s.writeInternalError(w, r, provider, op, err)
		return
	}

	SetOAuthTxCookie(w, handle)
	api.WriteData(w, http.StatusOK, startAuthorizeResponse{AuthorizeURL: authURL})
}

func (s *Service) reapExpiredOAuthTransactions(ctx context.Context, r *http.Request, provider Provider) {
	if _, err := s.q.DeleteExpiredOAuthTransactions(ctx, store.DeleteExpiredOAuthTransactionsParams{
		Cutoff:  s.tx.now(),
		MaxRows: startReapBatch,
	}); err != nil {
		s.logInternalError(r, provider, "reap_oauth_transactions", err)
	}
}

type startRejectionContextKey struct{}

type startRejectionRecord struct {
	reason rejectReason
	set    bool
}

func markStartRejection(ctx context.Context, reason rejectReason) {
	if rec, ok := ctx.Value(startRejectionContextKey{}).(*startRejectionRecord); ok {
		rec.reason, rec.set = reason, true
	}
}

type startStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *startStatusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *startStatusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *startStatusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (s *Service) logStartRejections(provider Provider) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &startRejectionRecord{}
			recorder := &startStatusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), startRejectionContextKey{}, rec)))

			if recorder.status < http.StatusBadRequest {
				return
			}
			reason := rec.reason
			if !rec.set {
				reason = startReasonForStatus(recorder.status)
			}
			if s.logger == nil {
				return
			}
			s.logger.WarnContext(r.Context(), "auth: start rejected",
				"request_id", api.RequestIDFromContext(r.Context()),
				"provider", provider,
				"status", recorder.status,
				"reason", reason.String(),
			)
		})
	}
}

func startReasonForStatus(status int) rejectReason {
	switch status {
	case http.StatusUnauthorized:
		return reasonStartSessionRequired
	case http.StatusForbidden:
		return reasonStartCSRFRejected
	case http.StatusTooManyRequests:
		return reasonStartRateLimited
	case http.StatusBadRequest:
		return reasonStartClientIPUnresolvable
	default:
		return reasonUnspecified
	}
}
