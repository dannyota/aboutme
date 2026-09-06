package dbroles

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
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
	config, err := parseLivePostgresConfig(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	db := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func parseLivePostgresConfig(databaseURL string) (*pgx.ConnConfig, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("TEST_DATABASE_URL is required")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.Database = "postgres"
	return config, nil
}

func TestParseLivePostgresConfigRejectsEmptyURL(t *testing.T) {
	if _, err := parseLivePostgresConfig(""); err == nil {
		t.Fatal("empty TEST_DATABASE_URL accepted")
	}
}

func TestEnsureLiveCreatesOrReusesExactRoles(t *testing.T) {
	db := livePostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var before int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_catalog.pg_roles WHERE rolname IN ('aboutme_runtime_owner','aboutme_migrator','aboutme_app','aboutme_restore_verify','aboutme_lifecycle_command','aboutme_fencing_proof','aboutme_maintenance')`).Scan(&before); err != nil {
		t.Fatal("count fixed roles")
	}
	result, err := Ensure(ctx, db)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result.Created+result.Verified != 7 {
		t.Fatalf("result = %+v, want seven roles", result)
	}
	t.Logf("fixed roles existing=%d created=%d verified=%d", before, result.Created, result.Verified)
	reused, err := Ensure(ctx, db)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if reused != (Result{Verified: 7}) {
		t.Fatalf("second result = %+v, want seven verified", reused)
	}
}

func TestEnsureLiveSerializesOnCommonDatabase(t *testing.T) {
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
		t.Fatal("hold bootstrap lock")
	}
	done := make(chan error, 1)
	go func() { _, err := Ensure(ctx, db); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("Ensure returned before lock release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal("release bootstrap lock")
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

func TestOnlyMigratorCanSetRuntimeOwner(t *testing.T) {
	db := livePostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Ensure(ctx, db); err != nil {
		t.Fatalf("prepare roles: %v", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal("dedicated connection")
	}
	defer resetAndCloseConnection(t, conn)
	for _, spec := range fixedRoles {
		if _, err := conn.ExecContext(ctx, "SET SESSION AUTHORIZATION "+spec.name); err != nil {
			t.Fatalf("authorize %s: %v", spec.name, err)
		}
		var current string
		if err := conn.QueryRowContext(ctx, `SELECT current_user`).Scan(&current); err != nil {
			t.Fatalf("current user for %s", spec.name)
		}
		if current != spec.name {
			t.Fatalf("current_user = %s, want %s", current, spec.name)
		}
		if spec.name == "aboutme_migrator" {
			if _, err := conn.ExecContext(ctx, `SET ROLE aboutme_runtime_owner`); err != nil {
				t.Fatalf("migrator cannot set runtime owner: %v", err)
			}
			if _, err := conn.ExecContext(ctx, `RESET ROLE`); err != nil {
				t.Fatalf("reset migrator role: %v", err)
			}
		} else if spec.name != "aboutme_runtime_owner" {
			if _, err := conn.ExecContext(ctx, `SET ROLE aboutme_runtime_owner`); err == nil {
				t.Fatalf("%s unexpectedly set runtime owner", spec.name)
			}
		}
		resetCtx, resetCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, resetErr := conn.ExecContext(resetCtx, `RESET SESSION AUTHORIZATION`)
		resetCancel()
		if resetErr != nil {
			t.Fatalf("reset authorization after %s", spec.name)
		}
	}
}

func resetAndCloseConnection(t *testing.T, conn *sql.Conn) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(cleanupCtx, `RESET ROLE`); err != nil {
		t.Errorf("cleanup RESET ROLE: %v", err)
	}
	if _, err := conn.ExecContext(cleanupCtx, `RESET SESSION AUTHORIZATION`); err != nil {
		t.Errorf("cleanup RESET SESSION AUTHORIZATION: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("close dedicated connection: %v", err)
	}
}
