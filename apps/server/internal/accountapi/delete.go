package accountapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/accountemail"
	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	deleteDrainTimeout = 5 * time.Second
	deletePlanAttempts = 3
)

var errAccountChanged = errors.New("accountapi: account changed")

// deletionRequest names the account to delete and the authority for it. A
// session means a self-delete, which proves recent reauthentication and
// revokes public state through the in-process coordinator. A reportedSlug
// means the operator command, which has no session and runs in another
// process than the server. Exactly one of the two is set; reportedSlug alone
// decides which path runs.
type deletionRequest struct {
	userID       uuid.UUID
	session      *store.Session
	reportedSlug string
}

func (r deletionRequest) operator() bool { return r.reportedSlug != "" }

var errDeletionRequestInvalid = errors.New("accountapi: deletion request needs exactly one of a session or a reported slug")

func (r deletionRequest) valid() bool { return (r.session != nil) != (r.reportedSlug != "") }

type accountDeletionPlan struct {
	user                store.User
	sessionID           uuid.UUID
	operatorSlug        string
	resumes             []resume.Resume
	discoveryGeneration int64
	auditID             uuid.UUID
	occurredAt          time.Time
}

// DeleteHandler returns the complete protected DELETE /api/v1/me handler.
func (s *Service) DeleteHandler() http.Handler {
	var handler http.Handler = http.HandlerFunc(s.handleDelete)
	if s.deleteAdmission != nil {
		handler = s.deleteAdmission(handler)
	}
	handler = auth.RequireCSRF(s.publicOrigin)(handler)
	handler = auth.RequireSession(s.sessions)(handler)
	return handler
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := validateDeleteRequest(r); err != nil {
		writeDeleteError(w, err)
		return
	}
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeDeleteError(w, auth.ErrSessionInvalid)
		return
	}
	if err := auth.RequireRecentReauth(sess, s.now()); err != nil {
		writeDeleteError(w, err)
		return
	}

	err := s.deleteAccount(r.Context(), sess)
	if err != nil {
		s.logDeleteFailure(r, err)
		writeDeleteError(w, err)
		return
	}

	auth.ClearSessionCookie(w)
	auth.ClearOAuthTxCookie(w)
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("Clear-Site-Data", `"cookies", "storage"`)
	w.WriteHeader(http.StatusNoContent)
}

func validateDeleteRequest(r *http.Request) error {
	if r.Method != http.MethodDelete || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return errDeleteRequestInvalid
	}
	if r.Body != nil && r.Body != http.NoBody {
		var probe [1]byte
		if n, err := r.Body.Read(probe[:]); n != 0 || err != io.EOF {
			return errDeleteRequestInvalid
		}
	}
	for _, name := range []string{
		"Authorization", "Content-Type", "Idempotency-Key", "If-Match",
		"If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range",
		"X-Resume-Schema-Version",
	} {
		if len(r.Header.Values(name)) != 0 {
			return errDeleteRequestInvalid
		}
	}
	return nil
}

var errDeleteRequestInvalid = errors.New("accountapi: invalid account request")

func (s *Service) deleteAccount(ctx context.Context, sess store.Session) error {
	_, err := s.deleteAccountForUser(ctx, deletionRequest{userID: sess.UserID, session: &sess})
	return err
}

// deleteAccountForUser is the one deletion path, keyed by user ID. It returns
// the plan that committed.
func (s *Service) deleteAccountForUser(ctx context.Context, req deletionRequest) (accountDeletionPlan, error) {
	if !req.valid() {
		return accountDeletionPlan{}, errDeletionRequestInvalid
	}
	if req.operator() {
		return s.deleteAccountAsOperator(ctx, req)
	}
	for attempt := 0; attempt < deletePlanAttempts; attempt++ {
		plan, err := s.prepareDeletion(ctx, req)
		if err != nil {
			return accountDeletionPlan{}, err
		}
		if s.afterPrepare != nil {
			s.afterPrepare()
		}
		transition, err := s.coordinator.Begin(ctx, plan.publicPlan())
		if err != nil {
			var mismatch *publicstate.GenerationMismatchError
			if errors.As(err, &mismatch) {
				continue
			}
			return accountDeletionPlan{}, err
		}
		if closeErr := transition.Close(ctx, s.now().Add(deleteDrainTimeout)); closeErr != nil {
			return accountDeletionPlan{}, closeErr
		}
		if s.afterClose != nil {
			s.afterClose()
		}

		outcome, committed, err := s.executeDeletion(ctx, plan)
		switch outcome {
		case deleteCommitted:
			if transitionErr := transition.Commit(committed); transitionErr != nil {
				return accountDeletionPlan{}, transitionErr
			}
			return plan, nil
		case deleteDefinitelyRolledBack:
			if rollbackErr := transition.Rollback(); rollbackErr != nil {
				return accountDeletionPlan{}, rollbackErr
			}
			if errors.Is(err, errAccountChanged) {
				continue
			}
			return accountDeletionPlan{}, err
		case deleteCommitUnknown:
			recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteDrainTimeout)
			recovery := &accountDeletionRecovery{pool: s.pool, plan: plan}
			recoveryErr := transition.Recover(recoveryCtx, recovery)
			cancel()
			if recoveryErr != nil {
				return accountDeletionPlan{}, recoveryErr
			}
			if recovery.committed {
				return plan, nil
			}
			return accountDeletionPlan{}, err
		default:
			return accountDeletionPlan{}, errors.New("accountapi: invalid commit outcome")
		}
	}
	return accountDeletionPlan{}, errAccountChanged
}

func (s *Service) prepareDeletion(ctx context.Context, req deletionRequest) (accountDeletionPlan, error) {
	q := store.New(s.pool)
	user, err := q.GetUserByID(ctx, req.userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if req.operator() {
				return accountDeletionPlan{}, errAccountChanged
			}
			return accountDeletionPlan{}, auth.ErrSessionInvalid
		}
		return accountDeletionPlan{}, fmt.Errorf("accountapi: preflight user: %w", err)
	}
	canonical, err := accountemail.Canonicalize(user.Email)
	if err != nil || canonical != user.Email {
		return accountDeletionPlan{}, errors.New("accountapi: stored email is not canonical")
	}
	resumes, err := resume.NewStore(s.pool, s.projector).List(ctx, user.ID)
	if err != nil {
		return accountDeletionPlan{}, fmt.Errorf("accountapi: preflight resumes: %w", err)
	}
	if len(resumes) > 3 {
		return accountDeletionPlan{}, errors.New("accountapi: owned resume set exceeds cap")
	}
	public, err := q.GetPublicState(ctx)
	if err != nil {
		return accountDeletionPlan{}, fmt.Errorf("accountapi: preflight public state: %w", err)
	}
	auditID, err := s.newAuditID()
	if err != nil {
		return accountDeletionPlan{}, fmt.Errorf("accountapi: generate audit id: %w", err)
	}
	plan := accountDeletionPlan{
		user: user, resumes: resumes, operatorSlug: req.reportedSlug,
		discoveryGeneration: public.DiscoveryGeneration,
		auditID:             auditID, occurredAt: s.now().UTC().Truncate(time.Microsecond),
	}
	if !req.operator() {
		plan.sessionID = req.session.ID
	}
	return plan, nil
}

func (p accountDeletionPlan) publicPlan() publicstate.Plan {
	targets := make([]publicstate.ResumeTarget, len(p.resumes))
	for i, item := range p.resumes {
		targets[i] = publicstate.ResumeTarget{ID: item.ID, ExpectedRevision: item.Revision, Class: publicstate.Revoking}
	}
	return publicstate.Plan{DiscoveryGeneration: &p.discoveryGeneration, Resumes: targets}
}

func writeDeleteError(w http.ResponseWriter, err error) {
	var drain *publicstate.DrainTimeoutError
	var unresolved *publicstate.RecoveryUnresolvedError
	switch {
	case errors.Is(err, errDeleteRequestInvalid):
		api.WriteError(w, http.StatusBadRequest, "request_invalid", "invalid account request")
	case errors.Is(err, auth.ErrSessionInvalid), errors.Is(err, pgx.ErrNoRows):
		api.WriteError(w, http.StatusUnauthorized, "session_required", "a valid session is required")
	case errors.Is(err, auth.ErrReauthRequired):
		api.WriteError(w, http.StatusForbidden, "reauth_required", "recent reauthentication is required")
	case errors.Is(err, errAccountChanged):
		api.WriteError(w, http.StatusConflict, "account_changed", "account changed; try again")
	case errors.As(err, &drain), errors.As(err, &unresolved), errors.Is(err, publicstate.ErrAdmissionClosed),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		api.WriteError(w, http.StatusServiceUnavailable, "account_unavailable", "account operation is unavailable")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

func (s *Service) logDeleteFailure(r *http.Request, err error) {
	if s.logger == nil || errors.Is(err, errDeleteRequestInvalid) || errors.Is(err, auth.ErrSessionInvalid) || errors.Is(err, auth.ErrReauthRequired) {
		return
	}
	attrs := []any{"request_id", api.RequestIDFromContext(r.Context()), "op", "delete_account"}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		attrs = append(attrs, "sqlstate", pgErr.Code)
	}
	s.logger.ErrorContext(r.Context(), "account deletion failed", attrs...)
}
