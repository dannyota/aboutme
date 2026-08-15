package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// mutationRecovery proves only the two durable outcomes of an indeterminate
// idempotency commit. It always uses the recovery pool, never Execute's tx.
type mutationRecovery struct {
	pool     *store.Pool
	identity mutationIdentity
	plan     publicstate.Plan
	retire   bool
	delete   *deleteRecoveryProof
	publish  *publishRecoveryProof
	response *resume.StoredResponse
}

type deleteRecoveryProof struct {
	ResumeID   uuid.UUID
	Slug       *string
	ReleasedAt time.Time
	PhotoKey   string
}

type publishRecoveryProof struct {
	ResumeID   uuid.UUID
	Effective  currentPublish
	OldSlug    *string
	ReleasedAt time.Time
}

// Resolve proves the mutation result after an indeterminate transaction outcome.
func (r *mutationRecovery) Resolve(ctx context.Context) (publicstate.RecoveryProof, error) {
	if r.pool == nil {
		return publicstate.RecoveryProof{}, errors.New("resumeapi: recovery pool is unavailable")
	}
	q := store.New(r.pool)
	record, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: r.identity.UserID, Route: r.identity.Operation, IdempotencyKey: r.identity.Key,
	})
	if err == nil {
		if !bytes.Equal(record.RequestHash, r.identity.RequestHash[:]) {
			return publicstate.RecoveryProof{}, errors.New("resumeapi: recovery record fingerprint mismatch")
		}
		if deleteErr := r.exactStoredDeleteResponse(record); deleteErr != nil {
			return publicstate.RecoveryProof{}, deleteErr
		}
		if publishErr := r.exactStoredPublishResponse(record); publishErr != nil {
			return publicstate.RecoveryProof{}, publishErr
		}
		stored, decodeErr := storedResponseFromRecord(record)
		if decodeErr != nil {
			return publicstate.RecoveryProof{}, fmt.Errorf("resumeapi: decode recovery response: %w", decodeErr)
		}
		r.response = &stored
		return r.committedProof(ctx, q)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return publicstate.RecoveryProof{}, fmt.Errorf("resumeapi: read recovery idempotency record: %w", err)
	}
	return r.notCommittedProof(ctx, q)
}

func (r mutationRecovery) exactStoredDeleteResponse(record store.IdempotencyRecord) error {
	if r.delete == nil {
		return nil
	}
	if record.ResponseStatus != 204 || !bytes.Equal(bytes.TrimSpace(record.ResponseBody), []byte("null")) {
		return errors.New("resumeapi: delete recovery response is not exact bodyless 204")
	}
	var headers map[string]string
	if err := json.Unmarshal(record.ResponseHeaders, &headers); err != nil {
		return fmt.Errorf("resumeapi: decode delete recovery headers: %w", err)
	}
	if len(headers) != 0 {
		return errors.New("resumeapi: delete recovery response has stored headers")
	}
	return nil
}

func (r mutationRecovery) committedProof(ctx context.Context, q *store.Queries) (publicstate.RecoveryProof, error) {
	state := publicstate.CommittedState{ResumeRevisions: make(map[uuid.UUID]int64)}
	for _, target := range r.plan.Resumes {
		row, err := q.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: target.ID, UserID: r.identity.UserID})
		if r.retire && errors.Is(err, pgx.ErrNoRows) {
			state.RetiredResumes = append(state.RetiredResumes, target.ID)
			continue
		}
		if err != nil {
			return publicstate.RecoveryProof{}, fmt.Errorf("resumeapi: prove committed resume state: %w", err)
		}
		if target.ExpectedRevision == int64(^uint64(0)>>1) || row.Revision != target.ExpectedRevision+1 {
			return publicstate.RecoveryProof{}, errors.New("resumeapi: committed recovery revision does not match")
		}
		state.ResumeRevisions[target.ID] = row.Revision
	}
	if r.plan.DiscoveryGeneration != nil {
		public, err := q.GetPublicState(ctx)
		if err != nil {
			return publicstate.RecoveryProof{}, fmt.Errorf("resumeapi: prove committed discovery state: %w", err)
		}
		if public.DiscoveryGeneration != *r.plan.DiscoveryGeneration+1 {
			return publicstate.RecoveryProof{}, errors.New("resumeapi: committed recovery discovery generation does not match")
		}
		state.DiscoveryGeneration = &public.DiscoveryGeneration
	}
	if r.delete != nil {
		if err := r.proveCommittedDelete(ctx); err != nil {
			return publicstate.RecoveryProof{}, err
		}
	}
	if r.publish != nil {
		if err := r.proveCommittedPublish(ctx, q); err != nil {
			return publicstate.RecoveryProof{}, err
		}
	}
	return publicstate.RecoveryProof{Disposition: publicstate.RecoveryCommitted, State: state}, nil
}

func (r mutationRecovery) exactStoredPublishResponse(record store.IdempotencyRecord) error {
	if r.publish == nil {
		return nil
	}
	if record.ResponseStatus != 200 {
		return errors.New("resumeapi: publish recovery response status is not exact")
	}
	var envelope struct {
		Data resumeJSON `json:"data"`
	}
	if err := json.Unmarshal(record.ResponseBody, &envelope); err != nil {
		return fmt.Errorf("resumeapi: decode publish recovery response: %w", err)
	}
	if envelope.Data.ID != r.publish.ResumeID || envelope.Data.Revision != fmt.Sprintf("%d", r.publish.Effective.Revision) ||
		envelope.Data.Live != r.publish.Effective.Live || envelope.Data.DownloadEnabled != r.publish.Effective.DownloadEnabled ||
		envelope.Data.SEOGeoEnabled != r.publish.Effective.SEOGeoEnabled || !equalOptionalString(envelope.Data.Slug, r.publish.Effective.Slug) {
		return errors.New("resumeapi: publish recovery response does not match intended state")
	}
	return nil
}

func (r mutationRecovery) proveCommittedDelete(ctx context.Context) error {
	if r.delete.Slug != nil {
		tombstone, err := store.New(r.pool).GetSlugTombstoneForUpdate(ctx, *r.delete.Slug)
		if err != nil {
			return fmt.Errorf("resumeapi: prove delete tombstone: %w", err)
		}
		if tombstone.ReleasedByUserID == nil || *tombstone.ReleasedByUserID != r.identity.UserID || !tombstone.ReleasedAt.Equal(r.delete.ReleasedAt) {
			return errors.New("resumeapi: delete tombstone proof does not match")
		}
	}
	if r.delete.PhotoKey != "" {
		job, err := store.New(r.pool).GetMediaDeletionJobByObjectKey(ctx, store.GetMediaDeletionJobByObjectKeyParams{
			ResumeID: r.delete.ResumeID, ObjectKey: r.delete.PhotoKey,
		})
		if err != nil {
			return fmt.Errorf("resumeapi: prove delete media job: %w", err)
		}
		if job.ResumeID != r.delete.ResumeID || job.ObjectKey != r.delete.PhotoKey {
			return errors.New("resumeapi: delete media job proof does not match")
		}
	}
	return nil
}

func (r mutationRecovery) proveCommittedPublish(ctx context.Context, q *store.Queries) error {
	row, err := q.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: r.publish.ResumeID, UserID: r.identity.UserID})
	if err != nil {
		return fmt.Errorf("resumeapi: prove publish resume state: %w", err)
	}
	if row.Revision != r.publish.Effective.Revision || row.Live != r.publish.Effective.Live ||
		row.DownloadEnabled != r.publish.Effective.DownloadEnabled || row.SEOGeoEnabled != r.publish.Effective.SEOGeoEnabled ||
		!equalOptionalString(row.Slug, r.publish.Effective.Slug) {
		return errors.New("resumeapi: publish recovery row does not match intended state")
	}
	if r.publish.Effective.Slug != nil {
		claim, claimErr := q.GetSlugClaim(ctx, *r.publish.Effective.Slug)
		if claimErr != nil || claim != r.publish.ResumeID {
			return errors.New("resumeapi: publish recovery claim does not match intended state")
		}
	}
	if r.publish.OldSlug != nil {
		tombstone, tombstoneErr := q.GetSlugTombstoneForUpdate(ctx, *r.publish.OldSlug)
		if tombstoneErr != nil || tombstone.ReleasedByUserID == nil || *tombstone.ReleasedByUserID != r.identity.UserID || !tombstone.ReleasedAt.Equal(r.publish.ReleasedAt) {
			return errors.New("resumeapi: publish recovery tombstone does not match intended state")
		}
	}
	return nil
}

func equalOptionalString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (r mutationRecovery) notCommittedProof(ctx context.Context, q *store.Queries) (publicstate.RecoveryProof, error) {
	state := publicstate.CommittedState{ResumeRevisions: make(map[uuid.UUID]int64)}
	for _, target := range r.plan.Resumes {
		row, err := q.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: target.ID, UserID: r.identity.UserID})
		if err != nil {
			return publicstate.RecoveryProof{}, fmt.Errorf("resumeapi: prove uncommitted resume state: %w", err)
		}
		if row.Revision != target.ExpectedRevision {
			return publicstate.RecoveryProof{}, errors.New("resumeapi: uncommitted recovery revision changed")
		}
		state.ResumeRevisions[target.ID] = row.Revision
	}
	if r.plan.DiscoveryGeneration != nil {
		public, err := q.GetPublicState(ctx)
		if err != nil {
			return publicstate.RecoveryProof{}, fmt.Errorf("resumeapi: prove uncommitted discovery state: %w", err)
		}
		if public.DiscoveryGeneration != *r.plan.DiscoveryGeneration {
			return publicstate.RecoveryProof{}, errors.New("resumeapi: uncommitted recovery discovery generation changed")
		}
		state.DiscoveryGeneration = &public.DiscoveryGeneration
	}
	return publicstate.RecoveryProof{Disposition: publicstate.RecoveryNotCommitted, State: state}, nil
}

func (r *mutationRecovery) recoveredResponse() (resume.StoredResponse, bool) {
	if r.response == nil {
		return resume.StoredResponse{}, false
	}
	return *r.response, true
}

var _ publicstate.RecoveryResolver = (*mutationRecovery)(nil)

func storedResponseFromRecord(record store.IdempotencyRecord) (resume.StoredResponse, error) {
	headers := map[string]string{}
	if err := json.Unmarshal(record.ResponseHeaders, &headers); err != nil {
		return resume.StoredResponse{}, err
	}
	return resume.StoredResponse{Status: int(record.ResponseStatus), Body: record.ResponseBody, Headers: headers}, nil
}
