package authmail

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

// requeueExpired returns every lease whose lease_expires_at has passed back to
// pending in one bounded transaction, rolling back the claim's attempt
// increment (the send never happened).
func (w *Worker) requeueExpired(ctx context.Context, now time.Time) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	qtx := w.queries.WithTx(tx)
	if _, err := qtx.RequeueExpiredAuthEmailLeases(ctx, store.RequeueExpiredAuthEmailLeasesParams{
		Now:           now,
		LimitRows:     staleLeaseBatch,
		IncludeNotice: w.noticeAllowed(ctx, qtx),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// cleanup runs three bounded cleanups, each in its own transaction:
// expired registrations, expired reset tokens, and sent/terminal jobs older
// than seven days. Every delete is capped at cleanupBatch (200).
func (w *Worker) cleanup(ctx context.Context, now time.Time) error {
	if err := w.deleteInTx(ctx, func(qtx *store.Queries) error {
		_, err := qtx.CleanupExpiredPasswordRegistrations(ctx, store.CleanupExpiredPasswordRegistrationsParams{
			Cutoff:    now,
			LimitRows: cleanupBatch,
		})
		return err
	}); err != nil {
		return err
	}
	if err := w.deleteInTx(ctx, func(qtx *store.Queries) error {
		_, err := qtx.CleanupExpiredPasswordResetTokens(ctx, store.CleanupExpiredPasswordResetTokensParams{
			Cutoff:    now,
			LimitRows: cleanupBatch,
		})
		return err
	}); err != nil {
		return err
	}
	return w.deleteInTx(ctx, func(qtx *store.Queries) error {
		_, err := qtx.CleanupFinishedAuthEmailJobs(ctx, store.CleanupFinishedAuthEmailJobsParams{
			Cutoff:        now.Add(-finishedJobRetain),
			LimitRows:     cleanupBatch,
			IncludeNotice: w.noticeManifestMatches(ctx, qtx),
		})
		return err
	})
}

func (w *Worker) deleteInTx(ctx context.Context, fn func(*store.Queries) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	if err := fn(w.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// backoffCap returns min(30s * 2^(attempt-1), 1h) for the attempt that just
// failed. Attempt 1 has a 30s cap; each retry doubles until the one-hour cap.
func backoffCap(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	cap := baseBackoff
	for i := int32(1); i < attempt; i++ {
		cap *= 2
		if cap >= maxBackoff {
			cap = maxBackoff
			break
		}
	}
	return cap
}
