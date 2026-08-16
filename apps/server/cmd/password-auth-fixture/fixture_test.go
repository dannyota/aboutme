package main

import (
	"strings"
	"testing"
)

const validDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"

func TestParseConfigValidatesDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string // substring the error must contain; empty = must succeed
	}{
		{name: "seed ok", args: []string{"seed", "--database-url", validDSN}},
		{name: "cleanup ok", args: []string{"cleanup", "--database-url", validDSN}},
		{name: "missing subcommand", args: nil, want: "subcommand"},
		{name: "unknown subcommand", args: []string{"drop", "--database-url", validDSN}, want: "unknown subcommand"},
		{name: "missing database url", args: []string{"seed"}, want: "--database-url is required"},
		{name: "database url without value", args: []string{"seed", "--database-url"}, want: "--database-url requires a value"},
		{name: "unknown arg", args: []string{"seed", "--extra"}, want: "unknown argument"},
		{name: "mysql scheme", args: []string{"seed", "--database-url", "mysql://127.0.0.1/aboutme_dev"}, want: "postgres"},
		{name: "non-loopback host", args: []string{"seed", "--database-url", "postgres://db.example.com/aboutme_dev"}, want: "127.0.0.1"},
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
				if cmd != tt.args[0] {
					t.Fatalf("cmd = %q, want %q", cmd, tt.args[0])
				}
				if cfg.DatabaseURL != validDSN {
					t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, validDSN)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseConfig() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestFixtureAccountsAreDeterministic(t *testing.T) {
	t.Parallel()

	if len(fixtureAccounts) != 3 {
		t.Fatalf("len(fixtureAccounts) = %d, want 3", len(fixtureAccounts))
	}

	seenIDs := make(map[string]bool, len(fixtureAccounts))
	seenEmails := make(map[string]bool, len(fixtureAccounts))
	for _, acct := range fixtureAccounts {
		if seenIDs[acct.ID.String()] {
			t.Fatalf("duplicate account id %s", acct.ID)
		}
		seenIDs[acct.ID.String()] = true
		if seenEmails[acct.Email] {
			t.Fatalf("duplicate account email %s", acct.Email)
		}
		seenEmails[acct.Email] = true

		// Canonical, local-only account emails: lowercase, ASCII, reserved
		// .invalid domain, exactly one @.
		if acct.Email != strings.ToLower(acct.Email) {
			t.Errorf("email %q is not lowercase", acct.Email)
		}
		if !strings.HasSuffix(acct.Email, "@example.invalid") {
			t.Errorf("email %q does not use the reserved .invalid domain", acct.Email)
		}
		if strings.Count(acct.Email, "@") != 1 {
			t.Errorf("email %q does not have exactly one @", acct.Email)
		}
		if acct.Name == "" {
			t.Errorf("account %s has an empty name", acct.ID)
		}
		if acct.Provider != nil {
			if acct.Provider.Provider != "google" {
				t.Errorf("provider = %q, want google", acct.Provider.Provider)
			}
			if !strings.HasPrefix(acct.Provider.ProviderUserID, "uat-google-") {
				t.Errorf("provider subject %q is not a local mock subject", acct.Provider.ProviderUserID)
			}
			if acct.IdentityID == acct.ID {
				t.Errorf("identity id must differ from user id for %s", acct.Email)
			}
		}
		if acct.Provider == nil && acct.IdentityID != uuidZero() {
			t.Errorf("account %s has an identity id but no provider", acct.Email)
		}
	}

	// Exactly one provider-only account and two password accounts.
	providerOnly := 0
	passwordAccounts := 0
	for _, acct := range fixtureAccounts {
		if acct.Provider != nil {
			providerOnly++
		}
		if acct.Password != "" {
			passwordAccounts++
		}
	}
	if providerOnly != 2 {
		t.Errorf("provider-linked accounts = %d, want 2", providerOnly)
	}
	if passwordAccounts != 2 {
		t.Errorf("password accounts = %d, want 2", passwordAccounts)
	}
}

func uuidZero() [16]byte {
	return [16]byte{}
}
