// Proves the ownership and grant shape ADR 0038 requires: aboutme_migrator
// owns every object in public (including goose_db_version and the citext
// extension) and belongs to no other role, and aboutme_app holds exactly
// SELECT/INSERT/UPDATE/DELETE (no grant option, nothing else) on each of
// the twenty business tables, nothing at all on goose_db_version, and
// cannot create or alter schema objects. Every check is proven against the
// real ACL and catalog state a live goose-migrated database produces, not
// against the migration source text.
package migrations_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// businessTables is the exact table set 00001_baseline.sql's GRANT
// statement names.
var businessTables = []string{
	"users", "identities", "oauth_transactions", "sessions", "idempotency_records",
	"resumes", "slug_tombstones", "idempotency_usage", "media_deletion_jobs",
	"public_state", "password_credentials", "password_registrations",
	"password_reset_tokens", "auth_email_jobs", "oauth_clients",
	"oauth_authorization_codes", "oauth_grants", "oauth_tokens",
	"lifecycle_audit_events", "privacy_sweep_state",
}

func TestAppRoleHasExactTablePrivileges(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	for _, table := range businessTables {
		t.Run(table, func(t *testing.T) {
			rows, err := tx.Query(ctx, `
				SELECT a.privilege_type, a.is_grantable
				FROM pg_class c, aclexplode(c.relacl) a
				JOIN pg_roles r ON r.oid = a.grantee
				WHERE c.relname = $1 AND c.relnamespace = 'public'::regnamespace
				  AND r.rolname = 'aboutme_app'
			`, table)
			if err != nil {
				t.Fatalf("query grants for %s: %v", table, err)
			}
			defer rows.Close()
			got := map[string]bool{}
			for rows.Next() {
				var privilege string
				var grantable bool
				if err := rows.Scan(&privilege, &grantable); err != nil {
					t.Fatalf("scan grant row: %v", err)
				}
				if grantable {
					t.Errorf("%s privilege %s is grantable, want not grantable", table, privilege)
				}
				got[privilege] = true
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate grants: %v", err)
			}
			want := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
			if len(got) != len(want) {
				t.Fatalf("%s privileges = %v, want exactly %v", table, got, want)
			}
			for _, p := range want {
				if !got[p] {
					t.Errorf("%s missing privilege %s", table, p)
				}
			}
		})
	}
}

func TestAppRoleHasNothingOnGooseVersionTable(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_class c, aclexplode(c.relacl) a
		JOIN pg_roles r ON r.oid = a.grantee
		WHERE c.relname = 'goose_db_version' AND c.relnamespace = 'public'::regnamespace
		  AND r.rolname = 'aboutme_app'
	`).Scan(&count); err != nil {
		t.Fatalf("query goose_db_version grants: %v", err)
	}
	if count != 0 {
		t.Errorf("aboutme_app grants on goose_db_version = %d, want 0", count)
	}
}

func TestAppRoleCannotCreateAlterOrReadGooseVersion(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	if _, err := tx.Exec(ctx, `SET ROLE aboutme_app`); err != nil {
		t.Fatalf("set role aboutme_app: %v", err)
	}

	if err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `CREATE TABLE aboutme_app_probe (id int)`)
		return err
	}); err == nil {
		t.Error("aboutme_app created a table, want permission denied")
	}
	if err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `ALTER TABLE users ADD COLUMN probe text`)
		return err
	}); err == nil {
		t.Error("aboutme_app altered users, want permission denied")
	}
	if err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `SELECT * FROM goose_db_version`)
		return err
	}); err == nil {
		t.Error("aboutme_app read goose_db_version, want permission denied")
	}
}

func TestMigratorOwnsEveryObjectInPublicIncludingGooseVersionAndCitext(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var relationMismatches int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_class c
		JOIN pg_roles r ON r.oid = c.relowner
		WHERE c.relnamespace = 'public'::regnamespace
		  AND c.relkind IN ('r', 'i', 'S')
		  AND r.rolname <> 'aboutme_migrator'
	`).Scan(&relationMismatches); err != nil {
		t.Fatalf("query object owners: %v", err)
	}
	if relationMismatches != 0 {
		t.Errorf("relations (tables/indexes/sequences) in public not owned by aboutme_migrator = %d, want 0", relationMismatches)
	}

	// Excludes a trusted extension's own member functions (pg_depend
	// deptype 'e'): PostgreSQL always attributes a trusted extension's
	// C-language functions to the cluster's bootstrap role, never to the
	// non-superuser role that ran CREATE EXTENSION, regardless of grants.
	// The extension's own catalog entry still records aboutme_migrator as
	// owner (checked below), which is what governs who may alter or drop
	// it.
	var funcMismatches int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.pronamespace = 'public'::regnamespace
		  AND r.rolname <> 'aboutme_migrator'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e'
		  )
	`).Scan(&funcMismatches); err != nil {
		t.Fatalf("query function owners: %v", err)
	}
	if funcMismatches != 0 {
		t.Errorf("non-extension functions in public not owned by aboutme_migrator = %d, want 0", funcMismatches)
	}

	var extensionOwner string
	if err := tx.QueryRow(ctx, `
		SELECT r.rolname FROM pg_extension e JOIN pg_roles r ON r.oid = e.extowner WHERE e.extname = 'citext'
	`).Scan(&extensionOwner); err != nil {
		t.Fatalf("query citext owner: %v", err)
	}
	if extensionOwner != "aboutme_migrator" {
		t.Errorf("citext extension owner = %q, want aboutme_migrator", extensionOwner)
	}
}

func TestMigratorHasNoRoleMembership(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_auth_members m
		JOIN pg_roles r ON r.oid = m.member
		WHERE r.rolname = 'aboutme_migrator'
	`).Scan(&count); err != nil {
		t.Fatalf("query migrator memberships: %v", err)
	}
	if count != 0 {
		t.Errorf("aboutme_migrator role memberships = %d, want 0", count)
	}
}

// TestAppRoleSmokeFlowThroughRealStoreQueries proves aboutme_app's exact
// grant set is enough for a real write path, not merely present in the
// catalog: insert a user and a resume (exercising the resume-cap and
// revision-notification triggers under the app role's own privileges,
// since neither function is SECURITY DEFINER), update, read back, then
// delete the user and confirm the resume cascades away.
func TestAppRoleSmokeFlowThroughRealStoreQueries(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	if _, err := tx.Exec(ctx, `SET ROLE aboutme_app`); err != nil {
		t.Fatalf("set role aboutme_app: %v", err)
	}

	userID := createTestUser(ctx, t, tx)
	row := defaultResumeRow(userID, "grants-smoke")
	resumeID, err := insertResumeReturningID(ctx, tx, row)
	if err != nil {
		t.Fatalf("aboutme_app insert resume: %v", err)
	}
	if err := updateResumeUserID(ctx, tx, resumeID, userID); err != nil {
		t.Fatalf("aboutme_app no-op update resume: %v", err)
	}
	var title string
	if err := tx.QueryRow(ctx, `SELECT title FROM resumes WHERE id = $1`, resumeID).Scan(&title); err != nil {
		t.Fatalf("aboutme_app select resume: %v", err)
	}
	if title != row.title {
		t.Errorf("resume title = %q, want %q", title, row.title)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("aboutme_app delete user: %v", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM resumes WHERE id = $1`, resumeID).Scan(&remaining); err != nil {
		t.Fatalf("aboutme_app count resumes: %v", err)
	}
	if remaining != 0 {
		t.Errorf("resumes remaining after cascaded user delete = %d, want 0", remaining)
	}
}
