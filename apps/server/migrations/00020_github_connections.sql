-- +goose Up
SET LOCAL lock_timeout = '5s';

CREATE TABLE github_connections (
    user_id uuid NOT NULL,
    github_user_id bigint NULL,
    login text NULL,
    connected_at timestamptz NULL,
    generation bigint NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT github_connections_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT github_connections_generation_check CHECK (generation > 0),
    CONSTRAINT github_connections_github_user_id_check CHECK (github_user_id > 0),
    CONSTRAINT github_connections_login_check CHECK (char_length(login) BETWEEN 1 AND 100),
    CONSTRAINT github_connections_fields_check CHECK (
        (github_user_id IS NULL AND login IS NULL AND connected_at IS NULL)
        OR (github_user_id IS NOT NULL AND login IS NOT NULL AND connected_at IS NOT NULL)
    )
);

-- +goose StatementBegin
CREATE FUNCTION github_connections_generation_grows() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.generation <= OLD.generation THEN
        RAISE EXCEPTION 'github connection generation must grow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'github_connections_generation_grows';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER github_connections_generation_grows
BEFORE UPDATE ON github_connections
FOR EACH ROW EXECUTE FUNCTION github_connections_generation_grows();

GRANT SELECT, INSERT, UPDATE, DELETE ON github_connections TO aboutme_app;

ALTER TABLE oauth_transactions
    ADD COLUMN evidence_generation bigint NULL;

ALTER TABLE oauth_transactions
    DROP CONSTRAINT oauth_transactions_purpose_check,
    DROP CONSTRAINT oauth_transactions_link_needs_user;

ALTER TABLE oauth_transactions
    ADD CONSTRAINT oauth_transactions_purpose_check CHECK (purpose = ANY (ARRAY['login'::text, 'link'::text, 'reauth'::text, 'view'::text, 'evidence'::text])),
    ADD CONSTRAINT oauth_transactions_link_needs_user CHECK (
        ((purpose = ANY (ARRAY['link'::text, 'reauth'::text, 'evidence'::text])) AND (linking_user_id IS NOT NULL))
        OR ((purpose = ANY (ARRAY['login'::text, 'view'::text])) AND (linking_user_id IS NULL))
    ),
    ADD CONSTRAINT oauth_transactions_evidence_check CHECK (
        ((purpose = 'evidence'::text) AND (provider = 'github'::text) AND (evidence_generation IS NOT NULL) AND (evidence_generation >= 0))
        OR ((purpose <> 'evidence'::text) AND (evidence_generation IS NULL))
    );

-- +goose Down
SET LOCAL lock_timeout = '5s';

DELETE FROM oauth_transactions WHERE purpose = 'evidence';

ALTER TABLE oauth_transactions
    DROP CONSTRAINT oauth_transactions_evidence_check,
    DROP CONSTRAINT oauth_transactions_link_needs_user,
    DROP CONSTRAINT oauth_transactions_purpose_check;

ALTER TABLE oauth_transactions
    ADD CONSTRAINT oauth_transactions_purpose_check CHECK (purpose = ANY (ARRAY['login'::text, 'link'::text, 'reauth'::text, 'view'::text])),
    ADD CONSTRAINT oauth_transactions_link_needs_user CHECK (
        ((purpose = ANY (ARRAY['link'::text, 'reauth'::text])) AND (linking_user_id IS NOT NULL))
        OR ((purpose = ANY (ARRAY['login'::text, 'view'::text])) AND (linking_user_id IS NULL))
    );

ALTER TABLE oauth_transactions
    DROP COLUMN evidence_generation;

DROP TRIGGER github_connections_generation_grows ON github_connections;
DROP FUNCTION github_connections_generation_grows();
DROP TABLE github_connections;
