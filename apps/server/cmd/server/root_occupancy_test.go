package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestParseCheckPublicRootCommand(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		nil,
		{"guide", "extra"},
		{"missing"},
		{"GUIDE"},
		{"a--b"},
	} {
		if _, err := parseCheckPublicRootCommand(args); err == nil {
			t.Fatalf("parseCheckPublicRootCommand(%q) accepted invalid arguments", args)
		}
	}
	root, err := parseCheckPublicRootCommand([]string{"guide"})
	if err != nil || root != "guide" {
		t.Fatalf("parseCheckPublicRootCommand() = %q, %v", root, err)
	}
}

func TestRootOccupancyExitCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		resume, tombstone bool
		want              int
	}{
		{false, false, rootOccupancyClearExitCode},
		{true, false, rootOccupancyResumeExitCode},
		{false, true, rootOccupancyTombstoneExitCode},
		{true, true, rootOccupancyBothExitCode},
	} {
		if got := rootOccupancyExitCode(rootOccupancy{ResumeOccupied: tc.resume, TombstoneOccupied: tc.tombstone}); got != tc.want {
			t.Fatalf("rootOccupancyExitCode(%t, %t) = %d, want %d", tc.resume, tc.tombstone, got, tc.want)
		}
	}
}

func TestExecuteCheckPublicRootConfigurationFailureIsFixedAndSafe(t *testing.T) {
	t.Parallel()
	const privateDSN = "postgres://private-user:private-password@/private-db?sslmode=%"
	var stdout bytes.Buffer
	err := executeCheckPublicRoot(context.Background(), "guide", func(name string) string {
		if name != "DATABASE_URL" {
			t.Fatalf("unexpected environment read %q", name)
		}
		return privateDSN
	}, &stdout)
	if err == nil {
		t.Fatal("executeCheckPublicRoot() error = nil")
	}
	if stdout.Len() != 0 || strings.Contains(err.Error(), privateDSN) {
		t.Fatalf("configuration failure leaked private input: stdout=%q error=%q", stdout.String(), err)
	}
	if err.Error() != "check public root database configuration is invalid" {
		t.Fatalf("error = %q", err)
	}
}

func TestCheckPublicRootOccupancy(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	for _, tc := range []struct {
		name, root        string
		resume, tombstone bool
		want              rootOccupancy
	}{
		{"clear", "guide", false, false, rootOccupancy{}},
		{"resume", "templates", true, false, rootOccupancy{ResumeOccupied: true}},
		{"tombstone", "privacy", false, true, rootOccupancy{TombstoneOccupied: true}},
		{"both", "register", true, true, rootOccupancy{ResumeOccupied: true, TombstoneOccupied: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.resume {
				seedRootOccupancyResume(t, pool, tc.root)
			}
			if tc.tombstone {
				seedRootOccupancyTombstone(t, pool, tc.root)
			}
			got, err := checkPublicRootOccupancy(t.Context(), pool, tc.root)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("occupancy = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExecuteCheckPublicRootOccupancy(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	for _, tc := range []struct {
		name, root        string
		resume, tombstone bool
		wantCode          int
	}{
		{"clear", "guide", false, false, rootOccupancyClearExitCode},
		{"resume", "templates", true, false, rootOccupancyResumeExitCode},
		{"tombstone", "privacy", false, true, rootOccupancyTombstoneExitCode},
		{"both", "register", true, true, rootOccupancyBothExitCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.resume {
				seedRootOccupancyResume(t, pool, tc.root)
			}
			if tc.tombstone {
				seedRootOccupancyTombstone(t, pool, tc.root)
			}
			var output bytes.Buffer
			err := executeCheckPublicRoot(t.Context(), tc.root, func(name string) string {
				if name != "DATABASE_URL" {
					t.Fatalf("unexpected environment read %q", name)
				}
				return dsn
			}, &output)
			assertRootOccupancyCommandResult(t, err, output.Bytes(), tc.resume, tc.tombstone, tc.wantCode)
		})
	}
}

func TestReadRootOccupancyUsesReadOnlyTransaction(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.BeginTx(t.Context(), rootOccupancyTxOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := readRootOccupancy(t.Context(), tx, "guide"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), `INSERT INTO slug_tombstones (slug) VALUES ($1)`, "guide")
	if err == nil {
		t.Fatal("read-only transaction accepted an insert")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("read-only insert error = %v, want SQLSTATE 25006", err)
	}
}

func seedRootOccupancyResume(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	userID := uuid.New()
	if _, err := pool.Exec(t.Context(), `INSERT INTO users (id, email, name) VALUES ($1, $2, 'Root Occupancy')`, userID, userID.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO resumes (user_id, title, slug, schema_version, personal_details, content, customization)
		VALUES ($1, 'Root Occupancy', $2, 1, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)`, userID, slug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete root occupancy user: %v", err)
		}
	})
}

func seedRootOccupancyTombstone(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `INSERT INTO slug_tombstones (slug) VALUES ($1)`, slug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM slug_tombstones WHERE slug = $1`, slug); err != nil {
			t.Errorf("delete tombstone: %v", err)
		}
	})
}

func assertRootOccupancyCommandResult(t *testing.T, err error, output []byte, resume, tombstone bool, wantCode int) {
	t.Helper()
	if wantCode == rootOccupancyClearExitCode {
		if err != nil {
			t.Fatalf("executeCheckPublicRoot() error = %v", err)
		}
	} else {
		var exit rootOccupancyExitError
		if !errors.As(err, &exit) || exit.code != wantCode {
			t.Fatalf("executeCheckPublicRoot() error = %v, want exit code %d", err, wantCode)
		}
	}
	var result map[string]bool
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode command output: %v", err)
	}
	if len(result) != 2 || result["resumeOccupied"] != resume || result["tombstoneOccupied"] != tombstone {
		t.Fatalf("command output = %s", output)
	}
}

func TestCheckPublicRootCommandRejectsAnUnreservedRoot(t *testing.T) {
	t.Parallel()
	if publicroots.Reserved("candidate") {
		t.Fatal("test root is unexpectedly reserved")
	}
	_, err := parseCheckPublicRootCommand([]string{"candidate"})
	if err == nil || !errors.Is(err, errCheckPublicRootUsage) {
		t.Fatalf("parseCheckPublicRootCommand() error = %v", err)
	}
}
