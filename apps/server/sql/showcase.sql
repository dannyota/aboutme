-- name: GetResumeShowcase :one
SELECT resume_id, requested_at, role, card_version, template_id FROM resume_showcase
WHERE resume_id = sqlc.arg(resume_id);

-- name: GetResumeShowcaseForUpdate :one
SELECT resume_id, requested_at, role, card_version, template_id FROM resume_showcase
WHERE resume_id = sqlc.arg(resume_id)
FOR UPDATE;

-- name: ListResumeShowcasesForUser :many
SELECT s.resume_id, s.requested_at, s.role, s.card_version, s.template_id
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
WHERE r.user_id = sqlc.arg(user_id);

-- name: InsertResumeShowcase :exec
INSERT INTO resume_showcase (resume_id, requested_at, role, card_version, template_id)
VALUES (
    sqlc.arg(resume_id), sqlc.arg(requested_at), sqlc.narg(role),
    sqlc.arg(card_version), sqlc.narg(template_id)
);

-- name: UpdateResumeShowcaseDerived :exec
UPDATE resume_showcase
SET card_version = sqlc.arg(card_version),
    template_id = sqlc.narg(template_id)
WHERE resume_id = sqlc.arg(resume_id);

-- name: UpdateResumeShowcaseRole :exec
UPDATE resume_showcase
SET role = sqlc.narg(role)
WHERE resume_id = sqlc.arg(resume_id);

-- name: DeleteResumeShowcase :execrows
DELETE FROM resume_showcase
WHERE resume_id = sqlc.arg(resume_id);

-- name: ListResumeShowcaseIDs :many
SELECT resume_id FROM resume_showcase
ORDER BY resume_id;

-- name: ListShowcase :many
SELECT r.id, r.slug, r.schema_version, r.personal_details,
       s.card_version, s.template_id, s.role, l.language
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
CROSS JOIN LATERAL (
    SELECT (CASE lower(split_part(coalesce(r.lng, ''), '-', 1))
        WHEN 'vi' THEN 'vi' WHEN 'en' THEN 'en' ELSE 'other' END)::text AS language
) AS l
WHERE r.live = true
  AND r.sign_in_to_view = false
  AND (sqlc.narg(role)::text IS NULL OR s.role = sqlc.narg(role)::text)
  AND (sqlc.narg(lang)::text IS NULL OR l.language = sqlc.narg(lang)::text)
  AND (
      sqlc.narg(template)::text IS NULL
      OR (sqlc.narg(template)::text = 'custom' AND s.template_id IS NULL)
      OR s.template_id = sqlc.narg(template)::text
  )
ORDER BY s.requested_at ASC, s.resume_id ASC
LIMIT sqlc.arg(page_size)::int OFFSET sqlc.arg(page_offset)::int;

-- name: CountShowcase :one
SELECT count(*)::bigint
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
CROSS JOIN LATERAL (
    SELECT (CASE lower(split_part(coalesce(r.lng, ''), '-', 1))
        WHEN 'vi' THEN 'vi' WHEN 'en' THEN 'en' ELSE 'other' END)::text AS language
) AS l
WHERE r.live = true
  AND r.sign_in_to_view = false
  AND (sqlc.narg(role)::text IS NULL OR s.role = sqlc.narg(role)::text)
  AND (sqlc.narg(lang)::text IS NULL OR l.language = sqlc.narg(lang)::text)
  AND (
      sqlc.narg(template)::text IS NULL
      OR (sqlc.narg(template)::text = 'custom' AND s.template_id IS NULL)
      OR s.template_id = sqlc.narg(template)::text
  );
