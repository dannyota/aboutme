package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	// Registers the pgx driver under database/sql's "pgx" name.
	_ "github.com/jackc/pgx/v5/stdlib"

	"danny.vn/aboutme/apps/server/migrations"
)

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

func MigrateTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	if err := migratedTestDatabaseSetup.prepare(dsn); err != nil {
		t.Fatalf("testutil: prepare migrated test database: %v", err)
	}
}

func RequireMigratedTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := RequireTestDatabaseURL(t)
	MigrateTestDatabase(t, dsn)
	return dsn
}

var newMigratedTestDatabaseCounter atomic.Uint64

func NewMigratedTestDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	base := RequireTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close admin connection: %v", closeErr)
		}
	})
	if pingErr := admin.PingContext(ctx); pingErr != nil {
		t.Fatalf("ping admin connection (is TEST_DATABASE_URL reachable?): %v", pingErr)
	}

	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), newMigratedTestDatabaseCounter.Add(1))
	if _, createErr := admin.ExecContext(ctx, `CREATE DATABASE `+name); createErr != nil {
		t.Fatalf("create database %s: %v", name, createErr)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// DROP DATABASE ... WITH (FORCE) (PostgreSQL 13+) ends lingering sessions instead of failing.
		if _, dropErr := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop database %s: %v", name, dropErr)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn := u.String()

	if bootstrapErr := bootstrapTestDatabaseRoles(ctx, dsn); bootstrapErr != nil {
		t.Fatalf("bootstrap roles on %s: %v", name, bootstrapErr)
	}

	db, err := migrations.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close migrated database: %v", err)
		}
	})
	if _, err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations to %s: %v", name, err)
	}

	return dsn, db
}
