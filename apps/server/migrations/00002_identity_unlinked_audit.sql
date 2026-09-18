-- +goose Up
-- A provider unlink records one lifecycle audit event: its kind and time only,
-- with no media job. This changes constraints on an existing table and creates
-- no object, so aboutme_app's grant from the baseline still covers it.
ALTER TABLE lifecycle_audit_events
    DROP CONSTRAINT lifecycle_audit_events_kind_check,
    DROP CONSTRAINT lifecycle_audit_events_scope_check,
    ADD CONSTRAINT lifecycle_audit_events_kind_check CHECK (
        kind IN ('account_deleted', 'identity_unlinked', 'media_deletion_overdue', 'media_deletion_completed')
    ),
    ADD CONSTRAINT lifecycle_audit_events_scope_check CHECK (
        (kind IN ('account_deleted', 'identity_unlinked') AND media_job_id IS NULL)
        OR (kind IN ('media_deletion_overdue', 'media_deletion_completed')
            AND media_job_id IS NOT NULL)
    );

-- +goose Down
-- The restored constraints reject identity_unlinked rows, so they go first.
DELETE FROM lifecycle_audit_events WHERE kind = 'identity_unlinked';
ALTER TABLE lifecycle_audit_events
    DROP CONSTRAINT lifecycle_audit_events_kind_check,
    DROP CONSTRAINT lifecycle_audit_events_scope_check,
    ADD CONSTRAINT lifecycle_audit_events_kind_check CHECK (
        kind IN ('account_deleted', 'media_deletion_overdue', 'media_deletion_completed')
    ),
    ADD CONSTRAINT lifecycle_audit_events_scope_check CHECK (
        (kind = 'account_deleted' AND media_job_id IS NULL)
        OR (kind IN ('media_deletion_overdue', 'media_deletion_completed')
            AND media_job_id IS NOT NULL)
    );
