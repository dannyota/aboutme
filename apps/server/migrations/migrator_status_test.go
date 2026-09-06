//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStatusPre13Inert13AndClosedProtectedAreReadOnly(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 12); err != nil {
		t.Fatal(err)
	}
	statuses, err := Status(ctx, db, LocalAdminMigratorIdentity())
	if err != nil || PendingCount(statuses) != 2 {
		t.Fatalf("pre-13 Status pending=%d error=%v", PendingCount(statuses), err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	statuses, err = Status(ctx, db, LocalAdminMigratorIdentity())
	if err != nil || PendingCount(statuses) != 1 {
		t.Fatalf("inert-13 Status pending=%d error=%v", PendingCount(statuses), err)
	}
	if _, err := AdoptHistoryOwner(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE public.runtime_write_state SET write_gate='closed' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	statuses, err = Status(ctx, db, LocalAdminMigratorIdentity())
	if err != nil || PendingCount(statuses) != 0 {
		t.Fatalf("closed protected Status pending=%d error=%v", PendingCount(statuses), err)
	}
}

func TestStatusCorruptionNeverRepairs(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE public.goose_db_version DISABLE TRIGGER goose_db_version_insert_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(ctx, db, LocalAdminMigratorIdentity()); !errors.Is(err, ErrMigrationHistoryCorrupt) {
		t.Fatalf("Status corruption error=%v", err)
	}
	var enabled string
	if err := db.QueryRowContext(ctx, `SELECT tgenabled FROM pg_trigger WHERE tgrelid='public.goose_db_version'::regclass AND tgname='goose_db_version_insert_guard'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != "D" {
		t.Fatalf("Status repaired trigger to %q", enabled)
	}
}

func TestStatusDirectRejectsAdmin(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Status(ctx, db, DirectMigratorIdentity()); err == nil {
		t.Fatal("direct Status accepted admin session")
	}
}

func TestRollbackStatusConnectionIgnoresCanceledOperationContext(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close status rollback connection: %v", err)
		}
	})
	if _, err := conn.ExecContext(ctx, `BEGIN READ ONLY`); err != nil {
		t.Fatal(err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := rollbackStatusConnection(canceled, conn); err != nil {
		t.Fatalf("detached rollback: %v", err)
	}
	var readOnly string
	if err := conn.QueryRowContext(ctx, `SHOW transaction_read_only`).Scan(&readOnly); err != nil {
		t.Fatal(err)
	}
	if readOnly != "off" {
		t.Fatal("connection remained in transaction after detached rollback")
	}
}
