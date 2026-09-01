-- Hand-written, sqlc-annotated queries (`-- name: X :one/:many/:exec`) that
-- become type-safe Go methods in internal/store via `make generate`.
-- Relational schema changes belong in apps/server/migrations; see
-- docs/design/data.md.

-- name: CreateUser :one
INSERT INTO users (email, name, avatar_key) VALUES ($1, $2, $3) RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetIdentityByProviderSubject :one
SELECT * FROM identities WHERE provider = $1 AND provider_user_id = $2;

-- name: CreateIdentity :one
-- The caller checks the provider subject first for the common case. The
-- database uniqueness constraint remains the concurrency backstop.
INSERT INTO identities (user_id, provider, provider_user_id) VALUES ($1, $2, $3) RETURNING *;

-- name: ListIdentitiesByUserID :many
-- Ordered by creation time then ID, oldest first with a deterministic tie-breaker.
SELECT * FROM identities WHERE user_id = $1 ORDER BY created_at ASC, id ASC;

-- name: CreateSession :one
-- Always inserts a brand-new row -- used both by Issue (fixation defense: a
-- login never reuses an existing session row) and by the >24h rotation
-- winner's successor insert (internal/auth.SessionManager.Authenticate),
-- which passes the predecessor's user_id, reauthenticated_at,
-- absolute_expires_at, ua, and ip through unchanged so a rotation never
-- extends absolute expiry or silently satisfies the recent-reauth gate.
-- rotated_from is NULL for a fresh login and contains the predecessor id
-- for a rotated successor. The partial unique index makes this an exact,
-- one-successor lineage link.
INSERT INTO sessions (
    user_id, token_hash, csrf_secret, created_at, last_seen_at, reauthenticated_at, absolute_expires_at, ua, ip, rotated_from
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
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

-- name: StartSessionRotationGrace :exec
-- Starts a predecessor's short grace period after its successor is first
-- used. BeginSessionRotation parks rotation_grace_until at
-- min(now + rotationAge, its own absolute_expires_at) -- non-NULL, so the
-- rotation CAS still
-- admits exactly one winner, far enough out that a successor whose one and
-- only raw-token delivery was lost never orphans the lineage, and bounded
-- so a predecessor that is never superseded in practice still expires on a
-- credential's timescale rather than the session's. This is the statement
-- that converts that parked value into a real, short deadline once the
-- successor's token has provably reached a client.
--
-- The `rotation_grace_until > grace_until` predicate makes it
-- monotonically SHRINKING and idempotent: the first call moves the
-- deadline in from the parked bound to first-use + rotationGrace;
-- every later call (the same successor's second, third, ... request, or a
-- concurrent one racing it) proposes a LATER deadline and therefore
-- matches no row. A predecessor's grace can never be pushed back out by
-- continued use of the successor, which would otherwise keep a superseded
-- credential alive indefinitely.
UPDATE sessions
SET rotation_grace_until = sqlc.arg(grace_until)::timestamptz
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND rotation_grace_until IS NOT NULL
  AND rotation_grace_until > sqlc.arg(grace_until)::timestamptz;

-- name: TouchLastSeenAt :exec
-- Unconditional: callers throttle to at most once per lastSeenThrottle
-- themselves (internal/auth.SessionManager.Authenticate) before issuing
-- this write, so it is not itself CAS-guarded.
UPDATE sessions SET last_seen_at = $2 WHERE id = $1;

-- name: TouchReauthenticatedAt :exec
-- Records that this session's lineage just completed a full OAuth login
-- after a real reauthentication round trip, never by rotation.
UPDATE sessions SET reauthenticated_at = $2 WHERE id = $1;

-- name: RevokeSession :exec
-- Idempotent: revoking an already-revoked (or nonexistent) session id
-- affects zero rows rather than erroring, so logout/revoke can be retried
-- safely. revoked_at is immediate and orthogonal to rotation_grace_until
-- and this query never changes that column.
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionForUser :execrows
-- Revokes only a live session owned by user_id. The live predicates match
-- ListLiveSessionsForUser so a hidden expired session cannot be reported as
-- revoked. The affected-row count lets the caller return the same absence
-- result for missing, expired, and differently owned sessions.
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
-- Lists sessions that internal/auth also considers live:
--
--   - not explicitly revoked (revoked_at IS NULL);
--   - not idle-expired: last_seen_at >= idle_cutoff, where idle_cutoff =
--     now - idleTimeout is computed in Go (session.go's own constant) and
--     passed as a query arg, never re-literalled as a SQL interval here;
--   - not absolute-expired: absolute_expires_at >= now;
--   - not a grace-dead rotation predecessor: rotation_grace_until IS NULL
--     OR rotation_grace_until >= now -- a superseded session keeps
--     revoked_at NULL but has rotation_grace_until in
--     the past, and that row must never appear in the caller's own
--     device list, exactly like an explicitly revoked one, just not
--     marked that way.
--
-- sqlc.arg(...)::timestamptz casts are explicit so both parameters' Go
-- types are plain (non-pointer) time.Time regardless of the nullable
-- columns they're compared against. Ordered newest-created first, with id
-- as a deterministic tiebreaker for equal creation times.
SELECT * FROM sessions
WHERE user_id = $1
  AND revoked_at IS NULL
  AND last_seen_at >= sqlc.arg(idle_cutoff)::timestamptz
  AND absolute_expires_at >= sqlc.arg(now)::timestamptz
  AND (rotation_grace_until IS NULL OR rotation_grace_until >= sqlc.arg(now)::timestamptz)
ORDER BY created_at DESC, id DESC;

-- name: FindLiveSuccessorSession :one
-- Finds the exact unrevoked successor through rotated_from. The partial unique
-- index in migration 00003 permits at most one successor per predecessor;
-- timestamps must never be used to reconstruct this lineage. The reverse
-- direction is already present in a session row's rotated_from value.
SELECT * FROM sessions WHERE rotated_from = $1 AND revoked_at IS NULL;

-- name: GetSessionByID :one
-- Loads a target so the caller can revoke its rotation partner.
-- RevokeSessionForUser returns only a row count, and the target need not be the
-- caller's current session, so the lineage values require this read.
SELECT * FROM sessions WHERE id = $1;

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

-- name: DeleteExpiredOAuthTransactions :execrows
-- OAuth start is unauthenticated and writes one row per request. Each start
-- therefore clears a bounded batch of expired rows before creating its own.
--
-- Bounded by an explicit LIMIT rather than an unqualified DELETE: this
-- runs on a request path, so its worst case must be a constant, not a
-- function of table size. The inner SELECT drives the index
-- oauth_transactions_expires_at_idx directly, oldest first, so repeated
-- calls make monotonic progress instead of rescanning the same rows.
--
-- expires_at alone is the predicate: a consumed transaction still carries
-- its original expires_at, so consumed and abandoned rows alike leave on
-- the same schedule, and a row is never removed while it could still be
-- validly consumed (ConsumeOAuthTransaction's own WHERE requires
-- expires_at > now).
DELETE FROM oauth_transactions
WHERE id IN (
    SELECT id FROM oauth_transactions
    WHERE expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY expires_at
    LIMIT sqlc.arg(max_rows)::int
);

-- name: LockUserForResumeWrite :one
SELECT id FROM users WHERE id = $1 FOR UPDATE;

-- name: CreateResume :one
INSERT INTO resumes (user_id, title, schema_version, lng,
                     personal_details, content, customization)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: GetResumeForUser :one
SELECT * FROM resumes WHERE id = $1 AND user_id = $2;

-- name: ListResumesForUser :many
SELECT * FROM resumes WHERE user_id = $1 ORDER BY created_at, id;

-- name: CountResumesForUser :one
SELECT count(*) FROM resumes WHERE user_id = $1;

-- name: DeleteResumeForUser :execrows
DELETE FROM resumes WHERE id = $1 AND user_id = $2;

-- name: UpdateResumeDocumentCAS :one
-- The returned revision is the NEW revision (revision + 1), never the
-- caller's expected one. A stale or non-matching $3 updates zero rows and
-- surfaces as pgx.ErrNoRows, which internal/resume turns into
-- *RevisionMismatchError or ErrNotFound.
UPDATE resumes
SET personal_details = $4, content = $5, customization = $6,
    schema_version = $7, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: UpdateResumeTitleCAS :one
-- The returned revision is the NEW revision (revision + 1), never the
-- caller's expected one. A stale or non-matching $3 updates zero rows and
-- surfaces as pgx.ErrNoRows, which internal/resume turns into
-- *RevisionMismatchError or ErrNotFound.
UPDATE resumes
SET title = $4, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: BackfillResumeDocumentCAS :execrows
-- This system backfill intentionally does not change revision or updated_at:
-- it persists the same projected document already served to readers. It is
-- not user-scoped. See docs/adr/0017-resume-document-versioning.md.
-- Fully named parameters distinguish the from/to schema versions. Both are
-- int32, and sqlc's positional naming would emit
-- `SchemaVersion` and `SchemaVersion_2`, neither carrying its direction.
-- A caller swapping them silently rewrites current rows back to the old
-- version. Named args make the pair unswappable at the call site.
UPDATE resumes
SET personal_details = sqlc.arg(personal_details),
    content = sqlc.arg(content),
    customization = sqlc.arg(customization),
    schema_version = sqlc.arg(to_schema_version)
WHERE id = sqlc.arg(id)
    AND schema_version = sqlc.arg(from_schema_version)
    AND revision = sqlc.arg(revision);

-- name: ListResumeIDsBelowSchemaVersion :many
SELECT id FROM resumes WHERE schema_version < $1 ORDER BY id LIMIT $2;

-- name: GetResumeByID :one
-- System-job read for the document-version backfill. It is intentionally
-- not user-scoped, unlike product reads. It adds no write path.
SELECT * FROM resumes WHERE id = $1;

-- name: UpdateResumeMetadataAndDocumentCAS :one
-- The P2B full-aggregate metadata write (D15/D17): title, lng, and the
-- caller's complete already-projected document under ONE revision CAS. The
-- returned revision is the NEW revision. A stale or non-matching $3
-- updates zero rows and surfaces as pgx.ErrNoRows, which internal/resume
-- turns into *RevisionMismatchError or ErrNotFound. user_id never appears
-- in a SET clause (the cap trigger fires on UPDATE OF user_id).
UPDATE resumes
SET title = sqlc.arg(title), lng = sqlc.narg(lng),
    personal_details = sqlc.arg(personal_details),
    content = sqlc.arg(content), customization = sqlc.arg(customization),
    schema_version = sqlc.arg(schema_version),
    revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: DeleteResumeForUserCAS :one
-- Revision-CAS delete returning the deleted row, so the caller can
-- validate the stored photo key and enqueue media cleanup in the same
-- transaction. Zero rows (stale revision, wrong owner, or missing id)
-- surfaces as pgx.ErrNoRows; the caller re-reads to distinguish staleness
-- from absence without creating an existence oracle.
DELETE FROM resumes
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING *;

-- name: CreateIdempotencyRecord :one
-- Returns PostgreSQL's normalized jsonb representation of the stored body
-- and approved headers plus the exact stored byte count, so the first
-- response and every replay use identical bytes and no caller recomputes
-- capacity accounting in Go.
INSERT INTO idempotency_records
    (user_id, route, idempotency_key, request_hash,
     response_status, response_body, response_headers, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING response_body, response_headers,
          octet_length(response_body::text) +
          octet_length(response_headers::text) AS stored_bytes;

-- name: GetIdempotencyRecord :one
SELECT * FROM idempotency_records
WHERE user_id = $1 AND route = $2 AND idempotency_key = $3;

-- name: NormalizeIdempotencyResponse :one
-- Normalizes a candidate response through the same jsonb representation an
-- insert would store and reports the exact byte count the usage
-- reservation must account for — the one canonical byte expression shared
-- with backfill, insert, and cleanup.
SELECT sqlc.arg(response_body)::jsonb AS response_body,
       sqlc.arg(response_headers)::jsonb AS response_headers,
       octet_length(sqlc.arg(response_body)::jsonb::text) +
       octet_length(sqlc.arg(response_headers)::jsonb::text) AS stored_bytes;

-- name: DeleteExpiredIdempotencyRecordForKey :one
-- Deletes exactly this key's record if (and only if) it has expired, and
-- reports the counters to release — zero rows deleted still returns one
-- aggregate row of zeros.
WITH deleted AS (
  DELETE FROM idempotency_records
  WHERE user_id = $1 AND route = $2 AND idempotency_key = $3
      AND expires_at <= $4
  RETURNING response_body, response_headers
)
SELECT count(*)::bigint AS deleted_records,
       COALESCE(sum(octet_length(response_body::text) +
                    octet_length(response_headers::text)), 0)::bigint
         AS deleted_bytes
FROM deleted;

-- name: DeleteExpiredIdempotencyRecordsForUser :one
-- The bounded request-path cleanup: at most limit_rows of the calling
-- user's expired records, deterministically oldest first by
-- (expires_at, id) on the composite index. FOR UPDATE SKIP LOCKED keeps
-- concurrent cleanup bounded and deadlock-free. Always returns one
-- aggregate row, even when nothing was deleted.
WITH doomed AS (
  SELECT idempotency_records.id FROM idempotency_records
  WHERE idempotency_records.user_id = $1
      AND idempotency_records.expires_at <= $2
  ORDER BY idempotency_records.expires_at, idempotency_records.id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
), deleted AS (
  DELETE FROM idempotency_records AS records
  USING doomed WHERE records.id = doomed.id
  RETURNING records.response_body, records.response_headers
)
SELECT count(*)::bigint AS deleted_records,
       COALESCE(sum(octet_length(response_body::text) +
                    octet_length(response_headers::text)), 0)::bigint
         AS deleted_bytes
FROM deleted;

-- name: DeleteExpiredIdempotencyRecordsGlobal :many
-- The P8-priv scheduled sweep's bounded page: oldest expired rows across
-- all users, grouped per user so the sweep can release each user's exact
-- counters (once per returned user) before committing that cleanup
-- transaction.
WITH doomed AS (
  SELECT id FROM idempotency_records
  WHERE expires_at <= sqlc.arg(now)::timestamptz
  ORDER BY expires_at, id
  LIMIT sqlc.arg(limit_rows)
  FOR UPDATE SKIP LOCKED
), deleted AS (
  DELETE FROM idempotency_records AS records
  USING doomed WHERE records.id = doomed.id
  RETURNING records.user_id, records.response_body, records.response_headers
)
SELECT user_id,
       count(*)::bigint AS deleted_records,
       COALESCE(sum(octet_length(response_body::text) +
                    octet_length(response_headers::text)), 0)::bigint
         AS deleted_bytes
FROM deleted
GROUP BY user_id;

-- name: GetOrCreateIdempotencyUsageForUpdate :one
-- Locks (creating if absent) the caller's usage row inside the current
-- transaction. The no-op DO UPDATE assignment is what makes the conflict
-- arm take the row lock and return the existing row; user_id itself never
-- appears in a SET clause.
INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
VALUES ($1, 0, 0)
ON CONFLICT (user_id) DO UPDATE
SET retained_records = idempotency_usage.retained_records
RETURNING *;

-- name: TryReserveIdempotencyUsage :one
-- Conditionally admits one new retained record of record_bytes stored
-- bytes within the caps. Zero rows (pgx.ErrNoRows) means the insert would
-- exceed a cap and the caller must reject with the typed capacity error
-- without committing the mutation.
UPDATE idempotency_usage
SET retained_records = retained_records + 1,
    stored_bytes = stored_bytes + sqlc.arg(record_bytes)::bigint
WHERE user_id = $1
  AND retained_records + 1 <= sqlc.arg(max_records)::bigint
  AND stored_bytes + sqlc.arg(record_bytes)::bigint <= sqlc.arg(max_bytes)::bigint
RETURNING retained_records, stored_bytes;

-- name: ReleaseIdempotencyUsage :execrows
-- Releases exactly the counters of physically deleted records, in the same
-- transaction as their deletion. The migration's purge-then-backfill order
-- plus transactional maintenance guarantee this can never underflow the
-- non-negative checks.
UPDATE idempotency_usage
SET retained_records = retained_records - sqlc.arg(records)::bigint,
    stored_bytes = stored_bytes - sqlc.arg(released_bytes)::bigint
WHERE user_id = $1;

-- name: GetIdempotencyCapacityRetryAfter :one
-- Inputs for the capacity Retry-After decision: while an expired retained
-- row remains the caller retries in one second (the next mutation's
-- bounded cleanup frees space); otherwise the earliest retained expiry
-- says when space can next appear.
-- earliest_expiry falls back to the caller's own now when the user retains
-- no records at all (capacity rejection with zero retained records cannot
-- normally happen); the caller maps any earliest_expiry <= now to the
-- one-second branch.
SELECT EXISTS (
         SELECT 1 FROM idempotency_records
         WHERE idempotency_records.user_id = $1
             AND idempotency_records.expires_at <= sqlc.arg(now)::timestamptz
       ) AS expired_backlog,
       COALESCE((SELECT min(idempotency_records.expires_at)
                 FROM idempotency_records
                 WHERE idempotency_records.user_id = $1),
                sqlc.arg(now)::timestamptz)::timestamptz
         AS earliest_expiry;

-- name: EnqueueMediaDeletionJob :execrows
-- Records exact-key cleanup work in the caller's transaction (ADR 0019).
-- Duplicate enqueue of the immutable key is idempotent (zero rows); the
-- table's own check constraint rejects a malformed or cross-resume key.
INSERT INTO media_deletion_jobs (resume_id, object_key)
VALUES ($1, $2)
ON CONFLICT (object_key) DO NOTHING;

-- name: GetMediaDeletionJobByObjectKey :one
-- Ambiguous-delete recovery proves the exact immutable cleanup job without
-- scanning or exposing unrelated object keys.
SELECT *
FROM media_deletion_jobs
WHERE resume_id = sqlc.arg(resume_id)::uuid
  AND object_key = sqlc.arg(object_key)::text;

-- name: GetPublicState :one
SELECT singleton, discovery_generation
FROM public_state
WHERE singleton = true;

-- name: GetPublicDiscoverySnapshot :one
-- The generation and eligible slug set are selected by one PostgreSQL
-- statement so aggregate discovery admission cannot pair different commits.
SELECT
    ps.discovery_generation,
    COALESCE(
        array_agg(r.slug::text ORDER BY r.slug::text COLLATE "C")
            FILTER (WHERE r.slug IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS slugs
FROM public_state AS ps
LEFT JOIN resumes AS r
    ON r.slug IS NOT NULL
    AND r.live = true
    AND r.seo_geo_enabled = true
WHERE ps.singleton = true
GROUP BY ps.discovery_generation;

-- name: LockPublicState :one
SELECT singleton, discovery_generation
FROM public_state
WHERE singleton = true
FOR UPDATE;

-- name: AdvanceDiscoveryGeneration :one
UPDATE public_state
SET discovery_generation = discovery_generation + 1
WHERE singleton = true
RETURNING discovery_generation;

-- name: GetPublicResumeBySlug :one
-- Missing, never-published, private, renamed, and deleted rows all surface as
-- pgx.ErrNoRows. Representation-specific flags are checked after this live
-- gate without exposing a different lookup result.
SELECT * FROM resumes
WHERE slug = sqlc.arg(slug)::text AND live = true;

-- name: GetPublicResumeByOwner :one
SELECT * FROM resumes
WHERE user_id = sqlc.arg(user_id)::uuid
  AND id = sqlc.arg(id)::uuid;

-- name: ListEligiblePublicSlugs :many
-- Discovery bytes contain only eligible slugs in raw byte order. The
-- COALESCE is unreachable under the predicate and gives sqlc a non-null Go
-- string instead of a pointer.
SELECT COALESCE(slug, '')::text AS slug
FROM resumes
WHERE slug IS NOT NULL AND live = true AND seo_geo_enabled = true
ORDER BY slug COLLATE "C" ASC;

-- name: LockSlugClaim :exec
SELECT pg_advisory_xact_lock(
  hashtextextended('aboutme.slug.v1:' || sqlc.arg(slug)::text, 0)
);

-- name: GetSlugClaim :one
SELECT id FROM resumes WHERE slug = sqlc.arg(slug)::text;

-- name: GetSlugTombstoneForUpdate :one
SELECT * FROM slug_tombstones WHERE slug = $1 FOR UPDATE;

-- name: ConsumeExpiredSlugTombstone :one
DELETE FROM slug_tombstones
WHERE slug = sqlc.arg(slug)::text
  AND released_at + interval '180 days' <=
      sqlc.arg(reusable_at)::timestamptz
RETURNING id;

-- name: InsertSlugTombstone :one
INSERT INTO slug_tombstones (slug, released_by_user_id, released_at)
VALUES (
  sqlc.arg(slug)::text,
  sqlc.narg(released_by_user_id)::uuid,
  sqlc.arg(released_at)::timestamptz
)
RETURNING *;

-- name: PublishResumeCAS :one
WITH input AS (
  SELECT
    sqlc.arg(id)::uuid AS id,
    sqlc.arg(user_id)::uuid AS user_id,
    sqlc.arg(expected_revision)::bigint AS expected_revision,
    sqlc.narg(slug)::text AS slug,
    sqlc.arg(live)::boolean AS live,
    sqlc.arg(download_enabled)::boolean AS download_enabled,
    sqlc.arg(seo_geo_enabled)::boolean AS seo_geo_enabled,
    sqlc.arg(updated_at)::timestamptz AS updated_at
)
UPDATE resumes AS resume
SET slug = input.slug,
    live = input.live,
    download_enabled = input.download_enabled,
    seo_geo_enabled = input.seo_geo_enabled,
    revision = resume.revision + 1,
    updated_at = input.updated_at
FROM input
WHERE resume.id = input.id
  AND resume.user_id = input.user_id
  AND resume.revision = input.expected_revision
RETURNING resume.*;

-- name: DeleteResumePublicCAS :one
DELETE FROM resumes
WHERE id = sqlc.arg(id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
  AND revision = sqlc.arg(expected_revision)::bigint
RETURNING *;

-- ---------------------------------------------------------------------------
-- Phase PA: password credentials, registrations, reset tokens, and email jobs.
-- Lock order (D4) is user -> credential -> reset token -> sessions. These
-- queries expose exactly the row locks that order needs and no others.
-- ---------------------------------------------------------------------------

-- name: GetUserForUpdate :one
-- The user-row lock every session issuer and password mutation serializes on.
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- name: GetUserByCanonicalEmail :one
-- Ownership read before a new-account insert. The caller passes the canonical
-- lowercase form; users.email is citext so the comparison is already
-- case-insensitive, and the unique constraint arbitrates the actual insert.
SELECT * FROM users WHERE email = $1;

-- name: GetPasswordCredential :one
SELECT * FROM password_credentials WHERE user_id = $1;

-- name: GetPasswordCredentialForUpdate :one
SELECT * FROM password_credentials WHERE user_id = $1 FOR UPDATE;

-- name: UpsertPasswordCredential :one
-- Insert-or-replace the single credential under the row lock the caller
-- already holds (GetPasswordCredentialForUpdate), re-encoding the hash and
-- bumping changed_at on the conflict arm.
INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET encoded_hash = EXCLUDED.encoded_hash,
    changed_at = EXCLUDED.changed_at
RETURNING *;

-- name: GetPasswordRegistrationByEmailForUpdate :one
SELECT * FROM password_registrations WHERE email = $1 FOR UPDATE;

-- name: GetPasswordRegistrationByDigest :one
SELECT * FROM password_registrations WHERE token_digest = $1;

-- name: GetPasswordRegistrationForUpdate :one
SELECT * FROM password_registrations WHERE id = $1 FOR UPDATE;

-- name: CreatePasswordRegistration :one
INSERT INTO password_registrations (email, name, encoded_hash, token_digest, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: DeletePasswordRegistration :execrows
DELETE FROM password_registrations WHERE id = $1;

-- name: GetPasswordResetTokenByUserForUpdate :one
SELECT * FROM password_reset_tokens WHERE user_id = $1 FOR UPDATE;

-- name: GetPasswordResetTokenByDigest :one
SELECT * FROM password_reset_tokens WHERE token_digest = $1;

-- name: GetPasswordResetTokenForUpdate :one
SELECT * FROM password_reset_tokens WHERE id = $1 FOR UPDATE;

-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_digest, created_at, expires_at)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DeletePasswordResetToken :execrows
DELETE FROM password_reset_tokens WHERE id = $1;

-- name: GetSessionByIDForUpdate :one
-- Session-row lock for password add/change, which revokes the current session
-- and every sibling atomically.
SELECT * FROM sessions WHERE id = $1 FOR UPDATE;

-- name: CreateAuthEmailJob :one
-- New jobs always start pending with attempts 0 (DEFAULT). The caller provides
-- the job id (a UUIDv7 it generated) so the outbox AAD binds to the stored row's
-- id; the caller has already encrypted the payload and computed the exact expiry
-- before commit.
INSERT INTO auth_email_jobs (
    id, kind, state, registration_id, reset_token_id, user_id, token_digest,
    key_id, nonce, ciphertext, created_at, expires_at, next_attempt_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: ListLiveAuthEmailJobKeyIDs :many
-- Startup readiness: the distinct key IDs every pending/leased (non-terminal,
-- non-expired) job references. The key ring must decrypt all of them.
SELECT DISTINCT COALESCE(key_id, '')::text AS key_id
FROM auth_email_jobs
WHERE state IN ('pending', 'leased')
  AND key_id IS NOT NULL
  AND expires_at > sqlc.arg(now)::timestamptz
ORDER BY key_id;

-- name: ClaimAuthEmailJobs :many
-- Bounded SKIP LOCKED claim: atomically transitions at most limit due pending
-- jobs to leased, increments attempts, stamps the lease pair, and returns the
-- leased rows. Two concurrent claimers are disjoint because each claims a
-- distinct row under FOR UPDATE SKIP LOCKED.
UPDATE auth_email_jobs
SET state = 'leased',
    attempts = attempts + 1,
    next_attempt_at = NULL,
    lease_owner = sqlc.arg(lease_owner)::text,
    lease_expires_at = sqlc.arg(lease_expires_at)::timestamptz
WHERE id IN (
    SELECT id FROM auth_email_jobs
    WHERE state = 'pending'
      AND next_attempt_at <= sqlc.arg(now)::timestamptz
    ORDER BY next_attempt_at, created_at, id
    LIMIT sqlc.arg(limit_rows)::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetLeasedAuthEmailJobForUpdate :one
-- Re-locks the exact leased job by its owner before the bounded send handoff,
-- so a token replacement that already committed cannot race this delivery.
SELECT * FROM auth_email_jobs
WHERE id = sqlc.arg(id)::uuid
  AND state = 'leased'
  AND lease_owner = sqlc.arg(lease_owner)::text
FOR UPDATE;

-- name: MarkAuthEmailJobSent :execrows
-- SES accepted: clears every encryption and lease field and records sent_at.
UPDATE auth_email_jobs
SET state = 'sent', key_id = NULL, nonce = NULL, ciphertext = NULL,
    next_attempt_at = NULL, lease_owner = NULL, lease_expires_at = NULL,
    sent_at = sqlc.arg(sent_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid AND state = 'leased';

-- name: MarkAuthEmailJobTerminal :execrows
-- Permanent failure, expiry, or the eighth attempt: clears every encryption and
-- lease field and records terminal_at. A stale pending job may terminate
-- without ever being leased (attempts stays 0).
UPDATE auth_email_jobs
SET state = 'terminal', key_id = NULL, nonce = NULL, ciphertext = NULL,
    next_attempt_at = NULL, lease_owner = NULL, lease_expires_at = NULL,
    terminal_at = sqlc.arg(terminal_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid AND state IN ('pending', 'leased');

-- name: RequeueAuthEmailJob :execrows
-- A temporary failure releases the lease back to pending with the next attempt
-- time, retaining ciphertext and the already-incremented attempt count. The
-- caller never requeues at attempts = 8 (that path marks terminal instead).
UPDATE auth_email_jobs
SET state = 'pending',
    next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz,
    lease_owner = NULL, lease_expires_at = NULL
WHERE id = sqlc.arg(id)::uuid AND state = 'leased';

-- name: RequeueExpiredAuthEmailLeases :execrows
-- Bounded stale-lease recovery: a lease that expired without a completed send
-- returns to pending, rolling back the claim's attempt increment (the send
-- never happened) and re-dueing immediately. Ordered so repeated calls make
-- monotonic progress.
UPDATE auth_email_jobs
SET state = 'pending',
    attempts = attempts - 1,
    next_attempt_at = sqlc.arg(now)::timestamptz,
    lease_owner = NULL, lease_expires_at = NULL
WHERE id IN (
    SELECT id FROM auth_email_jobs
    WHERE state = 'leased'
      AND lease_expires_at <= sqlc.arg(now)::timestamptz
    ORDER BY lease_expires_at, id
    LIMIT sqlc.arg(limit_rows)::int
    FOR UPDATE SKIP LOCKED
);

-- name: CleanupExpiredPasswordRegistrations :execrows
DELETE FROM password_registrations
WHERE id IN (
    SELECT id FROM password_registrations
    WHERE expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY expires_at, id
    LIMIT sqlc.arg(limit_rows)::int
);

-- name: CleanupExpiredPasswordResetTokens :execrows
DELETE FROM password_reset_tokens
WHERE id IN (
    SELECT id FROM password_reset_tokens
    WHERE expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY expires_at, id
    LIMIT sqlc.arg(limit_rows)::int
);

-- name: CleanupFinishedAuthEmailJobs :execrows
-- Sent/terminal jobs are retained for seven days of audit, then removed in a
-- bounded page, oldest outcome first.
DELETE FROM auth_email_jobs
WHERE id IN (
    SELECT id FROM auth_email_jobs
    WHERE state IN ('sent', 'terminal')
      AND COALESCE(sent_at, terminal_at) <= sqlc.arg(cutoff)::timestamptz
    ORDER BY COALESCE(sent_at, terminal_at), id
    LIMIT sqlc.arg(limit_rows)::int
);

-- ---------------------------------------------------------------------------
-- Phase PM: OAuth agent access — clients, authorization codes, grants, and
-- tokens. Storage is digest-only: no query here reads, writes, or returns raw
-- code or token material, and PKCE verifiers are never stored.
--
-- Lock order is oauth_clients -> users -> oauth_grants ->
-- oauth_authorization_codes -> oauth_tokens. Every path that revokes a token
-- family first locks its grant row (GetOAuthGrantForUpdate), and every path
-- that mutates a client's rows first locks the client row
-- (GetOAuthClientForUpdate) -- which is also what makes the idle-client sweep
-- skip a client another transaction is consenting to.
-- ---------------------------------------------------------------------------

-- name: CreateOAuthClient :one
-- Dynamic client registration (M1). The primary key returned here IS the
-- public client_id; there is no secret. last_used_at starts at created_at so
-- the column is never NULL and the ordering check holds from the first row.
INSERT INTO oauth_clients (client_name, redirect_uris, created_at, last_used_at)
VALUES (
    sqlc.arg(client_name),
    sqlc.arg(redirect_uris),
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
)
RETURNING *;

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE id = $1;

-- name: GetOAuthClientForUpdate :one
-- The client-row lock the authorize, consent, and token paths take before
-- writing a grant, code, or token for that client. Holding it is what lets
-- ListIdleOAuthClientCandidates skip (rather than collect) a client that is
-- being consented to right now.
SELECT * FROM oauth_clients WHERE id = $1 FOR UPDATE;

-- name: TouchOAuthClientLastUsed :execrows
-- Bounded activity record: at most one write per client per touch window. The
-- caller passes touch_before = now - window, so the throttle is enforced by
-- the predicate rather than by caller bookkeeping.
UPDATE oauth_clients
SET last_used_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id)
  AND last_used_at <= sqlc.arg(touch_before)::timestamptz;

-- name: DeleteOAuthClient :execrows
-- Removing a client cascades its codes, grants, and tokens: deregistering an
-- agent ends every authorization it holds.
DELETE FROM oauth_clients WHERE id = $1;

-- name: ListIdleOAuthClientCandidates :many
-- The bounded idle-client sweep (M1/M5): clients registered before
-- idle_before that hold no live grant and no live token. FOR UPDATE SKIP
-- LOCKED both reserves the returned rows for the caller's transaction -- so a
-- concurrent grant insert blocks on the FK's key-share lock and then fails
-- rather than being cascaded away -- and skips any client another transaction
-- already locked. Ordered oldest-first on oauth_clients_created_at_idx so
-- repeated sweeps make monotonic progress.
SELECT candidate.id FROM oauth_clients AS candidate
WHERE candidate.created_at < sqlc.arg(idle_before)::timestamptz
  AND NOT EXISTS (
      SELECT 1 FROM oauth_grants AS live_grant
      WHERE live_grant.client_id = candidate.id
        AND live_grant.revoked_at IS NULL
  )
  AND NOT EXISTS (
      SELECT 1 FROM oauth_tokens AS live_token
      WHERE live_token.client_id = candidate.id
        AND live_token.revoked_at IS NULL
        AND live_token.superseded_at IS NULL
        AND live_token.expires_at > sqlc.arg(now)::timestamptz
  )
ORDER BY candidate.created_at, candidate.id
LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
FOR UPDATE SKIP LOCKED;

-- name: DeleteOAuthClients :execrows
-- Deletes exactly the candidate set the caller just locked. The batch bound
-- lives in ListIdleOAuthClientCandidates; this statement never widens it.
DELETE FROM oauth_clients WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CreateOAuthAuthorizationCode :one
-- Code issue (M2). expires_at is computed here, not passed in, so the exact
-- 60-second TTL the table checks can never be missed by a caller. Only the
-- code's digest is stored; the raw code exists solely in the redirect the user
-- agent carries.
INSERT INTO oauth_authorization_codes (
    code_digest, client_id, user_id, scopes, code_challenge, redirect_uri,
    created_at, expires_at
) VALUES (
    sqlc.arg(code_digest), sqlc.arg(client_id), sqlc.arg(user_id), sqlc.arg(scopes),
    sqlc.arg(code_challenge), sqlc.arg(redirect_uri),
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz + interval '60 seconds'
)
RETURNING *;

-- name: GetOAuthAuthorizationCodeByDigest :one
-- Replay lookup: returns the row whether or not it was consumed, so the caller
-- can see the family a consumed code issued and revoke exactly those tokens.
SELECT * FROM oauth_authorization_codes WHERE code_digest = $1;

-- name: GetOAuthAuthorizationCodeByDigestForUpdate :one
-- The locked replay lookup used before a consumed-code replay revokes the
-- family it issued, so the revocation and the row it read cannot interleave.
SELECT * FROM oauth_authorization_codes WHERE code_digest = $1 FOR UPDATE;

-- name: ConsumeOAuthAuthorizationCode :one
-- Single-use consumption (M2). One conditional UPDATE both takes the row lock
-- and decides the winner: under two concurrent exchanges of the same code the
-- second blocks, re-evaluates the predicate after the first commits, matches
-- no row, and surfaces as pgx.ErrNoRows. Unknown, expired, and already-consumed
-- codes therefore collapse to that one outcome, so no caller can branch on the
-- reason. issued_family_id records the token family this exchange creates,
-- which is what a later replay revokes.
UPDATE oauth_authorization_codes
SET consumed_at = sqlc.arg(consumed_at)::timestamptz,
    issued_family_id = sqlc.arg(issued_family_id)::uuid
WHERE code_digest = sqlc.arg(code_digest)
  AND consumed_at IS NULL
  AND expires_at > sqlc.arg(consumed_at)::timestamptz
RETURNING *;

-- name: DeleteExpiredOAuthAuthorizationCodes :execrows
-- Bounded cleanup, oldest expiry first on oauth_authorization_codes_expires_at_idx.
-- expires_at alone is the predicate: a consumed code keeps its original expiry,
-- so consumed and abandoned rows leave on the same schedule, and a row is never
-- removed while ConsumeOAuthAuthorizationCode could still claim it.
DELETE FROM oauth_authorization_codes
WHERE id IN (
    SELECT id FROM oauth_authorization_codes
    WHERE expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY expires_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
    FOR UPDATE SKIP LOCKED
);

-- name: UpsertOAuthGrant :one
-- Consent approval (M8): records a new grant or refreshes the live one's
-- scopes. The conflict target names the partial unique index's predicate, so
-- the upsert arbitrates on exactly the "one live grant per (user, client)"
-- rule the database enforces. created_at is never rewritten -- the settings
-- list shows when the agent was first connected, not when it was last
-- re-approved.
INSERT INTO oauth_grants (user_id, client_id, scopes, created_at)
VALUES (
    sqlc.arg(user_id), sqlc.arg(client_id), sqlc.arg(scopes),
    sqlc.arg(created_at)::timestamptz
)
ON CONFLICT (user_id, client_id) WHERE revoked_at IS NULL
DO UPDATE SET scopes = EXCLUDED.scopes
RETURNING *;

-- name: GetLiveOAuthGrant :one
-- The grant-skip read: a live grant with equal-or-wider scopes lets authorize
-- issue a code without a second consent.
SELECT * FROM oauth_grants
WHERE user_id = $1 AND client_id = $2 AND revoked_at IS NULL;

-- name: GetOAuthGrantForUpdate :one
-- The grant-row lock every token-family revocation takes first, so a rotation
-- committing concurrently cannot resurrect a family the revocation just killed.
SELECT * FROM oauth_grants WHERE id = $1 FOR UPDATE;

-- name: CountLiveOAuthGrantsForUser :one
-- The M5 live-grant cap check. Callers serialize it with the user row lock
-- (GetUserForUpdate) so two concurrent approvals cannot both read the same
-- pre-cap count.
SELECT count(*) FROM oauth_grants WHERE user_id = $1 AND revoked_at IS NULL;

-- name: ListLiveOAuthGrantsForUser :many
-- The connected-agents list: client name, scopes, and when the agent was
-- connected and last used. Last-used comes from the grant's tokens rather than
-- a duplicated column, so there is one source of truth for agent activity.
-- Bounded by limit_rows; ordered newest-first with id as a deterministic
-- tiebreaker.
--
-- The correlated ORDER BY ... LIMIT 1 is deliberate where max() would read
-- more naturally: sqlc cannot type an aggregate expression and emits
-- interface{} for it, while a plain nullable column reference keeps the Go
-- type *time.Time — NULL for a grant whose tokens have never been used.
SELECT grant_row.id, grant_row.client_id, client_row.client_name,
       grant_row.scopes, grant_row.created_at,
       (
           SELECT token_row.last_used_at FROM oauth_tokens AS token_row
           WHERE token_row.grant_id = grant_row.id
             AND token_row.last_used_at IS NOT NULL
           ORDER BY token_row.last_used_at DESC
           LIMIT 1
       ) AS last_used_at
FROM oauth_grants AS grant_row
JOIN oauth_clients AS client_row ON client_row.id = grant_row.client_id
WHERE grant_row.user_id = sqlc.arg(user_id)
  AND grant_row.revoked_at IS NULL
ORDER BY grant_row.created_at DESC, grant_row.id DESC
LIMIT sqlc.arg(limit_rows)::int;

-- name: RevokeOAuthGrant :execrows
-- Internal revocation (RFC 7009 and consumed-code replay), where the grant id
-- came from a token row the caller already validated. Idempotent: revoking an
-- already-revoked grant affects zero rows.
UPDATE oauth_grants
SET revoked_at = sqlc.arg(revoked_at)::timestamptz
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeOAuthGrantForUser :one
-- The settings-page revocation. Ownership is part of the predicate, so a
-- missing, already-revoked, and differently owned grant all return
-- pgx.ErrNoRows and no existence oracle appears.
UPDATE oauth_grants
SET revoked_at = sqlc.arg(revoked_at)::timestamptz
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL
RETURNING *;

-- name: CreateOAuthToken :one
-- The first token of a family, issued by a code exchange (M3). expires_at is
-- clamped to the family's own death so no token can outlive the family it
-- belongs to; the table's ordering check is the backstop for raw SQL.
INSERT INTO oauth_tokens (
    token_digest, kind, family_id, client_id, user_id, grant_id,
    created_at, expires_at, family_expires_at
) VALUES (
    sqlc.arg(token_digest), sqlc.arg(kind), sqlc.arg(family_id), sqlc.arg(client_id),
    sqlc.arg(user_id), sqlc.arg(grant_id),
    sqlc.arg(created_at)::timestamptz,
    LEAST(sqlc.arg(expires_at)::timestamptz, sqlc.arg(family_expires_at)::timestamptz),
    sqlc.arg(family_expires_at)::timestamptz
)
RETURNING *;

-- name: InsertRotatedOAuthToken :one
-- Refresh rotation (M3). Every identity field -- family, client, user, grant,
-- and family expiry -- is read from the predecessor row rather than trusted
-- from the caller, so a rotated token structurally cannot join a different
-- family, client, user, or grant than the token it succeeds. A missing
-- predecessor selects no row and returns pgx.ErrNoRows instead of inserting an
-- orphan. The partial unique index on rotated_from means one predecessor can
-- mint at most one successor even under concurrent rotations.
INSERT INTO oauth_tokens (
    token_digest, kind, family_id, rotated_from, client_id, user_id, grant_id,
    created_at, expires_at, family_expires_at
)
SELECT
    sqlc.arg(token_digest), sqlc.arg(kind), predecessor.family_id, predecessor.id,
    predecessor.client_id, predecessor.user_id, predecessor.grant_id,
    sqlc.arg(created_at)::timestamptz,
    LEAST(sqlc.arg(expires_at)::timestamptz, predecessor.family_expires_at),
    predecessor.family_expires_at
FROM oauth_tokens AS predecessor
WHERE predecessor.id = sqlc.arg(rotated_from)::uuid
RETURNING *;

-- name: GetOAuthTokenAuthorityByDigest :one
-- The bearer boundary's single round trip (M7): token row, its grant, and its
-- account in one query. The caller applies expiry, revocation, supersession,
-- and kind checks to the returned row; every rejection class therefore reads
-- the same rows and produces the same closed response.
SELECT sqlc.embed(token_row), sqlc.embed(grant_row), sqlc.embed(user_row)
FROM oauth_tokens AS token_row
JOIN oauth_grants AS grant_row ON grant_row.id = token_row.grant_id
JOIN users AS user_row ON user_row.id = token_row.user_id
WHERE token_row.token_digest = sqlc.arg(token_digest);

-- name: SupersedeOAuthToken :one
-- Marks the presented refresh token superseded as part of a rotation. family_id
-- is part of the predicate, so a caller holding one family's identifier can
-- never mark a token belonging to another family, and a second supersession of
-- the same row affects nothing (pgx.ErrNoRows) -- which is exactly the replay
-- the caller turns into a whole-family revocation.
UPDATE oauth_tokens
SET superseded_at = sqlc.arg(superseded_at)::timestamptz
WHERE id = sqlc.arg(id)
  AND family_id = sqlc.arg(family_id)
  AND superseded_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeOAuthTokenFamily :execrows
-- Superseded-token replay defense: every still-live member of one family dies
-- together, in the caller's transaction, under the grant row lock.
UPDATE oauth_tokens
SET revoked_at = sqlc.arg(revoked_at)::timestamptz
WHERE family_id = sqlc.arg(family_id) AND revoked_at IS NULL;

-- name: RevokeOAuthTokensForGrant :execrows
-- Grant revocation kills every family issued under it, not just the newest.
UPDATE oauth_tokens
SET revoked_at = sqlc.arg(revoked_at)::timestamptz
WHERE grant_id = sqlc.arg(grant_id) AND revoked_at IS NULL;

-- name: TouchOAuthTokenLastUsed :execrows
-- Bounded activity record (M3): at most one write per token per touch window,
-- and never on a revoked token. The caller passes touch_before = now - window.
UPDATE oauth_tokens
SET last_used_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND (last_used_at IS NULL OR last_used_at <= sqlc.arg(touch_before)::timestamptz);

-- name: DeleteTerminalOAuthTokens :execrows
-- Bounded token cleanup. An access token leaves as soon as it has expired or
-- been revoked: it carries no replay-detection role. A refresh token is kept
-- until its whole family has expired, because a superseded or revoked refresh
-- token that is presented again must still be recognized as a replay rather
-- than as an unknown token. rotated_from is ON DELETE SET NULL, so removing a
-- predecessor detaches its successor instead of cascading past the batch bound.
DELETE FROM oauth_tokens
WHERE id IN (
    SELECT id FROM oauth_tokens
    WHERE (
        kind = 'access'
        AND (expires_at <= sqlc.arg(cutoff)::timestamptz
             OR revoked_at <= sqlc.arg(cutoff)::timestamptz)
    ) OR (
        kind = 'refresh' AND family_expires_at <= sqlc.arg(cutoff)::timestamptz
    )
    ORDER BY expires_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
    FOR UPDATE SKIP LOCKED
);
