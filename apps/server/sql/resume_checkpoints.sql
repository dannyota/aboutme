-- name: GetResumeForCheckpointWrite :one
SELECT * FROM resumes WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) FOR NO KEY UPDATE;

-- name: GetCheckpointActorName :one
SELECT c.client_name
FROM oauth_grants AS g
JOIN oauth_clients AS c ON c.id = g.client_id
WHERE g.id = sqlc.arg(grant_id) AND g.user_id = sqlc.arg(user_id);

-- name: InsertResumeCheckpoint :one
INSERT INTO resume_checkpoints (resume_id, started_at, cause, actor_grant_id, actor_name,
    title, lng, schema_version, personal_details, content, customization, byte_size)
SELECT r.id, sqlc.arg(started_at), sqlc.arg(cause), sqlc.narg(actor_grant_id), sqlc.narg(actor_name),
    sqlc.arg(title), sqlc.narg(lng), sqlc.arg(schema_version), sqlc.arg(personal_details),
    sqlc.arg(content), sqlc.arg(customization), sqlc.arg(byte_size)
FROM resumes AS r
WHERE r.id = sqlc.arg(resume_id) AND r.user_id = sqlc.arg(user_id)
RETURNING id;

-- name: PruneResumeCheckpoints :exec
WITH ranked AS (
    SELECT c.id, row_number() OVER newest AS position, sum(c.byte_size) OVER newest AS total_bytes
    FROM resume_checkpoints AS c
    JOIN resumes AS r ON r.id = c.resume_id AND r.user_id = sqlc.arg(user_id)
    WHERE r.id = sqlc.arg(resume_id)
    WINDOW newest AS (ORDER BY c.started_at DESC, c.id DESC ROWS UNBOUNDED PRECEDING)
)
DELETE FROM resume_checkpoints AS c USING ranked
WHERE c.id = ranked.id AND (ranked.position > 50 OR ranked.total_bytes > 2097152);

-- name: ResumeCheckpointOwnerExists :one
SELECT EXISTS (SELECT 1 FROM resumes WHERE id = sqlc.arg(resume_id) AND user_id = sqlc.arg(user_id));

-- name: ListCheckpointProjectIDs :many
SELECT id FROM projects
WHERE user_id = sqlc.arg(user_id) AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListResumeCheckpoints :many
SELECT c.id, c.started_at, c.cause, c.actor_name, c.title, c.byte_size
FROM resume_checkpoints AS c
JOIN resumes AS r ON r.id = c.resume_id AND r.user_id = sqlc.arg(user_id)
WHERE r.id = sqlc.arg(resume_id) AND c.started_at >= sqlc.arg(cutoff)
    AND c.schema_version = ANY(sqlc.arg(accepted_versions)::int[])
ORDER BY c.started_at DESC, c.id DESC
LIMIT 50;

-- name: GetResumeCheckpoint :one
SELECT c.*
FROM resume_checkpoints AS c
JOIN resumes AS r ON r.id = c.resume_id AND r.user_id = sqlc.arg(user_id)
WHERE r.id = sqlc.arg(resume_id) AND c.id = sqlc.arg(checkpoint_id)
    AND c.started_at >= sqlc.arg(cutoff) AND c.schema_version = ANY(sqlc.arg(accepted_versions)::int[]);

-- name: DeleteResumeCheckpoint :execrows
DELETE FROM resume_checkpoints AS c USING resumes AS r
WHERE r.id = c.resume_id AND r.user_id = sqlc.arg(user_id) AND r.id = sqlc.arg(resume_id)
    AND c.id = sqlc.arg(checkpoint_id) AND c.started_at >= sqlc.arg(cutoff)
    AND c.schema_version = ANY(sqlc.arg(accepted_versions)::int[]);

-- name: ClearResumeCheckpoints :exec
DELETE FROM resume_checkpoints AS c USING resumes AS r
WHERE r.id = c.resume_id AND r.user_id = sqlc.arg(user_id) AND r.id = sqlc.arg(resume_id);

-- name: ListAccountResumeCheckpoints :many
SELECT c.*
FROM resume_checkpoints AS c
JOIN resumes AS r ON r.id = c.resume_id AND r.user_id = sqlc.arg(user_id)
WHERE c.started_at >= sqlc.arg(cutoff) AND c.schema_version = ANY(sqlc.arg(accepted_versions)::int[])
ORDER BY c.resume_id, c.started_at DESC, c.id DESC;
