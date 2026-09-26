-- name: GetResumePreviewCard :one
-- A stored card is derived data (ADR 0055); every public read passes the
-- live-state gate before it reads one.
SELECT version, png FROM resume_preview_cards
WHERE resume_id = sqlc.arg(resume_id);

-- name: GetLiveResumeByID :one
SELECT * FROM resumes
WHERE id = sqlc.arg(id) AND live = true;

-- name: LockResumeForPreviewCard :one
-- FOR SHARE makes a card store wait for, or block, an unpublish, rename, or
-- delete of the same resume, since those lock the row for update. The row
-- returned is the latest committed one, so the caller recomputes the card
-- version from it before it writes.
SELECT * FROM resumes
WHERE id = sqlc.arg(id)
FOR SHARE;

-- name: UpsertResumePreviewCard :exec
INSERT INTO resume_preview_cards (resume_id, version, png, rendered_at)
VALUES (sqlc.arg(resume_id), sqlc.arg(version), sqlc.arg(png), sqlc.arg(rendered_at))
ON CONFLICT (resume_id) DO UPDATE
SET version = EXCLUDED.version, png = EXCLUDED.png, rendered_at = EXCLUDED.rendered_at;

-- name: ListLiveResumePreviewCardVersions :many
-- Every live resume and the version of its stored card, NULL when none.
SELECT r.id, c.version AS card_version
FROM resumes r
LEFT JOIN resume_preview_cards c ON c.resume_id = r.id
WHERE r.live = true
ORDER BY r.id;
