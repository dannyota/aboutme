-- +goose Up
-- The community showcase has no review (docs/design/showcase.md "Data and
-- contract", ADR 0029). An opt-in is listed while the resume is live and sign
-- in to view is off. The review columns stay, unused, so the previous release
-- keeps running on this schema; a later migration drops them.
--
-- Owners whose opt-in was declined see the switch off and may turn it on again.
DELETE FROM resume_showcase WHERE review_outcome = 'declined';

-- No review result stays stored. Pending and approved opt-ins are then listed
-- in opt-in order.
UPDATE resume_showcase
SET reviewed_key = NULL,
    review_outcome = NULL,
    reviewed_at = NULL,
    first_listed_at = NULL;

-- Inserts omit review_key, which Go no longer computes.
ALTER TABLE resume_showcase
    ALTER COLUMN review_key SET DEFAULT '0000000000000000';

-- Serves the listing: newest opt-in first, then resume ID. The old partial
-- index on first_listed_at stays until the unused columns go.
CREATE INDEX resume_showcase_requested_idx
    ON resume_showcase (requested_at DESC, resume_id DESC);

-- +goose Down
-- Cannot restore deleted rows or cleared review results.
DROP INDEX resume_showcase_requested_idx;

ALTER TABLE resume_showcase
    ALTER COLUMN review_key DROP DEFAULT;
