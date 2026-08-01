-- +goose Up
-- citext is hand-written here, not Atlas-generated: Atlas's Postgres schema
-- differ does not model `CREATE EXTENSION` at all (verified empirically —
-- `atlas schema inspect` returns an identical "public" schema for a bare
-- database and one with citext installed, so `atlas migrate diff` can never
-- produce this statement from sql/schema.sql). Extensions are therefore
-- migrated by hand (the usual approach for extensions Atlas cannot diff,
-- such as pgvector or citext). Everything Atlas CAN diff
-- (tables/columns/indexes/constraints) is generated from sql/schema.sql by
-- `make migrate-gen` into later-numbered files in this same directory.
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
DROP EXTENSION IF EXISTS citext;
