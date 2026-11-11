-- +goose Up
SET LOCAL lock_timeout = '5s';

-- The privacy sweep deletes session rows seven days after absolute expiry and
-- pages through them in (absolute_expires_at, id) order. The existing expiry
-- index covers only rows that still hold an IP or user agent. Index only, so it
-- needs no grants. Plain CREATE INDEX, because the table is small and
-- CONCURRENTLY cannot run inside the migration transaction.
CREATE INDEX sessions_absolute_expiry_idx ON sessions (absolute_expires_at, id);

-- +goose Down
SET LOCAL lock_timeout = '5s';

DROP INDEX sessions_absolute_expiry_idx;
