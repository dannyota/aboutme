package testutil

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	// Registers the pgx driver under database/sql's "pgx" name.
	_ "github.com/jackc/pgx/v5/stdlib"

	"danny.vn/aboutme/apps/server/internal/dbroles"
)

func BootstrapTestDatabaseRoles(t *testing.T, databaseURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := bootstrapTestDatabaseRoles(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
}

func bootstrapTestDatabaseRoles(ctx context.Context, databaseURL string) (resultErr error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return errors.New("testutil: invalid role-bootstrap connection configuration")
	}
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
