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

-- name: CreateSession :one
-- Always inserts a brand-new row -- used both by Issue (fixation defense: a
-- login never reuses an existing session row) and by the >24h rotation
-- winner's successor insert (internal/auth.SessionManager.Authenticate),
-- which passes the predecessor's user_id, reauthenticated_at,
-- absolute_expires_at, ua, and ip through unchanged so a rotation never
-- extends absolute expiry or silently satisfies the recent-reauth gate.
INSERT INTO sessions (
    user_id, token_hash, csrf_secret, created_at, last_seen_at, reauthenticated_at, absolute_expires_at, ua, ip
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: BeginSessionRotation :one
-- Single-row conditional UPDATE that decides the >24h rotation winner: only
-- a row that has neither already started rotating (rotation_grace_until
-- still NULL) nor been revoked is claimed. Under concurrent callers racing
-- the same session id, exactly one UPDATE affects a row (returns its id);
-- every other caller's UPDATE affects zero rows and pgx reports
-- pgx.ErrNoRows -- that caller lost the race and must not mint a second
-- successor (see internal/auth.SessionManager.Authenticate).
UPDATE sessions SET rotation_grace_until = $2
WHERE id = $1 AND rotation_grace_until IS NULL AND revoked_at IS NULL
RETURNING id;

-- name: TouchLastSeenAt :exec
-- Unconditional: callers throttle to at most once per lastSeenThrottle
-- themselves (internal/auth.SessionManager.Authenticate) before issuing
-- this write, so it is not itself CAS-guarded.
UPDATE sessions SET last_seen_at = $2 WHERE id = $1;

-- name: TouchReauthenticatedAt :exec
-- Records that this session's lineage just completed a full OAuth login
-- (design decision 2) -- called only after a real reauth round trip
-- (internal/auth.SessionManager.TouchReauthenticated), never by rotation.
UPDATE sessions SET reauthenticated_at = $2 WHERE id = $1;

-- name: RevokeSession :exec
-- Idempotent: revoking an already-revoked (or nonexistent) session id
-- affects zero rows rather than erroring, so logout/revoke can be retried
-- safely. revoked_at is immediate and orthogonal to rotation_grace_until
-- (design decision 1) -- this never touches that column.
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAllSessions :execrows
-- Logout-everywhere: revokes every one of the user's not-already-revoked
-- sessions and reports how many rows that affected.
UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreateOAuthTransaction :one
INSERT INTO oauth_transactions (
    handle_hash, provider, purpose, linking_user_id, state, pkce_verifier, nonce, redirect_uri, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: ConsumeOAuthTransaction :one
-- Atomically claims a transaction: only a row that is unexpired and not yet
-- consumed (as of now, $2) is updated and returned. A handle that is
-- unknown, expired, or already consumed matches no row, so the caller sees
-- exactly one outcome (pgx.ErrNoRows) for all three cases -- see
-- internal/auth.ErrTransactionInvalid's doc comment for why that collapse
-- is deliberate. The provider-mismatch check (RFC 9700 mix-up defense)
-- happens in Go, after this still consumes the row: an attacker replaying
-- a valid handle against the wrong provider's callback burns the
-- transaction just like any other outcome, so it can never be retried
-- against the correct provider either.
UPDATE oauth_transactions
SET consumed_at = $2
WHERE handle_hash = $1 AND consumed_at IS NULL AND expires_at > $2
RETURNING *;
