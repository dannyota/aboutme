package privacyretention

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"danny.vn/aboutme/apps/server/internal/store"
)

// ExpireIdempotency deletes expired replay records under the same user-first
// lock order used by writers and releases exact retained usage in each page's
// transaction.
func (w *Worker) ExpireIdempotency(ctx context.Context) (result Result, returnErr error) {
	started := time.Now()
	defer func() {
		result.DurationMilliseconds = time.Since(started).Milliseconds()
		w.logResult(ctx, "idempotency-expiry-sweep", result)
	}()

	runCtx, cancel := context.WithTimeout(ctx, maxRunDuration)
	defer cancel()
	conn, err := w.pool.Acquire(runCtx)
	if err != nil {
		result.Failures = 1
		return result, ErrSweepFailed
	}
	defer conn.Release()
	q := store.New(conn)
	locked, err := q.TryLockIdempotencyExpirySweep(runCtx)
	if err != nil {
		result.Failures = 1
		return result, ErrSweepFailed
	}
	if !locked {
		result.Skipped = 1
		return result, nil
	}
	defer func() {
		if unlockErr := w.unlockIdempotency(ctx, conn, q); unlockErr != nil {
			result.Successes = 0
			result.Failures = 1
			if returnErr == nil {
				returnErr = ErrSweepFailed
			}
		}
	}()
	if w.hooks.afterIdempotencyLock != nil {
		w.hooks.afterIdempotencyLock()
	}

	now := w.now()
	for result.IdempotencyRecordsDeleted < maxRowsPerRun {
		deleted, deletedBytes, pageErr := w.expireIdempotencyPage(runCtx, conn, now, pageSize)
		if pageErr != nil {
			result.Failures = 1
			return result, ErrSweepFailed
		}
		if deleted == 0 {
			break
		}
		result.Pages++
		result.IdempotencyRecordsDeleted += deleted
		result.IdempotencyBytesReleased += deletedBytes
		if deleted < int64(pageSize) {
			break
		}
	}
	backlog, err := q.GetIdempotencyExpiryBacklog(runCtx, now)
	if err != nil {
		result.Failures = 1
		return result, ErrSweepFailed
	}
	result.IdempotencyBacklog = backlog.Backlog
	result.IdempotencyOldestExpiredSeconds = backlog.OldestAgeSeconds
	result.Successes = 1
	return result, nil
}

func (w *Worker) expireIdempotencyPage(ctx context.Context, conn *pgxpool.Conn, cutoff time.Time, limit int32) (deleted, deletedBytes int64, returnErr error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if rollbackErr := rollbackBounded(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && returnErr == nil {
			returnErr = rollbackErr
		}
	}()
	row, err := store.New(tx).ExpireIdempotencyPage(ctx, store.ExpireIdempotencyPageParams{
		Cutoff: cutoff, LimitRows: limit,
	})
	if err != nil {
		return 0, 0, err
	}
	if row.DeletedRecords != row.ReleasedRecords || row.DeletedBytes != row.ReleasedBytes {
		return 0, 0, ErrSweepFailed
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return row.DeletedRecords, row.DeletedBytes, nil
}

func (w *Worker) unlockIdempotency(ctx context.Context, conn *pgxpool.Conn, q *store.Queries) error {
	return unlockOrDiscard(ctx, conn, func(unlockCtx context.Context) (bool, error) {
		if w.hooks.unlockIdempotency != nil {
			return w.hooks.unlockIdempotency(unlockCtx, conn, q)
		}
		return q.UnlockIdempotencyExpirySweep(unlockCtx)
	})
}

func (w *Worker) unlockRetention(ctx context.Context, conn *pgxpool.Conn, q *store.Queries) error {
	return unlockOrDiscard(ctx, conn, func(unlockCtx context.Context) (bool, error) {
		if w.hooks.unlockRetention != nil {
			return w.hooks.unlockRetention(unlockCtx, conn, q)
		}
		return q.UnlockPrivacyRetentionSweep(unlockCtx)
	})
}

func unlockOrDiscard(ctx context.Context, conn *pgxpool.Conn, unlock func(context.Context) (bool, error)) error {
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	unlocked, err := unlock(unlockCtx)
	if err == nil && unlocked {
		return nil
	}

	raw := conn.Hijack()
	closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer closeCancel()
	if closeErr := raw.Close(closeCtx); closeErr != nil {
		return ErrSweepFailed
	}
	return ErrSweepFailed
}

func rollbackBounded(ctx context.Context, tx pgx.Tx) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	return tx.Rollback(rollbackCtx)
}
