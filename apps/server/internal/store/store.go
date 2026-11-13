package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxPoolSize = 20

type runtimeSetting struct{ name, value string }

func timeoutDefaults() []runtimeSetting {
	return []runtimeSetting{
		{name: "statement_timeout", value: "15s"},
		{name: "idle_in_transaction_session_timeout", value: "30s"},
	}
}

type Pool struct {
	*pgxpool.Pool
}

// pgxpool does not dial until a connection is needed, so NewPool succeeds while the database is unreachable.
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

// PostgreSQL applies other startup parameters after options, so a default beside an options switch would override it.
func HasRuntimeSetting(params map[string]string, name string) bool {
	want := settingName(name)
	for key, value := range params {
		if key == "options" {
			// PostgreSQL's options parsing drops a backslash that escapes the next character.
			if strings.Contains(settingName(strings.ReplaceAll(value, `\`, "")), want) {
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
