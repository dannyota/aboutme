// Package privacyretention runs bounded privacy lifecycle sweeps.
package privacyretention

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	pageSize          int32 = 1_000
	maxRowsPerRun     int64 = 10_000
	oauthCleanupBatch       = 200
	maxRunDuration          = 30 * time.Minute
	cleanupTimeout          = 5 * time.Second
	sessionRetention        = 90 * 24 * time.Hour
	auditRetention          = 180 * 24 * time.Hour
	oauthClientIdle         = 24 * time.Hour
)

var (
	// ErrInvalidConfig reports a missing Worker dependency.
	ErrInvalidConfig = errors.New("privacy retention: invalid config")
	// ErrSweepFailed deliberately omits database details from command output.
	ErrSweepFailed = errors.New("privacy retention: sweep failed")
)

// Config supplies the retention worker's fixed dependencies.
type Config struct {
	Pool   *store.Pool
	Logger *slog.Logger
	Now    func() time.Time
}

// Result is the fixed, identifier-free signal surface shared by both sweep
// commands. Fields irrelevant to one command remain zero.
type Result struct {
	Successes int64 `json:"successes"`
	Failures  int64 `json:"failures"`
	Skipped   int64 `json:"overlapSkipped"`
	Pages     int64 `json:"pages"`

	IdempotencyRecordsDeleted       int64 `json:"idempotencyRecordsDeleted"`
	IdempotencyBytesReleased        int64 `json:"idempotencyBytesReleased"`
	IdempotencyBacklog              int64 `json:"idempotencyBacklog"`
	IdempotencyOldestExpiredSeconds int64 `json:"idempotencyOldestExpiredSeconds"`

	SessionMetadataRedacted     int64 `json:"sessionMetadataRedacted"`
	SessionMetadataBacklog      int64 `json:"sessionMetadataBacklog"`
	SessionOldestAgeSeconds     int64 `json:"sessionOldestAgeSeconds"`
	LifecycleAuditDeleted       int64 `json:"lifecycleAuditDeleted"`
	LifecycleAuditBacklog       int64 `json:"lifecycleAuditBacklog"`
	LifecycleAuditOldestSeconds int64 `json:"lifecycleAuditOldestSeconds"`
	CompletedMediaJobsDeleted   int64 `json:"completedMediaJobsDeleted"`
	CompletedMediaJobsBacklog   int64 `json:"completedMediaJobsBacklog"`
	CompletedMediaOldestSeconds int64 `json:"completedMediaOldestSeconds"`

	OAuthTransactionsDeleted       int64 `json:"oauthTransactionsDeleted"`
	OAuthAuthorizationCodesDeleted int64 `json:"oauthAuthorizationCodesDeleted"`
	OAuthTokensDeleted             int64 `json:"oauthTokensDeleted"`
	OAuthClientsDeleted            int64 `json:"oauthClientsDeleted"`

	DurationMilliseconds int64 `json:"durationMilliseconds"`
}

// Worker executes one bounded run at a time. PostgreSQL advisory locks also
// prevent overlap between different processes.
type Worker struct {
	pool   *store.Pool
	logger *slog.Logger
	now    func() time.Time

	hooks workerHooks
}

type workerHooks struct {
	afterIdempotencyLock func()
	afterRetentionLock   func()
	unlockIdempotency    func(context.Context, *pgxpool.Conn, *store.Queries) (bool, error)
	unlockRetention      func(context.Context, *pgxpool.Conn, *store.Queries) (bool, error)
}

// New validates the fixed worker dependencies.
func New(config Config) (*Worker, error) {
	if config.Pool == nil || config.Logger == nil || config.Now == nil {
		return nil, ErrInvalidConfig
	}
	return &Worker{pool: config.Pool, logger: config.Logger, now: config.Now}, nil
}

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
		return q.RedactSessionMetadataPage(ctx, store.RedactSessionMetadataPageParams{Cutoff: now.Add(-sessionRetention), LimitRows: limit})
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

func (w *Worker) loadRetentionBacklog(ctx context.Context, q *store.Queries, now time.Time, result *Result) error {
	sessions, err := q.GetSessionMetadataBacklog(ctx, store.GetSessionMetadataBacklogParams{
		Now: now, Cutoff: now.Add(-sessionRetention),
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
	result.SessionMetadataBacklog = sessions.Backlog
	result.SessionOldestAgeSeconds = sessions.OldestAgeSeconds
	result.LifecycleAuditBacklog = audits.Backlog
	result.LifecycleAuditOldestSeconds = audits.OldestAgeSeconds
	result.CompletedMediaJobsBacklog = media.Backlog
	result.CompletedMediaOldestSeconds = media.OldestAgeSeconds
	return nil
}

func (w *Worker) failed(result Result) (Result, error) {
	result.Failures = 1
	return result, ErrSweepFailed
}

func (w *Worker) logResult(ctx context.Context, command string, result Result) {
	level := slog.LevelInfo
	if result.Failures > 0 {
		level = slog.LevelError
	}
	w.logger.Log(ctx, level, "privacy retention sweep",
		"command", command,
		"successes", result.Successes,
		"failures", result.Failures,
		"overlap_skipped", result.Skipped,
		"pages", result.Pages,
		"idempotency_deleted", result.IdempotencyRecordsDeleted,
		"idempotency_bytes_released", result.IdempotencyBytesReleased,
		"idempotency_backlog", result.IdempotencyBacklog,
		"idempotency_oldest_expired_seconds", result.IdempotencyOldestExpiredSeconds,
		"session_metadata_redacted", result.SessionMetadataRedacted,
		"session_metadata_backlog", result.SessionMetadataBacklog,
		"session_oldest_age_seconds", result.SessionOldestAgeSeconds,
		"lifecycle_audit_deleted", result.LifecycleAuditDeleted,
		"lifecycle_audit_backlog", result.LifecycleAuditBacklog,
		"lifecycle_audit_oldest_seconds", result.LifecycleAuditOldestSeconds,
		"completed_media_jobs_deleted", result.CompletedMediaJobsDeleted,
		"completed_media_jobs_backlog", result.CompletedMediaJobsBacklog,
		"completed_media_oldest_seconds", result.CompletedMediaOldestSeconds,
		"oauth_transactions_deleted", result.OAuthTransactionsDeleted,
		"oauth_authorization_codes_deleted", result.OAuthAuthorizationCodesDeleted,
		"oauth_tokens_deleted", result.OAuthTokensDeleted,
		"oauth_clients_deleted", result.OAuthClientsDeleted,
		"duration_ms", result.DurationMilliseconds,
	)
}
