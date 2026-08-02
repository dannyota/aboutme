package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// TestPool_Integration_PingAndQuery exercises NewPool, Ping, and a real
// query against a live Postgres instance (spec §9). It is skipped unless
// TEST_DATABASE_URL is set, so `go test ./...` stays fully hermetic by
// default — no test in this package requires a database to be running.
// Uses internal/testutil.RequireTestDatabaseURL -- the same shared,
// fail-closed-under-REQUIRE_TEST_DB=1 helper internal/auth and
// internal/user use -- instead of a private copy. This test only runs
// `SELECT 1`, not a real table, so it does not need
// RequireMigratedTestDatabaseURL's migration step.
//
// To run it against the dev compose stack (deploy/compose.yml), start
// Postgres and point TEST_DATABASE_URL at the port your podman/docker setup
// publishes it on, matching the compose defaults for user/password/db, e.g.:
//
//	TEST_DATABASE_URL="postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable" \
//	  go test ./internal/store/... -run Integration -v
func TestPool_Integration_PingAndQuery(t *testing.T) {
	dsn := testutil.RequireTestDatabaseURL(t)

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
