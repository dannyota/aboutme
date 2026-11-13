package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"danny.vn/aboutme/apps/server/migrations"
)

const harnessTimeout = 30 * time.Second

func mapFS(files map[string]string) fstest.MapFS {
	m := make(fstest.MapFS, len(files))
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func mustQueryInt(ctx context.Context, t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func TestHarness_EmptyDatabaseToHead(t *testing.T) {
	t.Parallel()

	dsn := newTestDatabase(t)
	db, err := migrations.Open(dsn)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	results, err := migrations.Apply(ctx, db)
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Apply() applied zero migrations against an empty database")
	}

	statuses, err := migrations.Status(ctx, db)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	for _, s := range statuses {
		if s.State != goose.StateApplied {
			t.Errorf("migration %s state = %s, want %s", s.Source.Path, s.State, goose.StateApplied)
		}
	}

	installed := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_extension WHERE extname = 'citext'`)
	if installed != 1 {
		t.Errorf("pg_extension citext count = %d, want 1 after Apply()", installed)
	}

	results, err = migrations.Apply(ctx, db)
	if err != nil {
		t.Fatalf("second Apply() at head error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("second Apply() at head applied %d migrations, want 0", len(results))
	}
}

func TestHarness_PreviousReleaseStateToHead(t *testing.T) {
	t.Parallel()

	fsys := mapFS(map[string]string{
		"00001_release_v1.sql": "-- +goose Up\nCREATE TABLE release_v1 (id int PRIMARY KEY);\n",
		"00002_release_v2.sql": "-- +goose Up\nCREATE TABLE release_v2 (id int PRIMARY KEY);\n",
	})

	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)

	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	provider, err := migrations.NewProvider(db, fsys)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}

	if _, upToErr := provider.UpTo(ctx, 1); upToErr != nil {
		t.Fatalf("UpTo(1) error: %v", upToErr)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'release_v1'`); n != 1 {
		t.Fatalf("release_v1 table count = %d, want 1 after UpTo(1)", n)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'release_v2'`); n != 0 {
		t.Fatalf("release_v2 table count = %d, want 0 before the upgrade run", n)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	if len(results) != 1 || results[0].Source.Version != 2 {
		t.Fatalf("Up() applied %+v, want exactly migration version 2", results)
	}

	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'release_v2'`); n != 1 {
		t.Errorf("release_v2 table count = %d, want 1 after reaching head", n)
	}

	statuses, err := provider.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	for _, s := range statuses {
		if s.State != goose.StateApplied {
			t.Errorf("migration %s state = %s, want %s at head", s.Source.Path, s.State, goose.StateApplied)
		}
	}
}

const concurrencySleep = 1200 * time.Millisecond

// PostgreSQL advisory locks are session-scoped, so probe from one dedicated connection, not a pool.
func pollAdvisoryLock(ctx context.Context, t *testing.T, probeConn *sql.Conn, wantHeld bool) {
	t.Helper()

	const pollInterval = 10 * time.Millisecond
	deadline := time.Now().Add(harnessTimeout / 2)
	for time.Now().Before(deadline) {
		var locked bool
		if err := probeConn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, migrations.LockID).Scan(&locked); err != nil {
			t.Fatalf("pg_try_advisory_lock probe: %v", err)
		}
		if locked {
			if !wantHeld {
				if _, err := probeConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrations.LockID); err != nil {
					t.Fatalf("release probe lock: %v", err)
				}
				return
			}
			if _, err := probeConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrations.LockID); err != nil {
				t.Fatalf("release probe lock: %v", err)
			}
		} else if wantHeld {
			return
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("timed out waiting for advisory lock state held=%v", wantHeld)
}

func TestHarness_ConcurrentRunners_ExactlyOneApplies(t *testing.T) {
	t.Parallel()

	fsys := mapFS(map[string]string{
		"00001_slow_probe.sql": fmt.Sprintf(
			"-- +goose Up\nSELECT pg_sleep(%.3f);\nCREATE TABLE concurrency_probe (id serial PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT clock_timestamp());\nINSERT INTO concurrency_probe DEFAULT VALUES;\n",
			concurrencySleep.Seconds(),
		),
	})

	dsn := newTestDatabase(t)
	dbA := openTestDB(t, dsn)
	dbB := openTestDB(t, dsn)
	probeDB := openTestDB(t, dsn)

	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	probeConn, err := probeDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open probe connection: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := probeConn.Close(); closeErr != nil {
			t.Logf("close probe connection: %v", closeErr)
		}
	})

	// goose's lock retry period is in whole seconds with a minimum of 1 (lock.WithLockTimeout).
	lockOpts := []lock.SessionLockerOption{lock.WithLockTimeout(1, 30)}

	providerA, err := migrations.NewProvider(dbA, fsys, lockOpts...)
	if err != nil {
		t.Fatalf("NewProvider(A) error: %v", err)
	}
	providerB, err := migrations.NewProvider(dbB, fsys, lockOpts...)
	if err != nil {
		t.Fatalf("NewProvider(B) error: %v", err)
	}

	type outcome struct {
		results []*goose.MigrationResult
		err     error
	}
	outcomeA := make(chan outcome, 1)
	go func() {
		results, upErr := providerA.Up(ctx)
		outcomeA <- outcome{results: results, err: upErr}
	}()

	pollAdvisoryLock(ctx, t, probeConn, true)

	resultsB, errB := providerB.Up(ctx)
	oA := <-outcomeA

	if oA.err != nil {
		t.Fatalf("runner A: Up() error: %v", oA.err)
	}
	if errB != nil {
		t.Fatalf("runner B: Up() error: %v", errB)
	}

	applied, empty := 0, 0
	for i, res := range [][]*goose.MigrationResult{oA.results, resultsB} {
		switch len(res) {
		case 0:
			empty++
		case 1:
			if res[0].Source.Version != 1 {
				t.Fatalf("runner %d applied migration version = %d, want 1", i, res[0].Source.Version)
			}
			applied++
		default:
			t.Fatalf("runner %d: got %d results, want 0 or 1", i, len(res))
		}
	}
	if applied != 1 || empty != 1 {
		t.Fatalf("applied=%d empty=%d, want exactly one of each (no double-apply, no both-empty)", applied, empty)
	}

	rows := mustQueryInt(ctx, t, dbA, `SELECT count(*) FROM concurrency_probe`)
	if rows != 1 {
		t.Errorf("concurrency_probe row count = %d, want exactly 1 (no double-apply)", rows)
	}

	pollAdvisoryLock(ctx, t, probeConn, false)

	statuses, err := providerA.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	for _, s := range statuses {
		if s.State != goose.StateApplied {
			t.Errorf("migration %s state = %s, want %s", s.Source.Path, s.State, goose.StateApplied)
		}
	}
}

const partialFailureGood = "-- +goose Up\n" +
	"CREATE TABLE accounts (id int PRIMARY KEY, balance int NOT NULL);\n" +
	"INSERT INTO accounts (id, balance) VALUES (1, 100), (2, -5);\n"

const partialFailureBroken = "-- +goose Up\nALTER TABLE accounts ADD CONSTRAINT balance_non_negative CHECK (balance >= 0);\n"

func TestHarness_PartialFailureRecovers(t *testing.T) {
	t.Parallel()

	fsys := mapFS(map[string]string{
		"00001_good.sql":   partialFailureGood,
		"00002_broken.sql": partialFailureBroken,
	})

	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)

	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	provider, err := migrations.NewProvider(db, fsys)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}

	_, err = provider.Up(ctx)
	if err == nil {
		t.Fatal("Up() error = nil, want an error from migration 2's CHECK constraint violating account 2's pre-existing balance")
	}

	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM accounts`); n != 2 {
		t.Errorf("accounts row count = %d, want 2 (migration 1 must stay applied)", n)
	}
	// goose runs each SQL migration in its own transaction unless it is marked NO TRANSACTION.
	if n := mustQueryInt(ctx, t, db,
		`SELECT count(*) FROM pg_constraint WHERE conname = 'balance_non_negative'`,
	); n != 0 {
		t.Errorf("balance_non_negative constraint count = %d, want 0 (the failed migration's transaction must roll back completely)", n)
	}

	statuses, err := provider.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after failure error: %v", err)
	}
	state := map[int64]goose.State{}
	for _, s := range statuses {
		state[s.Source.Version] = s.State
	}
	if state[1] != goose.StateApplied {
		t.Errorf("migration 1 state = %s, want %s", state[1], goose.StateApplied)
	}
	if state[2] != goose.StatePending {
		t.Errorf("migration 2 state = %s, want %s (a failed migration must not be marked applied)", state[2], goose.StatePending)
	}
	if pending := migrations.PendingCount(statuses); pending != 1 {
		t.Errorf("PendingCount() = %d, want 1 (cmd/migrate -check would report 1 migration(s) pending and fail)", pending)
	}

	if _, err = db.ExecContext(ctx, `UPDATE accounts SET balance = 0 WHERE id = 2`); err != nil {
		t.Fatalf("repair pre-existing data: %v", err)
	}

	db2 := openTestDB(t, dsn)
	retryProvider, err := migrations.NewProvider(db2, fsys)
	if err != nil {
		t.Fatalf("NewProvider(retry) error: %v", err)
	}

	results, err := retryProvider.Up(ctx)
	if err != nil {
		t.Fatalf("Up() after data repair error: %v", err)
	}
	if len(results) != 1 || results[0].Source.Version != 2 {
		t.Fatalf("Up() after data repair applied %+v, want exactly migration version 2", results)
	}

	if n := mustQueryInt(ctx, t, db2,
		`SELECT count(*) FROM pg_constraint WHERE conname = 'balance_non_negative'`,
	); n != 1 {
		t.Errorf("balance_non_negative constraint count = %d, want 1 after recovery", n)
	}

	finalStatuses, err := retryProvider.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after recovery error: %v", err)
	}
	for _, s := range finalStatuses {
		if s.State != goose.StateApplied {
			t.Errorf("migration %s state = %s, want %s after recovery", s.Source.Path, s.State, goose.StateApplied)
		}
	}
	if pending := migrations.PendingCount(finalStatuses); pending != 0 {
		t.Errorf("PendingCount() after recovery = %d, want 0 (cmd/migrate -check would report up to date)", pending)
	}
}
