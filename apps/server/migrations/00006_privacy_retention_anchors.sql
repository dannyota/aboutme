-- +goose Up
ALTER TABLE slug_tombstones DROP COLUMN released_by_user_id;
CREATE INDEX slug_tombstones_released_at_idx ON slug_tombstones (released_at, id);

DROP INDEX sessions_metadata_age_idx;
CREATE INDEX sessions_metadata_expiry_idx ON sessions (absolute_expires_at, id) WHERE ua IS NOT NULL OR ip IS NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE sessions, slug_tombstones TO aboutme_app;

-- +goose Down
ALTER TABLE slug_tombstones ADD COLUMN released_by_user_id uuid NULL;
ALTER TABLE slug_tombstones ADD CONSTRAINT slug_tombstones_released_by_user_id_fkey
    FOREIGN KEY (released_by_user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE SET NULL;
DROP INDEX slug_tombstones_released_at_idx;

DROP INDEX sessions_metadata_expiry_idx;
CREATE INDEX sessions_metadata_age_idx ON sessions (created_at, id) WHERE ua IS NOT NULL OR ip IS NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE sessions, slug_tombstones TO aboutme_app;
