package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// requireTestDatabaseURL returns TEST_DATABASE_URL, skipping the calling
// test (not failing it) when unset -- UNLESS REQUIRE_TEST_DB=1 is also set
// in the environment, in which case a missing TEST_DATABASE_URL is a hard
// t.Fatal instead, matching internal/auth's own requireTestDatabaseURL
// (transaction_test.go): a gate run (`make server-test-db`, which sets
// REQUIRE_TEST_DB=1) must never pass vacuously with this test silently
// skipped.
func requireTestDatabaseURL(t *testing.T) string {
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

// TestPool_Integration_PingAndQuery exercises NewPool, Ping, and a real
// query against a live Postgres instance (spec §9). It is skipped unless
// TEST_DATABASE_URL is set, so `go test ./...` stays fully hermetic by
// default — no test in this package requires a database to be running.
//
// To run it against the dev compose stack (deploy/compose.yml), start
// Postgres and point TEST_DATABASE_URL at the port your podman/docker setup
// publishes it on, matching the compose defaults for user/password/db, e.g.:
//
//	TEST_DATABASE_URL="postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable" \
//	  go test ./internal/store/... -run Integration -v
func TestPool_Integration_PingAndQuery(t *testing.T) {
	dsn := requireTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	defer pool.Close(ctx)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}

	var got int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
		t.Fatalf("SELECT 1 error: %v", err)
	}
	if got != 1 {
		t.Errorf("SELECT 1 = %d, want 1", got)
	}
}
