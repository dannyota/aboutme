package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testDatabaseCounter disambiguates database names created within the same
// nanosecond (t.Parallel means several harness tests can call
// newTestDatabase at almost the same instant).
var testDatabaseCounter atomic.Uint64

// newTestDatabase creates a fresh, uniquely named database on the server
// pointed to by TEST_DATABASE_URL and returns a connection URL for it. The
// database is dropped in t.Cleanup. Every harness test gets its own
// database — never a shared one — so migration state, advisory locks, and
// concurrent-runner timing from one test can never leak into another; that
// isolation is what lets the harness tests run with t.Parallel().
//
// Skips the test (not a failure) when TEST_DATABASE_URL is unset, exactly
// like the existing internal/store integration test, so `go test ./...`
// stays fully hermetic by default.
func newTestDatabase(t *testing.T) string {
	t.Helper()

	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping live-database integration test")
	}

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Logf("close admin connection: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("ping admin connection (is TEST_DATABASE_URL reachable?): %v", err)
	}

	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), testDatabaseCounter.Add(1))
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// WITH (FORCE) (Postgres 13+) disconnects any lingering sessions —
		// e.g. a test's own *sql.DB pool that hasn't finished closing yet —
		// instead of DROP DATABASE failing with "database is being accessed
		// by other users".
		if _, err := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Logf("cleanup: drop database %s: %v", name, err)
		}
	})

	return dsnWithDatabase(t, base, name)
}

// dsnWithDatabase returns base with its database (URL path) replaced by
// name, preserving host, port, credentials, and query parameters (e.g.
// sslmode).
func dsnWithDatabase(t *testing.T, base, name string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + strings.TrimPrefix(name, "/")
	return u.String()
}

// openTestDB opens a *sql.DB against dsn and registers a cleanup that
// closes it. Kept tiny and separate from newTestDatabase so concurrency
// tests can open several independent connection pools against the same
// database (simulating separate runner processes), each with its own
// t.Cleanup-scoped lifetime.
func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})
	return db
}
