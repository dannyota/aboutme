package migrations

import "testing"

func TestMigrationIdentityConstructors(t *testing.T) {
	if got := DirectMigratorIdentity(); got.kind != migrationIdentityDirect {
		t.Fatalf("DirectMigratorIdentity kind=%d", got.kind)
	}
	if got := LocalAdminMigratorIdentity(); got.kind != migrationIdentityLocalAdmin {
		t.Fatalf("LocalAdminMigratorIdentity kind=%d", got.kind)
	}
	if (MigrationIdentity{}).valid() {
		t.Fatal("zero MigrationIdentity is valid")
	}
	if (MigrationIdentity{kind: 255}).valid() {
		t.Fatal("unknown MigrationIdentity is valid")
	}
}
