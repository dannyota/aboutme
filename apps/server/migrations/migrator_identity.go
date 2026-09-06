package migrations

type migrationIdentityKind uint8

const (
	migrationIdentityInvalid migrationIdentityKind = iota
	migrationIdentityDirect
	migrationIdentityLocalAdmin
)

// MigrationIdentity selects one fixed, non-configurable migration identity.
type MigrationIdentity struct {
	kind migrationIdentityKind
}

// DirectMigratorIdentity requires an aboutme_migrator login session.
func DirectMigratorIdentity() MigrationIdentity {
	return MigrationIdentity{kind: migrationIdentityDirect}
}

// LocalAdminMigratorIdentity requires the local aboutme superuser and then
// assumes the fixed aboutme_migrator session identity.
func LocalAdminMigratorIdentity() MigrationIdentity {
	return MigrationIdentity{kind: migrationIdentityLocalAdmin}
}

func (i MigrationIdentity) valid() bool {
	return i.kind == migrationIdentityDirect || i.kind == migrationIdentityLocalAdmin
}
