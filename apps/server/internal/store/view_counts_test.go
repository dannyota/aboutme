package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

type viewCountWrite struct {
	name   string
	add    func(ctx context.Context, q *store.Queries, ids []uuid.UUID, amount int32) error
	stored string
}

func viewCountWrites() []viewCountWrite {
	day := pgtype.Date{Time: time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC), Valid: true}
	repeat := func(n int, day pgtype.Date) []pgtype.Date {
		days := make([]pgtype.Date, n)
		for i := range days {
			days[i] = day
		}
		return days
	}
	fill := func(n int, value int32) []int32 {
		values := make([]int32, n)
		for i := range values {
			values[i] = value
		}
		return values
	}
	return []viewCountWrite{
		{
			name: "view days",
			add: func(ctx context.Context, q *store.Queries, ids []uuid.UUID, amount int32) error {
				n := len(ids)
				return q.AddResumeViewDays(ctx, store.AddResumeViewDaysParams{
					ResumeIds: ids, Days: repeat(n, day), Counted: fill(n, amount), Bot: fill(n, 0),
					Datacenter: fill(n, 0), Anomaly: fill(n, 0), Invalid: fill(n, 0), Crawler: fill(n, 0),
				})
			},
			stored: `SELECT coalesce(sum(counted), 0) FROM resume_view_days WHERE resume_id = $1`,
		},
		{
			name: "share signals",
			add: func(ctx context.Context, q *store.Queries, ids []uuid.UUID, amount int32) error {
				platforms := make([]string, len(ids))
				for i := range platforms {
					platforms[i] = "zalo"
				}
				return q.AddResumeShareSignalDays(ctx, store.AddResumeShareSignalDaysParams{
					ResumeIds: ids, Days: repeat(len(ids), day), Platforms: platforms, Fetches: fill(len(ids), amount),
				})
			},
			stored: `SELECT coalesce(sum(fetches), 0) FROM resume_share_signal_days WHERE resume_id = $1`,
		},
	}
}

func newViewCountEnv(t *testing.T) (context.Context, *pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	var userID uuid.UUID
	if err = pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, 'Views') RETURNING id`,
		uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("delete user: %v", cleanupErr)
		}
	})
	ids := make([]uuid.UUID, 2)
	for i := range ids {
		if err = pool.QueryRow(ctx, `
			INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
			VALUES ($1, 'Views', 2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb) RETURNING id`, userID).Scan(&ids[i]); err != nil {
			t.Fatalf("insert resume: %v", err)
		}
	}
	return ctx, pool, ids[0], ids[1]
}

func TestViewCountFlushSkipsAResumeDeletedDuringTheFlush(t *testing.T) {
	for _, write := range viewCountWrites() {
		t.Run(write.name, func(t *testing.T) {
			ctx, pool, kept, deleted := newViewCountEnv(t)
			deletion, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin deletion: %v", err)
			}
			t.Cleanup(func() { rollbackPublicTestTx(t, deletion) })
			var deletionPID int32
			if err = deletion.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletionPID); err != nil {
				t.Fatalf("deletion backend PID: %v", err)
			}
			if _, err = deletion.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, deleted); err != nil {
				t.Fatalf("delete resume: %v", err)
			}

			flushed := make(chan error, 1)
			go func() { flushed <- write.add(ctx, store.New(pool), []uuid.UUID{kept, deleted}, 3) }()
			waitForBackendBlockedBy(ctx, t, pool, deletionPID)
			if err = deletion.Commit(ctx); err != nil {
				t.Fatalf("commit deletion: %v", err)
			}
			select {
			case err = <-flushed:
				if err != nil {
					t.Fatalf("flush racing a resume deletion: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the flush did not finish after the deletion committed")
			}

			for id, want := range map[uuid.UUID]int64{kept: 3, deleted: 0} {
				var got int64
				if err = pool.QueryRow(ctx, write.stored, id).Scan(&got); err != nil {
					t.Fatalf("read stored counts: %v", err)
				}
				if got != want {
					t.Errorf("stored counts for resume %s = %d, want %d", id, got, want)
				}
			}
		})
	}
}

func TestViewCountFlushBlocksOnlyResumeDeletion(t *testing.T) {
	for _, write := range viewCountWrites() {
		t.Run(write.name, func(t *testing.T) {
			ctx, pool, resumeID, _ := newViewCountEnv(t)
			flush, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin flush: %v", err)
			}
			t.Cleanup(func() { rollbackPublicTestTx(t, flush) })
			if err = write.add(ctx, store.New(flush), []uuid.UUID{resumeID}, 1); err != nil {
				t.Fatalf("flush: %v", err)
			}

			if err = execWithLockTimeout(ctx, t, pool, `UPDATE resumes SET title = 'Renamed' WHERE id = $1`, resumeID); err != nil {
				t.Fatalf("update during an open flush: %v", err)
			}
			var pgErr *pgconn.PgError
			err = execWithLockTimeout(ctx, t, pool, `DELETE FROM resumes WHERE id = $1`, resumeID)
			if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
				t.Fatalf("delete during an open flush error = %v, want lock_not_available", err)
			}
		})
	}
}

func execWithLockTimeout(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			t.Errorf("rollback: %v", rollbackErr)
		}
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, sql, args...)
	return err
}

func waitForBackendBlockedBy(ctx context.Context, t *testing.T, pool *pgxpool.Pool, holder int32) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1::int = ANY (pg_blocking_pids(pid)))`,
			holder).Scan(&blocked); err != nil {
			t.Fatalf("probe blocked backend: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("no backend waited on backend %d", holder)
		case <-ticker.C:
		}
	}
}
