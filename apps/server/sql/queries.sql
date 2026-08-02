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

-- name: CreateIdentity :one
-- internal/auth's login-resolution algorithm calls
-- GetIdentityByProviderSubject first specifically to avoid racing
-- identities_provider_subject_key's UNIQUE (provider, provider_user_id) in
-- the common case; Task 10's link algorithm handles the
-- already-claimed-by-another-user case explicitly instead of relying on
-- this insert to fail.
INSERT INTO identities (user_id, provider, provider_user_id) VALUES ($1, $2, $3) RETURNING *;

-- name: ListIdentitiesByUserID :many
-- Task 9's GET /me: every provider identity linked to user_id, oldest
-- first (created_at) so the first-ever linked provider always sorts
-- first.
SELECT * FROM identities WHERE user_id = $1 ORDER BY created_at;

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

-- name: RevokeSessionForUser :execrows
-- Ownership-checked counterpart to RevokeSession: only revokes id if it
-- also belongs to user_id AND is still LIVE by the exact same predicates
-- ListLiveSessionsForUser uses below (fix round 1, findings I1/M5): a
-- session that is already idle-expired, absolute-expired, or a grace-dead
-- rotation predecessor must revoke ZERO rows here too -- otherwise a
-- caller could "revoke" a row GET /sessions' own device list already
-- refuses to show them, which is exactly the self-inconsistency the
-- review caught. Returns the affected row count so a caller can
-- distinguish "revoked" (1) from "no such LIVE session for this user" (0)
-- -- internal/auth.SessionManager.RevokeForUser's own caller (Task 9's
-- DELETE /sessions/{id}) turns 0 into a 404, not a 403, so it never
-- confirms whether the id exists for someone else, is merely dead, or
-- belongs to another user.
UPDATE sessions
SET revoked_at = $3
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND last_seen_at >= sqlc.arg(idle_cutoff)::timestamptz
  AND absolute_expires_at >= sqlc.arg(now)::timestamptz
  AND (rotation_grace_until IS NULL OR rotation_grace_until >= sqlc.arg(now)::timestamptz);

-- name: RevokeAllSessions :execrows
-- Logout-everywhere: revokes every one of the user's not-already-revoked
-- sessions and reports how many rows that affected.
UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: ListLiveSessionsForUser :many
-- Task 9's GET /sessions device list (design spec §3): every session
-- belonging to user_id that is still LIVE as of now -- mirrors
-- session.go's sessionDead exactly, predicate for predicate (fix round 1,
-- finding I1: the original version only excluded revoked/grace-dead rows,
-- silently listing idle-expired and absolute-expired sessions as live
-- devices with no GC to ever remove them):
--
--   - not explicitly revoked (revoked_at IS NULL);
--   - not idle-expired: last_seen_at >= idle_cutoff, where idle_cutoff =
--     now - idleTimeout is computed in GO (session.go's own constant) and
--     passed as a query arg, never re-literalled as a SQL interval here;
--   - not absolute-expired: absolute_expires_at >= now;
--   - not a grace-dead rotation predecessor: rotation_grace_until IS NULL
--     OR rotation_grace_until >= now -- a session rotation has already
--     superseded keeps revoked_at NULL but has rotation_grace_until in
--     the past, and that row must never appear in the caller's own
--     device list, exactly like an explicitly revoked one, just not
--     marked that way.
--
-- sqlc.arg(...)::timestamptz casts are explicit so both parameters' Go
-- types are plain (non-pointer) time.Time regardless of the nullable
-- columns they're compared against. Ordered newest-created first, with id
-- as a deterministic tiebreaker for two rows created in the same instant
-- (fix round 1, M4).
SELECT * FROM sessions
WHERE user_id = $1
  AND revoked_at IS NULL
  AND last_seen_at >= sqlc.arg(idle_cutoff)::timestamptz
  AND absolute_expires_at >= sqlc.arg(now)::timestamptz
  AND (rotation_grace_until IS NULL OR rotation_grace_until >= sqlc.arg(now)::timestamptz)
ORDER BY created_at DESC, id DESC;

-- name: FindImmediatePredecessorSession :one
-- Fix round 1, finding I2 / design owner ruling DD-C14: given a session
-- that may itself be a rotation SUCCESSOR, finds the exact row it was
-- rotated FROM, if that predecessor is still live (revoked_at IS NULL).
-- Not a heuristic: session.go's tryRotate sets
-- predecessor.rotation_grace_until = now + rotationGrace and
-- successor.created_at = now from the SAME `now` value in the same
-- function call, so predecessor.rotation_grace_until always equals
-- successor.created_at + rotationGrace EXACTLY for a genuine
-- predecessor/successor pair, and essentially never coincidentally for
-- an unrelated row (rotation_grace_until is a random-offset instant, not
-- a value any other write path in this schema ever produces). The caller
-- computes successor.created_at + rotationGrace in Go (rotationGrace is
-- session.go's own constant) and passes it as the single timestamp
-- argument -- this exists specifically because RequireSession's
-- context-based predecessor seam (ContextWithPredecessorSessionID) only
-- covers the narrow case where the SAME request's own Authenticate call
-- performs the rotation, which a CSRF-gated mutating endpoint can never
-- observe in practice (RequireCSRF validates against the POST-rotation
-- session, which a client cannot have a correct token for on its very
-- first use) -- see task-9-report.md's fix-round-1 section for the full
-- reasoning. user_id scopes the match to the caller's own lineage only,
-- so a coincidental rotation_grace_until collision with a DIFFERENT
-- user's session can never cross-match.
SELECT * FROM sessions
WHERE user_id = $1
  AND revoked_at IS NULL
  AND rotation_grace_until = sqlc.arg(predecessor_grace_until)::timestamptz;

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
