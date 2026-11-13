package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"danny.vn/aboutme/apps/server/internal/store"
)

//go:embed *.sql
var FS embed.FS

const LockID int64 = 2561609096

const migratorRole = "aboutme_migrator"

func Open(dsn string) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("migrations: parse database configuration: %w", err)
	}
	for _, setting := range []struct{ name, value string }{
		{name: "statement_timeout", value: "0"},
		{name: "lock_timeout", value: "5s"},
	} {
		if !store.HasRuntimeSetting(config.RuntimeParams, setting.name) {
			config.RuntimeParams[setting.name] = setting.value
		}
	}
	afterConnect := stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `SET ROLE `+migratorRole); err != nil {
			return fmt.Errorf("migrations: set role %s: %w", migratorRole, err)
		}
		var current string
		if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&current); err != nil {
			return fmt.Errorf("migrations: verify migrator role: %w", err)
		}
		if current != migratorRole {
			return fmt.Errorf("migrations: current_user = %s, want %s", current, migratorRole)
		}
		return nil
	})
	return stdlib.OpenDB(*config, afterConnect), nil
}

func NewProvider(db *sql.DB, fsys fs.FS, lockOpts ...lock.SessionLockerOption) (*goose.Provider, error) {
	allLockOpts := append([]lock.SessionLockerOption{lock.WithLockID(LockID)}, lockOpts...)
	locker, err := lock.NewPostgresSessionLocker(allLockOpts...)
	if err != nil {
		return nil, fmt.Errorf("migrations: create session locker: %w", err)
	}

	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("migrations: create provider: %w", err)
	}
	return p, nil
}

// goose's Provider.Status takes the SessionLocker whenever one is configured, so this provider has none.
func newLockFreeProvider(db *sql.DB, fsys fs.FS) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return nil, fmt.Errorf("migrations: create lock-free provider: %w", err)
	}
	return p, nil
}

func Apply(ctx context.Context, db *sql.DB, lockOpts ...lock.SessionLockerOption) ([]*goose.MigrationResult, error) {
	p, err := NewProvider(db, FS, lockOpts...)
	if err != nil {
		return nil, err
	}
	return p.Up(ctx)
}

func Status(ctx context.Context, db *sql.DB) ([]*goose.MigrationStatus, error) {
	p, err := newLockFreeProvider(db, FS)
	if err != nil {
		return nil, err
	}
	return p.Status(ctx)
}

func PendingCount(statuses []*goose.MigrationStatus) int {
	n := 0
	for _, s := range statuses {
		if s.State == goose.StatePending {
			n++
		}
	}
	return n
}
