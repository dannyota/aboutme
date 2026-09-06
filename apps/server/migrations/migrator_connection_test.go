package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMigrationDriverAcceptsPinnedPGX(t *testing.T) {
	db := openMigrationConnectionTestDB(t)
	conn := migrationConnection(t, db)
	if err := verifyMigrationDriver(conn); err != nil {
		t.Fatal(err)
	}
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil || pid <= 0 {
		t.Fatalf("pid=%d error=%v", pid, err)
	}
}

func TestMigrationDriverRejectsUnsupportedBeforeSQL(t *testing.T) {
	var queries atomic.Int32
	db := sql.OpenDB(&unsupportedMigrationConnector{queries: &queries})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close unsupported database: %v", err)
		}
	})
	conn := migrationConnection(t, db)
	if err := verifyMigrationDriver(conn); err == nil {
		t.Fatal("unsupported driver accepted")
	}
	if got := queries.Load(); got != 0 {
		t.Fatalf("queries=%d", got)
	}
}

func TestApplyRejectsUnsupportedTransportBeforeCatalogSQL(t *testing.T) {
	var queries, closes atomic.Int32
	db := sql.OpenDB(&unsupportedMigrationConnector{queries: &queries, closes: &closes})
	if _, err := Apply(context.Background(), db, LocalAdminMigratorIdentity()); err == nil {
		t.Fatal("Apply accepted unsupported transport")
	}
	if got := queries.Load(); got != 0 {
		t.Fatalf("queries=%d", got)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("driver closes=%d", got)
	}
}

func TestRetireMigrationBackendDisposesUnsupportedDriverWithoutSQL(t *testing.T) {
	var queries, closes atomic.Int32
	db := sql.OpenDB(&unsupportedMigrationConnector{queries: &queries, closes: &closes})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close unsupported database: %v", err)
		}
	})
	conn := migrationConnection(t, db)
	if _, err := captureMigrationBackend(conn); err == nil {
		t.Fatal("unsupported driver capture succeeded")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := queries.Load(); got != 0 {
		t.Fatalf("queries=%d", got)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("driver closes=%d", got)
	}
}

func TestRetireMigrationBackendClosesPhysicalConnectionBeforeReturn(t *testing.T) {
	db := openMigrationConnectionTestDB(t)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	conn := migrationConnection(t, db)
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		t.Fatal(err)
	}
	var oldPID int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&oldPID); err != nil {
		t.Fatal(err)
	}
	if err := finalizeMigrationBackend(context.Background(), backend, conn, false); err != nil {
		t.Fatal(err)
	}
	assertMigrationConnectionBackendGone(t, db, oldPID)

	replacement := migrationConnection(t, db)
	var newPID int32
	if err := replacement.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&newPID); err != nil {
		t.Fatal(err)
	}
	if newPID == oldPID {
		t.Fatalf("oldPID=%d newPID=%d", oldPID, newPID)
	}
}

func TestRetireMigrationBackendIgnoresCanceledCaller(t *testing.T) {
	db := openMigrationConnectionTestDB(t)
	conn := migrationConnection(t, db)
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		t.Fatal(err)
	}
	var oldPID int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&oldPID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := finalizeMigrationBackend(ctx, backend, conn, false); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("retirement took %v", elapsed)
	}
	assertMigrationConnectionBackendGone(t, db, oldPID)
}

func TestRetireMigrationBackendReportsUnavailableRawCallback(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn := migrationConnection(t, db)
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backend.retire(context.Background(), conn); err != nil {
		t.Fatalf("retirement after Raw unavailable: %v", err)
	}
}

func TestMigrationConnectionRejectsNil(t *testing.T) {
	if err := verifyMigrationDriver(nil); err == nil {
		t.Fatal("nil driver admission succeeded")
	}
	if _, err := captureMigrationBackend(nil); err == nil {
		t.Fatal("nil capture succeeded")
	}
}

func openMigrationConnectionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func migrationConnection(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close connection: %v", err)
		}
	})
	return conn
}

func assertMigrationConnectionBackendGone(t *testing.T, db *sql.DB, pid int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)`, pid).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("backend PID %d remains", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type unsupportedMigrationConnector struct {
	queries *atomic.Int32
	closes  *atomic.Int32
}

func (c *unsupportedMigrationConnector) Connect(context.Context) (driver.Conn, error) {
	return &unsupportedMigrationConn{queries: c.queries, closes: c.closes}, nil
}

func (*unsupportedMigrationConnector) Driver() driver.Driver { return unsupportedMigrationDriver{} }

type unsupportedMigrationDriver struct{}

func (unsupportedMigrationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type unsupportedMigrationConn struct {
	queries *atomic.Int32
	closes  *atomic.Int32
}

func (*unsupportedMigrationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.ErrUnsupported
}
func (c *unsupportedMigrationConn) Close() error {
	if c.closes != nil {
		c.closes.Add(1)
	}
	return nil
}
func (*unsupportedMigrationConn) Begin() (driver.Tx, error) { return nil, errors.ErrUnsupported }
func (c *unsupportedMigrationConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.queries.Add(1)
	return nil, fmt.Errorf("unexpected query: %w", errors.ErrUnsupported)
}
