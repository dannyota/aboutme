package accountapi

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// accountDeletionRecovery uses only the independent pool after an
// indeterminate COMMIT. It proves one whole durable outcome or fails closed.
type accountDeletionRecovery struct {
	pool      *store.Pool
	plan      accountDeletionPlan
	committed bool
}

// Resolve proves the durable deletion outcome after an ambiguous commit.
func (r *accountDeletionRecovery) Resolve(ctx context.Context) (publicstate.RecoveryProof, error) {
	q := store.New(r.pool)
	user, err := q.GetUserByID(ctx, r.plan.user.ID)
	if err == nil {
		if !sameDeletionUser(user, r.plan.user) {
			return publicstate.RecoveryProof{}, errors.New("accountapi: rollback proof user changed")
		}
		rows, listErr := q.ListResumesForUser(ctx, r.plan.user.ID)
		if listErr != nil {
			return publicstate.RecoveryProof{}, fmt.Errorf("accountapi: rollback proof resumes: %w", listErr)
		}
		if !sameRecoveryResumeSet(rows, r.plan) {
			return publicstate.RecoveryProof{}, errors.New("accountapi: rollback proof resume set changed")
		}
		public, publicErr := q.GetPublicState(ctx)
		if publicErr != nil || public.DiscoveryGeneration != r.plan.discoveryGeneration {
			return publicstate.RecoveryProof{}, errors.New("accountapi: rollback proof discovery changed")
		}
		return publicstate.RecoveryProof{
			Disposition: publicstate.RecoveryNotCommitted,
			State:       r.unchangedState(),
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return publicstate.RecoveryProof{}, fmt.Errorf("accountapi: recovery read user: %w", err)
	}

	public, err := q.GetPublicState(ctx)
	if err != nil || public.DiscoveryGeneration != r.plan.discoveryGeneration+1 {
		return publicstate.RecoveryProof{}, errors.New("accountapi: committed proof discovery does not match")
	}
	audit, err := q.GetAccountDeletedAuditEvent(ctx, r.plan.auditID)
	if err != nil || audit.Kind != "account_deleted" || audit.MediaJobID != nil || !audit.OccurredAt.Equal(r.plan.occurredAt) {
		return publicstate.RecoveryProof{}, errors.New("accountapi: committed proof audit does not match")
	}
	for _, item := range r.plan.resumes {
		if item.Slug != nil {
			tombstone, tombstoneErr := q.GetSlugTombstoneForUpdate(ctx, *item.Slug)
			if tombstoneErr != nil || tombstone.ReleasedByUserID != nil || !tombstone.ReleasedAt.Equal(r.plan.occurredAt) {
				return publicstate.RecoveryProof{}, errors.New("accountapi: committed proof tombstone does not match")
			}
		}
		if photo := item.Doc.PersonalDetails.Photo; photo != nil {
			job, jobErr := q.GetMediaDeletionJobByObjectKey(ctx, store.GetMediaDeletionJobByObjectKeyParams{
				ResumeID: item.ID, ObjectKey: photo.Key,
			})
			if jobErr != nil || job.ResumeID != item.ID || job.ObjectKey != photo.Key {
				return publicstate.RecoveryProof{}, errors.New("accountapi: committed proof media job does not match")
			}
		}
	}
	r.committed = true
	return publicstate.RecoveryProof{Disposition: publicstate.RecoveryCommitted, State: r.committedState()}, nil
}

func (r accountDeletionRecovery) unchangedState() publicstate.CommittedState {
	revisions := make(map[uuid.UUID]int64, len(r.plan.resumes))
	for _, item := range r.plan.resumes {
		revisions[item.ID] = item.Revision
	}
	discovery := r.plan.discoveryGeneration
	return publicstate.CommittedState{DiscoveryGeneration: &discovery, ResumeRevisions: revisions}
}

func (r accountDeletionRecovery) committedState() publicstate.CommittedState {
	retired := make([]uuid.UUID, len(r.plan.resumes))
	for i, item := range r.plan.resumes {
		retired[i] = item.ID
	}
	discovery := r.plan.discoveryGeneration + 1
	return publicstate.CommittedState{
		DiscoveryGeneration: &discovery,
		ResumeRevisions:     map[uuid.UUID]int64{}, RetiredResumes: retired,
	}
}

func sameRecoveryResumeSet(got []store.Resume, plan accountDeletionPlan) bool {
	if len(got) != len(plan.resumes) {
		return false
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ID.String() < got[j].ID.String() })
	want := append([]resumeIdentity(nil), resumeIdentities(plan)...)
	sort.Slice(want, func(i, j int) bool { return want[i].id.String() < want[j].id.String() })
	for i := range got {
		if got[i].ID != want[i].id || got[i].Revision != want[i].revision {
			return false
		}
	}
	return true
}

type resumeIdentity struct {
	id       uuid.UUID
	revision int64
}

func resumeIdentities(plan accountDeletionPlan) []resumeIdentity {
	items := make([]resumeIdentity, len(plan.resumes))
	for i, item := range plan.resumes {
		items[i] = resumeIdentity{id: item.ID, revision: item.Revision}
	}
	return items
}

var _ publicstate.RecoveryResolver = (*accountDeletionRecovery)(nil)
