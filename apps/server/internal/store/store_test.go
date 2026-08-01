// Package store_test exercises the pgx/v5 pool constructor: pool-size
// capping, context-aware close, and Ping against an unreachable database
// (no live Postgres instance is required or used by these tests).
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// unreachableDSN points at a closed local port so connection attempts fail
// fast (connection refused) without needing a real Postgres instance.
const unreachableDSN = "postgres://user:pass@127.0.0.1:1/aboutme?connect_timeout=1"

func TestNewPool_ClampsOversizedPoolToBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, unreachableDSN+"&pool_max_conns=50")
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	if got := pool.Config().MaxConns; got != store.MaxPoolSize {
		t.Errorf("MaxConns = %d, want %d (budget cap)", got, store.MaxPoolSize)
	}
}

func TestNewPool_KeepsSmallerConfiguredPoolSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, unreachableDSN+"&pool_max_conns=5")
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	if got := pool.Config().MaxConns; got != 5 {
		t.Errorf("MaxConns = %d, want 5 (below budget, unchanged)", got)
	}
}

func TestNewPool_DefaultPoolSizeRespectsBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, unreachableDSN)
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	if got := pool.Config().MaxConns; got <= 0 || got > store.MaxPoolSize {
		t.Errorf("MaxConns = %d, want within (0, %d]", got, store.MaxPoolSize)
	}
}

func TestNewPool_InvalidDSN(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := store.NewPool(ctx, "not a valid dsn ::: with spaces")
	if err == nil {
		t.Fatal("NewPool() error = nil, want error for invalid DSN")
	}
}

func TestPool_Ping_ReturnsErrorWhenDatabaseUnreachable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, unreachableDSN)
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err == nil {
		t.Error("Ping() error = nil, want error for unreachable database")
	}
}

func TestPool_Close_ReturnsWithinParentContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, unreachableDSN)
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		pool.Close(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not return within 5s")
	}
}
