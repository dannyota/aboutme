-- +goose Up
-- One stored link-preview card per live resume (ADR 0055). The card is
-- derived data: it holds only the current card version and its PNG, and
-- every read passes the public live-state gate first (ADR 0022).
CREATE TABLE resume_preview_cards (
    resume_id uuid NOT NULL,
    version text NOT NULL,
    png bytea NOT NULL,
    rendered_at timestamptz NOT NULL,
    PRIMARY KEY (resume_id),
    CONSTRAINT resume_preview_cards_resume_id_fkey FOREIGN KEY (resume_id) REFERENCES resumes (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT resume_preview_cards_version_check CHECK (version ~ '^[0-9a-f]{16}$'),
    CONSTRAINT resume_preview_cards_png_check CHECK (octet_length(png) BETWEEN 1 AND 524288)
);

-- Unpublish and rename remove the card in the same transaction that changes
-- the public state, whichever code path runs the update. Delete removes it
-- by the cascade above.
-- +goose StatementBegin
CREATE FUNCTION revoke_resume_preview_card() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM resume_preview_cards WHERE resume_id = OLD.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER resumes_revoke_preview_card
AFTER UPDATE ON resumes
FOR EACH ROW
WHEN (NOT NEW.live OR NEW.slug IS DISTINCT FROM OLD.slug)
EXECUTE FUNCTION revoke_resume_preview_card();

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE resume_preview_cards TO aboutme_app;

-- +goose Down
DROP TRIGGER resumes_revoke_preview_card ON resumes;
DROP FUNCTION revoke_resume_preview_card();
DROP TABLE resume_preview_cards;
