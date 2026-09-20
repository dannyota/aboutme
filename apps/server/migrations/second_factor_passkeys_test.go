package migrations_test

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func TestPasskeySecondFactorMigrationDefinesRequiredRelations(t *testing.T) {
	t.Parallel()

	contents, err := migrations.FS.ReadFile("00004_passkey_second_factor.sql")
	if err != nil {
		t.Fatalf("read passkey migration: %v", err)
	}

	for _, fragment := range []string{
		"ALTER TABLE users ADD COLUMN auth_epoch bigint NOT NULL DEFAULT 0",
		"CREATE TABLE second_factor_policies",
		"CREATE TABLE webauthn_credentials",
		"CREATE TABLE second_factor_recovery_codes",
		"CREATE TABLE pending_authentications",
		"CREATE TABLE webauthn_ceremonies",
		"CREATE TABLE authentication_security_events",
		"CREATE TRIGGER oauth_authorization_codes_fill_second_factor_bindings",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE",
	} {
		if !strings.Contains(string(contents), fragment) {
			t.Errorf("migration does not contain %q", fragment)
		}
	}
	if strings.Contains(string(contents), "totp") || strings.Contains(string(contents), "second_factor_recovery_codes_used_at") {
		t.Error("passkey migration contains a v0.4.3 or consumed-recovery field")
	}
}

func TestPasskeySecondFactorMigrationBindsAuthorizationCodesToTheirGrantOwner(t *testing.T) {
	t.Parallel()

	contents, err := migrations.FS.ReadFile("00004_passkey_second_factor.sql")
	if err != nil {
		t.Fatalf("read passkey migration: %v", err)
	}

	for _, fragment := range []string{
		"ADD CONSTRAINT oauth_grants_id_user_client_key UNIQUE (id, user_id, client_id)",
		"ADD CONSTRAINT oauth_authorization_codes_grant_id_user_client_fkey",
		"FOREIGN KEY (grant_id, user_id, client_id)",
		"REFERENCES oauth_grants (id, user_id, client_id) ON DELETE CASCADE",
	} {
		if !strings.Contains(string(contents), fragment) {
			t.Errorf("migration does not contain %q", fragment)
		}
	}
}

func TestPasskeySecondFactorMigrationAdmitsOnlyUserScopedSecurityMail(t *testing.T) {
	t.Parallel()

	contents, err := migrations.FS.ReadFile("00004_passkey_second_factor.sql")
	if err != nil {
		t.Fatalf("read passkey migration: %v", err)
	}

	for _, kind := range []string{
		"second_factor_enabled",
		"passkey_added",
		"passkey_removed",
		"second_factor_disabled",
		"recovery_codes_regenerated",
		"recovery_code_used",
		"second_factor_attempts_exhausted",
	} {
		if !strings.Contains(string(contents), kind) {
			t.Errorf("migration does not admit security mail kind %q", kind)
		}
	}
	for _, fragment := range []string{
		"DROP CONSTRAINT auth_email_jobs_kind_check",
		"DROP CONSTRAINT auth_email_jobs_scope_check",
		"DROP CONSTRAINT auth_email_jobs_token_digest_required_check",
		"registration_id IS NULL AND reset_token_id IS NULL AND user_id IS NOT NULL",
	} {
		if !strings.Contains(string(contents), fragment) {
			t.Errorf("migration does not contain %q", fragment)
		}
	}
}
