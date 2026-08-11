# Phase 2A — Resume domain & store (implementation plan)

> **Adopted Rev 3 (2026-08-03).** Rev 2's independent adversarial findings and
> owner corrections remain binding. Rev 3 is the execution-status and
> spec-consistency correction: it records Tasks 1–6 as reviewed and Task 7's
> review/re-review findings, restores the immutable v1 schema plus bidirectional
> adjacent-converter contract required by design §3, and makes the phase gates
> explicit. Acceptance rows `AC-DOC-010`, `AC-DOC-011`, `AC-DOC-012`, and
> `AC-SAVE-003` already exist in `../traceability/`.
>
> **Checkpoint integration (owner direction, 2026-08-03):** reviewed Tasks 1–7
> and their title/lint/idempotency corrections were integrated into `main` at
> `5805ddc` before phase exit. This is a development checkpoint only: all open
> tasks and every phase gate below remain binding, and P2B stays blocked.
>
> **For agentic workers (once adopted):** execute with
> superpowers:subagent-driven-development, one task per fresh subagent, Opus 5
> defect review between tasks. Steps use checkboxes: `[x]` records completed
> author work and `[ ]` remains an open action/gate. Every task's tests are
> written **before** implementation (TDD): write the failing test, observe the
> failure, implement, observe the pass, then review and commit.

**Goal:** the resume domain's data layer — `resumes`, `slug_tombstones`, and
`idempotency_records` tables with every spec §3 constraint and the DB-enforced
3-resume cap trigger; a single validated store layer all resume writes pass
through (JSON-Schema bounds, aggregate invariants, entry-id uniqueness, date
ranges, byte-exact size bounds — every bound with a limit+1 test); revision CAS
write safety; the idempotency record primitive; and the doc-shape migration
machinery (projection-only on read, CAS on write, CAS backfill) — all proven
against a live Postgres via the shared migration-applying test helper. No HTTP
surface: P2B owns endpoints, media, and OpenAPI changes.

**Execution base:** the phase started from `ad357d3` ("Merge Phase 1:
authentication and sessions", 2026-08-02). Remaining workers start from current
`main` at or after checkpoint `5805ddc`, run `git rev-parse HEAD`, and confirm
that ancestry before starting — worktree agents have checked out stale commits
before; verify, don't assume.

**Migration head:** the phase targeted `00003_add_sessions_rotated_from` and the
integrated checkpoint now carries `00004_add_resume_tables` plus
`00005_add_resume_cap_trigger`. Remaining work treats `00005` as the current
head and never edits a released migration or `atlas.sum` by hand outside the
pipeline described in Task 3. P1.1 needs no migration. Schema-head changes are
serialized through the integration owner; one merges at a time.

## Contents

Reference sections of this plan:

- [Design decisions this plan makes beyond the spec](decisions.md)
- [File structure produced by this phase](file-structure.md)
- [The write path this phase builds](write-path.md)
- [Integration handoffs (owner-applied, not worker-applied)](integration-handoffs.md)
- [Phase exit criteria](exit-criteria.md)

One file per task, in execution order:

- [Task 1: Extend the data-drift gate (unblocks the trigger)](task-01-drift-gate.md)
- [Task 2: Schema-package additions — entry-id uniqueness (AC-DOC-002) + embedded raw schema (D2)](task-02-schema-entry-id-uniqueness.md)
- [Task 2b: Establish the immutable v1 schema and released-version registry (AC-DOC-012)](task-02b-immutable-v1-schema.md)
- [Task 3: `resumes`, `slug_tombstones`, `idempotency_records` DDL + 3-resume trigger + migrations 00004/00005](task-03-resume-tables-and-migrations.md)
- [Task 4: sqlc queries + regenerated store layer](task-04-sqlc-queries.md)
- [Task 5: Document codec + validation pipeline — every size bound with a limit+1 test](task-05-codec-and-validation.md)
- [Task 6: Resume store — create (cap), get/list (projected), delete, revision CAS](task-06-resume-store.md)
- [Task 6a: Preserve a cleared contact value through validation and live writes (AC-DOC-009)](task-06a-cleared-contact-value.md)
- [Task 6b: Mechanically restrict generated resume writes to the domain store](task-06b-write-chokepoint.md)
- [Task 7: Idempotency record store (replay / reject / rollback primitive)](task-07-idempotency-store.md)
- [Task 8: Doc-shape migration machinery — projection-only read, CAS write persistence, CAS backfill, wire-version declarations](task-08-doc-shape-migration.md)
- [Task 9: Blind adversarial suite A — write-safety and cap concurrency matrix](task-09-blind-suite-write-safety.md)
- [Task 10: Blind adversarial suite B — doc-migration purity and CAS-vs-autosave races](task-10-blind-suite-doc-migration.md)
- [Task 11: Blind adversarial suite C — independently-derived size-bound limit+1 matrix](task-11-blind-suite-bounds.md)
- [Task 12: Traceability closure, docs, and integration handoffs](task-12-traceability-closure.md)

## Task status index

This is the owner-facing execution ledger. A task is not complete until its
independent defect review is green; implementation alone is reported separately.

| Task | Deliverable                                    | State                                      |
| ---- | ---------------------------------------------- | ------------------------------------------ |
| 1    | Drift-gate support for trigger/function DDL    | **Complete and independently reviewed** ✅ |
| 2    | Shared validation + embedded current schema    | **Complete and independently reviewed** ✅ |
| 2b   | Immutable `resume.v1.schema.json` foundation   | **Required before Task 8**                 |
| 3    | Resume tables, cap trigger, migrations         | **Complete and independently reviewed** ✅ |
| 4    | sqlc queries and generated store               | **Complete and independently reviewed** ✅ |
| 5    | Codec and full-bounds validation               | **Complete and independently reviewed** ✅ |
| 6    | Resume CRUD, ownership, cap, and revision CAS  | **Complete and independently reviewed** ✅ |
| 6a   | Cleared-contact draft fixture + live roundtrip | **Required before phase gate**             |
| 6b   | Generated-write-method mechanical restriction  | **Required before phase gate**             |
| 7    | Transactional idempotency primitive            | **Complete and independently reviewed** ✅ |
| 8    | Projection, bidirectional wire conversion, CAS | **Blocked on Task 2b**                     |
| 9    | Blind write-safety/concurrency suite           | **Pending**                                |
| 10   | Blind projection/backfill race suite           | **Pending**                                |
| 11   | Blind independently derived bounds suite       | **Pending**                                |
| 12   | Traceability, docs, and integration handoffs   | **Pending**                                |
| Gate | Design, adversarial, UAT, and evidence reviews | **Pending**                                |

### Immediate next-action order

1. Execute and independently review Task 2b (retained immutable v1 schema/types
   and released-version registries).
2. Execute and independently review Task 6a (cleared-contact shared fixture and
   live round-trip).
3. Execute integration-owner Task 6b and its independent security/defect review
   (mechanical generated-write choke point).
4. Execute and independently review Task 8 (projection, adjacent conversion,
   wire declarations, and CAS backfill).
5. Run Tasks 9, 10, and 11 with three separate fresh blind-test authors, then
   route any product finding to a separate implementation author and re-review.
6. Close Task 12 traceability, documentation, and P2B handoffs.
7. Run the phase design/consistency review, traceability closure, fresh
   adversarial review, immutable UAT catalog, and independent evidence
   verification at the exact candidate commit. Only then mark P2A complete and
   unlock dependent phases; checkpoint `5805ddc` did neither.

**Spec:** `../../specs/aboutme-design.md` §3 — the `resumes` and
`slug_tombstones` rows of the data-model table; "Relational constraints &
store-layer invariants" (all bullets); "Entry fields per sectionType";
"Optionality: draft-permissive, publish-strict"; the aggregate invariant bullet;
the doc-shape-migrations bullet; "Schema management" (declarative pattern + the
**wire-version compatibility** row: P2A owns both adjacent conversion directions
and synthetic old-client preparation/emission; P2B owns the real HTTP
persistence proof); §4 "Write-safety" (revision CAS + idempotency semantics —
the HTTP surface itself is P2B). **Master plan:** `../implementation-plan.md`
"Phase 2A — Resume domain & store" including the carried drift-gate limitation
note, plus "Global constraints", "Agent workflow", "Integration discipline",
"Testing strategy" (the Write-safety/concurrency row is owned here).
**Budgets:** `../budgets.md` — request body ≤ 256 KB (P0B middleware, already
enforced, not re-implemented here) and **resume document total ≤ 512 KB (P2A
store)**. **Traceability:** claims and gaps in the table below.

## Traceability rows claimed by this phase

| ID          | Statement                                                        | Claimed by      |
| ----------- | ---------------------------------------------------------------- | --------------- |
| AC-DOC-001  | Max 3 resumes per user, DB-enforced                              | Tasks 3, 6, 9   |
| AC-DOC-002  | Entry ids unique across the whole resume                         | Tasks 2, 5      |
| AC-DOC-003  | Date ranges (start ≤ end) — "not yet wired into live writes"     | Task 5 (wiring) |
| AC-DOC-004  | Every document bound has a limit+1 test                          | Tasks 5, 11     |
| AC-DOC-007  | Rich text ≤ 16 KB UTF-8 bytes — "not yet wired into live writes" | Task 5 (wiring) |
| AC-DOC-008  | Layout exactly-once aggregate — "not yet wired into live writes" | Task 5 (wiring) |
| AC-DOC-009  | Cleared contact value remains draft-valid and round-trips        | Task 6a         |
| AC-DOC-010  | Projection-only read, CAS write/backfill                         | Task 8          |
| AC-DOC-011  | Resume document total ≤ 512 KB at the store                      | Tasks 5, 11     |
| AC-DOC-012  | Immutable schemas + bidirectional wire compatibility             | Tasks 2b, 8     |
| AC-SAVE-003 | Idempotency store replay/reject/rollback                         | Tasks 7, 9      |

The following boundary is intentionally deferred and must remain visible:

- Customization delta paths from a fixed allowlist (spec §3 size-bounds bullet)
  — deferred to P2B with the delta-applying endpoint (decision D14); it has no
  row either way. Flagged so it is not lost.

## Existing foundation this plan builds on (verified at `ad357d3`)

| Piece                                                            | What's already there                                                                                                                                                                                                                                                                                                                            |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `apps/server/internal/store`                                     | sqlc-generated `Queries` (+ `WithTx(tx pgx.Tx)`), hand-written `Pool` (`MaxPoolSize` 20); sqlc.yaml overrides: `uuid → uuid.UUID`, `jsonb → json.RawMessage`, nullable → native pointers                                                                                                                                                        |
| `apps/server/internal/testutil`                                  | `RequireMigratedTestDatabaseURL(t)` — skip-or-fail-closed DSN helper **plus** advisory-locked migration apply; **every DB-backed test in this phase uses it**; also `Clock` (injectable time) and deterministic id helpers                                                                                                                      |
| `apps/server/sql/schema.sql`                                     | Declarative source of truth (auth tables); sqlc + Atlas both read it                                                                                                                                                                                                                                                                            |
| `apps/server/migrations/`                                        | `00001_extensions` (hand-written; the precedent for non-Atlas-diffable DDL), `00002_add_auth_tables`, `00003_add_sessions_rotated_from`, `atlas.sum`; 4-scenario harness (`harness_test.go`) incl. two-runner advisory-lock test                                                                                                                |
| `apps/server/cmd/migrate/gen`                                    | Atlas-diff → goose pipeline; `checkExtensionDeclarations` (name-set cross-check precedent); `checkNoUndiffableObjects` — **unconditionally rejects** trigger/function/view/sequence in `schema.sql` until this phase extends it (its doc comment says so)                                                                                       |
| `packages/schema` (via `go.work` + `replace`)                    | `resume.schema.json` (frozen v1), generated `schema.Resume`/entry types, hand-written `Section` (strict decode, `DisallowUnknownFields`), hand-written `store_validate.go` (`ValidateDocument`: rich-text bytes, layout aggregate, date ranges, URL schemes, photo traversal) + `validation/store.ts` TS half, shared `fixtures/store/*` corpus |
| `packages/schema/fixtures/store/invalid-duplicate-entry-id.json` | Staged for AC-DOC-002; **no validator or test consumes it yet** in either language                                                                                                                                                                                                                                                              |
| `apps/server/internal/auth`                                      | The established local patterns to follow: error sentinels, injected `now func() time.Time`, `export_test.go` seams, table-driven tests, adversarial `_adversarial_test.go` files                                                                                                                                                                |

## Environment facts (verified 2026-08-02 at `ad357d3`)

- Go 1.26.5; sqlc v1.31.1; Atlas **community v1.2.0** (the pinned version the
  drift gate enforces); Postgres 18.4 (`docker.io/library/postgres:18.4-alpine`
  via `make test-db-up`) — native `uuidv7()`, used for every new surrogate key.
- Run all Go commands from inside `apps/server` (the root `go.work` ties it to
  `packages/schema/gen/go`; `go build ./...` at the repo root matches no
  packages). If any command materializes a root `go.work.sum`, hand it to the
  integration owner to commit — never delete it.
- `make server-test-db` is the DB-backed gate: it sets `REQUIRE_TEST_DB=1`
  (fails closed on a missing DSN), includes `./internal/resume/...`, and passes
  `-race -count=1`. The integration-owner change landed in `6efd179` and is
  retained in checkpoint `5805ddc`.
- `go test` caches passing results — every ad-hoc live-DB invocation in this
  plan carries `-count=1`.
- The migration harness (`make server-migration-test`) automatically covers new
  migration files in its empty→head / prev→head / concurrent / partial-failure
  scenarios; Task 3 adds object-existence and behavior assertions on top, not a
  parallel harness.
- No OpenAPI change in this phase: P2A ships no HTTP surface. `make api-check`
  appears in no task's gate for that reason.

## Global constraints (inherited, plus phase-specific)

- Latest stable, pinned exactly (`go get x@latest`, commit the resolved
  `go.mod`/`go.sum`; never hand-write versions).
- Google Go style; `gofmt`/`goimports`; table-driven tests; Conventional
  Commits; no AI/agent mentions, no trailers.
- Determinism: inject `now func() time.Time` into every type with a TTL or
  time-dependent behavior (idempotency TTL, tombstone timestamps); no
  `time.Sleep`-based assertions; concurrency tests must pass under
  `-race -count=20`, not "usually" (flaky = broken).
- `apps/server` keeps passing `go build ./... && go vet ./... && go test ./...`
  (hermetic — DB-backed cases self-skip without `TEST_DATABASE_URL`) after every
  task.
- Generated artifacts (`internal/store/*.go` from sqlc, `migrations/*.sql` from
  the pipeline, `packages/schema/gen/*` from `generate.mjs`) are committed but
  never hand-edited — change the source and regenerate. The two deliberate
  hand-written exceptions are goose migration files for DDL Atlas cannot diff
  (Task 3, following `00001_extensions.sql`'s precedent) and the documented
  hand-written files inside `gen/go` (`section.go`, `store_validate.go` — their
  headers say so).
- Stage only explicit owned paths (`git add -- <paths>`); never `git add .`;
  inspect `git diff --cached --name-only` before every commit; never stage
  `.env`, `CLAUDE.md`, `AGENTS.md`, or another worker's files.
- Schema head, `migrations/`, `atlas.sum`, generated `internal/store`, and
  lockfiles are serialized through the integration owner — Tasks 2–4 are the
  only tasks that touch them, in order.
- **Serialized against Phase 3 (owner ruling on B10):**
  `packages/schema/scripts/generate.mjs`, `packages/schema/gen/**`,
  `packages/schema/test/gen.test.ts`, and `apps/server/go.{mod,sum}` are
  contested with P3's T1/T8 and join the serialized-artifact list. Task 2 and
  Task 5 each require an **exclusive-ownership window** on those paths, granted
  and sequenced by the integration owner before dispatch; a worker finding
  uncommitted changes in them stops and reports rather than merging around them.
