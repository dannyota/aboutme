-- Hand-written, sqlc-annotated queries (`-- name: X :one/:many/:exec`) that
-- become type-safe Go methods in internal/store via `make generate`. See
-- docs/specs/aboutme-design.md §3 "Schema management".

-- name: CreateUser :one
INSERT INTO users (email, name, avatar_key) VALUES ($1, $2, $3) RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetIdentityByProviderSubject :one
SELECT * FROM identities WHERE provider = $1 AND provider_user_id = $2;

-- name: BeginSessionRotation :one
UPDATE sessions SET rotation_grace_until = $2
WHERE id = $1 AND rotation_grace_until IS NULL AND revoked_at IS NULL
RETURNING id;
