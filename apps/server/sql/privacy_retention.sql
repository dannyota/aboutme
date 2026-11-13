-- name: TryLockIdempotencyExpirySweep :one
SELECT pg_try_advisory_lock(hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0));

-- name: UnlockIdempotencyExpirySweep :one
SELECT pg_advisory_unlock(hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0));

-- name: ExpireIdempotencyPage :one
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
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - (min(absolute_expires_at) - interval '90 days'), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM sessions
WHERE absolute_expires_at <= sqlc.arg(cutoff)::timestamptz
  AND (ua IS NOT NULL OR ip IS NOT NULL);

-- name: DeleteExpiredSessionsPage :execrows
-- Deletes session rows, revoked or not, whose absolute expiry is at or before
-- the cutoff, which the caller sets seven days before now. Rotation and reissue
-- copy absolute_expires_at unchanged, so every token in a rotation chain is
-- already dead by then.
--
-- The statement never waits for a row lock. It first locks, with SKIP LOCKED,
-- every row its delete reaches: the sessions, their expired pending
-- authentications, WebAuthn ceremonies (also those bound to such a pending
-- authentication) and TOTP enrollments, which ON DELETE CASCADE removes, and
-- the successors whose rotated_from ON DELETE SET NULL clears. A session whose
-- successor or referencing row it could not lock stays for a later page. So it
-- takes no user lock, cannot deadlock with sign-in, rotation, revocation or
-- account deletion, and never removes a live row.
--
-- This relies on no new row referencing a dead session: every path that
-- creates a pending authentication, ceremony or enrollment first locks its
-- session and requires it live (lockLiveSession). A reference committed after
-- this statement's snapshot would still be cascaded, and could make it wait.
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM sessions
    WHERE absolute_expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY absolute_expires_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
), pending AS MATERIALIZED (
    SELECT id
    FROM pending_authentications
    WHERE session_id = ANY (ARRAY(SELECT candidates.id FROM candidates))
      AND expires_at <= sqlc.arg(now)::timestamptz
    FOR UPDATE SKIP LOCKED
), ceremonies AS MATERIALIZED (
    SELECT id
    FROM webauthn_ceremonies
    WHERE (session_id = ANY (ARRAY(SELECT candidates.id FROM candidates))
           OR pending_authentication_id = ANY (ARRAY(SELECT pending.id FROM pending)))
      AND expires_at <= sqlc.arg(now)::timestamptz
    FOR UPDATE SKIP LOCKED
), enrollments AS MATERIALIZED (
    SELECT id
    FROM totp_enrollments
    WHERE session_id = ANY (ARRAY(SELECT candidates.id FROM candidates))
      AND expires_at <= sqlc.arg(now)::timestamptz
    FOR UPDATE SKIP LOCKED
), doomed AS MATERIALIZED (
    SELECT candidate.id
    FROM candidates AS candidate
    WHERE NOT EXISTS (
            SELECT 1 FROM sessions AS successor
            WHERE successor.rotated_from = candidate.id
              AND successor.id NOT IN (SELECT candidates.id FROM candidates))
      AND NOT EXISTS (
            SELECT 1 FROM pending_authentications AS bound
            WHERE bound.session_id = candidate.id
              AND bound.id NOT IN (SELECT pending.id FROM pending))
      AND NOT EXISTS (
            SELECT 1
            FROM webauthn_ceremonies AS ceremony
            LEFT JOIN pending_authentications AS bound
                ON bound.id = ceremony.pending_authentication_id
            WHERE (ceremony.session_id = candidate.id OR bound.session_id = candidate.id)
              AND ceremony.id NOT IN (SELECT ceremonies.id FROM ceremonies))
      AND NOT EXISTS (
            SELECT 1 FROM totp_enrollments AS enrollment
            WHERE enrollment.session_id = candidate.id
              AND enrollment.id NOT IN (SELECT enrollments.id FROM enrollments))
)
DELETE FROM sessions AS session
USING doomed
WHERE session.id = doomed.id;

-- name: GetExpiredSessionsBacklog :one
SELECT count(*)::bigint AS backlog,
       (COALESCE(floor(extract(epoch FROM
           greatest(sqlc.arg(now)::timestamptz - (min(absolute_expires_at) - interval '90 days'), interval '0 seconds'))), 0))::bigint
           AS oldest_age_seconds
FROM sessions
WHERE absolute_expires_at <= sqlc.arg(cutoff)::timestamptz;

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
