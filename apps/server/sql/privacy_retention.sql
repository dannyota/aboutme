-- Phase 8 privacy retention queries. Every mutation is bounded and ordered;
-- command-level advisory locks are session locks held on a dedicated pooled
-- connection for the whole run.

-- name: TryLockIdempotencyExpirySweep :one
SELECT pg_try_advisory_lock(hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0));

-- name: UnlockIdempotencyExpirySweep :one
SELECT pg_advisory_unlock(hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0));

-- name: ExpireIdempotencyPage :one
-- Lock users before replay rows and usage counters, matching every resume
-- writer. SKIP LOCKED lets inactive users progress past a busy account.
WITH candidate_users AS MATERIALIZED (
    SELECT owner.id
    FROM users AS owner
    JOIN LATERAL (
        SELECT record.expires_at, record.id
        FROM idempotency_records AS record
        WHERE record.user_id = owner.id
          AND record.expires_at <= sqlc.arg(cutoff)::timestamptz
        ORDER BY record.expires_at, record.id
        LIMIT 1
    ) AS oldest ON true
    ORDER BY oldest.expires_at, oldest.id, owner.id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE OF owner SKIP LOCKED
), doomed AS MATERIALIZED (
    SELECT record.id, record.user_id
    FROM idempotency_records AS record
    JOIN candidate_users AS owner ON owner.id = record.user_id
    WHERE record.expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY record.expires_at, record.id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE OF record SKIP LOCKED
), deleted AS (
    DELETE FROM idempotency_records AS record
    USING doomed
    WHERE record.id = doomed.id
    RETURNING record.user_id, record.response_body, record.response_headers
), per_user AS MATERIALIZED (
    SELECT user_id,
           count(*)::bigint AS deleted_records,
           sum(octet_length(response_body::text) +
               octet_length(response_headers::text))::bigint AS deleted_bytes
    FROM deleted
    GROUP BY user_id
), released AS (
    UPDATE idempotency_usage AS usage
    SET retained_records = usage.retained_records - per_user.deleted_records,
        stored_bytes = usage.stored_bytes - per_user.deleted_bytes
    FROM per_user
    WHERE usage.user_id = per_user.user_id
    RETURNING usage.user_id
)
SELECT COALESCE((SELECT sum(deleted_records) FROM per_user), 0)::bigint AS deleted_records,
       COALESCE((SELECT sum(deleted_bytes) FROM per_user), 0)::bigint AS deleted_bytes,
       COALESCE((SELECT sum(per_user.deleted_records)
                 FROM per_user JOIN released USING (user_id)), 0)::bigint AS released_records,
       COALESCE((SELECT sum(per_user.deleted_bytes)
                 FROM per_user JOIN released USING (user_id)), 0)::bigint AS released_bytes;

-- name: GetIdempotencyExpiryBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(cutoff)::timestamptz - min(expires_at), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM idempotency_records
WHERE expires_at <= sqlc.arg(cutoff)::timestamptz;

-- name: TryLockPrivacyRetentionSweep :one
SELECT pg_try_advisory_lock(hashtextextended('aboutme.privacy-retention-sweep.v1', 0));

-- name: UnlockPrivacyRetentionSweep :one
SELECT pg_advisory_unlock(hashtextextended('aboutme.privacy-retention-sweep.v1', 0));

-- name: RedactSessionMetadataPage :execrows
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM sessions
    WHERE created_at <= sqlc.arg(cutoff)::timestamptz
      AND (ua IS NOT NULL OR ip IS NOT NULL)
    ORDER BY created_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
UPDATE sessions AS session
SET ua = NULL, ip = NULL
FROM candidates
WHERE session.id = candidates.id;

-- name: GetSessionMetadataBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - min(created_at), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM sessions
WHERE created_at <= sqlc.arg(cutoff)::timestamptz
  AND (ua IS NOT NULL OR ip IS NOT NULL);

-- name: DeleteLifecycleAuditPage :execrows
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM lifecycle_audit_events
    WHERE occurred_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY occurred_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM lifecycle_audit_events AS event
USING candidates
WHERE event.id = candidates.id;

-- name: GetLifecycleAuditBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - min(occurred_at), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM lifecycle_audit_events
WHERE occurred_at <= sqlc.arg(cutoff)::timestamptz;

-- name: DeleteCompletedMediaJobsPage :execrows
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM media_deletion_jobs
    WHERE completed_at IS NOT NULL
      AND completed_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY completed_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM media_deletion_jobs AS job
USING candidates
WHERE job.id = candidates.id;

-- name: GetCompletedMediaJobsBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - min(completed_at), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM media_deletion_jobs
WHERE completed_at IS NOT NULL
  AND completed_at <= sqlc.arg(cutoff)::timestamptz;
