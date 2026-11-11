-- +goose Up
SET LOCAL lock_timeout = '5s';

-- Removes schema state the current code no longer uses. Every earlier release
-- reads the showcase review columns, so the first release with this migration
-- is the rollback floor (ADR 0028). No grants change: no table is added.

-- The showcase has no review (ADR 0028). Migration 00011 cleared these columns
-- and kept them, with the approval index, only for the release before it.
DROP INDEX resume_showcase_listing_idx;
ALTER TABLE resume_showcase
    DROP CONSTRAINT resume_showcase_first_listed_check,
    DROP CONSTRAINT resume_showcase_review_columns_check,
    DROP CONSTRAINT resume_showcase_review_outcome_check,
    DROP CONSTRAINT resume_showcase_reviewed_key_check,
    DROP CONSTRAINT resume_showcase_review_key_check;
ALTER TABLE resume_showcase
    DROP COLUMN first_listed_at,
    DROP COLUMN reviewed_at,
    DROP COLUMN review_outcome,
    DROP COLUMN reviewed_key,
    DROP COLUMN review_key;

-- Both code inserts pass grant_id and auth_epoch, which are NOT NULL since
-- 00004, so the trigger that filled them for older code has nothing to do.
DROP TRIGGER oauth_authorization_codes_fill_second_factor_bindings ON oauth_authorization_codes;
DROP FUNCTION fill_oauth_authorization_code_second_factor_bindings();

-- 00015 added an index for each of these queries' exact filter and order, so
-- these older indexes only cost writes:
--   ClaimAuthEmailJobs uses auth_email_jobs_claim_order_idx;
--   CleanupFinishedAuthEmailJobs uses auth_email_jobs_finished_idx;
--   live-session lookups by user use sessions_user_revoked_idx;
--   the idle-client sweep uses oauth_grants_client_revoked_idx.
-- None is unique, so none is an ON CONFLICT target.
DROP INDEX auth_email_jobs_claim_idx;
DROP INDEX auth_email_jobs_outcome_idx;
DROP INDEX sessions_user_id_active_idx;
DROP INDEX oauth_grants_client_live_idx;

-- +goose Down
SET LOCAL lock_timeout = '5s';

CREATE INDEX oauth_grants_client_live_idx ON oauth_grants (client_id) WHERE (revoked_at IS NULL);
CREATE INDEX sessions_user_id_active_idx ON sessions (user_id) WHERE (revoked_at IS NULL);
CREATE INDEX auth_email_jobs_outcome_idx ON auth_email_jobs (sent_at, terminal_at) WHERE (state = ANY (ARRAY['sent'::text, 'terminal'::text]));
CREATE INDEX auth_email_jobs_claim_idx ON auth_email_jobs (next_attempt_at, created_at, id) WHERE (state = 'pending');

-- +goose StatementBegin
CREATE FUNCTION fill_oauth_authorization_code_second_factor_bindings() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.grant_id IS NULL THEN
        SELECT grant_row.id INTO NEW.grant_id
        FROM oauth_grants AS grant_row
        WHERE grant_row.user_id = NEW.user_id
          AND grant_row.client_id = NEW.client_id
          AND grant_row.revoked_at IS NULL;
    END IF;
    IF NEW.auth_epoch IS NULL THEN
        SELECT user_row.auth_epoch INTO NEW.auth_epoch FROM users AS user_row WHERE user_row.id = NEW.user_id;
    END IF;
    IF NEW.grant_id IS NULL OR NEW.auth_epoch IS NULL THEN
        RAISE EXCEPTION 'missing oauth authorization code second-factor binding';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER oauth_authorization_codes_fill_second_factor_bindings
BEFORE INSERT ON oauth_authorization_codes
FOR EACH ROW EXECUTE FUNCTION fill_oauth_authorization_code_second_factor_bindings();

-- The review columns return as 00011 left every row: the fixed review key and
-- no review result. That is all they held, so nothing is lost.
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
