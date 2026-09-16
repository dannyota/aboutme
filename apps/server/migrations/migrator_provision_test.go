package migrations

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestProvisionDatabaseRejectsDriftBeforeMutation(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `GRANT USAGE ON SCHEMA public TO aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionDatabase(ctx, db); !errors.Is(err, ErrMigrationProvisioningDrift) {
		t.Fatalf("ProvisionDatabase error=%v", err)
	}
	var explicitCreate bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) x WHERE n.oid='public'::regnamespace AND x.grantee='aboutme_migrator'::regrole AND x.privilege_type='CREATE')`).Scan(&explicitCreate); err != nil {
		t.Fatal(err)
	}
	if explicitCreate {
		t.Fatal("failed provisioning mutated migrator schema grants")
	}
}

func TestProvisionDatabaseFoundationIsValidationOnly(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatalf("post-foundation validation: %v", err)
	}
	var generation int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != 1 {
		t.Fatalf("validation-only provision changed generation to %d", generation)
	}
}

func TestLocalMigrationDatabasePatternClasses(t *testing.T) {
	for _, test := range []struct {
		name  string
		match bool
	}{
		{"aboutme", true},
		{"aboutme_dev", true},
		{"aboutme_migrate_test_1788717868022123841_1", true},
		{"aboutme_migrate_cmd_test_1788717868022123841_1", true},
		{"aboutme_migrate_template_20_0123456789abcdef", true},
		{"aboutme_migrate_template_20_0123456789ABCDEF", false},
		{"aboutme_migrate_template_20_0123456789abcde", false},
		{"aboutme_migrate_template__0123456789abcdef", false},
		{"aboutme_prod", false},
		{"postgres", false},
		{"aboutme_migrate_template_20_0123456789abcdef; DROP DATABASE x", false},
	} {
		if got := localMigrationDatabasePattern.MatchString(test.name); got != test.match {
			t.Errorf("%q match=%t want=%t", test.name, got, test.match)
		}
	}
}

// TestProvisionDatabaseAcceptsNonSuperuserOwner reproduces the hosted chain:
// a non-superuser database owner provisions, then the real migrator applies
// every migration with the direct identity.
func TestProvisionDatabaseAcceptsNonSuperuserOwner(t *testing.T) {
	base := compositionBaseDSN(t)
	admin := compositionAdmin(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	name := compositionDatabaseNameFor(t)
	owner := "aboutme_prov_owner_" + name[len("aboutme_migrate_test_"):]
	const password = "local-provision-owner-test"
	if _, err := admin.ExecContext(ctx, `CREATE ROLE `+owner+` LOGIN NOSUPERUSER CREATEROLE PASSWORD '`+password+`'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, err := admin.ExecContext(cleanup, `DROP ROLE IF EXISTS `+owner); err != nil {
			t.Errorf("drop owner role: %v", err)
		}
	})
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name+` OWNER `+owner); err != nil {
		t.Fatal(err)
	}
	openCompositionDatabase(t, admin, base, name)

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	u.User = url.UserPassword(owner, password)
	ownerDB, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestDB(t, ownerDB)

	if err = provisionDatabase(ctx, ownerDB, "aboutme"); err == nil {
		t.Fatal("provisioning accepted a session that is not the named owner")
	}
	if err = provisionDatabase(ctx, ownerDB, owner); err != nil {
		t.Fatalf("non-superuser owner provisioning: %v", err)
	}
	var superuser string
	if err = ownerDB.QueryRowContext(ctx, `SELECT current_setting('is_superuser')`).Scan(&superuser); err != nil {
		t.Fatal(err)
	}
	if superuser != "off" {
		t.Fatalf("owner is_superuser=%q, want off", superuser)
	}

	adminURL, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = adminURL.User
	config, err := pgx.ParseConfig(u.String())
	if err != nil {
		t.Fatal(err)
	}
	migratorDB := stdlib.OpenDB(*config, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, execErr := conn.Exec(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`)
		return execErr
	}))
	defer closeTestDB(t, migratorDB)
	if _, err = Apply(ctx, migratorDB, DirectMigratorIdentity()); err != nil {
		t.Fatalf("direct migrator apply after owner provisioning: %v", err)
	}
	var head int64
	if err = migratorDB.QueryRowContext(ctx, `SELECT max(version_id) FROM public.goose_db_version WHERE is_applied`).Scan(&head); err != nil {
		t.Fatal(err)
	}
	sources, err := migrationSourcesFromFS(FS)
	if err != nil {
		t.Fatal(err)
	}
	if want := sources[len(sources)-1].Version; head != want {
		t.Fatalf("migration head %d, want %d", head, want)
	}
}

func closeTestDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("close database: %v", err)
	}
}
