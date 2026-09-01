package auth

// Public login uses GET. Privileged link and reauthentication use authenticated,
// CSRF-protected POST and return an authorization URL. See
// docs/adr/0014-oauth-start-methods.md.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// The start budget limits transaction inserts and downstream account-existence
// probes. Anonymous login is keyed by IP; authenticated link and reauth use
// account and IP.
const (
	startRateLimitRequests = 30
	startRateLimitWindow   = time.Minute
)

// startReapBatch bounds the cleanup cost paid by one start request.
const startReapBatch = 200

// authorizeURLBuilder creates the provider-specific transaction and URL. op is
// a fixed failure label and is empty on success.
type authorizeURLBuilder func(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID, returnPath string) (handle, authURL, op string, err error)

const maxLoginReturnPathBytes = 2048

// validatedLoginReturnPath accepts only a same-origin relative URL. Invalid or
// absent input becomes the fixed resume-list destination before any database
// row or provider redirect is created.
func validatedLoginReturnPath(raw string) string {
	if raw == "" || len(raw) > maxLoginReturnPathBytes ||
		!strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.ContainsAny(raw, "\\\r\n") {
		return defaultLoginReturnPath
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Opaque != "" {
		return defaultLoginReturnPath
	}
	return raw
}

// startAuthorizeResponse lets the client open the provider as a top-level
// navigation instead of following a fetch redirect.
type startAuthorizeResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// startRoute applies the purpose-specific middleware order:
//
//	GET:  logStartRejections -> limit -> handleLoginStart
//	POST: logStartRejections -> RequireSession -> limit -> RequireCSRF -> handleLinkStart
//
// Authentication must precede the POST limiter so its account key is present.
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
			// HEAD must not create or consume a transaction when a crawler
			// previews the route.
			markStartRejection(r.Context(), reasonStartMethodNotAllowed)
			w.Header().Set("Allow", "GET, POST")
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"method not allowed on "+r.URL.Path+"; use GET (login) or POST (link/reauth)")
		}
	}))
}

// startRateLimit keys anonymous GET by IP and authenticated POST by account and
// IP. Service fields let tests reduce the budget.
func (s *Service) startRateLimit() api.Middleware {
	return api.RateLimit(api.RateLimiterConfig{
		Requests:       s.startRateLimitRequests,
		Window:         s.startRateLimitWindow,
		TrustedProxies: s.trustedProxies,
		Key:            api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
		Logger:         s.logger,
	})
}

// handleLoginStart rejects privileged purposes before database or cookie work.
// Other values retain the least-privileged login default.
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request, provider Provider, build authorizeURLBuilder) {
	switch Purpose(r.URL.Query().Get("purpose")) {
	case PurposeLink, PurposeReauth:
		markStartRejection(r.Context(), reasonStartMethodNotAllowed)
		w.Header().Set("Allow", http.MethodPost)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"purpose=link and purpose=reauth require POST on "+r.URL.Path)
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

// handleLinkStart accepts only link or reauth after session and CSRF checks.
// Link requires recent reauth before a transaction is created; reauth cannot
// require its own result. See docs/design/security.md.
func (s *Service) handleLinkStart(w http.ResponseWriter, r *http.Request, provider Provider, build authorizeURLBuilder) {
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		// RequireSession normally populates this. Fail closed if the middleware
		// order is violated.
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
		if err := RequireRecentReauth(sess, s.sessionMgr.now()); err != nil {
			markStartRejection(r.Context(), reasonStartReauthRequired)
			api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
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

// reapExpiredOAuthTransactions performs bounded, best-effort cleanup before a
// start creates its row. Cleanup failure is logged but does not fail login.
func (s *Service) reapExpiredOAuthTransactions(ctx context.Context, r *http.Request, provider Provider) {
	if _, err := s.q.DeleteExpiredOAuthTransactions(ctx, store.DeleteExpiredOAuthTransactionsParams{
		Cutoff:  s.tx.now(),
		MaxRows: startReapBatch,
	}); err != nil {
		s.logInternalError(r, provider, "reap_oauth_transactions", err)
	}
}

// startRejectionContextKey is an unexported context key type
// (google.github.io/styleguide/go/decisions#contexts), so the recorder
// below can never collide with a value stored elsewhere.
type startRejectionContextKey struct{}

// startRejectionRecord carries an inner rejection reason through shared
// middleware. It distinguishes equal status codes such as reauth and CSRF 403s.
// One request goroutine writes it before the outer logger reads it.
type startRejectionRecord struct {
	reason rejectReason
	set    bool
}

// markStartRejection is a no-op outside logStartRejections.
func markStartRejection(ctx context.Context, reason rejectReason) {
	if rec, ok := ctx.Value(startRejectionContextKey{}).(*startRejectionRecord); ok {
		rec.reason, rec.set = reason, true
	}
}

// startStatusRecorder captures the response status for rejection logging.
//
// Unwrap preserves optional interfaces reached through http.ResponseController.
type startStatusRecorder struct {
	http.ResponseWriter
	status int
}

// Unwrap returns the wrapped writer for http.ResponseController.
func (w *startStatusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// WriteHeader records the first status written and forwards it.
func (w *startStatusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write records net/http's implicit 200 before forwarding the body.
func (w *startStatusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK // net/http's own implicit WriteHeader(200).
	}
	return w.ResponseWriter.Write(b)
}

// logStartRejections wraps a whole start route and emits exactly one Warn
// record per rejected request, naming the provider and a typed reason
// (reason.go's closed vocabulary). Successful starts log nothing here --
// the access log already records them.
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

// startReasonForStatus maps unmarked shared-middleware responses. Handler
// rejections with overlapping statuses mark their reason explicitly. Unknown
// statuses remain unspecified rather than guessed.
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
