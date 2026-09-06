-- +goose Up
ALTER TABLE media_deletion_jobs
    ADD COLUMN lease_id uuid,
    ADD COLUMN lease_expires_at timestamptz,
    ADD COLUMN completed_at timestamptz,
    ADD COLUMN overdue_at timestamptz,
    ADD COLUMN outcome text,
    ADD CONSTRAINT media_deletion_jobs_lease_check CHECK (
        (lease_id IS NULL AND lease_expires_at IS NULL)
        OR (lease_id IS NOT NULL AND lease_expires_at IS NOT NULL
            AND completed_at IS NULL AND lease_expires_at >= enqueued_at)
    ),
    ADD CONSTRAINT media_deletion_jobs_completion_check CHECK (
        (completed_at IS NULL AND outcome IS NULL)
        OR (completed_at IS NOT NULL AND completed_at >= enqueued_at
            AND outcome IS NOT NULL AND outcome IN ('deleted', 'absent'))
    ),
    ADD CONSTRAINT media_deletion_jobs_overdue_check CHECK (
        overdue_at IS NULL OR overdue_at >= enqueued_at + interval '24 hours'
    );

DROP INDEX media_deletion_jobs_next_attempt_idx;
CREATE INDEX media_deletion_jobs_next_attempt_idx
    ON media_deletion_jobs (next_attempt_at, id) WHERE completed_at IS NULL;
CREATE INDEX media_deletion_jobs_pending_age_idx
    ON media_deletion_jobs (enqueued_at, id) WHERE completed_at IS NULL;
CREATE INDEX media_deletion_jobs_completed_idx
    ON media_deletion_jobs (completed_at, id) WHERE completed_at IS NOT NULL;

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
CREATE INDEX lifecycle_audit_events_expiry_idx
    ON lifecycle_audit_events (occurred_at, id);

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

CREATE INDEX sessions_metadata_age_idx ON sessions (created_at, id)
    WHERE ua IS NOT NULL OR ip IS NOT NULL;

-- +goose Down
DROP INDEX sessions_metadata_age_idx;
DROP TABLE privacy_sweep_state;
DROP TABLE lifecycle_audit_events;
DROP INDEX media_deletion_jobs_completed_idx;
DROP INDEX media_deletion_jobs_pending_age_idx;
DROP INDEX media_deletion_jobs_next_attempt_idx;
CREATE INDEX media_deletion_jobs_next_attempt_idx
    ON media_deletion_jobs (next_attempt_at, id);
ALTER TABLE media_deletion_jobs
    DROP CONSTRAINT media_deletion_jobs_overdue_check,
    DROP CONSTRAINT media_deletion_jobs_completion_check,
    DROP CONSTRAINT media_deletion_jobs_lease_check,
    DROP COLUMN outcome,
    DROP COLUMN overdue_at,
    DROP COLUMN completed_at,
    DROP COLUMN lease_expires_at,
    DROP COLUMN lease_id;
