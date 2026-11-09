package privacyretention

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

// Retain redacts or deletes the daily privacy categories and performs one
// existing 200-row OAuth cleanup pass. Password and auth-mail cleanup remains
// owned by authmail.Worker.
func (w *Worker) Retain(ctx context.Context) (result Result, returnErr error) {
	started := time.Now()
	defer func() {
		result.DurationMilliseconds = time.Since(started).Milliseconds()
		w.logResult(ctx, "privacy-retention-sweep", result)
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
	locked, err := q.TryLockPrivacyRetentionSweep(runCtx)
	if err != nil {
		result.Failures = 1
		return result, ErrSweepFailed
	}
	if !locked {
		result.Skipped = 1
		return result, nil
	}
	defer func() {
		if unlockErr := w.unlockRetention(ctx, conn, q); unlockErr != nil {
			result.Successes = 0
			result.Failures = 1
			if returnErr == nil {
				returnErr = ErrSweepFailed
			}
		}
	}()
	if w.hooks.afterRetentionLock != nil {
		w.hooks.afterRetentionLock()
	}

	now := w.now()
	result.SessionMetadataRedacted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.RedactSessionMetadataPage(ctx, store.RedactSessionMetadataPageParams{Cutoff: now.Add(sessionRedactionLead), LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}
	result.LifecycleAuditDeleted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.DeleteLifecycleAuditPage(ctx, store.DeleteLifecycleAuditPageParams{Cutoff: now.Add(-auditRetention), LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}
	result.CompletedMediaJobsDeleted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.DeleteCompletedMediaJobsPage(ctx, store.DeleteCompletedMediaJobsPageParams{Cutoff: now.Add(-auditRetention), LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}
	result.AuthenticationSecurityEventsDeleted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.DeleteAuthenticationSecurityEventsPage(ctx, store.DeleteAuthenticationSecurityEventsPageParams{Cutoff: now.Add(-auditRetention), LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}
	result.SlugTombstonesDeleted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.DeleteExpiredSlugTombstonesPage(ctx, store.DeleteExpiredSlugTombstonesPageParams{Cutoff: now.Add(-slugReservation), LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}

	viewCutoff := viewcount.Date(viewcount.Day(now).AddDate(0, 0, -viewCountDays))
	result.ViewCountDaysDeleted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.DeleteOldResumeViewDaysPage(ctx, store.DeleteOldResumeViewDaysPageParams{Cutoff: viewCutoff, LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}
	result.ShareSignalDaysDeleted, result.Pages, err = runPages(runCtx, result.Pages, func(ctx context.Context, limit int32) (int64, error) {
		return q.DeleteOldResumeShareSignalDaysPage(ctx, store.DeleteOldResumeShareSignalDaysPageParams{Cutoff: viewCutoff, LimitRows: limit})
	})
	if err != nil {
		return w.failed(result)
	}

	if err := w.cleanupOAuth(runCtx, conn, q, now, &result); err != nil {
		return w.failed(result)
	}
	if err := w.loadRetentionBacklog(runCtx, q, now, &result); err != nil {
		return w.failed(result)
	}
	result.Successes = 1
	return result, nil
}

func runPages(ctx context.Context, pages int64, run func(context.Context, int32) (int64, error)) (total, updatedPages int64, err error) {
	updatedPages = pages
	for total < maxRowsPerRun {
		count, runErr := run(ctx, pageSize)
		if runErr != nil {
			return total, updatedPages, runErr
		}
		if count == 0 {
			return total, updatedPages, nil
		}
		total += count
		updatedPages++
		if count < int64(pageSize) {
			return total, updatedPages, nil
		}
	}
	return total, updatedPages, nil
}

func (w *Worker) cleanupOAuth(ctx context.Context, conn *pgxpool.Conn, q *store.Queries, now time.Time, result *Result) (returnErr error) {
	var err error
	result.OAuthTransactionsDeleted, err = q.DeleteExpiredOAuthTransactions(ctx, store.DeleteExpiredOAuthTransactionsParams{
		Cutoff: now, MaxRows: oauthCleanupBatch,
	})
	if err != nil {
		return err
	}
	result.OAuthAuthorizationCodesDeleted, err = q.DeleteExpiredOAuthAuthorizationCodes(ctx, store.DeleteExpiredOAuthAuthorizationCodesParams{
		Cutoff: now, LimitRows: oauthCleanupBatch,
	})
	if err != nil {
		return err
	}
	result.OAuthTokensDeleted, err = q.DeleteTerminalOAuthTokens(ctx, store.DeleteTerminalOAuthTokensParams{
		Cutoff: now, LimitRows: oauthCleanupBatch,
	})
	if err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rollbackErr := rollbackBounded(ctx, tx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && returnErr == nil {
			returnErr = rollbackErr
		}
	}()
	txq := store.New(tx)
	ids, err := txq.ListIdleOAuthClientCandidates(ctx, store.ListIdleOAuthClientCandidatesParams{
		IdleBefore: now.Add(-oauthClientIdle), Now: now, LimitRows: oauthCleanupBatch,
	})
	if err != nil {
		return err
	}
	if len(ids) > oauthCleanupBatch {
		return ErrSweepFailed
	}
	if len(ids) > 0 {
		result.OAuthClientsDeleted, err = txq.DeleteOAuthClients(ctx, ids)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (w *Worker) loadRetentionBacklog(ctx context.Context, q *store.Queries, now time.Time, result *Result) error {
	sessions, err := q.GetSessionMetadataBacklog(ctx, store.GetSessionMetadataBacklogParams{
		Now: now, Cutoff: now.Add(sessionRedactionLead),
	})
	if err != nil {
		return err
	}
	audits, err := q.GetLifecycleAuditBacklog(ctx, store.GetLifecycleAuditBacklogParams{
		Now: now, Cutoff: now.Add(-auditRetention),
	})
	if err != nil {
		return err
	}
	media, err := q.GetCompletedMediaJobsBacklog(ctx, store.GetCompletedMediaJobsBacklogParams{
		Now: now, Cutoff: now.Add(-auditRetention),
	})
	if err != nil {
		return err
	}
	events, err := q.GetAuthenticationSecurityEventsBacklog(ctx, store.GetAuthenticationSecurityEventsBacklogParams{
		Now: now, Cutoff: now.Add(-auditRetention),
	})
	if err != nil {
		return err
	}
	tombstones, err := q.GetExpiredSlugTombstonesBacklog(ctx, store.GetExpiredSlugTombstonesBacklogParams{
		Now: now, Cutoff: now.Add(-slugReservation),
	})
	if err != nil {
		return err
	}
	result.SessionMetadataBacklog = sessions.Backlog
	result.SessionOldestAgeSeconds = sessions.OldestAgeSeconds
	result.LifecycleAuditBacklog = audits.Backlog
	result.LifecycleAuditOldestSeconds = audits.OldestAgeSeconds
	result.CompletedMediaJobsBacklog = media.Backlog
	result.CompletedMediaOldestSeconds = media.OldestAgeSeconds
	result.AuthenticationSecurityEventsBacklog = events.Backlog
	result.AuthenticationSecurityEventsOldestSeconds = events.OldestAgeSeconds
	result.SlugTombstonesBacklog = tombstones.Backlog
	result.SlugTombstonesOldestSeconds = tombstones.OldestAgeSeconds
	return nil
}

func (w *Worker) failed(result Result) (Result, error) {
	result.Failures = 1
	return result, ErrSweepFailed
}
