-- +goose Up
SET LOCAL lock_timeout = '5s';

-- Indexes only: no behavior changes, so the previous release runs unchanged on
-- this schema, and indexes need no grants. Plain CREATE INDEX, because the
-- tables are small and CONCURRENTLY cannot run inside the migration transaction.

-- Foreign keys whose cascades and lookups would otherwise scan the referencing
-- table. A partial index on "column IS NOT NULL" still reaches every row a
-- cascade matches.
--
-- Account deletion must reach revoked sessions; live lookups add
-- revoked_at IS NULL.
CREATE INDEX sessions_user_revoked_idx ON sessions (user_id, revoked_at);
CREATE INDEX oauth_transactions_linking_user_id_idx
    ON oauth_transactions (linking_user_id) WHERE linking_user_id IS NOT NULL;
CREATE INDEX oauth_transactions_resume_id_idx
    ON oauth_transactions (resume_id) WHERE resume_id IS NOT NULL;
CREATE INDEX auth_email_jobs_registration_id_idx
    ON auth_email_jobs (registration_id) WHERE registration_id IS NOT NULL;
CREATE INDEX auth_email_jobs_reset_token_id_idx
    ON auth_email_jobs (reset_token_id) WHERE reset_token_id IS NOT NULL;
CREATE INDEX auth_email_jobs_user_id_idx
    ON auth_email_jobs (user_id) WHERE user_id IS NOT NULL;
-- Revoked grants keep their rows, so cascades must reach them; the idle-client
-- sweep adds revoked_at IS NULL.
CREATE INDEX oauth_grants_user_id_idx ON oauth_grants (user_id);
CREATE INDEX oauth_grants_client_revoked_idx ON oauth_grants (client_id, revoked_at);
CREATE INDEX oauth_tokens_client_id_idx ON oauth_tokens (client_id);
-- Also serves the oldest-live lookup, which orders by created_at, id.
CREATE INDEX pending_authentications_user_created_id_idx
    ON pending_authentications (user_id, created_at, id);
CREATE INDEX webauthn_ceremonies_user_id_idx ON webauthn_ceremonies (user_id);
CREATE INDEX authentication_security_events_user_id_idx
    ON authentication_security_events (user_id);

-- Mail worker. Each index matches its query's filter and ORDER BY exactly.
-- ClaimAuthEmailJobs: other mail before privacy notices, then due time.
CREATE INDEX auth_email_jobs_claim_order_idx
    ON auth_email_jobs ((kind = 'privacy_notice'), next_attempt_at, created_at, id)
    WHERE state = 'pending';
-- RequeueExpiredAuthEmailLeases.
CREATE INDEX auth_email_jobs_lease_expiry_idx
    ON auth_email_jobs (lease_expires_at, id) WHERE state = 'leased';
-- CleanupFinishedAuthEmailJobs.
CREATE INDEX auth_email_jobs_finished_idx
    ON auth_email_jobs ((COALESCE(sent_at, terminal_at)), id)
    WHERE state IN ('sent', 'terminal');

-- +goose Down
SET LOCAL lock_timeout = '5s';

DROP INDEX auth_email_jobs_finished_idx;
DROP INDEX auth_email_jobs_lease_expiry_idx;
DROP INDEX auth_email_jobs_claim_order_idx;

DROP INDEX authentication_security_events_user_id_idx;
DROP INDEX webauthn_ceremonies_user_id_idx;
DROP INDEX pending_authentications_user_created_id_idx;
DROP INDEX oauth_tokens_client_id_idx;
DROP INDEX oauth_grants_client_revoked_idx;
DROP INDEX oauth_grants_user_id_idx;
DROP INDEX auth_email_jobs_user_id_idx;
DROP INDEX auth_email_jobs_reset_token_id_idx;
DROP INDEX auth_email_jobs_registration_id_idx;
DROP INDEX oauth_transactions_resume_id_idx;
DROP INDEX oauth_transactions_linking_user_id_idx;
DROP INDEX sessions_user_revoked_idx;
