//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func TestMigration14ExactHistoryEnforcement(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if err := validateHistoryEnforcement(ctx, db); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close enforcement connection: %v", err)
		}
	})
	if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.ExecContext(context.Background(), `RESET SESSION AUTHORIZATION`); err != nil && !errors.Is(err, sql.ErrConnDone) && !errors.Is(err, driver.ErrBadConn) {
			t.Errorf("reset enforcement identity: %v", err)
		}
	})
	if _, err := conn.ExecContext(ctx, `DISCARD TEMP`); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"insert-without-marker": `INSERT INTO public.goose_db_version(version_id,is_applied) VALUES(15,true)`,
		"update":                `UPDATE public.goose_db_version SET is_applied=false WHERE version_id=14`,
		"delete":                `DELETE FROM public.goose_db_version WHERE version_id=14`,
		"truncate":              `TRUNCATE public.goose_db_version`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := conn.ExecContext(ctx, statement); err == nil {
				t.Fatal("history mutation succeeded")
			}
		})
	}
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE runtime_write_entry_v1(wrong integer)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO public.goose_db_version(version_id,is_applied) VALUES(15,true)`); sqlState(err) != "AM001" {
		t.Fatalf("forged marker SQLSTATE=%s error=%v", sqlState(err), err)
	}
	if _, err := conn.ExecContext(ctx, `DISCARD TEMP`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT public.runtime_enter_migrator()`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT public.runtime_begin_migration_write('migration-100000'); SELECT public.runtime_finish_write()`); err != nil {
		ignoreMigrationCleanupError(tx.Rollback())
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.goose_db_version(version_id,is_applied) VALUES(100000,true)`); err != nil {
		ignoreMigrationCleanupError(tx.Rollback())
		t.Fatalf("canonical six-digit history insert: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT public.runtime_exit_migrator()`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationPureDDLAdvancesOnceAndRollbackLeavesState(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	fs15 := embeddedMigrationMap(t, map[string]string{
		"00015_protected_ddl.sql": "-- +goose Up\n-- +goose StatementBegin\nSELECT public.runtime_begin_migration_write('migration-00015');\nCREATE TABLE public.protected_ddl_probe(id integer);\nSELECT public.runtime_finish_write();\n-- +goose StatementEnd\n\n-- +goose Down\n",
	})
	results, err := applyFS(ctx, db, fs15, LocalAdminMigratorIdentity())
	if err != nil || len(results) != 1 || results[0].Source.Version != 15 {
		t.Fatalf("migration 15 results=%v error=%v", results, err)
	}
	var generation int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	fs16 := embeddedMigrationMap(t, map[string]string{
		"00015_protected_ddl.sql": string(fs15["00015_protected_ddl.sql"].Data),
		"00016_rollback.sql":      "-- +goose Up\n-- +goose StatementBegin\nSELECT public.runtime_begin_migration_write('migration-00016');\nCREATE TABLE public.protected_rollback_probe(id integer);\nSELECT 1/0;\nSELECT public.runtime_finish_write();\n-- +goose StatementEnd\n\n-- +goose Down\n",
	})
	if _, err := applyFS(ctx, db, fs16, LocalAdminMigratorIdentity()); err == nil {
		t.Fatal("failing migration 16 succeeded")
	}
	var after, head int64
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT s.generation,(SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),to_regclass('public.protected_rollback_probe') IS NOT NULL FROM public.runtime_write_state s WHERE singleton`).Scan(&after, &head, &exists); err != nil {
		t.Fatal(err)
	}
	if after != generation || head != 15 || exists {
		t.Fatalf("rollback generation=%d/%d head=%d exists=%t", generation, after, head, exists)
	}
}

func TestMigration14DirectSQLRejectsHostileCatalogBeforeTransfer(t *testing.T) {
	for name, corruption := range map[string]string{
		"same-owner-index-replacement": `DROP INDEX public.auth_email_jobs_claim_idx; CREATE INDEX hostile_equal_count_idx ON public.auth_email_jobs(id)`,
		"extra-trigger":                `CREATE TRIGGER hostile_manifest_trigger BEFORE INSERT ON public.users FOR EACH ROW EXECUTE FUNCTION public.enforce_resume_cap()`,
	} {
		t.Run(name, func(t *testing.T) {
			db := newCompositionTestDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := ProvisionDatabase(ctx, db); err != nil {
				t.Fatal(err)
			}
			through13 := fstest.MapFS{}
			for _, source := range migrationSourcesFromFSForTest(t, 13) {
				through13[source.name] = &fstest.MapFile{Data: source.data}
			}
			if _, err := applyFS(ctx, db, through13, LocalAdminMigratorIdentity()); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, corruption); err != nil {
				t.Fatal(err)
			}
			var generation int64
			if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generation); err != nil {
				t.Fatal(err)
			}
			if err := applyMigration14WithoutManifestPrecheck(ctx, db); sqlState(err) != "AM001" {
				t.Fatalf("migration 14 SQLSTATE=%s error=%v", sqlState(err), err)
			}
			var after int64
			var owner string
			if err := db.QueryRowContext(ctx, `SELECT s.generation,pg_get_userbyid(c.relowner) FROM public.runtime_write_state s CROSS JOIN pg_class c WHERE s.singleton AND c.oid='public.goose_db_version'::regclass`).Scan(&after, &owner); err != nil {
				t.Fatal(err)
			}
			if after != generation || owner != "aboutme_migrator" {
				t.Fatalf("failed SQL mutated generation=%d/%d owner=%q", generation, after, owner)
			}
		})
	}
}

type testMigrationSource struct {
	name string
	data []byte
}

func migrationSourcesFromFSForTest(t *testing.T, through int64) []testMigrationSource {
	t.Helper()
	sources, err := migrationSourcesFromFS(FS)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]testMigrationSource, 0, through)
	for _, source := range sources {
		if source.Version > through {
			break
		}
		data, err := fs.ReadFile(FS, source.Path)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, testMigrationSource{name: source.Path, data: data})
	}
	return result
}

func applyMigration14WithoutManifestPrecheck(ctx context.Context, db *sql.DB) error {
	wrapped, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID))
	if err != nil {
		return err
	}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), wrapped, func(ctx context.Context, conn *sql.Conn, _ int32) error {
		return validateProtectedHistory(ctx, conn, true, true)
	})
	if err != nil {
		return err
	}
	data, err := fs.ReadFile(FS, "00014_migrator_enforcement.sql")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fstest.MapFS{"00014_migrator_enforcement.sql": {Data: data}}, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	_, err = provider.ApplyVersion(ctx, 14, true)
	return err
}
