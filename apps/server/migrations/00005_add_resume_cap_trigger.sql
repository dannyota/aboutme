-- +goose Up
-- enforce_resume_cap and resumes_enforce_cap are hand-written here, not
-- Atlas-generated: Atlas's Postgres schema differ silently drops
-- functions, triggers, procedures, views, sequences, rules, and policies
-- from a generated migration -- verified empirically against the pinned
-- Atlas community v1.2.0 (see cmd/migrate/gen/main.go's package doc
-- comment and checkUndiffableObjects). `make migrate-gen`/`make
-- data-drift` cross-check this file's statements against sql/schema.sql's
-- own CREATE FUNCTION/CREATE TRIGGER declarations byte-for-byte (after
-- comment/whitespace normalization) instead of a blanket reject, so the
-- two can never silently drift apart the way an un-cross-checked
-- hand-written migration could. Everything Atlas CAN diff
-- (tables/columns/indexes/constraints, including 00004's resumes,
-- slug_tombstones, and idempotency_records tables this trigger attaches
-- to) is generated from sql/schema.sql by `make migrate-gen` as usual.
--
-- +goose StatementBegin
CREATE FUNCTION enforce_resume_cap() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Serialize per-owner (D7). The lock blocks a competing writer; the
    -- count that follows then takes a FRESH snapshot and sees the row
    -- that writer committed. This holds even for writers that bypass the
    -- store layer -- but only under READ COMMITTED, which is Postgres's
    -- default and which every aboutme transaction must keep. At
    -- REPEATABLE READ the count would read a snapshot taken before the
    -- lock was granted, still see 2 rows, and admit a 4th resume.
    -- The store's create tx takes this same lock first (spec: belt and
    -- suspenders); identical order, no deadlock.
    PERFORM 1 FROM users WHERE id = NEW.user_id FOR UPDATE;
    IF (SELECT count(*) FROM resumes WHERE user_id = NEW.user_id) >= 3 THEN
        RAISE EXCEPTION 'resumes_user_cap_exceeded'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER resumes_enforce_cap
BEFORE INSERT OR UPDATE OF user_id ON resumes
FOR EACH ROW EXECUTE FUNCTION enforce_resume_cap();

-- +goose Down
DROP TRIGGER resumes_enforce_cap ON resumes;
DROP FUNCTION enforce_resume_cap();
