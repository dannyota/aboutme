// Package privacyretention runs bounded privacy lifecycle sweeps.
package privacyretention

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	pageSize          int32 = 1_000
	maxRowsPerRun     int64 = 10_000
	oauthCleanupBatch       = 200
	maxRunDuration          = 30 * time.Minute
	cleanupTimeout          = 5 * time.Second
	auditRetention          = 180 * 24 * time.Hour
	slugReservation         = 180 * 24 * time.Hour
	// viewCountDays is how long daily view counts are kept
	// (docs/design/viewer-analytics/README.md, "Rules"). They are not
	// personal data; the sweep bounds their growth.
	viewCountDays   = 400
	oauthClientIdle = 24 * time.Hour
	// sessionRedactionLead covers the daily sweep schedule plus one missed
	// run, so a session's IP and user agent are still redacted by its
	// absolute expiry (90 days after sign-in, docs/design/security.md) even
	// when a run is skipped.
	sessionRedactionLead = 48 * time.Hour
)

var (
	// ErrInvalidConfig reports a missing Worker dependency.
	ErrInvalidConfig = errors.New("privacy retention: invalid config")
	// ErrSweepFailed deliberately omits database details from command output.
	ErrSweepFailed = errors.New("privacy retention: sweep failed")
)

// Config supplies the retention worker's fixed dependencies.
type Config struct {
	// Pool supplies the connection that holds each sweep's advisory lock.
	Pool *store.Pool
	// Logger receives one result record per sweep.
	Logger *slog.Logger
	// Now returns the reference time for each sweep's cutoffs.
	Now func() time.Time
}

// Result is the fixed, identifier-free signal surface shared by both sweep
// commands. Fields irrelevant to one command remain zero.
type Result struct {
	// Successes is 1 when the sweep completed and released its advisory lock.
	Successes int64 `json:"successes"`
	// Failures is 1 when the sweep or its unlock failed.
	Failures int64 `json:"failures"`
	// Skipped is 1 when another run held the sweep's advisory lock.
	Skipped int64 `json:"overlapSkipped"`
	// Pages counts the non-empty pages the sweep processed.
	Pages int64 `json:"pages"`

	// IdempotencyRecordsDeleted counts expired replay records deleted.
	IdempotencyRecordsDeleted int64 `json:"idempotencyRecordsDeleted"`
	// IdempotencyBytesReleased counts stored response bytes released from usage.
	IdempotencyBytesReleased int64 `json:"idempotencyBytesReleased"`
	// IdempotencyBacklog counts expired replay records left after the run.
	IdempotencyBacklog int64 `json:"idempotencyBacklog"`
	// IdempotencyOldestExpiredSeconds is how long the oldest remaining
	// expired record has been past its expiry, in seconds.
	IdempotencyOldestExpiredSeconds int64 `json:"idempotencyOldestExpiredSeconds"`

	// SessionMetadataRedacted counts sessions whose IP and user agent were cleared.
	SessionMetadataRedacted int64 `json:"sessionMetadataRedacted"`
	// SessionMetadataBacklog counts sessions still due for redaction.
	SessionMetadataBacklog int64 `json:"sessionMetadataBacklog"`
	// SessionOldestAgeSeconds is the time since sign-in of the oldest
	// session still due for redaction, in seconds.
	SessionOldestAgeSeconds int64 `json:"sessionOldestAgeSeconds"`
	// LifecycleAuditDeleted counts lifecycle audit events deleted past retention.
	LifecycleAuditDeleted int64 `json:"lifecycleAuditDeleted"`
	// LifecycleAuditBacklog counts lifecycle audit events left past retention.
	LifecycleAuditBacklog int64 `json:"lifecycleAuditBacklog"`
	// LifecycleAuditOldestSeconds is the age of the oldest such event, in seconds.
	LifecycleAuditOldestSeconds int64 `json:"lifecycleAuditOldestSeconds"`
	// CompletedMediaJobsDeleted counts completed media deletion jobs deleted
	// past retention.
	CompletedMediaJobsDeleted int64 `json:"completedMediaJobsDeleted"`
	// CompletedMediaJobsBacklog counts completed media deletion jobs left
	// past retention.
	CompletedMediaJobsBacklog int64 `json:"completedMediaJobsBacklog"`
	// CompletedMediaOldestSeconds is the time since completion of the oldest
	// such job, in seconds.
	CompletedMediaOldestSeconds int64 `json:"completedMediaOldestSeconds"`
	// AuthenticationSecurityEventsDeleted counts authentication security
	// events deleted past retention.
	AuthenticationSecurityEventsDeleted int64 `json:"authenticationSecurityEventsDeleted"`
	// AuthenticationSecurityEventsBacklog counts authentication security
	// events left past retention.
	AuthenticationSecurityEventsBacklog int64 `json:"authenticationSecurityEventsBacklog"`
	// AuthenticationSecurityEventsOldestSeconds is the age of the oldest such
	// event, in seconds.
	AuthenticationSecurityEventsOldestSeconds int64 `json:"authenticationSecurityEventsOldestSeconds"`
	// SlugTombstonesDeleted counts slug tombstones deleted after their
	// reservation period.
	SlugTombstonesDeleted int64 `json:"slugTombstonesDeleted"`
	// SlugTombstonesBacklog counts slug tombstones left past their
	// reservation period.
	SlugTombstonesBacklog int64 `json:"slugTombstonesBacklog"`
	// SlugTombstonesOldestSeconds is the time since release of the oldest
	// such tombstone, in seconds.
	SlugTombstonesOldestSeconds int64 `json:"slugTombstonesOldestSeconds"`
	// ViewCountDaysDeleted counts daily view-count rows deleted past their
	// keep period.
	ViewCountDaysDeleted int64 `json:"viewCountDaysDeleted"`
	// ShareSignalDaysDeleted counts daily share-signal rows deleted past
	// their keep period.
	ShareSignalDaysDeleted int64 `json:"shareSignalDaysDeleted"`

	// OAuthTransactionsDeleted counts expired OAuth transactions deleted.
	OAuthTransactionsDeleted int64 `json:"oauthTransactionsDeleted"`
	// OAuthAuthorizationCodesDeleted counts expired authorization codes deleted.
	OAuthAuthorizationCodesDeleted int64 `json:"oauthAuthorizationCodesDeleted"`
	// OAuthTokensDeleted counts terminal OAuth tokens deleted.
	OAuthTokensDeleted int64 `json:"oauthTokensDeleted"`
	// OAuthClientsDeleted counts idle OAuth clients deleted.
	OAuthClientsDeleted int64 `json:"oauthClientsDeleted"`

	// DurationMilliseconds is the sweep's wall-clock duration.
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
		"authentication_security_events_deleted", result.AuthenticationSecurityEventsDeleted,
		"authentication_security_events_backlog", result.AuthenticationSecurityEventsBacklog,
		"authentication_security_events_oldest_seconds", result.AuthenticationSecurityEventsOldestSeconds,
		"slug_tombstones_deleted", result.SlugTombstonesDeleted,
		"slug_tombstones_backlog", result.SlugTombstonesBacklog,
		"slug_tombstones_oldest_seconds", result.SlugTombstonesOldestSeconds,
		"oauth_transactions_deleted", result.OAuthTransactionsDeleted,
		"oauth_authorization_codes_deleted", result.OAuthAuthorizationCodesDeleted,
		"oauth_tokens_deleted", result.OAuthTokensDeleted,
		"oauth_clients_deleted", result.OAuthClientsDeleted,
		"duration_ms", result.DurationMilliseconds,
	)
}
