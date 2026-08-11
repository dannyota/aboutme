# Task 1: Extend the data-drift gate (unblocks the trigger)

No acceptance ID — CI tooling. This is the master plan's carried blocker: until
it lands, `make migrate-gen` and `make data-drift` **reject** any
trigger/function in `sql/schema.sql`, so it strictly precedes Task 3. The
adversarial review **empirically re-confirmed the hole against the pinned
Atlas/sqlc**: a trigger declared in `schema.sql` and never migrated passes
`make data-drift` clean once the unconditional reject is naively removed — that
is the exact red case Step 2's body-drift test pins forever. Correction to the
master plan's wording (report, don't silently fix): the keyword-reject lives in
`apps/server/cmd/migrate/gen/main.go` (`checkNoUndiffableObjects` /
`undiffableObjectPattern`), which `scripts/check-data-drift.sh` invokes via
`go run ./cmd/migrate/gen -check` — the script itself needs no change.

**Files:** modify `apps/server/cmd/migrate/gen/main.go`,
`apps/server/cmd/migrate/gen/main_test.go` (+ `main_e2e_test.go` if the existing
e2e harness fits).

**Interfaces.** Produces (internal to the tool):

- A broadened `undiffableObjectPattern` covering
  `CREATE [OR REPLACE] [CONSTRAINT] TRIGGER`, `CREATE [OR REPLACE] FUNCTION`,
  `CREATE [OR REPLACE] PROCEDURE`,
  `CREATE [MATERIALIZED|RECURSIVE|TEMP|TEMPORARY|UNLOGGED] VIEW/SEQUENCE`,
  `CREATE RULE`, `CREATE POLICY` variants (review finding M-NEW's exact list
  plus B4's additions: `CREATE MATERIALIZED VIEW`, `CONSTRAINT TRIGGER`,
  `TEMP`/`UNLOGGED`/`RECURSIVE`, `PROCEDURE`, `RULE`, `POLICY`).
- **B4 addendum, unconditional, no escape hatch ever:** also reject
  `ALTER FUNCTION`, `ALTER TRIGGER`, and
  `ALTER TABLE … {ENABLE|DISABLE} TRIGGER`. Only a bare
  `CREATE [OR REPLACE] FUNCTION`/`CREATE [OR REPLACE] [CONSTRAINT] TRIGGER`
  statement is ever eligible for the D9 cross-check below — an `ALTER` that
  retargets a function body or silently toggles a trigger off must never get
  that escape hatch, since the cross-check only ever compares `CREATE` statement
  text.
- `checkUndiffableObjects(migrationsDir, schemaFile) error` replacing
  `checkNoUndiffableObjects`: FUNCTION/TRIGGER get the D9 statement-level
  cross-check (normalized statement in `schema.sql` == last occurrence across
  ordered migrations; names match in both directions; `DROP FUNCTION|TRIGGER` in
  any migration rejected); every other matched class — including the B4
  additions above — stays an unconditional rejection with the existing message
  shape.
- **B2 — the cross-check reads only `-- +goose Up`.** Split each migration file
  on its goose section markers before any statement extraction runs; feed only
  the `-- +goose Up` section's text to the FUNCTION/TRIGGER extraction and
  cross-check. `-- +goose Down` is never scanned. This is required because Task
  3's own `00005` file's `Down` section legitimately contains
  `DROP TRIGGER …; DROP FUNCTION …;` (rolling back the Up section) — if the gate
  scanned Down too, D9's own "any `DROP FUNCTION|TRIGGER` in a migration stays
  rejected" rule would trip against the plan's own migration every time.
- **B3 — normalization, pinned as an ordered pipeline** (each stage gets its own
  negative test): (1) strip `--` line comments and `/* */` block comments; (2)
  collapse runs of whitespace to one space, **except** inside a single-quoted
  string literal or a dollar-quoted span (`$$…$$` or `$tag$…$tag$`), where bytes
  compare verbatim; (3) compare the two normalized statements
  **case-sensitively** (no case-insensitive fallback — Postgres folds unquoted
  identifiers but not literal/dollar-quoted body text, and a case-insensitive
  compare could mask a real body drift); (4) elide a leading `OR REPLACE` before
  comparing, so `CREATE FUNCTION` in one place and `CREATE OR REPLACE FUNCTION`
  for the same object elsewhere don't false-positive as different declarations;
  (5) capture the object name from an optionally schema-qualified
  (`public.foo`), optionally double-quoted (`"Foo"`) identifier, anchored at the
  correct token — not the first identifier-shaped substring in the statement.
- Statement extraction must strip `--` comments first (existing pattern) and
  capture full statements: functions terminate at the `;` **after** the
  dollar-quoted body (`$$ … $$ LANGUAGE plpgsql;`) — a naive split-on-semicolon
  truncates inside the body; write the failing test for that first.

- [x] **Step 1: failing tests for the broadened keyword net.** Table-driven over
      schema texts containing each M-NEW variant → all must be detected (today
      `CREATE MATERIALIZED VIEW x` passes silently — assert the red). Extend the
      table with the B4 additions, each asserted unconditionally rejected with
      no cross-check path: `CREATE PROCEDURE`, `CREATE RULE`, `CREATE POLICY`,
      `ALTER FUNCTION`, `ALTER TRIGGER`, `ALTER TABLE …     ENABLE TRIGGER …`,
      `ALTER TABLE … DISABLE TRIGGER …`.
- [x] **Step 2: failing tests for the FUNCTION/TRIGGER cross-check.** Cases:
      schema declares fn+trigger, no migration → FAIL; matching hand-written
      migration → PASS; migration present but schema body edited (one token) →
      FAIL (the body-drift case name-set comparison would miss — this is what
      makes it a _real_ cross-check); a later migration re-declaring the fn with
      a new body + schema matching the new body → PASS (last-occurrence rule);
      migration declares a fn absent from schema → FAIL; `DROP FUNCTION` in a
      migration's `Up` section → FAIL; the dollar-quoted-body semicolon
      extraction case. **B2 scoping, both directions:** a migration whose
      `-- +goose Up` section declares the matching fn+trigger and whose
      `-- +goose Down` section drops trigger then function → PASS (Down is never
      scanned, so its `DROP`s never trigger the reject rule); a migration whose
      `Up` section is empty/ irrelevant but whose `Down` section happens to
      contain a `CREATE     FUNCTION`-shaped comment or string → the cross-check
      must not pick it up (Down stays entirely outside statement extraction).
      **B3 normalization, one negative test per bullet:** a comment injected
      mid-statement that must be stripped before comparison; whitespace
      reformatted (extra newlines/spaces) outside any quoted span → still
      matches; a single real body-token change **inside** a dollar-quoted span
      whose surrounding whitespace also differs → still FAIL (proves whitespace
      collapse doesn't accidentally erase the real diff); a same-object
      statement differing only by case in a literal/dollar-quoted body → FAIL
      (case-sensitive compare catches it — no folding); `CREATE     FUNCTION` vs
      `CREATE OR REPLACE FUNCTION` for the identical body → PASS (OR REPLACE
      elision); a schema-qualified (`public.foo`) or double-quoted (`"Foo"`)
      name in one location vs the bare name in the other, same object → PASS
      (name capture anchored correctly, not string-matched against the first
      identifier-shaped token).
- [x] **Step 3: implement; all red tests green.** Keep
      `checkExtensionDeclarations` untouched.
- [x] **Step 4: gate.**
      `cd apps/server && go build ./... && go vet ./... &&     go test ./cmd/migrate/gen/... -count=1`;
      then `make test-db-up &&     make data-drift` (must still pass clean at
      head — the check is a no-op until Task 3 adds objects) and
      `make server-migration-test`.
- [x] **Step 5: commit** —
      `git commit -m "feat(migrate): cross-check trigger and function DDL the Atlas differ cannot see" -- apps/server/cmd/migrate/gen`
