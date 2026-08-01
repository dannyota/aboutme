package main

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

// requireMigrateTestDatabaseURL returns TEST_DATABASE_URL, skipping the
// calling test (not failing it) when unset — exactly like
// migrations/testdb_test.go's newTestDatabase and internal/store's
// integration test, so `go test ./...` stays fully hermetic by default.
// This package keeps its own small copy of this live-DB test
// infrastructure rather than importing another test package's (unexported,
// and Go test helpers aren't shared across packages without a common
// importable dependency) — see migrations/testdb_test.go's package doc
// comment for the same established convention.
func requireMigrateTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping live-database integration test")
	}
	return dbURL
}

var migrateTestDatabaseCounter atomic.Uint64

// newMigrateTestDatabase creates a fresh, uniquely named database on the
// server pointed to by base and returns a connection URL for it, dropped
// in t.Cleanup.
func newMigrateTestDatabase(t *testing.T, base string) string {
	t.Helper()

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Logf("close admin connection: %v", closeErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = admin.PingContext(ctx); err != nil {
		t.Fatalf("ping admin connection (is TEST_DATABASE_URL reachable?): %v", err)
	}

	name := fmt.Sprintf("aboutme_migrate_cmd_test_%d_%d", time.Now().UnixNano(), migrateTestDatabaseCounter.Add(1))
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, dropErr := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Logf("cleanup: drop database %s: %v", name, dropErr)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + strings.TrimPrefix(name, "/")
	return u.String()
}

func openMigrateTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Logf("close database: %v", closeErr)
		}
	})
	return db
}
