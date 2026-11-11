// Package store provides the pgx/v5 connection pool used by the API server.
package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxPoolSize is the hard cap on pool connections, per the resource budget
// (below Postgres max_connections). See docs/design/budgets.md.
const MaxPoolSize = 20

type runtimeSetting struct{ name, value string }

// timeoutDefaults are the session settings every pooled connection starts
// with (docs/design/budgets.md). There is no lock_timeout: a lock wait counts
// toward the statement timeout. An operation that legitimately runs longer
// sets its own value with SET LOCAL inside its transaction.
func timeoutDefaults() []runtimeSetting {
	return []runtimeSetting{
		{name: "statement_timeout", value: "15s"},
		{name: "idle_in_transaction_session_timeout", value: "30s"},
	}
}

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
//
// Every connection starts with a 15 s statement_timeout and a 30 s
// idle_in_transaction_session_timeout, sent as startup parameters. A value
// databaseURL already sets, directly or through options, is kept; that
// includes a lock_timeout, which NewPool never adds.
func NewPool(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse database url: %w", err)
	}

	if cfg.MaxConns <= 0 || cfg.MaxConns > MaxPoolSize {
		cfg.MaxConns = MaxPoolSize
	}
	for _, setting := range timeoutDefaults() {
		if !HasRuntimeSetting(cfg.ConnConfig.RuntimeParams, setting.name) {
			cfg.ConnConfig.RuntimeParams[setting.name] = setting.value
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: create pool: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// HasRuntimeSetting reports whether params, the startup parameters of a
// parsed pgx connection configuration, may already set the server setting
// name: params has it as a key, or the options parameter mentions it
// anywhere. PostgreSQL applies the other startup parameters after options,
// so a default added beside an options switch would override it. Names
// match without case, with dashes and underscores alike. A mention that is
// not a switch, or a longer name that contains this one, also counts; that
// only skips a default.
func HasRuntimeSetting(params map[string]string, name string) bool {
	want := settingName(name)
	for key, value := range params {
		if key == "options" {
			if strings.Contains(settingName(value), want) {
				return true
			}
			continue
		}
		if settingName(key) == want {
			return true
		}
	}
	return false
}

func settingName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "-", "_"))
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
