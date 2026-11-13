package migrations_test

import (
	"context"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/migrations"
)

const statusLockFreeTimeout = 15 * time.Second

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
