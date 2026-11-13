package resume_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

type insertConflictTx struct {
	pgx.Tx
	insertErr error
}

func (tx *insertConflictTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.HasPrefix(query, "-- name: GetIdempotencyRecord :one"):
		return queryRowFailure{err: pgx.ErrNoRows}
	case tx.insertErr != nil && strings.HasPrefix(query, "-- name: CreateIdempotencyRecord :one"):
		return queryRowFailure{err: tx.insertErr}
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

type seededConflict struct {
	userID       uuid.UUID
	key          uuid.UUID
	hash         [32]byte
	winnerExpiry time.Duration
	insertErr    error
}

func executeWithSeededConflict(ctx context.Context, t *testing.T, q *store.Queries, pool *store.Pool,
	now func() time.Time, c seededConflict, mutate func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, int, error) {
	t.Helper()
	begins := 0
	idem := resume.NewIdempotencyStoreWithHooksForTest(pool, now,
		func(ctx context.Context) (pgx.Tx, error) {
			begins++
			var insertErr error
			if begins == 2 {
				createIdempotencyRecord(ctx, t, q, c.userID, idempotencyTestRoute, c.key, c.hash, now().Add(c.winnerExpiry))
				insertErr = c.insertErr
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				return nil, err
			}
			return &insertConflictTx{Tx: tx, insertErr: insertErr}, nil
		}, nil)
	result, err := idem.Execute(ctx, c.userID, idempotencyTestRoute, c.key, c.hash, mutate)
	return result, begins, err
}

func TestIdempotencyConflict_RequiresExactConstraint(t *testing.T) {
	t.Parallel()
	clock := testutil.NewClockAtEpoch()
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	fresh := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"fresh":true}`)}
	mutate := func(*store.Queries) (resume.StoredResponse, error) { return fresh, nil }

	tests := []struct {
		name         string
		winnerExpiry time.Duration
		insertErr    *pgconn.PgError
	}{
		{
			name:         "exact constraint replays the live winner",
			winnerExpiry: time.Hour,
		},
		{
			name:         "another unique constraint returns the error",
			winnerExpiry: time.Hour,
			insertErr:    &pgconn.PgError{Code: "23505", ConstraintName: "idempotency_records_pkey"},
		},
		{
			name:         "another SQLSTATE returns the error",
			winnerExpiry: time.Hour,
			insertErr:    &pgconn.PgError{Code: "23503", ConstraintName: "idempotency_records_user_route_key_key"},
		},
		{
			name:         "another unique constraint never replays an expired record",
			winnerExpiry: -time.Hour,
			insertErr:    &pgconn.PgError{Code: "23505", ConstraintName: "idempotency_records_pkey"},
		},
		{
			name:         "another SQLSTATE never replays an expired record",
			winnerExpiry: -time.Hour,
			insertErr:    &pgconn.PgError{Code: "23503", ConstraintName: "idempotency_records_user_route_key_key"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := seededConflict{
				userID:       createTestUserWithContext(ctx, t, q),
				key:          uuid.New(),
				hash:         sha256.Sum256([]byte(tc.name)),
				winnerExpiry: tc.winnerExpiry,
			}
			if tc.insertErr != nil {
				c.insertErr = tc.insertErr
			}
			result, begins, err := executeWithSeededConflict(ctx, t, q, pool, clock.Now, c, mutate)

			if begins != 2 {
				t.Fatalf("Execute began %d transactions, want a cleanup and a mutation transaction", begins)
			}
			if tc.insertErr == nil {
				if err != nil || !result.Replayed || result.Outcome != resume.CommitCommitted {
					t.Fatalf("Execute() = (%+v, %v), want the committed winner replayed", result, err)
				}
				assertJSONEqual(t, result.Response.Body, json.RawMessage(`{"seeded":true}`))
				return
			}
			if !errors.Is(err, tc.insertErr) {
				t.Errorf("Execute() error = %v, want the insert error %v returned without recovery", err, tc.insertErr)
			}
			if result.Replayed || result.Outcome != resume.CommitDefinitelyRolledBack {
				t.Errorf("Execute() = %+v, want a rolled-back result that replays nothing", result)
			}
		})
	}

	t.Run("exact constraint refuses an expired record and the retry runs fresh", func(t *testing.T) {
		calls := 0
		counted := func(*store.Queries) (resume.StoredResponse, error) {
			calls++
			return fresh, nil
		}
		c := seededConflict{
			userID:       createTestUserWithContext(ctx, t, q),
			key:          uuid.New(),
			hash:         sha256.Sum256([]byte(t.Name())),
			winnerExpiry: -time.Hour,
			insertErr:    &pgconn.PgError{Code: "23505", ConstraintName: "idempotency_records_user_route_key_key"},
		}
		got, begins, err := executeWithSeededConflict(ctx, t, q, pool, clock.Now, c, counted)
		if err == nil || got.Replayed || got.Outcome != resume.CommitDefinitelyRolledBack {
			t.Fatalf("Execute() = %+v, %v; want a definite rollback that replays nothing", got, err)
		}
		if begins != 2 || calls != 1 {
			t.Fatalf("Execute began %d transactions and ran the mutation %d times, want 2 and 1", begins, calls)
		}

		retry, err := resume.NewIdempotencyStoreForTest(pool, clock.Now).Execute(ctx, c.userID, idempotencyTestRoute, c.key, c.hash, counted)
		if err != nil || retry.Replayed || retry.Outcome != resume.CommitCommitted || calls != 2 {
			t.Fatalf("retry = %+v, %v after %d mutations; want a fresh commit", retry, err, calls)
		}
		row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID: c.userID, Route: idempotencyTestRoute, IdempotencyKey: c.key,
		})
		if err != nil {
			t.Fatalf("GetIdempotencyRecord() error = %v", err)
		}
		if wantExpiry := clock.Now().Add(resume.IdempotencyTTL); !row.ExpiresAt.Equal(wantExpiry) {
			t.Errorf("stored record expires at %s, want the fresh record's %s", row.ExpiresAt, wantExpiry)
		}
		assertJSONEqual(t, row.ResponseBody, fresh.Body)
		assertIdempotencyUsageMatchesRows(ctx, t, pool, c.userID)
	})
}
