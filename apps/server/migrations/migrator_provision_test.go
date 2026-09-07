package migrations

import (
	"context"
	"errors"
	"testing"
	"time"
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
