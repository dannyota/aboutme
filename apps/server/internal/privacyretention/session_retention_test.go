package privacyretention

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

var sessionCutoff = retentionNow.Add(-sessionRetention)

const absoluteTimeout = 90 * 24 * time.Hour

func TestRunPagesUntilEmpty(t *testing.T) {
	full := slices.Repeat([]int64{int64(pageSize)}, 9)
	for _, tc := range []struct {
		name       string
		counts     []int64
		wantLimits []int32
		wantTotal  int64
		wantPages  int64
	}{
		{"short page continues", []int64{999, 501, 0}, []int32{1000, 1000, 1000}, 1500, 2},
		{"cap stays exact after a short page", append(append([]int64{400}, full...), 600), append(slices.Repeat([]int32{pageSize}, 10), 600), maxRowsPerRun, 11},
		{"empty first page", []int64{0}, []int32{1000}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var limits []int32
			total, pages, err := runPagesUntilEmpty(context.Background(), 5, func(_ context.Context, limit int32) (int64, error) {
				limits = append(limits, limit)
				if len(limits) > len(tc.counts) {
					t.Fatalf("page %d requested after the last scripted page", len(limits))
				}
				return tc.counts[len(limits)-1], nil
			})
			if err != nil || total != tc.wantTotal || pages != 5+tc.wantPages || !slices.Equal(limits, tc.wantLimits) {
				t.Errorf("run = (%d rows, %d pages, limits %v, %v), want (%d, %d, %v, nil)", total, pages, limits, err, tc.wantTotal, 5+tc.wantPages, tc.wantLimits)
			}
		})
	}

	failure := errors.New("page failed")
	calls := 0
	total, pages, err := runPagesUntilEmpty(context.Background(), 0, func(context.Context, int32) (int64, error) {
		calls++
		if calls == 2 {
			return 0, failure
		}
		return 700, nil
	})
	if !errors.Is(err, failure) || total != 700 || pages != 1 {
		t.Errorf("failed run = (%d rows, %d pages, %v), want (700, 1, page failed)", total, pages, err)
	}
}

func TestRetainDeletesSessionsSevenDaysAfterAbsoluteExpiry(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	worker := newSessionWorker(t, pool)
	userID := createRetentionUser(ctx, t, store.New(pool))
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })

	past := insertSessionChain(ctx, t, pool, userID, sessionCutoff.Add(-time.Second), 1)[0]
	atCutoff := insertSessionChain(ctx, t, pool, userID, sessionCutoff, 1)[0]
	inside := insertSessionChain(ctx, t, pool, userID, sessionCutoff.Add(time.Second), 1)[0]
	live := insertSessionChain(ctx, t, pool, userID, retentionNow.Add(30*24*time.Hour), 1)[0]
	revokedPast := insertSessionChain(ctx, t, pool, userID, sessionCutoff.Add(-24*time.Hour), 1)[0]
	revokedInside := insertSessionChain(ctx, t, pool, userID, sessionCutoff.Add(24*time.Hour), 1)[0]
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at = absolute_expires_at - interval '1 day' WHERE id = ANY($1::uuid[])`, []uuid.UUID{revokedPast, revokedInside}); err != nil {
		t.Fatalf("revoke sessions: %v", err)
	}

	result := retainWithin(ctx, t, worker)
	assertSessionRetention(t, result, 3, 0, 0)
	for id, want := range map[uuid.UUID]bool{past: false, atCutoff: false, revokedPast: false, inside: true, live: true, revokedInside: true} {
		assertRowExists(ctx, t, pool, "sessions", id, want)
	}
	assertSessionRetention(t, retainWithin(ctx, t, worker), 0, 0, 0)
}

func TestRetainDeletesRotationChainsWhole(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	worker := newSessionWorker(t, pool)
	userID := createRetentionUser(ctx, t, store.New(pool))
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })

	expiry := sessionCutoff.Add(-24 * time.Hour)
	chain := insertSessionChain(ctx, t, pool, userID, expiry, 3)
	held := insertSessionChain(ctx, t, pool, userID, expiry, 2)
	guarded := insertSessionChain(ctx, t, pool, userID, expiry, 1)[0]
	liveSuccessor := insertSession(ctx, t, pool, userID, uuid.New(), retentionNow.Add(24*time.Hour), &guarded)
	lockTx := lockRows(ctx, t, pool, lockedRow{"sessions", held[1]})

	wantAge := int64((retentionNow.Sub(expiry) + absoluteTimeout).Seconds())
	assertSessionRetention(t, retainWithin(ctx, t, worker), 3, 3, wantAge)
	for _, id := range chain {
		assertRowExists(ctx, t, pool, "sessions", id, false)
	}
	for _, id := range []uuid.UUID{held[0], held[1], guarded, liveSuccessor} {
		assertRowExists(ctx, t, pool, "sessions", id, true)
	}
	assertRotatedFrom(ctx, t, pool, held[1], &held[0])
	assertRotatedFrom(ctx, t, pool, liveSuccessor, &guarded)

	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release session lock: %v", err)
	}
	assertSessionRetention(t, retainWithin(ctx, t, worker), 2, 1, wantAge)
	assertRowExists(ctx, t, pool, "sessions", held[0], false)
	assertRowExists(ctx, t, pool, "sessions", held[1], false)
	assertRowExists(ctx, t, pool, "sessions", guarded, true)
	assertRotatedFrom(ctx, t, pool, liveSuccessor, &guarded)
}

func TestDeleteExpiredSessionsPageKeepsSplitChainForNextPage(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	userID := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })
	chain := insertSessionChain(ctx, t, pool, userID, sessionCutoff.Add(-time.Hour), 3)

	if got := deleteSessionsPage(ctx, t, q, 2); got != 1 {
		t.Errorf("first page deleted %d, want 1", got)
	}
	assertRowExists(ctx, t, pool, "sessions", chain[0], false)
	assertRotatedFrom(ctx, t, pool, chain[1], nil)
	assertRotatedFrom(ctx, t, pool, chain[2], &chain[1])

	if got := deleteSessionsPage(ctx, t, q, 2); got != 2 {
		t.Errorf("second page deleted %d, want 2", got)
	}
	if got := deleteSessionsPage(ctx, t, q, 2); got != 0 {
		t.Errorf("drained page deleted %d, want 0", got)
	}
}

func TestRetainDeletesExpiredSessionReferences(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker := newSessionWorker(t, pool)
	swept, heldPendingUser, heldCeremonyUser, unexpiredUser := createRetentionUser(ctx, t, q), createRetentionUser(ctx, t, q), createRetentionUser(ctx, t, q), createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, swept, heldPendingUser, heldCeremonyUser, unexpiredUser) })

	expiry := sessionCutoff.Add(-24 * time.Hour)
	issued := expiry.Add(-time.Hour)
	sweptSession := insertSessionChain(ctx, t, pool, swept, expiry, 1)[0]
	sweptPending := insertPendingReauth(ctx, t, pool, swept, sweptSession, issued)
	sweptRows := map[string]uuid.UUID{
		"pending_authentications": sweptPending,
		"webauthn_ceremonies":     insertCeremony(ctx, t, pool, swept, &sweptSession, nil, issued),
		"totp_enrollments":        insertTOTPEnrollment(ctx, t, pool, swept, sweptSession, issued),
	}
	sweptAssertion := insertCeremony(ctx, t, pool, swept, nil, &sweptPending, issued)
	liveSession := insertSessionChain(ctx, t, pool, swept, retentionNow.Add(24*time.Hour), 1)[0]
	livePending := insertPendingReauth(ctx, t, pool, swept, liveSession, retentionNow.Add(-time.Minute))

	heldPendingSession := insertSessionChain(ctx, t, pool, heldPendingUser, expiry, 1)[0]
	heldPending := insertPendingReauth(ctx, t, pool, heldPendingUser, heldPendingSession, issued)
	heldCeremonySession := insertSessionChain(ctx, t, pool, heldCeremonyUser, expiry, 1)[0]
	boundPending := insertPendingReauth(ctx, t, pool, heldCeremonyUser, heldCeremonySession, issued)
	heldCeremony := insertCeremony(ctx, t, pool, heldCeremonyUser, nil, &boundPending, issued)
	unexpiredSession := insertSessionChain(ctx, t, pool, unexpiredUser, expiry, 1)[0]
	unexpiredEnrollment := insertTOTPEnrollment(ctx, t, pool, unexpiredUser, unexpiredSession, retentionNow.Add(-time.Minute))
	lockTx := lockRows(ctx, t, pool, lockedRow{"pending_authentications", heldPending}, lockedRow{"webauthn_ceremonies", heldCeremony})

	wantAge := int64((retentionNow.Sub(expiry) + absoluteTimeout).Seconds())
	assertSessionRetention(t, retainWithin(ctx, t, worker), 1, 3, wantAge)
	assertRowExists(ctx, t, pool, "sessions", sweptSession, false)
	for table, id := range sweptRows {
		assertRowExists(ctx, t, pool, table, id, false)
	}
	assertRowExists(ctx, t, pool, "webauthn_ceremonies", sweptAssertion, false)
	assertRowExists(ctx, t, pool, "sessions", liveSession, true)
	assertRowExists(ctx, t, pool, "pending_authentications", livePending, true)
	for _, id := range []uuid.UUID{heldPendingSession, heldCeremonySession, unexpiredSession} {
		assertRowExists(ctx, t, pool, "sessions", id, true)
	}
	assertRowExists(ctx, t, pool, "pending_authentications", heldPending, true)
	assertRowExists(ctx, t, pool, "pending_authentications", boundPending, true)
	assertRowExists(ctx, t, pool, "webauthn_ceremonies", heldCeremony, true)

	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release reference locks: %v", err)
	}
	assertSessionRetention(t, retainWithin(ctx, t, worker), 2, 1, wantAge)
	assertRowExists(ctx, t, pool, "sessions", heldPendingSession, false)
	assertRowExists(ctx, t, pool, "pending_authentications", heldPending, false)
	assertRowExists(ctx, t, pool, "sessions", heldCeremonySession, false)
	assertRowExists(ctx, t, pool, "pending_authentications", boundPending, false)
	assertRowExists(ctx, t, pool, "webauthn_ceremonies", heldCeremony, false)
	assertRowExists(ctx, t, pool, "sessions", unexpiredSession, true)
	assertRowExists(ctx, t, pool, "totp_enrollments", unexpiredEnrollment, true)
}

func TestRetainSkipsSessionsHeldByAnotherTransaction(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker := newSessionWorker(t, pool)
	kept, leaving := createRetentionUser(ctx, t, q), createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, kept, leaving) })

	lockedExpiry := sessionCutoff.Add(-3 * 24 * time.Hour)
	locked := insertSessionChain(ctx, t, pool, kept, lockedExpiry, 1)[0]
	leavingChain := insertSessionChain(ctx, t, pool, leaving, sessionCutoff.Add(-24*time.Hour), 2)
	insertPendingReauth(ctx, t, pool, leaving, leavingChain[1], sessionCutoff.Add(-25*time.Hour))
	lockTx := lockRows(ctx, t, pool, lockedRow{"sessions", locked})
	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin account deletion: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, deletion) })
	if _, deleteErr := deletion.Exec(ctx, `DELETE FROM users WHERE id = $1`, leaving); deleteErr != nil {
		t.Fatalf("delete account: %v", deleteErr)
	}

	wantAge := int64((retentionNow.Sub(lockedExpiry) + absoluteTimeout).Seconds())
	assertSessionRetention(t, retainWithin(ctx, t, worker), 0, 3, wantAge)

	if commitErr := deletion.Commit(ctx); commitErr != nil {
		t.Fatalf("commit account deletion: %v", commitErr)
	}
	if commitErr := lockTx.Commit(ctx); commitErr != nil {
		t.Fatalf("release session lock: %v", commitErr)
	}
	assertSessionRetention(t, retainWithin(ctx, t, worker), 1, 0, 0)
	assertRowExists(ctx, t, pool, "sessions", locked, false)
	for _, id := range leavingChain {
		assertRowExists(ctx, t, pool, "sessions", id, false)
	}
}

func TestRetainKeepsDeletingSessionsAfterShortPage(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	worker := newSessionWorker(t, pool)
	userID := createRetentionUser(ctx, t, store.New(pool))
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })

	held := insertSessionChain(ctx, t, pool, userID, sessionCutoff.Add(-24*time.Hour), 2)
	insertExpiredSessionsBulk(ctx, t, pool, userID, 1500, sessionCutoff)
	lockRows(ctx, t, pool, lockedRow{"sessions", held[1]})

	wantAge := int64((sessionRetention + 24*time.Hour + absoluteTimeout).Seconds())
	assertSessionRetention(t, retainWithin(ctx, t, worker), 1500, 2, wantAge)
}

func TestRetainCapsSessionDeletionPerPageAndRun(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	worker := newSessionWorker(t, pool)
	userID := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, userID) })
	insertExpiredSessionsBulk(ctx, t, pool, userID, int(maxRowsPerRun)+int(pageSize)+1, sessionCutoff)

	if got := deleteSessionsPage(ctx, t, q, 5*pageSize); got != int64(pageSize) {
		t.Errorf("page over the cap deleted %d, want %d", got, pageSize)
	}
	wantAge := int64((sessionRetention + absoluteTimeout).Seconds())
	assertSessionRetention(t, retainWithin(ctx, t, worker), maxRowsPerRun, 1, wantAge)
}

func newSessionWorker(t *testing.T, pool *store.Pool) *Worker {
	t.Helper()
	worker, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return worker
}

func retainWithin(ctx context.Context, t *testing.T, worker *Worker) Result {
	t.Helper()
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result, err := worker.Retain(runCtx)
	if err != nil {
		t.Fatalf("Retain: %v (%+v)", err, result)
	}
	return result
}

func assertSessionRetention(t *testing.T, result Result, deleted, backlog, oldestSeconds int64) {
	t.Helper()
	if result.ExpiredSessionsDeleted != deleted || result.ExpiredSessionsBacklog != backlog || result.ExpiredSessionsOldestAgeSeconds != oldestSeconds {
		t.Errorf("session retention = (%d deleted, %d backlog, %d seconds), want (%d, %d, %d)",
			result.ExpiredSessionsDeleted, result.ExpiredSessionsBacklog, result.ExpiredSessionsOldestAgeSeconds, deleted, backlog, oldestSeconds)
	}
}

func deleteSessionsPage(ctx context.Context, t *testing.T, q *store.Queries, limit int32) int64 {
	t.Helper()
	deleted, err := q.DeleteExpiredSessionsPage(ctx, store.DeleteExpiredSessionsPageParams{Cutoff: sessionCutoff, Now: retentionNow, LimitRows: limit})
	if err != nil {
		t.Fatalf("DeleteExpiredSessionsPage(%d): %v", limit, err)
	}
	return deleted
}

func insertSessionChain(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, absoluteExpiresAt time.Time, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for i, id := range ids {
		var rotatedFrom *uuid.UUID
		if i > 0 {
			rotatedFrom = &ids[i-1]
		}
		insertSession(ctx, t, pool, userID, id, absoluteExpiresAt, rotatedFrom)
	}
	return ids
}

func insertSession(ctx context.Context, t *testing.T, pool *store.Pool, userID, id uuid.UUID, absoluteExpiresAt time.Time, rotatedFrom *uuid.UUID) uuid.UUID {
	t.Helper()
	signIn := absoluteExpiresAt.Add(-absoluteTimeout)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, csrf_secret, created_at, last_seen_at, reauthenticated_at, absolute_expires_at, rotated_from)
		VALUES ($1, $2, sha256(uuid_send($1::uuid)), sha256(uuid_send(gen_random_uuid())), $3, $3, $3, $4, $5)`,
		id, userID, signIn, absoluteExpiresAt, rotatedFrom); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return id
}

func insertExpiredSessionsBulk(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, count int, absoluteExpiresAt time.Time) {
	t.Helper()
	signIn := absoluteExpiresAt.Add(-absoluteTimeout)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(user_id, token_hash, csrf_secret, created_at, last_seen_at, reauthenticated_at, absolute_expires_at)
		SELECT $1, sha256(uuid_send(gen_random_uuid())), sha256(uuid_send(gen_random_uuid())), $2, $2, $2, $3
		FROM generate_series(1, $4::int)`, userID, signIn, absoluteExpiresAt, count); err != nil {
		t.Fatalf("insert bulk sessions: %v", err)
	}
}

func insertPendingReauth(ctx context.Context, t *testing.T, pool *store.Pool, userID, sessionID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO pending_authentications
		(token_digest, csrf_secret, user_id, purpose, auth_epoch, session_id, primary_verified_at, return_path, created_at, expires_at)
		VALUES (sha256(uuid_send(gen_random_uuid())), sha256(uuid_send(gen_random_uuid())), $1, 'reauth', 0, $2, $3, '/', $3, $3::timestamptz + interval '5 minutes')
		RETURNING id`, userID, sessionID, createdAt).Scan(&id); err != nil {
		t.Fatalf("insert pending reauthentication: %v", err)
	}
	return id
}

func insertCeremony(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, sessionID, pendingID *uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	purpose := "registration"
	if pendingID != nil {
		purpose = "assertion"
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO webauthn_ceremonies
		(token_digest, challenge_digest, user_id, purpose, auth_epoch, session_id, pending_authentication_id, created_at, expires_at)
		VALUES (sha256(uuid_send(gen_random_uuid())), sha256(uuid_send(gen_random_uuid())), $1, $2, 0, $3, $4, $5, $5::timestamptz + interval '5 minutes')
		RETURNING id`, userID, purpose, sessionID, pendingID, createdAt).Scan(&id); err != nil {
		t.Fatalf("insert %s ceremony: %v", purpose, err)
	}
	return id
}

func insertTOTPEnrollment(ctx context.Context, t *testing.T, pool *store.Pool, userID, sessionID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO totp_enrollments
		(id, token_digest, user_id, session_id, auth_epoch, issuer, key_id, nonce, ciphertext, format_version, created_at, expires_at)
		VALUES ($1, sha256(uuid_send($1::uuid)), $2, $3, 0, 'aboutme.test', 'tk1_' || repeat('A', 22),
		        decode(repeat('00', 12), 'hex'), decode(repeat('00', 36), 'hex'), 1, $4, $4::timestamptz + interval '10 minutes')`,
		id, userID, sessionID, createdAt); err != nil {
		t.Fatalf("insert TOTP enrollment: %v", err)
	}
	return id
}

type lockedRow struct {
	table string
	id    uuid.UUID
}

func lockRows(ctx context.Context, t *testing.T, pool *store.Pool, rows ...lockedRow) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin row locks: %v", err)
	}
	t.Cleanup(func() { rollbackTestTx(ctx, t, tx) })
	for _, row := range rows {
		if _, lockErr := tx.Exec(ctx, `SELECT id FROM `+pgx.Identifier{row.table}.Sanitize()+` WHERE id = $1 FOR UPDATE`, row.id); lockErr != nil {
			t.Fatalf("lock %s row: %v", row.table, lockErr)
		}
	}
	return tx
}

func assertRotatedFrom(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID, want *uuid.UUID) {
	t.Helper()
	var got *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT rotated_from FROM sessions WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read session lineage: %v", err)
	}
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Errorf("session %s rotated_from = %v, want %v", id, got, want)
	}
}
