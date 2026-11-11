-- Privacy retention queries. Every mutation is bounded and ordered;
-- command-level advisory locks are session locks held on a dedicated pooled
-- connection for the whole run.

-- name: TryLockIdempotencyExpirySweep :one
SELECT pg_try_advisory_lock(hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0));

-- name: UnlockIdempotencyExpirySweep :one
SELECT pg_advisory_unlock(hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0));

-- name: ExpireIdempotencyPage :one
-- Candidates come from expired records in (expires_at, id) order, not from
-- every account. Each record's user is locked before any replay row or usage
-- counter, matching every resume writer. LIMIT counts only rows whose user
-- lock succeeded, so SKIP LOCKED passes over a busy user's records and later
-- users still fill the page. ARRAY() reads every locked row before the first
-- replay row is locked.
WITH locked AS MATERIALIZED (
    SELECT record.id
    FROM idempotency_records AS record
    JOIN users AS owner ON owner.id = record.user_id
    WHERE record.expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY record.expires_at, record.id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE OF owner SKIP LOCKED
), doomed AS MATERIALIZED (
    SELECT record.id, record.user_id
    FROM idempotency_records AS record
    WHERE record.id = ANY (ARRAY(SELECT locked.id FROM locked))
      AND record.expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY record.expires_at, record.id
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
-- Session IP and user agent are redacted before the session's absolute expiry
-- (docs/design/operations.md), not by created_at, so a rotation or reissue
-- successor that inherits the original absolute expiry cannot extend how long
-- its metadata survives. The caller's cutoff already carries the redaction
-- lead ahead of now, so this query only compares absolute_expires_at to it.
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM sessions
    WHERE absolute_expires_at <= sqlc.arg(cutoff)::timestamptz
      AND (ua IS NOT NULL OR ip IS NOT NULL)
    ORDER BY absolute_expires_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
UPDATE sessions AS session
SET ua = NULL, ip = NULL
FROM candidates
WHERE session.id = candidates.id;

-- name: GetSessionMetadataBacklog :one
-- oldest_age_seconds is age since the original sign-in, not since
-- absolute_expires_at, matching every other *_oldest_seconds field: a
-- session's absolute expiry is sign-in plus the fixed 90-day absolute timeout
-- (docs/design/security.md), so subtracting that interval back out of
-- absolute_expires_at recovers the sign-in time.
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - (min(absolute_expires_at) - interval '90 days'), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM sessions
WHERE absolute_expires_at <= sqlc.arg(cutoff)::timestamptz
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

-- name: DeleteAuthenticationSecurityEventsPage :execrows
WITH candidates AS MATERIALIZED (
    SELECT id FROM authentication_security_events
    WHERE occurred_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY occurred_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM authentication_security_events AS event
USING candidates
WHERE event.id = candidates.id;

-- name: GetAuthenticationSecurityEventsBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - min(occurred_at), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM authentication_security_events
WHERE occurred_at <= sqlc.arg(cutoff)::timestamptz;

-- name: DeleteExpiredSlugTombstonesPage :execrows
-- A released slug stays reserved and unlinked from any account for 180 days
-- (docs/design/product.md), then the privacy sweep deletes the reservation.
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM slug_tombstones
    WHERE released_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY released_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM slug_tombstones AS tombstone
USING candidates
WHERE tombstone.id = candidates.id;

-- name: GetExpiredSlugTombstonesBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - min(released_at), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM slug_tombstones
WHERE released_at <= sqlc.arg(cutoff)::timestamptz;

-- name: DeleteExpiredProjectSlugHoldsPage :execrows
WITH doomed AS (
    SELECT user_id, slug FROM project_slug_holds
    WHERE released_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY released_at, user_id, slug
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM project_slug_holds AS hold
USING doomed WHERE hold.user_id = doomed.user_id AND hold.slug = doomed.slug;
