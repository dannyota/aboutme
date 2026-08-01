// Migration harness tests (task 0.3b): empty database -> head, previous-
// release state -> head, two concurrent runners, and partial-failure
// recovery — run against a real Postgres instance, gated behind
// TEST_DATABASE_URL exactly like internal/store's integration test (see
// testdb_test.go's newTestDatabase, and the package comment there).
//
// The "previous release", "concurrent runners", and "partial failure"
// scenarios use small synthetic migration sets built with fstest.MapFS
// instead of the real embedded FS. This keeps the harness's correctness
// independent of how many real product migrations exist at any given
// time (today: exactly one, the hand-written citext extension — see
// migrations.go's package comment) while still exercising the exact same
// production code path (migrations.NewProvider, the same goose Provider +
// session-locker machinery cmd/migrate uses). Only the "empty database ->
// head" test needs the real schema, since its whole point is proving the
// real embedded migrations apply cleanly end-to-end.
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

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// harnessTimeout bounds every harness test's context. Generous relative to
// the deliberately slow (pg_sleep) concurrency test below, tight enough
// that a genuine deadlock still fails the test suite instead of hanging
// CI.
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

// -----------------------------------------------------------------------
// Scenario 1: empty database -> head, using the real embedded migrations.
// -----------------------------------------------------------------------

func TestHarness_EmptyDatabaseToHead(t *testing.T) {
	t.Parallel()

	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)

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

	// Confirm real product state, not just goose's own bookkeeping: the
	// hand-written extensions migration (migrations/00001_extensions.sql)
	// must have actually installed citext.
	installed := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_extension WHERE extname = 'citext'`)
	if installed != 1 {
		t.Errorf("pg_extension citext count = %d, want 1 after Apply()", installed)
	}

	// Re-applying at head must be a safe no-op.
	results, err = migrations.Apply(ctx, db)
	if err != nil {
		t.Fatalf("second Apply() at head error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("second Apply() at head applied %d migrations, want 0", len(results))
	}
}

// -----------------------------------------------------------------------
// Scenario 2: previous-release state -> head, using a synthetic 2-step
// migration set so the test means the same thing regardless of how many
// real migrations exist.
// -----------------------------------------------------------------------

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

	// Simulate "previous release": only the first migration has ever run,
	// same as a database that was last migrated before release_v2 shipped.
	if _, upToErr := provider.UpTo(ctx, 1); upToErr != nil {
		t.Fatalf("UpTo(1) error: %v", upToErr)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'release_v1'`); n != 1 {
		t.Fatalf("release_v1 table count = %d, want 1 after UpTo(1)", n)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'release_v2'`); n != 0 {
		t.Fatalf("release_v2 table count = %d, want 0 before the upgrade run", n)
	}

	// The upgrade: a runner starting from that previous-release state must
	// reach head, applying only what's missing.
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

// -----------------------------------------------------------------------
// Scenario 3: two concurrent runners — exactly one applies, the other
// waits and observes the applied state; no double-apply, no deadlock.
// -----------------------------------------------------------------------

// concurrencySleep is how long the single synthetic migration sleeps
// server-side (via pg_sleep) before doing its real work — long enough that
// runner A is virtually guaranteed to still be inside it (and so still
// holding the advisory lock) when the deterministic contention probe below
// observes it, short enough to keep the suite fast.
const concurrencySleep = 1200 * time.Millisecond

// pollAdvisoryLock polls from probeConn (a single dedicated connection —
// advisory locks are session-scoped, so a pooled *sql.DB could hand back a
// different underlying session on each call and silently defeat this
// check) until pg_try_advisory_lock(migrations.LockID) reports the state
// wantHeld: true waits for some other session to hold the lock (contention
// observed), false waits for nobody to hold it (fully released). Failing
// probe attempts that transiently acquire the lock themselves release it
// immediately before retrying, so this never itself holds the lock as a
// side effect of checking. This is an observation loop over real database
// state — not a fixed sleep used as a synchronization guess — so
// contention is proven, not inferred from timing.
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
				// Confirmed: nobody else holds it. Release our own
				// just-acquired grant and return.
				if _, err := probeConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrations.LockID); err != nil {
					t.Fatalf("release probe lock: %v", err)
				}
				return
			}
			// We hold it now (nobody else does yet): release immediately
			// and keep polling for wantHeld=true.
			if _, err := probeConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrations.LockID); err != nil {
				t.Fatalf("release probe lock: %v", err)
			}
		} else if wantHeld {
			return // confirmed: some other session holds it
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
	// Independent *sql.DB pools against the SAME database, standing in for
	// separate runner processes (e.g. two ECS tasks deploying at once) —
	// not goroutines sharing one pool, which would prove nothing about
	// cross-process locking.
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

	// Poll fast: goose's default session-locker retry (5s intervals, up to
	// 5 minutes) is fine in production but would make this test slow. The
	// lock mechanism under test is real Postgres pg_try_advisory_lock
	// contention either way — only the polling cadence changes. 1 second
	// is the library's minimum period; 30 retries gives a 30s budget,
	// comfortably above concurrencySleep.
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

	// Deterministically wait until A actually holds the advisory lock
	// before starting B, so B genuinely contends from the start instead of
	// (best case) racing a channel-close barrier and (worst case) starting
	// so late the two never overlap at all.
	pollAdvisoryLock(ctx, t, probeConn, true)

	resultsB, errB := providerB.Up(ctx)
	oA := <-outcomeA

	if oA.err != nil {
		t.Fatalf("runner A: Up() error: %v", oA.err)
	}
	if errB != nil {
		t.Fatalf("runner B: Up() error: %v", errB)
	}

	// Exactly one runner actually applied version 1; the other, having
	// waited out the lock, re-checked and found nothing pending.
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

	// No double-apply: the migration's INSERT ran exactly once, regardless
	// of which runner's Up() call actually executed it.
	rows := mustQueryInt(ctx, t, dbA, `SELECT count(*) FROM concurrency_probe`)
	if rows != 1 {
		t.Errorf("concurrency_probe row count = %d, want exactly 1 (no double-apply)", rows)
	}

	// Both runners have fully released the lock: the probe can now
	// acquire it itself.
	pollAdvisoryLock(ctx, t, probeConn, false)

	statuses, err := migrations.Status(ctx, dbA)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	for _, s := range statuses {
		if s.State != goose.StateApplied {
			t.Errorf("migration %s state = %s, want %s", s.Source.Path, s.State, goose.StateApplied)
		}
	}
}

// -----------------------------------------------------------------------
// Scenario 4: partial-failure recovery — a migration fails against
// repairable pre-existing data (not a bug in the migration itself), the
// data is repaired, and an unchanged rerun reports and recovers correctly.
// -----------------------------------------------------------------------

// good seeds accounts with one valid row and one row that will later
// violate a constraint migration 2 adds — a stand-in for pre-existing
// production data that predates a new invariant, not a bug in either
// migration's SQL.
const partialFailureGood = "-- +goose Up\n" +
	"CREATE TABLE accounts (id int PRIMARY KEY, balance int NOT NULL);\n" +
	"INSERT INTO accounts (id, balance) VALUES (1, 100), (2, -5);\n"

// partialFailureBroken adds a CHECK constraint that account 2's
// pre-existing balance (-5, from partialFailureGood above) violates.
// Unlike a real bug in the migration's own SQL, this file's bytes are
// never edited between the failing run and the recovery below — see the
// test body: recovery repairs the *data*, then reruns this exact,
// unchanged file, matching how an append-only, immutable migration
// history (design spec §3 "Lifecycle" post-release; the CI append-only
// gate at .github/workflows/ci.yml's migrations-append-only job) is
// actually recovered from in production. A prior version of this test
// instead swapped in a second file with corrected SQL under the same
// version number, which demonstrates a pre-release *edit*, not recovery —
// see review-datalayer.txt's "Important" findings.
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

	// Recoverable state, part 1: migration 1's effects (both accounts,
	// including the bad balance) are intact...
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM accounts`); n != 2 {
		t.Errorf("accounts row count = %d, want 2 (migration 1 must stay applied)", n)
	}
	// ...and migration 2's effects were fully rolled back, not
	// half-applied: goose runs each SQL migration inside its own
	// transaction by default (no "-- +goose NO TRANSACTION" marker here),
	// so the failed ALTER TABLE must leave no trace of the constraint.
	if n := mustQueryInt(ctx, t, db,
		`SELECT count(*) FROM pg_constraint WHERE conname = 'balance_non_negative'`,
	); n != 0 {
		t.Errorf("balance_non_negative constraint count = %d, want 0 (the failed migration's transaction must roll back completely)", n)
	}

	// The next status check reports correctly: migration 1 applied,
	// migration 2 still pending — not silently marked applied, not stuck
	// reporting an error forever. Checked via migrations.PendingCount, the
	// exact function cmd/migrate's `-check` flag uses to decide whether to
	// fail (see cmd/migrate/main.go's runCheck) — so this assertion is
	// equivalent to running `cmd/migrate -check` against this database at
	// this point, not a parallel reimplementation of its logic.
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

	// Recovery: repair the pre-existing bad data — not the migration file,
	// which stays byte-for-byte the same fsys defined above — then rerun
	// through a SECOND, independent *sql.DB pool against the same
	// database. A second pool (rather than reusing db) rules out the
	// retry passing merely because a leaked session from the first pool
	// happened to still be holding (and could trivially re-acquire, via
	// Postgres's reentrant advisory-lock semantics) the lock on the exact
	// same physical connection: this forces a genuinely fresh lock
	// acquisition.
	if _, err = db.ExecContext(ctx, `UPDATE accounts SET balance = 0 WHERE id = 2`); err != nil {
		t.Fatalf("repair pre-existing data: %v", err)
	}

	db2 := openTestDB(t, dsn)
	retryProvider, err := migrations.NewProvider(db2, fsys) // fsys: byte-identical to the failing run above
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
