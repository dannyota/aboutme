-- Account export deliberately reads only portable profile fields, provider
-- names, and owner resume rows. Credentials, provider subjects, object keys,
-- and lifecycle records do not cross the HTTP export boundary.

-- name: GetAccountExportProfile :one
SELECT id, email, name, created_at, updated_at
FROM users
WHERE id = $1;

-- name: ListAccountExportProviders :many
SELECT provider
FROM identities
WHERE user_id = $1
ORDER BY created_at, id;

-- name: ListAccountExportResumes :many
-- Carries sign_in_to_view with the other publish settings; the pass epoch
-- never leaves the server (docs/design/viewer-analytics/sign-in-to-view.md
-- "Setting"). The showcase columns are null without an opt-in
-- (docs/design/showcase.md "Privacy and abuse").
SELECT r.id, r.user_id, r.title, r.slug, r.live, r.download_enabled, r.seo_geo_enabled,
    r.schema_version, r.revision, r.lng, r.personal_details, r.content, r.customization,
    r.created_at, r.updated_at, r.public_title, r.favicon_emoji, r.sign_in_to_view,
    s.requested_at AS showcase_requested_at, s.role AS showcase_role
FROM resumes r
LEFT JOIN resume_showcase s ON s.resume_id = r.id
WHERE r.user_id = $1
ORDER BY r.created_at, r.id
LIMIT 4;
