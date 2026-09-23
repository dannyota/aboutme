-- +goose Up
CREATE TABLE totp_credentials (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    key_id text NOT NULL,
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    format_version smallint NOT NULL,
    last_used_step bigint NOT NULL,
    failed_attempts integer NOT NULL DEFAULT 0,
    cooldown_until timestamptz NULL,
    last_failed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL,
    CONSTRAINT totp_credentials_key_id_check CHECK (
        octet_length(key_id) = 26 AND key_id ~ '^tk1_[A-Za-z0-9_-]{22}$'
    ),
    CONSTRAINT totp_credentials_nonce_check CHECK (octet_length(nonce) = 12),
    CONSTRAINT totp_credentials_ciphertext_check CHECK (octet_length(ciphertext) = 36),
    CONSTRAINT totp_credentials_format_version_check CHECK (format_version = 1),
    CONSTRAINT totp_credentials_last_used_step_check CHECK (last_used_step >= 0),
    CONSTRAINT totp_credentials_failed_attempts_check CHECK (failed_attempts BETWEEN 0 AND 1000),
    CONSTRAINT totp_credentials_updated_order_check CHECK (updated_at >= created_at)
);
CREATE INDEX totp_credentials_key_id_idx ON totp_credentials (key_id);

CREATE TABLE totp_enrollments (
    id uuid PRIMARY KEY,
    token_digest bytea NOT NULL UNIQUE,
    user_id uuid NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    session_id uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    auth_epoch bigint NOT NULL,
    issuer text NOT NULL,
    key_id text NOT NULL,
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    format_version smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT totp_enrollments_token_digest_length_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT totp_enrollments_epoch_check CHECK (auth_epoch >= 0),
    CONSTRAINT totp_enrollments_issuer_check CHECK (
        octet_length(issuer) BETWEEN 1 AND 253
        AND issuer ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$'
    ),
    CONSTRAINT totp_enrollments_key_id_check CHECK (
        octet_length(key_id) = 26 AND key_id ~ '^tk1_[A-Za-z0-9_-]{22}$'
    ),
    CONSTRAINT totp_enrollments_nonce_check CHECK (octet_length(nonce) = 12),
    CONSTRAINT totp_enrollments_ciphertext_check CHECK (octet_length(ciphertext) = 36),
    CONSTRAINT totp_enrollments_format_version_check CHECK (format_version = 1),
    CONSTRAINT totp_enrollments_expiry_check CHECK (expires_at = created_at + interval '10 minutes')
);
CREATE INDEX totp_enrollments_key_id_idx ON totp_enrollments (key_id);
CREATE INDEX totp_enrollments_expires_id_idx ON totp_enrollments (expires_at, id);

ALTER TABLE second_factor_policies ADD COLUMN attempt_mail_at timestamptz NULL;

ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_kind_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_kind_check CHECK (
    kind = ANY (ARRAY[
        'verify'::text, 'reset'::text, 'password_changed'::text,
        'second_factor_enabled'::text, 'passkey_added'::text,
        'passkey_removed'::text, 'second_factor_disabled'::text,
        'recovery_codes_regenerated'::text, 'recovery_code_used'::text,
        'second_factor_attempts_exhausted'::text,
        'totp_added'::text, 'totp_replaced'::text, 'totp_removed'::text
    ])
);
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_scope_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_scope_check CHECK (
    (kind = 'verify' AND registration_id IS NOT NULL AND reset_token_id IS NULL AND user_id IS NULL)
    OR (kind = 'reset' AND reset_token_id IS NOT NULL AND registration_id IS NULL AND user_id IS NULL)
    OR (kind = 'password_changed' AND user_id IS NOT NULL AND registration_id IS NULL AND reset_token_id IS NULL)
    OR (kind = ANY (ARRAY[
        'second_factor_enabled'::text, 'passkey_added'::text,
        'passkey_removed'::text, 'second_factor_disabled'::text,
        'recovery_codes_regenerated'::text, 'recovery_code_used'::text,
        'second_factor_attempts_exhausted'::text,
        'totp_added'::text, 'totp_replaced'::text, 'totp_removed'::text
    ]) AND registration_id IS NULL AND reset_token_id IS NULL AND user_id IS NOT NULL)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE totp_credentials, totp_enrollments TO aboutme_app;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM totp_credentials LIMIT 1) THEN
        RAISE EXCEPTION 'totp_second_factor down migration refused: totp_credentials has rows';
    END IF;
END;
$$;
-- +goose StatementEnd

DELETE FROM auth_email_jobs
WHERE kind = ANY (ARRAY['totp_added'::text, 'totp_replaced'::text, 'totp_removed'::text]);

ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_scope_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_scope_check CHECK (
    (kind = 'verify' AND registration_id IS NOT NULL AND reset_token_id IS NULL AND user_id IS NULL)
    OR (kind = 'reset' AND reset_token_id IS NOT NULL AND registration_id IS NULL AND user_id IS NULL)
    OR (kind = 'password_changed' AND user_id IS NOT NULL AND registration_id IS NULL AND reset_token_id IS NULL)
    OR (kind = ANY (ARRAY[
        'second_factor_enabled'::text, 'passkey_added'::text,
        'passkey_removed'::text, 'second_factor_disabled'::text,
        'recovery_codes_regenerated'::text, 'recovery_code_used'::text,
        'second_factor_attempts_exhausted'::text
    ]) AND registration_id IS NULL AND reset_token_id IS NULL AND user_id IS NOT NULL)
);
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_kind_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_kind_check CHECK (
    kind = ANY (ARRAY[
        'verify'::text, 'reset'::text, 'password_changed'::text,
        'second_factor_enabled'::text, 'passkey_added'::text,
        'passkey_removed'::text, 'second_factor_disabled'::text,
        'recovery_codes_regenerated'::text, 'recovery_code_used'::text,
        'second_factor_attempts_exhausted'::text
    ])
);

DROP TABLE totp_enrollments;
DROP TABLE totp_credentials;
ALTER TABLE second_factor_policies DROP COLUMN attempt_mail_at;
