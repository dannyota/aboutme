// Tests for the lock-wait/migration deadline split (item 4 of the
// data-layer review, review-datalayer.txt "Important" — cmd/migrate/main.go's
// old single five-minute timeout doubled as goose's own lock-wait budget,
// so a runner that legitimately waited the full budget for a contended
// advisory lock could acquire it with zero time left to actually apply or
// check anything).
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// TestBudgets_OuterExceedsLockWaitAndMigrationBudgetsIndependently is a
// hermetic, deterministic guard on the budgets arithmetic itself: no
// database or timing involved, just the invariant the review requires —
// "the outer deadline strictly greater than both [the lock-wait budget and
// the migration budget]". This is what would have caught the original bug
// immediately: before this task, there was no separate lock-wait/migration
// split at all, so outer() (then just the single `timeout` constant) was
// exactly goose's own default lock-wait budget — the exact regression the
// last assertion below guards against by name.
func TestBudgets_OuterExceedsLockWaitAndMigrationBudgetsIndependently(t *testing.T) {
	t.Parallel()

	b := defaultBudgets
	if b.outer() <= b.lockWait() {
		t.Errorf("outer() = %s, want strictly greater than lockWait() = %s", b.outer(), b.lockWait())
	}
	if b.outer() <= b.migration {
		t.Errorf("outer() = %s, want strictly greater than migration budget = %s", b.outer(), b.migration)
	}
	if b.outer() == b.lockWait() {
		t.Error("outer() == lockWait(): a contender that waits the full lock-wait budget would have zero time left to migrate")
	}
}

// TestBudgets_ValidateAcceptsDefaultBudgets guards against a validate()
// that's too strict for the value actually shipped in production.
func TestBudgets_ValidateAcceptsDefaultBudgets(t *testing.T) {
	t.Parallel()

	if err := defaultBudgets.validate(); err != nil {
		t.Errorf("defaultBudgets.validate() error = %v, want nil", err)
	}
}

// TestBudgets_ValidateRejectsSubSecondLockPeriod is the regression test
// for review finding M8: a lockPeriod under one second previously
// truncated silently to 0 in lockOpts' uint64(lockPeriod/time.Second)
// (integer division), while lockWait() kept computing outer()'s deadline
// from the untruncated value — a budget that looks internally consistent
// but isn't. validate() must catch this before run() ever builds a
// context deadline from it.
func TestBudgets_ValidateRejectsSubSecondLockPeriod(t *testing.T) {
	t.Parallel()

	b := budgets{lockPeriod: 500 * time.Millisecond, lockRetries: 6, migration: 10 * time.Second, slack: 5 * time.Second}
	err := b.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want an error for a sub-second lockPeriod")
	}
	if !strings.Contains(err.Error(), "lockPeriod") {
		t.Errorf("validate() error = %q, want it to mention lockPeriod", err)
	}
}

// TestBudgets_ValidateRejectsZeroValue guards the zero value specifically
// (the review noted it fails closed today via a chain of downstream
// errors — PingContext against an already-expired context, then goose's
// own WithLockTimeout(0, 0) rejection — but validate() should reject it
// directly and clearly instead of relying on that chain).
func TestBudgets_ValidateRejectsZeroValue(t *testing.T) {
	t.Parallel()

	var b budgets
	if err := b.validate(); err == nil {
		t.Fatal("validate() error = nil, want an error for the zero value")
	}
}

func TestBudgets_ValidateRejectsZeroLockRetries(t *testing.T) {
	t.Parallel()

	b := budgets{lockPeriod: 5 * time.Second, lockRetries: 0, migration: 10 * time.Second, slack: 5 * time.Second}
	err := b.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want an error for zero lockRetries")
	}
	if !strings.Contains(err.Error(), "lockRetries") {
		t.Errorf("validate() error = %q, want it to mention lockRetries", err)
	}
}

func TestBudgets_ValidateRejectsNonPositiveMigrationBudget(t *testing.T) {
	t.Parallel()

	b := budgets{lockPeriod: 5 * time.Second, lockRetries: 6, migration: 0, slack: 5 * time.Second}
	err := b.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want an error for a zero migration budget")
	}
	if !strings.Contains(err.Error(), "migration") {
		t.Errorf("validate() error = %q, want it to mention the migration budget", err)
	}
}

func TestBudgets_ValidateRejectsNegativeSlack(t *testing.T) {
	t.Parallel()

	b := budgets{lockPeriod: 5 * time.Second, lockRetries: 6, migration: 10 * time.Second, slack: -1 * time.Second}
	err := b.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want an error for negative slack")
	}
	if !strings.Contains(err.Error(), "slack") {
		t.Errorf("validate() error = %q, want it to mention slack", err)
	}
}

// TestRun_InvalidBudgets_FailsFastWithoutDatabaseURL proves run() checks
// budgets.validate() before even requiring DATABASE_URL/ENV: the cheapest,
// dependency-free check goes first, so an invalid configuration reports
// itself rather than hiding behind a connection error.
func TestRun_InvalidBudgets_FailsFastWithoutDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ENV", "")

	orig := runBudgets
	runBudgets = budgets{lockPeriod: 500 * time.Millisecond, lockRetries: 6, migration: 10 * time.Second, slack: 5 * time.Second}
	t.Cleanup(func() { runBudgets = orig })

	err := run(false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want an invalid-budgets error")
	}
	if !strings.Contains(err.Error(), "lockPeriod") {
		t.Errorf("run() error = %q, want it to mention lockPeriod (not a DATABASE_URL error — budgets must be checked first)", err)
	}
}

// waitForLockContention polls from a dedicated probe connection (a single
// stable session — advisory locks are session-scoped, so a pooled *sql.DB
// could observe a different underlying connection on each call) until
// pg_try_advisory_lock(migrations.LockID) returns false, proving some
// other session currently holds it. This is an observation loop over real
// database state, not a fixed sleep used as a synchronization guess — the
// same technique migrations/harness_test.go's concurrency test uses.
func waitForLockContention(ctx context.Context, t *testing.T, dsn string) {
	t.Helper()

	probeDB := openMigrateTestDB(t, dsn)
	probeConn, err := probeDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open probe connection: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := probeConn.Close(); closeErr != nil {
			t.Logf("close probe connection: %v", closeErr)
		}
	})

	const pollInterval = 10 * time.Millisecond
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var locked bool
		if err := probeConn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, migrations.LockID).Scan(&locked); err != nil {
			t.Fatalf("pg_try_advisory_lock probe: %v", err)
		}
		if !locked {
			return
		}
		// The probe itself acquired the lock (nobody else holds it yet):
		// release immediately and retry shortly.
		if _, err := probeConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrations.LockID); err != nil {
			t.Fatalf("release probe lock: %v", err)
		}
		time.Sleep(pollInterval)
	}
	t.Fatal("timed out waiting to observe the advisory lock held by another session")
}

// TestRun_DeadlineBudgets_ContenderSucceedsAfterApproachingLockWait is the
// item-4 live-database regression test: a lock hold that approaches (but
// stays under) the configured lock-wait budget must still leave the
// contender enough of its separate migration budget to succeed.
func TestRun_DeadlineBudgets_ContenderSucceedsAfterApproachingLockWait(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	// Small budgets so the test runs in seconds, not five real minutes —
	// the lock-wait/migration *split* under test doesn't depend on the
	// production constants' exact values (see
	// TestBudgets_OuterExceedsLockWaitAndMigrationBudgetsIndependently for
	// that).
	origBudgets := runBudgets
	runBudgets = budgets{
		lockPeriod:  1 * time.Second,
		lockRetries: 6, // 6s lock-wait budget
		migration:   10 * time.Second,
		slack:       5 * time.Second,
	}
	t.Cleanup(func() { runBudgets = origBudgets })

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("ENV", "dev")
	t.Setenv("PUBLIC_ORIGIN", "https://aboutme.vn")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	holderDB := openMigrateTestDB(t, dsn)
	holderConn, err := holderDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open holder connection: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := holderConn.Close(); closeErr != nil {
			t.Logf("close holder connection: %v", closeErr)
		}
	})
	if _, err := holderConn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrations.LockID); err != nil {
		t.Fatalf("acquire holder lock: %v", err)
	}

	// Hold for 4s of the 6s lock-wait budget (~67%): long enough that a
	// broken outer()==lockWait() composition would leave the contender's
	// Apply() call racing an already-near-exhausted context, short enough
	// to keep the test fast.
	const holdDuration = 4 * time.Second
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(holdDuration)
		if _, unlockErr := holderConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrations.LockID); unlockErr != nil {
			t.Errorf("release holder lock: %v", unlockErr)
		}
	}()
	t.Cleanup(func() { <-released })

	// Deterministically confirm the holder actually holds the lock before
	// starting the contender, so the contender's wait genuinely starts
	// from (close to) t=0 rather than after unpredictable goroutine
	// scheduling.
	waitForLockContention(ctx, t, dsn)

	var buf bytes.Buffer
	start := time.Now()
	runErr := run(false, &buf)
	elapsed := time.Since(start)

	if runErr != nil {
		t.Fatalf("run() error = %v (after %s), want the contender to wait for the lock and then succeed: output=%q",
			runErr, elapsed, buf.String())
	}
	if elapsed < holdDuration*7/10 {
		t.Errorf("run() returned after %s, want it to have genuinely waited for the lock (hold duration %s): output=%q",
			elapsed, holdDuration, buf.String())
	}
	t.Logf("run() waited %s for a %s lock hold, then succeeded: %s", elapsed, holdDuration, buf.String())
}

// maxCheckElapsed bounds how long `run(true, ...)` (`-check`) may take
// while another session holds migrations.LockID. Generous relative to a
// real query round-trip, far below any plausible lock-wait retry interval
// (goose's own default alone is 5s, and this test's own runBudgets
// override below uses a 6s lock-wait budget) — so a regression that makes
// -check contend for the lock even once, however briefly, fails this
// bound.
const maxCheckElapsed = 2 * time.Second

// TestRun_Check_DoesNotBlockOnAdvisoryLock is the cmd/migrate-level
// regression test for review finding C1: `migrate -check` (run(true,
// ...)) must return promptly even while another session holds
// migrations.LockID applying migrations — this package's own doc comment
// (main.go's package comment) calls -check "a scriptable drift check,
// e.g. in a pre-deploy readiness gate", which is only true if it can run
// *during* a deploy rather than blocking for the full lock-wait budget and
// then reporting a false failure. Uses a short runBudgets override purely
// so a regression to lock-taking behavior fails this test in seconds
// rather than minutes; -check's actual lock-freedom does not depend on
// budgets at all (see migrations.Status's doc comment).
func TestRun_Check_DoesNotBlockOnAdvisoryLock(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	origBudgets := runBudgets
	runBudgets = budgets{
		lockPeriod:  1 * time.Second,
		lockRetries: 6, // 6s lock-wait budget: irrelevant to -check if the fix holds, load-bearing if it doesn't
		migration:   10 * time.Second,
		slack:       5 * time.Second,
	}
	t.Cleanup(func() { runBudgets = origBudgets })

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("ENV", "dev")
	t.Setenv("PUBLIC_ORIGIN", "https://aboutme.vn")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Migrate to head first (no lock contention yet), so -check's only
	// possible reason to report a non-nil error later is a genuine
	// pending migration — not simply "this fresh database was never
	// migrated" masking the lock-contention behavior under test.
	setupDB := openMigrateTestDB(t, dsn)
	if _, err := migrations.Apply(ctx, setupDB); err != nil {
		t.Fatalf("setup Apply() error: %v", err)
	}

	holderDB := openMigrateTestDB(t, dsn)
	holderConn, err := holderDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open holder connection: %v", err)
	}
	t.Cleanup(func() {
		if _, unlockErr := holderConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrations.LockID); unlockErr != nil {
			t.Logf("release holder lock: %v", unlockErr)
		}
		if closeErr := holderConn.Close(); closeErr != nil {
			t.Logf("close holder connection: %v", closeErr)
		}
	})
	if _, err := holderConn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrations.LockID); err != nil {
		t.Fatalf("acquire holder lock: %v", err)
	}

	// Deterministically confirm the holder actually holds the lock before
	// measuring -check, so a pass can't be explained by the holder simply
	// not having acquired it yet.
	waitForLockContention(ctx, t, dsn)

	var buf bytes.Buffer
	start := time.Now()
	runErr := run(true, &buf)
	elapsed := time.Since(start)

	if runErr != nil {
		t.Fatalf("run(true, ...) error while another session holds the advisory lock: %v (after %s): output=%q",
			runErr, elapsed, buf.String())
	}
	if elapsed > maxCheckElapsed {
		t.Errorf("run(true, ...) took %s while the advisory lock was held elsewhere, want < %s — "+
			"a pre-deploy readiness check must never contend for the lock, not merely have a short retry budget",
			elapsed, maxCheckElapsed)
	}
	t.Logf("run(true, ...) returned in %s while the lock was held elsewhere: %s", elapsed, buf.String())
}
