-- name: GetTOTPCredentialForUpdate :one
SELECT * FROM totp_credentials WHERE user_id = sqlc.arg(user_id)::uuid FOR UPDATE;

-- name: CountTOTPCredentialsForUser :one
SELECT count(*) FROM totp_credentials WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: InstallTOTPCredential :one
INSERT INTO totp_credentials (
    id, user_id, key_id, nonce, ciphertext, format_version, last_used_step, created_at, updated_at
) VALUES (
    sqlc.arg(id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(key_id)::text, sqlc.arg(nonce)::bytea,
    sqlc.arg(ciphertext)::bytea, 1, sqlc.arg(last_used_step)::bigint,
    sqlc.arg(created_at)::timestamptz, sqlc.arg(created_at)::timestamptz
) RETURNING *;

-- name: ReplaceTOTPCredential :one
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
UPDATE totp_credentials
SET last_used_step = sqlc.arg(step)::bigint,
    updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
  AND sqlc.arg(step)::bigint > last_used_step
RETURNING *;

-- name: RecordTOTPCredentialFailure :one
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
UPDATE totp_credentials
SET failed_attempts = 0,
    cooldown_until = NULL,
    last_failed_at = NULL,
    updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
RETURNING *;

-- name: DeleteTOTPCredentialForUser :one
DELETE FROM totp_credentials WHERE user_id = sqlc.arg(user_id)::uuid RETURNING *;

-- name: ListTOTPCredentialsOffActiveKey :many
SELECT * FROM totp_credentials
WHERE key_id <> sqlc.arg(active_key_id)::text
  AND id > sqlc.arg(after_id)::uuid
ORDER BY id
LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
FOR UPDATE SKIP LOCKED;

-- name: ReencryptTOTPCredential :execrows
UPDATE totp_credentials
SET key_id = sqlc.arg(new_key_id)::text,
    nonce = sqlc.arg(nonce)::bytea,
    ciphertext = sqlc.arg(ciphertext)::bytea,
    updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id)::uuid AND key_id = sqlc.arg(old_key_id)::text;

-- name: CountTOTPCredentialsOffActiveKey :one
SELECT count(*) FROM totp_credentials WHERE key_id <> sqlc.arg(active_key_id)::text;

-- name: GetTOTPEnrollmentForUpdate :one
SELECT * FROM totp_enrollments WHERE token_digest = sqlc.arg(token_digest) FOR UPDATE;

-- name: CreateTOTPEnrollment :one
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
DELETE FROM totp_enrollments WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: DeleteTOTPEnrollmentByID :execrows
DELETE FROM totp_enrollments WHERE id = sqlc.arg(id)::uuid;

-- name: CleanupExpiredTOTPEnrollments :execrows
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
SELECT * FROM totp_enrollments
WHERE key_id <> sqlc.arg(active_key_id)::text
  AND id > sqlc.arg(after_id)::uuid
ORDER BY id
LIMIT LEAST(sqlc.arg(limit_rows)::int, 200)
FOR UPDATE SKIP LOCKED;

-- name: ReencryptTOTPEnrollment :execrows
UPDATE totp_enrollments
SET key_id = sqlc.arg(new_key_id)::text,
    nonce = sqlc.arg(nonce)::bytea,
    ciphertext = sqlc.arg(ciphertext)::bytea
WHERE id = sqlc.arg(id)::uuid AND key_id = sqlc.arg(old_key_id)::text;

-- name: CountTOTPEnrollmentsOffActiveKey :one
SELECT count(*) FROM totp_enrollments WHERE key_id <> sqlc.arg(active_key_id)::text;

-- name: ListTOTPActiveKeyIDs :many
SELECT key_id FROM totp_credentials
UNION
SELECT key_id FROM totp_enrollments WHERE expires_at > sqlc.arg(now)::timestamptz
LIMIT 3;

-- name: ClaimSecondFactorAttemptMailWindow :one
UPDATE second_factor_policies
SET attempt_mail_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid
  AND (attempt_mail_at IS NULL OR attempt_mail_at <= sqlc.arg(now)::timestamptz - interval '1 hour')
RETURNING *;
