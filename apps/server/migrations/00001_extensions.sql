-- +goose Up
-- Migrations are the sole relational schema source. citext is explicit here so
-- goose installs it before later migrations use the type. See ADR 0010.
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
DROP EXTENSION IF EXISTS citext;
