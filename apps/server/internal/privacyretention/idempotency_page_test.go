package privacyretention

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

func TestExpireIdempotencyPage_TakesOldestExpiredRecordsAcrossUsers(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	first := createRetentionUser(ctx, t, q)
	second := createRetentionUser(ctx, t, q)
	third := createRetentionUser(ctx, t, q)
	unexpired := createRetentionUser(ctx, t, q)
	empty := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, first, second, third, unexpired, empty) })

	firstOld := insertIdempotency(ctx, t, q, first, retentionNow.Add(-4*time.Hour), json.RawMessage(`{"n":1}`), json.RawMessage(`{}`))
	secondOld := insertIdempotency(ctx, t, q, second, retentionNow.Add(-3*time.Hour), json.RawMessage(`{"n":22}`), json.RawMessage(`{"ETag":"b"}`))
	thirdOld := insertIdempotency(ctx, t, q, third, retentionNow.Add(-2*time.Hour), json.RawMessage(`{"n":333}`), json.RawMessage(`{}`))
	firstNew := insertIdempotency(ctx, t, q, first, retentionNow.Add(-time.Hour), json.RawMessage(`{"n":4444}`), json.RawMessage(`{}`))
	secondNew := insertIdempotency(ctx, t, q, second, retentionNow, json.RawMessage(`null`), json.RawMessage(`{}`))
	firstLive := insertIdempotency(ctx, t, q, first, retentionNow.Add(time.Hour), json.RawMessage(`{"live":1}`), json.RawMessage(`{}`))
	secondLive := insertIdempotency(ctx, t, q, second, retentionNow.Add(2*time.Hour), json.RawMessage(`{"live":2}`), json.RawMessage(`{}`))
	unexpiredLive := insertIdempotency(ctx, t, q, unexpired, retentionNow.Add(time.Second), json.RawMessage(`{"live":3}`), json.RawMessage(`{}`))
	for _, id := range []uuid.UUID{first, second, third, unexpired} {
		seedUsage(ctx, t, pool, id)
	}

	if got, want := expirePage(ctx, t, q, 3), pageResult(3, firstOld+secondOld+thirdOld); got != want {
		t.Errorf("first page = %+v, want %+v", got, want)
	}
	assertUsage(ctx, t, pool, first, 2, firstNew+firstLive)
	assertUsage(ctx, t, pool, second, 2, secondNew+secondLive)
	assertUsage(ctx, t, pool, third, 0, 0)
	assertUsage(ctx, t, pool, unexpired, 1, unexpiredLive)
	assertExpiries(ctx, t, pool, first, retentionNow.Add(-time.Hour), retentionNow.Add(time.Hour))
	assertExpiries(ctx, t, pool, second, retentionNow, retentionNow.Add(2*time.Hour))
	assertExpiries(ctx, t, pool, third)

	if got, want := expirePage(ctx, t, q, 3), pageResult(2, firstNew+secondNew); got != want {
		t.Errorf("second page = %+v, want %+v", got, want)
	}
	assertUsage(ctx, t, pool, first, 1, firstLive)
	assertUsage(ctx, t, pool, second, 1, secondLive)
	assertExpiries(ctx, t, pool, first, retentionNow.Add(time.Hour))
	assertExpiries(ctx, t, pool, second, retentionNow.Add(2*time.Hour))
	assertExpiries(ctx, t, pool, unexpired, retentionNow.Add(time.Second))

	if got, want := expirePage(ctx, t, q, 3), pageResult(0, 0); got != want {
		t.Errorf("drained page = %+v, want %+v", got, want)
	}
}

func TestExpireIdempotencyPage_IgnoresAccountsWithoutExpiredRecords(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	idle := createIdleUsers(ctx, t, pool, 1500)
	swept := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, swept) })
	sweptBytes := insertIdempotency(ctx, t, q, swept, retentionNow.Add(-time.Hour), json.RawMessage(`{"old":true}`), json.RawMessage(`{}`))
	sweptBytes += insertIdempotency(ctx, t, q, swept, retentionNow, json.RawMessage(`null`), json.RawMessage(`{"ETag":"s"}`))
	seedUsage(ctx, t, pool, swept)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin page: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, tx) })
	got, err := store.New(tx).ExpireIdempotencyPage(ctx, store.ExpireIdempotencyPageParams{Cutoff: retentionNow, LimitRows: pageSize})
	if err != nil {
		t.Fatalf("ExpireIdempotencyPage: %v", err)
	}
	if want := pageResult(2, sweptBytes); got != want {
		t.Errorf("page = %+v, want %+v", got, want)
	}
	if free := countLockableUsers(ctx, t, pool, idle); free != int64(len(idle)) {
		t.Errorf("page locked %d accounts without expired records, want 0", int64(len(idle))-free)
	}
	if free := countLockableUsers(ctx, t, pool, []uuid.UUID{swept}); free != 0 {
		t.Error("page left the swept account unlocked")
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit page: %v", commitErr)
	}

	var records, retained, storedBytes int64
	if scanErr := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM idempotency_records WHERE user_id = ANY($1::uuid[])),
		(SELECT sum(retained_records)::bigint FROM idempotency_usage WHERE user_id = ANY($1::uuid[])),
		(SELECT sum(stored_bytes)::bigint FROM idempotency_usage WHERE user_id = ANY($1::uuid[]))`, idle).Scan(&records, &retained, &storedBytes); scanErr != nil {
		t.Fatalf("read idle accounts: %v", scanErr)
	}
	if n := int64(len(idle)); records != n || retained != n || storedBytes != 6*n {
		t.Errorf("idle accounts = (%d records, %d retained, %d bytes), want (%d, %d, %d)", records, retained, storedBytes, n, n, 6*n)
	}
	assertUsage(ctx, t, pool, swept, 0, 0)
}

func TestExpireIdempotency_SweepsPastBusyUserHoldingFullPage(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	busy := createRetentionUser(ctx, t, q)
	other := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, busy, other) })
	insertExpiredBulk(ctx, t, pool, busy, int(pageSize), retentionNow.Add(-2*time.Hour))
	otherBytes := insertIdempotency(ctx, t, q, other, retentionNow.Add(-time.Hour), json.RawMessage(`{"other":1}`), json.RawMessage(`{}`))
	otherBytes += insertIdempotency(ctx, t, q, other, retentionNow, json.RawMessage(`{"other":2}`), json.RawMessage(`{}`))
	seedUsage(ctx, t, pool, busy)
	seedUsage(ctx, t, pool, other)

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin busy-user lock: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, lockTx) })
	if _, lockErr := lockTx.Exec(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, busy); lockErr != nil {
		t.Fatalf("lock busy user: %v", lockErr)
	}

	result, err := worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency: %v", err)
	}
	if result.Pages != 1 || result.IdempotencyRecordsDeleted != 2 || result.IdempotencyBytesReleased != otherBytes {
		t.Errorf("run with busy head = (%d pages, %d records, %d bytes), want (1, 2, %d)",
			result.Pages, result.IdempotencyRecordsDeleted, result.IdempotencyBytesReleased, otherBytes)
	}
	if result.IdempotencyBacklog != int64(pageSize) || result.IdempotencyOldestExpiredSeconds != 7200 {
		t.Errorf("backlog = (%d, %d seconds), want (%d, 7200)", result.IdempotencyBacklog, result.IdempotencyOldestExpiredSeconds, pageSize)
	}
	assertUsage(ctx, t, pool, busy, int64(pageSize), 6*int64(pageSize))
	assertUsage(ctx, t, pool, other, 0, 0)

	if commitErr := lockTx.Commit(ctx); commitErr != nil {
		t.Fatalf("release busy-user lock: %v", commitErr)
	}
	result, err = worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency after unlock: %v", err)
	}
	if result.IdempotencyRecordsDeleted != int64(pageSize) || result.IdempotencyBacklog != 0 {
		t.Errorf("run after unlock = %+v", result)
	}
	assertUsage(ctx, t, pool, busy, 0, 0)
}

func TestExpireIdempotency_ContinuesAfterShortPage(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	held := createRetentionUser(ctx, t, q)
	other := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, held, other) })
	insertExpiredBulk(ctx, t, pool, held, 600, retentionNow.Add(-2*time.Hour))
	insertExpiredBulk(ctx, t, pool, other, 600, retentionNow.Add(-time.Hour))
	seedUsage(ctx, t, pool, held)
	seedUsage(ctx, t, pool, other)
	pages := releaseReplayRowsAfterFirstPage(ctx, t, pool, worker, held)

	result, err := worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency: %v", err)
	}
	if want := []int64{400, 800}; !slices.Equal(*pages, want) {
		t.Errorf("page deletions = %v, want %v", *pages, want)
	}
	if result.Pages != 2 || result.IdempotencyRecordsDeleted != 1200 || result.IdempotencyBytesReleased != 7200 || result.IdempotencyBacklog != 0 {
		t.Errorf("run = %+v, want 2 pages, 1200 records, 7200 bytes, no backlog", result)
	}
	assertUsage(ctx, t, pool, held, 0, 0)
	assertUsage(ctx, t, pool, other, 0, 0)
}

func TestExpireIdempotency_RunCapStaysExactAfterShortPage(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	held := createRetentionUser(ctx, t, q)
	other := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, held, other) })
	insertExpiredBulk(ctx, t, pool, held, 600, retentionNow.Add(-2*time.Hour))
	insertExpiredBulk(ctx, t, pool, other, int(maxRowsPerRun), retentionNow.Add(-time.Hour))
	seedUsage(ctx, t, pool, held)
	seedUsage(ctx, t, pool, other)
	pages := releaseReplayRowsAfterFirstPage(ctx, t, pool, worker, held)

	result, err := worker.ExpireIdempotency(ctx)
	if err != nil {
		t.Fatalf("ExpireIdempotency: %v", err)
	}
	want := []int64{400}
	for range 9 {
		want = append(want, int64(pageSize))
	}
	want = append(want, 600)
	if !slices.Equal(*pages, want) {
		t.Errorf("page deletions = %v, want %v", *pages, want)
	}
	if result.Pages != 11 || result.IdempotencyRecordsDeleted != maxRowsPerRun || result.IdempotencyBytesReleased != 6*maxRowsPerRun || result.IdempotencyBacklog != 600 {
		t.Errorf("run = %+v, want 11 pages, %d records, %d bytes, backlog 600", result, maxRowsPerRun, 6*maxRowsPerRun)
	}
	assertUsage(ctx, t, pool, held, 0, 0)
	assertUsage(ctx, t, pool, other, 600, 3600)
}

func TestExpireIdempotencyPage_CapsRowsAcrossUsers(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	users := []uuid.UUID{createRetentionUser(ctx, t, q), createRetentionUser(ctx, t, q), createRetentionUser(ctx, t, q)}
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, users...) })
	for i, id := range users {
		insertExpiredBulk(ctx, t, pool, id, 400, retentionNow.Add(-time.Duration(len(users)-i)*time.Hour))
		seedUsage(ctx, t, pool, id)
	}

	if got, want := expirePage(ctx, t, q, 5000), pageResult(1000, 6000); got != want {
		t.Errorf("page over the cap = %+v, want %+v", got, want)
	}
	assertUsage(ctx, t, pool, users[0], 0, 0)
	assertUsage(ctx, t, pool, users[1], 0, 0)
	assertUsage(ctx, t, pool, users[2], 200, 1200)

	if got, want := expirePage(ctx, t, q, 7), pageResult(7, 42); got != want {
		t.Errorf("page under the cap = %+v, want %+v", got, want)
	}
	assertUsage(ctx, t, pool, users[2], 193, 1158)
}

func expirePage(ctx context.Context, t *testing.T, q *store.Queries, limit int32) store.ExpireIdempotencyPageRow {
	t.Helper()
	row, err := q.ExpireIdempotencyPage(ctx, store.ExpireIdempotencyPageParams{Cutoff: retentionNow, LimitRows: limit})
	if err != nil {
		t.Fatalf("ExpireIdempotencyPage(%d): %v", limit, err)
	}
	return row
}

func pageResult(records, bytes int64) store.ExpireIdempotencyPageRow {
	return store.ExpireIdempotencyPageRow{DeletedRecords: records, DeletedBytes: bytes, ReleasedRecords: records, ReleasedBytes: bytes}
}

func insertExpiredBulk(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, count int, expiresAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO idempotency_records
		(user_id, route, idempotency_key, request_hash, response_status, response_body, response_headers, expires_at)
		SELECT $1, 'bulk', uuidv7(), decode(repeat('00', 32), 'hex'), 204, 'null'::jsonb, '{}'::jsonb, $2
		FROM generate_series(1, $3::int)`, userID, expiresAt, count); err != nil {
		t.Fatalf("insert bulk idempotency records: %v", err)
	}
}

func releaseReplayRowsAfterFirstPage(ctx context.Context, t *testing.T, pool *store.Pool, worker *Worker, userID uuid.UUID) *[]int64 {
	t.Helper()
	rowLocks, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin replay-row locks: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, rowLocks) })
	if _, lockErr := rowLocks.Exec(ctx, `SELECT id FROM idempotency_records WHERE user_id = $1 FOR UPDATE`, userID); lockErr != nil {
		t.Fatalf("lock replay rows: %v", lockErr)
	}
	pages := &[]int64{}
	worker.hooks.afterIdempotencyPage = func(deleted int64) {
		*pages = append(*pages, deleted)
		if len(*pages) == 1 {
			if commitErr := rowLocks.Commit(ctx); commitErr != nil {
				t.Errorf("release replay-row locks: %v", commitErr)
			}
		}
	}
	return pages
}

func createIdleUsers(ctx context.Context, t *testing.T, pool *store.Pool, count int) []uuid.UUID {
	t.Helper()
	rows, err := pool.Query(ctx, `INSERT INTO users (email, name)
		SELECT gen_random_uuid()::text || '@example.test', 'Retention idle'
		FROM generate_series(1, $1::int)
		RETURNING id`, count)
	if err != nil {
		t.Fatalf("insert idle users: %v", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatalf("collect idle users: %v", err)
	}
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, ids...) })
	if _, insertErr := pool.Exec(ctx, `INSERT INTO idempotency_records
		(user_id, route, idempotency_key, request_hash, response_status, response_body, response_headers, expires_at)
		SELECT id, 'idle', uuidv7(), decode(repeat('00', 32), 'hex'), 204, 'null'::jsonb, '{}'::jsonb, $2
		FROM unnest($1::uuid[]) AS id`, ids, retentionNow.Add(time.Hour)); insertErr != nil {
		t.Fatalf("insert idle records: %v", insertErr)
	}
	if _, usageErr := pool.Exec(ctx, `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
		SELECT id, 1, 6 FROM unnest($1::uuid[]) AS id`, ids); usageErr != nil {
		t.Fatalf("insert idle usage: %v", usageErr)
	}
	return ids
}

func countLockableUsers(ctx context.Context, t *testing.T, pool *store.Pool, ids []uuid.UUID) int64 {
	t.Helper()
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock probe: %v", err)
	}
	defer rollbackTestTx(ctx, t, probe)
	var free int64
	if scanErr := probe.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT id FROM users WHERE id = ANY($1::uuid[]) FOR UPDATE SKIP LOCKED
	) AS free`, ids).Scan(&free); scanErr != nil {
		t.Fatalf("probe user locks: %v", scanErr)
	}
	return free
}

func assertExpiries(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, want ...time.Time) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT expires_at FROM idempotency_records WHERE user_id = $1 ORDER BY expires_at`, userID)
	if err != nil {
		t.Fatalf("read expiries: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		t.Fatalf("collect expiries: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("user %s expiries = %v, want %v", userID, got, want)
		return
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("user %s expiries = %v, want %v", userID, got, want)
			return
		}
	}
}
