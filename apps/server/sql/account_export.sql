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
SELECT r.id, r.user_id, r.title, r.slug, r.live, r.download_enabled, r.seo_geo_enabled,
    r.schema_version, r.revision, r.lng, r.personal_details, r.content, r.customization,
    r.created_at, r.updated_at, r.public_title, r.favicon_emoji, r.sign_in_to_view,
    s.requested_at AS showcase_requested_at, s.role AS showcase_role
FROM resumes r
LEFT JOIN resume_showcase s ON s.resume_id = r.id
WHERE r.user_id = $1
ORDER BY r.created_at, r.id
LIMIT 4;

-- name: ListAccountExportProjects :many
SELECT * FROM projects WHERE user_id = $1 ORDER BY created_at, id;
