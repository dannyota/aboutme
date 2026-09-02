package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const validDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"

func TestParseConfigGuardsTheDatabase(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string // substring the error must contain; empty = must succeed
	}{
		{name: "seed ok", args: []string{"seed", "--database-url", validDSN}},
		{name: "cleanup ok", args: []string{"cleanup", "--database-url", validDSN}},
		{name: "missing subcommand", args: nil, want: "subcommand"},
		{name: "unknown subcommand", args: []string{"drop", "--database-url", validDSN}, want: "unknown subcommand"},
		{name: "missing url", args: []string{"seed"}, want: "--database-url is required"},
		{name: "test database refused", args: []string{"seed", "--database-url", "postgres://127.0.0.1/aboutme"}, want: "aboutme_dev"},
		{name: "remote host refused", args: []string{"seed", "--database-url", "postgres://db.example.com/aboutme_dev"}, want: "127.0.0.1"},
		{name: "mysql refused", args: []string{"seed", "--database-url", "mysql://127.0.0.1/aboutme_dev"}, want: "postgres"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, cfg, err := parseConfig(tt.args)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("parseConfig() error = %v", err)
				}
				if cmd != tt.args[0] || cfg.DatabaseURL != validDSN {
					t.Fatalf("cmd=%q dsn=%q", cmd, cfg.DatabaseURL)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseConfig() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestEmbeddedFixtureMatchesSchemaPackage(t *testing.T) {
	t.Parallel()
	upstream, err := os.ReadFile("../../../../packages/schema/fixtures/full.json")
	if err != nil {
		t.Fatalf("read schema fixture: %v", err)
	}
	if !bytes.Equal(upstream, fullFixture) {
		t.Fatal("cmd/dev-seed/testdata/full.json drifted from packages/schema/fixtures/full.json; copy it again")
	}
	if _, _, _, err := splitResumeDoc(fullFixture); err != nil {
		t.Fatalf("splitResumeDoc: %v", err)
	}
}

func TestSeedIdentitiesAreFixed(t *testing.T) {
	t.Parallel()
	if seedUser.ID.String() != "5d000000-0000-4000-8000-000000000001" ||
		seedResumeID.String() != "5d000000-0000-4000-8000-000000000002" ||
		seedUser.Email != "dev@aboutme.invalid" || seedUser.Password != "aboutme-dev-password-1" {
		t.Fatal("seed identities changed; the spec, runbook, and entry proof pin them")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestSeedIsIdempotentAndCleanupIsExact(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	t.Cleanup(func() { _ = runCleanupWithDB(ctx, db) })

	for i := 0; i < 2; i++ {
		if err := runSeedWithDB(ctx, db); err != nil {
			t.Fatalf("seed run %d: %v", i+1, err)
		}
	}
	if n := countRows(t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
	if n := countRows(t, db, `SELECT count(*) FROM password_credentials WHERE user_id = $1`, seedUser.ID); n != 1 {
		t.Fatalf("credentials = %d, want 1", n)
	}
	if n := countRows(t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND user_id = $2 AND live = false AND slug IS NULL AND revision = 1 AND schema_version = 2`, seedResumeID, seedUser.ID); n != 1 {
		t.Fatalf("seed resume rows = %d, want 1 private v2 resume at revision 1", n)
	}

	// An edited document survives a re-seed.
	if _, err := db.ExecContext(ctx, `UPDATE resumes SET title = 'edited', revision = 7 WHERE id = $1`, seedResumeID); err != nil {
		t.Fatal(err)
	}
	if err := runSeedWithDB(ctx, db); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if n := countRows(t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND title = 'edited' AND revision = 7`, seedResumeID); n != 1 {
		t.Fatal("re-seed overwrote an existing document")
	}

	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n := countRows(t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID) + countRows(t, db, `SELECT count(*) FROM resumes WHERE id = $1`, seedResumeID); n != 0 {
		t.Fatalf("rows after cleanup = %d, want 0", n)
	}
}

func TestSeedFailsWhenEmailBelongsToAnotherAccount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	other := "5d000000-0000-4000-8000-0000000000ff"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'Other')`, other, seedUser.Email); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, other) })

	err := runSeedWithDB(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "exists under a different id") {
		t.Fatalf("runSeedWithDB error = %v, want the different-id refusal", err)
	}
}
