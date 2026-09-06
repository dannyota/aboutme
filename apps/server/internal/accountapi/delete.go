package accountapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	deleteDrainTimeout = 5 * time.Second
	deletePlanAttempts = 3
)

var errAccountChanged = errors.New("accountapi: account changed")

type accountDeletionPlan struct {
	user                store.User
	sessionID           uuid.UUID
	resumes             []resume.Resume
	discoveryGeneration int64
	auditID             uuid.UUID
	occurredAt          time.Time
}

type deleteCommitOutcome uint8

const (
	deleteDefinitelyRolledBack deleteCommitOutcome = iota
	deleteCommitted
	deleteCommitUnknown
)

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
	for attempt := 0; attempt < deletePlanAttempts; attempt++ {
		plan, err := s.prepareDeletion(ctx, sess)
		if err != nil {
			return err
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
			return err
		}
		if closeErr := transition.Close(ctx, s.now().Add(deleteDrainTimeout)); closeErr != nil {
			return closeErr
		}
		if s.afterClose != nil {
			s.afterClose()
		}

		outcome, committed, err := s.executeDeletion(ctx, plan)
		switch outcome {
		case deleteCommitted:
			if transitionErr := transition.Commit(committed); transitionErr != nil {
				return transitionErr
			}
			return nil
		case deleteDefinitelyRolledBack:
			if rollbackErr := transition.Rollback(); rollbackErr != nil {
				return rollbackErr
			}
			if errors.Is(err, errAccountChanged) {
				continue
			}
			return err
		case deleteCommitUnknown:
			recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteDrainTimeout)
			recovery := &accountDeletionRecovery{pool: s.pool, plan: plan}
			recoveryErr := transition.Recover(recoveryCtx, recovery)
			cancel()
			if recoveryErr != nil {
				return recoveryErr
			}
			if recovery.committed {
				return nil
			}
			return err
		default:
			return errors.New("accountapi: invalid commit outcome")
		}
	}
	return errAccountChanged
}

func (s *Service) prepareDeletion(ctx context.Context, sess store.Session) (accountDeletionPlan, error) {
	q := store.New(s.pool)
	user, err := q.GetUserByID(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
	return accountDeletionPlan{
		user: user, sessionID: sess.ID, resumes: resumes,
		discoveryGeneration: public.DiscoveryGeneration,
		auditID:             auditID, occurredAt: s.now().UTC().Truncate(time.Microsecond),
	}, nil
}

func (p accountDeletionPlan) publicPlan() publicstate.Plan {
	targets := make([]publicstate.ResumeTarget, len(p.resumes))
	for i, item := range p.resumes {
		targets[i] = publicstate.ResumeTarget{ID: item.ID, ExpectedRevision: item.Revision, Class: publicstate.Revoking}
	}
	return publicstate.Plan{DiscoveryGeneration: &p.discoveryGeneration, Resumes: targets}
}

func (s *Service) executeDeletion(ctx context.Context, plan accountDeletionPlan) (deleteCommitOutcome, publicstate.CommittedState, error) {
	tx, err := s.beginTx(ctx)
	if err != nil {
		return deleteDefinitelyRolledBack, publicstate.CommittedState{}, fmt.Errorf("accountapi: begin deletion: %w", err)
	}
	qtx := store.New(tx)

	if err := s.mutateDeletion(ctx, qtx, plan); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteDrainTimeout)
		rollbackErr := tx.Rollback(rollbackCtx)
		cancel()
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return deleteDefinitelyRolledBack, publicstate.CommittedState{}, fmt.Errorf("accountapi: roll back deletion: %w", rollbackErr)
		}
		return deleteDefinitelyRolledBack, publicstate.CommittedState{}, err
	}
	if err := s.commitTx(ctx, tx); err != nil {
		return classifyDeleteCommitError(err), publicstate.CommittedState{}, fmt.Errorf("accountapi: commit deletion: %w", err)
	}
	nextDiscovery := plan.discoveryGeneration + 1
	retired := make([]uuid.UUID, len(plan.resumes))
	for i, item := range plan.resumes {
		retired[i] = item.ID
	}
	return deleteCommitted, publicstate.CommittedState{
		DiscoveryGeneration: &nextDiscovery,
		ResumeRevisions:     map[uuid.UUID]int64{},
		RetiredResumes:      retired,
	}, nil
}

func (s *Service) mutateDeletion(ctx context.Context, qtx *store.Queries, plan accountDeletionPlan) error {
	slugs := make([]string, 0, len(plan.resumes))
	for _, item := range plan.resumes {
		if item.Slug != nil {
			slugs = append(slugs, *item.Slug)
		}
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		if err := qtx.LockSlugClaim(ctx, slug); err != nil {
			return fmt.Errorf("accountapi: lock slug: %w", err)
		}
	}
	public, err := qtx.LockPublicState(ctx)
	if err != nil {
		return fmt.Errorf("accountapi: lock public state: %w", err)
	}
	if public.DiscoveryGeneration != plan.discoveryGeneration {
		return errAccountChanged
	}
	if lockErr := qtx.LockCanonicalAccountEmail(ctx, plan.user.Email); lockErr != nil {
		return fmt.Errorf("accountapi: lock canonical email: %w", lockErr)
	}
	var registration *store.PasswordRegistration
	if row, lockErr := qtx.GetPasswordRegistrationByEmailForUpdate(ctx, plan.user.Email); lockErr == nil {
		registration = &row
	} else if !errors.Is(lockErr, pgx.ErrNoRows) {
		return fmt.Errorf("accountapi: lock password registration: %w", lockErr)
	}
	lockedUser, err := qtx.GetUserForUpdate(ctx, plan.user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrSessionInvalid
		}
		return fmt.Errorf("accountapi: lock user: %w", err)
	}
	if s.afterUserLock != nil {
		s.afterUserLock()
	}
	if !sameDeletionUser(lockedUser, plan.user) {
		return errAccountChanged
	}
	set, err := qtx.ListAccountDeletionResumeSetForUpdate(ctx, plan.user.ID)
	if err != nil {
		return fmt.Errorf("accountapi: lock resume set: %w", err)
	}
	if !sameDeletionResumeSet(set, plan.resumes) {
		return errAccountChanged
	}
	liveSession, err := qtx.GetAccountDeletionSessionForUpdate(ctx, store.GetAccountDeletionSessionForUpdateParams{
		ID: plan.sessionID, UserID: plan.user.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrSessionInvalid
		}
		return fmt.Errorf("accountapi: lock session: %w", err)
	}
	if err := auth.RequireLiveSession(liveSession, s.now()); err != nil {
		return err
	}
	if err := auth.RequireRecentReauth(liveSession, s.now()); err != nil {
		return err
	}

	for _, item := range plan.resumes {
		if photo := item.Doc.PersonalDetails.Photo; photo != nil {
			if _, err := media.ParsePhotoKey(item.ID, photo.Key); err != nil {
				return fmt.Errorf("accountapi: invalid stored photo key: %w", err)
			}
		}
	}
	if registration != nil {
		if _, err := qtx.DeletePasswordRegistration(ctx, registration.ID); err != nil {
			return fmt.Errorf("accountapi: delete password registration: %w", err)
		}
	}
	for _, item := range plan.resumes {
		if item.Slug != nil {
			if _, err := qtx.InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{
				Slug: *item.Slug, ReleasedByUserID: &plan.user.ID, ReleasedAt: plan.occurredAt,
			}); err != nil {
				return fmt.Errorf("accountapi: insert slug tombstone: %w", err)
			}
		}
		if photo := item.Doc.PersonalDetails.Photo; photo != nil {
			if _, err := s.enqueueMediaJob(ctx, qtx, store.EnqueueMediaDeletionJobParams{
				ResumeID: item.ID, ObjectKey: photo.Key,
			}); err != nil {
				return fmt.Errorf("accountapi: enqueue media deletion: %w", err)
			}
		}
	}
	if generation, err := qtx.AdvanceDiscoveryGeneration(ctx); err != nil {
		return fmt.Errorf("accountapi: advance discovery: %w", err)
	} else if generation != plan.discoveryGeneration+1 {
		return errors.New("accountapi: discovery generation advanced unexpectedly")
	}
	if _, err := qtx.InsertAccountDeletedAuditEvent(ctx, store.InsertAccountDeletedAuditEventParams{
		ID: plan.auditID, OccurredAt: plan.occurredAt,
	}); err != nil {
		return fmt.Errorf("accountapi: insert deletion audit: %w", err)
	}
	if deleted, err := qtx.DeleteAccountUser(ctx, plan.user.ID); err != nil {
		return fmt.Errorf("accountapi: delete user: %w", err)
	} else if deleted != 1 {
		return errAccountChanged
	}
	return nil
}

func sameDeletionUser(got, want store.User) bool {
	return got.ID == want.ID && got.Email == want.Email && got.Name == want.Name &&
		equalStringPointers(got.AvatarKey, want.AvatarKey) && got.CreatedAt.Equal(want.CreatedAt) && got.UpdatedAt.Equal(want.UpdatedAt)
}

func sameDeletionResumeSet(got []store.ListAccountDeletionResumeSetForUpdateRow, want []resume.Resume) bool {
	if len(got) != len(want) {
		return false
	}
	wantSet := make([]store.ListAccountDeletionResumeSetForUpdateRow, len(want))
	for i, item := range want {
		wantSet[i] = store.ListAccountDeletionResumeSetForUpdateRow{ID: item.ID, Revision: item.Revision}
	}
	sort.Slice(wantSet, func(i, j int) bool { return bytes.Compare(wantSet[i].ID[:], wantSet[j].ID[:]) < 0 })
	for i := range got {
		if got[i] != wantSet[i] {
			return false
		}
	}
	return true
}

func equalStringPointers(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func classifyDeleteCommitError(err error) deleteCommitOutcome {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "40003" || pgErr.Code == "08007") {
		return deleteCommitUnknown
	}
	if errors.Is(err, pgx.ErrTxCommitRollback) || pgErr != nil {
		return deleteDefinitelyRolledBack
	}
	return deleteCommitUnknown
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
