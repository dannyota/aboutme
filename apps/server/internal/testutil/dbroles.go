package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

// BootstrapTestDatabaseRoles ensures cluster roles through the common postgres database.
func BootstrapTestDatabaseRoles(t *testing.T, databaseURL string) {
	t.Helper()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("testutil: invalid role-bootstrap connection configuration")
	}
	config.Database = "postgres"
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(1)
	defer func() {
		if err := db.Close(); err != nil {
			t.Error("testutil: close role-bootstrap connection failed")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := dbroles.Ensure(ctx, db); err != nil {
		t.Fatal("testutil: database role bootstrap failed")
	}
}
