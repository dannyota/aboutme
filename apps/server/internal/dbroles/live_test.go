package dbroles

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func livePostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("REQUIRE_TEST_DB") != "1" {
		t.Skip("REQUIRE_TEST_DB is not 1")
	}
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is required when REQUIRE_TEST_DB=1")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal("open TEST_DATABASE_URL")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func TestEnsureLiveCreatesOrReusesExactRoles(t *testing.T) {
	db := livePostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var before int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_catalog.pg_roles WHERE rolname IN ('aboutme_migrator','aboutme_app')`).Scan(&before); err != nil {
		t.Fatal("count fixed roles")
	}
	result, err := Ensure(ctx, db)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result.Created+result.Verified != 2 {
		t.Fatalf("result = %+v, want two roles", result)
	}
	t.Logf("fixed roles existing=%d created=%d verified=%d", before, result.Created, result.Verified)
	reused, err := Ensure(ctx, db)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if reused != (Result{Verified: 2}) {
		t.Fatalf("second result = %+v, want two verified", reused)
	}
}

func TestEnsureLiveSerializesWithinDatabase(t *testing.T) {
	db := livePostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Ensure(ctx, db); err != nil {
		t.Fatalf("prepare roles: %v", err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin holder")
	}
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, LockID); err != nil {
		t.Fatal("hold db-setup lock")
	}
	done := make(chan error, 1)
	go func() { _, err := Ensure(ctx, db); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("Ensure returned before lock release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal("release db-setup lock")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ensure after release: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Ensure did not finish after lock release")
	}
}

// TestEnsureLiveGrantsExactDatabaseAndSchemaPrivileges checks the database
// and schema privileges each role holds after Ensure. A role with no grants
// stands in for PUBLIC.
func TestEnsureLiveGrantsExactDatabaseAndSchemaPrivileges(t *testing.T) {
	db := livePostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Ensure(ctx, db); err != nil {
		t.Fatalf("prepare roles: %v", err)
	}

	probe := fmt.Sprintf("aboutme_dbroles_probe_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE ROLE `+probe+` NOLOGIN NOINHERIT`); err != nil {
		t.Fatalf("create probe role: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, `DROP ROLE IF EXISTS `+probe); err != nil {
			t.Errorf("drop probe role: %v", err)
		}
	})

	type privilege struct {
		role, kind, name, grant string
		want                    bool
	}
	checks := []privilege{
		{"aboutme_migrator", "database", "", "CONNECT", true},
		{"aboutme_migrator", "database", "", "CREATE", true},
		{"aboutme_migrator", "database", "", "TEMP", false},
		{"aboutme_app", "database", "", "CONNECT", true},
		{"aboutme_app", "database", "", "CREATE", false},
		{"aboutme_app", "database", "", "TEMP", false},
		{probe, "database", "", "CONNECT", false},
		{probe, "database", "", "CREATE", false},
		// db-setup revokes ALL from PUBLIC, including TEMP.
		{probe, "database", "", "TEMP", false},
		{"aboutme_migrator", "schema", "public", "USAGE", true},
		{"aboutme_migrator", "schema", "public", "CREATE", true},
		{"aboutme_app", "schema", "public", "USAGE", true},
		{"aboutme_app", "schema", "public", "CREATE", false},
		// db-setup keeps PUBLIC's default USAGE on schema public.
		{probe, "schema", "public", "USAGE", true},
		{probe, "schema", "public", "CREATE", false},
	}
	for _, c := range checks {
		var query string
		switch c.kind {
		case "database":
			query = `SELECT has_database_privilege($1, current_database(), $2)`
		case "schema":
			query = `SELECT has_schema_privilege($1, $3, $2)`
		}
		var got bool
		var err error
		if c.kind == "schema" {
			err = db.QueryRowContext(ctx, query, c.role, c.grant, c.name).Scan(&got)
		} else {
			err = db.QueryRowContext(ctx, query, c.role, c.grant).Scan(&got)
		}
		if err != nil {
			t.Fatalf("check %s %s %s: %v", c.role, c.kind, c.grant, err)
		}
		if got != c.want {
			t.Fatalf("%s %s privilege %s = %v, want %v", c.role, c.kind, c.grant, got, c.want)
		}
	}
}

// TestEnsureLiveSetsNoDefaultACLs asserts Ensure never runs ALTER DEFAULT
// PRIVILEGES: every migration grants aboutme_app explicitly, so a future
// object never inherits a standing default grant.
func TestEnsureLiveSetsNoDefaultACLs(t *testing.T) {
	db := livePostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Ensure(ctx, db); err != nil {
		t.Fatalf("prepare roles: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_default_acl`).Scan(&count); err != nil {
		t.Fatalf("count pg_default_acl: %v", err)
	}
	if count != 0 {
		t.Errorf("pg_default_acl rows = %d, want 0", count)
	}
}
