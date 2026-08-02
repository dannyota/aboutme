-- Declarative schema for the aboutme database — the single source of truth
-- for both sqlc (type-safe queries) and migrations. See
-- docs/specs/aboutme-design.md §3 "Schema management" for the full
-- contract: `make migrate-gen` diffs this file against a throwaway
-- Postgres database with Atlas and writes goose-format SQL into
-- migrations/; apps/server/migrations/*.sql is generated output and
-- should not be hand-edited except through that pipeline.
--
-- Phase 1 (auth & sessions) tables — users, identities, sessions,
-- oauth_transactions — are defined below. Phase 2A's resume domain
-- (resumes, slug_tombstones) is NOT defined here yet; it lands per
-- docs/plans/implementation-plan.md.
--
-- citext was enabled ahead of Phase 1 (in Phase 0B, tasks 0.3/0.3b)
-- because it is infrastructure (an extension), not product schema:
-- `users.email citext UNIQUE` below (spec §3) needs it, and enabling it
-- early means the Phase 1 migration only adds tables, never mixes a
-- CREATE EXTENSION with the first CREATE TABLE it depends on.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    email citext NOT NULL,
    name text NOT NULL,
    avatar_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key UNIQUE (email)
);

CREATE TABLE identities (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('google', 'github', 'linkedin')),
    provider_user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT identities_provider_subject_key UNIQUE (provider, provider_user_id)
);
CREATE INDEX identities_user_id_idx ON identities (user_id);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL,
    csrf_secret bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    -- design decision 2: tracks the last full OAuth login this session's
    -- lineage completed; gates sensitive operations (link, unlink, delete,
    -- revoke-all).
    reauthenticated_at timestamptz NOT NULL DEFAULT now(),
    absolute_expires_at timestamptz NOT NULL,
    -- design decision 1: set only during >24h rotation; NULL for a session
    -- that has never rotated. Orthogonal to revoked_at.
    rotation_grace_until timestamptz,
    -- explicit hard revoke (logout, per-session revoke, revoke-all). Never
    -- set by rotation.
    revoked_at timestamptz,
    ua text,
    ip inet,
    -- fix round 3, DD-C14c (owner ruling): the exact rotation-lineage link
    -- a successor row carries back to the predecessor it was minted from
    -- -- set once, at INSERT time, by tryRotate's successor insert
    -- (internal/auth/session.go), never updated afterward. NULL for a
    -- session that has never been rotated INTO (i.e. every session that
    -- isn't itself a rotation successor: a fresh login, or a predecessor
    -- that hasn't yet rotated). ON DELETE SET NULL rather than CASCADE:
    -- this column only ever exists to let application code find a
    -- session's own lineage partner(s) -- a successor row must never be
    -- deleted just because its predecessor eventually is (predecessor
    -- rows are never actually DELETEd by this application at all, only
    -- revoked in place, but the constraint is written defensively rather
    -- than assuming that always holds).
    rotated_from uuid REFERENCES sessions (id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX sessions_user_id_active_idx ON sessions (user_id)
    WHERE revoked_at IS NULL;
-- A predecessor has AT MOST ONE successor -- BeginSessionRotation's
-- single-row conditional UPDATE (rotation_grace_until IS NULL AND
-- revoked_at IS NULL) is the CAS that guarantees only one caller ever
-- wins the right to mint a successor for a given predecessor; this
-- partial unique index makes the database itself enforce that invariant
-- too, rather than trusting the CAS alone.
CREATE UNIQUE INDEX sessions_rotated_from_key ON sessions (rotated_from)
    WHERE rotated_from IS NOT NULL;

CREATE TABLE oauth_transactions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    -- sha256 of the __Host-oauth-tx cookie value; the handle is a bearer
    -- credential, hashed at rest like the session token (design decision 3).
    handle_hash bytea NOT NULL,
    provider text NOT NULL CHECK (provider IN ('google', 'github', 'linkedin')),
    purpose text NOT NULL CHECK (purpose IN ('login', 'link', 'reauth')),
    linking_user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    -- cleartext: a correlator already visible in the redirect URL, not an
    -- independent secret (design decision 3).
    state text NOT NULL,
    pkce_verifier text NOT NULL,
    nonce text,
    redirect_uri text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT oauth_transactions_handle_hash_key UNIQUE (handle_hash),
    CONSTRAINT oauth_transactions_link_needs_user CHECK (
        (purpose IN ('link', 'reauth') AND linking_user_id IS NOT NULL)
        OR (purpose = 'login' AND linking_user_id IS NULL)
    )
);
CREATE INDEX oauth_transactions_expires_at_idx ON oauth_transactions (expires_at);
