-- +goose Up
SET LOCAL lock_timeout = '5s';

ALTER TABLE resumes
    ADD COLUMN session_actor text,
    ADD COLUMN session_started_at timestamptz,
    ADD COLUMN session_last_write_at timestamptz,
    ADD CONSTRAINT resumes_session_actor_check CHECK (
        session_actor = 'owner' OR session_actor ~ '^grant:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ),
    ADD CONSTRAINT resumes_session_columns_check CHECK (
        num_nonnulls(session_actor, session_started_at, session_last_write_at) IN (0, 3)
    );

CREATE TABLE resume_checkpoints (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    resume_id uuid NOT NULL REFERENCES resumes (id) ON DELETE CASCADE,
    started_at timestamptz NOT NULL,
    cause text NOT NULL CHECK (cause IN ('edit', 'assistant', 'restore', 'accept')),
    actor_grant_id uuid REFERENCES oauth_grants (id) ON DELETE SET NULL,
    actor_name text,
    title text NOT NULL CHECK (char_length(title) <= 160),
    lng text CHECK (lng IS NULL OR char_length(lng) <= 35),
    schema_version integer NOT NULL CHECK (schema_version >= 1),
    personal_details jsonb NOT NULL CHECK (NOT (personal_details ? 'photo')),
    content jsonb NOT NULL,
    customization jsonb NOT NULL,
    byte_size integer NOT NULL CHECK (byte_size > 0),
    CONSTRAINT resume_checkpoints_actor_name_check CHECK ((cause = 'assistant') = (actor_name IS NOT NULL)),
    CONSTRAINT resume_checkpoints_actor_grant_check CHECK (actor_grant_id IS NULL OR cause = 'assistant')
);

CREATE INDEX resume_checkpoints_resume_started_idx ON resume_checkpoints (resume_id, started_at DESC);
CREATE INDEX resume_checkpoints_started_idx ON resume_checkpoints (started_at);
CREATE INDEX resume_checkpoints_actor_grant_idx ON resume_checkpoints (actor_grant_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON resume_checkpoints TO aboutme_app;

-- +goose Down
SET LOCAL lock_timeout = '5s';

DROP TABLE resume_checkpoints;

ALTER TABLE resumes
    DROP CONSTRAINT resumes_session_columns_check,
    DROP CONSTRAINT resumes_session_actor_check,
    DROP COLUMN session_last_write_at,
    DROP COLUMN session_started_at,
    DROP COLUMN session_actor;
