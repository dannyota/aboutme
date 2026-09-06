package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var sessionDatabaseCounter atomic.Uint64
var sessionDatabasePattern = regexp.MustCompile(`^aboutme_migrate_test_[0-9]+_[0-9]+$`)

func newSessionTestDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("TEST_DATABASE_URL not set")
	}
	admin, openErr := sql.Open("pgx", base)
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Errorf("close admin: %v", err)
		}
	})
	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), sessionDatabaseCounter.Add(1))
	if !sessionDatabasePattern.MatchString(name) {
		t.Fatalf("unsafe database name %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + strings.TrimPrefix(name, "/")
	dsn := u.String()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	applyCtx, applyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer applyCancel()
	if _, err := Apply(applyCtx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return dsn, db
}

func sessionBackendGone(t *testing.T, admin *sql.DB, pid int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		var exists bool
		if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)`, pid).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("PID %d remains", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
