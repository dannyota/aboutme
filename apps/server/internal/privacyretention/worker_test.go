package privacyretention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var retentionNow = time.Date(2001, time.January, 1, 12, 0, 0, 0, time.UTC)

func TestNewValidatesDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := &store.Pool{}
	if _, err := New(Config{Pool: pool, Logger: logger, Now: func() time.Time { return retentionNow }}); err != nil {
		t.Fatalf("New(valid) error = %v", err)
	}
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"nil pool", Config{Logger: logger, Now: func() time.Time { return retentionNow }}},
		{"nil logger", Config{Pool: pool, Now: func() time.Time { return retentionNow }}},
		{"nil clock", Config{Pool: pool, Logger: logger}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestExpireIdempotencyPreservesBoundaryAccountingAndBusyUsers(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker, err := New(Config{Pool: pool, Logger: logger, Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	active := createRetentionUser(ctx, t, q)
	busy := createRetentionUser(ctx, t, q)
	t.Cleanup(func() {
		cleanupUsers(ctx, t, pool, active, busy)
	})

	activeExpiredBytes := insertIdempotency(ctx, t, q, active, retentionNow, json.RawMessage(`null`), json.RawMessage(`{}`))
	activeExpiredBytes += insertIdempotency(ctx, t, q, active, retentionNow.Add(-time.Second), json.RawMessage(`{"ok":true}`), json.RawMessage(`{"ETag":"x"}`))
	activeLiveBytes := insertIdempotency(ctx, t, q, active, retentionNow.Add(time.Second), json.RawMessage(`{"live":true}`), json.RawMessage(`{}`))
	busyExpiredBytes := insertIdempotency(ctx, t, q, busy, retentionNow.Add(-time.Hour), json.RawMessage(`{"busy":true}`), json.RawMessage(`{}`))
	seedUsage(ctx, t, pool, active)
	seedUsage(ctx, t, pool, busy)
	liveBefore, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{UserID: active, Route: "route", IdempotencyKey: liveKeyForUser(active)})
	if err != nil {
		t.Fatalf("read live replay before sweep: %v", err)
	}

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin busy-user lock: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, lockTx) })
	if _, lockErr := lockTx.Exec(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, busy); lockErr != nil {
		t.Fatalf("lock busy user: %v", lockErr)
	}

	result, err := worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency: %v", err)
	}
	if result.IdempotencyRecordsDeleted != 2 || result.IdempotencyBytesReleased != activeExpiredBytes {
		t.Errorf("result accounting = (%d, %d), want (2, %d)", result.IdempotencyRecordsDeleted, result.IdempotencyBytesReleased, activeExpiredBytes)
	}
	if result.IdempotencyBacklog != 1 || result.IdempotencyOldestExpiredSeconds != 3600 {
		t.Errorf("backlog = (%d, %d seconds), want (1, 3600)", result.IdempotencyBacklog, result.IdempotencyOldestExpiredSeconds)
	}
	assertUsage(ctx, t, pool, active, 1, activeLiveBytes)
	assertUsage(ctx, t, pool, busy, 1, busyExpiredBytes)

	if commitErr := lockTx.Commit(ctx); commitErr != nil {
		t.Fatalf("release busy-user lock: %v", commitErr)
	}
	result, err = worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency after unlock: %v", err)
	}
	if result.IdempotencyRecordsDeleted != 1 || result.IdempotencyBytesReleased != busyExpiredBytes || result.IdempotencyBacklog != 0 {
		t.Errorf("second result = %+v", result)
	}
	assertUsage(ctx, t, pool, busy, 0, 0)

	liveAfter, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{UserID: active, Route: "route", IdempotencyKey: liveKeyForUser(active)})
	if err != nil {
		t.Fatalf("unexpired replay record was removed: %v", err)
	}
	if liveAfter.ResponseStatus != liveBefore.ResponseStatus || !bytes.Equal(liveAfter.ResponseBody, liveBefore.ResponseBody) || !bytes.Equal(liveAfter.ResponseHeaders, liveBefore.ResponseHeaders) {
		t.Errorf("unexpired replay changed: before=%+v after=%+v", liveBefore, liveAfter)
	}
}

func TestExpireIdempotencyRollsBackOnMissingUsage(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })
	insertIdempotency(ctx, t, q, userID, retentionNow, json.RawMessage(`{"rollback":true}`), json.RawMessage(`{}`))

	result, err := worker.ExpireIdempotency(ctx)
	if !errors.Is(err, ErrSweepFailed) || result.Failures != 1 {
		t.Fatalf("ExpireIdempotency = (%+v, %v), want fixed failure", result, err)
	}
	var records int64
	if scanErr := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, userID).Scan(&records); scanErr != nil {
		t.Fatalf("count rollback records: %v", scanErr)
	}
	if records != 1 {
		t.Errorf("record count after accounting failure = %d, want 1", records)
	}

	seedUsage(ctx, t, pool, userID)
	result, err = worker.ExpireIdempotency(ctx)
	if err != nil || result.IdempotencyRecordsDeleted != 1 {
		t.Fatalf("sweep after failure released its advisory lock: (%+v, %v)", result, err)
	}
}

func TestExpireIdempotencyStopsAtRunCap(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })
	if _, insertErr := pool.Exec(ctx, `INSERT INTO idempotency_records
		(user_id, route, idempotency_key, request_hash, response_status, response_body, response_headers, expires_at)
		SELECT $1, 'bulk', uuidv7(), decode(repeat('00', 32), 'hex'), 204, 'null'::jsonb, '{}'::jsonb, $2
		FROM generate_series(1, 10001)`, userID, retentionNow); insertErr != nil {
		t.Fatalf("insert bulk idempotency records: %v", insertErr)
	}
	seedUsage(ctx, t, pool, userID)

	result, err := worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency: %v", err)
	}
	if result.IdempotencyRecordsDeleted != maxRowsPerRun || result.Pages != maxRowsPerRun/int64(pageSize) || result.IdempotencyBacklog != 1 {
		t.Errorf("capped result = %+v", result)
	}
	assertUsage(ctx, t, pool, userID, 1, 6)
}

func TestExpireIdempotencySkipsOverlappingCommand(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()
	q := store.New(conn)
	locked, err := q.TryLockIdempotencyExpirySweep(ctx)
	if err != nil || !locked {
		t.Fatalf("hold command lock = (%t, %v)", locked, err)
	}
	defer func() {
		unlocked, unlockErr := q.UnlockIdempotencyExpirySweep(ctx)
		if unlockErr != nil || !unlocked {
			t.Errorf("release command lock = (%t, %v)", unlocked, unlockErr)
		}
	}()
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := worker.ExpireIdempotency(ctx)
	if err != nil || result.Skipped != 1 || result.Successes != 0 || result.Failures != 0 {
		t.Errorf("overlap result = (%+v, %v)", result, err)
	}
}

func TestFailedAdvisoryUnlockDiscardsPhysicalConnection(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Worker, *uint32)
		run  func(context.Context, *Worker) (Result, error)
		lock func(context.Context, *store.Queries) (bool, error)
		free func(context.Context, *store.Queries) (bool, error)
	}{
		{
			name: "idempotency",
			set: func(worker *Worker, pid *uint32) {
				worker.hooks.unlockIdempotency = func(_ context.Context, conn *pgxpool.Conn, _ *store.Queries) (bool, error) {
					*pid = conn.Conn().PgConn().PID()
					return false, nil
				}
			},
			run:  func(ctx context.Context, worker *Worker) (Result, error) { return worker.ExpireIdempotency(ctx) },
			lock: func(ctx context.Context, q *store.Queries) (bool, error) { return q.TryLockIdempotencyExpirySweep(ctx) },
			free: func(ctx context.Context, q *store.Queries) (bool, error) { return q.UnlockIdempotencyExpirySweep(ctx) },
		},
		{
			name: "retention",
			set: func(worker *Worker, pid *uint32) {
				worker.hooks.unlockRetention = func(_ context.Context, conn *pgxpool.Conn, _ *store.Queries) (bool, error) {
					*pid = conn.Conn().PgConn().PID()
					return false, errors.New("injected unlock failure")
				}
			},
			run:  func(ctx context.Context, worker *Worker) (Result, error) { return worker.Retain(ctx) },
			lock: func(ctx context.Context, q *store.Queries) (bool, error) { return q.TryLockPrivacyRetentionSweep(ctx) },
			free: func(ctx context.Context, q *store.Queries) (bool, error) { return q.UnlockPrivacyRetentionSweep(ctx) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, pool := newRetentionPool(t)
			worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			var failedPID uint32
			tc.set(worker, &failedPID)
			result, err := tc.run(ctx, worker)
			if !errors.Is(err, ErrSweepFailed) || result.Failures != 1 || failedPID == 0 {
				t.Fatalf("failed unlock = (%+v, %v, pid %d)", result, err, failedPID)
			}

			probe, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire replacement connection: %v", err)
			}
			defer probe.Release()
			if replacementPID := probe.Conn().PgConn().PID(); replacementPID == failedPID {
				t.Fatalf("failed unlock connection %d returned to pool", failedPID)
			}
			q := store.New(probe)
			locked, err := tc.lock(ctx, q)
			if err != nil || !locked {
				t.Fatalf("replacement lock = (%t, %v)", locked, err)
			}
			unlocked, err := tc.free(ctx, q)
			if err != nil || !unlocked {
				t.Fatalf("replacement unlock = (%t, %v)", unlocked, err)
			}
		})
	}
}

func TestCancellationReleasesAdvisoryLockForAnotherSession(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setHook func(*Worker, context.CancelFunc)
		run     func(context.Context, *Worker) (Result, error)
		tryLock func(context.Context, *store.Queries) (bool, error)
		unlock  func(context.Context, *store.Queries) (bool, error)
	}{
		{
			name:    "idempotency",
			setHook: func(worker *Worker, cancel context.CancelFunc) { worker.hooks.afterIdempotencyLock = cancel },
			run:     func(ctx context.Context, worker *Worker) (Result, error) { return worker.ExpireIdempotency(ctx) },
			tryLock: func(ctx context.Context, q *store.Queries) (bool, error) { return q.TryLockIdempotencyExpirySweep(ctx) },
			unlock:  func(ctx context.Context, q *store.Queries) (bool, error) { return q.UnlockIdempotencyExpirySweep(ctx) },
		},
		{
			name:    "retention",
			setHook: func(worker *Worker, cancel context.CancelFunc) { worker.hooks.afterRetentionLock = cancel },
			run:     func(ctx context.Context, worker *Worker) (Result, error) { return worker.Retain(ctx) },
			tryLock: func(ctx context.Context, q *store.Queries) (bool, error) { return q.TryLockPrivacyRetentionSweep(ctx) },
			unlock:  func(ctx context.Context, q *store.Queries) (bool, error) { return q.UnlockPrivacyRetentionSweep(ctx) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, pool := newRetentionPool(t)
			worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			runCtx, cancel := context.WithCancel(ctx)
			tc.setHook(worker, cancel)
			result, err := tc.run(runCtx, worker)
			if !errors.Is(err, ErrSweepFailed) || result.Failures != 1 {
				t.Fatalf("canceled run = (%+v, %v)", result, err)
			}

			first, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire former worker session: %v", err)
			}
			defer first.Release()
			second, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire independent session: %v", err)
			}
			defer second.Release()
			q := store.New(second)
			locked, err := tc.tryLock(ctx, q)
			if err != nil || !locked {
				t.Fatalf("independent session lock after cancellation = (%t, %v)", locked, err)
			}
			unlocked, err := tc.unlock(ctx, q)
			if err != nil || !unlocked {
				t.Fatalf("independent session unlock = (%t, %v)", unlocked, err)
			}
		})
	}
}

func TestExpireIdempotencyComposesWithConcurrentWriterAndAccountDeletion(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	writerUser := createRetentionUser(ctx, t, q)
	deletedUser := createRetentionUser(ctx, t, q)
	t.Cleanup(func() {
		cleanupUsers(ctx, t, pool, writerUser, deletedUser)
	})

	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin writer: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, writer) })
	if _, lockErr := writer.Exec(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, writerUser); lockErr != nil {
		t.Fatalf("lock writer user: %v", lockErr)
	}
	wq := store.New(writer)
	insertIdempotency(ctx, t, wq, writerUser, retentionNow, json.RawMessage(`{"writer":true}`), json.RawMessage(`{}`))
	if _, usageErr := writer.Exec(ctx, `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
		SELECT user_id, count(*), sum(octet_length(response_body::text) + octet_length(response_headers::text))
		FROM idempotency_records WHERE user_id = $1 GROUP BY user_id`, writerUser); usageErr != nil {
		t.Fatalf("seed writer usage: %v", usageErr)
	}
	if result, sweepErr := worker.ExpireIdempotency(ctx); sweepErr != nil || result.IdempotencyRecordsDeleted != 0 {
		t.Fatalf("sweep alongside writer = (%+v, %v)", result, sweepErr)
	}
	if commitErr := writer.Commit(ctx); commitErr != nil {
		t.Fatalf("commit writer: %v", commitErr)
	}
	if result, sweepErr := worker.ExpireIdempotency(ctx); sweepErr != nil || result.IdempotencyRecordsDeleted != 1 {
		t.Fatalf("sweep after writer = (%+v, %v)", result, sweepErr)
	}

	insertIdempotency(ctx, t, q, deletedUser, retentionNow, json.RawMessage(`{"delete":true}`), json.RawMessage(`{}`))
	seedUsage(ctx, t, pool, deletedUser)
	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deletion: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, deletion) })
	if _, deleteErr := deletion.Exec(ctx, `DELETE FROM users WHERE id = $1`, deletedUser); deleteErr != nil {
		t.Fatalf("delete user: %v", deleteErr)
	}
	if result, sweepErr := worker.ExpireIdempotency(ctx); sweepErr != nil || result.IdempotencyRecordsDeleted != 0 || result.IdempotencyBacklog != 1 {
		t.Fatalf("sweep alongside deletion = (%+v, %v)", result, sweepErr)
	}
	if commitErr := deletion.Commit(ctx); commitErr != nil {
		t.Fatalf("commit deletion: %v", commitErr)
	}
	if result, sweepErr := worker.ExpireIdempotency(ctx); sweepErr != nil || result.IdempotencyBacklog != 0 {
		t.Fatalf("sweep after deletion = (%+v, %v)", result, sweepErr)
	}
}

func TestRetainUsesExactBoundariesAndPreservesPendingMedia(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{
		Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return retentionNow },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })

	oldSession := insertSessionWithExpiry(ctx, t, pool, userID, retentionNow.Add(-91*24*time.Hour), retentionNow.Add(sessionRedactionLead-time.Second), nil)
	exactSession := insertSessionWithExpiry(ctx, t, pool, userID, retentionNow.Add(-91*24*time.Hour), retentionNow.Add(sessionRedactionLead), nil)
	youngSession := insertSessionWithExpiry(ctx, t, pool, userID, retentionNow.Add(-91*24*time.Hour), retentionNow.Add(sessionRedactionLead+time.Second), nil)

	oldAudit := insertAudit(ctx, t, pool, "account_deleted", retentionNow.Add(-180*24*time.Hour-time.Second))
	// The sweep deletes every audit kind by age, including provider unlinks.
	exactAudit := insertAudit(ctx, t, pool, "identity_unlinked", retentionNow.Add(-180*24*time.Hour))
	youngAudit := insertAccountDeletionAudit(ctx, t, q, pool, retentionNow.Add(-180*24*time.Hour+time.Second))
	oldSecurityEvent := insertAuthenticationSecurityEvent(ctx, t, pool, userID, retentionNow.Add(-180*24*time.Hour-time.Second))
	exactSecurityEvent := insertAuthenticationSecurityEvent(ctx, t, pool, userID, retentionNow.Add(-180*24*time.Hour))
	youngSecurityEvent := insertAuthenticationSecurityEvent(ctx, t, pool, userID, retentionNow.Add(-180*24*time.Hour+time.Second))

	oldCompleted := insertMediaJob(ctx, t, pool, retentionNow.Add(-181*24*time.Hour), retentionNow.Add(-180*24*time.Hour-time.Second), true)
	exactCompleted := insertMediaJob(ctx, t, pool, retentionNow.Add(-181*24*time.Hour), retentionNow.Add(-180*24*time.Hour), true)
	youngCompleted := insertMediaJob(ctx, t, pool, retentionNow.Add(-181*24*time.Hour), retentionNow.Add(-180*24*time.Hour+time.Second), true)
	pending := insertMediaJob(ctx, t, pool, retentionNow.Add(-365*24*time.Hour), time.Time{}, false)
	oauth := insertOAuthRetentionRows(ctx, t, q, pool, userID)
	t.Cleanup(func() {
		cleanupExec(ctx, t, pool, "audit rows", `DELETE FROM lifecycle_audit_events WHERE id = ANY($1::uuid[])`, []uuid.UUID{oldAudit, exactAudit, youngAudit})
		cleanupExec(ctx, t, pool, "authentication security events", `DELETE FROM authentication_security_events WHERE id = ANY($1::uuid[])`, []uuid.UUID{oldSecurityEvent, exactSecurityEvent, youngSecurityEvent})
		cleanupExec(ctx, t, pool, "media rows", `DELETE FROM media_deletion_jobs WHERE id = ANY($1::uuid[])`, []uuid.UUID{oldCompleted, exactCompleted, youngCompleted, pending})
	})

	result, err := worker.Retain(ctx)
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if result.SessionMetadataRedacted != 2 || result.LifecycleAuditDeleted != 2 || result.CompletedMediaJobsDeleted != 2 || result.AuthenticationSecurityEventsDeleted != 2 {
		t.Errorf("retention counts = %+v", result)
	}
	if result.OAuthTransactionsDeleted != 1 || result.OAuthAuthorizationCodesDeleted != 1 || result.OAuthTokensDeleted != 1 || result.OAuthClientsDeleted != 1 {
		t.Errorf("OAuth cleanup counts = %+v", result)
	}
	assertSessionMetadata(ctx, t, pool, oldSession, false)
	assertSessionMetadata(ctx, t, pool, exactSession, false)
	assertSessionMetadata(ctx, t, pool, youngSession, true)
	assertRowExists(ctx, t, pool, "lifecycle_audit_events", oldAudit, false)
	assertRowExists(ctx, t, pool, "lifecycle_audit_events", exactAudit, false)
	assertRowExists(ctx, t, pool, "lifecycle_audit_events", youngAudit, true)
	assertRowExists(ctx, t, pool, "authentication_security_events", oldSecurityEvent, false)
	assertRowExists(ctx, t, pool, "authentication_security_events", exactSecurityEvent, false)
	assertRowExists(ctx, t, pool, "authentication_security_events", youngSecurityEvent, true)
	assertRowExists(ctx, t, pool, "media_deletion_jobs", oldCompleted, false)
	assertRowExists(ctx, t, pool, "media_deletion_jobs", exactCompleted, false)
	assertRowExists(ctx, t, pool, "media_deletion_jobs", youngCompleted, true)
	assertRowExists(ctx, t, pool, "media_deletion_jobs", pending, true)
	assertRowExists(ctx, t, pool, "oauth_clients", oauth.liveClient, true)
	assertRowExists(ctx, t, pool, "oauth_clients", oauth.idleClient, false)

	again, err := worker.Retain(ctx)
	if err != nil {
		t.Fatalf("second Retain: %v", err)
	}
	if again.SessionMetadataRedacted != 0 || again.LifecycleAuditDeleted != 0 || again.CompletedMediaJobsDeleted != 0 || again.AuthenticationSecurityEventsDeleted != 0 {
		t.Errorf("second Retain mutated retained rows: %+v", again)
	}
}

// TestRetainRedactsSessionMetadataByAbsoluteExpiry proves the privacy notice's
// session rule: IP and user-agent redaction runs a sessionRedactionLead ahead
// of a session's absolute expiry (docs/design/operations.md), not by its
// created_at, so a rotation successor cannot keep metadata alive past the
// original sign-in's 90-day bound, and the daily sweep schedule (plus one
// missed run) still finishes redaction inside that bound.
func TestRetainRedactsSessionMetadataByAbsoluteExpiry(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{
		Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return retentionNow },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })

	// A predecessor signed in 91 days ago (absolute_expires_at = sign-in +
	// 90d = 1 day ago) rotates into a successor created 1 day ago. Rotation
	// inherits absolute_expires_at unchanged (session.go's
	// createRotationSuccessor), so the successor is just as overdue for
	// redaction as its predecessor despite its recent created_at.
	signIn := retentionNow.Add(-91 * 24 * time.Hour)
	absoluteExpiry := signIn.Add(90 * 24 * time.Hour)
	predecessor := insertSessionWithExpiry(ctx, t, pool, userID, signIn, absoluteExpiry, nil)
	successor := insertSessionWithExpiry(ctx, t, pool, userID, retentionNow.Add(-24*time.Hour), absoluteExpiry, &predecessor)

	// Boundary pair: absolute_expires_at exactly now+48h (the redaction lead)
	// is redacted; one second later is kept, even though its created_at (89
	// days ago) is well past the old created_at-based cutoff.
	atBoundary := insertSessionWithExpiry(ctx, t, pool, userID, retentionNow.Add(-89*24*time.Hour), retentionNow.Add(sessionRedactionLead), nil)
	pastBoundary := insertSessionWithExpiry(ctx, t, pool, userID, retentionNow.Add(-89*24*time.Hour), retentionNow.Add(sessionRedactionLead+time.Second), nil)

	// A session locked by a concurrent writer is skipped (FOR UPDATE SKIP
	// LOCKED) and stays in the backlog, so the backlog and its age-since-
	// sign-in accounting are observable in the same run.
	lockedSignIn := retentionNow.Add(-95 * 24 * time.Hour)
	lockedExpiry := lockedSignIn.Add(90 * 24 * time.Hour)
	locked := insertSessionWithExpiry(ctx, t, pool, userID, lockedSignIn, lockedExpiry, nil)
	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin session lock: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, lockTx) })
	if _, lockErr := lockTx.Exec(ctx, `SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, locked); lockErr != nil {
		t.Fatalf("lock session: %v", lockErr)
	}

	result, err := worker.Retain(ctx)
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	assertSessionMetadata(ctx, t, pool, predecessor, false)
	assertSessionMetadata(ctx, t, pool, successor, false)
	assertSessionMetadata(ctx, t, pool, atBoundary, false)
	assertSessionMetadata(ctx, t, pool, pastBoundary, true)
	assertSessionMetadata(ctx, t, pool, locked, true)
	wantAge := int64(retentionNow.Sub(lockedSignIn).Seconds())
	if result.SessionMetadataBacklog != 1 || result.SessionOldestAgeSeconds != wantAge {
		t.Errorf("backlog = (%d, %d seconds), want (1, %d)", result.SessionMetadataBacklog, result.SessionOldestAgeSeconds, wantAge)
	}
}

// TestRetainDeletesExpiredSlugTombstones proves the privacy notice's
// tombstone rule (docs/design/product.md): a released slug stays reserved for
// 180 days, unlinked from any account, then the daily privacy sweep deletes
// it.
func TestRetainDeletesExpiredSlugTombstones(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	worker, err := New(Config{
		Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return retentionNow },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	expired := insertSlugTombstone(ctx, t, pool, "expired-slug", retentionNow.Add(-180*24*time.Hour))
	kept := insertSlugTombstone(ctx, t, pool, "kept-slug", retentionNow.Add(-180*24*time.Hour+time.Second))
	t.Cleanup(func() {
		cleanupExec(ctx, t, pool, "slug tombstones", `DELETE FROM slug_tombstones WHERE slug = ANY($1::text[])`, []string{expired, kept})
	})

	if _, err := worker.Retain(ctx); err != nil {
		t.Fatalf("Retain: %v", err)
	}
	assertSlugTombstoneExists(ctx, t, pool, expired, false)
	assertSlugTombstoneExists(ctx, t, pool, kept, true)
}

func newRetentionPool(t *testing.T) (context.Context, *store.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(ctx) })
	return ctx, pool
}

func createRetentionUser(ctx context.Context, t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Retention test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user.ID
}

func liveKeyForUser(userID uuid.UUID) uuid.UUID {
	key := userID
	key[0] ^= 0xff
	return key
}

func insertIdempotency(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, expiresAt time.Time, body, headers json.RawMessage) int64 {
	t.Helper()
	key := uuid.New()
	if expiresAt.After(retentionNow) {
		key = liveKeyForUser(userID)
	}
	row, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID: userID, Route: "route", IdempotencyKey: key, RequestHash: make([]byte, 32),
		ResponseStatus: 200, ResponseBody: body, ResponseHeaders: headers, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateIdempotencyRecord: %v", err)
	}
	return int64(row.StoredBytes)
}

func seedUsage(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
		SELECT user_id, count(*), sum(octet_length(response_body::text) + octet_length(response_headers::text))
		FROM idempotency_records WHERE user_id = $1 GROUP BY user_id`, userID)
	if err != nil {
		t.Fatalf("seed usage: %v", err)
	}
}

func assertUsage(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, records, bytes int64) {
	t.Helper()
	var gotRecords, gotBytes int64
	if err := pool.QueryRow(ctx, `SELECT retained_records, stored_bytes FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&gotRecords, &gotBytes); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if gotRecords != records || gotBytes != bytes {
		t.Errorf("usage = (%d, %d), want (%d, %d)", gotRecords, gotBytes, records, bytes)
	}
}

// insertSessionWithExpiry inserts a session with independently chosen
// created_at and absolute_expires_at values. rotatedFrom links a rotation
// successor to its predecessor, matching createRotationSuccessor's lineage.
func insertSessionWithExpiry(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, createdAt, absoluteExpiresAt time.Time, rotatedFrom *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, csrf_secret, created_at, last_seen_at, reauthenticated_at, absolute_expires_at, ua, ip, rotated_from)
		VALUES ($1, $2, $3, $4, $5, $5, $5, $6, 'test-agent', '192.0.2.1', $7)`,
		id, userID, uuid.NewString(), uuid.NewString(), createdAt, absoluteExpiresAt, rotatedFrom)
	if err != nil {
		t.Fatalf("insert session with expiry: %v", err)
	}
	return id
}

// insertSlugTombstone inserts a raw slug_tombstones row with no owning
// account, matching the privacy notice's rule that a released slug carries no
// account link (docs/design/product.md).
func insertSlugTombstone(ctx context.Context, t *testing.T, pool *store.Pool, slug string, releasedAt time.Time) string {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO slug_tombstones (slug, released_at) VALUES ($1, $2)`, slug, releasedAt); err != nil {
		t.Fatalf("insert slug tombstone: %v", err)
	}
	return slug
}

func assertSlugTombstoneExists(ctx context.Context, t *testing.T, pool *store.Pool, slug string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM slug_tombstones WHERE slug = $1)`, slug).Scan(&got); err != nil {
		t.Fatalf("read slug tombstone: %v", err)
	}
	if got != want {
		t.Errorf("slug tombstone %s exists = %t, want %t", slug, got, want)
	}
}

func insertAudit(ctx context.Context, t *testing.T, pool *store.Pool, kind string, occurredAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO lifecycle_audit_events (id, kind, occurred_at) VALUES ($1, $2, $3)`, id, kind, occurredAt); err != nil {
		t.Fatalf("insert audit: %v", err)
	}
	return id
}

func insertAuthenticationSecurityEvent(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, occurredAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO authentication_security_events (kind, user_id, passkey_id, stored_counter, received_counter, occurred_at)
		VALUES ('passkey_counter_non_increasing', $1, $2, 1, 1, $3)
		RETURNING id`, userID, uuid.New(), occurredAt).Scan(&id); err != nil {
		t.Fatalf("insert authentication security event: %v", err)
	}
	return id
}

func insertAccountDeletionAudit(ctx context.Context, t *testing.T, q *store.Queries, pool *store.Pool, occurredAt time.Time) uuid.UUID {
	t.Helper()
	userID := createRetentionUser(ctx, t, q)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin account deletion: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, tx) })
	id := uuid.New()
	if _, insertErr := tx.Exec(ctx, `INSERT INTO lifecycle_audit_events (id, kind, occurred_at) VALUES ($1, 'account_deleted', $2)`, id, occurredAt); insertErr != nil {
		t.Fatalf("insert account deletion audit: %v", insertErr)
	}
	if _, deleteErr := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); deleteErr != nil {
		t.Fatalf("delete audited account: %v", deleteErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit account deletion: %v", commitErr)
	}
	assertRowExists(ctx, t, pool, "lifecycle_audit_events", id, true)
	return id
}

func insertMediaJob(ctx context.Context, t *testing.T, pool *store.Pool, enqueuedAt, completedAt time.Time, completed bool) uuid.UUID {
	t.Helper()
	id, resumeID := uuid.New(), uuid.New()
	key := "resumes/" + resumeID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
	if completed {
		_, err := pool.Exec(ctx, `INSERT INTO media_deletion_jobs
			(id, resume_id, object_key, enqueued_at, next_attempt_at, completed_at, outcome)
			VALUES ($1, $2, $3, $4, $4, $5, 'deleted')`, id, resumeID, key, enqueuedAt, completedAt)
		if err != nil {
			t.Fatalf("insert completed media job: %v", err)
		}
	} else {
		_, err := pool.Exec(ctx, `INSERT INTO media_deletion_jobs
			(id, resume_id, object_key, enqueued_at, next_attempt_at, overdue_at)
			VALUES ($1, $2, $3, $4, $4, $5)`, id, resumeID, key, enqueuedAt, enqueuedAt.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("insert pending media job: %v", err)
		}
	}
	return id
}

type oauthRetentionRows struct {
	liveClient uuid.UUID
	idleClient uuid.UUID
}

func insertOAuthRetentionRows(ctx context.Context, t *testing.T, q *store.Queries, pool *store.Pool, userID uuid.UUID) oauthRetentionRows {
	t.Helper()
	liveClient, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName: "Retention live", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: retentionNow,
	})
	if err != nil {
		t.Fatalf("create live OAuth client: %v", err)
	}
	idleClient, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName: "Retention idle", RedirectURIs: json.RawMessage(`["https://idle.example/callback"]`), CreatedAt: retentionNow.Add(-25 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create idle OAuth client: %v", err)
	}
	t.Cleanup(func() {
		cleanupExec(ctx, t, pool, "OAuth clients", `DELETE FROM oauth_clients WHERE id = ANY($1::uuid[])`, []uuid.UUID{liveClient.ID, idleClient.ID})
	})
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: userID, ClientID: liveClient.ID, Scopes: "resumes:read", CreatedAt: retentionNow.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create OAuth grant: %v", err)
	}
	codeCreated := retentionNow.Add(-61 * time.Second)
	if _, codeErr := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: []byte(uuid.NewString() + uuid.NewString())[:32], ClientID: liveClient.ID, UserID: userID,
		Scopes: "resumes:read", CodeChallenge: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RedirectURI: "https://agent.example/callback", CreatedAt: codeCreated,
	}); codeErr != nil {
		t.Fatalf("create expired authorization code: %v", codeErr)
	}
	tokenCreated := retentionNow.Add(-2 * time.Hour)
	if _, tokenErr := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: []byte(uuid.NewString() + uuid.NewString())[:32], Kind: "access", FamilyID: uuid.New(),
		ClientID: liveClient.ID, UserID: userID, GrantID: grant.ID, CreatedAt: tokenCreated,
		ExpiresAt: tokenCreated.Add(time.Hour), FamilyExpiresAt: tokenCreated.Add(30 * 24 * time.Hour),
	}); tokenErr != nil {
		t.Fatalf("create expired access token: %v", tokenErr)
	}
	if _, transactionErr := pool.Exec(ctx, `INSERT INTO oauth_transactions
		(handle_hash, provider, purpose, state, pkce_verifier, redirect_uri, return_path, expires_at)
		VALUES ($1, 'google', 'login', 'state', 'verifier', 'https://aboutme.example/callback', '/app/resumes', $2)`,
		[]byte(uuid.NewString() + uuid.NewString())[:32], retentionNow); transactionErr != nil {
		t.Fatalf("create expired OAuth transaction: %v", transactionErr)
	}
	return oauthRetentionRows{liveClient: liveClient.ID, idleClient: idleClient.ID}
}

func assertSessionMetadata(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID, present bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `SELECT ua IS NOT NULL OR ip IS NOT NULL FROM sessions WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read session metadata: %v", err)
	}
	if got != present {
		t.Errorf("session %s metadata present = %t, want %t", id, got, present)
	}
}

func assertRowExists(ctx context.Context, t *testing.T, pool *store.Pool, table string, id uuid.UUID, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+pgx.Identifier{table}.Sanitize()+` WHERE id = $1)`, id).Scan(&got); err != nil {
		t.Fatalf("read %s row: %v", table, err)
	}
	if got != want {
		t.Errorf("%s row %s exists = %t, want %t", table, id, got, want)
	}
}

func cleanupUsers(ctx context.Context, t *testing.T, pool *store.Pool, ids ...uuid.UUID) {
	t.Helper()
	cleanupExec(ctx, t, pool, "users", `DELETE FROM users WHERE id = ANY($1::uuid[])`, ids)
}

func cleanupExec(ctx context.Context, t *testing.T, pool *store.Pool, name, statement string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, statement, args...); err != nil {
		t.Errorf("cleanup %s: %v", name, err)
	}
}

func rollbackTestTx(ctx context.Context, t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback test transaction: %v", err)
	}
}
