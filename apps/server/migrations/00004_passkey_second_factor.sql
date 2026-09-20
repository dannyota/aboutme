-- +goose Up
DELETE FROM oauth_authorization_codes;

ALTER TABLE users ADD COLUMN auth_epoch bigint NOT NULL DEFAULT 0
    CONSTRAINT users_auth_epoch_check CHECK (auth_epoch >= 0);
ALTER TABLE sessions ADD COLUMN auth_epoch bigint NOT NULL DEFAULT 0
    CONSTRAINT sessions_auth_epoch_check CHECK (auth_epoch >= 0);
ALTER TABLE sessions ADD COLUMN second_factor_verified_at timestamptz NULL;
ALTER TABLE oauth_grants ADD COLUMN auth_epoch bigint NOT NULL DEFAULT 0
    CONSTRAINT oauth_grants_auth_epoch_check CHECK (auth_epoch >= 0);
ALTER TABLE oauth_authorization_codes ADD COLUMN grant_id uuid NULL;
ALTER TABLE oauth_authorization_codes ADD COLUMN auth_epoch bigint NULL
    CONSTRAINT oauth_authorization_codes_auth_epoch_check CHECK (auth_epoch >= 0);

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
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_token_digest_required_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_token_digest_required_check CHECK (
    (kind = ANY (ARRAY['verify'::text, 'reset'::text])) = (token_digest IS NOT NULL)
);

CREATE TABLE second_factor_policies (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    webauthn_user_handle bytea NOT NULL UNIQUE,
    enabled_at timestamptz NOT NULL,
    CONSTRAINT second_factor_policies_handle_length_check CHECK (octet_length(webauthn_user_handle) = 32)
);

CREATE TABLE webauthn_credentials (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE,
    public_key bytea NOT NULL,
    sign_count bigint NOT NULL DEFAULT 0,
    backup_eligible boolean NOT NULL,
    backup_state boolean NOT NULL,
    transports text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NULL,
    CONSTRAINT webauthn_credentials_credential_length_check CHECK (octet_length(credential_id) BETWEEN 16 AND 1023),
    CONSTRAINT webauthn_credentials_public_key_length_check CHECK (octet_length(public_key) BETWEEN 1 AND 2048),
    CONSTRAINT webauthn_credentials_sign_count_check CHECK (sign_count BETWEEN 0 AND 4294967295),
    CONSTRAINT webauthn_credentials_backup_state_check CHECK (backup_eligible OR NOT backup_state),
    CONSTRAINT webauthn_credentials_transports_check CHECK (transports <@ ARRAY['usb', 'nfc', 'ble', 'smart-card', 'hybrid', 'internal']::text[]),
    CONSTRAINT webauthn_credentials_last_used_order_check CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);
CREATE INDEX webauthn_credentials_user_created_id_idx ON webauthn_credentials (user_id, created_at, id);

CREATE TABLE second_factor_recovery_codes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_digest bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT second_factor_recovery_codes_digest_length_check CHECK (octet_length(code_digest) = 32)
);
CREATE INDEX second_factor_recovery_codes_user_digest_idx ON second_factor_recovery_codes (user_id, code_digest);

CREATE TABLE pending_authentications (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    token_digest bytea NOT NULL UNIQUE,
    csrf_secret bytea NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose text NOT NULL,
    auth_epoch bigint NOT NULL,
    session_id uuid NULL REFERENCES sessions (id) ON DELETE CASCADE,
    primary_verified_at timestamptz NOT NULL,
    return_path text NOT NULL,
    failed_attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    CONSTRAINT pending_authentications_token_length_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT pending_authentications_csrf_length_check CHECK (octet_length(csrf_secret) = 32),
    CONSTRAINT pending_authentications_purpose_check CHECK (purpose IN ('login', 'reauth')),
    CONSTRAINT pending_authentications_epoch_check CHECK (auth_epoch >= 0),
    CONSTRAINT pending_authentications_binding_check CHECK ((purpose = 'login' AND session_id IS NULL) OR (purpose = 'reauth' AND session_id IS NOT NULL)),
    CONSTRAINT pending_authentications_return_path_check CHECK (octet_length(return_path) BETWEEN 1 AND 2048 AND left(return_path, 1) = '/' AND left(return_path, 2) <> '//' AND position(chr(92) in return_path) = 0),
    CONSTRAINT pending_authentications_attempts_check CHECK (failed_attempts BETWEEN 0 AND 5),
    CONSTRAINT pending_authentications_expiry_check CHECK (expires_at = created_at + interval '5 minutes'),
    CONSTRAINT pending_authentications_consumed_order_check CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX pending_authentications_expires_id_idx ON pending_authentications (expires_at, id);

CREATE TABLE webauthn_ceremonies (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    token_digest bytea NOT NULL UNIQUE,
    challenge_digest bytea NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose text NOT NULL,
    auth_epoch bigint NOT NULL,
    session_id uuid NULL REFERENCES sessions (id) ON DELETE CASCADE,
    pending_authentication_id uuid NULL REFERENCES pending_authentications (id) ON DELETE CASCADE,
    proposed_user_handle bytea NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    CONSTRAINT webauthn_ceremonies_token_length_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT webauthn_ceremonies_challenge_length_check CHECK (octet_length(challenge_digest) = 32),
    CONSTRAINT webauthn_ceremonies_purpose_check CHECK (purpose IN ('registration', 'assertion')),
    CONSTRAINT webauthn_ceremonies_epoch_check CHECK (auth_epoch >= 0),
    CONSTRAINT webauthn_ceremonies_binding_check CHECK ((purpose = 'registration' AND session_id IS NOT NULL AND pending_authentication_id IS NULL) OR (purpose = 'assertion' AND session_id IS NULL AND pending_authentication_id IS NOT NULL)),
    CONSTRAINT webauthn_ceremonies_proposed_handle_check CHECK (proposed_user_handle IS NULL OR octet_length(proposed_user_handle) = 32),
    CONSTRAINT webauthn_ceremonies_expiry_check CHECK (expires_at = created_at + interval '5 minutes'),
    CONSTRAINT webauthn_ceremonies_consumed_order_check CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX webauthn_ceremonies_expires_id_idx ON webauthn_ceremonies (expires_at, id);

CREATE TABLE authentication_security_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    kind text NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    passkey_id uuid NOT NULL,
    stored_counter bigint NOT NULL,
    received_counter bigint NOT NULL,
    occurred_at timestamptz NOT NULL,
    CONSTRAINT authentication_security_events_kind_check CHECK (kind = 'passkey_counter_non_increasing'),
    CONSTRAINT authentication_security_events_stored_counter_check CHECK (stored_counter BETWEEN 0 AND 4294967295),
    CONSTRAINT authentication_security_events_received_counter_check CHECK (received_counter BETWEEN 0 AND 4294967295)
);
CREATE INDEX authentication_security_events_occurred_id_idx ON authentication_security_events (occurred_at, id);

ALTER TABLE oauth_grants
    ADD CONSTRAINT oauth_grants_id_user_client_key UNIQUE (id, user_id, client_id);
ALTER TABLE oauth_authorization_codes
    ADD CONSTRAINT oauth_authorization_codes_grant_id_user_client_fkey
    FOREIGN KEY (grant_id, user_id, client_id)
    REFERENCES oauth_grants (id, user_id, client_id) ON DELETE CASCADE;

-- +goose StatementBegin
CREATE FUNCTION fill_oauth_authorization_code_second_factor_bindings() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.grant_id IS NULL THEN
        SELECT grant_row.id INTO NEW.grant_id
        FROM oauth_grants AS grant_row
        WHERE grant_row.user_id = NEW.user_id
          AND grant_row.client_id = NEW.client_id
          AND grant_row.revoked_at IS NULL;
    END IF;
    IF NEW.auth_epoch IS NULL THEN
        SELECT user_row.auth_epoch INTO NEW.auth_epoch FROM users AS user_row WHERE user_row.id = NEW.user_id;
    END IF;
    IF NEW.grant_id IS NULL OR NEW.auth_epoch IS NULL THEN
        RAISE EXCEPTION 'missing oauth authorization code second-factor binding';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER oauth_authorization_codes_fill_second_factor_bindings
BEFORE INSERT ON oauth_authorization_codes
FOR EACH ROW EXECUTE FUNCTION fill_oauth_authorization_code_second_factor_bindings();

ALTER TABLE oauth_authorization_codes ALTER COLUMN grant_id SET NOT NULL;
ALTER TABLE oauth_authorization_codes ALTER COLUMN auth_epoch SET NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE
    second_factor_policies, webauthn_credentials, second_factor_recovery_codes,
    pending_authentications, webauthn_ceremonies, authentication_security_events
TO aboutme_app;

-- +goose Down
DELETE FROM auth_email_jobs
WHERE kind = ANY (ARRAY[
    'second_factor_enabled'::text, 'passkey_added'::text,
    'passkey_removed'::text, 'second_factor_disabled'::text,
    'recovery_codes_regenerated'::text, 'recovery_code_used'::text,
    'second_factor_attempts_exhausted'::text
]);
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_token_digest_required_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_token_digest_required_check CHECK (
    (kind = ANY (ARRAY['verify'::text, 'reset'::text])) = (token_digest IS NOT NULL)
);
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_scope_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_scope_check CHECK (
    (kind = 'verify' AND registration_id IS NOT NULL AND reset_token_id IS NULL AND user_id IS NULL)
    OR (kind = 'reset' AND reset_token_id IS NOT NULL AND registration_id IS NULL AND user_id IS NULL)
    OR (kind = 'password_changed' AND user_id IS NOT NULL AND registration_id IS NULL AND reset_token_id IS NULL)
);
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_kind_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_kind_check CHECK (
    kind = ANY (ARRAY['verify'::text, 'reset'::text, 'password_changed'::text])
);
DROP TRIGGER oauth_authorization_codes_fill_second_factor_bindings ON oauth_authorization_codes;
DROP FUNCTION fill_oauth_authorization_code_second_factor_bindings();
ALTER TABLE oauth_authorization_codes DROP CONSTRAINT oauth_authorization_codes_grant_id_user_client_fkey;
ALTER TABLE oauth_grants DROP CONSTRAINT oauth_grants_id_user_client_key;
ALTER TABLE oauth_authorization_codes DROP COLUMN auth_epoch;
ALTER TABLE oauth_authorization_codes DROP COLUMN grant_id;
DROP TABLE authentication_security_events;
DROP TABLE webauthn_ceremonies;
DROP TABLE pending_authentications;
DROP TABLE second_factor_recovery_codes;
DROP TABLE webauthn_credentials;
DROP TABLE second_factor_policies;
ALTER TABLE oauth_grants DROP COLUMN auth_epoch;
ALTER TABLE sessions DROP COLUMN second_factor_verified_at;
ALTER TABLE sessions DROP COLUMN auth_epoch;
ALTER TABLE users DROP COLUMN auth_epoch;
