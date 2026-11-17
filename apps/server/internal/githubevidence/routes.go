package githubevidence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	unavailableCode     = "evidence_unavailable"
	sessionRequiredCode = "evidence_session_required"
	requestInvalidCode  = "request_invalid"
)

type statusBody struct {
	Generation  int64           `json:"generation"`
	HasEvidence bool            `json:"hasEvidence"`
	Connection  *connectionBody `json:"connection"`
	Session     *sessionBody    `json:"session"`
}

type connectionBody struct {
	GitHubUserID int64     `json:"githubUserId"`
	Login        string    `json:"login"`
	ConnectedAt  time.Time `json:"connectedAt"`
}

type sessionBody struct {
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	if !s.enabled {
		unavailable := http.HandlerFunc(writeUnavailable)
		mux.Handle(StatusPath, unavailable)
		mux.Handle(StatusPath+"/", unavailable)
		return
	}
	cookie := func(handler http.HandlerFunc) http.Handler {
		return auth.RequireSession(s.sessions)(auth.RequireCSRF(s.publicOrigin)(handler))
	}
	mux.Handle(StatusPath, methods("GET, DELETE", map[string]http.Handler{
		http.MethodGet:    cookie(s.handleStatus),
		http.MethodDelete: cookie(s.handleDisconnect),
	}))
	mux.Handle(StartPath, methods(http.MethodPost, map[string]http.Handler{
		http.MethodPost: cookie(s.handleStart),
	}))
	mux.Handle(CallbackPath, methods(http.MethodGet, map[string]http.Handler{
		http.MethodGet: auth.OptionalSession(s.sessions)(http.HandlerFunc(s.handleCallback)),
	}))
	mux.Handle(SessionEndPath, methods(http.MethodPost, map[string]http.Handler{
		http.MethodPost: cookie(s.handleSessionEnd),
	}))
}

func methods(allow string, handlers map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, ok := handlers[r.Method]
		if !ok {
			w.Header().Set("Allow", allow)
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed; use "+allow)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func writeUnavailable(w http.ResponseWriter, _ *http.Request) {
	api.WriteError(w, http.StatusNotFound, unavailableCode, "GitHub evidence is not available")
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	if !plainRequest(r) {
		writeRequestInvalid(w, "this request takes no query or body")
		return
	}
	var body statusBody
	err := s.inAccount(r.Context(), sess, func(ctx context.Context, a account) error {
		var snapshotErr error
		body, snapshotErr = s.statusSnapshot(ctx, a)
		return snapshotErr
	})
	if errors.Is(err, auth.ErrSessionInvalid) {
		writeSessionRequired(w)
		return
	}
	if err != nil {
		s.writeInternal(w, r, "read_status", err)
		return
	}
	api.WriteData(w, http.StatusOK, body)
}

func (s *Service) statusSnapshot(ctx context.Context, a account) (statusBody, error) {
	var body statusBody
	if a.found {
		body.Generation = a.connection.Generation
	}
	if row := a.connection; a.connected() {
		body.Connection = &connectionBody{GitHubUserID: *row.GithubUserID, Login: *row.Login, ConnectedAt: row.ConnectedAt.UTC()}
	}
	hasEvidence, err := a.q.AccountHasProjectEvidence(ctx, a.user.ID)
	if err != nil {
		return statusBody{}, fmt.Errorf("githubevidence: read evidence presence: %w", err)
	}
	body.HasEvidence = hasEvidence
	if s.hooks.statusRead != nil {
		s.hooks.statusRead(a.tx)
	}
	if live, isLive := s.currentSession(a); isLive {
		body.Session = &sessionBody{ExpiresAt: live.expiresAt.UTC()}
	}
	return body, nil
}

func (s *Service) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	if !plainRequest(r) {
		writeRequestInvalid(w, "this request takes no query or body")
		return
	}
	var ended bool
	err := s.inAccount(r.Context(), sess, func(_ context.Context, a account) error {
		ended = s.registry.end(a.user.ID)
		return nil
	})
	if errors.Is(err, auth.ErrSessionInvalid) {
		writeSessionRequired(w)
		return
	}
	if err != nil {
		s.writeInternal(w, r, "end_session", err)
		return
	}
	if !ended {
		api.WriteError(w, http.StatusConflict, sessionRequiredCode, "no GitHub evidence session is live")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) connection(r *http.Request, userID uuid.UUID) (store.GithubConnection, bool, error) {
	row, err := s.q.GetGitHubConnection(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GithubConnection{}, false, nil
	}
	if err != nil {
		return store.GithubConnection{}, false, err
	}
	return row, true, nil
}

func plainRequest(r *http.Request) bool {
	return r.URL.RawQuery == "" && !r.URL.ForceQuery && emptyBody(r)
}

func emptyBody(r *http.Request) bool {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		return false
	}
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	var probe [1]byte
	n, err := r.Body.Read(probe[:])
	return n == 0 && errors.Is(err, io.EOF)
}

func writeSessionRequired(w http.ResponseWriter) {
	auth.ClearSessionCookie(w)
	api.WriteError(w, http.StatusUnauthorized, "session_required", "a valid session is required")
}

func writeRequestInvalid(w http.ResponseWriter, message string) {
	api.WriteError(w, http.StatusBadRequest, requestInvalidCode, message)
}

func writeLimited(w http.ResponseWriter, retry time.Duration) {
	seconds := int(math.Ceil(retry.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	api.WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; retry later")
}

func (s *Service) writeInternal(w http.ResponseWriter, r *http.Request, op string, err error) {
	s.logInternal(r, op, err)
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

func (s *Service) logInternal(r *http.Request, op string, err error) {
	attrs := []any{"request_id", api.RequestIDFromContext(r.Context()), "op", op}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		attrs = append(attrs, "sqlstate", pgErr.Code)
	}
	s.logger.ErrorContext(r.Context(), "github evidence: internal error", attrs...)
}
