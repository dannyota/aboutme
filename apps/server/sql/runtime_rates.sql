-- name: RuntimeAdmitTokenRate :one
WITH result AS MATERIALIZED (SELECT public.runtime_admit_token_rate(sqlc.arg(policy_id)::text,sqlc.arg(key_digest)::bytea) AS r)
SELECT
  (result.r).allowed::boolean AS allowed,
  (result.r).retry_after_seconds::integer AS retry_after_seconds,
  (result.r).bucket_kind::text AS bucket_kind,
  COALESCE((result.r).partition,0::smallint)::smallint AS partition_value, ((result.r).partition IS NOT NULL)::boolean AS partition_present,
  (result.r).effective_at::timestamptz AS effective_at
FROM result;

-- name: RuntimePasswordFailureState :one
WITH result AS MATERIALIZED (SELECT public.runtime_password_failure_state(sqlc.arg(key_digest)::bytea) AS r)
SELECT
  (result.r).exhausted::boolean AS exhausted,
  (result.r).retry_after_seconds::integer AS retry_after_seconds,
  (result.r).bucket_kind::text AS bucket_kind,
  COALESCE((result.r).partition,0::smallint)::smallint AS partition_value, ((result.r).partition IS NOT NULL)::boolean AS partition_present,
  (result.r).effective_at::timestamptz AS effective_at,
  (result.r).allocated::boolean AS allocated,
  (result.r).activity_refreshed::boolean AS activity_refreshed
FROM result;

-- name: RuntimeRecordPasswordFailure :one
WITH result AS MATERIALIZED (SELECT public.runtime_record_password_failure(sqlc.arg(key_digest)::bytea) AS r)
SELECT
  (result.r).exhausted::boolean AS exhausted,
  (result.r).retry_after_seconds::integer AS retry_after_seconds,
  (result.r).bucket_kind::text AS bucket_kind,
  COALESCE((result.r).partition,0::smallint)::smallint AS partition_value, ((result.r).partition IS NOT NULL)::boolean AS partition_present,
  (result.r).effective_at::timestamptz AS effective_at,
  (result.r).allocated::boolean AS allocated,
  (result.r).activity_refreshed::boolean AS activity_refreshed
FROM result;

-- name: RuntimeClearPasswordFailure :one
WITH result AS MATERIALIZED (SELECT public.runtime_clear_password_failure(sqlc.arg(key_digest)::bytea) AS r)
SELECT
  (result.r).cleared::boolean AS cleared,
  (result.r).bucket_kind::text AS bucket_kind,
  COALESCE((result.r).partition,0::smallint)::smallint AS partition_value, ((result.r).partition IS NOT NULL)::boolean AS partition_present
FROM result;

-- name: RuntimeAdmitSlugChange :one
WITH result AS MATERIALIZED (SELECT public.runtime_admit_slug_change(sqlc.arg(key_digest)::bytea) AS r)
SELECT
  (result.r).allowed::boolean AS allowed,
  (result.r).retry_after_seconds::integer AS retry_after_seconds,
  (result.r).bucket_kind::text AS bucket_kind,
  COALESCE((result.r).partition,0::smallint)::smallint AS partition_value, ((result.r).partition IS NOT NULL)::boolean AS partition_present,
  (result.r).effective_at::timestamptz AS effective_at
FROM result;

-- name: RuntimeReserveOAuthFailedGrant :one
WITH result AS MATERIALIZED (SELECT public.runtime_reserve_oauth_failed_grant(sqlc.arg(attempt_id)::uuid,sqlc.arg(client_id)::uuid) AS r)
SELECT
  (result.r).allowed::boolean AS allowed,
  (result.r).retry_after_seconds::integer AS retry_after_seconds,
  COALESCE((result.r).bucket_kind,''::text)::text AS bucket_kind_value, ((result.r).bucket_kind IS NOT NULL)::boolean AS bucket_kind_present,
  COALESCE((result.r).partition,0::smallint)::smallint AS partition_value, ((result.r).partition IS NOT NULL)::boolean AS partition_present,
  (result.r).replayed::boolean AS replayed
FROM result;

-- name: RuntimeFinishAdmissionAttempt :one
WITH result AS MATERIALIZED (SELECT public.runtime_finish_admission_attempt(sqlc.arg(attempt_id)::uuid,sqlc.arg(outcome)::text) AS r)
SELECT
  (result.r).resolution::text AS resolution,
  COALESCE((result.r).stored_outcome,''::text)::text AS stored_outcome_value, ((result.r).stored_outcome IS NOT NULL)::boolean AS stored_outcome_present
FROM result;

-- name: RuntimeCleanupAdmissionAttemptReceipts :one
WITH result AS MATERIALIZED (SELECT public.runtime_cleanup_admission_attempt_receipts(sqlc.arg(page_size)::integer) AS r)
SELECT (result.r).deleted_count::integer AS deleted_count
FROM result;

-- name: RuntimeCleanupRateBuckets :one
WITH result AS MATERIALIZED (SELECT public.runtime_cleanup_rate_buckets(sqlc.arg(policy_id)::text,sqlc.arg(page_size)::integer) AS r)
SELECT
  (result.r).deleted_count::integer AS deleted_count,
  (result.r).effective_at::timestamptz AS effective_at,
  (result.r).policy_idle::boolean AS policy_idle
FROM result;
