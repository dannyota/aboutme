package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	version13ManifestSHA256 = "14b480b1e5739348ed8287b792135d4b305ec70d2314313475e47ff26bce4066"
	version13ManifestSource = `function|public|enforce_resume_cap()
function|public|notify_resume_revision()
index|public|auth_email_jobs_claim_idx
index|public|auth_email_jobs_outcome_idx
index|public|auth_email_jobs_pkey
index|public|goose_db_version_pkey
index|public|idempotency_records_expires_at_idx
index|public|idempotency_records_pkey
index|public|idempotency_records_user_expires_id_idx
index|public|idempotency_records_user_route_key_key
index|public|idempotency_usage_pkey
index|public|identities_pkey
index|public|identities_provider_subject_key
index|public|identities_user_id_idx
index|public|lifecycle_audit_events_expiry_idx
index|public|lifecycle_audit_events_job_kind_key
index|public|lifecycle_audit_events_pkey
index|public|media_deletion_jobs_completed_idx
index|public|media_deletion_jobs_next_attempt_idx
index|public|media_deletion_jobs_object_key_key
index|public|media_deletion_jobs_pending_age_idx
index|public|media_deletion_jobs_pkey
index|public|oauth_authorization_codes_client_id_idx
index|public|oauth_authorization_codes_code_digest_key
index|public|oauth_authorization_codes_expires_at_idx
index|public|oauth_authorization_codes_pkey
index|public|oauth_authorization_codes_user_id_idx
index|public|oauth_clients_created_at_idx
index|public|oauth_clients_pkey
index|public|oauth_grants_client_live_idx
index|public|oauth_grants_live_user_client_key
index|public|oauth_grants_pkey
index|public|oauth_tokens_cleanup_idx
index|public|oauth_tokens_client_live_idx
index|public|oauth_tokens_family_id_idx
index|public|oauth_tokens_grant_id_idx
index|public|oauth_tokens_pkey
index|public|oauth_tokens_rotated_from_key
index|public|oauth_tokens_token_digest_key
index|public|oauth_tokens_user_id_idx
index|public|oauth_transactions_expires_at_idx
index|public|oauth_transactions_handle_hash_key
index|public|oauth_transactions_pkey
index|public|password_credentials_pkey
index|public|password_registrations_email_key
index|public|password_registrations_expires_at_idx
index|public|password_registrations_pkey
index|public|password_registrations_token_digest_key
index|public|password_reset_tokens_expires_at_idx
index|public|password_reset_tokens_pkey
index|public|password_reset_tokens_token_digest_key
index|public|password_reset_tokens_user_id_key
index|public|privacy_sweep_state_pkey
index|public|public_state_pkey
index|public|resumes_photo_reference_idx
index|public|resumes_pkey
index|public|resumes_slug_key
index|public|resumes_user_id_idx
index|public|sessions_metadata_age_idx
index|public|sessions_pkey
index|public|sessions_rotated_from_key
index|public|sessions_token_hash_key
index|public|sessions_user_id_active_idx
index|public|slug_tombstones_pkey
index|public|slug_tombstones_slug_key
index|public|users_email_key
index|public|users_pkey
sequence|public|goose_db_version_id_seq
table|public|auth_email_jobs
table|public|goose_db_version
table|public|idempotency_records
table|public|idempotency_usage
table|public|identities
table|public|lifecycle_audit_events
table|public|media_deletion_jobs
table|public|oauth_authorization_codes
table|public|oauth_clients
table|public|oauth_grants
table|public|oauth_tokens
table|public|oauth_transactions
table|public|password_credentials
table|public|password_registrations
table|public|password_reset_tokens
table|public|privacy_sweep_state
table|public|public_state
table|public|resumes
table|public|sessions
table|public|slug_tombstones
table|public|users
type|public|auth_email_jobs
type|public|goose_db_version
type|public|idempotency_records
type|public|idempotency_usage
type|public|identities
type|public|lifecycle_audit_events
type|public|media_deletion_jobs
type|public|oauth_authorization_codes
type|public|oauth_clients
type|public|oauth_grants
type|public|oauth_tokens
type|public|oauth_transactions
type|public|password_credentials
type|public|password_registrations
type|public|password_reset_tokens
type|public|privacy_sweep_state
type|public|public_state
type|public|resumes
type|public|sessions
type|public|slug_tombstones
type|public|users
`
)

var errVersion13ManifestMismatch = errors.New("migration version 13 object manifest mismatch")

type version13ManifestObject struct {
	kind      string
	schema    string
	identity  string
	signature string
}

type version13ObjectEvidence struct {
	object     version13ManifestObject
	oid        uint32
	owner      string
	definition string
	acl        string
	parent     string
	rowCount   int64
}

type version13ManifestSnapshot struct {
	objects  []version13ObjectEvidence
	triggers []string
}

type version13ACLEntry struct {
	Scope       string `json:"scope"`
	Public      bool   `json:"public"`
	GranteeOID  string `json:"grantee_oid"`
	Grantee     string `json:"grantee"`
	GrantorOID  string `json:"grantor_oid"`
	Grantor     string `json:"grantor"`
	Privilege   string `json:"privilege"`
	GrantOption bool   `json:"grant_option"`
}

type manifestQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func fixedVersion13Manifest() []version13ManifestObject {
	lines := strings.Split(strings.TrimSuffix(version13ManifestSource, "\n"), "\n")
	objects := make([]version13ManifestObject, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "|")
		identity := parts[2]
		signature := ""
		if parts[0] == "function" {
			signature = identity
		}
		objects = append(objects, version13ManifestObject{kind: parts[0], schema: parts[1], identity: identity, signature: signature})
	}
	return objects
}

func version13ManifestDigest() string {
	digest := sha256.Sum256([]byte(version13ManifestSource))
	return hex.EncodeToString(digest[:])
}

func fixedVersion13AlterStatements() []string {
	statements := make([]string, 0, 23)
	for _, object := range fixedVersion13Manifest() {
		switch object.kind {
		case "table":
			statements = append(statements, fmt.Sprintf(`ALTER TABLE "public"."%s" OWNER TO "aboutme_runtime_owner"`, object.identity))
		case "function":
			name := strings.TrimSuffix(object.signature, "()")
			statements = append(statements, fmt.Sprintf(`ALTER FUNCTION "public"."%s"() OWNER TO "aboutme_runtime_owner"`, name))
		}
	}
	return statements
}

func validateVersion13Manifest(ctx context.Context, db manifestQueryer, expectedSourceOwner string) (*version13ManifestSnapshot, error) {
	expected := fixedVersion13Manifest()
	if len(expected) != 110 || version13ManifestDigest() != version13ManifestSHA256 {
		return nil, fmt.Errorf("%w: frozen source digest", errVersion13ManifestMismatch)
	}
	expectedByKey := make(map[string]version13ManifestObject, len(expected))
	for _, object := range expected {
		expectedByKey[manifestObjectKey(object.kind, object.schema, object.identity)] = object
	}

	rows, err := db.QueryContext(ctx, version13CatalogQuery)
	if err != nil {
		return nil, fmt.Errorf("query version 13 catalog: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration and implicit-close errors.
	seen := make(map[string]bool, len(expected))
	snapshot := &version13ManifestSnapshot{}
	for rows.Next() {
		var evidence version13ObjectEvidence
		if err := rows.Scan(&evidence.object.kind, &evidence.object.schema, &evidence.object.identity, &evidence.oid, &evidence.owner, &evidence.definition, &evidence.acl, &evidence.parent); err != nil {
			return nil, fmt.Errorf("scan version 13 catalog: %w", err)
		}
		key := manifestObjectKey(evidence.object.kind, evidence.object.schema, evidence.object.identity)
		object, ok := expectedByKey[key]
		if !ok || seen[key] {
			return nil, fmt.Errorf("%w: unexpected or duplicate object %s", errVersion13ManifestMismatch, key)
		}
		if evidence.owner != expectedSourceOwner {
			return nil, fmt.Errorf("%w: %s owner %q, expected %q", errVersion13ManifestMismatch, key, evidence.owner, expectedSourceOwner)
		}
		evidence.object = object
		seen[key] = true
		snapshot.objects = append(snapshot.objects, evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate version 13 catalog: %w", err)
	}
	for key := range expectedByKey {
		if !seen[key] {
			return nil, fmt.Errorf("%w: missing object %s", errVersion13ManifestMismatch, key)
		}
	}
	for index := range snapshot.objects {
		if snapshot.objects[index].object.kind != "table" {
			continue
		}
		statement := fmt.Sprintf(`SELECT count(*) FROM "public"."%s"`, snapshot.objects[index].object.identity)
		if err := db.QueryRowContext(ctx, statement).Scan(&snapshot.objects[index].rowCount); err != nil {
			return nil, fmt.Errorf("count manifest table %s: %w", snapshot.objects[index].object.identity, err)
		}
	}
	if err := validateVersion13Foundation(ctx, db); err != nil {
		return nil, err
	}
	if err := validateVersion13Dependencies(ctx, db, snapshot); err != nil {
		return nil, err
	}
	sort.Slice(snapshot.objects, func(i, j int) bool {
		return manifestObjectKey(snapshot.objects[i].object.kind, snapshot.objects[i].object.schema, snapshot.objects[i].object.identity) < manifestObjectKey(snapshot.objects[j].object.kind, snapshot.objects[j].object.schema, snapshot.objects[j].object.identity)
	})
	return snapshot, nil
}

func manifestObjectKey(kind, schema, identity string) string {
	return kind + "|" + schema + "|" + identity
}

func validateVersion13Foundation(ctx context.Context, db manifestQueryer) error {
	var count int
	if err := db.QueryRowContext(ctx, version13FoundationQuery).Scan(&count); err != nil {
		return fmt.Errorf("query runtime foundation owners: %w", err)
	}
	if count != 16 {
		return fmt.Errorf("%w: runtime foundation owner count %d", errVersion13ManifestMismatch, count)
	}
	return nil
}

func validateVersion13Dependencies(ctx context.Context, db manifestQueryer, snapshot *version13ManifestSnapshot) error {
	rows, err := db.QueryContext(ctx, version13TriggerQuery)
	if err != nil {
		return fmt.Errorf("query version 13 triggers: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var trigger string
		if err := rows.Scan(&trigger); err != nil {
			return err
		}
		snapshot.triggers = append(snapshot.triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	wantTriggers := []string{
		"resumes|resume_revision_notification|O|0|notify_resume_revision()|CREATE TRIGGER resume_revision_notification AFTER INSERT OR DELETE OR UPDATE ON resumes FOR EACH ROW EXECUTE FUNCTION notify_resume_revision()",
		"resumes|resumes_enforce_cap|O|0|enforce_resume_cap()|CREATE TRIGGER resumes_enforce_cap BEFORE INSERT OR UPDATE OF user_id ON resumes FOR EACH ROW EXECUTE FUNCTION enforce_resume_cap()",
	}
	wantProtectedTriggers := []string{
		"goose_db_version|goose_db_version_insert_guard|O|0|runtime_assert_migration_history_write()|CREATE TRIGGER goose_db_version_insert_guard BEFORE INSERT ON goose_db_version FOR EACH ROW EXECUTE FUNCTION runtime_assert_migration_history_write()",
		"goose_db_version|goose_db_version_truncate_guard|O|0|runtime_deny_migration_history_write()|CREATE TRIGGER goose_db_version_truncate_guard BEFORE TRUNCATE ON goose_db_version FOR EACH STATEMENT EXECUTE FUNCTION runtime_deny_migration_history_write()",
		"goose_db_version|goose_db_version_update_delete_guard|O|0|runtime_deny_migration_history_write()|CREATE TRIGGER goose_db_version_update_delete_guard BEFORE DELETE OR UPDATE ON goose_db_version FOR EACH ROW EXECUTE FUNCTION runtime_deny_migration_history_write()",
		wantTriggers[0], wantTriggers[1],
	}
	if fmt.Sprint(snapshot.triggers) != fmt.Sprint(wantTriggers) && fmt.Sprint(snapshot.triggers) != fmt.Sprint(wantProtectedTriggers) {
		return fmt.Errorf("%w: trigger dependencies %v", errVersion13ManifestMismatch, snapshot.triggers)
	}
	var validIdentity bool
	if err := db.QueryRowContext(ctx, version13IdentitySequenceQuery).Scan(&validIdentity); err != nil {
		return fmt.Errorf("query Goose identity dependency: %w", err)
	}
	if !validIdentity {
		return fmt.Errorf("%w: Goose identity sequence dependency", errVersion13ManifestMismatch)
	}
	for _, evidence := range snapshot.objects {
		switch evidence.object.kind {
		case "index":
			if evidence.parent != expectedVersion13IndexParent(evidence.object.identity) {
				return fmt.Errorf("%w: index %s parent %q", errVersion13ManifestMismatch, evidence.object.identity, evidence.parent)
			}
		case "type":
			if evidence.parent != evidence.object.identity {
				return fmt.Errorf("%w: row type %s parent %q", errVersion13ManifestMismatch, evidence.object.identity, evidence.parent)
			}
		}
	}
	return nil
}

func expectedVersion13IndexParent(index string) string {
	best := ""
	for _, object := range fixedVersion13Manifest() {
		if object.kind == "table" && strings.HasPrefix(index, object.identity+"_") && len(object.identity) > len(best) {
			best = object.identity
		}
	}
	return best
}

func compareVersion13ManifestSnapshots(before, after *version13ManifestSnapshot, sourceOwner, targetOwner string, allowedACLChanges map[string]string) error {
	if before == nil || after == nil || len(before.objects) != len(after.objects) {
		return fmt.Errorf("%w: snapshot size", errVersion13ManifestMismatch)
	}
	afterByKey := make(map[string]version13ObjectEvidence, len(after.objects))
	for _, evidence := range after.objects {
		afterByKey[manifestObjectKey(evidence.object.kind, evidence.object.schema, evidence.object.identity)] = evidence
	}
	for _, oldEvidence := range before.objects {
		key := manifestObjectKey(oldEvidence.object.kind, oldEvidence.object.schema, oldEvidence.object.identity)
		newEvidence, ok := afterByKey[key]
		if !ok || oldEvidence.owner != sourceOwner || newEvidence.owner != targetOwner ||
			oldEvidence.oid != newEvidence.oid || oldEvidence.definition != newEvidence.definition ||
			oldEvidence.parent != newEvidence.parent || oldEvidence.rowCount != newEvidence.rowCount {
			return fmt.Errorf("%w: changed evidence for %s", errVersion13ManifestMismatch, key)
		}
		oldACL, newACL := normalizeVersion13ACLPair(oldEvidence.acl, newEvidence.acl, sourceOwner, targetOwner)
		if oldACL != newACL {
			allowedACL, allowed := allowedACLChanges[key]
			if !allowed || newEvidence.acl != allowedACL {
				return fmt.Errorf("%w: changed ACL for %s", errVersion13ManifestMismatch, key)
			}
		}
	}
	if fmt.Sprint(before.triggers) != fmt.Sprint(after.triggers) {
		return fmt.Errorf("%w: changed triggers", errVersion13ManifestMismatch)
	}
	return nil
}

func normalizeVersion13ACLPair(beforeACL, afterACL, sourceOwner, targetOwner string) (string, string) {
	var afterEntries []version13ACLEntry
	if err := json.Unmarshal([]byte(afterACL), &afterEntries); err != nil {
		return "invalid:" + beforeACL, "invalid:" + afterACL
	}
	var targetOID string
	for _, entry := range afterEntries {
		if entry.Grantee == targetOwner {
			targetOID = entry.GranteeOID
		}
		if entry.Grantor == targetOwner {
			targetOID = entry.GrantorOID
		}
	}
	var beforeEntries []version13ACLEntry
	if err := json.Unmarshal([]byte(beforeACL), &beforeEntries); err != nil || targetOID == "" {
		return "invalid:" + beforeACL, canonicalVersion13ACL(afterEntries)
	}
	for index := range beforeEntries {
		if beforeEntries[index].Grantee == sourceOwner && !beforeEntries[index].Public {
			beforeEntries[index].Grantee = targetOwner
			beforeEntries[index].GranteeOID = targetOID
		}
		if beforeEntries[index].Grantor == sourceOwner {
			beforeEntries[index].Grantor = targetOwner
			beforeEntries[index].GrantorOID = targetOID
		}
	}
	return canonicalVersion13ACL(beforeEntries), canonicalVersion13ACL(afterEntries)
}

func canonicalVersion13ACL(entries []version13ACLEntry) string {
	sort.Slice(entries, func(i, j int) bool {
		left := fmt.Sprintf("%s\x00%t\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t", entries[i].Scope, entries[i].Public, entries[i].GranteeOID, entries[i].Grantee, entries[i].GrantorOID, entries[i].Grantor, entries[i].Privilege, entries[i].GrantOption)
		right := fmt.Sprintf("%s\x00%t\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t", entries[j].Scope, entries[j].Public, entries[j].GranteeOID, entries[j].Grantee, entries[j].GrantorOID, entries[j].Grantor, entries[j].Privilege, entries[j].GrantOption)
		return left < right
	})
	canonical, err := json.Marshal(entries)
	if err != nil {
		return "invalid"
	}
	return string(canonical)
}

const version13CatalogQuery = `
WITH foundation_relations AS (
 SELECT c.oid FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relname IN ('runtime_write_state','runtime_write_state_pkey')
), objects AS (
 SELECT CASE c.relkind WHEN 'r' THEN 'table' WHEN 'p' THEN 'table' WHEN 'i' THEN 'index' WHEN 'I' THEN 'index' WHEN 'S' THEN 'sequence' ELSE 'relation:'||c.relkind::text END AS kind,
   n.nspname AS schema_name,c.relname AS identity,c.oid,c.relowner AS owner_oid,
   CASE WHEN c.relkind IN ('i','I') THEN pg_catalog.pg_get_indexdef(c.oid)
        WHEN c.relkind='S' THEN concat_ws('|',c.relkind::text,(SELECT concat_ws(',',s.seqtypid::text,s.seqstart::text,s.seqincrement::text,s.seqmax::text,s.seqmin::text,s.seqcache::text,s.seqcycle::text) FROM pg_catalog.pg_sequence s WHERE s.seqrelid=c.oid))
        ELSE concat_ws('|',c.relkind::text,c.relpersistence::text,c.relrowsecurity::text,c.relforcerowsecurity::text,
          (SELECT string_agg(concat_ws(':',a.attnum::text,a.attname,pg_catalog.format_type(a.atttypid,a.atttypmod),a.attnotnull::text,a.attidentity::text,a.attgenerated::text,COALESCE(pg_catalog.pg_get_expr(d.adbin,d.adrelid),'')),',' ORDER BY a.attnum) FROM pg_catalog.pg_attribute a LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped),
          (SELECT string_agg(pg_catalog.pg_get_constraintdef(k.oid,true),',' ORDER BY k.conname) FROM pg_catalog.pg_constraint k WHERE k.conrelid=c.oid)) END AS definition,
   COALESCE((SELECT jsonb_agg(jsonb_build_object('scope',a.scope,'public',a.grantee=0,'grantee_oid',a.grantee,'grantee',CASE WHEN a.grantee=0 THEN '' ELSE pg_catalog.pg_get_userbyid(a.grantee) END,'grantor_oid',a.grantor,'grantor',pg_catalog.pg_get_userbyid(a.grantor),'privilege',a.privilege_type,'grant_option',a.is_grantable) ORDER BY a.scope,a.grantee,a.grantor,a.privilege_type,a.is_grantable)::text
     FROM (SELECT 'relation' AS scope,x.* FROM pg_catalog.aclexplode(COALESCE(c.relacl,pg_catalog.acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner))) x
       UNION ALL SELECT 'column:'||at.attnum::text||':'||at.attname,x.* FROM pg_catalog.pg_attribute at CROSS JOIN LATERAL pg_catalog.aclexplode(at.attacl) x WHERE at.attrelid=c.oid AND at.attnum>0 AND NOT at.attisdropped AND at.attacl IS NOT NULL) a),'[]') AS acl,COALESCE(parent.relname,'') AS parent
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 LEFT JOIN pg_catalog.pg_index i ON i.indexrelid=c.oid LEFT JOIN pg_catalog.pg_class parent ON parent.oid=i.indrelid
 WHERE n.nspname='public' AND c.oid NOT IN (SELECT oid FROM foundation_relations)
   AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_class'::regclass AND d.objid=c.oid AND d.deptype='e')
 UNION ALL
 SELECT CASE WHEN p.prokind='f' THEN 'function' ELSE 'routine:'||p.prokind::text END,n.nspname,p.proname||'('||pg_catalog.pg_get_function_identity_arguments(p.oid)||')',p.oid,p.proowner,
   pg_catalog.pg_get_functiondef(p.oid),COALESCE((SELECT jsonb_agg(jsonb_build_object('scope','routine','public',x.grantee=0,'grantee_oid',x.grantee,'grantee',CASE WHEN x.grantee=0 THEN '' ELSE pg_catalog.pg_get_userbyid(x.grantee) END,'grantor_oid',x.grantor,'grantor',pg_catalog.pg_get_userbyid(x.grantor),'privilege',x.privilege_type,'grant_option',x.is_grantable) ORDER BY x.grantee,x.grantor,x.privilege_type,x.is_grantable)::text FROM pg_catalog.aclexplode(COALESCE(p.proacl,pg_catalog.acldefault('f',p.proowner))) x),'[]'),''
 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='public'
   AND p.proname||'('||pg_catalog.pg_get_function_identity_arguments(p.oid)||')' NOT IN ('runtime_assert_write_finished()','runtime_validate_write_marker()','runtime_create_write_marker()','runtime_require_write_entry()','runtime_enter_write()','runtime_assert_write_entry()','runtime_assert_business_write()','runtime_finish_write()','runtime_validate_migrator_marker()','runtime_enter_migrator()','runtime_begin_migration_write(operation_id text)','runtime_exit_migrator()','runtime_read_migrator_metadata()')
   AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_proc'::regclass AND d.objid=p.oid AND d.deptype='e')
 UNION ALL
 SELECT 'type',n.nspname,t.typname,t.oid,t.typowner,concat_ws('|',t.typtype::text,t.typrelid::text,t.typcategory::text),COALESCE((SELECT jsonb_agg(jsonb_build_object('scope','type','public',x.grantee=0,'grantee_oid',x.grantee,'grantee',CASE WHEN x.grantee=0 THEN '' ELSE pg_catalog.pg_get_userbyid(x.grantee) END,'grantor_oid',x.grantor,'grantor',pg_catalog.pg_get_userbyid(x.grantor),'privilege',x.privilege_type,'grant_option',x.is_grantable) ORDER BY x.grantee,x.grantor,x.privilege_type,x.is_grantable)::text FROM pg_catalog.aclexplode(COALESCE(t.typacl,pg_catalog.acldefault('T',t.typowner))) x),'[]'),COALESCE(c.relname,'')
 FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace LEFT JOIN pg_catalog.pg_class c ON c.oid=t.typrelid
 WHERE n.nspname='public' AND t.typname<>'runtime_write_state'
   AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend array_dependency
     WHERE array_dependency.classid='pg_catalog.pg_type'::regclass AND array_dependency.objid=t.oid
       AND array_dependency.refclassid='pg_catalog.pg_type'::regclass AND array_dependency.deptype='i')
   AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_type'::regclass AND d.objid=t.oid AND d.deptype='e')
)
SELECT kind,schema_name,identity,oid,pg_catalog.pg_get_userbyid(owner_oid),definition,acl,parent
FROM objects ORDER BY kind,schema_name,identity`

const version13FoundationQuery = `
SELECT count(*) FROM (
 SELECT c.oid FROM pg_catalog.pg_class c WHERE c.relnamespace='public'::regnamespace AND c.relname='runtime_write_state' AND c.relkind='r' AND pg_catalog.pg_get_userbyid(c.relowner)='aboutme_runtime_owner'
 UNION ALL SELECT c.oid FROM pg_catalog.pg_class c JOIN pg_catalog.pg_index i ON i.indexrelid=c.oid WHERE c.relnamespace='public'::regnamespace AND c.relname='runtime_write_state_pkey' AND c.relkind='i' AND i.indrelid='public.runtime_write_state'::regclass AND i.indisprimary AND pg_catalog.pg_get_userbyid(c.relowner)='aboutme_runtime_owner'
 UNION ALL SELECT t.oid FROM pg_catalog.pg_type t WHERE t.typnamespace='public'::regnamespace AND t.typname='runtime_write_state' AND t.typrelid='public.runtime_write_state'::regclass AND pg_catalog.pg_get_userbyid(t.typowner)='aboutme_runtime_owner'
 UNION ALL SELECT p.oid FROM pg_catalog.pg_proc p WHERE p.pronamespace='public'::regnamespace AND p.proname||'('||pg_catalog.pg_get_function_identity_arguments(p.oid)||')' IN ('runtime_assert_write_finished()','runtime_validate_write_marker()','runtime_create_write_marker()','runtime_require_write_entry()','runtime_enter_write()','runtime_assert_write_entry()','runtime_assert_business_write()','runtime_finish_write()','runtime_validate_migrator_marker()','runtime_enter_migrator()','runtime_begin_migration_write(operation_id text)','runtime_exit_migrator()','runtime_read_migrator_metadata()') AND p.prokind='f' AND pg_catalog.pg_get_userbyid(p.proowner)='aboutme_runtime_owner'
) foundation`

const version13TriggerQuery = `
SELECT c.relname||'|'||t.tgname||'|'||t.tgenabled::text||'|'||t.tgconstraint||'|'||p.proname||'('||pg_catalog.pg_get_function_identity_arguments(p.oid)||')|'||pg_catalog.pg_get_triggerdef(t.oid,true)
FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_proc p ON p.oid=t.tgfoid
WHERE n.nspname='public' AND NOT t.tgisinternal ORDER BY c.relname,t.tgname`

const version13IdentitySequenceQuery = `
SELECT EXISTS (
 SELECT 1 FROM pg_catalog.pg_class seq JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_class'::regclass AND d.objid=seq.oid
 JOIN pg_catalog.pg_class tbl ON d.refclassid='pg_catalog.pg_class'::regclass AND d.refobjid=tbl.oid
 JOIN pg_catalog.pg_attribute a ON a.attrelid=tbl.oid AND a.attnum=d.refobjsubid
 JOIN pg_catalog.pg_type row_type ON row_type.typrelid=tbl.oid
 JOIN pg_catalog.pg_index pi ON pi.indrelid=tbl.oid AND pi.indisprimary JOIN pg_catalog.pg_class idx ON idx.oid=pi.indexrelid
 WHERE seq.relnamespace='public'::regnamespace AND seq.relname='goose_db_version_id_seq' AND seq.relkind='S'
   AND d.deptype='i' AND tbl.oid='public.goose_db_version'::regclass AND a.attname='id' AND a.attidentity='d' AND a.attgenerated='' AND NOT a.atthasdef
   AND seq.relowner=tbl.relowner AND row_type.typowner=tbl.relowner AND idx.relowner=tbl.relowner
)`
