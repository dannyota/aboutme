-- +goose Up
SET LOCAL lock_timeout = '5s';

-- Drops the unused showcase review columns, their checks, and the approval
-- index (docs/design/showcase.md "Data and contract"). Releases from v0.9.2 name
-- only the kept columns; every earlier release selects all ten through sqlc's
-- star expansion and fails on this schema. schema-floor names v0.9.2, so the
-- deploy raises the release fence to it before this migration runs
-- (docs/design/release-floors.md "Schema floor"). No grants change: the grant
-- is on the table.
DROP INDEX resume_showcase_listing_idx;

ALTER TABLE resume_showcase
    DROP CONSTRAINT resume_showcase_review_key_check,
    DROP CONSTRAINT resume_showcase_reviewed_key_check,
    DROP CONSTRAINT resume_showcase_review_outcome_check,
    DROP CONSTRAINT resume_showcase_review_columns_check,
    DROP CONSTRAINT resume_showcase_first_listed_check,
    DROP COLUMN review_key,
    DROP COLUMN reviewed_key,
    DROP COLUMN review_outcome,
    DROP COLUMN reviewed_at,
    DROP COLUMN first_listed_at;

-- +goose Down
SET LOCAL lock_timeout = '5s';

-- Restores the version-17 columns, checks, and index. Existing rows take the
-- defaults: no review key and no review result, which every check accepts.
ALTER TABLE resume_showcase
    ADD COLUMN review_key text NOT NULL DEFAULT '0000000000000000',
    ADD COLUMN reviewed_key text NULL,
    ADD COLUMN review_outcome text NULL,
    ADD COLUMN reviewed_at timestamptz NULL,
    ADD COLUMN first_listed_at timestamptz NULL;

ALTER TABLE resume_showcase
    ADD CONSTRAINT resume_showcase_review_key_check CHECK (review_key ~ '^[0-9a-f]{16}$'),
    ADD CONSTRAINT resume_showcase_reviewed_key_check CHECK ((reviewed_key IS NULL) OR (reviewed_key ~ '^[0-9a-f]{16}$')),
    ADD CONSTRAINT resume_showcase_review_outcome_check CHECK (
        (review_outcome IS NULL) OR (review_outcome = ANY (ARRAY['approved'::text, 'declined'::text]))
    ),
    ADD CONSTRAINT resume_showcase_review_columns_check CHECK (
        ((reviewed_key IS NULL) = (review_outcome IS NULL))
        AND ((reviewed_at IS NULL) = (review_outcome IS NULL))
    ),
    ADD CONSTRAINT resume_showcase_first_listed_check CHECK (
        ((first_listed_at IS NULL) OR (review_outcome IS NOT NULL))
        AND ((review_outcome IS DISTINCT FROM 'approved'::text) OR (first_listed_at IS NOT NULL))
    );

CREATE INDEX resume_showcase_listing_idx
    ON resume_showcase (first_listed_at DESC, resume_id DESC)
    WHERE review_outcome = 'approved';
