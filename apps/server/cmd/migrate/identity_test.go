package main

import (
	"testing"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func TestMigrationIdentityEnvironmentIsFixed(t *testing.T) {
	t.Setenv("MIGRATION_IDENTITY", "direct")
	if got, err := migrationIdentityFromEnvironment(); err != nil || got != migrations.DirectMigratorIdentity() {
		t.Fatalf("direct identity=%v error=%v", got, err)
	}
	t.Setenv("MIGRATION_IDENTITY", "local-aboutme")
	if got, err := migrationIdentityFromEnvironment(); err != nil || got != migrations.LocalAdminMigratorIdentity() {
		t.Fatalf("local identity=%v error=%v", got, err)
	}
	for _, value := range []string{"", "aboutme", "aboutme_migrator", "admin"} {
		t.Setenv("MIGRATION_IDENTITY", value)
		if _, err := migrationIdentityFromEnvironment(); err == nil {
			t.Fatalf("identity %q accepted", value)
		}
	}
}
