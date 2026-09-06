package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var manifestDatabaseCounter atomic.Uint64

func newManifestTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is required when REQUIRE_TEST_DB=1")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping live manifest test")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close manifest admin database: %v", closeErr)
		}
	})
	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), manifestDatabaseCounter.Add(1))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, createErr := admin.ExecContext(ctx, `CREATE DATABASE `+name); createErr != nil {
		t.Fatalf("create disposable manifest database: %v", createErr)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, dropErr := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop disposable manifest database: %v", dropErr)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + strings.TrimPrefix(name, "/")
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close manifest database: %v", closeErr)
		}
	})
	applyContext, stopApply := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopApply()
	provider, providerErr := NewProvider(db, FS)
	if providerErr != nil {
		t.Fatalf("create version-13 manifest provider: %v", providerErr)
	}
	if _, applyErr := provider.UpTo(applyContext, 13); applyErr != nil {
		t.Fatalf("apply migrations to manifest database: %v", applyErr)
	}
	return db
}

func manifestRollbackMutation(t *testing.T, db *sql.DB, statement string, validate func(manifestQueryer) error) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, execErr := tx.ExecContext(context.Background(), statement); execErr != nil {
		t.Fatalf("mutate manifest fixture: %v", execErr)
	}
	return validate(tx)
}
