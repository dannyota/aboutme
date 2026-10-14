-- +goose Up
ALTER TABLE oauth_clients
    ADD COLUMN metadata_url text COLLATE "C",
    ADD CONSTRAINT oauth_clients_metadata_url_length_check
        CHECK (metadata_url IS NULL OR octet_length(metadata_url) BETWEEN 9 AND 256);

CREATE UNIQUE INDEX oauth_clients_metadata_url_idx
    ON oauth_clients (metadata_url COLLATE "C") WHERE metadata_url IS NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_clients TO aboutme_app;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    LOCK TABLE oauth_clients IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM oauth_clients WHERE metadata_url IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove client metadata identity while URL clients exist';
    END IF;
END
$$;
-- +goose StatementEnd
DROP INDEX oauth_clients_metadata_url_idx;
ALTER TABLE oauth_clients DROP CONSTRAINT oauth_clients_metadata_url_length_check,
    DROP COLUMN metadata_url;
