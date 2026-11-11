-- +goose Up
CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug text COLLATE "C" NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    document jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_owner_slug_key UNIQUE (user_id, slug),
    CONSTRAINT projects_slug_check CHECK (
        length(slug) BETWEEN 2 AND 40
        AND slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
    ),
    CONSTRAINT projects_document_check CHECK (
        jsonb_typeof(document) = 'object'
        AND octet_length(document::text) <= 98304
    )
);
CREATE INDEX projects_owner_created_idx ON projects (user_id, created_at, id);

CREATE TABLE project_slug_holds (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug text COLLATE "C" NOT NULL CHECK (
        length(slug) BETWEEN 2 AND 40
        AND slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
    ),
    released_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, slug)
);
CREATE INDEX project_slug_holds_released_idx
    ON project_slug_holds (released_at, user_id, slug);

GRANT SELECT, INSERT, UPDATE, DELETE ON projects, project_slug_holds TO aboutme_app;

-- +goose Down
DROP TABLE project_slug_holds;
DROP TABLE projects;
