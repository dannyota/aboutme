-- +goose Up
-- Bounded idempotency retention (ADR 0016) and the durable media
-- deletion-job ledger (ADR 0019, P2B D13). Everything below runs in this
-- migration's single transaction, so the purge, the backfill, and the
-- constraint enablement all observe one transaction timestamp.

-- Approved deterministic response headers (Location, ETag,
-- X-Resume-Schema-Version) stored beside the body. Existing records get
-- the empty object; the object-type check is a separate named constraint
-- so a violation is identifiable by name.
ALTER TABLE idempotency_records
    ADD COLUMN response_headers jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE idempotency_records
    ADD CONSTRAINT idempotency_records_response_headers_object_check
        CHECK (jsonb_typeof(response_headers) = 'object');

-- Deterministic oldest-first request-path cleanup: at most 200 of the
-- calling user's expired rows per mutation, ordered by (expires_at, id).
CREATE INDEX idempotency_records_user_expires_id_idx
    ON idempotency_records (user_id, expires_at, id);

-- Purge every already-expired legacy record BEFORE backfilling usage, so
-- the backfilled counters cover exactly the physically retained rows and a
-- later cleanup that releases exact per-row counters can never underflow.
DELETE FROM idempotency_records WHERE expires_at <= now();

-- Per-user usage counters: every physically retained record (including
-- expired backlog not yet deleted) and its stored body + approved-header
-- bytes. Maintained transactionally under the existing users-row lock; the
-- 50,000-record / 1 GiB admission check reads only this row.
CREATE TABLE idempotency_usage (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    retained_records bigint NOT NULL,
    stored_bytes bigint NOT NULL
);

-- Backfill one row per user that still retains records, using the single
-- canonical byte expression shared by insert returns, cleanup returns, and
-- quota accounting: octet_length(response_body::text) +
-- octet_length(response_headers::text). A JSON null body counts as 4 bytes
-- and the empty headers object as 2.
INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
SELECT user_id,
       count(*),
       COALESCE(sum(octet_length(response_body::text) +
                    octet_length(response_headers::text)), 0)
FROM idempotency_records
GROUP BY user_id;

-- Non-negative checks are enabled only after the backfill above.
ALTER TABLE idempotency_usage
    ADD CONSTRAINT idempotency_usage_retained_records_nonnegative_check
        CHECK (retained_records >= 0);
ALTER TABLE idempotency_usage
    ADD CONSTRAINT idempotency_usage_stored_bytes_nonnegative_check
        CHECK (stored_bytes >= 0);

-- Durable media deletion-job ledger: cleanup WORK, never media ownership
-- (the document remains the sole ownership record). Reference removal
-- enqueues (resume_id, object_key) in the same transaction; P8-priv owns
-- claim leases, retries, terminal audit records, and removal of completed
-- jobs. Deliberately NO foreign key to resumes: resume and account
-- deletion must not cascade away pending physical deletion.
CREATE TABLE media_deletion_jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    resume_id uuid NOT NULL,
    object_key text NOT NULL,
    enqueued_at timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    attempt_count integer NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    CONSTRAINT media_deletion_jobs_object_key_key UNIQUE (object_key),
    CONSTRAINT media_deletion_jobs_attempt_count_nonnegative_check
        CHECK (attempt_count >= 0),
    -- D11's exact server-derived key grammar, with the embedded canonical
    -- resume UUID required to equal resume_id (resume_id::text is the
    -- canonical lowercase hyphenated form). A malformed or cross-resume
    -- key can never enter the ledger.
    CONSTRAINT media_deletion_jobs_key_matches_resume_check
        CHECK (object_key ~ ('^resumes/' || resume_id::text || '/photo-[0-9a-f]{32}\.(jpg|png)$'))
);

-- Bounded oldest-due claims for the P8-priv drain worker.
CREATE INDEX media_deletion_jobs_next_attempt_idx
    ON media_deletion_jobs (next_attempt_at, id);

-- +goose Down
-- Inert cargo per the append-only rule (rollback is a forward migration);
-- kept accurate so sqlc and local tooling can replay Down cleanly.
DROP INDEX media_deletion_jobs_next_attempt_idx;
DROP TABLE media_deletion_jobs;
DROP TABLE idempotency_usage;
DROP INDEX idempotency_records_user_expires_id_idx;
ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_response_headers_object_check;
ALTER TABLE idempotency_records
    DROP COLUMN response_headers;
