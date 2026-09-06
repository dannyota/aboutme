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
SELECT id, user_id, title, slug, live, download_enabled, seo_geo_enabled,
    schema_version, revision, lng, personal_details, content, customization,
    created_at, updated_at
FROM resumes
WHERE user_id = $1
ORDER BY created_at, id
LIMIT 4;
