package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

// unreachableDSN points at a closed local port so connection attempts fail
// fast (connection refused) without needing a real Postgres instance —
// same technique internal/store's unit tests use.
const unreachableDSN = "postgres://user:pass@127.0.0.1:1/aboutme?connect_timeout=1"

// These tests use t.Setenv, which the testing package forbids combining
// with t.Parallel (it mutates process-global state), so they run
// sequentially — each is still fast, since none reaches a real dial.

func TestRun_MissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ENV", "dev")

	err := run(false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want error for missing DATABASE_URL")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("run() error = %q, want it to mention DATABASE_URL", err)
	}
}

func TestRun_UnreachableDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", unreachableDSN)
	t.Setenv("ENV", "dev")
	t.Setenv("PUBLIC_ORIGIN", "https://aboutme.vn")

	err := run(false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want error for an unreachable database")
	}
	if !strings.Contains(err.Error(), "ping database") {
		t.Errorf("run() error = %q, want a ping-database error", err)
	}
}

func TestRun_UnreachableDatabase_CheckMode(t *testing.T) {
	t.Setenv("DATABASE_URL", unreachableDSN)
	t.Setenv("ENV", "dev")
	t.Setenv("PUBLIC_ORIGIN", "https://aboutme.vn")

	err := run(true, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want error for an unreachable database in -check mode")
	}
}

// TestMigrationCommandErrorReportsVersionAndSQLSTATEWithoutDetail proves
// migrationCommandError names the failing migration's version and
// Postgres SQLSTATE while keeping every other error detail — query text,
// data values, the DSN — out of the command's output.
func TestMigrationCommandErrorReportsVersionAndSQLSTATEWithoutDetail(t *testing.T) {
	sensitive := "dial tcp postgres://admin:secret@private-host:5432/db SQLSTATE 08006"
	partial := &goose.PartialError{
		Failed: &goose.MigrationResult{Source: &goose.Source{Version: 7}},
		Err:    fmt.Errorf("%s: %w", sensitive, &pgconn.PgError{Code: "23514", Message: "resumes_user_cap_exceeded"}),
	}

	got := migrationCommandError(partial).Error()
	want := "migration 7 failed: SQLSTATE 23514"
	if got != want {
		t.Fatalf("migrationCommandError() = %q, want %q", got, want)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "private-host") || strings.Contains(got, "resumes_user_cap_exceeded") {
		t.Fatalf("migrationCommandError() leaked detail: %q", got)
	}
}

// TestMigrationCommandErrorFallsBackWithoutAPartialError proves a
// non-migration failure (e.g. Status's own catalog-read error) still gets
// a generic, detail-free message.
func TestMigrationCommandErrorFallsBackWithoutAPartialError(t *testing.T) {
	got := migrationCommandError(errors.New("postgres://secret@host/database SQLSTATE 08006")).Error()
	if got != "migration operation failed" {
		t.Fatalf("migrationCommandError() = %q, want the generic fallback", got)
	}
}
