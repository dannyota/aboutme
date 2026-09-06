package mediacleanup

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// DeleteDue drains at most 2,000 jobs that were due when this run began. It
// claims only one four-worker wave at a time so no job waits behind another
// wave while its 30-second lease runs down.
func (w *Worker) DeleteDue(ctx context.Context) (result Result, err error) {
	started := w.now()
	defer func() { result.Duration = durationSince(w.now, started) }()

	lockConnection, acquired, err := w.acquireRunLock(ctx, deletionLock)
	if err != nil {
		return result, err
	}
	if !acquired {
		result.Overlap = true
		return result, nil
	}
	defer func() {
		if releaseErr := w.releaseRunLock(ctx, lockConnection, deletionLock); releaseErr != nil {
			result.Failed++
			if err == nil {
				err = releaseErr
			} else {
				err = errors.Join(err, releaseErr)
			}
		}
	}()

	queries := store.New(w.pool)
	dueAt := w.now()
	marked := int64(0)
	for marked < deletionRunLimit {
		now := w.now()
		count, markErr := queries.MarkMediaDeletionOverduePage(ctx, store.MarkMediaDeletionOverduePageParams{
			Cutoff: now.Add(-overdueAge), MaxRows: deletionPageSize, Now: now,
		})
		if markErr != nil {
			return result, ErrDatabase
		}
		marked += count
		if count < deletionPageSize {
			break
		}
	}
	for result.Claimed < deletionRunLimit && ctx.Err() == nil {
		waveSize := min(workerCount, deletionPageSize, deletionRunLimit-int(result.Claimed))
		leaseID := uuid.New()
		now := w.now()
		jobs, queryErr := queries.ClaimMediaDeletionJobs(ctx, store.ClaimMediaDeletionJobsParams{
			LeaseID: leaseID, LeaseExpiresAt: now.Add(leaseDuration),
			DueAt: dueAt, Now: now, MaxRows: int32(waveSize),
		})
		if queryErr != nil {
			return result, ErrDatabase
		}
		if len(jobs) == 0 {
			break
		}
		result.Claimed += int64(len(jobs))
		for _, item := range processBounded(ctx, jobs, func(itemCtx context.Context, job store.MediaDeletionJob) runItemResult {
			return w.deleteClaimed(itemCtx, job, leaseID)
		}) {
			addItemResult(&result, item)
		}
	}

	stateNow := w.now()
	state, queryErr := queries.GetMediaDeletionQueueState(ctx, store.GetMediaDeletionQueueStateParams{
		Now: stateNow, OverdueCutoff: stateNow.Add(-overdueAge),
	})
	if queryErr != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, ErrDatabase
	}
	result.Backlog = state.Pending
	result.Overdue = state.Overdue
	if state.Pending > 0 {
		if oldest, ok := state.OldestEnqueuedAt.(time.Time); ok && oldest.Before(stateNow) {
			result.OldestAge = stateNow.Sub(oldest)
		}
	}
	return result, nil
}

func (w *Worker) deleteClaimed(ctx context.Context, job store.MediaDeletionJob, leaseID uuid.UUID) runItemResult {
	result := runItemResult{}
	if job.LeaseID == nil || *job.LeaseID != leaseID {
		result.failed = true
		return result
	}
	queries := store.New(w.pool)
	now := w.now()
	if !job.EnqueuedAt.After(now.Add(-overdueAge)) {
		if _, err := queries.MarkClaimedMediaDeletionOverdue(ctx, store.MarkClaimedMediaDeletionOverdueParams{
			Now: now, ID: job.ID, LeaseID: leaseID, Cutoff: now.Add(-overdueAge),
		}); err != nil {
			result.failed = true
			return result
		}
	}
	if _, err := media.ParsePhotoKey(job.ResumeID, job.ObjectKey); err != nil {
		return w.requeueFailure(ctx, queries, job, leaseID, true)
	}
	live, err := queries.MediaObjectHasLiveReference(ctx, job.ObjectKey)
	if err != nil {
		result.failed = true
		return result
	}
	if live {
		result = w.requeueFailure(ctx, queries, job, leaseID, true)
		result.live = true
		return result
	}

	deleteCtx, cancel := context.WithTimeout(ctx, objectDeadline)
	deleteErr := w.media.Delete(deleteCtx, job.ObjectKey)
	cancel()
	if deleteErr != nil && !errors.Is(deleteErr, media.ErrNotFound) {
		w.logger.WarnContext(ctx, "mediacleanup: media deletion failed")
		return w.requeueFailure(ctx, queries, job, leaseID, true)
	}
	outcome := "deleted"
	if errors.Is(deleteErr, media.ErrNotFound) {
		outcome = "absent"
	}
	finalizeCtx, finalizeCancel := cleanupContext(ctx)
	defer finalizeCancel()
	completed, err := queries.CompleteClaimedMediaDeletion(finalizeCtx, store.CompleteClaimedMediaDeletionParams{
		Now: w.now(), Outcome: outcome, ID: job.ID, LeaseID: leaseID,
	})
	if err != nil || !completed {
		result.failed = true
		return result
	}
	result.succeeded = true
	result.deleted = outcome == "deleted"
	result.absent = outcome == "absent"
	return result
}

func (w *Worker) requeueFailure(ctx context.Context, queries *store.Queries, job store.MediaDeletionJob, leaseID uuid.UUID, countFailure bool) runItemResult {
	result := runItemResult{failed: countFailure}
	requeueCtx, cancel := cleanupContext(ctx)
	defer cancel()
	rows, err := queries.RequeueClaimedMediaDeletion(requeueCtx, store.RequeueClaimedMediaDeletionParams{
		NextAttemptAt: w.now().Add(retryDelay(job.AttemptCount)), ID: job.ID, LeaseID: leaseID,
	})
	if err != nil || rows != 1 {
		result.failed = true
	}
	return result
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), objectDeadline)
}
