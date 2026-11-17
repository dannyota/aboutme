package resume_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func TestExecuteBeforeCommit_RunsOnceAfterTheRecordAndJustBeforeCommit(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("before commit"))
	var calls atomic.Int32
	var callsAtCommit int32
	var recordsAtCommit int
	idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now, nil,
		func(commitCtx context.Context, tx pgx.Tx) error {
			callsAtCommit = calls.Load()
			if err := tx.QueryRow(commitCtx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND idempotency_key = $2`,
				userID, key).Scan(&recordsAtCommit); err != nil {
				return err
			}
			return tx.Commit(commitCtx)
		})
	mutate := func(*store.Queries) (resume.StoredResponse, error) {
		return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"done":true}`), BeforeCommit: func() { calls.Add(1) }}, nil
	}
	result, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil || result.Outcome != resume.CommitCommitted || result.Replayed {
		t.Fatalf("Execute = (%+v, %v), want a fresh commit", result, err)
	}
	if callsAtCommit != 1 || recordsAtCommit != 1 || calls.Load() != 1 {
		t.Fatalf("at commit: hook calls = %d, records = %d; after: calls = %d; want 1, 1, 1", callsAtCommit, recordsAtCommit, calls.Load())
	}
	replay, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil || !replay.Replayed || calls.Load() != 1 {
		t.Fatalf("replay = (%+v, %v), hook calls = %d; want a replay without the hook", replay, err, calls.Load())
	}
}

func TestExecuteBeforeCommit_SkippedWhenStorageRollsBack(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	var calls atomic.Int32
	hook := func() { calls.Add(1) }
	for _, tc := range []struct {
		name   string
		before func()
		status int
	}{
		{"unstorable status", func() {}, 0},
		{"byte capacity", func() {
			if _, err := pool.Exec(ctx, `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes) VALUES ($1, 0, $2)
				ON CONFLICT (user_id) DO UPDATE SET stored_bytes = EXCLUDED.stored_bytes`, userID, int64(1<<30)); err != nil {
				t.Fatal(err)
			}
		}, 200},
	} {
		tc.before()
		key := uuid.New()
		_, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, sha256.Sum256([]byte(tc.name)),
			func(*store.Queries) (resume.StoredResponse, error) {
				return resume.StoredResponse{Status: tc.status, Body: json.RawMessage(`{"stored":false}`), BeforeCommit: hook}, nil
			})
		if err == nil {
			t.Fatalf("%s: Execute succeeded", tc.name)
		}
		if _, getErr := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
		}); !errors.Is(getErr, pgx.ErrNoRows) {
			t.Fatalf("%s: record after a rollback: %v", tc.name, getErr)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("hook calls after rolled-back storage = %d, want 0", calls.Load())
	}
}
