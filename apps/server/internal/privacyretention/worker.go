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
	pageSize             int32 = 1_000
	maxRowsPerRun        int64 = 10_000
	oauthCleanupBatch          = 200
	maxRunDuration             = 30 * time.Minute
	cleanupTimeout             = 5 * time.Second
	auditRetention             = 180 * 24 * time.Hour
	slugReservation            = 180 * 24 * time.Hour
	viewCountDays              = 400
	oauthClientIdle            = 24 * time.Hour
	sessionRedactionLead       = 48 * time.Hour
	sessionRetention           = 7 * 24 * time.Hour
)

var (
	ErrInvalidConfig = errors.New("privacy retention: invalid config")
	ErrSweepFailed   = errors.New("privacy retention: sweep failed")
)

type Config struct {
	Pool   *store.Pool
	Logger *slog.Logger
	Now    func() time.Time
}

type Result struct {
	Successes int64 `json:"successes"`
	Failures  int64 `json:"failures"`
	Skipped   int64 `json:"overlapSkipped"`
	Pages     int64 `json:"pages"`

	IdempotencyRecordsDeleted       int64 `json:"idempotencyRecordsDeleted"`
	IdempotencyBytesReleased        int64 `json:"idempotencyBytesReleased"`
	IdempotencyBacklog              int64 `json:"idempotencyBacklog"`
	IdempotencyOldestExpiredSeconds int64 `json:"idempotencyOldestExpiredSeconds"`

	SessionMetadataRedacted                   int64 `json:"sessionMetadataRedacted"`
	SessionMetadataBacklog                    int64 `json:"sessionMetadataBacklog"`
	SessionOldestAgeSeconds                   int64 `json:"sessionOldestAgeSeconds"`
	ExpiredSessionsDeleted                    int64 `json:"expiredSessionsDeleted"`
	ExpiredSessionsBacklog                    int64 `json:"expiredSessionsBacklog"`
	ExpiredSessionsOldestAgeSeconds           int64 `json:"expiredSessionsOldestAgeSeconds"`
	LifecycleAuditDeleted                     int64 `json:"lifecycleAuditDeleted"`
	LifecycleAuditBacklog                     int64 `json:"lifecycleAuditBacklog"`
	LifecycleAuditOldestSeconds               int64 `json:"lifecycleAuditOldestSeconds"`
	CompletedMediaJobsDeleted                 int64 `json:"completedMediaJobsDeleted"`
	CompletedMediaJobsBacklog                 int64 `json:"completedMediaJobsBacklog"`
	CompletedMediaOldestSeconds               int64 `json:"completedMediaOldestSeconds"`
	AuthenticationSecurityEventsDeleted       int64 `json:"authenticationSecurityEventsDeleted"`
	AuthenticationSecurityEventsBacklog       int64 `json:"authenticationSecurityEventsBacklog"`
	AuthenticationSecurityEventsOldestSeconds int64 `json:"authenticationSecurityEventsOldestSeconds"`
	ProjectSlugHoldsDeleted                   int64 `json:"projectSlugHoldsDeleted"`
	SlugTombstonesDeleted                     int64 `json:"slugTombstonesDeleted"`
	SlugTombstonesBacklog                     int64 `json:"slugTombstonesBacklog"`
	SlugTombstonesOldestSeconds               int64 `json:"slugTombstonesOldestSeconds"`
	ViewCountDaysDeleted                      int64 `json:"viewCountDaysDeleted"`
	ShareSignalDaysDeleted                    int64 `json:"shareSignalDaysDeleted"`

	OAuthTransactionsDeleted       int64 `json:"oauthTransactionsDeleted"`
	OAuthAuthorizationCodesDeleted int64 `json:"oauthAuthorizationCodesDeleted"`
	OAuthTokensDeleted             int64 `json:"oauthTokensDeleted"`
	OAuthClientsDeleted            int64 `json:"oauthClientsDeleted"`

	DurationMilliseconds int64 `json:"durationMilliseconds"`
}

type Worker struct {
	pool   *store.Pool
	logger *slog.Logger
	now    func() time.Time

	hooks workerHooks
}

type workerHooks struct {
	afterIdempotencyLock func()
	afterIdempotencyPage func(deleted int64)
	afterRetentionLock   func()
	unlockIdempotency    func(context.Context, *pgxpool.Conn, *store.Queries) (bool, error)
	unlockRetention      func(context.Context, *pgxpool.Conn, *store.Queries) (bool, error)
}

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
		"expired_sessions_deleted", result.ExpiredSessionsDeleted,
		"expired_sessions_backlog", result.ExpiredSessionsBacklog,
		"expired_sessions_oldest_age_seconds", result.ExpiredSessionsOldestAgeSeconds,
		"lifecycle_audit_deleted", result.LifecycleAuditDeleted,
		"lifecycle_audit_backlog", result.LifecycleAuditBacklog,
		"lifecycle_audit_oldest_seconds", result.LifecycleAuditOldestSeconds,
		"completed_media_jobs_deleted", result.CompletedMediaJobsDeleted,
		"completed_media_jobs_backlog", result.CompletedMediaJobsBacklog,
		"completed_media_oldest_seconds", result.CompletedMediaOldestSeconds,
		"authentication_security_events_deleted", result.AuthenticationSecurityEventsDeleted,
		"authentication_security_events_backlog", result.AuthenticationSecurityEventsBacklog,
		"authentication_security_events_oldest_seconds", result.AuthenticationSecurityEventsOldestSeconds,
		"project_slug_holds_deleted", result.ProjectSlugHoldsDeleted,
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
