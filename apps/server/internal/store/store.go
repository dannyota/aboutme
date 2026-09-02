// Package store provides the pgx/v5 connection pool used by the API server.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxPoolSize is the hard cap on pool connections, per the resource budget
// (below Postgres max_connections). See docs/design/budgets.md.
const MaxPoolSize = 20

// Pool wraps a pgxpool.Pool with a context-aware Close.
type Pool struct {
	*pgxpool.Pool
}

// NewPool parses databaseURL and creates a connection pool capped at
// MaxPoolSize connections. Pool creation is lazy: pgxpool does not dial the
// database until a connection is actually needed (e.g. via Ping or a
// query), so NewPool succeeds even when the database is temporarily
// unreachable. This is required so a DB outage never blocks server startup
// or fails liveness — see internal/api's health handlers.
func NewPool(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse database url: %w", err)
	}

	if cfg.MaxConns <= 0 || cfg.MaxConns > MaxPoolSize {
		cfg.MaxConns = MaxPoolSize
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: create pool: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// Close closes the pool, returning as soon as the close completes or ctx is
// done, whichever comes first. The underlying pool is always closed
// eventually; ctx only bounds how long the caller waits for that to happen.
func (p *Pool) Close(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		p.Pool.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}
