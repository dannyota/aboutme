-- +goose Up
-- Baseline migration (ADR 0038): the full schema before the first
-- production deploy. Column order, constraint names, index names, and
-- function/trigger bodies match the released schema exactly, so sqlc
-- output stays byte-identical. See ADR 0038 and docs/design/data.md.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id uuid NOT NULL DEFAULT uuidv7(),
    email public.citext NOT NULL,
    name text NOT NULL,
    avatar_key text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT users_email_key UNIQUE (email)
);

CREATE TABLE identities (
    id uuid NOT NULL DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    provider text NOT NULL,
    provider_user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT identities_provider_subject_key UNIQUE (provider, provider_user_id),
    CONSTRAINT identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT identities_provider_check CHECK (provider = ANY (ARRAY['google'::text, 'github'::text, 'linkedin'::text]))
);
CREATE INDEX identities_user_id_idx ON identities (user_id);

CREATE TABLE oauth_transactions (
    id uuid NOT NULL DEFAULT uuidv7(),
    handle_hash bytea NOT NULL,
    provider text NOT NULL,
    purpose text NOT NULL,
    linking_user_id uuid NULL,
    state text NOT NULL,
    pkce_verifier text NOT NULL,
    nonce text NULL,
    redirect_uri text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    return_path text NOT NULL DEFAULT '/app/resumes',
    PRIMARY KEY (id),
    CONSTRAINT oauth_transactions_handle_hash_key UNIQUE (handle_hash),
    CONSTRAINT oauth_transactions_linking_user_id_fkey FOREIGN KEY (linking_user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_transactions_link_needs_user CHECK (((purpose = ANY (ARRAY['link'::text, 'reauth'::text])) AND (linking_user_id IS NOT NULL)) OR ((purpose = 'login'::text) AND (linking_user_id IS NULL))),
    CONSTRAINT oauth_transactions_provider_check CHECK (provider = ANY (ARRAY['google'::text, 'github'::text, 'linkedin'::text])),
    CONSTRAINT oauth_transactions_purpose_check CHECK (purpose = ANY (ARRAY['login'::text, 'link'::text, 'reauth'::text])),
    CONSTRAINT oauth_transactions_return_path_check CHECK (
        octet_length(return_path) BETWEEN 1 AND 2048
        AND left(return_path, 1) = '/'
        AND left(return_path, 2) <> '//'
        AND position(chr(92) in return_path) = 0
    )
);
CREATE INDEX oauth_transactions_expires_at_idx ON oauth_transactions (expires_at);

CREATE TABLE sessions (
    id uuid NOT NULL DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    token_hash bytea NOT NULL,
    csrf_secret bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    reauthenticated_at timestamptz NOT NULL DEFAULT now(),
    absolute_expires_at timestamptz NOT NULL,
    rotation_grace_until timestamptz NULL,
    revoked_at timestamptz NULL,
    ua text NULL,
    ip inet NULL,
    rotated_from uuid NULL,
    PRIMARY KEY (id),
    CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT sessions_rotated_from_fkey FOREIGN KEY (rotated_from) REFERENCES sessions (id) ON UPDATE NO ACTION ON DELETE SET NULL
);
CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX sessions_user_id_active_idx ON sessions (user_id) WHERE (revoked_at IS NULL);
CREATE UNIQUE INDEX sessions_rotated_from_key ON sessions (rotated_from) WHERE (rotated_from IS NOT NULL);
CREATE INDEX sessions_metadata_age_idx ON sessions (created_at, id) WHERE ua IS NOT NULL OR ip IS NOT NULL;

CREATE TABLE idempotency_records (
    id uuid NOT NULL DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    route text NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    response_headers jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (id),
    CONSTRAINT idempotency_records_user_route_key_key UNIQUE (user_id, route, idempotency_key),
    CONSTRAINT idempotency_records_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT idempotency_records_response_headers_object_check CHECK (jsonb_typeof(response_headers) = 'object')
);
CREATE INDEX idempotency_records_expires_at_idx ON idempotency_records (expires_at);
CREATE INDEX idempotency_records_user_expires_id_idx ON idempotency_records (user_id, expires_at, id);

CREATE TABLE resumes (
    id uuid NOT NULL DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    title text NOT NULL,
    slug text NULL,
    live boolean NOT NULL DEFAULT false,
    download_enabled boolean NOT NULL DEFAULT true,
    seo_geo_enabled boolean NOT NULL DEFAULT false,
    schema_version integer NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    lng text NULL,
    personal_details jsonb NOT NULL,
    content jsonb NOT NULL,
    customization jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT resumes_slug_key UNIQUE (slug),
    CONSTRAINT resumes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT resumes_live_requires_slug_check CHECK ((NOT live) OR (slug IS NOT NULL)),
    CONSTRAINT resumes_lng_length_check CHECK ((lng IS NULL) OR (char_length(lng) <= 35)),
    CONSTRAINT resumes_revision_check CHECK (revision >= 1),
    CONSTRAINT resumes_schema_version_check CHECK (schema_version >= 1),
    CONSTRAINT resumes_seo_requires_live_check CHECK ((NOT seo_geo_enabled) OR live),
    CONSTRAINT resumes_slug_format_check CHECK ((slug IS NULL) OR ((char_length(slug) >= 4) AND (char_length(slug) <= 30) AND (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text))),
    CONSTRAINT resumes_title_length_check CHECK (char_length(title) <= 160)
);
CREATE INDEX resumes_user_id_idx ON resumes (user_id);
CREATE INDEX resumes_photo_reference_idx
    ON resumes ((personal_details -> 'photo' ->> 'key'))
    WHERE personal_details -> 'photo' ->> 'key' IS NOT NULL;

CREATE TABLE slug_tombstones (
    id uuid NOT NULL DEFAULT uuidv7(),
    slug text NOT NULL,
    released_by_user_id uuid NULL,
    released_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT slug_tombstones_slug_key UNIQUE (slug),
    CONSTRAINT slug_tombstones_released_by_user_id_fkey FOREIGN KEY (released_by_user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE SET NULL,
    CONSTRAINT slug_tombstones_slug_format_check CHECK ((char_length(slug) >= 4) AND (char_length(slug) <= 30) AND (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text))
);

CREATE TABLE idempotency_usage (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    retained_records bigint NOT NULL,
    stored_bytes bigint NOT NULL,
    CONSTRAINT idempotency_usage_retained_records_nonnegative_check CHECK (retained_records >= 0),
    CONSTRAINT idempotency_usage_stored_bytes_nonnegative_check CHECK (stored_bytes >= 0)
);

CREATE TABLE media_deletion_jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    resume_id uuid NOT NULL,
    object_key text NOT NULL,
    enqueued_at timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    attempt_count integer NOT NULL DEFAULT 0,
    lease_id uuid,
    lease_expires_at timestamptz,
    completed_at timestamptz,
    overdue_at timestamptz,
    outcome text,
    PRIMARY KEY (id),
    CONSTRAINT media_deletion_jobs_object_key_key UNIQUE (object_key),
    CONSTRAINT media_deletion_jobs_attempt_count_nonnegative_check
        CHECK (attempt_count >= 0),
    -- The server-derived key grammar, with the embedded canonical resume
    -- UUID required to equal resume_id (resume_id::text is the canonical
    -- lowercase hyphenated form). A malformed or cross-resume key can
    -- never enter the ledger.
    CONSTRAINT media_deletion_jobs_key_matches_resume_check
        CHECK (object_key ~ ('^resumes/' || resume_id::text || '/photo-[0-9a-f]{32}\.(jpg|png)$')),
    CONSTRAINT media_deletion_jobs_lease_check CHECK (
        (lease_id IS NULL AND lease_expires_at IS NULL)
        OR (lease_id IS NOT NULL AND lease_expires_at IS NOT NULL
            AND completed_at IS NULL AND lease_expires_at >= enqueued_at)
    ),
    CONSTRAINT media_deletion_jobs_completion_check CHECK (
        (completed_at IS NULL AND outcome IS NULL)
        OR (completed_at IS NOT NULL AND completed_at >= enqueued_at
            AND outcome IS NOT NULL AND outcome IN ('deleted', 'absent'))
    ),
    CONSTRAINT media_deletion_jobs_overdue_check CHECK (
        overdue_at IS NULL OR overdue_at >= enqueued_at + interval '24 hours'
    )
);
-- Bounded oldest-due claims for the drain worker, partial on rows still
-- pending completion.
CREATE INDEX media_deletion_jobs_next_attempt_idx
    ON media_deletion_jobs (next_attempt_at, id) WHERE completed_at IS NULL;
CREATE INDEX media_deletion_jobs_pending_age_idx
    ON media_deletion_jobs (enqueued_at, id) WHERE completed_at IS NULL;
CREATE INDEX media_deletion_jobs_completed_idx
    ON media_deletion_jobs (completed_at, id) WHERE completed_at IS NOT NULL;

-- Durable aggregate discovery generation. Exactly one checked row exists;
-- startup reads it before readiness and every membership-changing mutation
-- locks and advances it in the same transaction as the public-state write.
CREATE TABLE public_state (
    singleton boolean PRIMARY KEY DEFAULT true,
    discovery_generation bigint NOT NULL,
    CONSTRAINT public_state_singleton_check CHECK (singleton),
    CONSTRAINT public_state_discovery_generation_positive_check
        CHECK (discovery_generation > 0)
);
INSERT INTO public_state (singleton, discovery_generation) VALUES (true, 1);

CREATE TABLE password_credentials (
    user_id uuid NOT NULL,
    encoded_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    changed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id),
    CONSTRAINT password_credentials_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT password_credentials_encoded_hash_length_check CHECK (octet_length(encoded_hash) BETWEEN 1 AND 192),
    CONSTRAINT password_credentials_changed_after_created_check CHECK (changed_at >= created_at)
);

CREATE TABLE password_registrations (
    id uuid NOT NULL DEFAULT uuidv7(),
    email public.citext NOT NULL,
    name text NOT NULL,
    encoded_hash bytea NOT NULL,
    token_digest bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT password_registrations_email_key UNIQUE (email),
    CONSTRAINT password_registrations_token_digest_key UNIQUE (token_digest),
    CONSTRAINT password_registrations_name_length_check CHECK (octet_length(name) BETWEEN 1 AND 400),
    CONSTRAINT password_registrations_encoded_hash_length_check CHECK (octet_length(encoded_hash) BETWEEN 1 AND 192),
    CONSTRAINT password_registrations_token_digest_length_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT password_registrations_expiry_check CHECK (expires_at = created_at + interval '24 hours')
);
CREATE INDEX password_registrations_expires_at_idx ON password_registrations (expires_at);

CREATE TABLE password_reset_tokens (
    id uuid NOT NULL DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    token_digest bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT password_reset_tokens_user_id_key UNIQUE (user_id),
    CONSTRAINT password_reset_tokens_token_digest_key UNIQUE (token_digest),
    CONSTRAINT password_reset_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT password_reset_tokens_token_digest_length_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT password_reset_tokens_expiry_check CHECK (expires_at = created_at + interval '30 minutes')
);
CREATE INDEX password_reset_tokens_expires_at_idx ON password_reset_tokens (expires_at);

CREATE TABLE auth_email_jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    kind text NOT NULL,
    state text NOT NULL,
    registration_id uuid NULL,
    reset_token_id uuid NULL,
    user_id uuid NULL,
    token_digest bytea NULL,
    key_id text NULL,
    nonce bytea NULL,
    ciphertext bytea NULL,
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    next_attempt_at timestamptz NULL,
    lease_owner text NULL,
    lease_expires_at timestamptz NULL,
    sent_at timestamptz NULL,
    terminal_at timestamptz NULL,
    PRIMARY KEY (id),
    CONSTRAINT auth_email_jobs_registration_id_fkey FOREIGN KEY (registration_id) REFERENCES password_registrations (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT auth_email_jobs_reset_token_id_fkey FOREIGN KEY (reset_token_id) REFERENCES password_reset_tokens (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT auth_email_jobs_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT auth_email_jobs_kind_check CHECK (kind = ANY (ARRAY['verify'::text, 'reset'::text, 'password_changed'::text])),
    CONSTRAINT auth_email_jobs_state_check CHECK (state = ANY (ARRAY['pending'::text, 'leased'::text, 'sent'::text, 'terminal'::text])),
    CONSTRAINT auth_email_jobs_scope_check CHECK (
        (kind = 'verify' AND registration_id IS NOT NULL AND reset_token_id IS NULL AND user_id IS NULL)
        OR (kind = 'reset' AND reset_token_id IS NOT NULL AND registration_id IS NULL AND user_id IS NULL)
        OR (kind = 'password_changed' AND user_id IS NOT NULL AND registration_id IS NULL AND reset_token_id IS NULL)
    ),
    CONSTRAINT auth_email_jobs_token_digest_required_check CHECK (
        (kind = ANY (ARRAY['verify'::text, 'reset'::text])) = (token_digest IS NOT NULL)
    ),
    CONSTRAINT auth_email_jobs_token_digest_length_check CHECK (token_digest IS NULL OR octet_length(token_digest) = 32),
    CONSTRAINT auth_email_jobs_key_id_check CHECK (key_id IS NULL OR (octet_length(key_id) BETWEEN 1 AND 64 AND key_id !~ '[^ -~]')),
    CONSTRAINT auth_email_jobs_nonce_check CHECK (nonce IS NULL OR octet_length(nonce) = 12),
    CONSTRAINT auth_email_jobs_ciphertext_check CHECK (ciphertext IS NULL OR octet_length(ciphertext) BETWEEN 1 AND 4112),
    CONSTRAINT auth_email_jobs_attempts_check CHECK (
        (state = 'pending' AND attempts BETWEEN 0 AND 7)
        OR (state = ANY (ARRAY['leased'::text, 'sent'::text]) AND attempts BETWEEN 1 AND 8)
        OR (state = 'terminal' AND attempts BETWEEN 0 AND 8)
    ),
    CONSTRAINT auth_email_jobs_expiry_check CHECK (expires_at <= created_at + interval '24 hours'),
    CONSTRAINT auth_email_jobs_state_matrix_check CHECK (
        (state = 'pending' AND key_id IS NOT NULL AND nonce IS NOT NULL AND ciphertext IS NOT NULL
            AND next_attempt_at IS NOT NULL AND lease_owner IS NULL AND lease_expires_at IS NULL
            AND sent_at IS NULL AND terminal_at IS NULL)
        OR (state = 'leased' AND key_id IS NOT NULL AND nonce IS NOT NULL AND ciphertext IS NOT NULL
            AND next_attempt_at IS NULL AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL
            AND sent_at IS NULL AND terminal_at IS NULL)
        OR (state = 'sent' AND key_id IS NULL AND nonce IS NULL AND ciphertext IS NULL
            AND next_attempt_at IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL
            AND sent_at IS NOT NULL AND terminal_at IS NULL)
        OR (state = 'terminal' AND key_id IS NULL AND nonce IS NULL AND ciphertext IS NULL
            AND next_attempt_at IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL
            AND sent_at IS NULL AND terminal_at IS NOT NULL)
    )
);
CREATE INDEX auth_email_jobs_claim_idx ON auth_email_jobs (next_attempt_at, created_at, id) WHERE (state = 'pending');
CREATE INDEX auth_email_jobs_outcome_idx ON auth_email_jobs (sent_at, terminal_at) WHERE (state = ANY (ARRAY['sent'::text, 'terminal'::text]));

CREATE TABLE oauth_clients (
    id uuid NOT NULL DEFAULT uuidv7(),
    client_name text NOT NULL,
    redirect_uris jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT oauth_clients_client_name_length_check CHECK (
        char_length(client_name) BETWEEN 1 AND 64
        AND octet_length(client_name) BETWEEN 1 AND 256
    ),
    CONSTRAINT oauth_clients_client_name_control_check CHECK (client_name !~ '[[:cntrl:]]'),
    CONSTRAINT oauth_clients_redirect_uris_check CHECK (
        jsonb_typeof(redirect_uris) = 'array'
        AND jsonb_array_length(redirect_uris) BETWEEN 1 AND 5
        AND NOT jsonb_path_exists(redirect_uris, '$[*] ? (@.type() != "string")')
        AND NOT jsonb_path_exists(redirect_uris, '$[*] ? (!(@ like_regex "^[!-~]{1,255}[!-~]{0,255}[!-~]{0,2}$"))')
    ),
    CONSTRAINT oauth_clients_last_used_order_check CHECK (last_used_at >= created_at)
);
-- Drives the bounded idle-client sweep oldest-first, so repeated sweeps
-- make monotonic progress instead of rescanning the same rows.
CREATE INDEX oauth_clients_created_at_idx ON oauth_clients (created_at, id);

CREATE TABLE oauth_authorization_codes (
    id uuid NOT NULL DEFAULT uuidv7(),
    code_digest bytea NOT NULL,
    client_id uuid NOT NULL,
    user_id uuid NOT NULL,
    scopes text NOT NULL,
    code_challenge text NOT NULL,
    redirect_uri text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    issued_family_id uuid NULL,
    PRIMARY KEY (id),
    CONSTRAINT oauth_authorization_codes_code_digest_key UNIQUE (code_digest),
    CONSTRAINT oauth_authorization_codes_client_id_fkey FOREIGN KEY (client_id) REFERENCES oauth_clients (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_authorization_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_authorization_codes_code_digest_length_check CHECK (octet_length(code_digest) = 32),
    CONSTRAINT oauth_authorization_codes_scopes_check CHECK (
        scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text])
    ),
    CONSTRAINT oauth_authorization_codes_code_challenge_check CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
    CONSTRAINT oauth_authorization_codes_redirect_uri_check CHECK (
        redirect_uri ~ '^[!-~]{1,255}[!-~]{0,255}[!-~]{0,2}$'
    ),
    CONSTRAINT oauth_authorization_codes_expiry_check CHECK (expires_at = created_at + interval '60 seconds'),
    CONSTRAINT oauth_authorization_codes_consumed_family_check CHECK (
        (consumed_at IS NULL) = (issued_family_id IS NULL)
    ),
    CONSTRAINT oauth_authorization_codes_consumed_order_check CHECK (
        consumed_at IS NULL OR consumed_at >= created_at
    )
);
CREATE INDEX oauth_authorization_codes_expires_at_idx ON oauth_authorization_codes (expires_at, id);
CREATE INDEX oauth_authorization_codes_client_id_idx ON oauth_authorization_codes (client_id);
CREATE INDEX oauth_authorization_codes_user_id_idx ON oauth_authorization_codes (user_id);

-- The partial unique index -- not application code -- is what makes a
-- second live grant for one (user, client) impossible: two concurrent
-- consent approvals contend on it and exactly one survives. A revoked
-- grant keeps its row for audit and leaves the live slot free.
CREATE TABLE oauth_grants (
    id uuid NOT NULL DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    client_id uuid NOT NULL,
    scopes text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz NULL,
    PRIMARY KEY (id),
    CONSTRAINT oauth_grants_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_grants_client_id_fkey FOREIGN KEY (client_id) REFERENCES oauth_clients (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_grants_scopes_check CHECK (
        scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text])
    ),
    CONSTRAINT oauth_grants_revoked_order_check CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE UNIQUE INDEX oauth_grants_live_user_client_key ON oauth_grants (user_id, client_id) WHERE (revoked_at IS NULL);
CREATE INDEX oauth_grants_client_live_idx ON oauth_grants (client_id) WHERE (revoked_at IS NULL);

-- Access and refresh tokens share one table keyed by a 32-byte digest. A
-- refresh family is identified by family_id and dies at family_expires_at;
-- no token may outlive its family, and an access token may not outlive one
-- hour. rotated_from is the exact one-successor lineage link, mirroring
-- sessions: the partial unique index below means one predecessor can never
-- mint two successors, so a rotation race has exactly one winner.
CREATE TABLE oauth_tokens (
    id uuid NOT NULL DEFAULT uuidv7(),
    token_digest bytea NOT NULL,
    kind text NOT NULL,
    family_id uuid NOT NULL,
    rotated_from uuid NULL,
    client_id uuid NOT NULL,
    user_id uuid NOT NULL,
    grant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    family_expires_at timestamptz NOT NULL,
    revoked_at timestamptz NULL,
    superseded_at timestamptz NULL,
    last_used_at timestamptz NULL,
    PRIMARY KEY (id),
    CONSTRAINT oauth_tokens_token_digest_key UNIQUE (token_digest),
    CONSTRAINT oauth_tokens_rotated_from_fkey FOREIGN KEY (rotated_from) REFERENCES oauth_tokens (id) ON UPDATE NO ACTION ON DELETE SET NULL,
    CONSTRAINT oauth_tokens_client_id_fkey FOREIGN KEY (client_id) REFERENCES oauth_clients (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_tokens_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES oauth_grants (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT oauth_tokens_token_digest_length_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT oauth_tokens_kind_check CHECK (kind = ANY (ARRAY['access'::text, 'refresh'::text])),
    CONSTRAINT oauth_tokens_rotated_from_self_check CHECK (rotated_from IS NULL OR rotated_from <> id),
    CONSTRAINT oauth_tokens_expiry_order_check CHECK (expires_at > created_at AND expires_at <= family_expires_at),
    CONSTRAINT oauth_tokens_family_lifetime_check CHECK (family_expires_at <= created_at + interval '30 days'),
    CONSTRAINT oauth_tokens_access_lifetime_check CHECK (
        kind <> 'access' OR expires_at <= created_at + interval '1 hour'
    ),
    CONSTRAINT oauth_tokens_revoked_order_check CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT oauth_tokens_superseded_order_check CHECK (superseded_at IS NULL OR superseded_at >= created_at),
    CONSTRAINT oauth_tokens_last_used_order_check CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);
CREATE UNIQUE INDEX oauth_tokens_rotated_from_key ON oauth_tokens (rotated_from) WHERE (rotated_from IS NOT NULL);
CREATE INDEX oauth_tokens_family_id_idx ON oauth_tokens (family_id);
CREATE INDEX oauth_tokens_grant_id_idx ON oauth_tokens (grant_id);
CREATE INDEX oauth_tokens_user_id_idx ON oauth_tokens (user_id);
CREATE INDEX oauth_tokens_client_live_idx ON oauth_tokens (client_id) WHERE (revoked_at IS NULL AND superseded_at IS NULL);
CREATE INDEX oauth_tokens_cleanup_idx ON oauth_tokens (expires_at, id);

CREATE TABLE lifecycle_audit_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    kind text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    media_job_id uuid,
    CONSTRAINT lifecycle_audit_events_kind_check CHECK (
        kind IN ('account_deleted', 'media_deletion_overdue', 'media_deletion_completed')
    ),
    CONSTRAINT lifecycle_audit_events_scope_check CHECK (
        (kind = 'account_deleted' AND media_job_id IS NULL)
        OR (kind IN ('media_deletion_overdue', 'media_deletion_completed')
            AND media_job_id IS NOT NULL)
    ),
    CONSTRAINT lifecycle_audit_events_job_kind_key UNIQUE (media_job_id, kind)
);
CREATE INDEX lifecycle_audit_events_expiry_idx ON lifecycle_audit_events (occurred_at, id);

CREATE TABLE privacy_sweep_state (
    name text PRIMARY KEY,
    cursor text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_sweep_state_name_check CHECK (name = 'media-orphan-sweep'),
    CONSTRAINT privacy_sweep_state_cursor_check CHECK (
        octet_length(cursor) <= 1024 AND cursor !~ '[[:cntrl:]]'
    )
);
INSERT INTO privacy_sweep_state (name) VALUES ('media-orphan-sweep');

-- Enforces the three-resume-per-user cap under concurrent writers; see
-- docs/design/data.md.
-- +goose StatementBegin
CREATE FUNCTION enforce_resume_cap() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- A no-op assignment does not increase either owner's count. Returning
    -- before the lock also avoids serializing an update that cannot affect the
    -- cap.
    IF TG_OP = 'UPDATE' AND NEW.user_id IS NOT DISTINCT FROM OLD.user_id THEN
        RETURN NEW;
    END IF;

    -- Serialize per-owner (D7). The lock blocks a competing writer; the
    -- count that follows then takes a FRESH snapshot and sees the row
    -- that writer committed. This holds even for writers that bypass the
    -- store layer -- but only under READ COMMITTED, which is Postgres's
    -- default and which every aboutme transaction must keep. At
    -- REPEATABLE READ the count would read a snapshot taken before the
    -- lock was granted, still see 2 rows, and admit a 4th resume.
    -- The store's create tx takes this same lock first (spec: belt and
    -- suspenders); identical order, no deadlock.
    PERFORM 1 FROM users WHERE id = NEW.user_id FOR UPDATE;
    IF (SELECT count(*) FROM resumes WHERE user_id = NEW.user_id) >= 3 THEN
        RAISE EXCEPTION 'resumes_user_cap_exceeded'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER resumes_enforce_cap
BEFORE INSERT OR UPDATE OF user_id ON resumes
FOR EACH ROW EXECUTE FUNCTION enforce_resume_cap();

-- +goose StatementBegin
CREATE FUNCTION notify_resume_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    changed resumes%ROWTYPE;
    next_revision bigint;
BEGIN
    IF TG_OP = 'DELETE' THEN
        changed := OLD;
        next_revision := CASE WHEN OLD.revision = 9223372036854775807
            THEN OLD.revision ELSE OLD.revision + 1 END;
    ELSE
        changed := NEW;
        next_revision := NEW.revision;
    END IF;
    PERFORM pg_notify('aboutme_resume_revision', json_build_object(
        'account_id', changed.user_id,
        'resume_id', changed.id,
        'revision', next_revision,
        'deleted', TG_OP = 'DELETE'
    )::text);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER resume_revision_notification
AFTER INSERT OR UPDATE OR DELETE ON resumes
FOR EACH ROW EXECUTE FUNCTION notify_resume_revision();

-- aboutme_app gets exactly SELECT/INSERT/UPDATE/DELETE on every business
-- table; nothing on goose_db_version. Every later migration grants
-- explicitly -- there is no ALTER DEFAULT PRIVILEGES.
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE
    users, identities, oauth_transactions, sessions, idempotency_records,
    resumes, slug_tombstones, idempotency_usage, media_deletion_jobs,
    public_state, password_credentials, password_registrations,
    password_reset_tokens, auth_email_jobs, oauth_clients,
    oauth_authorization_codes, oauth_grants, oauth_tokens,
    lifecycle_audit_events, privacy_sweep_state
TO aboutme_app;

-- +goose Down
DROP TRIGGER resume_revision_notification ON resumes;
DROP FUNCTION notify_resume_revision();
DROP TRIGGER resumes_enforce_cap ON resumes;
DROP FUNCTION enforce_resume_cap();

DROP TABLE privacy_sweep_state;
DROP TABLE lifecycle_audit_events;
DROP TABLE oauth_tokens;
DROP TABLE oauth_grants;
DROP TABLE oauth_authorization_codes;
DROP TABLE oauth_clients;
DROP TABLE auth_email_jobs;
DROP TABLE password_reset_tokens;
DROP TABLE password_registrations;
DROP TABLE password_credentials;
DROP TABLE public_state;
DROP TABLE media_deletion_jobs;
DROP TABLE idempotency_usage;
DROP TABLE slug_tombstones;
DROP TABLE resumes;
DROP TABLE idempotency_records;
DROP TABLE sessions;
DROP TABLE oauth_transactions;
DROP TABLE identities;
DROP TABLE users;

DROP EXTENSION IF EXISTS citext;
