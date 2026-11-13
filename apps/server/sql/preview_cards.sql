-- name: GetResumePreviewCard :one
SELECT version, png FROM resume_preview_cards
WHERE resume_id = sqlc.arg(resume_id);

-- name: GetLiveResumeByID :one
SELECT * FROM resumes
WHERE id = sqlc.arg(id) AND live = true;

-- name: LockResumeForPreviewCard :one
SELECT * FROM resumes
WHERE id = sqlc.arg(id)
FOR SHARE;

-- name: UpsertResumePreviewCard :exec
INSERT INTO resume_preview_cards (resume_id, version, png, rendered_at)
VALUES (sqlc.arg(resume_id), sqlc.arg(version), sqlc.arg(png), sqlc.arg(rendered_at))
ON CONFLICT (resume_id) DO UPDATE
SET version = EXCLUDED.version, png = EXCLUDED.png, rendered_at = EXCLUDED.rendered_at;

-- name: ListLiveResumePreviewCardVersions :many
SELECT r.id, c.version AS card_version
FROM resumes r
LEFT JOIN resume_preview_cards c ON c.resume_id = r.id
WHERE r.live = true
ORDER BY r.id;
