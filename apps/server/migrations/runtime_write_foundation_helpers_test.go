package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

const runtimeBarrierKey int64 = 3727108639518528074

func runtimeWriteDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t, newTestDatabase(t))
	if _, err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

func runtimeWriteProbe(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
CREATE TABLE public.runtime_write_probe(id integer PRIMARY KEY, value integer NOT NULL DEFAULT 0);
ALTER TABLE public.runtime_write_probe OWNER TO aboutme_runtime_owner;
CREATE TRIGGER runtime_write_probe_assert
  BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_write_probe
  FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_business_write('probe');
GRANT SELECT,INSERT,UPDATE,DELETE ON public.runtime_write_probe TO aboutme_app;
GRANT SELECT,INSERT,UPDATE,DELETE ON public.runtime_write_probe TO aboutme_maintenance;
GRANT SELECT,INSERT,UPDATE,DELETE ON public.runtime_write_probe TO aboutme_migrator`)
	if err != nil {
		t.Fatalf("create runtime write probe: %v", err)
	}
}

func expectSQLState(t *testing.T, want string, err error) {
	t.Helper()
	if got := sqlState(err); got != want {
		t.Fatalf("SQLSTATE=%q, want %q: %v", got, want, err)
	}
}

func runtimeProbeCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_write_probe`).Scan(&count); err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	return count
}

func execSQL(t *testing.T, e interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, statement string, args ...any) {
	t.Helper()
	if _, err := e.ExecContext(context.Background(), statement, args...); err != nil {
		t.Fatalf("execute %q: %v", statement, err)
	}
}

func markerCollisionSQL(kind string) string {
	switch kind {
	case "view":
		return `CREATE TEMP VIEW runtime_write_entry_v1 AS SELECT 1 AS value`
	case "type":
		return `CREATE TYPE pg_temp.runtime_write_entry_v1 AS (value integer)`
	case "domain":
		return `CREATE DOMAIN pg_temp.runtime_write_entry_v1 AS integer`
	case "enum":
		return `CREATE TYPE pg_temp.runtime_write_entry_v1 AS ENUM ('poison')`
	case "index-name":
		return `CREATE TEMP TABLE collision_source(value integer); CREATE INDEX runtime_write_entry_v1_pkey ON pg_temp.collision_source(value)`
	case "wrong-table":
		return `CREATE TEMP TABLE runtime_write_entry_v1(value integer)`
	default:
		panic(fmt.Sprintf("unknown collision %q", kind))
	}
}

func waitForRuntimeCondition(db *sql.DB, statement string, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ready bool
		if err := db.QueryRowContext(ctx, statement, args...).Scan(&ready); err != nil {
			return err
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for database lock condition")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runtimeWriteConn(t *testing.T, db *sql.DB, role string) *sql.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET SESSION AUTHORIZATION "+role); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("close failed connection: %v", closeErr)
		}
		t.Fatalf("set session authorization %s: %v", role, err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.ExecContext(cleanup, `RESET ROLE`); err != nil {
			t.Logf("reset role: %v", err)
		}
		if _, err := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); err != nil {
			t.Logf("reset session authorization: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Logf("close dedicated connection: %v", err)
		}
	})
	return conn
}

func runtimeWriteState(t *testing.T, db *sql.DB) (int64, time.Time, time.Time) {
	t.Helper()
	var generation int64
	var lastWrite, lastAccepted time.Time
	if err := db.QueryRowContext(context.Background(), `SELECT generation, last_write_at, last_accepted_writer_at FROM public.runtime_write_state WHERE singleton`).Scan(&generation, &lastWrite, &lastAccepted); err != nil {
		t.Fatalf("read runtime state: %v", err)
	}
	return generation, lastWrite, lastAccepted
}
