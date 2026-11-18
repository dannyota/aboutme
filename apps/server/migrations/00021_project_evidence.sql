-- +goose Up
SET LOCAL lock_timeout = '5s';

ALTER TABLE github_connections
    ADD COLUMN binding bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT github_connections_binding_check CHECK (binding > 0);

-- +goose StatementBegin
CREATE FUNCTION github_connections_binding_never_shrinks() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.binding < OLD.binding THEN
        RAISE EXCEPTION 'github connection binding must not shrink'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'github_connections_binding_never_shrinks';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER github_connections_binding_never_shrinks
BEFORE UPDATE ON github_connections
FOR EACH ROW EXECUTE FUNCTION github_connections_binding_never_shrinks();

CREATE TABLE project_evidence (
    id uuid NOT NULL DEFAULT uuidv7(),
    project_id uuid NOT NULL,
    position integer NOT NULL,
    kind text NOT NULL,
    repository_id bigint NOT NULL,
    item_id bigint NOT NULL,
    pull_number bigint NULL,
    release_tag text NULL,
    url text NOT NULL,
    repository_name text NOT NULL,
    title text NOT NULL,
    item_date timestamptz NOT NULL,
    checked_at timestamptz NOT NULL,
    checked_binding bigint NOT NULL,
    rule_met boolean NOT NULL,
    flag_reason text NULL,
    PRIMARY KEY (id),
    CONSTRAINT project_evidence_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT project_evidence_item_key UNIQUE (project_id, kind, item_id),
    CONSTRAINT project_evidence_position_key UNIQUE (project_id, position) DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT project_evidence_position_check CHECK (position >= 0),
    CONSTRAINT project_evidence_kind_check CHECK (kind = ANY (ARRAY['repository'::text, 'pullRequest'::text, 'release'::text])),
    CONSTRAINT project_evidence_ids_check CHECK (repository_id > 0 AND item_id > 0),
    CONSTRAINT project_evidence_kind_fields_check CHECK (
        (kind = 'repository' AND item_id = repository_id AND pull_number IS NULL AND release_tag IS NULL)
        OR (kind = 'pullRequest' AND pull_number IS NOT NULL AND pull_number > 0 AND release_tag IS NULL)
        OR (kind = 'release' AND release_tag IS NOT NULL AND pull_number IS NULL)
    ),
    CONSTRAINT project_evidence_release_tag_check CHECK (char_length(release_tag) BETWEEN 1 AND 255),
    CONSTRAINT project_evidence_url_check CHECK (
        char_length(url) <= 300
        AND starts_with(url, 'https://github.com/')
        AND (
            (kind = 'repository' AND url ~ '^https://github\.com/[^/?#[:space:]]+/[^/?#[:space:]]+$')
            OR (kind = 'pullRequest' AND url ~ '^https://github\.com/[^/?#[:space:]]+/[^/?#[:space:]]+/pull/[1-9][0-9]*$')
            OR (kind = 'release' AND url ~ '^https://github\.com/[^/?#[:space:]]+/[^/?#[:space:]]+/releases/tag/[^/?#[:space:]]+$')
        )
    ),
    CONSTRAINT project_evidence_repository_name_check CHECK (char_length(repository_name) BETWEEN 1 AND 200),
    CONSTRAINT project_evidence_title_check CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT project_evidence_checked_binding_check CHECK (checked_binding > 0),
    CONSTRAINT project_evidence_flag_reason_check CHECK (flag_reason = ANY (ARRAY['rule_failed'::text, 'gone'::text])),
    CONSTRAINT project_evidence_rule_flag_check CHECK (rule_met = (flag_reason IS NULL))
);

GRANT SELECT, INSERT, UPDATE, DELETE ON project_evidence TO aboutme_app;

ALTER TABLE oauth_transactions
    ADD COLUMN evidence_project_id uuid NULL,
    ADD CONSTRAINT oauth_transactions_evidence_project_check CHECK (evidence_project_id IS NULL OR purpose = 'evidence'::text);

-- +goose Down
SET LOCAL lock_timeout = '5s';

ALTER TABLE oauth_transactions
    DROP CONSTRAINT oauth_transactions_evidence_project_check,
    DROP COLUMN evidence_project_id;

DROP TABLE project_evidence;

DROP TRIGGER github_connections_binding_never_shrinks ON github_connections;
DROP FUNCTION github_connections_binding_never_shrinks();

ALTER TABLE github_connections
    DROP CONSTRAINT github_connections_binding_check,
    DROP COLUMN binding;
