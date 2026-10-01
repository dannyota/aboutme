-- +goose Up
-- Community showcase (docs/design/showcase.md "Data and contract", ADR 0029).
-- One row per opted-in resume. The row is a publication setting, not
-- document content: the owner's role, the request time, the operator's review
-- result, and three values Go keeps current inside every resume write
-- transaction (review key, card version, template ID). Ending the opt-in
-- deletes the row; a resume or account delete removes it by the cascade.
--
-- The showcase page is a Nuxt root, so no resume may hold its slug. The
-- operator settles that with the owner before the deploy retries.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM resumes WHERE slug = 'showcase') THEN
        RAISE EXCEPTION 'resume_showcase migration refused: a resume holds the slug showcase';
    END IF;
END;
$$;
-- +goose StatementEnd

CREATE TABLE resume_showcase (
    resume_id uuid NOT NULL,
    requested_at timestamptz NOT NULL,
    role text NULL,
    review_key text NOT NULL,
    card_version text NOT NULL,
    template_id text NULL,
    reviewed_key text NULL,
    review_outcome text NULL,
    reviewed_at timestamptz NULL,
    first_listed_at timestamptz NULL,
    PRIMARY KEY (resume_id),
    CONSTRAINT resume_showcase_resume_id_fkey FOREIGN KEY (resume_id) REFERENCES resumes (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT resume_showcase_role_check CHECK (
        (role IS NULL) OR (role = ANY (ARRAY[
            'backend'::text, 'frontend'::text, 'mobile'::text, 'devops'::text,
            'data-ai'::text, 'qa'::text, 'fresher'::text, 'brse'::text,
            'security'::text, 'other'::text
        ]))
    ),
    CONSTRAINT resume_showcase_review_key_check CHECK (review_key ~ '^[0-9a-f]{16}$'),
    CONSTRAINT resume_showcase_card_version_check CHECK (card_version ~ '^[0-9a-f]{16}$'),
    CONSTRAINT resume_showcase_template_id_check CHECK (
        (template_id IS NULL)
        OR ((char_length(template_id) <= 64) AND (template_id ~ '^[a-z0-9]+(-[a-z0-9]+)*$'))
    ),
    CONSTRAINT resume_showcase_reviewed_key_check CHECK ((reviewed_key IS NULL) OR (reviewed_key ~ '^[0-9a-f]{16}$')),
    CONSTRAINT resume_showcase_review_outcome_check CHECK (
        (review_outcome IS NULL) OR (review_outcome = ANY (ARRAY['approved'::text, 'declined'::text]))
    ),
    CONSTRAINT resume_showcase_review_columns_check CHECK (
        ((reviewed_key IS NULL) = (review_outcome IS NULL))
        AND ((reviewed_at IS NULL) = (review_outcome IS NULL))
    ),
    CONSTRAINT resume_showcase_first_listed_check CHECK (
        ((first_listed_at IS NULL) OR (review_outcome IS NOT NULL))
        AND ((review_outcome IS DISTINCT FROM 'approved'::text) OR (first_listed_at IS NOT NULL))
    )
);

-- Serves the listing: newest first approval, then resume ID.
CREATE INDEX resume_showcase_listing_idx
    ON resume_showcase (first_listed_at DESC, resume_id DESC)
    WHERE review_outcome = 'approved';

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE resume_showcase TO aboutme_app;

-- +goose Down
DROP TABLE resume_showcase;
