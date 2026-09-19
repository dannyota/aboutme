-- +goose Up
-- The owner's public page title and emoji favicon are publication settings
-- beside slug, download, and discovery (ADR 0042). NULL keeps today's page.
-- The server validates graphemes and emoji; these checks only bound storage
-- and rule out the empty string, which the API stores as NULL. This adds
-- columns to an existing table, so aboutme_app's table grant covers them.
ALTER TABLE resumes
    ADD COLUMN public_title text NULL,
    ADD COLUMN favicon_emoji text NULL,
    ADD CONSTRAINT resumes_public_title_check CHECK (
        public_title IS NULL
        OR (char_length(public_title) BETWEEN 1 AND 560)
    ),
    ADD CONSTRAINT resumes_favicon_emoji_check CHECK (
        favicon_emoji IS NULL
        OR (char_length(favicon_emoji) BETWEEN 1 AND 16)
    );

-- +goose Down
ALTER TABLE resumes
    DROP CONSTRAINT resumes_favicon_emoji_check,
    DROP CONSTRAINT resumes_public_title_check,
    DROP COLUMN favicon_emoji,
    DROP COLUMN public_title;
