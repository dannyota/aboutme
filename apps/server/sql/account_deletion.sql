-- Account deletion owns the global lock order documented in
-- docs/design/operations.md. The caller takes slug advisory locks and
-- public_state before these account-scoped locks.

-- name: LockCanonicalAccountEmail :exec
SELECT pg_advisory_xact_lock(
  hashtextextended('aboutme.email.v1:' || sqlc.arg(email)::text, 0)
);

-- name: ListAccountDeletionResumeSetForUpdate :many
SELECT id, revision
FROM resumes
WHERE user_id = sqlc.arg(user_id)::uuid
ORDER BY id
FOR UPDATE;

-- name: GetAccountDeletionSessionForUpdate :one
SELECT *
FROM sessions
WHERE id = sqlc.arg(id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
FOR UPDATE;

-- name: InsertAccountDeletedAuditEvent :one
INSERT INTO lifecycle_audit_events (id, kind, occurred_at, media_job_id)
VALUES (sqlc.arg(id)::uuid, 'account_deleted', sqlc.arg(occurred_at)::timestamptz, NULL)
RETURNING *;

-- name: GetAccountDeletedAuditEvent :one
SELECT *
FROM lifecycle_audit_events
WHERE id = sqlc.arg(id)::uuid
  AND kind = 'account_deleted'
  AND media_job_id IS NULL;

-- name: DeleteAccountUser :execrows
DELETE FROM users WHERE id = sqlc.arg(id)::uuid;
