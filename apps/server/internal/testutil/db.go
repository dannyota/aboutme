package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	// Registers the pgx driver under database/sql's "pgx" name, so
	// prepareMigratedTestDatabase's sql.Open("pgx", dsn) resolves. testutil is
	// imported only by _test.go files (never production code, see the
	// package doc comment in clock.go), so this registration only ever runs
	// inside a test binary.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// RequireTestDatabaseURL returns TEST_DATABASE_URL, skipping the calling
// test (not failing it) when unset -- UNLESS REQUIRE_TEST_DB=1 is also set
// in the environment, in which case a missing TEST_DATABASE_URL is a hard
// t.Fatal instead. `make server-test-db` sets REQUIRE_TEST_DB=1 precisely so
// a gate run can never pass vacuously with every DB-backed test silently
// skipped.
func RequireTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("REQUIRE_TEST_DB=1 is set but TEST_DATABASE_URL is unset; refusing to silently skip this live-database test")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping live-database integration test")
	}
	return dsn
}

// MigrateTestDatabase prepares each exact DSN once per test binary. Concurrent
// and later callers receive the same result, including a setup failure.
// Separate test binaries retain Apply's database lock and protected recheck.
// Each DB-backed test calls this before using tables it did not create.
func MigrateTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	if err := migratedTestDatabaseSetup.prepare(dsn); err != nil {
		t.Fatalf("testutil: prepare migrated test database: %v", err)
	}
}

// RequireMigratedTestDatabaseURL is the common case for a DB-backed
// package's test setup: it combines RequireTestDatabaseURL (skip, or
// fail-closed under REQUIRE_TEST_DB=1, when no live database is
// configured) with MigrateTestDatabase (bring that database's schema to
// head), so callers get back a DSN they can open a pool or transaction
// against and immediately query real tables with.
func RequireMigratedTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := RequireTestDatabaseURL(t)
	MigrateTestDatabase(t, dsn)
	return dsn
}

var (
	migratedTemplateOnce sync.Once
	migratedTemplateName string
	migratedTemplateErr  error
	migratedCloneCounter atomic.Uint64
)

// NewMigratedTestDatabase returns the DSN and an open pool for a disposable
// clone of every embedded migration. The clone is dropped in t.Cleanup.
func NewMigratedTestDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	base := RequireTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	migratedTemplateOnce.Do(func() {
		if err := bootstrapTestDatabaseRoles(ctx, base); err != nil {
			migratedTemplateErr = err
			return
		}
		migratedTemplateName, migratedTemplateErr = migrations.EnsureTemplateDatabase(ctx, base, migrations.FS, 0)
	})
	if migratedTemplateErr != nil {
		t.Fatalf("build migrated template: %v", migratedTemplateErr)
	}
	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), migratedCloneCounter.Add(1))
	if err := migrations.CloneTemplateDatabase(ctx, base, migratedTemplateName, name); err != nil {
		t.Fatalf("clone migrated template: %v", err)
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, dropErr := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop migrated clone: %v", dropErr)
		}
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close admin: %v", closeErr)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close migrated clone: %v", err)
		}
	})
	return u.String(), db
}
