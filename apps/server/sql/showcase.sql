-- Community showcase queries (docs/design/showcase.md, ADR 0029). The
-- resume_showcase row exists only while the opt-in does.

-- name: GetResumeShowcase :one
SELECT * FROM resume_showcase
WHERE resume_id = sqlc.arg(resume_id);

-- name: GetResumeShowcaseForUpdate :one
-- Locks the opt-in row so a derived-value write and an operator review
-- serialize: a review that names a key the write just replaced matches no row.
SELECT * FROM resume_showcase
WHERE resume_id = sqlc.arg(resume_id)
FOR UPDATE;

-- name: ListResumeShowcasesForUser :many
SELECT s.*
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
WHERE r.user_id = sqlc.arg(user_id);

-- name: InsertResumeShowcase :exec
-- A new opt-in starts with no review result and no first-listed time.
INSERT INTO resume_showcase (resume_id, requested_at, role, review_key, card_version, template_id)
VALUES (
    sqlc.arg(resume_id), sqlc.arg(requested_at), sqlc.narg(role),
    sqlc.arg(review_key), sqlc.arg(card_version), sqlc.narg(template_id)
);

-- name: UpdateResumeShowcaseDerived :exec
UPDATE resume_showcase
SET review_key = sqlc.arg(review_key),
    card_version = sqlc.arg(card_version),
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

-- name: ApproveResumeShowcase :one
-- Approves only while the named key is still the current review key. The
-- first approval of an opt-in sets first_listed_at; later approvals keep it.
UPDATE resume_showcase AS s
SET reviewed_key = s.review_key,
    review_outcome = 'approved',
    reviewed_at = sqlc.arg(reviewed_at)::timestamptz,
    first_listed_at = COALESCE(s.first_listed_at, sqlc.arg(reviewed_at)::timestamptz)
FROM resumes AS r
WHERE r.id = s.resume_id
  AND r.slug = sqlc.arg(slug)::text
  AND s.review_key = sqlc.arg(review_key)::text
RETURNING s.resume_id;

-- name: DeclineResumeShowcase :one
-- Declines only while the named key is still the current review key. It keeps
-- first_listed_at: a later approval of a changed key does not move the resume up.
UPDATE resume_showcase AS s
SET reviewed_key = s.review_key,
    review_outcome = 'declined',
    reviewed_at = sqlc.arg(reviewed_at)::timestamptz
FROM resumes AS r
WHERE r.id = s.resume_id
  AND r.slug = sqlc.arg(slug)::text
  AND s.review_key = sqlc.arg(review_key)::text
RETURNING s.resume_id;

-- name: ListPendingResumeShowcases :many
-- Opted in with no review result for the current key.
SELECT r.slug, s.review_key, s.card_version, s.requested_at
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
WHERE s.review_outcome IS NULL OR s.reviewed_key IS DISTINCT FROM s.review_key
ORDER BY s.requested_at, s.resume_id;

-- name: GetResumeShowcaseBySlug :one
SELECT r.slug, s.role, s.review_key, s.card_version, s.template_id, s.reviewed_key,
       s.review_outcome, s.requested_at, s.reviewed_at, s.first_listed_at
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
WHERE r.slug = sqlc.arg(slug)::text;

-- name: ListShowcase :many
-- The public listing. One statement checks all five listing conditions: the
-- opt-in row exists, the review result is approved, the approved key equals
-- the current key, the resume is live, and sign in to view is off. It reads
-- committed state on every call; nothing caches it. The page needs only the
-- personal details (for the card's image text), so it reads no other document
-- part; schema_version tells the caller whether those details are current.
SELECT r.id, r.slug, r.schema_version, r.personal_details,
       s.card_version, s.template_id, s.role, l.language
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
CROSS JOIN LATERAL (
    SELECT (CASE lower(split_part(coalesce(r.lng, ''), '-', 1))
        WHEN 'vi' THEN 'vi' WHEN 'en' THEN 'en' ELSE 'other' END)::text AS language
) AS l
WHERE s.review_outcome = 'approved'
  AND s.reviewed_key = s.review_key
  AND r.live = true
  AND r.sign_in_to_view = false
  AND (sqlc.narg(role)::text IS NULL OR s.role = sqlc.narg(role)::text)
  AND (sqlc.narg(lang)::text IS NULL OR l.language = sqlc.narg(lang)::text)
  AND (
      sqlc.narg(template)::text IS NULL
      OR (sqlc.narg(template)::text = 'custom' AND s.template_id IS NULL)
      OR s.template_id = sqlc.narg(template)::text
  )
ORDER BY s.first_listed_at DESC, s.resume_id DESC
LIMIT sqlc.arg(page_size)::int OFFSET sqlc.arg(page_offset)::int;

-- name: CountShowcase :one
SELECT count(*)::bigint
FROM resume_showcase s
JOIN resumes r ON r.id = s.resume_id
CROSS JOIN LATERAL (
    SELECT (CASE lower(split_part(coalesce(r.lng, ''), '-', 1))
        WHEN 'vi' THEN 'vi' WHEN 'en' THEN 'en' ELSE 'other' END)::text AS language
) AS l
WHERE s.review_outcome = 'approved'
  AND s.reviewed_key = s.review_key
  AND r.live = true
  AND r.sign_in_to_view = false
  AND (sqlc.narg(role)::text IS NULL OR s.role = sqlc.narg(role)::text)
  AND (sqlc.narg(lang)::text IS NULL OR l.language = sqlc.narg(lang)::text)
  AND (
      sqlc.narg(template)::text IS NULL
      OR (sqlc.narg(template)::text = 'custom' AND s.template_id IS NULL)
      OR s.template_id = sqlc.narg(template)::text
  );
