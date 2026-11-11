-- Callers hold LockUserForResumeWrite throughout mutations and replay storage.

-- name: ListProjectsForUser :many
SELECT * FROM projects WHERE user_id = $1 ORDER BY created_at, id;

-- name: GetProjectForUser :one
SELECT * FROM projects WHERE id = $1 AND user_id = $2;

-- name: CountProjectsForUser :one
SELECT count(*) FROM projects WHERE user_id = $1;

-- name: ProjectDocumentBytes :one
SELECT octet_length(sqlc.arg(document)::jsonb::text)::int AS bytes;

-- name: CreateProject :one
INSERT INTO projects (user_id, slug, document, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4) RETURNING *;

-- name: ReplaceProjectCAS :one
UPDATE projects SET slug = sqlc.arg(slug), document = sqlc.arg(document),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
    AND revision = sqlc.arg(expected_revision)
RETURNING *;

-- name: DeleteProjectCAS :execrows
DELETE FROM projects WHERE id = $1 AND user_id = $2 AND revision = $3;

-- name: GetProjectSlugClaim :one
SELECT id FROM projects WHERE user_id = $1 AND slug = $2;

-- name: GetProjectSlugHold :one
SELECT released_at FROM project_slug_holds WHERE user_id = $1 AND slug = $2;

-- name: HoldProjectSlug :exec
INSERT INTO project_slug_holds (user_id, slug, released_at) VALUES ($1, $2, $3)
ON CONFLICT (user_id, slug) DO UPDATE SET released_at = EXCLUDED.released_at;

-- name: DeleteAccountProjectSlugHolds :execrows
DELETE FROM project_slug_holds WHERE user_id = $1;

-- name: ProposeProjectSlug :one
WITH occupied AS MATERIALIZED (
    SELECT p.slug FROM projects AS p WHERE p.user_id = sqlc.arg(user_id)::uuid
    UNION
    SELECT h.slug FROM project_slug_holds AS h
    WHERE h.user_id = sqlc.arg(user_id)::uuid AND h.released_at > sqlc.arg(cutoff)::timestamptz
), candidates AS (
    SELECT n, CASE WHEN n = 1 THEN sqlc.arg(base)::text
        ELSE rtrim(left(sqlc.arg(base)::text, 39 - length(n::text)), '-') || '-' || n::text
        END AS slug
    -- A 40-character base ending in "-2" equals its own second candidate, so
    -- one spare candidate keeps a free slug among them.
    FROM generate_series(1, (SELECT count(*)::int + 2 FROM occupied)) AS n
)
SELECT candidates.slug::text FROM candidates
WHERE NOT EXISTS (SELECT 1 FROM occupied WHERE occupied.slug = candidates.slug)
ORDER BY n LIMIT 1;
