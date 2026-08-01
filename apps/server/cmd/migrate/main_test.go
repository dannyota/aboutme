package main

import (
	"bytes"
	"strings"
	"testing"
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

	err := run(true, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want error for an unreachable database in -check mode")
	}
}
