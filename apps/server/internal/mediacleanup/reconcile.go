package mediacleanup

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Reconcile scans at most 10,000 objects from the durable cursor. A dry run
// performs the same listing, validation, age and database authority reads but
// changes no object, job, audit row, or cursor.
func (w *Worker) Reconcile(ctx context.Context, dryRun bool) (result Result, err error) {
	started := w.now()
	defer func() { result.Duration = durationSince(w.now, started) }()

	lockConnection, acquired, err := w.acquireRunLock(ctx, orphanLock)
	if err != nil {
		return result, err
	}
	if !acquired {
		result.Overlap = true
		return result, nil
	}
	defer func() {
		if releaseErr := w.releaseRunLock(ctx, lockConnection, orphanLock); releaseErr != nil {
			result.Failed++
			if err == nil {
				err = releaseErr
			} else {
				err = errors.Join(err, releaseErr)
			}
		}
	}()

	queries := store.New(w.pool)
	cursor, queryErr := queries.GetMediaOrphanSweepCursor(ctx)
	if queryErr != nil {
		return result, ErrDatabase
	}
	if cursor != "" {
		if _, parseErr := parseListedKey(cursor); parseErr != nil {
			return result, ErrBackend
		}
	}

	for result.Scanned < orphanRunLimit && ctx.Err() == nil {
		listCtx, cancel := context.WithTimeout(ctx, objectDeadline)
		objects, nextCursor, listErr := w.media.ListPage(listCtx, "resumes/", cursor, orphanPageSize)
		cancel()
		if listErr != nil {
			w.logger.WarnContext(ctx, "mediacleanup: media listing failed")
			return result, ErrBackend
		}
		validated, validationErr := validateListedPage(cursor, objects, nextCursor)
		if validationErr != nil {
			return result, validationErr
		}
		result.Scanned += int64(len(validated))

		candidates := make([]listedObject, 0, len(validated))
		cutoff := w.now().Add(-orphanMinimumAge)
		for _, object := range validated {
			if object.object.UpdatedAt.After(cutoff) {
				continue
			}
			classification, classifyErr := queries.ClassifyMediaObject(ctx, object.object.Key)
			if classifyErr != nil {
				return result, ErrDatabase
			}
			switch {
			case classification.Live:
				result.Live++
			case classification.Queued:
				result.Queued++
			default:
				candidates = append(candidates, object)
			}
		}
		result.Candidates += int64(len(candidates))

		if !dryRun {
			pageSafe := true
			for _, item := range processBounded(ctx, candidates, w.reconcileObject) {
				addItemResult(&result, item)
				if item.unsafe {
					pageSafe = false
				}
			}
			if !pageSafe {
				return result, ErrDatabase
			}
		}

		if nextCursor == "" {
			if !dryRun {
				if saveErr := saveOrphanCursor(ctx, queries, "", w.now()); saveErr != nil {
					return result, saveErr
				}
			}
			return result, nil
		}
		cursor = nextCursor
		if result.Scanned >= orphanRunLimit {
			result.Backlog = 1
		}
		if !dryRun {
			if saveErr := saveOrphanCursor(ctx, queries, cursor, w.now()); saveErr != nil {
				return result, saveErr
			}
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

func (w *Worker) reconcileObject(ctx context.Context, object listedObject) runItemResult {
	queries := store.New(w.pool)
	leaseID := uuid.New()
	now := w.now()
	job, err := queries.CreateAndClaimOrphanMediaDeletion(ctx, store.CreateAndClaimOrphanMediaDeletionParams{
		ResumeID: object.resumeID, ObjectKey: object.object.Key, Now: now,
		LeaseID: leaseID, LeaseExpiresAt: now.Add(leaseDuration),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			classification, classifyErr := queries.ClassifyMediaObject(ctx, object.object.Key)
			if classifyErr != nil {
				return runItemResult{failed: true, unsafe: true}
			}
			if classification.Live {
				return runItemResult{live: true}
			}
			if classification.Queued {
				return runItemResult{queued: true}
			}
			return runItemResult{failed: true, unsafe: true}
		}
		return runItemResult{failed: true, unsafe: true}
	}
	result := runItemResult{enqueued: true}
	if w.hooks.afterOrphanClaim != nil {
		w.hooks.afterOrphanClaim(job)
	}

	for attempt := 0; attempt < 3; attempt++ {
		live, liveErr := queries.MediaObjectHasLiveReference(ctx, object.object.Key)
		if liveErr != nil {
			requeued := w.requeueFailure(ctx, queries, job, leaseID, true)
			requeued.enqueued = true
			return requeued
		}
		if live {
			requeued := w.requeueFailure(ctx, queries, job, leaseID, true)
			requeued.enqueued = true
			requeued.live = true
			return requeued
		}

		deleteCtx, cancel := context.WithTimeout(ctx, objectDeadline)
		deleteErr := w.media.Delete(deleteCtx, object.object.Key)
		cancel()
		if deleteErr == nil || errors.Is(deleteErr, media.ErrNotFound) {
			outcome := "deleted"
			if errors.Is(deleteErr, media.ErrNotFound) {
				outcome = "absent"
			}
			finalizeCtx, finalizeCancel := cleanupContext(ctx)
			completed, completeErr := queries.CompleteClaimedMediaDeletion(finalizeCtx, store.CompleteClaimedMediaDeletionParams{
				Now: w.now(), Outcome: outcome, ID: job.ID, LeaseID: leaseID,
			})
			finalizeCancel()
			if completeErr != nil || !completed {
				result.failed = true
				return result
			}
			result.succeeded = true
			result.deleted = outcome == "deleted"
			result.absent = outcome == "absent"
			return result
		}
		if attempt < 2 && w.wait(ctx, time.Duration(1<<attempt)*time.Second) {
			continue
		}
		break
	}
	w.logger.WarnContext(ctx, "mediacleanup: orphan media deletion failed")
	requeued := w.requeueFailure(ctx, queries, job, leaseID, true)
	requeued.enqueued = true
	return requeued
}

func saveOrphanCursor(ctx context.Context, queries *store.Queries, cursor string, now time.Time) error {
	rows, err := queries.SaveMediaOrphanSweepCursor(ctx, store.SaveMediaOrphanSweepCursorParams{Cursor: cursor, Now: now})
	if err != nil || rows != 1 {
		return ErrDatabase
	}
	return nil
}

func waitBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
