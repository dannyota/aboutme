-- Authenticator-app (TOTP) second-factor queries. Every TOTP transaction
-- takes locks in the order fixed by
-- docs/design/totp-second-factor-contract.md#removal-recovery-and-races:
-- user, current session when present, policy, credential, enrollment, then
-- pending authentication when applicable. GetUserForUpdate,
-- GetSessionByIDForUpdate, and GetSecondFactorPolicyForUpdate already exist
-- in queries.sql; this file adds the credential and enrollment steps.
--
-- Every mutation below that reads a value it also writes (the failure
-- counter, the doubling cool-down, the monotonic step) must run inside the
-- same transaction as a prior GetTOTPCredentialForUpdate, so the row stays
-- locked from the first read through the write and no concurrent
-- transaction can observe or apply a stale value in between.

-- name: GetTOTPCredentialForUpdate :one
-- The credential-row lock. Callers read cooldown_until here, before any
-- decryption, so a live cool-down short-circuits with no decrypt attempt and
-- no failure counted (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget).
SELECT * FROM totp_credentials WHERE user_id = sqlc.arg(user_id)::uuid FOR UPDATE;

-- name: CountTOTPCredentialsForUser :one
-- Unlocked existence count (0 or 1) for the shared active-factor counter;
-- one active TOTP credential per account.
SELECT count(*) FROM totp_credentials WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: InstallTOTPCredential :one
-- First install under the user lock. The caller generates id as a UUIDv7 and
-- seals the secret with it before this insert (docs/design/totp-key-management.md#sealing).
INSERT INTO totp_credentials (
    id, user_id, key_id, nonce, ciphertext, format_version, last_used_step, created_at, updated_at
) VALUES (
    sqlc.arg(id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(key_id)::text, sqlc.arg(nonce)::bytea,
    sqlc.arg(ciphertext)::bytea, 1, sqlc.arg(last_used_step)::bigint,
    sqlc.arg(created_at)::timestamptz, sqlc.arg(created_at)::timestamptz
) RETURNING *;

-- name: ReplaceTOTPCredential :one
-- Replacement under the row lock from GetTOTPCredentialForUpdate: keeps the
-- existing id and created_at, reseals under a fresh nonce, and resets the
-- failure budget, including last_failed_at, because a fresh secret cannot
-- inherit a stale cool-down.
UPDATE totp_credentials
SET key_id = sqlc.arg(key_id)::text,
    nonce = sqlc.arg(nonce)::bytea,
    ciphertext = sqlc.arg(ciphertext)::bytea,
    format_version = 1,
    last_used_step = sqlc.arg(last_used_step)::bigint,
    failed_attempts = 0,
    cooldown_until = NULL,
    last_failed_at = NULL,
    updated_at = sqlc.arg(updated_at)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
RETURNING *;

-- name: AdvanceTOTPCredentialStep :one
-- Compare-and-advance on a valid code: the WHERE guard is the single-use
-- rule, so a replayed step (equal, not greater) affects zero rows and the
-- caller sees pgx.ErrNoRows. Requires the row already locked by
-- GetTOTPCredentialForUpdate in this transaction.
UPDATE totp_credentials
SET last_used_step = sqlc.arg(step)::bigint,
    updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
  AND sqlc.arg(step)::bigint > last_used_step
RETURNING *;

-- name: RecordTOTPCredentialFailure :one
-- Increments the consecutive-failure counter, saturating at 1,000, and on
-- every fifth consecutive failure sets a cool-down of 15 minutes doubling to
-- at most 24 hours (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget).
-- The WHERE guard repeats the cool-down check GetTOTPCredentialForUpdate
-- already made, so this only ever applies to a row not currently cooling
-- down. Every counted failure also sets last_failed_at, which gates whether a
-- later valid code may reset the budget.
UPDATE totp_credentials
SET failed_attempts = LEAST(failed_attempts + 1, 1000),
    cooldown_until = CASE
        WHEN LEAST(failed_attempts + 1, 1000) % 5 = 0
        THEN sqlc.arg(now)::timestamptz + LEAST(
            interval '15 minutes' * power(2.0, LEAST(LEAST(failed_attempts + 1, 1000) / 5 - 1, 7)),
            interval '24 hours'
        )
        ELSE cooldown_until
    END,
    last_failed_at = sqlc.arg(now)::timestamptz,
    updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
  AND (cooldown_until IS NULL OR cooldown_until <= sqlc.arg(now)::timestamptz)
RETURNING *;

-- name: ResetTOTPCredentialFailureBudget :one
-- Generic reset to the zero failure budget. Callers decide when this runs
-- (a successful code at least 24 hours past the last failure, a replacement,
-- or a removal); storage only guarantees it always clears all three fields
-- together (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget).
-- A completed password reset never calls this: it leaves the budget
-- unchanged.
UPDATE totp_credentials
SET failed_attempts = 0,
    cooldown_until = NULL,
    last_failed_at = NULL,
    updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
RETURNING *;

-- name: DeleteTOTPCredentialForUser :one
-- Removal. Returns pgx.ErrNoRows when no credential exists, which the
-- caller maps to 404 factor_not_found.
DELETE FROM totp_credentials WHERE user_id = sqlc.arg(user_id)::uuid RETURNING *;

-- name: ListTOTPCredentialsOffActiveKey :many
-- Bounded re-encryption batch: at most 200 rows off the active key, in id
-- order after after_id, skipping any row a live factor transaction already
-- holds (docs/design/totp-key-management.md#rotation). The after_id cursor
-- lets the caller page past a row it already visited in an earlier batch of
-- the same run (for example one that failed to decrypt and so stays off
-- the active key), instead of reselecting it until the row budget is spent.
SELECT * FROM totp_credentials
WHERE key_id <> sqlc.arg(active_key_id)::text
  AND id > sqlc.arg(after_id)::uuid
ORDER BY id
LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
FOR UPDATE SKIP LOCKED;

-- name: ReencryptTOTPCredential :execrows
-- Key-ID compare-before-update: the WHERE clause only applies the rewrite
-- when the row is still sealed under the exact key the caller decrypted, so
-- a row already rewritten by a concurrent run is left alone.
UPDATE totp_credentials
SET key_id = sqlc.arg(new_key_id)::text,
    nonce = sqlc.arg(nonce)::bytea,
    ciphertext = sqlc.arg(ciphertext)::bytea,
    updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id)::uuid AND key_id = sqlc.arg(old_key_id)::text;

-- name: CountTOTPCredentialsOffActiveKey :one
-- Rotation gate: the count a rotation run must see at zero before the key
-- switch proceeds.
SELECT count(*) FROM totp_credentials WHERE key_id <> sqlc.arg(active_key_id)::text;

-- name: GetTOTPEnrollmentForUpdate :one
-- The enrollment-row lock, keyed by the enrollment token's digest. The
-- caller checks user, session, epoch, and expiry against the returned row
-- and collapses any mismatch to 400 enrollment_invalid.
SELECT * FROM totp_enrollments WHERE token_digest = sqlc.arg(token_digest) FOR UPDATE;

-- name: CreateTOTPEnrollment :one
-- Starts one enrollment under the user lock, after DeleteTOTPEnrollmentForUser
-- has removed any prior row in the same transaction: the atomic supersession
-- (docs/design/totp-second-factor-contract.md#enrollment-and-replacement-api).
INSERT INTO totp_enrollments (
    id, token_digest, user_id, session_id, auth_epoch, issuer, key_id, nonce, ciphertext,
    format_version, created_at, expires_at
) VALUES (
    sqlc.arg(id)::uuid, sqlc.arg(token_digest)::bytea, sqlc.arg(user_id)::uuid,
    sqlc.arg(session_id)::uuid, sqlc.arg(auth_epoch)::bigint, sqlc.arg(issuer)::text,
    sqlc.arg(key_id)::text, sqlc.arg(nonce)::bytea, sqlc.arg(ciphertext)::bytea, 1,
    sqlc.arg(created_at)::timestamptz, sqlc.arg(expires_at)::timestamptz
) RETURNING *;

-- name: DeleteTOTPEnrollmentForUser :execrows
-- Deletes any existing enrollment row for the account, expired or not: the
-- first half of atomic supersession on start, and the unconditional
-- enrollment cleanup on removal.
DELETE FROM totp_enrollments WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: DeleteTOTPEnrollmentByID :execrows
-- Claim by delete: totp_enrollments has no consumed_at column, so deleting
-- the row already locked by GetTOTPEnrollmentForUpdate is the only exit for
-- a successful completion or a disabled-flag completion attempt.
DELETE FROM totp_enrollments WHERE id = sqlc.arg(id)::uuid;

-- name: CleanupExpiredTOTPEnrollments :execrows
-- Bounded expiry cleanup, at most 200 rows per admitted enrollment start
-- (docs/design/budgets.md), skipping rows a concurrent claim or rotation run
-- already holds.
WITH candidates AS MATERIALIZED (
    SELECT id FROM totp_enrollments
    WHERE expires_at <= CURRENT_TIMESTAMP
    ORDER BY expires_at, id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM totp_enrollments AS target USING candidates
WHERE target.id = candidates.id;

-- name: ListTOTPEnrollmentsOffActiveKey :many
-- Bounded re-encryption batch for enrollments, same shape and after_id
-- paging cursor as ListTOTPCredentialsOffActiveKey. The caller deletes an
-- expired row here outright instead of decrypting it
-- (docs/design/totp-key-management.md#rotation).
SELECT * FROM totp_enrollments
WHERE key_id <> sqlc.arg(active_key_id)::text
  AND id > sqlc.arg(after_id)::uuid
ORDER BY id
LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
FOR UPDATE SKIP LOCKED;

-- name: ReencryptTOTPEnrollment :execrows
-- Key-ID compare-before-update for an unexpired enrollment row.
UPDATE totp_enrollments
SET key_id = sqlc.arg(new_key_id)::text,
    nonce = sqlc.arg(nonce)::bytea,
    ciphertext = sqlc.arg(ciphertext)::bytea
WHERE id = sqlc.arg(id)::uuid AND key_id = sqlc.arg(old_key_id)::text;

-- name: CountTOTPEnrollmentsOffActiveKey :one
-- Rotation gate for enrollments, paired with CountTOTPCredentialsOffActiveKey.
SELECT count(*) FROM totp_enrollments WHERE key_id <> sqlc.arg(active_key_id)::text;

-- name: ListTOTPActiveKeyIDs :many
-- Bounded key-ring health check across both tables without decrypting. A
-- healthy ring holds at most two distinct IDs (one active, at most one
-- previous); reading up to three is enough to prove a third, unknown ID
-- exists (docs/design/totp-key-management.md#key-failures).
SELECT key_id FROM totp_credentials
UNION
SELECT key_id FROM totp_enrollments WHERE expires_at > sqlc.arg(now)::timestamptz
LIMIT 3;

-- name: ClaimSecondFactorAttemptMailWindow :one
-- Conditional claim for the one-per-hour second_factor_attempts_exhausted
-- mail cap, under the policy lock. Zero rows means the window has not
-- elapsed and the caller must suppress the mail; the state change that
-- triggered the attempt still applies regardless
-- (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget).
UPDATE second_factor_policies
SET attempt_mail_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
  AND (attempt_mail_at IS NULL OR attempt_mail_at <= sqlc.arg(now)::timestamptz - interval '1 hour')
RETURNING *;
