-- +goose Up
CREATE TABLE privacy_notice_campaigns (
    id text PRIMARY KEY CHECK (id = 'crowdsec-postgres-paths-v1'),
    content_sha256 bytea NOT NULL CHECK (octet_length(content_sha256) = 32),
    cutoff_at timestamptz NOT NULL,
    expected_count bigint NOT NULL CHECK (expected_count >= 0),
    capture_cursor uuid,
    state text NOT NULL CHECK (state IN ('capturing', 'sealed', 'activated', 'aborted', 'retired')),
    sealed_at timestamptz,
    first_activated_at timestamptz,
    activated_at timestamptz,
    aborted_at timestamptz,
    retired_at timestamptz,
    purged_at timestamptz,
    activation_count bigint NOT NULL DEFAULT 0 CHECK (activation_count >= 0),
    accepted_count bigint CHECK (accepted_count >= 0),
    deleted_count bigint CHECK (deleted_count >= 0),
    last_accepted_at timestamptz,
    recovery_count bigint NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
    recovery_expired_count bigint NOT NULL DEFAULT 0 CHECK (recovery_expired_count >= 0),
    recovery_attempts_exhausted_count bigint NOT NULL DEFAULT 0 CHECK (recovery_attempts_exhausted_count >= 0),
    recovery_provider_rejected_count bigint NOT NULL DEFAULT 0 CHECK (recovery_provider_rejected_count >= 0),
    recovery_payload_invalid_count bigint NOT NULL DEFAULT 0 CHECK (recovery_payload_invalid_count >= 0),
    recovery_job_missing_count bigint NOT NULL DEFAULT 0 CHECK (recovery_job_missing_count >= 0),
    recovery_delivery_failed_count bigint NOT NULL DEFAULT 0 CHECK (recovery_delivery_failed_count >= 0),
    CONSTRAINT privacy_notice_campaigns_sealed_check CHECK (state NOT IN ('sealed', 'activated') OR sealed_at IS NOT NULL),
    CONSTRAINT privacy_notice_campaigns_activated_check CHECK ((state = 'activated') = (activated_at IS NOT NULL)),
    CONSTRAINT privacy_notice_campaigns_activation_history_check CHECK (
        (first_activated_at IS NULL) = (activation_count = 0)
        AND (state <> 'activated' OR activation_count > 0)
    ),
    CONSTRAINT privacy_notice_campaigns_recovery_check CHECK (
        recovery_count::numeric = recovery_expired_count::numeric + recovery_attempts_exhausted_count::numeric
            + recovery_provider_rejected_count::numeric + recovery_payload_invalid_count::numeric
            + recovery_job_missing_count::numeric + recovery_delivery_failed_count::numeric
    ),
    CONSTRAINT privacy_notice_campaigns_retired_check CHECK (state <> 'retired' OR retired_at IS NOT NULL),
    CONSTRAINT privacy_notice_campaigns_purged_check CHECK (purged_at IS NULL OR retired_at IS NOT NULL),
    CONSTRAINT privacy_notice_campaigns_totals_check CHECK (
        (accepted_count IS NULL AND deleted_count IS NULL AND last_accepted_at IS NULL)
        OR (accepted_count IS NOT NULL AND deleted_count IS NOT NULL
            AND accepted_count + deleted_count = expected_count
            AND ((accepted_count = 0 AND last_accepted_at IS NULL)
                OR (accepted_count > 0 AND last_accepted_at IS NOT NULL)))
    ),
    CONSTRAINT privacy_notice_campaigns_activation_totals_check CHECK (state <> 'activated' OR accepted_count IS NOT NULL)
);

CREATE TABLE privacy_notice_recipients (
    id uuid PRIMARY KEY,
    campaign_id text NOT NULL REFERENCES privacy_notice_campaigns(id) ON DELETE RESTRICT,
    user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    job_id uuid UNIQUE NOT NULL,
    state text NOT NULL CHECK (state IN ('unqueued', 'queued', 'accepted', 'deleted', 'failed', 'cancelled')),
    generation integer NOT NULL DEFAULT 0 CHECK (generation >= 0),
    queued_at timestamptz,
    accepted_at timestamptz,
    deleted_at timestamptz,
    failed_at timestamptz,
    cancelled_at timestamptz,
    failure_code text CHECK (failure_code IN ('expired', 'attempts_exhausted', 'provider_rejected', 'payload_invalid', 'job_missing', 'delivery_failed')),
    CONSTRAINT privacy_notice_recipients_outcome_check CHECK (
        (state = 'unqueued' AND failure_code IS NULL)
        OR (state = 'queued' AND queued_at IS NOT NULL AND failure_code IS NULL)
        OR (state = 'accepted' AND accepted_at IS NOT NULL AND failure_code IS NULL)
        OR (state = 'deleted' AND deleted_at IS NOT NULL AND user_id IS NULL AND failure_code IS NULL)
        OR (state = 'failed' AND failed_at IS NOT NULL AND failure_code IS NOT NULL)
        OR (state = 'cancelled' AND cancelled_at IS NOT NULL AND failure_code IS NULL)
    )
);
CREATE UNIQUE INDEX privacy_notice_recipients_campaign_user_idx
    ON privacy_notice_recipients(campaign_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX privacy_notice_recipients_state_idx ON privacy_notice_recipients(campaign_id, state, id);
CREATE INDEX privacy_notice_recipients_user_idx ON privacy_notice_recipients(user_id);

ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_kind_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_kind_check CHECK (
    kind IN ('verify', 'reset', 'password_changed', 'second_factor_enabled',
        'passkey_added', 'passkey_removed', 'second_factor_disabled',
        'recovery_codes_regenerated', 'recovery_code_used',
        'second_factor_attempts_exhausted', 'totp_added', 'totp_replaced',
        'totp_removed', 'privacy_notice')
);
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_scope_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_scope_check CHECK (
    (kind = 'verify' AND registration_id IS NOT NULL AND reset_token_id IS NULL AND user_id IS NULL)
    OR (kind = 'reset' AND reset_token_id IS NOT NULL AND registration_id IS NULL AND user_id IS NULL)
    OR (kind NOT IN ('verify', 'reset') AND user_id IS NOT NULL AND registration_id IS NULL AND reset_token_id IS NULL)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON privacy_notice_campaigns, privacy_notice_recipients TO aboutme_app;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    LOCK TABLE privacy_notice_campaigns, privacy_notice_recipients, auth_email_jobs IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM privacy_notice_campaigns)
        OR EXISTS (SELECT 1 FROM auth_email_jobs WHERE kind = 'privacy_notice') THEN
        RAISE EXCEPTION 'privacy notice down refused: lifetime evidence exists';
    END IF;
END
$$;
-- +goose StatementEnd
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
ALTER TABLE auth_email_jobs DROP CONSTRAINT auth_email_jobs_kind_check;
ALTER TABLE auth_email_jobs ADD CONSTRAINT auth_email_jobs_kind_check CHECK (
    kind IN ('verify', 'reset', 'password_changed', 'second_factor_enabled',
        'passkey_added', 'passkey_removed', 'second_factor_disabled',
        'recovery_codes_regenerated', 'recovery_code_used',
        'second_factor_attempts_exhausted', 'totp_added', 'totp_replaced', 'totp_removed')
);
DROP TABLE privacy_notice_recipients;
DROP TABLE privacy_notice_campaigns;
