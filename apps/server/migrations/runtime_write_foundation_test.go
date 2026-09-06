package migrations_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRuntimeWriteFoundationCatalogAndState(t *testing.T) {
	db := openTestDB(t, newTestDatabase(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := applyRuntimeWriteFoundation(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	digest := sha256.Sum256([]byte("aboutme.runtime-write-barrier.v1"))
	if got := int64(binary.BigEndian.Uint64(digest[:8])); got != 3727108639518528074 {
		t.Fatalf("barrier key = %d", got)
	}
	var generation, enforcement int
	var gate, kind, operation, historyOwner string
	var lastWrite, lastAccepted, updated time.Time
	if err := db.QueryRowContext(ctx, `SELECT generation, write_gate, last_writer_kind, last_writer_operation_id, migrator_enforcement_version, migration_history_owner,last_write_at,last_accepted_writer_at,updated_at FROM public.runtime_write_state WHERE singleton`).Scan(&generation, &gate, &kind, &operation, &enforcement, &historyOwner, &lastWrite, &lastAccepted, &updated); err != nil {
		t.Fatalf("read foundation state: %v", err)
	}
	if generation != 1 || gate != "open" || kind != "bootstrap" || operation != "migration-00013" || enforcement != 0 || historyOwner == "" {
		t.Fatalf("unexpected seed: %d %s %s %s %d %s", generation, gate, kind, operation, enforcement, historyOwner)
	}
	if !lastWrite.Equal(lastAccepted) || !lastWrite.Equal(updated) {
		t.Fatalf("seed timestamps differ: write=%v accepted=%v updated=%v", lastWrite, lastAccepted, updated)
	}
	rows, err := db.QueryContext(ctx, `SELECT p.proname, r.rolname, p.prosecdef, p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.proname LIKE 'runtime_%write%' OR p.proname LIKE 'runtime_%migrator%' ORDER BY p.proname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck // rows.Err is checked below.
	seen := map[string]bool{}
	for rows.Next() {
		var name, owner string
		var definer bool
		var config string
		if err := rows.Scan(&name, &owner, &definer, &config); err != nil {
			t.Fatal(err)
		}
		if owner != "aboutme_runtime_owner" || !definer || config != "{search_path=pg_catalog}" {
			t.Fatalf("unsafe function %s owner=%s definer=%v config=%v", name, owner, definer, config)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime_enter_write", "runtime_assert_write_entry", "runtime_assert_business_write", "runtime_finish_write", "runtime_assert_write_finished", "runtime_enter_migrator", "runtime_begin_migration_write", "runtime_exit_migrator", "runtime_read_migrator_metadata"} {
		if !seen[name] {
			t.Errorf("missing function %s", name)
		}
	}
	var stopFunctions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname IN ('runtime_close_write_gate','finalize_stop_receipt','begin_wake')`).Scan(&stopFunctions); err != nil || stopFunctions != 0 {
		t.Fatalf("stop authority count=%d err=%v", stopFunctions, err)
	}
	var runtimeOwnerCreate bool
	if err := db.QueryRowContext(ctx, `SELECT has_schema_privilege('aboutme_runtime_owner','public','CREATE')`).Scan(&runtimeOwnerCreate); err != nil {
		t.Fatal(err)
	}
	if runtimeOwnerCreate {
		t.Fatal("runtime owner retained temporary schema CREATE")
	}
}

func TestRuntimeWriteFoundationInstallsAsNonSuperuserMigrator(t *testing.T) {
	dsn := newTestDatabase(t)
	admin := openTestDB(t, dsn)
	var databaseName string
	if err := admin.QueryRowContext(context.Background(), `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	// newTestDatabase emits identifiers containing only this fixed prefix and digits.
	if !strings.HasPrefix(databaseName, "aboutme_migrate_test_") {
		t.Fatalf("unexpected disposable database name %q", databaseName)
	}
	execSQL(t, admin, `GRANT CONNECT,TEMPORARY,CREATE ON DATABASE `+databaseName+` TO aboutme_migrator`)
	execSQL(t, admin, `GRANT TEMPORARY ON DATABASE `+databaseName+` TO aboutme_runtime_owner`)
	execSQL(t, admin, `GRANT USAGE ON SCHEMA public TO aboutme_migrator`)
	execSQL(t, admin, `GRANT CREATE ON SCHEMA public TO aboutme_migrator WITH GRANT OPTION`)

	migratorDB := openTestDB(t, dsn)
	migratorDB.SetMaxOpenConns(1)
	migratorDB.SetMaxIdleConns(1)
	conn, err := migratorDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, conn, `SET SESSION AUTHORIZATION aboutme_migrator`)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := applyRuntimeWriteFoundation(context.Background(), migratorDB); err != nil {
		t.Fatalf("apply migrations as aboutme_migrator: %v", err)
	}

	var owner string
	if err := admin.QueryRowContext(context.Background(), `SELECT relowner::regrole::text FROM pg_class WHERE oid='public.runtime_write_state'::regclass`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != "aboutme_runtime_owner" {
		t.Fatalf("runtime state owner=%q", owner)
	}
	var runtimeOwnerCreate bool
	if err := admin.QueryRowContext(context.Background(), `SELECT has_schema_privilege('aboutme_runtime_owner','public','CREATE')`).Scan(&runtimeOwnerCreate); err != nil {
		t.Fatal(err)
	}
	if runtimeOwnerCreate {
		t.Fatal("runtime owner retained schema CREATE after transfer")
	}
	assertRuntimeWriteGrantMatrix(t, admin)
}

func TestRuntimeWriteApplicationBoundary(t *testing.T) {
	db := openTestDB(t, newTestDatabase(t))
	ctx := context.Background()
	if _, err := applyRuntimeWriteFoundation(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE public.runtime_write_probe(id integer PRIMARY KEY); ALTER TABLE public.runtime_write_probe OWNER TO aboutme_runtime_owner; CREATE TRIGGER runtime_write_probe_assert BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_write_probe FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_business_write('probe'); GRANT SELECT,INSERT,UPDATE,DELETE ON public.runtime_write_probe TO aboutme_app`); err != nil {
		t.Fatalf("create probe: %v", err)
	}
	app := runtimeWriteConn(t, db, "aboutme_app")
	if _, err := app.ExecContext(ctx, `INSERT INTO public.runtime_write_probe VALUES (1)`); sqlState(err) != "AM001" {
		t.Fatalf("direct DML state=%s err=%v", sqlState(err), err)
	}
	tx, err := app.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // Ensures fatal paths release the dedicated connection.
	var entered int64
	if queryErr := tx.QueryRowContext(ctx, `SELECT generation FROM public.runtime_enter_write()`).Scan(&entered); queryErr != nil {
		t.Fatal(queryErr)
	}
	if _, execErr := tx.ExecContext(ctx, `INSERT INTO public.runtime_write_probe VALUES (1),(2); UPDATE public.runtime_write_probe SET id=id`); execErr != nil {
		t.Fatal(execErr)
	}
	if _, execErr := tx.ExecContext(ctx, `SELECT public.runtime_finish_write()`); execErr != nil {
		t.Fatal(execErr)
	}
	if _, execErr := tx.ExecContext(ctx, `SELECT public.runtime_finish_write()`); execErr != nil {
		t.Fatalf("repeat finish: %v", execErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	generation, _, accepted := runtimeWriteState(t, db)
	if generation != 2 || accepted.IsZero() {
		t.Fatalf("generation=%d accepted=%v", generation, accepted)
	}
	clean, err := app.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clean.Rollback() //nolint:errcheck
	if _, cleanErr := clean.ExecContext(ctx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`); cleanErr != nil {
		t.Fatal(cleanErr)
	}
	if cleanErr := clean.Commit(); cleanErr != nil {
		t.Fatal(cleanErr)
	}
	cleanGeneration, _, _ := runtimeWriteState(t, db)
	if cleanGeneration != generation {
		t.Fatalf("clean entry advanced %d to %d", generation, cleanGeneration)
	}
	tx, err = app.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // Ensures fatal paths release the dedicated connection.
	if _, err := tx.ExecContext(ctx, `SELECT public.runtime_enter_write()`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); sqlState(err) != "55000" {
		t.Fatalf("unfinished constraint state=%s err=%v", sqlState(err), err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	generation2, _, _ := runtimeWriteState(t, db)
	if generation2 != generation {
		t.Fatalf("rollback advanced generation %d -> %d", generation, generation2)
	}
}

func TestRuntimeWriteMigratorSessionLifecycle(t *testing.T) {
	db := openTestDB(t, newTestDatabase(t))
	if _, err := applyRuntimeWriteFoundation(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner`); err != nil {
			t.Logf("revoke test CREATE: %v", err)
		}
	})
	conn := runtimeWriteConn(t, db, "aboutme_migrator")
	ctx := context.Background()
	var metadataGate, metadataOwner string
	var metadataGeneration int64
	var metadataEnforcement int
	if err := conn.QueryRowContext(ctx, `SELECT write_gate,generation,migrator_enforcement_version,migration_history_owner FROM public.runtime_read_migrator_metadata()`).Scan(&metadataGate, &metadataGeneration, &metadataEnforcement, &metadataOwner); err != nil {
		t.Fatal(err)
	}
	if metadataGate != "open" || metadataGeneration != 1 || metadataEnforcement != 0 || metadataOwner == "" {
		t.Fatalf("unexpected migrator metadata: %s %d %d %s", metadataGate, metadataGeneration, metadataEnforcement, metadataOwner)
	}
	if _, err := conn.ExecContext(ctx, `SELECT public.runtime_enter_migrator(); SELECT public.runtime_enter_migrator()`); err != nil {
		t.Fatal(err)
	}
	var locks int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ShareLock'`).Scan(&locks); err != nil {
		t.Fatal(err)
	}
	if locks != 1 {
		t.Fatalf("session lock count=%d", locks)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // Ensures fatal paths release the dedicated connection.
	if _, execErr := tx.ExecContext(ctx, `SELECT public.runtime_begin_migration_write('migration-00014'); SET LOCAL ROLE aboutme_runtime_owner; CREATE TABLE public.runtime_migration_probe(id int); RESET ROLE; SELECT public.runtime_finish_write()`); execErr != nil {
		t.Fatal(execErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	if _, exitErr := conn.ExecContext(ctx, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
		t.Fatal(exitErr)
	}
	if _, reuseErr := conn.ExecContext(ctx, `SELECT public.runtime_enter_migrator(); SELECT public.runtime_exit_migrator()`); reuseErr != nil {
		t.Fatalf("reuse empty preserved marker: %v", reuseErr)
	}
	if queryErr := conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1`).Scan(&locks); queryErr != nil {
		t.Fatal(queryErr)
	}
	if locks != 0 {
		t.Fatalf("lock leaked: %d", locks)
	}
	probe, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close() //nolint:errcheck
	var exclusive bool
	if err := probe.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, int64(3727108639518528074)).Scan(&exclusive); err != nil {
		t.Fatal(err)
	}
	if !exclusive {
		t.Fatal("exclusive peer did not acquire after one migrator exit")
	}
	if _, err := probe.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, int64(3727108639518528074)); err != nil {
		t.Fatal(err)
	}
	app := runtimeWriteConn(t, db, "aboutme_app")
	if _, err := app.ExecContext(ctx, `SELECT public.runtime_enter_migrator()`); err == nil {
		t.Fatal("app entered migrator")
	}
}

func TestRuntimeWriteMarkerLookalikesAreContamination(t *testing.T) {
	for _, collision := range []string{"view", "type", "domain", "enum", "index-name", "wrong-table"} {
		t.Run(collision, func(t *testing.T) {
			db := runtimeWriteDB(t)
			app := runtimeWriteConn(t, db, "aboutme_app")
			execSQL(t, app, markerCollisionSQL(collision))
			_, err := app.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
			expectSQLState(t, "AM001", err)
		})
	}
}

func TestRuntimeWriteMarkerExactShapeAndACL(t *testing.T) {
	db := runtimeWriteDB(t)
	app := runtimeWriteConn(t, db, "aboutme_app")
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	execSQL(t, tx, `SELECT public.runtime_enter_write()`)

	var columns, constraints, triggers, indexes, foreignACL int
	err = tx.QueryRowContext(context.Background(), `
SELECT
 (SELECT count(*) FROM pg_attribute WHERE attrelid='pg_temp.runtime_write_entry_v1'::regclass AND attnum>0 AND NOT attisdropped),
 (SELECT count(*) FROM pg_constraint WHERE conrelid='pg_temp.runtime_write_entry_v1'::regclass AND contype IN ('c','p')),
 (SELECT count(*) FROM pg_trigger WHERE tgrelid='pg_temp.runtime_write_entry_v1'::regclass AND NOT tgisinternal),
 (SELECT count(*) FROM pg_index WHERE indrelid='pg_temp.runtime_write_entry_v1'::regclass),
 (SELECT count(*) FROM pg_class c CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(c.relacl,pg_catalog.acldefault('r',c.relowner))) a WHERE c.oid='pg_temp.runtime_write_entry_v1'::regclass AND a.grantee<>c.relowner)`).Scan(&columns, &constraints, &triggers, &indexes, &foreignACL)
	if err != nil {
		t.Fatal(err)
	}
	if columns != 9 || constraints != 4 || triggers != 1 || indexes != 1 || foreignACL != 0 {
		t.Fatalf("marker catalog columns=%d constraints=%d triggers=%d indexes=%d foreign_acl=%d", columns, constraints, triggers, indexes, foreignACL)
	}

	for name, statement := range map[string]string{
		"select":          `SELECT * FROM pg_temp.runtime_write_entry_v1`,
		"insert":          `INSERT INTO pg_temp.runtime_write_entry_v1(transaction_id,backend_pid,session_role_oid,writer_kind,entry_generation) VALUES(pg_current_xact_id(),pg_backend_pid(),(session_user::regrole)::oid,'app',1)`,
		"update":          `UPDATE pg_temp.runtime_write_entry_v1 SET dirty=true`,
		"delete":          `DELETE FROM pg_temp.runtime_write_entry_v1`,
		"truncate":        `TRUNCATE pg_temp.runtime_write_entry_v1`,
		"alter":           `ALTER TABLE pg_temp.runtime_write_entry_v1 ADD COLUMN poison integer`,
		"drop":            `DROP TABLE pg_temp.runtime_write_entry_v1`,
		"disable-trigger": `ALTER TABLE pg_temp.runtime_write_entry_v1 DISABLE TRIGGER runtime_write_entry_finished_assert`,
		"grant":           `GRANT SELECT ON pg_temp.runtime_write_entry_v1 TO aboutme_app`,
	} {
		t.Run(name, func(t *testing.T) {
			sp := "sp_" + strings.ReplaceAll(name, "-", "_")
			execSQL(t, tx, `SAVEPOINT `+sp)
			_, statementErr := tx.ExecContext(context.Background(), statement)
			expectSQLState(t, "42501", statementErr)
			execSQL(t, tx, `ROLLBACK TO SAVEPOINT `+sp)
		})
	}
	execSQL(t, tx, `SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
}

func TestRuntimeWriteRejectsMalformedOwnerMarkers(t *testing.T) {
	cases := map[string]string{
		"wrong-dirty-default":    `ALTER TABLE pg_temp.runtime_write_entry_v1 ALTER COLUMN dirty SET DEFAULT true`,
		"wrong-accepted-default": `ALTER TABLE pg_temp.runtime_write_entry_v1 ALTER COLUMN accepted_write SET DEFAULT true`,
		"wrong-finished-default": `ALTER TABLE pg_temp.runtime_write_entry_v1 ALTER COLUMN finished SET DEFAULT true`,
		"wrong-writer-check":     `ALTER TABLE pg_temp.runtime_write_entry_v1 DROP CONSTRAINT runtime_write_entry_v1_writer_kind_check; ALTER TABLE pg_temp.runtime_write_entry_v1 ADD CONSTRAINT runtime_write_entry_v1_writer_kind_check CHECK(writer_kind IN ('app','maintenance','lifecycle','proof','migrator','bootstrap'))`,
		"wrong-generation-check": `ALTER TABLE pg_temp.runtime_write_entry_v1 DROP CONSTRAINT runtime_write_entry_v1_entry_generation_check; ALTER TABLE pg_temp.runtime_write_entry_v1 ADD CONSTRAINT runtime_write_entry_v1_entry_generation_check CHECK(entry_generation >= 1)`,
		"wrong-operation-check":  `ALTER TABLE pg_temp.runtime_write_entry_v1 DROP CONSTRAINT runtime_write_entry_v1_operation_id_check; ALTER TABLE pg_temp.runtime_write_entry_v1 ADD CONSTRAINT runtime_write_entry_v1_operation_id_check CHECK(operation_id IS NULL OR octet_length(operation_id) BETWEEN 1 AND 127)`,
		"column-acl":             `GRANT SELECT(dirty) ON pg_temp.runtime_write_entry_v1 TO aboutme_app`,
		"table-acl":              `GRANT SELECT ON pg_temp.runtime_write_entry_v1 TO aboutme_app`,
		"extra-index":            `CREATE INDEX poison_runtime_marker ON pg_temp.runtime_write_entry_v1(backend_pid)`,
		"index-shape":            `ALTER TABLE pg_temp.runtime_write_entry_v1 DROP CONSTRAINT runtime_write_entry_v1_pkey; CREATE UNIQUE INDEX runtime_write_entry_v1_pkey ON pg_temp.runtime_write_entry_v1(transaction_id) INCLUDE (backend_pid); ALTER TABLE pg_temp.runtime_write_entry_v1 ADD CONSTRAINT runtime_write_entry_v1_pkey PRIMARY KEY USING INDEX runtime_write_entry_v1_pkey`,
		"trigger-mode":           `ALTER TABLE pg_temp.runtime_write_entry_v1 DISABLE TRIGGER runtime_write_entry_finished_assert`,
		"trigger-function":       `DROP TRIGGER runtime_write_entry_finished_assert ON pg_temp.runtime_write_entry_v1; CREATE FUNCTION pg_temp.poison_marker_trigger() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NULL; END'; CREATE CONSTRAINT TRIGGER runtime_write_entry_finished_assert AFTER INSERT ON pg_temp.runtime_write_entry_v1 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pg_temp.poison_marker_trigger()`,
	}
	for name, ddl := range cases {
		t.Run(name, func(t *testing.T) {
			db := runtimeWriteDB(t)
			conn := runtimeWriteConn(t, db, "aboutme_migrator")
			execSQL(t, conn, `SELECT public.runtime_enter_migrator()`)
			baseline, err := conn.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			execSQL(t, baseline, `SELECT public.runtime_begin_migration_write('baseline-shape'); SELECT public.runtime_finish_write()`)
			if commitErr := baseline.Commit(); commitErr != nil {
				t.Fatal(commitErr)
			}
			tx, err := conn.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback() //nolint:errcheck
			execSQL(t, tx, `SET LOCAL ROLE aboutme_runtime_owner`)
			execSQL(t, tx, ddl)
			execSQL(t, tx, `RESET ROLE`)
			_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_begin_migration_write('migration-shape')`)
			expectSQLState(t, "AM001", err)
		})
	}
}

func TestRuntimeWriteEntryFailuresDoNotChangeRows(t *testing.T) {
	tests := map[string]func(*testing.T, *sql.DB, *sql.Conn) error{
		"direct": func(_ *testing.T, _ *sql.DB, app *sql.Conn) error {
			_, err := app.ExecContext(context.Background(), `INSERT INTO public.runtime_write_probe VALUES(1,0)`)
			return err
		},
		"lock-only": func(t *testing.T, _ *sql.DB, app *sql.Conn) error {
			execSQL(t, app, `SELECT pg_advisory_lock_shared($1)`, runtimeBarrierKey)
			t.Cleanup(func() {
				if _, unlockErr := app.ExecContext(context.Background(), `SELECT pg_advisory_unlock_shared($1)`, runtimeBarrierKey); unlockErr != nil {
					t.Logf("unlock shared barrier: %v", unlockErr)
				}
			})
			_, err := app.ExecContext(context.Background(), `INSERT INTO public.runtime_write_probe VALUES(1,0)`)
			return err
		},
		"closed-gate": func(t *testing.T, db *sql.DB, app *sql.Conn) error {
			execSQL(t, db, `UPDATE public.runtime_write_state SET write_gate='closed'`)
			_, err := app.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			db := runtimeWriteDB(t)
			runtimeWriteProbe(t, db)
			app := runtimeWriteConn(t, db, "aboutme_app")
			err := run(t, db, app)
			if name == "closed-gate" {
				expectSQLState(t, "55000", err)
			} else {
				expectSQLState(t, "AM001", err)
			}
			if count := runtimeProbeCount(t, db); count != 0 {
				t.Fatalf("failed write changed %d rows", count)
			}
		})
	}
}

func TestRuntimeWriteSavepointsConstraintsAndReuse(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	app := runtimeWriteConn(t, db, "aboutme_app")
	initial, _, _ := runtimeWriteState(t, db)

	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_enter_write(); SAVEPOINT dirty; INSERT INTO public.runtime_write_probe VALUES(1,0); ROLLBACK TO SAVEPOINT dirty; SELECT public.runtime_finish_write(); SET CONSTRAINTS ALL IMMEDIATE; SAVEPOINT reenter`)
	_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
	expectSQLState(t, "AM001", err)
	execSQL(t, tx, `ROLLBACK TO SAVEPOINT reenter`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	if generation, _, _ := runtimeWriteState(t, db); generation != initial {
		t.Fatalf("rolled-back dirty savepoint advanced generation to %d", generation)
	}

	tx, err = app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SET CONSTRAINTS ALL IMMEDIATE`)
	_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
	expectSQLState(t, "55000", err)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}

	tx, err = app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); INSERT INTO public.runtime_write_probe VALUES(2,0); SAVEPOINT finished; SELECT public.runtime_finish_write(); ROLLBACK TO SAVEPOINT finished`)
	_, err = tx.ExecContext(context.Background(), `SET CONSTRAINTS ALL IMMEDIATE`)
	expectSQLState(t, "55000", err)
	execSQL(t, tx, `ROLLBACK TO SAVEPOINT finished; SELECT public.runtime_finish_write(); SET CONSTRAINTS ALL IMMEDIATE`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	if generation, _, _ := runtimeWriteState(t, db); generation != initial+1 {
		t.Fatalf("finish savepoint recovery generation=%d, want %d", generation, initial+1)
	}

	for i := 0; i < 2; i++ {
		tx, err = app.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`)
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeWriteConcurrentFinishesDoNotLoseIncrements(t *testing.T) {
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	if _, err := applyRuntimeWriteFoundation(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	runtimeWriteProbe(t, db)
	initial, _, _ := runtimeWriteState(t, db)

	const writers = 8
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker, err := sql.Open("pgx", dsn)
			if err != nil {
				errs <- err
				return
			}
			defer worker.Close() //nolint:errcheck
			ctx := context.Background()
			conn, err := worker.Conn(ctx)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close() //nolint:errcheck
			if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_app`); err != nil {
				errs <- err
				return
			}
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				errs <- err
				return
			}
			defer tx.Rollback() //nolint:errcheck
			if _, err = tx.ExecContext(ctx, `SELECT public.runtime_enter_write()`); err != nil {
				errs <- err
				return
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO public.runtime_write_probe VALUES($1,0)`, id+1); err != nil {
				errs <- err
				return
			}
			<-start
			if _, err = tx.ExecContext(ctx, `SELECT public.runtime_finish_write()`); err == nil {
				err = tx.Commit()
			}
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if generation, _, _ := runtimeWriteState(t, db); generation != initial+writers {
		t.Fatalf("generation=%d, want %d", generation, initial+writers)
	}
}

func TestRuntimeWriteRepeatedEntryReturnsBoundGeneration(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	first := runtimeWriteConn(t, db, "aboutme_app")
	second := runtimeWriteConn(t, db, "aboutme_app")
	firstTx, err := first.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer firstTx.Rollback() //nolint:errcheck
	var initialGeneration int64
	if queryErr := firstTx.QueryRowContext(context.Background(), `SELECT generation FROM public.runtime_enter_write()`).Scan(&initialGeneration); queryErr != nil {
		t.Fatal(queryErr)
	}
	secondTx, err := second.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, secondTx, `SELECT public.runtime_enter_write(); INSERT INTO public.runtime_write_probe VALUES(50,0); SELECT public.runtime_finish_write()`)
	if commitErr := secondTx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	var repeatedGeneration int64
	if queryErr := firstTx.QueryRowContext(context.Background(), `SELECT generation FROM public.runtime_enter_write()`).Scan(&repeatedGeneration); queryErr != nil {
		t.Fatal(queryErr)
	}
	if repeatedGeneration != initialGeneration {
		t.Fatalf("repeated entry generation=%d, want bound %d", repeatedGeneration, initialGeneration)
	}
	execSQL(t, firstTx, `SELECT public.runtime_finish_write()`)
	if commitErr := firstTx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
}

func TestRuntimeWriteRepeatedEntryStillValidatesGate(t *testing.T) {
	for _, mutation := range []string{`UPDATE public.runtime_write_state SET write_gate='closed'`, `DELETE FROM public.runtime_write_state`} {
		db := runtimeWriteDB(t)
		app := runtimeWriteConn(t, db, "aboutme_app")
		tx, err := app.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		execSQL(t, tx, `SELECT public.runtime_enter_write()`)
		execSQL(t, db, mutation)
		_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
		expectSQLState(t, "55000", err)
	}
}

func TestRuntimeWriteExclusiveBarrierOrdering(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	holder, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close() //nolint:errcheck
	execSQL(t, holder, `SELECT pg_advisory_lock($1)`, runtimeBarrierKey)
	app := runtimeWriteConn(t, db, "aboutme_app")
	var appPID int
	if queryErr := app.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&appPID); queryErr != nil {
		t.Fatal(queryErr)
	}
	entered := make(chan error, 1)
	entryCtx, cancelEntry := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelEntry()
	go func() {
		_, err := app.ExecContext(entryCtx, `BEGIN; SELECT public.runtime_enter_write(); SELECT * FROM public.runtime_write_probe FOR UPDATE; SELECT public.runtime_finish_write(); COMMIT`)
		entered <- err
	}()
	waitErr := waitForRuntimeCondition(db, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ShareLock' AND NOT granted)`, appPID)
	var rowLocks int
	var rowLockErr error
	if waitErr == nil {
		rowLockErr = db.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_locks WHERE pid=$1 AND locktype IN ('tuple','relation') AND relation='public.runtime_write_probe'::regclass AND granted`, appPID).Scan(&rowLocks)
	}
	_, unlockErr := holder.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, runtimeBarrierKey)
	entryErr := <-entered
	if waitErr != nil {
		t.Fatalf("observe writer waiting on barrier: %v; entry result: %v", waitErr, entryErr)
	}
	if rowLockErr != nil {
		t.Fatal(rowLockErr)
	}
	if rowLocks != 0 {
		t.Fatalf("writer acquired %d probe locks before barrier entry", rowLocks)
	}
	if unlockErr != nil {
		t.Fatal(unlockErr)
	}
	if entryErr != nil {
		t.Fatal(entryErr)
	}
}

func TestRuntimeWriteEnteredWriterFinishesBeforeExclusiveAcquisition(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	app := runtimeWriteConn(t, db, "aboutme_app")
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // Release the writer before connection cleanup on fatal paths.
	execSQL(t, tx, `SELECT public.runtime_enter_write(); INSERT INTO public.runtime_write_probe VALUES(60,0); SELECT * FROM public.runtime_write_probe WHERE id=60 FOR UPDATE`)

	exclusive, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer exclusive.Close() //nolint:errcheck
	var exclusivePID int
	if queryErr := exclusive.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&exclusivePID); queryErr != nil {
		t.Fatal(queryErr)
	}
	acquired := make(chan error, 1)
	lockCtx, cancelLock := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLock()
	go func() {
		_, lockErr := exclusive.ExecContext(lockCtx, `SELECT pg_advisory_lock($1)`, runtimeBarrierKey)
		acquired <- lockErr
	}()
	waitErr := waitForRuntimeCondition(db, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ExclusiveLock' AND NOT granted)`, exclusivePID)
	if waitErr != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			t.Logf("rollback entered writer: %v", rollbackErr)
		}
		lockErr := <-acquired
		if lockErr == nil {
			if _, unlockErr := exclusive.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, runtimeBarrierKey); unlockErr != nil {
				t.Logf("unlock exclusive barrier after observation failure: %v", unlockErr)
			}
		}
		t.Fatalf("observe exclusive waiter: %v; lock result: %v", waitErr, lockErr)
	}
	_, finishErr := tx.ExecContext(context.Background(), `SELECT public.runtime_finish_write()`)
	var commitErr error
	if finishErr == nil {
		commitErr = tx.Commit()
	} else {
		commitErr = tx.Rollback()
	}
	lockErr := <-acquired
	var unlockErr error
	if lockErr == nil {
		_, unlockErr = exclusive.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, runtimeBarrierKey)
	}
	if finishErr != nil {
		t.Fatal(finishErr)
	}
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	if unlockErr != nil {
		t.Fatal(unlockErr)
	}
}

func TestRuntimeWriteMigratorFailureAndTransactionMatrix(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	execSQL(t, db, `GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner`)
	t.Cleanup(func() {
		if _, revokeErr := db.ExecContext(context.Background(), `REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner`); revokeErr != nil {
			t.Logf("revoke test CREATE: %v", revokeErr)
		}
	})
	migrator := runtimeWriteConn(t, db, "aboutme_migrator")

	_, err := migrator.ExecContext(context.Background(), `SELECT public.runtime_begin_migration_write('missing')`)
	expectSQLState(t, "AM001", err)
	execSQL(t, migrator, `SELECT public.runtime_enter_migrator()`)
	execSQL(t, migrator, `SELECT pg_advisory_unlock_shared($1)`, runtimeBarrierKey)
	_, err = migrator.ExecContext(context.Background(), `SELECT public.runtime_begin_migration_write('lost')`)
	expectSQLState(t, "AM001", err)
	_, err = migrator.ExecContext(context.Background(), `SELECT public.runtime_exit_migrator()`)
	expectSQLState(t, "AM001", err)

	// A contaminated backend is closed; use a fresh backend for migration cases.
	migrator = runtimeWriteConn(t, db, "aboutme_migrator")
	execSQL(t, migrator, `SELECT public.runtime_enter_migrator()`)
	initial, _, _ := runtimeWriteState(t, db)
	active, activeErr := migrator.BeginTx(context.Background(), nil)
	if activeErr != nil {
		t.Fatal(activeErr)
	}
	execSQL(t, active, `SELECT public.runtime_begin_migration_write('migration-active-exit')`)
	_, activeErr = active.ExecContext(context.Background(), `SELECT public.runtime_exit_migrator()`)
	expectSQLState(t, "AM001", activeErr)
	if rollbackErr := active.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	for i, body := range []string{
		`SET LOCAL ROLE aboutme_runtime_owner; CREATE TABLE public.runtime_pure_ddl(id integer); RESET ROLE`,
		`SET LOCAL ROLE aboutme_runtime_owner; CREATE TABLE public.runtime_ddl_dml(id integer); RESET ROLE; INSERT INTO public.runtime_write_probe VALUES(20,0)`,
	} {
		tx, beginErr := migrator.BeginTx(context.Background(), nil)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		execSQL(t, tx, fmt.Sprintf(`SELECT public.runtime_begin_migration_write('migration-%d')`, i+20))
		execSQL(t, tx, body)
		execSQL(t, tx, `SELECT public.runtime_finish_write()`)
		if commitErr := tx.Commit(); commitErr != nil {
			t.Fatal(commitErr)
		}
	}
	tx, err := migrator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_begin_migration_write('migration-rollback'); SET LOCAL ROLE aboutme_runtime_owner; CREATE TABLE public.runtime_rollback_ddl(id integer); RESET ROLE`)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	if generation, _, _ := runtimeWriteState(t, db); generation != initial+2 {
		t.Fatalf("migrator generation=%d, want %d", generation, initial+2)
	}
	execSQL(t, migrator, `SELECT public.runtime_exit_migrator()`)
}

func TestRuntimeWriteGrantAndRoleMatrix(t *testing.T) {
	db := runtimeWriteDB(t)
	assertRuntimeWriteGrantMatrix(t, db)
}

func assertRuntimeWriteGrantMatrix(t *testing.T, db *sql.DB) {
	t.Helper()
	roles := []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_migrator", "aboutme_restore_verify"}
	functions := []string{
		"runtime_enter_write()", "runtime_finish_write()", "runtime_enter_migrator()",
		"runtime_begin_migration_write(text)", "runtime_exit_migrator()", "runtime_read_migrator_metadata()",
		"runtime_assert_write_entry()", "runtime_assert_business_write()",
		"runtime_assert_write_finished()", "runtime_validate_write_marker()",
		"runtime_create_write_marker()", "runtime_require_write_entry()",
		"runtime_validate_migrator_marker()",
	}
	for _, role := range roles {
		for _, function := range functions {
			want := false
			switch function {
			case "runtime_enter_write()":
				want = role == "aboutme_app" || role == "aboutme_maintenance" || role == "aboutme_lifecycle_command" || role == "aboutme_fencing_proof"
			case "runtime_finish_write()":
				want = role != "aboutme_restore_verify"
			case "runtime_enter_migrator()", "runtime_begin_migration_write(text)", "runtime_exit_migrator()":
				want = role == "aboutme_migrator"
			case "runtime_read_migrator_metadata()":
				want = role == "aboutme_migrator"
			}
			var got bool
			if err := db.QueryRowContext(context.Background(), `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, "public."+function).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s execute %s=%v, want %v", role, function, got, want)
			}
		}
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var got bool
			if err := db.QueryRowContext(context.Background(), `SELECT has_table_privilege($1,'public.runtime_write_state',$2)`, role, privilege).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got {
				t.Errorf("%s unexpectedly has %s on runtime_write_state", role, privilege)
			}
		}
	}
}

func TestRuntimeWriteWriterKindsAndAcceptedTail(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	initialGeneration, initialWrite, initialAccepted := runtimeWriteState(t, db)

	maintenance := runtimeWriteConn(t, db, "aboutme_maintenance")
	tx, err := maintenance.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var kind string
	if queryErr := tx.QueryRowContext(context.Background(), `SELECT writer_kind FROM public.runtime_enter_write()`).Scan(&kind); queryErr != nil {
		t.Fatal(queryErr)
	}
	if kind != "maintenance" {
		t.Fatalf("maintenance kind=%q", kind)
	}
	execSQL(t, tx, `INSERT INTO public.runtime_write_probe VALUES(30,0); SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	generation, lastWrite, accepted := runtimeWriteState(t, db)
	if generation != initialGeneration+1 || !lastWrite.After(initialWrite) || !accepted.Equal(initialAccepted) {
		t.Fatalf("maintenance state generation=%d write=%v accepted=%v", generation, lastWrite, accepted)
	}

	app := runtimeWriteConn(t, db, "aboutme_app")
	tx, err = app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); UPDATE public.runtime_write_probe SET value=value+1; SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	generation, lastWrite, accepted = runtimeWriteState(t, db)
	if generation != initialGeneration+2 || !accepted.After(initialAccepted) || !accepted.Equal(lastWrite) {
		t.Fatalf("app state generation=%d write=%v accepted=%v", generation, lastWrite, accepted)
	}
}

func TestRuntimeWriteTimestampHighWaterSurvivesBackwardClock(t *testing.T) {
	db := runtimeWriteDB(t)
	runtimeWriteProbe(t, db)
	future := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond)
	execSQL(t, db, `UPDATE public.runtime_write_state SET last_write_at=$1,last_accepted_writer_at=$1,updated_at=$1`, future)
	app := runtimeWriteConn(t, db, "aboutme_app")
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); INSERT INTO public.runtime_write_probe VALUES(40,0); SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	_, lastWrite, accepted := runtimeWriteState(t, db)
	var updated time.Time
	if err := db.QueryRowContext(context.Background(), `SELECT updated_at FROM public.runtime_write_state WHERE singleton`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if !lastWrite.Equal(future) || !accepted.Equal(future) || !updated.Equal(future) {
		t.Fatalf("timestamp high water regressed: write=%v accepted=%v updated=%v want=%v", lastWrite, accepted, updated, future)
	}
}

func TestRuntimeWriteMigratorMarkerContaminationAndEntryUnlock(t *testing.T) {
	for _, ddl := range []string{
		`CREATE TEMP VIEW pg_temp.runtime_migrator_session_v1 AS SELECT true AS singleton`,
		`CREATE TEMP TABLE pg_temp.runtime_migrator_session_v1(singleton boolean PRIMARY KEY CHECK(singleton),backend_pid integer NOT NULL,session_role_oid oid NOT NULL,entry_generation bigint NOT NULL CHECK(entry_generation>0),entered_at timestamptz NOT NULL); CREATE INDEX poison_migrator_marker ON pg_temp.runtime_migrator_session_v1(backend_pid)`,
	} {
		db := runtimeWriteDB(t)
		conn := runtimeWriteConn(t, db, "aboutme_migrator")
		execSQL(t, conn, `SET ROLE aboutme_runtime_owner`)
		execSQL(t, conn, ddl)
		execSQL(t, conn, `RESET ROLE`)
		_, err := conn.ExecContext(context.Background(), `SELECT public.runtime_enter_migrator()`)
		expectSQLState(t, "AM001", err)
	}

	db := runtimeWriteDB(t)
	execSQL(t, db, `UPDATE public.runtime_write_state SET write_gate='closed'`)
	migrator := runtimeWriteConn(t, db, "aboutme_migrator")
	_, err := migrator.ExecContext(context.Background(), `SELECT public.runtime_enter_migrator()`)
	expectSQLState(t, "55000", err)
	peer, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close() //nolint:errcheck
	var acquired bool
	if err := peer.QueryRowContext(context.Background(), `SELECT pg_try_advisory_lock($1)`, runtimeBarrierKey).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("failed migrator entry leaked its session lock")
	}
	execSQL(t, peer, `SELECT pg_advisory_unlock($1)`, runtimeBarrierKey)
}

func TestRuntimeWriteRejectsMalformedMigratorMarkers(t *testing.T) {
	cases := map[string]string{
		"unexpected-default":     `ALTER TABLE pg_temp.runtime_migrator_session_v1 ALTER COLUMN backend_pid SET DEFAULT 1`,
		"wrong-singleton-check":  `ALTER TABLE pg_temp.runtime_migrator_session_v1 DROP CONSTRAINT runtime_migrator_session_v1_singleton_check; ALTER TABLE pg_temp.runtime_migrator_session_v1 ADD CONSTRAINT runtime_migrator_session_v1_singleton_check CHECK(singleton IS TRUE)`,
		"wrong-generation-check": `ALTER TABLE pg_temp.runtime_migrator_session_v1 DROP CONSTRAINT runtime_migrator_session_v1_entry_generation_check; ALTER TABLE pg_temp.runtime_migrator_session_v1 ADD CONSTRAINT runtime_migrator_session_v1_entry_generation_check CHECK(entry_generation >= 1)`,
		"column-acl":             `GRANT SELECT(backend_pid) ON pg_temp.runtime_migrator_session_v1 TO aboutme_app`,
		"table-acl":              `GRANT SELECT ON pg_temp.runtime_migrator_session_v1 TO aboutme_app`,
		"index-shape":            `ALTER TABLE pg_temp.runtime_migrator_session_v1 DROP CONSTRAINT runtime_migrator_session_v1_pkey; CREATE UNIQUE INDEX runtime_migrator_session_v1_pkey ON pg_temp.runtime_migrator_session_v1(singleton) INCLUDE (backend_pid); ALTER TABLE pg_temp.runtime_migrator_session_v1 ADD CONSTRAINT runtime_migrator_session_v1_pkey PRIMARY KEY USING INDEX runtime_migrator_session_v1_pkey`,
		"unexpected-trigger":     `CREATE FUNCTION pg_temp.poison_migrator_trigger() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END'; CREATE TRIGGER poison_migrator_trigger BEFORE UPDATE ON pg_temp.runtime_migrator_session_v1 FOR EACH ROW EXECUTE FUNCTION pg_temp.poison_migrator_trigger()`,
	}
	for name, ddl := range cases {
		t.Run(name, func(t *testing.T) {
			db := runtimeWriteDB(t)
			conn := runtimeWriteConn(t, db, "aboutme_migrator")
			execSQL(t, conn, `SELECT public.runtime_enter_migrator()`)
			execSQL(t, conn, `SET ROLE aboutme_runtime_owner; `+ddl+`; RESET ROLE`)
			_, err := conn.ExecContext(context.Background(), `SELECT public.runtime_enter_migrator()`)
			expectSQLState(t, "AM001", err)
		})
	}
}

func TestRuntimeWriteMigratorReservedCatalogCollisions(t *testing.T) {
	for name, ddl := range map[string]string{
		"domain":     `CREATE DOMAIN pg_temp.runtime_migrator_session_v1 AS integer`,
		"enum":       `CREATE TYPE pg_temp.runtime_migrator_session_v1 AS ENUM ('poison')`,
		"index-name": `CREATE TEMP TABLE pg_temp.collision_source(value integer); CREATE INDEX runtime_migrator_session_v1_pkey ON pg_temp.collision_source(value)`,
	} {
		t.Run(name, func(t *testing.T) {
			db := runtimeWriteDB(t)
			conn := runtimeWriteConn(t, db, "aboutme_migrator")
			execSQL(t, conn, `SET ROLE aboutme_runtime_owner; `+ddl+`; RESET ROLE`)
			_, err := conn.ExecContext(context.Background(), `SELECT public.runtime_enter_migrator()`)
			expectSQLState(t, "AM001", err)
		})
	}
}

func TestRuntimeWriteCanceledMigratorEntryReleasesSessionBarrier(t *testing.T) {
	db := runtimeWriteDB(t)
	blocker, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback() //nolint:errcheck
	execSQL(t, blocker, `LOCK TABLE public.runtime_write_state IN ACCESS EXCLUSIVE MODE`)

	migrator := runtimeWriteConn(t, db, "aboutme_migrator")
	var migratorPID int
	if queryErr := migrator.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&migratorPID); queryErr != nil {
		t.Fatal(queryErr)
	}
	entryResult := make(chan error, 1)
	go func() {
		_, entryErr := migrator.ExecContext(context.Background(), `SELECT public.runtime_enter_migrator()`)
		entryResult <- entryErr
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var blockedAfterBarrier bool
		if queryErr := db.QueryRowContext(context.Background(), `
SELECT
 EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ShareLock' AND granted)
 AND EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='public.runtime_write_state'::regclass AND NOT granted)`, migratorPID).Scan(&blockedAfterBarrier); queryErr != nil {
			t.Fatal(queryErr)
		}
		if blockedAfterBarrier {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("migrator did not block on state after acquiring session barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var canceled bool
	if queryErr := db.QueryRowContext(context.Background(), `SELECT pg_cancel_backend($1)`, migratorPID).Scan(&canceled); queryErr != nil {
		t.Fatal(queryErr)
	}
	if !canceled {
		t.Fatal("pg_cancel_backend returned false")
	}
	select {
	case entryErr := <-entryResult:
		expectSQLState(t, "57014", entryErr)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled migrator entry did not return")
	}
	if rollbackErr := blocker.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	peer, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close() //nolint:errcheck
	var acquired bool
	if err := peer.QueryRowContext(context.Background(), `SELECT pg_try_advisory_lock($1)`, runtimeBarrierKey).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("canceled migrator entry retained session barrier")
	}
	execSQL(t, peer, `SELECT pg_advisory_unlock($1)`, runtimeBarrierKey)
}

func TestRuntimeWriteMarkerCommitRollbackAndMigratorExitRollback(t *testing.T) {
	db := runtimeWriteDB(t)
	app := runtimeWriteConn(t, db, "aboutme_app")
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	// ON COMMIT DELETE ROWS is proved by successful reuse of the same backend.
	tx, err = app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	tx, err = app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}

	migrator := runtimeWriteConn(t, db, "aboutme_migrator")
	execSQL(t, migrator, `SELECT public.runtime_enter_migrator()`)
	tx, err = migrator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, tx, `SELECT public.runtime_exit_migrator()`)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	// The PRESERVE marker returns on rollback while unlock remains effective.
	_, err = migrator.ExecContext(context.Background(), `SELECT public.runtime_begin_migration_write('after-exit-rollback')`)
	expectSQLState(t, "AM001", err)
}

func TestRuntimeWriteMissingStateAndWrongMarkerIdentity(t *testing.T) {
	t.Run("missing-state", func(t *testing.T) {
		db := runtimeWriteDB(t)
		execSQL(t, db, `DELETE FROM public.runtime_write_state`)
		app := runtimeWriteConn(t, db, "aboutme_app")
		_, err := app.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
		expectSQLState(t, "55000", err)
	})

	t.Run("wrong-current-row", func(t *testing.T) {
		db := runtimeWriteDB(t)
		migrator := runtimeWriteConn(t, db, "aboutme_migrator")
		execSQL(t, migrator, `SELECT public.runtime_enter_migrator()`)
		tx, err := migrator.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		execSQL(t, tx, `SELECT public.runtime_begin_migration_write('wrong-marker'); SAVEPOINT poison; SET LOCAL ROLE aboutme_runtime_owner; UPDATE pg_temp.runtime_write_entry_v1 SET backend_pid=backend_pid+1; RESET ROLE`)
		_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_finish_write()`)
		expectSQLState(t, "AM001", err)
		execSQL(t, tx, `ROLLBACK TO SAVEPOINT poison; SELECT public.runtime_finish_write()`)
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		execSQL(t, migrator, `SELECT public.runtime_exit_migrator()`)
	})
}

func TestRuntimeWriteDiscardTempBeforeFinishFails(t *testing.T) {
	db := runtimeWriteDB(t)
	app := runtimeWriteConn(t, db, "aboutme_app")
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	execSQL(t, tx, `SELECT public.runtime_enter_write(); SAVEPOINT discard_attempt`)
	if _, err = tx.ExecContext(context.Background(), `DISCARD TEMP`); err == nil {
		t.Fatal("DISCARD TEMP succeeded with pending finish trigger")
	}
	execSQL(t, tx, `ROLLBACK TO SAVEPOINT discard_attempt; SELECT public.runtime_finish_write()`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
}

func TestRuntimeWriteDiscardTempAfterFinishCannotReenter(t *testing.T) {
	db := runtimeWriteDB(t)
	app := runtimeWriteConn(t, db, "aboutme_app")
	initial, _, _ := runtimeWriteState(t, db)
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write(); SET CONSTRAINTS ALL IMMEDIATE; DISCARD TEMP; SAVEPOINT reenter_after_discard`)
	_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
	expectSQLState(t, "AM001", err)
	execSQL(t, tx, `ROLLBACK TO SAVEPOINT reenter_after_discard`)
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	if generation, _, _ := runtimeWriteState(t, db); generation != initial {
		t.Fatalf("clean discarded finish changed generation to %d", generation)
	}
}

func TestRuntimeWriteFinishGuardNamespaceInventory(t *testing.T) {
	db := runtimeWriteDB(t)
	digest := sha256.Sum256([]byte("aboutme.runtime-write-finish.v1"))
	if got := int32(binary.BigEndian.Uint32(digest[:4])); got != 295306100 {
		t.Fatalf("finish namespace=%d", got)
	}
	rows, err := db.QueryContext(context.Background(), `SELECT proname FROM pg_proc WHERE pronamespace='public'::regnamespace AND prosrc LIKE '%295306100%' ORDER BY proname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"runtime_begin_migration_write", "runtime_enter_write", "runtime_finish_write"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("finish namespace functions=%v, want %v", names, want)
	}
}

func TestRuntimeWriteFinishGuardConcurrentBackends(t *testing.T) {
	db := runtimeWriteDB(t)
	first := runtimeWriteConn(t, db, "aboutme_app")
	second := runtimeWriteConn(t, db, "aboutme_app")
	transactions := make([]*sql.Tx, 0, 2)
	pids := make([]int, 0, 2)
	for _, conn := range []*sql.Conn{first, second} {
		tx, err := conn.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		transactions = append(transactions, tx)
		execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`)
		var pid int
		if err := tx.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	if pids[0] == pids[1] {
		t.Fatal("connections unexpectedly share backend PID")
	}
	for i, tx := range transactions {
		var locks int
		if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND classid=295306100 AND objid=pg_backend_pid() AND objsubid=2 AND mode='ExclusiveLock' AND granted`).Scan(&locks); err != nil {
			t.Fatal(err)
		}
		if locks != 1 {
			t.Fatalf("backend %d guard count=%d", i, locks)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeWriteFinishGuardCommitRollbackPIDReuse(t *testing.T) {
	db := runtimeWriteDB(t)
	app := runtimeWriteConn(t, db, "aboutme_app")
	for _, commit := range []bool{true, false, true} {
		tx, err := app.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		execSQL(t, tx, `SELECT public.runtime_enter_write(); SELECT public.runtime_finish_write()`)
		if commit {
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		} else if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeWriteFinishGuardManualEarlyLockFails(t *testing.T) {
	t.Run("application-entry", func(t *testing.T) {
		db := runtimeWriteDB(t)
		app := runtimeWriteConn(t, db, "aboutme_app")
		tx, err := app.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		execSQL(t, tx, `SELECT pg_advisory_xact_lock(295306100,pg_backend_pid())`)
		_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_enter_write()`)
		expectSQLState(t, "AM001", err)
	})

	t.Run("migration-begin", func(t *testing.T) {
		db := runtimeWriteDB(t)
		migrator := runtimeWriteConn(t, db, "aboutme_migrator")
		execSQL(t, migrator, `SELECT public.runtime_enter_migrator()`)
		tx, err := migrator.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		execSQL(t, tx, `SELECT pg_advisory_xact_lock(295306100,pg_backend_pid())`)
		_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_begin_migration_write('early-guard')`)
		expectSQLState(t, "AM001", err)
	})
}

func TestRuntimeWriteFinishGuardForeignHolderDoesNotWait(t *testing.T) {
	db := runtimeWriteDB(t)
	app := runtimeWriteConn(t, db, "aboutme_app")
	holder, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close() //nolint:errcheck
	var appPID int
	if queryErr := app.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&appPID); queryErr != nil {
		t.Fatal(queryErr)
	}
	execSQL(t, holder, `SELECT pg_advisory_lock(295306100,$1)`, appPID)
	defer func() {
		if _, unlockErr := holder.ExecContext(context.Background(), `SELECT pg_advisory_unlock(295306100,$1)`, appPID); unlockErr != nil {
			t.Logf("unlock foreign finish guard: %v", unlockErr)
		}
	}()
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	execSQL(t, tx, `SELECT public.runtime_enter_write()`)
	started := time.Now()
	_, err = tx.ExecContext(context.Background(), `SELECT public.runtime_finish_write()`)
	expectSQLState(t, "AM001", err)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("finish guard waited %v", elapsed)
	}
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
