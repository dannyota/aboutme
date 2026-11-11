-- +goose Up
SET LOCAL lock_timeout = '5s';

-- Removes schema state no release at or above the production floor uses, so
-- the previous release runs unchanged on this schema. No grants change: no
-- table is added. The unused showcase review columns stay until the deploy can
-- raise the release floor before a migration that drops them.

-- Every code insert since v0.4.2 passes grant_id and auth_epoch, which are NOT
-- NULL since 00004, so the trigger that filled them for older code has nothing
-- to do.
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
