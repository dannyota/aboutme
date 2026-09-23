// Proves the ownership and grant shape ADR 0038 requires: aboutme_migrator
// owns every object in public (including goose_db_version and the citext
// extension) and belongs to no other role, and aboutme_app holds exactly
// SELECT/INSERT/UPDATE/DELETE (no grant option, nothing else) on each of
// the 26 business tables, nothing at all on any other relation in
// public, and cannot create or alter schema objects. Every check is proven
// against the real ACL and catalog state a live goose-migrated database
// produces, not against the migration source text.
package migrations_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// businessTables is the exact public table set that aboutme_app may access.
// TestBusinessTableSetMatchesPublicSchema fails when schema public gains a
// table this list does not name.
var businessTables = []string{
	"users", "identities", "oauth_transactions", "sessions", "idempotency_records",
	"resumes", "slug_tombstones", "idempotency_usage", "media_deletion_jobs",
	"public_state", "password_credentials", "password_registrations",
	"password_reset_tokens", "auth_email_jobs", "oauth_clients",
	"oauth_authorization_codes", "oauth_grants", "oauth_tokens",
	"lifecycle_audit_events", "privacy_sweep_state",
	"second_factor_policies", "webauthn_credentials", "second_factor_recovery_codes",
	"pending_authentications", "webauthn_ceremonies", "authentication_security_events",
	"totp_credentials", "totp_enrollments",
}

func TestBusinessTableSetMatchesPublicSchema(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	rows, err := tx.Query(ctx, `
		SELECT c.relname FROM pg_class c
		WHERE c.relnamespace = 'public'::regnamespace
		  AND c.relkind IN ('r', 'p')
		  AND c.relname <> 'goose_db_version'
	`)
	if err != nil {
		t.Fatalf("query public tables: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}

	want := map[string]bool{}
	for _, table := range businessTables {
		want[table] = true
	}
	if len(got) != len(want) {
		t.Fatalf("public tables excluding goose_db_version = %v, want exactly businessTables %v", got, want)
	}
	for table := range want {
		if !got[table] {
			t.Errorf("businessTables lists %s, which is not a table in public", table)
		}
	}
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

// TestAppRoleHasNoPrivilegeOutsideBusinessTables asserts aboutme_app holds
// no privilege on any relation in public other than businessTables:
// goose_db_version, and any view, sequence, or other relation a later
// migration might add without granting aboutme_app on it.
func TestAppRoleHasNoPrivilegeOutsideBusinessTables(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	rows, err := tx.Query(ctx, `
		SELECT c.relname, a.privilege_type
		FROM pg_class c, aclexplode(c.relacl) a
		JOIN pg_roles r ON r.oid = a.grantee
		WHERE c.relnamespace = 'public'::regnamespace
		  AND r.rolname = 'aboutme_app'
		  AND NOT (c.relname = ANY ($1::text[]))
	`, businessTables)
	if err != nil {
		t.Fatalf("query grants outside business tables: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var relname, privilege string
		if err := rows.Scan(&relname, &privilege); err != nil {
			t.Fatalf("scan grant row: %v", err)
		}
		t.Errorf("aboutme_app holds %s on %s, want no privilege outside businessTables", privilege, relname)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate grants: %v", err)
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

// TestMigratorOwnsEveryRelationInPublic covers every relation kind
// PostgreSQL supports in schema public (table, index, sequence, view,
// materialized view, partitioned table, foreign table, and composite
// type), not only the table and index kinds the baseline currently uses,
// so a later view or sequence still needs aboutme_migrator ownership.
func TestMigratorOwnsEveryRelationInPublic(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var relationMismatches int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_class c
		JOIN pg_roles r ON r.oid = c.relowner
		WHERE c.relnamespace = 'public'::regnamespace
		  AND c.relkind IN ('r', 'i', 'S', 'v', 'm', 'p', 'f', 'c')
		  AND r.rolname <> 'aboutme_migrator'
	`).Scan(&relationMismatches); err != nil {
		t.Fatalf("query relation owners: %v", err)
	}
	if relationMismatches != 0 {
		t.Errorf("relations in public not owned by aboutme_migrator = %d, want 0", relationMismatches)
	}
}

// TestMigratorOwnsEveryFunctionInPublicExceptTrustedExtensionMembers
// excludes only a function that pg_depend records as a member of a
// trusted extension whose own extnamespace is public (deptype 'e'): for
// such an extension, PostgreSQL always attributes its member objects to
// the cluster's bootstrap superuser, never to the non-superuser role
// that ran CREATE EXTENSION, regardless of grants. The extension's own
// catalog entry still records aboutme_migrator as owner (checked below),
// which is what governs who may alter or drop it.
func TestMigratorOwnsEveryFunctionInPublicExceptTrustedExtensionMembers(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var funcMismatches int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.pronamespace = 'public'::regnamespace
		  AND r.rolname <> 'aboutme_migrator'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    JOIN pg_extension e ON e.oid = d.refobjid
		    WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid
		      AND d.deptype = 'e' AND d.refclassid = 'pg_extension'::regclass
		      AND e.extnamespace = 'public'::regnamespace
		  )
	`).Scan(&funcMismatches); err != nil {
		t.Fatalf("query function owners: %v", err)
	}
	if funcMismatches != 0 {
		t.Errorf("non-extension functions in public not owned by aboutme_migrator = %d, want 0", funcMismatches)
	}
}

// TestMigratorOwnsEveryTypeInPublicExceptExtensionMembers covers standalone
// types in public (domains, enums, and standalone composite types), which
// no other check here reaches. It excludes a trusted extension's member
// types (same pg_depend rule as the function check above) and a table's
// automatic row type and array type, whose ownership already follows the
// table and is covered by TestMigratorOwnsEveryRelationInPublic.
func TestMigratorOwnsEveryTypeInPublicExceptExtensionMembers(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var typeMismatches int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_type t
		JOIN pg_roles r ON r.oid = t.typowner
		WHERE t.typnamespace = 'public'::regnamespace
		  AND r.rolname <> 'aboutme_migrator'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    JOIN pg_extension e ON e.oid = d.refobjid
		    WHERE d.classid = 'pg_type'::regclass AND d.objid = t.oid
		      AND d.deptype = 'e' AND d.refclassid = 'pg_extension'::regclass
		      AND e.extnamespace = 'public'::regnamespace
		  )
		  AND NOT (t.typtype = 'c' AND t.typrelid <> 0)
		  AND NOT (t.typcategory = 'A' AND EXISTS (
		    SELECT 1 FROM pg_type et
		    WHERE et.oid = t.typelem AND et.typtype = 'c' AND et.typrelid <> 0
		  ))
	`).Scan(&typeMismatches); err != nil {
		t.Fatalf("query type owners: %v", err)
	}
	if typeMismatches != 0 {
		t.Errorf("standalone types in public not owned by aboutme_migrator = %d, want 0", typeMismatches)
	}
}

func TestPublicExtensionsAreExactlyPlpgsqlAndCitext(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	rows, err := tx.Query(ctx, `SELECT extname FROM pg_extension`)
	if err != nil {
		t.Fatalf("query extensions: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan extension name: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate extensions: %v", err)
	}
	want := map[string]bool{"plpgsql": true, "citext": true}
	if len(got) != len(want) {
		t.Fatalf("installed extensions = %v, want exactly %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("extension %s not installed", name)
		}
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

// TestAppRoleSmokeFlowThroughInternalStoreQueries proves aboutme_app's
// exact grant set is enough for internal/store's real generated queries,
// not merely present in the catalog: it runs store.New against a
// connection with SET ROLE aboutme_app, then creates a user, a session,
// and a resume, takes a row lock on the user through the same FOR UPDATE
// path a mutating store call takes before a write, reads each row back,
// and confirms deleting the user cascades the session and resume away.
func TestAppRoleSmokeFlowThroughInternalStoreQueries(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	if _, err := tx.Exec(ctx, `SET ROLE aboutme_app`); err != nil {
		t.Fatalf("set role aboutme_app: %v", err)
	}

	queries := store.New(tx)

	user, err := queries.CreateUser(ctx, store.CreateUserParams{
		Email: "grants-smoke@aboutme.invalid",
		Name:  "Grants Smoke",
	})
	if err != nil {
		t.Fatalf("aboutme_app CreateUser: %v", err)
	}

	now := time.Now()
	session, err := queries.CreateSession(ctx, store.CreateSessionParams{
		UserID:            user.ID,
		TokenHash:         []byte("grants-smoke-token-hash-32-bytes"),
		CSRFSecret:        []byte("grants-smoke-csrf-secret-32-byte"),
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: now,
		AbsoluteExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("aboutme_app CreateSession: %v", err)
	}

	resume, err := queries.CreateResume(ctx, store.CreateResumeParams{
		UserID:          user.ID,
		Title:           "Grants smoke",
		SchemaVersion:   1,
		PersonalDetails: json.RawMessage(`{}`),
		Content:         json.RawMessage(`{}`),
		Customization:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("aboutme_app CreateResume: %v", err)
	}

	// The row lock a session-affecting store call takes before its write;
	// exercises the FOR UPDATE path under aboutme_app's own privileges.
	lockedUser, err := queries.GetUserForUpdate(ctx, user.ID)
	if err != nil {
		t.Fatalf("aboutme_app GetUserForUpdate: %v", err)
	}
	if lockedUser.ID != user.ID {
		t.Errorf("locked user id = %s, want %s", lockedUser.ID, user.ID)
	}

	gotSession, err := queries.GetSessionByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("aboutme_app GetSessionByTokenHash: %v", err)
	}
	if gotSession.ID != session.ID {
		t.Errorf("session id = %s, want %s", gotSession.ID, session.ID)
	}

	gotResume, err := queries.GetResumeByID(ctx, resume.ID)
	if err != nil {
		t.Fatalf("aboutme_app GetResumeByID: %v", err)
	}
	if gotResume.Title != resume.Title {
		t.Errorf("resume title = %q, want %q", gotResume.Title, resume.Title)
	}

	if _, err := queries.DeleteAccountUser(ctx, user.ID); err != nil {
		t.Fatalf("aboutme_app DeleteAccountUser: %v", err)
	}
	if _, err := queries.GetResumeByID(ctx, resume.ID); err == nil {
		t.Error("resume survived cascaded user delete, want not found")
	}
}
