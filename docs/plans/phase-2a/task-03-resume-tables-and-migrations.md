# Task 3: `resumes`, `slug_tombstones`, `idempotency_records` DDL + 3-resume trigger + migrations 00004/00005

Structural prerequisite for AC-DOC-001 (the DB-enforced half lands here). All
P2A DDL lands in this one task so the schema head changes **once**.

**Files:** modify `apps/server/sql/schema.sql`; create (generated)
`apps/server/migrations/00004_add_resume_tables.sql`; create (hand-written)
`apps/server/migrations/00005_add_resume_cap_trigger.sql`; regenerate
`apps/server/migrations/atlas.sum` via the pinned Atlas; create
`apps/server/migrations/resume_schema_test.go`; commit the regenerated
`apps/server/internal/store/models.go` (see owner correction 1 below).

> **Owner correction 1 (2026-08-03) — `internal/store/models.go` is in this
> task's scope.** This plan's Step 3 said `make sqlc-check` "must stay clean —
> no query changes yet". That was wrong: sqlc derives `models.go` from
> `schema.sql`, so **adding a table changes the generated models whether or not
> any query references it**. Confirmed at execution — `make data-drift` fails
> with `internal/store is out of date with sql/*.sql` and a modified `models.go`
> adding `Resume`, `SlugTombstone`, and `IdempotencyRecord`. The repo rule is
> that generated artifacts are committed alongside the source change that causes
> them, so the regeneration belongs to **this** task, not Task 4. Task 4 still
> owns `sql/queries.sql` and the query-derived generated files. `internal/store`
> remains a serialized artifact; this task and Task 4 are its only P2A writers,
> in that order.
>
> **Owner correction 2 (2026-08-03) — `BETWEEN` is replaced by explicit
> `>=`/`<=` in both slug format checks (applied to the DDL above).** As
> originally ratified, both checks used `char_length(slug) BETWEEN 4 AND 30`.
> That makes `make data-drift` fail forever. Atlas's own parse of `schema.sql`
> expands `BETWEEN` into a nested expression tree — `((A AND B) AND C)` — while
> Postgres flattens the executed constraint to `(A AND B AND C)`; the differ
> compares the two textually and never converges. Proven at execution by
> generating the migration Atlas wanted: its `Up` and `Down` differ **only** in
> parenthesis nesting, with no semantic change. The explicit form is
> byte-for-byte what Postgres reports, so the diff converges. `BETWEEN` is
> inclusive, so the constraint's meaning is identical — 4 and 30 remain
> accepted, 3 and 31 remain rejected, and Task 3's boundary matrix is unchanged.
> Recorded rather than silently patched because this edits ratified DDL text.

**DDL appended to `sql/schema.sql`** (decisions D5/D7/D11/D15; constraint names
≤ 63 bytes):

```sql
CREATE TABLE resumes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title text NOT NULL,
    slug text,
    live boolean NOT NULL DEFAULT false,
    download_enabled boolean NOT NULL DEFAULT true,
    seo_geo_enabled boolean NOT NULL DEFAULT false,
    schema_version integer NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    lng text,
    personal_details jsonb NOT NULL,
    content jsonb NOT NULL,
    customization jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT resumes_slug_key UNIQUE (slug),
    CONSTRAINT resumes_slug_format_check CHECK (
        slug IS NULL
        OR (char_length(slug) >= 4
            AND char_length(slug) <= 30
            AND slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')
    ),
    CONSTRAINT resumes_live_requires_slug_check CHECK (NOT live OR slug IS NOT NULL),
    CONSTRAINT resumes_seo_requires_live_check CHECK (NOT seo_geo_enabled OR live),
    CONSTRAINT resumes_title_length_check CHECK (char_length(title) <= 160),
    CONSTRAINT resumes_lng_length_check CHECK (lng IS NULL OR char_length(lng) <= 35),
    CONSTRAINT resumes_schema_version_check CHECK (schema_version >= 1),
    CONSTRAINT resumes_revision_check CHECK (revision >= 1)
);
CREATE INDEX resumes_user_id_idx ON resumes (user_id);

CREATE TABLE slug_tombstones (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    slug text NOT NULL,
    released_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    released_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT slug_tombstones_slug_key UNIQUE (slug),
    CONSTRAINT slug_tombstones_slug_format_check CHECK (
        char_length(slug) >= 4
        AND char_length(slug) <= 30
        AND slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
    )
);

CREATE TABLE idempotency_records (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    route text NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT idempotency_records_user_route_key_key
        UNIQUE (user_id, route, idempotency_key)
);
CREATE INDEX idempotency_records_expires_at_idx
    ON idempotency_records (expires_at);

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

CREATE TRIGGER resumes_enforce_cap
BEFORE INSERT OR UPDATE OF user_id ON resumes
FOR EACH ROW EXECUTE FUNCTION enforce_resume_cap();
```

- [x] **Step 0 (spike, minutes, before anything):** append a minimal
      `CREATE FUNCTION … $$…$$; CREATE TRIGGER …` pair to a scratch copy of
      `schema.sql` and run `sqlc generate` against it. sqlc's pg_query-based
      parser is expected to accept plpgsql DDL and ignore it; **if it errors,
      STOP** — that is a missing tooling decision for the integration owner
      (splitting schema.sql would fork the single source of truth and is not
      this plan's call). Do not improvise.
- [x] **Step 1: failing migrated-DB tests.** `resume_schema_test.go`, using
      `testutil.RequireMigratedTestDatabaseURL` (skip/fail-closed pattern): -
      Constraint boundary matrix via direct SQL: slug length 3 → rejected, 4 →
      accepted, 30 → accepted, 31 → rejected; `-lead`, `trail-`, `dou--ble`,
      uppercase → rejected (each names `resumes_slug_format_check`);
      `live=true, slug NULL` → rejected; `seo_geo_enabled=true, live=false` →
      rejected; title at 160 → accepted, 161 → rejected; lng 35/36; `revision 0`
      → rejected; duplicate slug → `resumes_slug_key`; tombstone slug format +
      dup; idempotency `(user, route, key)` dup → unique violation. - Trigger
      existence + behavior: 3 inserts for one user succeed, 4th raises SQLSTATE
      `23514` message `resumes_user_cap_exceeded` — via **raw SQL**, proving
      no-store-bypass; deleting one row lets a new insert succeed; a second user
      is unaffected. - Trigger survives the migration path (not just
      schema.sql): these tests run against the goose-migrated DB, which is the
      point. Run:
      `cd apps/server && TEST_DATABASE_URL=… go test ./migrations/...     -run ResumeSchema -count=1`
      → **FAIL** (tables absent). **B1 ruling — this is the only permitted
      landing order, and why.** The DDL block above is shown as one unit for
      readability, but it **must not** be appended to `sql/schema.sql` in one
      shot. Task 1's `checkNoUndiffableObjects` cross-check runs as the _first,
      dependency-free_ step of `run()` in `cmd/migrate/gen/main.go` — before
      Atlas is even invoked, in both `-check` and generate mode. If the
      function/trigger DDL lands in `schema.sql` before any migration declares a
      matching statement, that cross-check fails `make migrate-gen` itself:
      there is no migration yet for it to match, so generating `00004` — which
      needs `migrate-gen` to run at all — becomes impossible with the
      function/trigger already present. The only way through is tables-first
      (nothing for the FUNCTION/TRIGGER cross-check to reject yet), then landing
      the function/trigger declaration and its matching hand-written migration
      **together, in the same edit**, so the cross-check always sees a
      schema.sql declaration and a migration statement appear atomically.

- [x] **Step 2a: append tables + indexes only; generate `00004`.** Append just
      the three `CREATE TABLE …`/`CREATE INDEX …` statements above (no function,
      no trigger) to `sql/schema.sql`. `make test-db-up` then `make migrate-gen`
      — inspect the generated `00004_add_resume_tables.sql`: it must contain the
      three tables, constraints, and indexes, and there is nothing else in
      `schema.sql` yet for it to omit. Rename per the tool's output convention
      (the pipeline numbers it).
- [x] **Step 2b: append function + trigger, and hand-write `00005`, in the same
      edit.** Append `CREATE FUNCTION enforce_resume_cap` and
      `CREATE TRIGGER resumes_enforce_cap` to `sql/schema.sql`, **and** in the
      same edit hand-write `00005_add_resume_cap_trigger.sql` (goose
      `-- +goose Up` with `-- +goose StatementBegin/StatementEnd` around the
      function body — goose otherwise splits on the body's semicolons — and a
      `-- +goose Down` dropping trigger then function; Task 1's B2 scoping means
      that `Down` section is never scanned by the cross-check), with a header
      comment mirroring `00001_extensions.sql`'s explaining _why_ it is
      hand-written. Landing either half without the other fails Task 1's gate in
      that direction (schema.sql alone → no matching migration to cross-check
      against; migration alone → the migration's statement has no `schema.sql`
      declaration to match) — that is the intended fail-shut behavior, not a bug
      to work around. Refresh the directory hash:
      `cd apps/server && atlas migrate hash --dir file://migrations     --dir-format goose`
      (pinned v1.2.0). `atlas.sum` is a serialized artifact — this task's commit
      is its one legitimate change.
- [x] **Step 3: green.** Step 1's tests pass. Then the full data gates:
      `make sqlc-check` (no query changes yet — must stay clean),
      `make server-migration-test` (harness picks up 00004/00005 in all four
      scenarios), `make data-drift` (Task 1's cross-check now proves
      schema.sql's fn/trigger match 00005 — also run the red case once locally:
      perturb one token of the function body in `schema.sql`, confirm
      `make data-drift` fails, revert).
- [x] **Step 4: commit** —
      `git commit -m "feat(resume): add resumes, slug_tombstones, idempotency_records tables and 3-resume cap trigger" -- apps/server/sql/schema.sql apps/server/migrations apps/server/internal/store/models.go`
