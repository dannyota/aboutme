package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/migrations"
)

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

func TestBudgets_ValidateAcceptsDefaultBudgets(t *testing.T) {
	t.Parallel()

	if err := defaultBudgets.validate(); err != nil {
		t.Errorf("defaultBudgets.validate() error = %v, want nil", err)
	}
}

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

// PostgreSQL advisory locks are session-scoped, so probe from one dedicated connection, not a pool.
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
		if _, err := probeConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrations.LockID); err != nil {
			t.Fatalf("release probe lock: %v", err)
		}
		time.Sleep(pollInterval)
	}
	t.Fatal("timed out waiting to observe the advisory lock held by another session")
}

func TestRun_DeadlineBudgets_ContenderSucceedsAfterApproachingLockWait(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	origBudgets := runBudgets
	runBudgets = budgets{
		lockPeriod:  1 * time.Second,
		lockRetries: 6,
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

const maxCheckElapsed = 2 * time.Second

func TestRun_Check_DoesNotBlockOnAdvisoryLock(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	origBudgets := runBudgets
	runBudgets = budgets{
		lockPeriod:  1 * time.Second,
		lockRetries: 6,
		migration:   10 * time.Second,
		slack:       5 * time.Second,
	}
	t.Cleanup(func() { runBudgets = origBudgets })

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("ENV", "dev")
	t.Setenv("PUBLIC_ORIGIN", "https://aboutme.vn")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	setupDB, err := migrations.Open(dsn)
	if err != nil {
		t.Fatalf("open setup database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := setupDB.Close(); closeErr != nil {
			t.Logf("close setup database: %v", closeErr)
		}
	})
	if _, applyErr := migrations.Apply(ctx, setupDB); applyErr != nil {
		t.Fatalf("setup Apply() error: %v", applyErr)
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
