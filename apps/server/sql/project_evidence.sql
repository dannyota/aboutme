-- name: ListProjectEvidence :many
SELECT e.id, e.project_id, e.position, e.kind, e.repository_id, e.item_id, e.pull_number, e.release_tag, e.url,
    e.repository_name, e.title, e.item_date, e.checked_at, e.checked_binding, e.rule_met, e.flag_reason,
    COALESCE(e.rule_met AND c.github_user_id IS NOT NULL AND c.binding = e.checked_binding, false)::bool AS confirmed
FROM project_evidence AS e
JOIN projects AS p ON p.id = e.project_id
LEFT JOIN github_connections AS c ON c.user_id = p.user_id
WHERE p.user_id = sqlc.arg(user_id) AND e.project_id = sqlc.arg(project_id)
ORDER BY e.position;

-- name: ListEvidenceForProjects :many
SELECT e.id, e.project_id, e.position, e.kind, e.repository_id, e.item_id, e.pull_number, e.release_tag, e.url,
    e.repository_name, e.title, e.item_date, e.checked_at, e.checked_binding, e.rule_met, e.flag_reason,
    COALESCE(e.rule_met AND c.github_user_id IS NOT NULL AND c.binding = e.checked_binding, false)::bool AS confirmed
FROM project_evidence AS e
JOIN projects AS p ON p.id = e.project_id
LEFT JOIN github_connections AS c ON c.user_id = p.user_id
WHERE p.user_id = sqlc.arg(user_id) AND e.project_id = ANY(sqlc.arg(project_ids)::uuid[])
ORDER BY e.project_id, e.position;

-- name: CountProjectEvidence :one
SELECT count(*)::int AS items FROM project_evidence WHERE project_id = sqlc.arg(project_id);

-- name: GetProjectEvidence :one
SELECT e.*
FROM project_evidence AS e
JOIN projects AS p ON p.id = e.project_id
WHERE e.id = sqlc.arg(id) AND e.project_id = sqlc.arg(project_id) AND p.user_id = sqlc.arg(user_id);

-- name: FindProjectEvidenceItem :one
SELECT id FROM project_evidence
WHERE project_id = sqlc.arg(project_id) AND kind = sqlc.arg(kind) AND item_id = sqlc.arg(item_id);

-- name: InsertProjectEvidence :one
INSERT INTO project_evidence (
    project_id, position, kind, repository_id, item_id, pull_number, release_tag, url,
    repository_name, title, item_date, checked_at, checked_binding, rule_met, flag_reason
)
SELECT sqlc.arg(project_id)::uuid, COALESCE(max(e.position) + 1, 0)::int, sqlc.arg(kind)::text,
    sqlc.arg(repository_id)::bigint, sqlc.arg(item_id)::bigint, sqlc.narg(pull_number)::bigint,
    sqlc.narg(release_tag)::text, sqlc.arg(url)::text, sqlc.arg(repository_name)::text, sqlc.arg(title)::text,
    sqlc.arg(item_date)::timestamptz, sqlc.arg(checked_at)::timestamptz, sqlc.arg(checked_binding)::bigint,
    sqlc.arg(rule_met)::bool, sqlc.narg(flag_reason)::text
FROM project_evidence AS e
WHERE e.project_id = sqlc.arg(project_id)::uuid
RETURNING *;

-- name: RewriteProjectEvidence :one
UPDATE project_evidence
SET pull_number = sqlc.narg(pull_number), release_tag = sqlc.narg(release_tag), url = sqlc.arg(url),
    repository_name = sqlc.arg(repository_name), title = sqlc.arg(title), item_date = sqlc.arg(item_date),
    checked_at = sqlc.arg(checked_at), checked_binding = sqlc.arg(checked_binding), rule_met = sqlc.arg(rule_met),
    flag_reason = sqlc.narg(flag_reason)
WHERE id = sqlc.arg(id) AND project_id = sqlc.arg(project_id)
RETURNING *;

-- name: DeleteProjectEvidence :execrows
DELETE FROM project_evidence WHERE id = sqlc.arg(id) AND project_id = sqlc.arg(project_id);

-- name: ReorderProjectEvidence :execrows
UPDATE project_evidence AS e
SET position = (o.ordinality - 1)::int
FROM unnest(sqlc.arg(ids)::uuid[]) WITH ORDINALITY AS o(id, ordinality)
WHERE e.id = o.id AND e.project_id = sqlc.arg(project_id);

-- name: AdvanceProjectRevision :one
UPDATE projects SET revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND revision = sqlc.arg(expected_revision)
RETURNING *;

-- name: ProjectStoredDocumentBytes :one
SELECT octet_length(document::text)::int AS bytes FROM projects WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: ListProjectsWithEvidence :many
SELECT p.id
FROM projects AS p
WHERE p.user_id = sqlc.arg(user_id) AND EXISTS (SELECT 1 FROM project_evidence AS e WHERE e.project_id = p.id)
ORDER BY p.id;

-- name: DeleteAccountEvidence :many
WITH removed AS (
    DELETE FROM project_evidence AS e
    USING projects AS p
    WHERE p.id = e.project_id AND p.user_id = sqlc.arg(user_id)
    RETURNING e.project_id
), advanced AS (
    UPDATE projects
    SET revision = revision + 1, updated_at = sqlc.arg(updated_at)
    WHERE user_id = sqlc.arg(user_id) AND id IN (SELECT project_id FROM removed)
    RETURNING id, revision
)
SELECT id, revision FROM advanced ORDER BY id;
