package accountapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestOperatorStatusUsesAccountIDAfterRename(t *testing.T) {
	f := newOperatorFixture(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE resumes SET slug=NULL, live=false WHERE user_id=$1`, f.user.ID); err != nil {
		t.Fatal(err)
	}
	present, err := AccountExists(f.ctx, f.pool, f.user.ID)
	if err != nil || !present {
		t.Fatalf("renamed account: %v %v", present, err)
	}
	present, err = AccountExists(f.ctx, f.pool, uuid.New())
	if err != nil || present {
		t.Fatalf("absent account: %v %v", present, err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err := AccountExists(ctx, f.pool, f.user.ID); err == nil {
		t.Fatal("canceled snapshot succeeded")
	}
}

func TestOperatorStatusAfterDeletion(t *testing.T) {
	f := newOperatorFixture(t)
	if _, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], f.user.ID); err != nil {
		t.Fatal(err)
	}
	present, err := AccountExists(f.ctx, f.pool, f.user.ID)
	if err != nil || present {
		t.Fatalf("deleted account: %v %v", present, err)
	}
}

func TestOperatorSnapshotIsReadOnly(t *testing.T) {
	f := newOperatorFixture(t)
	err := ReadOperatorSnapshot(f.ctx, f.pool, func(ctx context.Context, tx pgx.Tx) error {
		for _, setting := range []struct{ query, want string }{
			{"SHOW transaction_isolation", "repeatable read"},
			{"SHOW transaction_read_only", "on"},
			{"SHOW statement_timeout", "30s"},
		} {
			var got string
			if err := tx.QueryRow(ctx, setting.query).Scan(&got); err != nil || got != setting.want {
				t.Fatalf("%s = %q, %v", setting.query, got, err)
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.user.ID)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("snapshot write = %v, want read-only refusal", err)
	}
	f.assertNothingDeleted(t)
}

func TestOperatorSnapshotRetainsExistenceAcrossConcurrentDeletion(t *testing.T) {
	f := newOperatorFixture(t)
	err := ReadOperatorSnapshot(f.ctx, f.pool, func(ctx context.Context, tx pgx.Tx) error {
		read := func() bool {
			var present bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, f.user.ID).Scan(&present); err != nil {
				t.Fatal(err)
			}
			return present
		}
		if !read() {
			t.Fatal("account missing before deletion")
		}
		if _, err := f.service.DeleteAccountBySlug(ctx, f.slugs[0], f.user.ID); err != nil {
			return err
		}
		if !read() {
			t.Fatal("repeatable read changed inside snapshot")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if present, err := AccountExists(f.ctx, f.pool, f.user.ID); err != nil || present {
		t.Fatalf("fresh snapshot = %v, %v", present, err)
	}
}
