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

const unreachableDSN = "postgres://user:pass@127.0.0.1:1/aboutme?connect_timeout=1"

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

func TestMigrationCommandErrorFallsBackWithoutAPartialError(t *testing.T) {
	got := migrationCommandError(errors.New("postgres://secret@host/database SQLSTATE 08006")).Error()
	if got != "migration operation failed" {
		t.Fatalf("migrationCommandError() = %q, want the generic fallback", got)
	}
}
