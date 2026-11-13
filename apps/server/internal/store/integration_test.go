package store_test

import (
	"context"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

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
