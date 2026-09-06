package testutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

var migratedTestDatabaseSetup = newTestDatabaseSetupCache(prepareMigratedTestDatabase)

type testDatabaseSetupCache struct {
	mu      sync.Mutex
	entries map[string]*testDatabaseSetupEntry
	setup   func(string) error
}

type testDatabaseSetupEntry struct {
	done chan struct{}
	err  error
}

func newTestDatabaseSetupCache(setup func(string) error) *testDatabaseSetupCache {
	return &testDatabaseSetupCache{
		entries: make(map[string]*testDatabaseSetupEntry),
		setup:   setup,
	}
}

func (cache *testDatabaseSetupCache) prepare(dsn string) error {
	cache.mu.Lock()
	if entry, ok := cache.entries[dsn]; ok {
		cache.mu.Unlock()
		<-entry.done
		return entry.err
	}
	entry := &testDatabaseSetupEntry{done: make(chan struct{})}
	cache.entries[dsn] = entry
	cache.mu.Unlock()

	entry.err = cache.setup(dsn)
	close(entry.done)
	return entry.err
}

func prepareMigratedTestDatabase(dsn string) (resultErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := bootstrapTestDatabaseRoles(ctx, dsn); err != nil {
		return fmt.Errorf("bootstrap database roles: %w", err)
	}
	migrationDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for migrations: %w", err)
	}
	defer func() {
		if closeErr := migrationDB.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close migration database: %w", closeErr))
		}
	}()

	if err := migrationDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database (is TEST_DATABASE_URL reachable?): %w", err)
	}
	var hasFoundation bool
	if err := migrationDB.QueryRowContext(ctx, `SELECT to_regclass('public.runtime_write_state') IS NOT NULL`).Scan(&hasFoundation); err != nil {
		return fmt.Errorf("inspect migration foundation: %w", err)
	}
	if !hasFoundation {
		if err := migrations.ProvisionDatabase(ctx, migrationDB); err != nil {
			return fmt.Errorf("provision migration database: %w", err)
		}
	}
	_, applyErr := migrations.Apply(ctx, migrationDB, migrations.LocalAdminMigratorIdentity())
	if errors.Is(applyErr, migrations.ErrHistoryAdoptionRequired) {
		if _, err := migrations.AdoptHistoryOwner(ctx, migrationDB); err != nil {
			return fmt.Errorf("adopt local migration history: %w", err)
		}
		_, applyErr = migrations.Apply(ctx, migrationDB, migrations.LocalAdminMigratorIdentity())
	}
	if applyErr != nil {
		return fmt.Errorf("apply migrations: %w", applyErr)
	}
	return nil
}
