// Regression test proving Status is genuinely lock-free: goose's exported
// Provider.Status always requests the configured advisory lock internally
// when a SessionLocker is configured (Provider.status calls
// initialize(ctx, true) — verified against pressly/goose/v3@v3.27.3's
// source), so only a provider built with no SessionLocker at all (see
// migrations.go's newLockFreeProvider) skips lock acquisition; a fast
// lock-wait budget on a locked provider still takes the real lock, just
// faster. Status runs through its pinned repeatable-read, read-only
// transaction, so a readiness check built on it (cmd/migrate's `-check`
// flag) can run concurrently with an in-progress deploy instead of
// blocking for the full lock-wait budget and then failing.
package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// statusLockFreeTimeout bounds this test's context: generous relative to
// the deliberately-held lock below, tight enough that a regression to a
// lock-taking Status still fails the test suite in seconds rather than
// hanging for the production lock-wait budget.
const statusLockFreeTimeout = 15 * time.Second

// maxLockFreeElapsed is how long Status is allowed to take while the
// advisory lock is held elsewhere. Generous relative to a real query
// round-trip, far below any plausible lock-wait retry interval (goose's
// own default alone is 5s), so a regression that makes Status retry the
// lock even once would fail this bound.
const maxLockFreeElapsed = 2 * time.Second

func TestStatus_DoesNotBlockOnAdvisoryLock(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), statusLockFreeTimeout)
	defer cancel()

	// Apply the real embedded migrations first so Status has real,
	// non-trivial state to report on, not just an empty database.
	if _, applyErr := migrations.Apply(ctx, db); applyErr != nil {
		t.Fatalf("Apply() error: %v", applyErr)
	}

	holderDB := openTestDB(t, dsn)
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
	var locked bool
	if err = holderConn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, migrations.LockID).Scan(&locked); err != nil {
		t.Fatalf("acquire holder lock: %v", err)
	}
	if !locked {
		t.Fatal("holder failed to acquire the advisory lock (unexpected contention from an unrelated session)")
	}

	start := time.Now()
	statuses, err := migrations.Status(ctx, db)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Status() error while another session holds the advisory lock: %v (after %s)", err, elapsed)
	}
	if len(statuses) == 0 {
		t.Fatal("Status() returned no statuses")
	}
	if elapsed > maxLockFreeElapsed {
		t.Errorf("Status() took %s while the advisory lock was held elsewhere, want < %s — "+
			"it must never contend for migrations.LockID at all, not merely have a short retry budget",
			elapsed, maxLockFreeElapsed)
	}
	t.Logf("Status() returned %d statuses in %s while the lock was held elsewhere", len(statuses), elapsed)
}
