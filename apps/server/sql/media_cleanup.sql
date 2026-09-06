-- Media deletion and orphan-reconciliation queries. The document photo key is
-- the ownership authority; media_deletion_jobs is only durable cleanup work.

-- name: ClaimMediaDeletionJobs :many
WITH candidates AS MATERIALIZED (
    SELECT job.id
    FROM media_deletion_jobs AS job
    WHERE job.completed_at IS NULL
      AND job.next_attempt_at <= sqlc.arg(due_at)::timestamptz
      AND (job.lease_id IS NULL OR job.lease_expires_at <= sqlc.arg(now)::timestamptz)
    ORDER BY job.next_attempt_at, job.id
    LIMIT LEAST(sqlc.arg(max_rows)::int, 200)
    FOR UPDATE SKIP LOCKED
)
UPDATE media_deletion_jobs AS job
SET lease_id = sqlc.arg(lease_id)::uuid,
    lease_expires_at = sqlc.arg(lease_expires_at)::timestamptz,
    next_attempt_at = sqlc.arg(lease_expires_at)::timestamptz,
    attempt_count = job.attempt_count + 1
FROM candidates
WHERE job.id = candidates.id
RETURNING job.*;

-- name: MarkClaimedMediaDeletionOverdue :one
WITH marked AS (
    UPDATE media_deletion_jobs AS job
    SET overdue_at = sqlc.arg(now)::timestamptz
    WHERE job.id = sqlc.arg(id)::uuid
      AND job.lease_id = sqlc.arg(lease_id)::uuid
      AND job.completed_at IS NULL
      AND job.overdue_at IS NULL
      AND job.enqueued_at <= sqlc.arg(cutoff)::timestamptz
    RETURNING job.id
), audited AS (
    INSERT INTO lifecycle_audit_events (kind, occurred_at, media_job_id)
    SELECT 'media_deletion_overdue', sqlc.arg(now)::timestamptz, marked.id
    FROM marked
    ON CONFLICT (media_job_id, kind) DO NOTHING
    RETURNING id
)
SELECT EXISTS (SELECT 1 FROM marked) AS marked;

-- name: MarkMediaDeletionOverduePage :one
WITH candidates AS MATERIALIZED (
    SELECT job.id
    FROM media_deletion_jobs AS job
    WHERE job.completed_at IS NULL
      AND job.overdue_at IS NULL
      AND job.enqueued_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY job.enqueued_at, job.id
    LIMIT LEAST(sqlc.arg(max_rows)::int, 200)
    FOR UPDATE SKIP LOCKED
), marked AS (
    UPDATE media_deletion_jobs AS job
    SET overdue_at = sqlc.arg(now)::timestamptz
    FROM candidates
    WHERE job.id = candidates.id
    RETURNING job.id
), audited AS (
    INSERT INTO lifecycle_audit_events (kind, occurred_at, media_job_id)
    SELECT 'media_deletion_overdue', sqlc.arg(now)::timestamptz, marked.id
    FROM marked
    ON CONFLICT (media_job_id, kind) DO NOTHING
    RETURNING id
)
SELECT count(*)::bigint AS marked FROM marked;

-- name: MediaObjectHasLiveReference :one
SELECT EXISTS (
    SELECT 1
    FROM resumes
    WHERE personal_details -> 'photo' ->> 'key' = sqlc.arg(object_key)::text
) AS live;

-- name: CompleteClaimedMediaDeletion :one
WITH completed AS (
    UPDATE media_deletion_jobs AS job
    SET completed_at = sqlc.arg(now)::timestamptz,
        outcome = sqlc.arg(outcome)::text,
        lease_id = NULL,
        lease_expires_at = NULL
    WHERE job.id = sqlc.arg(id)::uuid
      AND job.lease_id = sqlc.arg(lease_id)::uuid
      AND job.completed_at IS NULL
    RETURNING job.id
), audited AS (
    INSERT INTO lifecycle_audit_events (kind, occurred_at, media_job_id)
    SELECT 'media_deletion_completed', sqlc.arg(now)::timestamptz, completed.id
    FROM completed
    ON CONFLICT (media_job_id, kind) DO NOTHING
    RETURNING id
)
SELECT EXISTS (SELECT 1 FROM completed) AS completed;

-- name: RequeueClaimedMediaDeletion :execrows
UPDATE media_deletion_jobs
SET next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz,
    lease_id = NULL,
    lease_expires_at = NULL
WHERE id = sqlc.arg(id)::uuid
  AND lease_id = sqlc.arg(lease_id)::uuid
  AND completed_at IS NULL;

-- name: GetMediaDeletionQueueState :one
SELECT count(*) FILTER (WHERE completed_at IS NULL)::bigint AS pending,
       count(*) FILTER (
           WHERE completed_at IS NULL
             AND next_attempt_at <= sqlc.arg(now)::timestamptz
       )::bigint AS due,
       count(*) FILTER (
           WHERE completed_at IS NULL
             AND enqueued_at <= sqlc.arg(overdue_cutoff)::timestamptz
       )::bigint AS overdue,
       COALESCE(
           min(enqueued_at) FILTER (WHERE completed_at IS NULL),
           sqlc.arg(now)::timestamptz
       ) AS oldest_enqueued_at
FROM media_deletion_jobs;

-- name: GetMediaOrphanSweepCursor :one
SELECT cursor
FROM privacy_sweep_state
WHERE name = 'media-orphan-sweep';

-- name: SaveMediaOrphanSweepCursor :execrows
UPDATE privacy_sweep_state
SET cursor = sqlc.arg(cursor)::text,
    updated_at = sqlc.arg(now)::timestamptz
WHERE name = 'media-orphan-sweep';

-- name: ClassifyMediaObject :one
SELECT EXISTS (
           SELECT 1
           FROM resumes
           WHERE personal_details -> 'photo' ->> 'key' = sqlc.arg(object_key)::text
       ) AS live,
       EXISTS (
           SELECT 1
           FROM media_deletion_jobs
           WHERE object_key = sqlc.arg(object_key)::text
             AND completed_at IS NULL
       ) AS queued;

-- name: CreateAndClaimOrphanMediaDeletion :one
INSERT INTO media_deletion_jobs (
    resume_id, object_key, enqueued_at, next_attempt_at,
    attempt_count, lease_id, lease_expires_at
)
SELECT sqlc.arg(resume_id)::uuid,
       sqlc.arg(object_key)::text,
       sqlc.arg(now)::timestamptz,
       sqlc.arg(now)::timestamptz,
       1,
       sqlc.arg(lease_id)::uuid,
       sqlc.arg(lease_expires_at)::timestamptz
WHERE NOT EXISTS (
      SELECT 1
      FROM resumes
      WHERE personal_details -> 'photo' ->> 'key' = sqlc.arg(object_key)::text
  )
ON CONFLICT (object_key) DO NOTHING
RETURNING *;
