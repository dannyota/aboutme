-- +goose Up
SET LOCAL lock_timeout = '5s';

-- The community showcase has no review (docs/design/showcase.md "Data and
-- contract", ADR 0029). An opt-in is listed while the resume is live and sign
-- in to view is off. The review columns stay, unused, so the previous release
-- keeps running on this schema; a later migration drops them.
--
-- Owners whose opt-in was declined see the switch off and may turn it on again.
DELETE FROM resume_showcase WHERE review_outcome = 'declined';

-- No review result or review hash stays stored. The previous release rewrites
-- the key at its own start, so a rollback is unaffected. Pending and approved opt-ins are then listed
-- in opt-in order.
UPDATE resume_showcase
SET reviewed_key = NULL,
    review_outcome = NULL,
    reviewed_at = NULL,
    first_listed_at = NULL,
    review_key = '0000000000000000';

-- Inserts omit review_key, which Go no longer computes.
ALTER TABLE resume_showcase
    ALTER COLUMN review_key SET DEFAULT '0000000000000000';

-- Serves the listing: newest opt-in first, then resume ID. The old partial
-- index on first_listed_at stays until the unused columns go.
CREATE INDEX resume_showcase_requested_idx
    ON resume_showcase (requested_at DESC, resume_id DESC);

-- +goose Down
SET LOCAL lock_timeout = '5s';

-- Cannot restore deleted rows or cleared review results.
DROP INDEX resume_showcase_requested_idx;

ALTER TABLE resume_showcase
    ALTER COLUMN review_key DROP DEFAULT;
