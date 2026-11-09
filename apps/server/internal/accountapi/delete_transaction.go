package accountapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/privacynotice"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type deleteCommitOutcome uint8

const (
	deleteDefinitelyRolledBack deleteCommitOutcome = iota
	deleteCommitted
	deleteCommitUnknown
)

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
	if plan.operatorSlug != "" && !slices.Contains(slugs, plan.operatorSlug) {
		// The reported slug moved to another account after the plan was built.
		// Lock it too so the owner check below reads a stable row.
		slugs = append(slugs, plan.operatorSlug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		if err := qtx.LockSlugClaim(ctx, slug); err != nil {
			return fmt.Errorf("accountapi: lock slug: %w", err)
		}
	}
	if plan.operatorSlug != "" {
		if err := requireSlugOwner(ctx, qtx, plan.operatorSlug, plan.user.ID); err != nil {
			return err
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
			if plan.operatorSlug != "" {
				return errAccountChanged
			}
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
	if noticeErr := privacynotice.ResolveAccountDeletion(ctx, qtx, lockedUser.ID, plan.occurredAt); noticeErr != nil {
		return noticeErr
	}
	set, err := qtx.ListAccountDeletionResumeSetForUpdate(ctx, plan.user.ID)
	if err != nil {
		return fmt.Errorf("accountapi: lock resume set: %w", err)
	}
	if !sameDeletionResumeSet(set, plan.resumes) {
		return errAccountChanged
	}
	if plan.operatorSlug == "" {
		if err := requireSelfDeleteProof(ctx, qtx, plan, lockedUser, s.now()); err != nil {
			return err
		}
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
				Slug: *item.Slug, ReleasedAt: plan.occurredAt,
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
	if plan.operatorSlug == "" {
		// The operator command runs in another process than the server, whose
		// in-memory fences would stay on the old generation and refuse every
		// discovery read and public-state transition after a durable advance.
		if generation, err := qtx.AdvanceDiscoveryGeneration(ctx); err != nil {
			return fmt.Errorf("accountapi: advance discovery: %w", err)
		} else if generation != plan.discoveryGeneration+1 {
			return errors.New("accountapi: discovery generation advanced unexpectedly")
		}
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

// requireSelfDeleteProof locks the deleting session and the second-factor
// policy and requires recent reauthentication. The operator command has no
// session and skips it.
func requireSelfDeleteProof(ctx context.Context, qtx *store.Queries, plan accountDeletionPlan, lockedUser store.User, now time.Time) error {
	liveSession, err := qtx.GetAccountDeletionSessionForUpdate(ctx, store.GetAccountDeletionSessionForUpdateParams{
		ID: plan.sessionID, UserID: plan.user.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrSessionInvalid
		}
		return fmt.Errorf("accountapi: lock session: %w", err)
	}
	policy, err := qtx.GetSecondFactorPolicyForUpdate(ctx, lockedUser.ID)
	var policyPtr *store.SecondFactorPolicy
	switch {
	case err == nil:
		policyPtr = &policy
	case errors.Is(err, pgx.ErrNoRows):
		// Unenrolled account: RequireRecentSecondFactorReauth checks primary
		// proof only.
	default:
		return fmt.Errorf("accountapi: lock factor policy: %w", err)
	}
	if err := auth.RequireRecentSecondFactorReauth(lockedUser, policyPtr, liveSession, now); err != nil {
		return err
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
