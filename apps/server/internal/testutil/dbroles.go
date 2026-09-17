package testutil

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	// Registers the pgx driver under database/sql's "pgx" name for the
	// sql.Open call below. testutil is imported only by _test.go files, so
	// this registration only ever runs inside a test binary.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

// BootstrapTestDatabaseRoles ensures the fixed aboutme_migrator/aboutme_app
// roles exist and applies databaseURL's database and schema grants --
// exactly what a real deployment's db-setup does before migrating. Safe to
// call repeatedly, including from multiple test databases that share a
// cluster: role creation is idempotent cluster-wide, and the grants it
// (re)applies are idempotent per database (see internal/dbroles.Ensure).
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
