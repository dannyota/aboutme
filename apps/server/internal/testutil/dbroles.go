package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

// BootstrapTestDatabaseRoles ensures cluster roles through the common postgres database.
func BootstrapTestDatabaseRoles(t *testing.T, databaseURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := bootstrapTestDatabaseRoles(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
}

func bootstrapTestDatabaseRoles(ctx context.Context, databaseURL string) (resultErr error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("testutil: invalid role-bootstrap connection configuration")
	}
	config.Database = "postgres"
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(1)
	defer func() {
		if err := db.Close(); err != nil {
			resultErr = errors.Join(resultErr, errors.New("testutil: close role-bootstrap connection failed"))
		}
	}()
	if _, err := dbroles.Ensure(ctx, db); err != nil {
		return errors.New("testutil: database role bootstrap failed")
	}
	return nil
}
