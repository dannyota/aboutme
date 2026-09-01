package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const testNativeDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"

func testClientName() string {
	return fixtureClientNamePrefix + uuid.NewString()
}

func TestParseConfigValidatesNativeDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "seed", args: []string{"seed", "--database-url", testNativeDSN, "--client-name", testClientName()}},
		{name: "cleanup", args: []string{"cleanup", "--database-url", testNativeDSN, "--client-name", testClientName()}},
		{name: "missing command", want: "subcommand"},
		{name: "unknown command", args: []string{"drop", "--database-url", testNativeDSN}, want: "unknown subcommand"},
		{name: "missing url", args: []string{"seed"}, want: "--database-url is required"},
		{name: "missing url value", args: []string{"seed", "--database-url"}, want: "requires a value"},
		{name: "missing client name", args: []string{"seed", "--database-url", testNativeDSN}, want: "--client-name is required"},
		{name: "missing client name value", args: []string{"seed", "--database-url", testNativeDSN, "--client-name"}, want: "requires a value"},
		{name: "legacy fixed client name", args: []string{"seed", "--database-url", testNativeDSN, "--client-name", "aboutme MCP UAT"}, want: "UUIDv4"},
		{name: "non-v4 client name", args: []string{"seed", "--database-url", testNativeDSN, "--client-name", fixtureClientNamePrefix + "53000000-0000-1000-8000-000000000001"}, want: "UUIDv4"},
		{name: "uppercase client name", args: []string{"seed", "--database-url", testNativeDSN, "--client-name", fixtureClientNamePrefix + "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"}, want: "UUIDv4"},
		{name: "unknown argument", args: []string{"seed", "--other"}, want: "unknown argument"},
		{name: "wrong scheme", args: []string{"seed", "--database-url", "https://127.0.0.1/aboutme_dev"}, want: "postgres"},
		{name: "localhost alias", args: []string{"seed", "--database-url", "postgres://localhost/aboutme_dev"}, want: "127.0.0.1"},
		{name: "remote host", args: []string{"seed", "--database-url", "postgres://db.example.invalid/aboutme_dev"}, want: "127.0.0.1"},
		{name: "wrong database", args: []string{"seed", "--database-url", "postgres://127.0.0.1/aboutme"}, want: "aboutme_dev"},
		{name: "empty database", args: []string{"seed", "--database-url", "postgres://127.0.0.1"}, want: "name a database"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, cfg, err := parseConfig(tt.args)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("parseConfig() error = %v", err)
				}
				if cmd != tt.args[0] || cfg.DatabaseURL != testNativeDSN || cfg.ClientName == "" {
					t.Fatalf("parseConfig() = %q, %#v", cmd, cfg)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseConfig() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestFixtureIdentityAndClientMarkersAreClosed(t *testing.T) {
	t.Parallel()

	if fixtureUser.ID.String() != "53000000-0000-4000-8000-000000000001" {
		t.Fatalf("fixture user ID = %s", fixtureUser.ID)
	}
	if fixtureUser.IdentityID.String() != "53000000-0000-4000-8000-000000000011" {
		t.Fatalf("fixture identity ID = %s", fixtureUser.IdentityID)
	}
	if fixtureUser.Email != "bob@example.invalid" || fixtureUser.Name != "Bob Local" {
		t.Fatalf("fixture user = %#v", fixtureUser)
	}
	if fixtureUser.Provider != "google" || fixtureUser.ProviderUserID != "uat-google-003" {
		t.Fatalf("fixture provider = %q/%q", fixtureUser.Provider, fixtureUser.ProviderUserID)
	}
	if fixtureClientNamePrefix != "aboutme MCP UAT " {
		t.Fatalf("fixture client name prefix = %q", fixtureClientNamePrefix)
	}
	if fixtureRedirectURI != "http://127.0.0.1:20090/callback" {
		t.Fatalf("fixture redirect URI = %q", fixtureRedirectURI)
	}
}

func TestCleanFixtureLeavesAnotherRunClient(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	targetName := testClientName()
	otherName := testClientName()
	for otherName == targetName {
		otherName = testClientName()
	}
	var targetID, otherID uuid.UUID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := db.ExecContext(cleanupCtx,
			`DELETE FROM oauth_clients WHERE id IN ($1, $2)`, targetID, otherID,
		); cleanupErr != nil {
			t.Errorf("clean test OAuth clients: %v", cleanupErr)
		}
	})
	for name, id := range map[string]*uuid.UUID{
		targetName: &targetID,
		otherName:  &otherID,
	} {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO oauth_clients (client_name, redirect_uris, created_at, last_used_at)
			VALUES ($1, $2::jsonb, now(), now()) RETURNING id
		`, name, fixtureRedirectsJSON).Scan(id); err != nil {
			t.Fatalf("insert OAuth client %q: %v", name, err)
		}
	}
	if err := cleanFixture(ctx, db, targetName); err != nil {
		t.Fatalf("cleanFixture() error = %v", err)
	}
	for name, want := range map[string]int64{targetName: 0, otherName: 1} {
		var got int64
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM oauth_clients WHERE client_name = $1`, name,
		).Scan(&got); err != nil {
			t.Fatalf("count OAuth client %q: %v", name, err)
		}
		if got != want {
			t.Errorf("OAuth client %q count = %d, want %d", name, got, want)
		}
	}
}
