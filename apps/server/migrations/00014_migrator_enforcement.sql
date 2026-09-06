-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00014');

DO $$ BEGIN
  IF pg_catalog.has_schema_privilege('aboutme_runtime_owner','public','CREATE') THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime owner has unexpected schema CREATE';
  END IF;
END $$;

SELECT pg_catalog.set_config(
  'role',
  CASE WHEN (SELECT pg_catalog.pg_get_userbyid(relowner) FROM pg_catalog.pg_class WHERE oid='public.goose_db_version'::regclass)='aboutme_runtime_owner'
       THEN 'aboutme_runtime_owner' ELSE 'aboutme_migrator' END,
  true
);

LOCK TABLE public.auth_email_jobs,public.goose_db_version,public.idempotency_records,
 public.idempotency_usage,public.identities,public.lifecycle_audit_events,
 public.media_deletion_jobs,public.oauth_authorization_codes,public.oauth_clients,
 public.oauth_grants,public.oauth_tokens,public.oauth_transactions,
 public.password_credentials,public.password_registrations,public.password_reset_tokens,
 public.privacy_sweep_state,public.public_state,public.resumes,public.sessions,
 public.slug_tombstones,public.users IN ACCESS EXCLUSIVE MODE;

DO $runtime_static$
BEGIN
  CREATE TEMP TABLE pg_temp.runtime_v13_transfer_objects ON COMMIT DROP AS
SELECT c.oid,'table'::text AS kind,c.relname AS identity,c.relowner AS source_owner,
 concat_ws('|',c.relkind::text,c.relpersistence::text,c.relrowsecurity::text,c.relforcerowsecurity::text,
  (SELECT string_agg(concat_ws(':',a.attnum::text,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull::text,a.attidentity::text,a.attgenerated::text,COALESCE(pg_get_expr(d.adbin,d.adrelid),'')),',' ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped),
  (SELECT string_agg(pg_get_constraintdef(k.oid,true),',' ORDER BY k.conname) FROM pg_constraint k WHERE k.conrelid=c.oid)) AS definition
FROM pg_class c WHERE c.relnamespace='public'::regnamespace AND c.relkind='r' AND c.relname=ANY(ARRAY['auth_email_jobs','goose_db_version','idempotency_records','idempotency_usage','identities','lifecycle_audit_events','media_deletion_jobs','oauth_authorization_codes','oauth_clients','oauth_grants','oauth_tokens','oauth_transactions','password_credentials','password_registrations','password_reset_tokens','privacy_sweep_state','public_state','resumes','sessions','slug_tombstones','users'])
UNION ALL
SELECT p.oid,'function',p.proname,p.proowner,pg_get_functiondef(p.oid)
FROM pg_proc p WHERE p.pronamespace='public'::regnamespace AND p.prokind='f' AND p.proname IN ('enforce_resume_cap','notify_resume_revision') AND pg_get_function_identity_arguments(p.oid)='';
END
$runtime_static$;

DO $$ BEGIN
 IF (SELECT count(*) FROM pg_temp.runtime_v13_transfer_objects)<>23
    OR (SELECT count(DISTINCT source_owner) FROM pg_temp.runtime_v13_transfer_objects)<>1
    OR (SELECT pg_get_userbyid(min(source_owner::bigint)::oid) FROM pg_temp.runtime_v13_transfer_objects) NOT IN ('aboutme_migrator','aboutme_runtime_owner') THEN
   RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration manifest source mismatch';
 END IF;
END $$;

DO $runtime_static$
BEGIN
  CREATE TEMP TABLE pg_temp.runtime_v13_transfer_dependents ON COMMIT DROP AS
SELECT c.oid,'index'::text AS kind,c.relname AS identity,c.relowner AS source_owner,
  pg_get_indexdef(c.oid) AS definition,parent.relname AS parent
FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid JOIN pg_class parent ON parent.oid=i.indrelid
WHERE c.relnamespace='public'::regnamespace AND c.relkind IN ('i','I')
  AND parent.relname=ANY(ARRAY['auth_email_jobs','goose_db_version','idempotency_records','idempotency_usage','identities','lifecycle_audit_events','media_deletion_jobs','oauth_authorization_codes','oauth_clients','oauth_grants','oauth_tokens','oauth_transactions','password_credentials','password_registrations','password_reset_tokens','privacy_sweep_state','public_state','resumes','sessions','slug_tombstones','users'])
UNION ALL
SELECT t.oid,'type',t.typname,t.typowner,concat_ws('|',t.typtype::text,t.typrelid::text,t.typcategory::text),c.relname
FROM pg_type t JOIN pg_class c ON c.oid=t.typrelid
WHERE t.typnamespace='public'::regnamespace AND c.relname=ANY(ARRAY['auth_email_jobs','goose_db_version','idempotency_records','idempotency_usage','identities','lifecycle_audit_events','media_deletion_jobs','oauth_authorization_codes','oauth_clients','oauth_grants','oauth_tokens','oauth_transactions','password_credentials','password_registrations','password_reset_tokens','privacy_sweep_state','public_state','resumes','sessions','slug_tombstones','users'])
UNION ALL
SELECT seq.oid,'sequence',seq.relname,seq.relowner,
  concat_ws('|',seq.relkind::text,(SELECT concat_ws(',',s.seqtypid::text,s.seqstart::text,s.seqincrement::text,s.seqmax::text,s.seqmin::text,s.seqcache::text,s.seqcycle::text) FROM pg_sequence s WHERE s.seqrelid=seq.oid)),
  tbl.relname
FROM pg_class seq JOIN pg_depend d ON d.classid='pg_class'::regclass AND d.objid=seq.oid AND d.deptype='i'
JOIN pg_class tbl ON d.refclassid='pg_class'::regclass AND d.refobjid=tbl.oid
WHERE seq.relnamespace='public'::regnamespace AND seq.relname='goose_db_version_id_seq' AND tbl.relname='goose_db_version';
END
$runtime_static$;

DO $runtime_static$
BEGIN
  CREATE TEMP TABLE pg_temp.runtime_v13_transfer_triggers ON COMMIT DROP AS
SELECT t.oid,c.relname AS parent,t.tgname AS identity,t.tgenabled,t.tgconstraint,
  p.proname||'('||pg_get_function_identity_arguments(p.oid)||')' AS function_identity,
  pg_get_triggerdef(t.oid,true) AS definition
FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_proc p ON p.oid=t.tgfoid
WHERE c.relnamespace='public'::regnamespace AND NOT t.tgisinternal
  AND c.relname='resumes' AND t.tgname IN ('resume_revision_notification','resumes_enforce_cap');
END
$runtime_static$;

DO $$ BEGIN
 IF (SELECT count(*) FROM pg_temp.runtime_v13_transfer_dependents)<>87
    OR (SELECT count(*) FROM pg_temp.runtime_v13_transfer_dependents WHERE kind='index')<>65
    OR (SELECT array_agg(identity::text ORDER BY identity) FROM pg_temp.runtime_v13_transfer_dependents WHERE kind='index')<>ARRAY['auth_email_jobs_claim_idx','auth_email_jobs_outcome_idx','auth_email_jobs_pkey','goose_db_version_pkey','idempotency_records_expires_at_idx','idempotency_records_pkey','idempotency_records_user_expires_id_idx','idempotency_records_user_route_key_key','idempotency_usage_pkey','identities_pkey','identities_provider_subject_key','identities_user_id_idx','lifecycle_audit_events_expiry_idx','lifecycle_audit_events_job_kind_key','lifecycle_audit_events_pkey','media_deletion_jobs_completed_idx','media_deletion_jobs_next_attempt_idx','media_deletion_jobs_object_key_key','media_deletion_jobs_pending_age_idx','media_deletion_jobs_pkey','oauth_authorization_codes_client_id_idx','oauth_authorization_codes_code_digest_key','oauth_authorization_codes_expires_at_idx','oauth_authorization_codes_pkey','oauth_authorization_codes_user_id_idx','oauth_clients_created_at_idx','oauth_clients_pkey','oauth_grants_client_live_idx','oauth_grants_live_user_client_key','oauth_grants_pkey','oauth_tokens_cleanup_idx','oauth_tokens_client_live_idx','oauth_tokens_family_id_idx','oauth_tokens_grant_id_idx','oauth_tokens_pkey','oauth_tokens_rotated_from_key','oauth_tokens_token_digest_key','oauth_tokens_user_id_idx','oauth_transactions_expires_at_idx','oauth_transactions_handle_hash_key','oauth_transactions_pkey','password_credentials_pkey','password_registrations_email_key','password_registrations_expires_at_idx','password_registrations_pkey','password_registrations_token_digest_key','password_reset_tokens_expires_at_idx','password_reset_tokens_pkey','password_reset_tokens_token_digest_key','password_reset_tokens_user_id_key','privacy_sweep_state_pkey','public_state_pkey','resumes_photo_reference_idx','resumes_pkey','resumes_slug_key','resumes_user_id_idx','sessions_metadata_age_idx','sessions_pkey','sessions_rotated_from_key','sessions_token_hash_key','sessions_user_id_active_idx','slug_tombstones_pkey','slug_tombstones_slug_key','users_email_key','users_pkey']
    OR (SELECT count(*) FROM pg_temp.runtime_v13_transfer_dependents WHERE kind='type')<>21
    OR (SELECT count(*) FROM pg_temp.runtime_v13_transfer_dependents WHERE kind='sequence')<>1
    OR (SELECT count(*) FROM pg_temp.runtime_v13_transfer_triggers)<>2
    OR (SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace AND NOT t.tgisinternal)<>2
    OR (SELECT count(DISTINCT source_owner) FROM pg_temp.runtime_v13_transfer_dependents)<>1
    OR (SELECT min(source_owner) FROM pg_temp.runtime_v13_transfer_dependents)<>(SELECT min(source_owner) FROM pg_temp.runtime_v13_transfer_objects)
    OR (SELECT count(*) FROM pg_class c WHERE c.relnamespace='public'::regnamespace
          AND c.relname NOT IN ('runtime_write_state','runtime_write_state_pkey')
          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='e'))<>87
    OR (SELECT count(*) FROM pg_type t WHERE t.typnamespace='public'::regnamespace AND t.typname<>'runtime_write_state'
          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_type'::regclass AND d.objid=t.oid AND d.refclassid='pg_type'::regclass AND d.deptype='i')
          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_type'::regclass AND d.objid=t.oid AND d.deptype='e'))<>21
    OR (SELECT count(*) FROM pg_proc p WHERE p.pronamespace='public'::regnamespace
          AND p.proname||'('||pg_get_function_identity_arguments(p.oid)||')' NOT IN ('runtime_assert_write_finished()','runtime_validate_write_marker()','runtime_create_write_marker()','runtime_require_write_entry()','runtime_enter_write()','runtime_assert_write_entry()','runtime_assert_business_write()','runtime_finish_write()','runtime_validate_migrator_marker()','runtime_enter_migrator()','runtime_begin_migration_write(operation_id text)','runtime_exit_migrator()','runtime_read_migrator_metadata()')
          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_proc'::regclass AND d.objid=p.oid AND d.deptype='e'))<>2 THEN
   RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration dependent manifest mismatch';
 END IF;
END $$;

DO $runtime_static$
BEGIN
  CREATE TEMP TABLE pg_temp.runtime_v13_transfer_acl ON COMMIT DROP AS
SELECT o.oid,o.source_owner,'relation'::text AS scope,x.grantee,x.grantor,x.privilege_type,x.is_grantable
FROM pg_temp.runtime_v13_transfer_objects o JOIN pg_class c ON c.oid=o.oid
CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault('r',c.relowner))) x WHERE o.kind='table'
UNION ALL
SELECT o.oid,o.source_owner,'column:'||a.attnum::text,x.grantee,x.grantor,x.privilege_type,x.is_grantable
FROM pg_temp.runtime_v13_transfer_objects o JOIN pg_attribute a ON a.attrelid=o.oid
CROSS JOIN LATERAL aclexplode(a.attacl) x WHERE o.kind='table' AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL
UNION ALL
SELECT o.oid,o.source_owner,'function',x.grantee,x.grantor,x.privilege_type,x.is_grantable
FROM pg_temp.runtime_v13_transfer_objects o JOIN pg_proc p ON p.oid=o.oid
CROSS JOIN LATERAL aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) x WHERE o.kind='function';
END
$runtime_static$;

GRANT SELECT ON pg_temp.runtime_v13_transfer_objects,
  pg_temp.runtime_v13_transfer_acl,
  pg_temp.runtime_v13_transfer_dependents,
  pg_temp.runtime_v13_transfer_triggers TO aboutme_runtime_owner;

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;

SELECT pg_catalog.set_config(
  'role',
  CASE WHEN (SELECT pg_catalog.pg_get_userbyid(relowner) FROM pg_catalog.pg_class WHERE oid='public.goose_db_version'::regclass)='aboutme_runtime_owner'
       THEN 'aboutme_runtime_owner' ELSE 'aboutme_migrator' END,
  true
);

DO $runtime_static$
BEGIN
  CREATE TEMP TABLE pg_temp.runtime_v13_transfer_rows ON COMMIT DROP AS
SELECT 'auth_email_jobs'::text AS identity,count(*)::bigint AS row_count FROM public.auth_email_jobs
UNION ALL
SELECT 'goose_db_version'::text AS identity,count(*)::bigint AS row_count FROM public.goose_db_version
UNION ALL
SELECT 'idempotency_records'::text AS identity,count(*)::bigint AS row_count FROM public.idempotency_records
UNION ALL
SELECT 'idempotency_usage'::text AS identity,count(*)::bigint AS row_count FROM public.idempotency_usage
UNION ALL
SELECT 'identities'::text AS identity,count(*)::bigint AS row_count FROM public.identities
UNION ALL
SELECT 'lifecycle_audit_events'::text AS identity,count(*)::bigint AS row_count FROM public.lifecycle_audit_events
UNION ALL
SELECT 'media_deletion_jobs'::text AS identity,count(*)::bigint AS row_count FROM public.media_deletion_jobs
UNION ALL
SELECT 'oauth_authorization_codes'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_authorization_codes
UNION ALL
SELECT 'oauth_clients'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_clients
UNION ALL
SELECT 'oauth_grants'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_grants
UNION ALL
SELECT 'oauth_tokens'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_tokens
UNION ALL
SELECT 'oauth_transactions'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_transactions
UNION ALL
SELECT 'password_credentials'::text AS identity,count(*)::bigint AS row_count FROM public.password_credentials
UNION ALL
SELECT 'password_registrations'::text AS identity,count(*)::bigint AS row_count FROM public.password_registrations
UNION ALL
SELECT 'password_reset_tokens'::text AS identity,count(*)::bigint AS row_count FROM public.password_reset_tokens
UNION ALL
SELECT 'privacy_sweep_state'::text AS identity,count(*)::bigint AS row_count FROM public.privacy_sweep_state
UNION ALL
SELECT 'public_state'::text AS identity,count(*)::bigint AS row_count FROM public.public_state
UNION ALL
SELECT 'resumes'::text AS identity,count(*)::bigint AS row_count FROM public.resumes
UNION ALL
SELECT 'sessions'::text AS identity,count(*)::bigint AS row_count FROM public.sessions
UNION ALL
SELECT 'slug_tombstones'::text AS identity,count(*)::bigint AS row_count FROM public.slug_tombstones
UNION ALL
SELECT 'users'::text AS identity,count(*)::bigint AS row_count FROM public.users;
END
$runtime_static$;

ALTER TABLE public.auth_email_jobs OWNER TO aboutme_runtime_owner;
ALTER TABLE public.goose_db_version OWNER TO aboutme_runtime_owner;
ALTER TABLE public.idempotency_records OWNER TO aboutme_runtime_owner;
ALTER TABLE public.idempotency_usage OWNER TO aboutme_runtime_owner;
ALTER TABLE public.identities OWNER TO aboutme_runtime_owner;
ALTER TABLE public.lifecycle_audit_events OWNER TO aboutme_runtime_owner;
ALTER TABLE public.media_deletion_jobs OWNER TO aboutme_runtime_owner;
ALTER TABLE public.oauth_authorization_codes OWNER TO aboutme_runtime_owner;
ALTER TABLE public.oauth_clients OWNER TO aboutme_runtime_owner;
ALTER TABLE public.oauth_grants OWNER TO aboutme_runtime_owner;
ALTER TABLE public.oauth_tokens OWNER TO aboutme_runtime_owner;
ALTER TABLE public.oauth_transactions OWNER TO aboutme_runtime_owner;
ALTER TABLE public.password_credentials OWNER TO aboutme_runtime_owner;
ALTER TABLE public.password_registrations OWNER TO aboutme_runtime_owner;
ALTER TABLE public.password_reset_tokens OWNER TO aboutme_runtime_owner;
ALTER TABLE public.privacy_sweep_state OWNER TO aboutme_runtime_owner;
ALTER TABLE public.public_state OWNER TO aboutme_runtime_owner;
ALTER TABLE public.resumes OWNER TO aboutme_runtime_owner;
ALTER TABLE public.sessions OWNER TO aboutme_runtime_owner;
ALTER TABLE public.slug_tombstones OWNER TO aboutme_runtime_owner;
ALTER TABLE public.users OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.enforce_resume_cap() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.notify_resume_revision() OWNER TO aboutme_runtime_owner;

GRANT SELECT ON pg_temp.runtime_v13_transfer_objects,
  pg_temp.runtime_v13_transfer_acl,
  pg_temp.runtime_v13_transfer_rows,
  pg_temp.runtime_v13_transfer_dependents,
  pg_temp.runtime_v13_transfer_triggers TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

DO $$
DECLARE runtime_oid oid := 'aboutme_runtime_owner'::regrole;
BEGIN
 IF EXISTS (
   SELECT 1 FROM pg_temp.runtime_v13_transfer_objects old
   LEFT JOIN LATERAL (
     SELECT c.oid,c.relname AS identity,c.relowner AS owner,
       concat_ws('|',c.relkind::text,c.relpersistence::text,c.relrowsecurity::text,c.relforcerowsecurity::text,
        (SELECT string_agg(concat_ws(':',a.attnum::text,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull::text,a.attidentity::text,a.attgenerated::text,COALESCE(pg_get_expr(d.adbin,d.adrelid),'')),',' ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped),
        (SELECT string_agg(pg_get_constraintdef(k.oid,true),',' ORDER BY k.conname) FROM pg_constraint k WHERE k.conrelid=c.oid)) AS definition
     FROM pg_class c WHERE old.kind='table' AND c.oid=old.oid
     UNION ALL SELECT p.oid,p.proname,p.proowner,pg_get_functiondef(p.oid) FROM pg_proc p WHERE old.kind='function' AND p.oid=old.oid
   ) current ON true
   WHERE current.oid IS NULL OR current.owner<>runtime_oid OR current.definition IS DISTINCT FROM old.definition
 ) OR EXISTS (
   SELECT 1 FROM pg_temp.runtime_v13_transfer_dependents old
   LEFT JOIN LATERAL (
     SELECT c.oid,CASE WHEN c.relkind IN ('i','I') THEN 'index' WHEN c.relkind='S' THEN 'sequence' END AS kind,
       c.relname AS identity,c.relowner AS owner,
       CASE WHEN c.relkind IN ('i','I') THEN pg_get_indexdef(c.oid)
            ELSE concat_ws('|',c.relkind::text,(SELECT concat_ws(',',s.seqtypid::text,s.seqstart::text,s.seqincrement::text,s.seqmax::text,s.seqmin::text,s.seqcache::text,s.seqcycle::text) FROM pg_sequence s WHERE s.seqrelid=c.oid)) END AS definition,
       parent.relname AS parent
     FROM pg_class c LEFT JOIN pg_index i ON i.indexrelid=c.oid LEFT JOIN pg_class parent ON parent.oid=COALESCE(i.indrelid,(SELECT d.refobjid FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='i' LIMIT 1))
     WHERE old.kind IN ('index','sequence') AND c.oid=old.oid
     UNION ALL
     SELECT t.oid,'type',t.typname,t.typowner,concat_ws('|',t.typtype::text,t.typrelid::text,t.typcategory::text),c.relname
     FROM pg_type t JOIN pg_class c ON c.oid=t.typrelid WHERE old.kind='type' AND t.oid=old.oid
   ) current ON true
   WHERE current.oid IS NULL OR current.kind<>old.kind OR current.identity<>old.identity
     OR current.owner<>runtime_oid OR current.definition IS DISTINCT FROM old.definition OR current.parent<>old.parent
 ) OR EXISTS (
   SELECT 1 FROM pg_temp.runtime_v13_transfer_triggers old
   LEFT JOIN LATERAL (
     SELECT t.oid,c.relname AS parent,t.tgname AS identity,t.tgenabled,t.tgconstraint,
       p.proname||'('||pg_get_function_identity_arguments(p.oid)||')' AS function_identity,
       pg_get_triggerdef(t.oid,true) AS definition
     FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.oid=old.oid
   ) current ON true
   WHERE current.oid IS NULL OR current.parent<>old.parent OR current.identity<>old.identity
     OR current.tgenabled<>old.tgenabled OR current.tgconstraint<>old.tgconstraint
     OR current.function_identity<>old.function_identity OR current.definition<>old.definition
 ) OR EXISTS (
   WITH current_acl AS (
     SELECT o.oid,'relation'::text AS scope,x.grantee,x.grantor,x.privilege_type,x.is_grantable
     FROM pg_temp.runtime_v13_transfer_objects o JOIN pg_class c ON c.oid=o.oid CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault('r',c.relowner))) x WHERE o.kind='table'
     UNION ALL SELECT o.oid,'column:'||a.attnum::text,x.grantee,x.grantor,x.privilege_type,x.is_grantable FROM pg_temp.runtime_v13_transfer_objects o JOIN pg_attribute a ON a.attrelid=o.oid CROSS JOIN LATERAL aclexplode(a.attacl) x WHERE o.kind='table' AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL
     UNION ALL SELECT o.oid,'function',x.grantee,x.grantor,x.privilege_type,x.is_grantable FROM pg_temp.runtime_v13_transfer_objects o JOIN pg_proc p ON p.oid=o.oid CROSS JOIN LATERAL aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) x WHERE o.kind='function'
   ), expected_acl AS (
     SELECT oid,scope,CASE WHEN grantee=source_owner THEN runtime_oid ELSE grantee END AS grantee,
       CASE WHEN grantor=source_owner THEN runtime_oid ELSE grantor END AS grantor,privilege_type,is_grantable
     FROM pg_temp.runtime_v13_transfer_acl
   )
   (SELECT * FROM current_acl EXCEPT SELECT * FROM expected_acl)
   UNION ALL
   (SELECT * FROM expected_acl EXCEPT SELECT * FROM current_acl)
 ) OR EXISTS (
   WITH current_rows AS (SELECT 'auth_email_jobs'::text AS identity,count(*)::bigint AS row_count FROM public.auth_email_jobs
UNION ALL
SELECT 'goose_db_version'::text AS identity,count(*)::bigint AS row_count FROM public.goose_db_version
UNION ALL
SELECT 'idempotency_records'::text AS identity,count(*)::bigint AS row_count FROM public.idempotency_records
UNION ALL
SELECT 'idempotency_usage'::text AS identity,count(*)::bigint AS row_count FROM public.idempotency_usage
UNION ALL
SELECT 'identities'::text AS identity,count(*)::bigint AS row_count FROM public.identities
UNION ALL
SELECT 'lifecycle_audit_events'::text AS identity,count(*)::bigint AS row_count FROM public.lifecycle_audit_events
UNION ALL
SELECT 'media_deletion_jobs'::text AS identity,count(*)::bigint AS row_count FROM public.media_deletion_jobs
UNION ALL
SELECT 'oauth_authorization_codes'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_authorization_codes
UNION ALL
SELECT 'oauth_clients'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_clients
UNION ALL
SELECT 'oauth_grants'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_grants
UNION ALL
SELECT 'oauth_tokens'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_tokens
UNION ALL
SELECT 'oauth_transactions'::text AS identity,count(*)::bigint AS row_count FROM public.oauth_transactions
UNION ALL
SELECT 'password_credentials'::text AS identity,count(*)::bigint AS row_count FROM public.password_credentials
UNION ALL
SELECT 'password_registrations'::text AS identity,count(*)::bigint AS row_count FROM public.password_registrations
UNION ALL
SELECT 'password_reset_tokens'::text AS identity,count(*)::bigint AS row_count FROM public.password_reset_tokens
UNION ALL
SELECT 'privacy_sweep_state'::text AS identity,count(*)::bigint AS row_count FROM public.privacy_sweep_state
UNION ALL
SELECT 'public_state'::text AS identity,count(*)::bigint AS row_count FROM public.public_state
UNION ALL
SELECT 'resumes'::text AS identity,count(*)::bigint AS row_count FROM public.resumes
UNION ALL
SELECT 'sessions'::text AS identity,count(*)::bigint AS row_count FROM public.sessions
UNION ALL
SELECT 'slug_tombstones'::text AS identity,count(*)::bigint AS row_count FROM public.slug_tombstones
UNION ALL
SELECT 'users'::text AS identity,count(*)::bigint AS row_count FROM public.users)
   (SELECT * FROM current_rows EXCEPT SELECT * FROM pg_temp.runtime_v13_transfer_rows)
   UNION ALL
   (SELECT * FROM pg_temp.runtime_v13_transfer_rows EXCEPT SELECT * FROM current_rows)
 ) THEN
   RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration manifest transfer mismatch';
 END IF;
END $$;

CREATE FUNCTION public.runtime_assert_migration_history_write()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE operation text; parsed_version bigint; marker_xid xid8;
BEGIN
  IF TG_OP <> 'INSERT' OR session_user<>'aboutme_migrator' THEN
    RAISE EXCEPTION USING ERRCODE='42501', MESSAGE='migration history operation denied';
  END IF;
  PERFORM public.runtime_validate_write_marker();
  SELECT operation_id,transaction_id INTO operation,marker_xid
    FROM pg_temp.runtime_write_entry_v1
   WHERE transaction_id=pg_current_xact_id() AND backend_pid=pg_backend_pid()
     AND session_role_oid=(session_user::regrole)::oid AND writer_kind='migrator'
     AND dirty AND finished;
  IF NOT FOUND OR (SELECT count(*) FROM pg_temp.runtime_write_entry_v1)<>1 OR marker_xid<>pg_current_xact_id()
     OR operation !~ '^migration-[0-9]{5,19}$' THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration history marker mismatch';
  END IF;
  BEGIN
    parsed_version := substring(operation from 11)::bigint;
  EXCEPTION WHEN numeric_value_out_of_range THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration history version mismatch';
  END;
  IF parsed_version<14 OR operation<>('migration-'||lpad(parsed_version::text,GREATEST(5,length(parsed_version::text)),'0'))
     OR NEW.version_id<>parsed_version OR NEW.is_applied IS DISTINCT FROM true THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration history version mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION public.runtime_deny_migration_history_write()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
  RAISE EXCEPTION USING ERRCODE='42501', MESSAGE='migration history operation denied';
END;
$$;

REVOKE ALL ON FUNCTION public.runtime_assert_migration_history_write() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_deny_migration_history_write() FROM PUBLIC;
CREATE TRIGGER goose_db_version_insert_guard BEFORE INSERT ON public.goose_db_version
FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_migration_history_write();
CREATE TRIGGER goose_db_version_update_delete_guard BEFORE UPDATE OR DELETE ON public.goose_db_version
FOR EACH ROW EXECUTE FUNCTION public.runtime_deny_migration_history_write();
CREATE TRIGGER goose_db_version_truncate_guard BEFORE TRUNCATE ON public.goose_db_version
FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_deny_migration_history_write();
REVOKE ALL ON TABLE public.goose_db_version FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;
REVOKE ALL PRIVILEGES (id,version_id,is_applied,tstamp) ON TABLE public.goose_db_version FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;
GRANT SELECT,INSERT ON TABLE public.goose_db_version TO aboutme_migrator;
DO $$
DECLARE history_oid oid := 'public.goose_db_version'::regclass; owner_oid oid := 'aboutme_runtime_owner'::regrole;
BEGIN
  IF EXISTS (SELECT 1 FROM pg_catalog.aclexplode((SELECT relacl FROM pg_catalog.pg_class WHERE oid=history_oid)) acl
    WHERE (acl.grantee=owner_oid AND (acl.privilege_type NOT IN ('SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN') OR acl.is_grantable))
       OR (acl.grantee='aboutme_migrator'::regrole AND (acl.privilege_type NOT IN ('SELECT','INSERT') OR acl.is_grantable))
       OR acl.grantee NOT IN (owner_oid,'aboutme_migrator'::regrole))
     OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute WHERE attrelid=history_oid AND attnum>0 AND NOT attisdropped AND attacl IS NOT NULL) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration history ACL mismatch';
  END IF;
END $$;
UPDATE public.runtime_write_state
   SET migrator_enforcement_version=1,migration_history_owner='aboutme_runtime_owner'
 WHERE singleton;
RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
DO $$ BEGIN
  IF pg_catalog.has_schema_privilege('aboutme_runtime_owner','public','CREATE') THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime owner retains schema CREATE';
  END IF;
END $$;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
