package migrations_test

import (
	"context"
	"testing"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// 00002's down path must delete identity_unlinked rows before it restores the
// older constraints, keep every other audit row, and leave a schema that the up
// path can migrate again.
func TestIdentityUnlinkedAuditMigrationUpDownUp(t *testing.T) {
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

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := provider.UpTo(ctx, 2); err != nil {
		t.Fatalf("UpTo(2) error: %v", err)
	}
	for _, kind := range []string{"account_deleted", "identity_unlinked"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO lifecycle_audit_events (kind) VALUES ($1)`, kind); err != nil {
			t.Fatalf("insert %s at version 2: %v", kind, err)
		}
	}

	if _, err := provider.DownTo(ctx, 1); err != nil {
		t.Fatalf("DownTo(1) with an identity_unlinked row present: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM lifecycle_audit_events WHERE kind = 'identity_unlinked'`); n != 0 {
		t.Fatalf("identity_unlinked rows after down = %d, want 0", n)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM lifecycle_audit_events WHERE kind = 'account_deleted'`); n != 1 {
		t.Fatalf("account_deleted rows after down = %d, want 1", n)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO lifecycle_audit_events (kind) VALUES ('identity_unlinked')`); err == nil {
		t.Fatal("version 1 accepted identity_unlinked, want the restored kind check to reject it")
	}

	if _, err := provider.UpTo(ctx, 2); err != nil {
		t.Fatalf("UpTo(2) after down error: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO lifecycle_audit_events (kind) VALUES ('identity_unlinked')`); err != nil {
		t.Fatalf("insert identity_unlinked after re-up: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO lifecycle_audit_events (kind, media_job_id) VALUES ('identity_unlinked', uuidv7())`); err == nil {
		t.Fatal("re-up accepted identity_unlinked with a media job, want the scope check to reject it")
	}
}
