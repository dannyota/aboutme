package main

import (
	"strings"
	"testing"
)

const testNativeDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"

func TestParseConfigValidatesNativeDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "seed", args: []string{"seed", "--database-url", testNativeDSN}},
		{name: "cleanup", args: []string{"cleanup", "--database-url", testNativeDSN}},
		{name: "missing command", want: "subcommand"},
		{name: "unknown command", args: []string{"drop", "--database-url", testNativeDSN}, want: "unknown subcommand"},
		{name: "missing url", args: []string{"seed"}, want: "--database-url is required"},
		{name: "missing value", args: []string{"seed", "--database-url"}, want: "requires a value"},
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
				if cmd != tt.args[0] || cfg.DatabaseURL != testNativeDSN {
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
	if fixtureClientName != "aboutme MCP UAT" {
		t.Fatalf("fixture client name = %q", fixtureClientName)
	}
	if fixtureRedirectURI != "http://127.0.0.1:20090/callback" {
		t.Fatalf("fixture redirect URI = %q", fixtureRedirectURI)
	}
}
