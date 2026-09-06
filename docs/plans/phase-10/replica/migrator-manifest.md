# Version-13 migration object manifest

## Authority and format

Frozen from migrations 00001 through 00013 at accepted B1 commit
`a5c47fa451ae14a8100571d5027c5b1a218ae4f3`. Root reran a fresh PostgreSQL 18.4
catalog probe and confirmed all 110 exact names and source owners, the 16
foundation owners, trigger dependencies and the Goose identity dependency. A
rollback-only transfer moved Goose's table, index, sequence and row type
together and restored them. The disposable database was dropped and verified
absent. Fresh migrator-owned convergence remains a B3 implementation test.

Canonical digest input is every nonblank `kind|public|identity` line below, in
bytewise order with one LF after each line. It contains 21 tables, 65 indexes,
21 row types, two functions and one sequence. The SHA-256 is:

`14b480b1e5739348ed8287b792135d4b305ec70d2314313475e47ff26bce4066`

B3 recomputes this digest from its fixed source. A changed manifest requires
review and an explicit plan correction; catalog queries cannot expand it.

## Transfer objects

These version-13 objects move from their exact expected source owner to
aboutme_runtime_owner. Existing local adoption expects aboutme. Fresh migration
00014 expects aboutme_migrator. No mixed source owner is accepted. The identity
sequence listed below follows its OWNED BY table dependency; validate its owner
after ALTER TABLE and do not issue an independent ALTER SEQUENCE OWNER if
PostgreSQL rejects ownership changes for an internal identity sequence.

```text
table|public|auth_email_jobs
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
function|public|enforce_resume_cap()
function|public|notify_resume_revision()
table|public|goose_db_version
sequence|public|goose_db_version_id_seq
```

## Table-owned dependent relations and types

ALTER TABLE OWNER transfers its indexes, constraints, TOAST relation, and row
type with the table. Validation still requires these explicit user-visible index
identities before and after transfer. Internal TOAST identities are excluded
because PostgreSQL assigns their names and dependencies.

```text
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
```

```text
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
```

These are 21 table composite row types: the 20 application tables plus Goose.
Validate pg_type.typrelid points to the exact manifest table and owner; ALTER
TABLE handles their ownership. There are no migration-created views,
materialized views, standalone application types, or application sequences at
version 13.

## Already-canonical foundation objects

Adoption and 00014 verify these are already owned by runtime_owner and never
include them in a source-owner transfer:

table public.runtime_write_state functions
public.runtime_assert_write_finished(), runtime_validate_write_marker(),
runtime_create_write_marker(), runtime_require_write_entry(),
runtime_enter_write(), runtime_assert_write_entry(),
runtime_assert_business_write(), runtime_finish_write(),
runtime_validate_migrator_marker(), runtime_enter_migrator(),
runtime_begin_migration_write(text), runtime_exit_migrator(),
runtime_read_migrator_metadata().

## Excluded objects

- Extension citext and every extension member from pg_depend deptype `e`.
- pg_catalog, information_schema, pg_toast, temporary schemas, PostgreSQL
  internals, and objects not named above.
- Trigger objects have no independent owner. Validate exact v13 triggers
  resumes_enforce_cap and resume_revision_notification plus their function
  dependencies; their table/function ownership converges through the manifest.
- Grants, default privileges, rows, identities, constraints, and definitions are
  validation inputs, not ownership-transfer wildcards.

## Validation algorithm

Materialize the manifest as fixed VALUES(kind,schema,name,signature) in code or
SQL. Before mutation, resolve every item by exact schema/name/kind/signature,
require one match and the expected source owner, and reject extra same-name kind
collisions. Separately query public user objects excluding extension members and
the enumerated runtime foundation set; reject any migration-managed object not
represented. Validate dependency ownership for every listed index/composite
type/trigger. Do not build ALTER statements from catalog names. Execute fixed
quoted ALTER statements generated from the reviewed manifest source.

After transfer, rerun the same queries requiring runtime_owner for every listed
owner-bearing object, unchanged OIDs, unchanged row counts, preserved non-owner
effective privileges, and unchanged definitions. Normalize only PostgreSQL's
expected old/new owner and grantor OID rewrite in ACL comparisons, plus the
explicit provisioning/Goose grants. Do not require byte-for-byte ACL equality.
Any mismatch aborts before runtime state metadata changes.
