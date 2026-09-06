package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestManifestDigestAndExactVersion13Inventory(t *testing.T) {
	objects := fixedVersion13Manifest()
	if len(objects) != 110 {
		t.Fatalf("manifest rows=%d, want 110", len(objects))
	}
	if got := version13ManifestDigest(); got != version13ManifestSHA256 {
		t.Fatalf("manifest digest=%s, want %s", got, version13ManifestSHA256)
	}
	counts := map[string]int{}
	last := ""
	for _, object := range objects {
		line := manifestObjectKey(object.kind, object.schema, object.identity)
		if line <= last {
			t.Fatalf("manifest is not strictly sorted at %q after %q", line, last)
		}
		last = line
		counts[object.kind]++
		if object.schema != "public" {
			t.Fatalf("manifest schema=%q", object.schema)
		}
	}
	want := map[string]int{"table": 21, "index": 65, "type": 21, "function": 2, "sequence": 1}
	if fmt.Sprint(counts) != fmt.Sprint(want) {
		t.Fatalf("manifest kind counts=%v, want %v", counts, want)
	}
	statements := fixedVersion13AlterStatements()
	if len(statements) != 23 {
		t.Fatalf("ALTER statements=%d, want 23", len(statements))
	}
	joined := strings.Join(statements, "\n")
	for _, forbidden := range []string{"REASSIGN OWNED", "ALTER SEQUENCE", "runtime_write_state", "*"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("ALTER source contains forbidden %q", forbidden)
		}
	}
}

func TestManifestValidVersion13CatalogAndEvidence(t *testing.T) {
	db := newManifestTestDatabase(t)
	snapshot, err := validateVersion13Manifest(context.Background(), db, "aboutme")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.objects) != 110 || len(snapshot.triggers) != 2 {
		t.Fatalf("snapshot objects=%d triggers=%d", len(snapshot.objects), len(snapshot.triggers))
	}
	for _, evidence := range snapshot.objects {
		if evidence.oid == 0 || evidence.definition == "" {
			t.Fatalf("incomplete evidence for %s", evidence.object.identity)
		}
	}
}

func TestManifestMissingExtraWrongKindAndMixedOwnerReject(t *testing.T) {
	db := newManifestTestDatabase(t)
	validate := func(q manifestQueryer) error {
		_, err := validateVersion13Manifest(context.Background(), q, "aboutme")
		return err
	}
	tests := map[string]string{
		"missing":                  `DROP INDEX public.auth_email_jobs_claim_idx`,
		"extra":                    `CREATE TABLE public.manifest_extra(id integer PRIMARY KEY)`,
		"same-name-wrong-kind":     `DROP INDEX public.auth_email_jobs_claim_idx; CREATE VIEW public.auth_email_jobs_claim_idx AS SELECT 1 AS value`,
		"foundation-name-overload": `CREATE FUNCTION public.runtime_enter_write(integer) RETURNS integer LANGUAGE sql AS 'SELECT $1'`,
		"extra-procedure":          `CREATE PROCEDURE public.manifest_extra_procedure() LANGUAGE sql AS 'SELECT 1'`,
		"extra-enum":               `CREATE TYPE public.manifest_extra_enum AS ENUM ('extra')`,
		"mixed-owner":              `ALTER TABLE public.auth_email_jobs OWNER TO aboutme_runtime_owner`,
	}
	for name, statement := range tests {
		t.Run(name, func(t *testing.T) {
			err := manifestRollbackMutation(t, db, statement, validate)
			if !errors.Is(err, errVersion13ManifestMismatch) {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}

func TestManifestLiveOwnerTransferPreservesStructuredACLEvidence(t *testing.T) {
	db := newManifestTestDatabase(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, execErr := tx.ExecContext(context.Background(), `GRANT SELECT ON public.users TO aboutme_app WITH GRANT OPTION; GRANT UPDATE(email) ON public.users TO aboutme_app WITH GRANT OPTION; GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner`); execErr != nil {
		t.Fatal(execErr)
	}
	before, err := validateVersion13Manifest(context.Background(), tx, "aboutme")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range fixedVersion13AlterStatements() {
		if _, execErr := tx.ExecContext(context.Background(), statement); execErr != nil {
			t.Fatalf("execute fixed transfer %q: %v", statement, execErr)
		}
	}
	if _, execErr := tx.ExecContext(context.Background(), `REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner`); execErr != nil {
		t.Fatal(execErr)
	}
	after, err := validateVersion13Manifest(context.Background(), tx, "aboutme_runtime_owner")
	if err != nil {
		t.Fatal(err)
	}
	if compareErr := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", nil); compareErr != nil {
		t.Fatalf("owner transfer changed protected evidence: %v", compareErr)
	}
	usersACL := ""
	for _, evidence := range after.objects {
		if evidence.object.kind == "table" && evidence.object.identity == "users" {
			usersACL = evidence.acl
		}
	}
	if !strings.Contains(usersACL, `"grantee": "aboutme_app"`) || !strings.Contains(usersACL, `"scope": "column:2:email"`) {
		t.Fatalf("structured ACL lost exact prefixed principal: %q", usersACL)
	}
	if _, execErr := tx.ExecContext(context.Background(), `REVOKE GRANT OPTION FOR SELECT ON public.users FROM aboutme_app`); execErr != nil {
		t.Fatal(execErr)
	}
	changed, err := validateVersion13Manifest(context.Background(), tx, "aboutme_runtime_owner")
	if err != nil {
		t.Fatal(err)
	}
	if compareErr := compareVersion13ManifestSnapshots(before, changed, "aboutme", "aboutme_runtime_owner", nil); !errors.Is(compareErr, errVersion13ManifestMismatch) {
		t.Fatalf("changed non-owner grant option error=%v", compareErr)
	}
	if _, execErr := tx.ExecContext(context.Background(), `GRANT SELECT ON public.users TO aboutme_app WITH GRANT OPTION; REVOKE GRANT OPTION FOR UPDATE(email) ON public.users FROM aboutme_app`); execErr != nil {
		t.Fatal(execErr)
	}
	changed, err = validateVersion13Manifest(context.Background(), tx, "aboutme_runtime_owner")
	if err != nil {
		t.Fatal(err)
	}
	if compareErr := compareVersion13ManifestSnapshots(before, changed, "aboutme", "aboutme_runtime_owner", nil); !errors.Is(compareErr, errVersion13ManifestMismatch) {
		t.Fatalf("changed non-owner column grant option error=%v", compareErr)
	}
}

func TestManifestSnapshotComparisonPreservesEvidenceAndNormalizesOwnerACL(t *testing.T) {
	object := version13ManifestObject{kind: "table", schema: "public", identity: "users"}
	before := &version13ManifestSnapshot{objects: []version13ObjectEvidence{{
		object: object, oid: 42, owner: "aboutme", definition: "fixed", acl: `[{"scope":"relation","public":false,"grantee_oid":"10","grantee":"aboutme","grantor_oid":"10","grantor":"aboutme","privilege":"DELETE","grant_option":true},{"scope":"relation","public":false,"grantee_oid":"30","grantee":"aboutme_app","grantor_oid":"10","grantor":"aboutme","privilege":"SELECT","grant_option":true}]`, parent: "", rowCount: 7,
	}}, triggers: []string{"fixed-trigger"}}
	after := &version13ManifestSnapshot{objects: []version13ObjectEvidence{{
		object: object, oid: 42, owner: "aboutme_runtime_owner", definition: "fixed", acl: `[{"scope":"relation","public":false,"grantee_oid":"30","grantee":"aboutme_app","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"SELECT","grant_option":true},{"scope":"relation","public":false,"grantee_oid":"20","grantee":"aboutme_runtime_owner","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"DELETE","grant_option":true}]`, parent: "", rowCount: 7,
	}}, triggers: []string{"fixed-trigger"}}
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", nil); err != nil {
		t.Fatalf("owner/grantor normalization rejected: %v", err)
	}
	originalACL := after.objects[0].acl
	after.objects[0].acl = `[{"scope":"relation","public":false,"grantee_oid":"30","grantee":"aboutme_app","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"SELECT","grant_option":true},{"scope":"relation","public":false,"grantee_oid":"20","grantee":"aboutme_runtime_owner","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"DELETE","grant_option":true},{"scope":"relation","public":false,"grantee_oid":"10","grantee":"aboutme","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"SELECT","grant_option":false}]`
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", nil); !errors.Is(err, errVersion13ManifestMismatch) {
		t.Fatalf("retained source-owner grant error=%v", err)
	}
	after.objects[0].acl = originalACL
	after.objects[0].definition = "changed"
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", nil); !errors.Is(err, errVersion13ManifestMismatch) {
		t.Fatalf("definition change error=%v", err)
	}
	after.objects[0].definition = "fixed"
	after.objects[0].acl = `[{"scope":"relation","public":false,"grantee_oid":"20","grantee":"aboutme_runtime_owner","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"DELETE","grant_option":true},{"scope":"relation","public":false,"grantee_oid":"30","grantee":"aboutme_app","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"SELECT","grant_option":false}]`
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", nil); !errors.Is(err, errVersion13ManifestMismatch) {
		t.Fatalf("unexpected ACL change error=%v", err)
	}
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", map[string]string{"table|public|users": after.objects[0].acl}); err != nil {
		t.Fatalf("named ACL change rejected: %v", err)
	}
	before.objects[0].acl = `[{"scope":"relation","public":true,"grantee_oid":"0","grantee":"","grantor_oid":"10","grantor":"aboutme","privilege":"SELECT","grant_option":false}]`
	after.objects[0].acl = `[{"scope":"relation","public":false,"grantee_oid":"99","grantee":"PUBLIC","grantor_oid":"20","grantor":"aboutme_runtime_owner","privilege":"SELECT","grant_option":false}]`
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", nil); !errors.Is(err, errVersion13ManifestMismatch) {
		t.Fatalf("PUBLIC role alias error=%v", err)
	}
}

func TestManifestFoundationAndDependenciesReject(t *testing.T) {
	db := newManifestTestDatabase(t)
	validate := func(q manifestQueryer) error {
		_, err := validateVersion13Manifest(context.Background(), q, "aboutme")
		return err
	}
	tests := map[string]string{
		"foundation-owner":     `ALTER FUNCTION public.runtime_read_migrator_metadata() OWNER TO aboutme`,
		"foundation-procedure": `DROP FUNCTION public.runtime_read_migrator_metadata(); CREATE PROCEDURE public.runtime_read_migrator_metadata() LANGUAGE SQL AS 'SELECT 1'; ALTER PROCEDURE public.runtime_read_migrator_metadata() OWNER TO aboutme_runtime_owner`,
		"foundation-index":     `ALTER TABLE public.runtime_write_state DROP CONSTRAINT runtime_write_state_pkey; CREATE INDEX runtime_write_state_pkey ON public.users(id)`,
		"index-parent":         `ALTER TABLE public.users DROP CONSTRAINT users_email_key; CREATE INDEX users_email_key ON public.resumes(id)`,
		"row-type-parent":      `ALTER TABLE public.users RENAME TO users_moved; CREATE TYPE public.users AS (id integer)`,
		"trigger-function":     `DROP TRIGGER resumes_enforce_cap ON public.resumes; CREATE TRIGGER resumes_enforce_cap BEFORE INSERT OR UPDATE OF user_id ON public.resumes FOR EACH ROW EXECUTE FUNCTION public.notify_resume_revision()`,
		"identity-sequence":    `ALTER TABLE public.goose_db_version ALTER COLUMN id DROP IDENTITY`,
	}
	for name, statement := range tests {
		t.Run(name, func(t *testing.T) {
			err := manifestRollbackMutation(t, db, statement, validate)
			if !errors.Is(err, errVersion13ManifestMismatch) {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}
