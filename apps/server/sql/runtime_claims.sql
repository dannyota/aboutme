-- name: RuntimeAcquireSingleClaim :one
WITH result AS MATERIALIZED (SELECT public.runtime_acquire_single_claim(sqlc.arg(claim_id)::uuid,sqlc.arg(policy_id)::text,sqlc.arg(replica_id)::uuid,sqlc.narg(work_id)::uuid,sqlc.arg(scope_kind)::text,sqlc.arg(scope_digest)::bytea) AS r)
SELECT
  (result.r).outcome::text AS outcome,
  (result.r).claim_id::uuid AS claim_id,
  COALESCE((result.r).policy_id,''::text)::text AS policy_id_value, ((result.r).policy_id IS NOT NULL)::boolean AS policy_id_present,
  COALESCE((result.r).replica_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS replica_id_value, ((result.r).replica_id IS NOT NULL)::boolean AS replica_id_present,
  COALESCE((result.r).work_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS work_id_value, ((result.r).work_id IS NOT NULL)::boolean AS work_id_present,
  COALESCE((result.r).state,''::text)::text AS state_value, ((result.r).state IS NOT NULL)::boolean AS state_present,
  COALESCE((result.r).admitted_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS admitted_at_value, ((result.r).admitted_at IS NOT NULL)::boolean AS admitted_at_present,
  COALESCE((result.r).deadline_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS deadline_at_value, ((result.r).deadline_at IS NOT NULL)::boolean AS deadline_at_present,
  COALESCE((result.r).released_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS released_at_value, ((result.r).released_at IS NOT NULL)::boolean AS released_at_present,
  COALESCE((result.r).release_reason,''::text)::text AS release_reason_value, ((result.r).release_reason IS NOT NULL)::boolean AS release_reason_present,
  COALESCE((result.r).request_digest,''::bytea)::bytea AS request_digest_value, ((result.r).request_digest IS NOT NULL)::boolean AS request_digest_present,
  COALESCE((result.r).scope_count,0::smallint)::smallint AS scope_count_value, ((result.r).scope_count IS NOT NULL)::boolean AS scope_count_present,
  COALESCE((result.r).scope_1_kind,''::text)::text AS scope_1_kind_value, ((result.r).scope_1_kind IS NOT NULL)::boolean AS scope_1_kind_present,
  COALESCE((result.r).scope_1_digest,''::bytea)::bytea AS scope_1_digest_value, ((result.r).scope_1_digest IS NOT NULL)::boolean AS scope_1_digest_present,
  COALESCE((result.r).scope_1_allocation_ordinal,0::bigint)::bigint AS scope_1_allocation_ordinal_value, ((result.r).scope_1_allocation_ordinal IS NOT NULL)::boolean AS scope_1_allocation_ordinal_present,
  COALESCE((result.r).scope_2_kind,''::text)::text AS scope_2_kind_value, ((result.r).scope_2_kind IS NOT NULL)::boolean AS scope_2_kind_present,
  COALESCE((result.r).scope_2_digest,''::bytea)::bytea AS scope_2_digest_value, ((result.r).scope_2_digest IS NOT NULL)::boolean AS scope_2_digest_present,
  COALESCE((result.r).scope_2_allocation_ordinal,0::bigint)::bigint AS scope_2_allocation_ordinal_value, ((result.r).scope_2_allocation_ordinal IS NOT NULL)::boolean AS scope_2_allocation_ordinal_present
FROM result;

-- name: RuntimeAcquireSSEClaim :one
WITH result AS MATERIALIZED (SELECT public.runtime_acquire_sse_claim(sqlc.arg(claim_id)::uuid,sqlc.arg(replica_id)::uuid,sqlc.arg(ip_digest)::bytea,sqlc.narg(account_digest)::bytea) AS r)
SELECT
  (result.r).outcome::text AS outcome,
  (result.r).claim_id::uuid AS claim_id,
  COALESCE((result.r).policy_id,''::text)::text AS policy_id_value, ((result.r).policy_id IS NOT NULL)::boolean AS policy_id_present,
  COALESCE((result.r).replica_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS replica_id_value, ((result.r).replica_id IS NOT NULL)::boolean AS replica_id_present,
  COALESCE((result.r).work_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS work_id_value, ((result.r).work_id IS NOT NULL)::boolean AS work_id_present,
  COALESCE((result.r).state,''::text)::text AS state_value, ((result.r).state IS NOT NULL)::boolean AS state_present,
  COALESCE((result.r).admitted_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS admitted_at_value, ((result.r).admitted_at IS NOT NULL)::boolean AS admitted_at_present,
  COALESCE((result.r).deadline_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS deadline_at_value, ((result.r).deadline_at IS NOT NULL)::boolean AS deadline_at_present,
  COALESCE((result.r).released_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS released_at_value, ((result.r).released_at IS NOT NULL)::boolean AS released_at_present,
  COALESCE((result.r).release_reason,''::text)::text AS release_reason_value, ((result.r).release_reason IS NOT NULL)::boolean AS release_reason_present,
  COALESCE((result.r).request_digest,''::bytea)::bytea AS request_digest_value, ((result.r).request_digest IS NOT NULL)::boolean AS request_digest_present,
  COALESCE((result.r).scope_count,0::smallint)::smallint AS scope_count_value, ((result.r).scope_count IS NOT NULL)::boolean AS scope_count_present,
  COALESCE((result.r).scope_1_kind,''::text)::text AS scope_1_kind_value, ((result.r).scope_1_kind IS NOT NULL)::boolean AS scope_1_kind_present,
  COALESCE((result.r).scope_1_digest,''::bytea)::bytea AS scope_1_digest_value, ((result.r).scope_1_digest IS NOT NULL)::boolean AS scope_1_digest_present,
  COALESCE((result.r).scope_1_allocation_ordinal,0::bigint)::bigint AS scope_1_allocation_ordinal_value, ((result.r).scope_1_allocation_ordinal IS NOT NULL)::boolean AS scope_1_allocation_ordinal_present,
  COALESCE((result.r).scope_2_kind,''::text)::text AS scope_2_kind_value, ((result.r).scope_2_kind IS NOT NULL)::boolean AS scope_2_kind_present,
  COALESCE((result.r).scope_2_digest,''::bytea)::bytea AS scope_2_digest_value, ((result.r).scope_2_digest IS NOT NULL)::boolean AS scope_2_digest_present,
  COALESCE((result.r).scope_2_allocation_ordinal,0::bigint)::bigint AS scope_2_allocation_ordinal_value, ((result.r).scope_2_allocation_ordinal IS NOT NULL)::boolean AS scope_2_allocation_ordinal_present
FROM result;

-- name: RuntimePromoteClaim :one
WITH result AS MATERIALIZED (SELECT public.runtime_promote_claim(sqlc.arg(claim_id)::uuid,sqlc.arg(expected_replica_id)::uuid,sqlc.arg(request_digest)::bytea) AS r)
SELECT
  (result.r).outcome::text AS outcome,
  (result.r).claim_id::uuid AS claim_id,
  COALESCE((result.r).policy_id,''::text)::text AS policy_id_value, ((result.r).policy_id IS NOT NULL)::boolean AS policy_id_present,
  COALESCE((result.r).replica_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS replica_id_value, ((result.r).replica_id IS NOT NULL)::boolean AS replica_id_present,
  COALESCE((result.r).work_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS work_id_value, ((result.r).work_id IS NOT NULL)::boolean AS work_id_present,
  COALESCE((result.r).state,''::text)::text AS state_value, ((result.r).state IS NOT NULL)::boolean AS state_present,
  COALESCE((result.r).admitted_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS admitted_at_value, ((result.r).admitted_at IS NOT NULL)::boolean AS admitted_at_present,
  COALESCE((result.r).deadline_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS deadline_at_value, ((result.r).deadline_at IS NOT NULL)::boolean AS deadline_at_present,
  COALESCE((result.r).released_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS released_at_value, ((result.r).released_at IS NOT NULL)::boolean AS released_at_present,
  COALESCE((result.r).release_reason,''::text)::text AS release_reason_value, ((result.r).release_reason IS NOT NULL)::boolean AS release_reason_present,
  COALESCE((result.r).request_digest,''::bytea)::bytea AS request_digest_value, ((result.r).request_digest IS NOT NULL)::boolean AS request_digest_present,
  COALESCE((result.r).scope_count,0::smallint)::smallint AS scope_count_value, ((result.r).scope_count IS NOT NULL)::boolean AS scope_count_present,
  COALESCE((result.r).scope_1_kind,''::text)::text AS scope_1_kind_value, ((result.r).scope_1_kind IS NOT NULL)::boolean AS scope_1_kind_present,
  COALESCE((result.r).scope_1_digest,''::bytea)::bytea AS scope_1_digest_value, ((result.r).scope_1_digest IS NOT NULL)::boolean AS scope_1_digest_present,
  COALESCE((result.r).scope_1_allocation_ordinal,0::bigint)::bigint AS scope_1_allocation_ordinal_value, ((result.r).scope_1_allocation_ordinal IS NOT NULL)::boolean AS scope_1_allocation_ordinal_present,
  COALESCE((result.r).scope_2_kind,''::text)::text AS scope_2_kind_value, ((result.r).scope_2_kind IS NOT NULL)::boolean AS scope_2_kind_present,
  COALESCE((result.r).scope_2_digest,''::bytea)::bytea AS scope_2_digest_value, ((result.r).scope_2_digest IS NOT NULL)::boolean AS scope_2_digest_present,
  COALESCE((result.r).scope_2_allocation_ordinal,0::bigint)::bigint AS scope_2_allocation_ordinal_value, ((result.r).scope_2_allocation_ordinal IS NOT NULL)::boolean AS scope_2_allocation_ordinal_present
FROM result;

-- name: RuntimeReleaseClaim :one
WITH result AS MATERIALIZED (SELECT public.runtime_release_claim(sqlc.arg(claim_id)::uuid,sqlc.arg(expected_replica_id)::uuid,sqlc.arg(request_digest)::bytea,sqlc.arg(reason)::text) AS r)
SELECT
  (result.r).outcome::text AS outcome,
  (result.r).claim_id::uuid AS claim_id,
  COALESCE((result.r).policy_id,''::text)::text AS policy_id_value, ((result.r).policy_id IS NOT NULL)::boolean AS policy_id_present,
  COALESCE((result.r).replica_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS replica_id_value, ((result.r).replica_id IS NOT NULL)::boolean AS replica_id_present,
  COALESCE((result.r).work_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS work_id_value, ((result.r).work_id IS NOT NULL)::boolean AS work_id_present,
  COALESCE((result.r).state,''::text)::text AS state_value, ((result.r).state IS NOT NULL)::boolean AS state_present,
  COALESCE((result.r).admitted_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS admitted_at_value, ((result.r).admitted_at IS NOT NULL)::boolean AS admitted_at_present,
  COALESCE((result.r).deadline_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS deadline_at_value, ((result.r).deadline_at IS NOT NULL)::boolean AS deadline_at_present,
  COALESCE((result.r).released_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS released_at_value, ((result.r).released_at IS NOT NULL)::boolean AS released_at_present,
  COALESCE((result.r).release_reason,''::text)::text AS release_reason_value, ((result.r).release_reason IS NOT NULL)::boolean AS release_reason_present,
  COALESCE((result.r).request_digest,''::bytea)::bytea AS request_digest_value, ((result.r).request_digest IS NOT NULL)::boolean AS request_digest_present,
  COALESCE((result.r).scope_count,0::smallint)::smallint AS scope_count_value, ((result.r).scope_count IS NOT NULL)::boolean AS scope_count_present,
  COALESCE((result.r).scope_1_kind,''::text)::text AS scope_1_kind_value, ((result.r).scope_1_kind IS NOT NULL)::boolean AS scope_1_kind_present,
  COALESCE((result.r).scope_1_digest,''::bytea)::bytea AS scope_1_digest_value, ((result.r).scope_1_digest IS NOT NULL)::boolean AS scope_1_digest_present,
  COALESCE((result.r).scope_1_allocation_ordinal,0::bigint)::bigint AS scope_1_allocation_ordinal_value, ((result.r).scope_1_allocation_ordinal IS NOT NULL)::boolean AS scope_1_allocation_ordinal_present,
  COALESCE((result.r).scope_2_kind,''::text)::text AS scope_2_kind_value, ((result.r).scope_2_kind IS NOT NULL)::boolean AS scope_2_kind_present,
  COALESCE((result.r).scope_2_digest,''::bytea)::bytea AS scope_2_digest_value, ((result.r).scope_2_digest IS NOT NULL)::boolean AS scope_2_digest_present,
  COALESCE((result.r).scope_2_allocation_ordinal,0::bigint)::bigint AS scope_2_allocation_ordinal_value, ((result.r).scope_2_allocation_ordinal IS NOT NULL)::boolean AS scope_2_allocation_ordinal_present
FROM result;

-- name: RuntimeResolveClaim :one
WITH result AS MATERIALIZED (SELECT public.runtime_resolve_claim(sqlc.arg(claim_id)::uuid,sqlc.arg(expected_replica_id)::uuid,sqlc.arg(request_digest)::bytea) AS r)
SELECT
  (result.r).outcome::text AS outcome,
  (result.r).claim_id::uuid AS claim_id,
  COALESCE((result.r).policy_id,''::text)::text AS policy_id_value, ((result.r).policy_id IS NOT NULL)::boolean AS policy_id_present,
  COALESCE((result.r).replica_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS replica_id_value, ((result.r).replica_id IS NOT NULL)::boolean AS replica_id_present,
  COALESCE((result.r).work_id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS work_id_value, ((result.r).work_id IS NOT NULL)::boolean AS work_id_present,
  COALESCE((result.r).state,''::text)::text AS state_value, ((result.r).state IS NOT NULL)::boolean AS state_present,
  COALESCE((result.r).admitted_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS admitted_at_value, ((result.r).admitted_at IS NOT NULL)::boolean AS admitted_at_present,
  COALESCE((result.r).deadline_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS deadline_at_value, ((result.r).deadline_at IS NOT NULL)::boolean AS deadline_at_present,
  COALESCE((result.r).released_at,'1970-01-01 UTC'::timestamptz)::timestamptz AS released_at_value, ((result.r).released_at IS NOT NULL)::boolean AS released_at_present,
  COALESCE((result.r).release_reason,''::text)::text AS release_reason_value, ((result.r).release_reason IS NOT NULL)::boolean AS release_reason_present,
  COALESCE((result.r).request_digest,''::bytea)::bytea AS request_digest_value, ((result.r).request_digest IS NOT NULL)::boolean AS request_digest_present,
  COALESCE((result.r).scope_count,0::smallint)::smallint AS scope_count_value, ((result.r).scope_count IS NOT NULL)::boolean AS scope_count_present,
  COALESCE((result.r).scope_1_kind,''::text)::text AS scope_1_kind_value, ((result.r).scope_1_kind IS NOT NULL)::boolean AS scope_1_kind_present,
  COALESCE((result.r).scope_1_digest,''::bytea)::bytea AS scope_1_digest_value, ((result.r).scope_1_digest IS NOT NULL)::boolean AS scope_1_digest_present,
  COALESCE((result.r).scope_1_allocation_ordinal,0::bigint)::bigint AS scope_1_allocation_ordinal_value, ((result.r).scope_1_allocation_ordinal IS NOT NULL)::boolean AS scope_1_allocation_ordinal_present,
  COALESCE((result.r).scope_2_kind,''::text)::text AS scope_2_kind_value, ((result.r).scope_2_kind IS NOT NULL)::boolean AS scope_2_kind_present,
  COALESCE((result.r).scope_2_digest,''::bytea)::bytea AS scope_2_digest_value, ((result.r).scope_2_digest IS NOT NULL)::boolean AS scope_2_digest_present,
  COALESCE((result.r).scope_2_allocation_ordinal,0::bigint)::bigint AS scope_2_allocation_ordinal_value, ((result.r).scope_2_allocation_ordinal IS NOT NULL)::boolean AS scope_2_allocation_ordinal_present
FROM result;

-- name: RuntimeGCReleasedClaimReceipts :one
WITH result AS MATERIALIZED (
  SELECT gc FROM public.runtime_gc_released_claim_receipts() AS gc
)
SELECT (result.gc).deleted_claim_count::integer AS deleted_claim_count,
  (result.gc).deleted_scope_summary_count::integer AS deleted_scope_summary_count
FROM result;
