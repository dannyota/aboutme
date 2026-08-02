package testutil

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	// Registers the pgx driver under database/sql's "pgx" name, so
	// MigrateTestDatabase's sql.Open("pgx", dsn) below resolves. testutil is
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

// MigrateTestDatabase applies every pending migration to dsn. It is safe to
// call from more than one DB-backed package's test setup, including
// concurrently across separate `go test` binaries (as `go test ./...`
// spawns one per package): migrations.Apply serializes through the same
// Postgres session-level advisory lock (migrations.LockID) production
// deploys use, and goose re-checks which migrations are actually still
// pending under that lock rather than trusting a snapshot taken before it
// was acquired, so a second, third, or Nth caller applying an
// already-migrated database is a fast no-op, not a race or a double-apply.
//
// Every DB-backed package's test setup must call this (directly, or via
// RequireMigratedTestDatabaseURL) before touching any table it didn't
// create itself. Before this helper existed, only internal/auth's test
// setup happened to apply migrations, so internal/user's and
// internal/store's integration tests silently depended on internal/auth's
// package tests having already run first in the same `go test ./...`
// invocation -- an undeclared cross-package ordering dependency that failed
// with "relation \"users\" does not exist" against a cold database whenever
// that ordering didn't hold (e.g. `go test ./internal/user/...` run alone).
func MigrateTestDatabase(t *testing.T, dsn string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	migrationDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("testutil: open database for migrations: %v", err)
	}
	defer func() {
		if closeErr := migrationDB.Close(); closeErr != nil {
			t.Logf("testutil: close migration database: %v", closeErr)
		}
	}()

	if err := migrationDB.PingContext(ctx); err != nil {
		t.Fatalf("testutil: ping database (is TEST_DATABASE_URL reachable?): %v", err)
	}
	if _, err := migrations.Apply(ctx, migrationDB); err != nil {
		t.Fatalf("testutil: apply migrations: %v", err)
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
