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

	"danny.vn/aboutme/apps/server/internal/testutil"
)

var testDatabaseCounter atomic.Uint64

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
		// DROP DATABASE ... WITH (FORCE) (PostgreSQL 13+) ends lingering sessions instead of failing.
		if _, err := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Logf("cleanup: drop database %s: %v", name, err)
		}
	})

	dsn := dsnWithDatabase(t, base, name)
	testutil.BootstrapTestDatabaseRoles(t, dsn)
	return dsn
}

func dsnWithDatabase(t *testing.T, base, name string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + strings.TrimPrefix(name, "/")
	return u.String()
}

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
