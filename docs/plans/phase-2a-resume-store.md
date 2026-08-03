# Phase 2A — Resume domain & store (implementation plan)

> **Adopted Rev 3 (2026-08-03).** Rev 2's independent adversarial findings and
> owner corrections remain binding. Rev 3 is the execution-status and
> spec-consistency correction: it records Tasks 1–6 as reviewed and Task 7's
> review/re-review findings, restores the immutable v1 schema plus bidirectional
> adjacent-converter contract required by design §3, and makes the phase gates
> explicit. Acceptance rows `AC-DOC-010`, `AC-DOC-011`, `AC-DOC-012`, and
> `AC-SAVE-003` already exist in `traceability.md`.
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

**Spec:** `../specs/aboutme-design.md` §3 — the `resumes` and `slug_tombstones`
rows of the data-model table; "Relational constraints & store-layer invariants"
(all bullets); "Entry fields per sectionType"; "Optionality: draft-permissive,
publish-strict"; the aggregate invariant bullet; the doc-shape-migrations
bullet; "Schema management" (declarative pattern + the **wire-version
compatibility** row: P2A owns both adjacent conversion directions and synthetic
old-client preparation/emission; P2B owns the real HTTP persistence proof); §4
"Write-safety" (revision CAS + idempotency semantics — the HTTP surface itself
is P2B). **Master plan:** `implementation-plan.md` "Phase 2A — Resume domain &
store" including the carried drift-gate limitation note, plus "Global
constraints", "Agent workflow", "Integration discipline", "Testing strategy"
(the Write-safety/concurrency row is owned here). **Budgets:** `budgets.md` —
request body ≤ 256 KB (P0B middleware, already enforced, not re-implemented
here) and **resume document total ≤ 512 KB (P2A store)**. **Traceability:**
claims and gaps in the table below.

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

## Design decisions this plan makes beyond the spec

The spec states the resume data-model **policy** precisely but leaves mechanisms
open. Each gap gets an explicit decision here, flagged for Fable/Opus 5 to
challenge in review — never a TODO.

| #   | Gap in the spec                                                                                                                        | Decision made here                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| --- | -------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D1  | Go has no ajv: nothing in `apps/server` enforces the schema's per-field `maxLength`/`maxItems`/`maxProperties`/patterns on live writes | Add the pinned latest stable `github.com/santhosh-tekuri/jsonschema/v6` (draft 2020-12) and validate the **canonical re-marshaled document** against the embedded schema on every store write, before typed decode and aggregate validation. Owner-ruled adoption conditions, all mandatory: (a) **format assertion enabled and pinned** (`uuid`/`uri` formats must assert, matching ajv's configuration — verify ajv's format posture in `packages/schema` and pin both sides identically); (b) the compiler is constructed with **no URL loader** — any attempt to resolve a remote/file `$ref` fails, so validation can never depend on the network or filesystem; (c) **compiled once at package init from `schema.RawSchema`, with a hard startup failure** if compilation fails — never lazily, never per-request; (d) the task report **records the transitive dependency set** the new module introduces (from `go mod graph` delta) for the Opus review; (e) the **cross-language verdict-parity test** (Task 5 Step 3b) over every `packages/schema` fixture plus the bounds corpus — that test, not the shared file, is what makes "one schema, both languages" true. **Rejected alternative, recorded:** generating the bounds checks from the schema in `generate.mjs` — the repo's precedent for types, and what P3 uses for the sanitizer allowlist — was considered and rejected because generating a JSON-Schema-subset compiler is its own correctness risk: a generator bug silently narrows enforcement, exactly the drift class this decision exists to prevent, whereas a widely-used conformance-tested validator is verified against the official JSON-Schema test suite |
| D2  | `resume.schema.json` lives outside the `gen/go` module, so `go:embed` cannot reach it                                                  | Owner-ruled: `packages/schema/scripts/generate.mjs` emits a **generated Go source constant** — `gen/go/rawschema.go` with the `// Code generated … DO NOT EDIT.` header exposing `schema.RawSchema []byte` — not a `.json` copy. It is covered by the existing generated-output byte-compare in `packages/schema/test/gen.test.ts` unchanged, and one Go test asserts `schema.RawSchema` byte-equals `../../resume.schema.json` read at test time, closing the loop (Task 2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| D3  | AC-DOC-002 (entry-id uniqueness) has a staged fixture but no validator in either language                                              | Extend the **shared store-layer validator in `packages/schema`** (both `validation/store.ts` and `gen/go/store_validate.go`, conformance-tested against `fixtures/store/invalid-duplicate-entry-id.json`), not an apps/server-only check — keeping the two halves in lockstep is the package's whole point. This is a post-P0 contract-adjacent change: a dedicated reviewed commit (Task 2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| D4  | Spec stores ONE document shape but the table has three jsonb columns + `schema_version`                                                | The stored representation **decomposes** the document: `schema_version` column ↔ `doc.schemaVersion`; the three jsonb columns hold `personalDetails`/`content`/`customization` and **never contain a `schemaVersion` key**. `internal/resume`'s codec is the only assembly/decomposition point                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| D5  | Column defaults/bounds the spec doesn't state                                                                                          | `live` default `false`; `download_enabled` default `true`, `seo_geo_enabled` default `false` (mirroring §1's publish-dialog defaults so an unpublished row is already in the default publish posture); `revision` starts at **1**; `schema_version` app-written, `NOT NULL`, no DB default (it must match the document, a DB default could lie); `title text NOT NULL`, `CHECK char_length(title) <= 160`, empty string allowed (draft-permissive spirit: clearing a title to retype must not block a write); `lng text NULL`, `CHECK char_length(lng) <= 35` (BCP 47 ceiling), format unvalidated — the documented i18n boundary. **Ratified by the integration owner 2026-08-02** (title ≤ 160, lng ≤ 35, and D11's 24 h TTL); the owner lands the three numbers in `docs/plans/budgets.md` — an owner-landed prerequisite, not a P2A worker edit                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| D6  | "Ordered map" `content` vs Postgres jsonb, which does **not** preserve object key order                                                | Section **order authority is `customization.layout.sections`** (already the aggregate invariant's structure); entry order is the `entries` array. jsonb key normalization is therefore harmless. **Owner ruling: this contradicts a frozen-spec reading and therefore needs an ADR, not a plan note** — the integration owner authors it (as with ADR 0008 for P3's template-apply contradiction); the ADR is an **owner-landed prerequisite** this plan is written against. Recorded so no one "fixes" ordering by switching to `json` or an order array inside `content`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |

> **Owner correction 3 (2026-08-03) — two forward-binding constraints D7 states
> but does not pin.** Task 3's review established both; they bind Tasks 4, 6 and
> 7, and any later phase writing `resumes`.
>
> 1. **Every transaction touching `resumes` must run at READ COMMITTED**
>    (Postgres's default — so this is a prohibition on raising it, not a setting
>    to add). The trigger's lock-then-count is race-proof only there: the count
>    following `PERFORM … FOR UPDATE` takes a fresh snapshot and sees the
>    competing writer's committed row. At REPEATABLE READ the count reads a
>    snapshot predating the lock, still sees 2 rows, and admits a 4th resume —
>    violating AC-DOC-001 through exactly the bypassing-writer path D7 claims to
>    close. The trigger comment now says so; no
>    `SET TRANSACTION ISOLATION LEVEL` may appear in any P2A store path.
> 2. **No update statement may list `user_id` in its `SET` clause.** The trigger
>    fires on `UPDATE OF user_id` and counts rows including the one being
>    updated, so a self-referential `SET user_id = user_id` on an owner already
>    at 3 falsely raises `resumes_user_cap_exceeded`. Task 4's
>    `UpdateResumeDocumentCAS`/`UpdateResumeTitleCAS` already exclude it; this
>    records that as a requirement rather than an accident.

### D7 — Trigger mechanics and race safety

Use a `BEFORE INSERT OR UPDATE OF user_id` trigger. Its function first locks the
owner row (`PERFORM 1 FROM users WHERE id = NEW.user_id FOR UPDATE`) and then
counts, so it remains race-safe for writers bypassing the store. Store creates
take the same lock in the same order. A cap violation raises SQLSTATE `23514`
with message `resumes_user_cap_exceeded`; the store maps only that exact pair to
`ErrResumeCapExceeded`.

### D8 — Migration authorship

Generate `00004_add_resume_tables.sql` with `make migrate-gen`; hand-author
`00005_add_resume_cap_trigger.sql` for function/trigger DDL Atlas Community
cannot diff, following `00001_extensions.sql`; refresh `atlas.sum` with the
pinned Atlas. D9 makes the hand-authored object drift-safe.

### D9 — Drift-gate extension

`cmd/migrate/gen` broadens its unconditional keyword net, but permits FUNCTION
and TRIGGER only when each normalized declaration in `schema.sql` matches the
last same-object declaration in ordered migration `Up` sections, in both
directions. Drops, alters, views, sequences, procedures, rules, policies, and
all unsupported variants still fail closed. Task 1 pins statement extraction and
normalization details.

### D10 — Total-document measurement

Measure the 512 KB limit on canonical assembled JSON, including the injected
`schemaVersion`. This is deterministic, independent of jsonb representation, and
protects later granular writes even when each HTTP body is below 256 KB.

### D11 — Idempotency record and retention

Use `UNIQUE (user_id, route, idempotency_key)`, SHA-256 of the raw request body,
`response_status`, jsonb `response_body`, and a 24-hour expiry. The mutation and
record insert share one transaction. After the expiry preflight, take the
existing user-row transaction lock before the live-record lookup; same-user
contenders serialize in the same order as resume Create, so a same-key follower
observes and replays/rejects the winner before invoking its callback. The unique
constraint remains a fail-closed backstop. Callbacks may use only the supplied
transaction and must have no non-transactional side effects: a callback can run
again after a rolled-back/crashed attempt even though concurrent committed
duplicates are prevented.

Every `Execute` first commits an opportunistic reap of the calling user's
expired rows so cleanup survives key-reuse and mutation errors. The mutation
transaction repeats the same-key expiry check to close the race. A global sweep
may join P8-priv later.

### D12 — CAS backfill visibility

Backfill changes neither `revision` nor `updated_at`: all readers already see
the projected current document, so the storage rewrite is not observable. Its
predicate is `id + old schema_version + observed revision`. Task 8 must prove
byte-identical projected reads before/after backfill. P2B must persist the full
document through the codec; granular `jsonb_set` writes are forbidden because
they could reintroduce old shape outside the backfill CAS.

### D13 — Bidirectional adjacent converters

Each adjacent pair registers explicit `Up` and `Down` functions over full
canonical JSON. Production v1 has no pair; synthetic v1⇄v2 tests prove both
directions, old-client preparation to the current canonical shape,
supported-version emission, and fail-closed missing paths. Accepted input and
emitted output version sets are declared separately. Real HTTP persistence is
P2B's AC-SAVE-004 gate.

### D14 — Customization delta boundary

P2B owns the fixed allowlist for `PATCH …/customization` because it bounds the
request delta. P2A still validates the complete stored aggregate on every write.
The P2B handoff and traceability gap remain explicit.

### D15 — Slug tombstones

Use a uuidv7 surrogate primary key, unique slug, nullable
`released_by_user_id … ON DELETE SET NULL`, and `released_at`. Do not store
`expires_at`; P5A applies the authoritative 180-day claim-time rule and owns
tombstone queries/re-release behavior.

### D16 — Store validation choke point

Write APIs take typed `schema.Resume`, re-marshal canonical JSON, then run
JSON-Schema validation, the total-size bound, and aggregate validation. Strict
decode remains P2B's ingress guard. Generated sqlc write methods remain a
convention until the phase-exit lint restriction lands.

### D17 — Ownership scoping

Every per-resume query includes `id + user_id`. Wrong-owner and missing rows map
to the same `ErrNotFound`, including CAS methods, so the store exposes no
existence oracle.

### D18 — Pure projected reads

`Get` and `List` project stored documents to `CurrentVersion` without writes and
also expose the stored version for backfill progress. Tests pin unchanged row
bytes, revision, and `updated_at`; one unprojectable List row fails the whole
operation with no partial result.

### D19 — Immutable released schemas

`resume.v1.schema.json` is the immutable released contract. `resume.schema.json`
remains the current generation entry point and is byte-identical while
`CurrentVersion == 1`; generated code exposes raw schemas and retained Go/TS
type namespaces by released version. Store validation accepts only a document
already projected to current, while conversion validates both source and target
schemas. CI rejects modification/deletion/rename of released schema files and
requires every released schema to retain derived version-scoped types. Generated
bytes may change only through reviewed generator fixes and regeneration; a new
schema version is append-only and requires an adjacent `Up`/`Down` pair plus
declaration changes.

## File structure produced by this phase

| File                                                                                                                  | Responsibility                                                                                                                |
| --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `apps/server/cmd/migrate/gen/main.go` (modify) + tests                                                                | D9 drift-gate extension (Task 1)                                                                                              |
| `packages/schema/validation/store.ts`, `gen/go/store_validate.go` (+tests) (modify)                                   | Entry-id uniqueness validator, both halves (Task 2)                                                                           |
| `packages/schema/scripts/generate.mjs` (modify), `gen/go/rawschema.go` (generated)                                    | D2 raw-schema Go source constant (Task 2); byte-compare coverage via existing `packages/schema/test/gen.test.ts`              |
| `packages/schema/resume.v1.schema.json`, `gen/{go,ts}/v1/**`, registries/tests                                        | Immutable v1 schema and retained generated types (Task 2b)                                                                    |
| `packages/schema/fixtures/bounds/` (generated corpus + `manifest.json`), `packages/schema/test/bounds-parity.test.ts` | D1(e) cross-language verdict-parity corpus: ajv and jsonschema/v6 must agree on every fixture + bounds document (Tasks 5, 11) |
| `apps/server/sql/schema.sql` (append)                                                                                 | `resumes`, `slug_tombstones`, `idempotency_records`, cap function + trigger (Task 3)                                          |
| `apps/server/migrations/00004_add_resume_tables.sql` (generated)                                                      | Tables/constraints/indexes (Task 3)                                                                                           |
| `apps/server/migrations/00005_add_resume_cap_trigger.sql` (hand-written) + `atlas.sum`                                | Function + trigger DDL Atlas cannot diff (Task 3)                                                                             |
| `apps/server/migrations/resume_schema_test.go`                                                                        | Migrated-DB constraint/trigger existence + behavior tests (Task 3)                                                            |
| `apps/server/sql/queries.sql` (append), `apps/server/internal/store/*.go` (regenerated)                               | sqlc queries + generated types (Task 4)                                                                                       |
| `apps/server/internal/resume/{resume.go,codec.go,validate.go,store.go}` + tests                                       | Domain type, codec (D4), validation pipeline (D16), store API (Tasks 5–6)                                                     |
| cleared-contact fixture + Go/TS/store tests                                                                           | Close AC-DOC-009 at shared validation and live-write boundaries (Task 6a)                                                     |
| `.semgrep.yml`, policy regression script, root `Makefile`                                                             | Mechanically restrict generated resume/idempotency writes to `internal/resume` (Task 6b)                                      |
| `apps/server/internal/resume/bounds_test.go`                                                                          | The schema-driven limit+1 harness (Task 5)                                                                                    |
| `apps/server/internal/resume/idempotency.go` + tests                                                                  | D11 idempotency primitive (Task 7)                                                                                            |
| `apps/server/internal/resume/docmigrate/{docmigrate.go,backfill.go}` + tests                                          | D12/D13/D18 projection + CAS backfill + wire-version declarations (Task 8)                                                    |
| `apps/server/internal/resume/{writesafety,docmigrate,bounds}_adversarial_test.go`                                     | Blind suites A, B, and C (Tasks 9–11; three separate fresh authors)                                                           |
| `apps/server/go.mod`/`go.sum` (modify)                                                                                | `santhosh-tekuri/jsonschema/v6` pin (Task 5; serialized per B10)                                                              |
| `docs/plans/traceability.md` (modify)                                                                                 | Row closure against owner-minted IDs (Task 12)                                                                                |

Not touched by this phase: `docs/api/openapi.yaml`, `apps/web/**`, and
`deploy/**`. The integration owner already landed the root `Makefile` change
that adds the resume package to `server-test-db`. Task 2b copies the
already-frozen current schema into the first immutable released-version file; it
does not change the schema contract.

## The write path this phase builds

```mermaid
flowchart TD
    A[P2B handler - future] -->|schema.Resume + expected revision| B[resume.Store]
    B --> C[canonical marshal - D16]
    C --> D[JSON-Schema validate vs embedded resume.schema.json - D1]
    D --> E[total doc &le; 512 KB - D10]
    E --> F[schema.ValidateDocument: rich-text bytes, layout, dates, URL schemes + entry-id uniqueness - D3]
    F --> G{valid?}
    G -->|no| H[ErrDocumentInvalid with issues]
    G -->|yes| I[tx: CAS UPDATE ... WHERE id AND user_id AND revision = expected]
    I -->|1 row| J[revision + 1 returned]
    I -->|0 rows| K[re-read: ErrNotFound or ErrRevisionMismatch with current doc + revision]
```

Backfill vs autosave (the D12 race, proven in Tasks 8/10):

```mermaid
sequenceDiagram
    participant BF as Backfill job
    participant DB as resumes row (v_old, rev 7)
    participant AS as Autosave (CAS rev 7)
    BF->>DB: read schema_version=v_old, revision=7
    AS->>DB: UPDATE ... SET doc(v_cur), revision=8 WHERE revision=7
    DB-->>AS: 1 row - autosave wins, doc now current version
    BF->>DB: UPDATE ... WHERE schema_version=v_old AND revision=7
    DB-->>BF: 0 rows - BackfillLostRace: retryable (observation stale; re-observe and retry)
```

---

### Task 1: Extend the data-drift gate (unblocks the trigger)

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

---

### Task 2: Schema-package additions — entry-id uniqueness (AC-DOC-002) + embedded raw schema (D2)

Post-P0 contract-adjacent change: dedicated reviewed commit(s), regeneration
included, per the master plan's contract-change rule. Does **not** touch
`resume.schema.json` itself. **Serialized (B10):** every file below is on the
P3-contested list; this task runs only inside an owner-granted
exclusive-ownership window on `packages/schema/**`.

**Files:** modify `packages/schema/validation/store.ts`,
`packages/schema/test/store-validation.test.ts`,
`packages/schema/gen/go/store_validate.go`,
`packages/schema/gen/go/store_validate_test.go`,
`packages/schema/scripts/generate.mjs` (its byte-compare coverage lives in the
existing `packages/schema/test/gen.test.ts`); create (generated, committed)
`packages/schema/gen/go/rawschema.go`; create
`packages/schema/gen/go/rawschema_test.go`.

**Interfaces.** Produces:
`ValidateEntryIDUniqueness(content map[string]Section) []ValidationIssue` (Go) /
`validateEntryIdUniqueness` (TS), both folded into `ValidateDocument`/the TS
aggregate entry point; `schema.RawSchema []byte` in generated `rawschema.go` (D2
— a Go source constant with the `DO NOT EDIT` header, not an embedded `.json`
file).

- [x] **Step 1: failing conformance tests, both languages.** Go:
      `TestValidateDocument_DuplicateEntryID` consuming
      `fixtures/store/invalid-duplicate-entry-id.json` (duplicate ids in
      **different sections** — the whole-resume rule, not per-section); TS: the
      mirror case in `store-validation.test.ts`. Add one green case (same id
      nowhere duplicated) and one cross-section-duplicate fixture already exists
      — verify it encodes the cross-section case; if it is same-section-only,
      add a second fixture rather than editing it. Run
      `cd packages/schema && npm test` and
      `cd packages/schema/gen/go && go test ./...` → **FAIL**.
- [x] **Step 2: implement both halves; green.** Deterministic issue ordering
      (sort by path) like the existing validators.
- [x] **Step 3: failing raw-schema test.** `rawschema_test.go`: read
      `../../resume.schema.json` at test time and assert `schema.RawSchema`
      byte-equals it — this one test closes the copy-drift loop from the Go
      side; the existing `gen.test.ts` byte-compare covers it from the generator
      side unchanged. Run → **FAIL** (`RawSchema` undefined). Extend
      `generate.mjs` to emit `rawschema.go` (generated header, `DO NOT EDIT`);
      run `make schema-gen`; commit generated output; green.
- [x] **Step 4: gate.** `make schema-check` (regenerates via npm ci + vitest,
      incl. `gen.test.ts`; proves no drift) and
      `cd packages/schema/gen/go && go test ./...`.
- [x] **Step 5: commit** —
      `git commit -m "feat(schema): enforce whole-resume entry-id uniqueness and generate the raw-schema Go constant" -- packages/schema`

---

### Task 2b: Establish the immutable v1 schema and released-version registry (AC-DOC-012)

This correction is required by design §3 before Task 8. It does not invent v2;
it preserves the already released v1 bytes and makes the append-only policy
enforceable from the first version.

**Files:** create `packages/schema/resume.v1.schema.json`, retained
version-scoped generated types under `packages/schema/gen/go/v1/**` and
`packages/schema/gen/ts/v1/**`, plus released-version manifests/registries;
modify `packages/schema/scripts/generate.mjs` and generator/drift tests. The
integration owner adds a `released-schema-append-only` job beside
`.github/workflows/ci.yml`'s existing `migrations-append-only` job.

- [ ] **Step 1: failing immutability/derivation tests.** Assert the current
      `resume.schema.json` and `resume.v1.schema.json` are byte-identical while
      `CurrentVersion == 1`; current Go/TS outputs and `RawSchema` derive from
      v1; independently import/compile the retained versioned Go v1 and TS v1
      types; a raw-schema/type-manifest registry contains exactly version 1; and
      unknown versions fail closed.
- [ ] **Step 2: add the immutable snapshot and version-aware generator input;
      regenerate; green.** The generator takes an explicit released-schema path
      rather than discovering the newest filename implicitly. It emits both the
      current convenience outputs and version-scoped snapshots; future releases
      add a new namespace, while any regeneration of an old namespace remains
      mechanically derived from its immutable schema.
- [ ] **Step 3: add the exact append-only CI job.** In
      `.github/workflows/ci.yml`, parallel the name-status logic at lines
      141–156: allow only `A` for `packages/schema/resume.v*.schema.json` and
      reject `M`, `D`, and `R` against the PR base/before SHA. A shell-level
      negative test proves each rejected status and one added-version success.
      Separately, `schema-check` fails if any released schema lacks its
      version-scoped Go/TS output or if regeneration drifts; generated types are
      retained but may be regenerated by a reviewed generator/toolchain fix.
- [ ] **Step 4: gate.** `make schema-check`; Go tests for the generated schema
      package; fresh-cache lint for touched Go; `make docs-fmt docs-lint` if the
      shared gate or its documentation changes.
- [ ] **Step 5: independent defect review, then commit** —
      `git commit -m "feat(schema): preserve the immutable v1 resume contract" -- packages/schema`

---

### Task 3: `resumes`, `slug_tombstones`, `idempotency_records` DDL + 3-resume trigger + migrations 00004/00005

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

---

### Task 4: sqlc queries + regenerated store layer

Structural prerequisite; no acceptance ID.

**Files:** append `apps/server/sql/queries.sql`; regenerate
`apps/server/internal/store/` (committed, never hand-edited); create
`apps/server/internal/resume/resume_shapes_test.go` (compile-time shape
assertions, the P1 Task 1 pattern).

**Queries produced** (names verbatim — later tasks import them):

```sql
-- name: LockUserForResumeWrite :one
SELECT id FROM users WHERE id = $1 FOR UPDATE;

-- name: CreateResume :one
INSERT INTO resumes (user_id, title, schema_version, lng,
                     personal_details, content, customization)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: GetResumeForUser :one
SELECT * FROM resumes WHERE id = $1 AND user_id = $2;

-- name: ListResumesForUser :many
SELECT * FROM resumes WHERE user_id = $1 ORDER BY created_at, id;

-- name: CountResumesForUser :one
SELECT count(*) FROM resumes WHERE user_id = $1;

-- name: DeleteResumeForUser :execrows
DELETE FROM resumes WHERE id = $1 AND user_id = $2;

-- name: UpdateResumeDocumentCAS :one
UPDATE resumes
SET personal_details = $4, content = $5, customization = $6,
    schema_version = $7, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: UpdateResumeTitleCAS :one
UPDATE resumes
SET title = $4, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: BackfillResumeDocumentCAS :execrows
-- D12, all three omissions deliberate — do NOT "fix" them: this does not
-- bump `revision` (a backfill rewrites storage to something byte-identical
-- to what every reader was already served, so nothing observable changes),
-- does not bump `updated_at` (which tracks user-visible change), and is not
-- user-scoped (it is a system job).
-- Fully named params (owner decision 2026-08-03): the from/to schema
-- versions are both int32 and sqlc's positional naming would emit
-- `SchemaVersion` and `SchemaVersion_2`, neither carrying its direction.
-- A caller swapping them silently rewrites current rows back to the old
-- version. Named args make the pair unswappable at the call site.
UPDATE resumes
SET personal_details = sqlc.arg(personal_details),
    content = sqlc.arg(content),
    customization = sqlc.arg(customization),
    schema_version = sqlc.arg(to_schema_version)
WHERE id = sqlc.arg(id)
    AND schema_version = sqlc.arg(from_schema_version)
    AND revision = sqlc.arg(revision);

-- name: ListResumeIDsBelowSchemaVersion :many
SELECT id FROM resumes WHERE schema_version < $1 ORDER BY id LIMIT $2;

-- name: CreateIdempotencyRecord :exec
INSERT INTO idempotency_records
    (user_id, route, idempotency_key, request_hash,
     response_status, response_body, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetIdempotencyRecord :one
SELECT * FROM idempotency_records
WHERE user_id = $1 AND route = $2 AND idempotency_key = $3;

-- name: DeleteIdempotencyRecordIfExpired :execrows
DELETE FROM idempotency_records
WHERE user_id = $1 AND route = $2 AND idempotency_key = $3
    AND expires_at <= $4;

-- name: DeleteExpiredIdempotencyRecordsForUser :execrows
-- D11 opportunistic reaping: every Execute commits this per-user cleanup
-- before its mutation transaction, so expiry enforcement survives a rejected
-- key reuse or mutation error instead of depending on a future global job.
DELETE FROM idempotency_records
WHERE user_id = $1 AND expires_at <= $2;
```

Note `BackfillResumeDocumentCAS` deliberately does **not** touch `revision` or
`updated_at` (D12; `updated_at` tracks user-visible changes for the same reason)
and is not user-scoped (a system job). No `slug_tombstones` queries (D15 — P5A
defines that contract).

- [x] **Step 1: failing compile-time shape test** pinning the generated contract
      later tasks build on:
      `store.Resume{ID, UserID uuid.UUID, Title string, Slug *string, Live,     DownloadEnabled, SeoGeoEnabled bool, SchemaVersion int32, Revision     int64, Lng *string, PersonalDetails, Content, Customization     json.RawMessage, CreatedAt, UpdatedAt time.Time}`
      and `store.IdempotencyRecord{…}` (per the committed sqlc.yaml overrides:
      pointers for nullables, `json.RawMessage` for jsonb). Run
      `cd apps/server && go test ./internal/resume/...` → **FAIL**. **Owner
      decision (2026-08-03), replacing this step's open question:** sqlc emits
      `SeoGeoEnabled`, which breaks the Go initialism rule and would disagree
      with Task 6's domain field `SEOGeoEnabled` — two names for one concept
      differing only in casing, meeting at the codec. Add
      `seo_geo_enabled: "SEOGeoEnabled"` to `sqlc.yaml`'s existing `rename:`
      block, which already carries exactly this correction for `ua`, `ip`,
      `csrf_secret`, `pkce_verifier`, `redirect_uri` and `oauth_transaction`.
      This regenerates `internal/store/models.go` a second time (Task 3 landed
      it first under owner correction 1); `sqlc.yaml` and the regenerated output
      are therefore in **this** task's scope and commit.
- [x] **Step 2: append queries, `make sqlc-gen`, commit generated output; Step 1
      compiles green.**
- [x] **Step 3: gate.** `make sqlc-check` (regenerate → no diff),
      `make server-build server-vet server-test`, `make data-drift`.
- [x] **Step 4: commit** —
      `git commit -m "feat(resume): add resume and idempotency sqlc queries" -- apps/server/sql/queries.sql apps/server/sqlc.yaml apps/server/internal/store apps/server/internal/resume`
      (`sqlc.yaml` added 2026-08-03: the `SEOGeoEnabled` rename above lives
      there, and omitting it from the commit would leave `make sqlc-check`
      drifting on the next run.)

---

### Task 5: Document codec + validation pipeline — every size bound with a limit+1 test

Wires AC-DOC-003 / AC-DOC-007 / AC-DOC-008 into live writes; closes AC-DOC-002
at the write path; and closes AC-DOC-004 / AC-DOC-011 by enforcing the 512 KB
store budget and every schema bound with a limit+1 test.

**Files:** create `apps/server/internal/resume/{codec.go,validate.go}`,
`codec_test.go`, `validate_test.go`, `bounds_test.go`, `export_test.go`; modify
`apps/server/go.mod`/`go.sum` (add `github.com/santhosh-tekuri/jsonschema/v6` at
latest stable, pinned — **serialized per B10**, exclusive window required);
create (generated, committed) `packages/schema/fixtures/bounds/` +
`manifest.json` and `packages/schema/test/bounds-parity.test.ts` (**also
B10-serialized**).

**D1 adoption conditions bound to this task** (owner-ruled, all verified by test
or recorded evidence, none optional):

- Format assertion **enabled and pinned**, configured to match ajv's posture in
  `packages/schema` exactly (verify what ajv asserts for `format: uuid` /
  `format: uri` there first; whatever it is, both sides must agree — the parity
  test is the enforcement).
- Compiler constructed with **no URL loader**: resolving any external `$ref`
  fails. One test proves it (compile a schema with a remote `$ref` → error).
- Compiled **once at package init** from `schema.RawSchema`; compilation failure
  is a **hard startup failure** (panic in `init`/`MustCompile` style), never
  lazy, never per-call. One test proves the compiled schema is reused (pointer
  identity or init-once counter via export_test seam).
- The task report records the **transitive dependency delta** (`go mod graph`
  before/after) for the Opus review.
- **Verdict parity (D1(e))** is what makes "one schema, both languages" true —
  Step 3b below, not the shared file itself.

**Validation scope (D19):** `ValidateForStore` validates at `CurrentVersion`
only; its input is always a to-be-persisted current-version document or a
completed projection. Pre-current intermediate shapes never pass through it —
Task 8's synthetic-converter seam is the test that exercises that boundary.

**Interfaces.** Produces:

```go
package resume

const MaxDocumentBytes = 512 * 1024 // budgets.md: resume doc total, P2A store

// AssembleCanonical injects schemaVersion (D4) and marshals the canonical
// full document; DecodeParts strict-decodes the three stored jsonb parts.
func AssembleCanonical(doc schema.Resume) ([]byte, error)
func DecodeParts(personalDetails, content, customization json.RawMessage,
    schemaVersion int32) (schema.Resume, error)
// encodeParts is UNEXPORTED (owner correction 5): it is the only way to
// produce the three jsonb values, so keeping it package-private is the half
// of the D16 choke point that can actually be enforced. Tests reach it
// through export_test.go. AssembleCanonical stays exported — it marshals
// and never writes, and Task 11's blind suite consumes it by name.
func encodeParts(doc schema.Resume) (personalDetails, content,
    customization json.RawMessage, err error)

type ValidationError struct{ Issues []string } // stable, sorted, path-first
func (e *ValidationError) Error() string

// ValidateForStore is the single write-path choke point (D16/D1):
// canonical marshal → JSON-Schema validation (embedded schema.RawSchema) →
// MaxDocumentBytes → schema.ValidateDocument (incl. Task 2's entry-id
// uniqueness). Returns *ValidationError or nil.
func ValidateForStore(doc schema.Resume) error
```

- [x] **Step 1: failing codec round-trip tests.** Parts→doc→parts byte-stable
      for `packages/schema/fixtures/{minimal,full,draft-*}.json`
      (draft-permissiveness preserved: absent vs `""` distinguishable after a
      round trip — the spec's "never fabricate a sentinel" rule as a test);
      parts never contain a `schemaVersion` key (D4); unknown field in a stored
      part → decode error (strict).
- [x] **Step 2: failing pipeline tests.** Every `fixtures/store/invalid-*`
      fixture rejected by `ValidateForStore` with a matching issue; every valid
      fixture accepted; issues deterministic across runs.
- [x] **Step 3: the bounds harness (`bounds_test.go`) — failing first.** Two
      layers: 1. **Named-bound matrix**, one limit / limit+1 pair per bound:
      total doc `512*1024` bytes (construct via rich-text padding; +1 byte →
      rejected); 24 sections / 25; 64 entries in one section / 65; 16 personal
      details / 17; rich text 16384 bytes / 16385 (byte-exact, e.g. `é` padding
      per AC-DOC-007); and one pair per distinct `maxLength` class in the schema
      (36 sectionKey, 40 label, 64 iconKey, 80 displayName, 120
      city/country/name, 160 fullName/headline/jobTitle/…/title/subtitle, 256
      detail value, 512 photo key, 2048 link, 16384 richText code points). 2.
      **Completeness guard:** parse `schema.RawSchema` in the test, walk it for
      every `maxLength`/`maxItems`/`maxProperties` declaration, and assert the
      harness's exercised-bounds inventory covers each (path → limit). A future
      schema bound without a limit+1 test fails this guard loudly instead of
      silently shipping unenforced. This also closes AC-DOC-004's recorded
      partial-coverage note at the live-write layer (the P0 ajv fixture gap
      itself stays P0's row — see the companion note).
- [x] **Step 3b: the cross-language verdict-parity corpus (D1(e)) — failing
      first.** The Go bounds harness **emits its generated matrix documents** as
      a committed corpus: `packages/schema/fixtures/bounds/*.json` plus
      `manifest.json` rows
      `{file, boundPath, limit, expect: "valid"|     "invalid"}` (regenerated
      deterministically; `bounds_test.go` fails on drift against the committed
      corpus, same discipline as every generated artifact). Two consumers assert
      verdicts: `bounds_test.go` runs `jsonschema/v6` over the corpus **and
      every existing `packages/schema/fixtures/**` fixture** (valid/invalid by
      naming convention + the store-fixture expectations); the new
      `packages/schema/test/bounds-parity.test.ts` runs **ajv** over the
      identical corpus + fixtures and asserts the same verdicts. A disagreement
      anywhere — either direction — is a red build: this test, not the shared
      schema file, is what makes "one schema, both languages" true.
      (Store-layer-only rejections — entry-id duplicates, byte-length, layout
      aggregate — are marked `expect: "valid"` at the JSON-Schema layer in the
      manifest, with the store verdict as a separate column, so the two layers
      can never be conflated.)
- [x] **Step 4: implement (`jsonschema/v6` compiled once at init from
      `schema.RawSchema` per the D1 conditions, package-level, immutable); all
      green.** These are pure unit tests — no DB. Record the `go mod     graph`
      delta in the task report.
- [x] **Step 5: gate.**
      `cd apps/server && go build ./... && go vet ./... &&     go test ./internal/resume/... -count=1`;
      `make server-build server-vet     server-test`; `make schema-check` (the
      parity vitest rides in it).
- [x] **Step 6: commit** —
      `git commit -m "feat(resume): add document codec and full-bounds store validation pipeline" -- apps/server/internal/resume apps/server/go.mod apps/server/go.sum packages/schema/fixtures/bounds packages/schema/test/bounds-parity.test.ts`

---

> **Owner correction 5 (2026-08-03) — how far the write-path choke point is
> actually enforced.** D16 calls `internal/resume` the single write-path choke
> point, and Task 6's review correctly objected that a doc comment asserting
> that guarantee is not the same as providing it. What this phase enforces, and
> what it does not:
>
> - **Enforced:** `encodeParts` is **unexported**, so no package outside
>   `internal/resume` can produce the three jsonb values. Tests reach it through
>   the existing `export_test.go` seam.
> - **Not enforced, and deliberately named as convention:** sqlc generates
>   `store.Queries.CreateResume` / `UpdateResumeDocumentCAS` /
>   `UpdateResumeTitleCAS` as exported methods that any package may call. They
>   cannot be unexported without hand-editing generated code, which this repo
>   forbids. `AssembleCanonical` also stays exported — it marshals and never
>   writes, and Task 11's blind suite consumes it by name.
>
> A `forbidigo`-style lint rule restricting those three generated methods to
> `internal/resume` is the real closure and is **recorded as a phase-gate
> follow-up**, not silently skipped. Until it lands, `store.go`'s package
> comment must describe the convention as a convention — an unenforced invariant
> stated as a guarantee is what a future implementer will trust.

### Task 6: Resume store — create (cap), get/list (projected), delete, revision CAS

Satisfies the store half of **AC-DOC-001** and builds the write-safety primitive
AC-SAVE-001 (P2B) will surface over HTTP.

**Files:** create `apps/server/internal/resume/{resume.go,store.go}`,
`store_test.go`; extend `export_test.go`.

**Interfaces.** Produces:

```go
package resume

type Resume struct {
    ID              uuid.UUID
    UserID          uuid.UUID
    Title           string
    Slug            *string
    Live            bool
    DownloadEnabled bool
    SEOGeoEnabled   bool
    StoredSchemaVersion int32 // D18: pre-projection version, observable
    Revision        int64     // API serializes as string — P2B's concern
    Lng             *string
    Doc             schema.Resume // always projected to CurrentVersion
    CreatedAt, UpdatedAt time.Time
}

var (
    ErrNotFound       = errors.New("resume: not found") // D17: also "not yours"
    ErrCapExceeded    = errors.New("resume: user resume cap exceeded")
    ErrTitleTooLong   = errors.New("resume: title exceeds 160 characters")
)

const MaxTitleCharacters = 160 // budgets.md; Unicode code points

type RevisionMismatchError struct {
    CurrentRevision int64
    Current         Resume // for the 412 body P2B must return (spec §4)
}
func (e *RevisionMismatchError) Error() string

type Store struct {
    pool *store.Pool
    q    *store.Queries
    proj *docmigrate.Projector // Task 8; identity projection until then
    now  func() time.Time
}
func NewStore(pool *store.Pool, proj *docmigrate.Projector) *Store

// Create validates doc, then in one tx: LockUserForResumeWrite (spec's
// FOR UPDATE), CountResumesForUser >= 3 → ErrCapExceeded, else insert.
// Title validation runs before opening the transaction and defensively in the
// tx-scoped core; empty is allowed and 161 Unicode code points fail closed.
// The D7 trigger backstops it; a 23514 'resumes_user_cap_exceeded' from
// the insert also maps to ErrCapExceeded. Thin wrapper (B7): begin tx,
// build qtx := s.q.WithTx(tx), call createTx, commit.
func (s *Store) Create(ctx context.Context, userID uuid.UUID, title string, doc schema.Resume) (Resume, error)

func (s *Store) Get(ctx context.Context, userID, id uuid.UUID) (Resume, error)
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Resume, error)
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error

// SaveDocument is the CAS write: ValidateForStore, then
// UpdateResumeDocumentCAS at schema_version = docmigrate.CurrentVersion.
// 0 rows → re-read inside the same tx: absent → ErrNotFound; present →
// *RevisionMismatchError carrying the current (projected) doc + revision.
// Thin wrapper (B7) around saveDocumentTx — see below.
func (s *Store) SaveDocument(ctx context.Context, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (newRevision int64, err error)

// Thin wrapper (B7) around saveTitleTx — see below.
func (s *Store) SaveTitle(ctx context.Context, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error)

// createTx / saveDocumentTx / saveTitleTx (B7, owner ruling): the tx-scoped
// cores. Each takes an already-open *store.Queries (qtx) and does its
// writes on it, performing NO transaction management of its own — no
// Begin/Commit/Rollback. The pool-based Create/SaveDocument/SaveTitle above
// are the only callers that open a tx around them for the common case. This
// split exists so Task 7's IdempotencyStore.Execute can compose its mutate
// closure with the REAL cap-check/CAS logic inside its own transaction
// (mutate(qtx) calling s.createTx(ctx, qtx, …) etc.), instead of
// reimplementing the cap check or the CAS predicate a second time.
func (s *Store) createTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, doc schema.Resume) (Resume, error)
func (s *Store) saveDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (newRevision int64, err error)
func (s *Store) saveTitleTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error)
```

Task ordering note: `docmigrate.Projector` is Task 8; to keep tasks
independently landable, Task 6 defines a minimal
`docmigrate.NewIdentityProjector()` stub in `docmigrate/docmigrate.go` (current
version passthrough, `CurrentVersion = 1`) that Task 8 completes — declared here
so file ownership stays disjoint-by-time, not overlapping.

- [x] **Step 1: failing happy-path integration tests** (all via
      `testutil.RequireMigratedTestDatabaseURL`, table-driven): create → get
      round-trip (doc byte-stable through codec; revision 1; defaults
      live=false/download=true/seo=false); list ordering stable
      (`created_at, id`); delete → `ErrNotFound` on re-get; get/delete with the
      wrong user → `ErrNotFound` (D17 — the other user's row untouched, assert
      full-row equality before/after).
- [x] **Step 2: failing cap tests.** 3 creates succeed, 4th → `ErrCapExceeded`;
      delete one → create succeeds again; a second user is unaffected. (The
      N-way concurrency race is Suite A's, Task 9 — the author writes the
      sequential cases only; do not pre-empt the blind suite.)
- [x] **Step 3: failing CAS tests.** Save with correct revision → revision 2,
      doc updated; stale revision → `*RevisionMismatchError` with current
      revision + current doc (assert the doc is the _winning_ content); unknown
      id → `ErrNotFound`; invalid doc → `*ValidationError`, row untouched
      (full-row comparison — validation must run before any write); `SaveTitle`
      same matrix.
- [x] **Step 4: implement; green.** Implement the `…Tx` cores first (B7:
      `createTx`/`saveDocumentTx`/`saveTitleTx` — no tx management inside them),
      then `Create`/`SaveDocument`/`SaveTitle` as thin begin-tx/`WithTx`/commit
      wrappers around them; re-run Steps 1–3's tests unmodified against the
      wrapper form (they must still pass — the split is an internal refactor,
      not a behavior change). pgx error mapping via
      `pgconn.PgError{Code: "23514", Message: "resumes_user_cap_exceeded"}`
      (exact match on both — D7).
- [x] **Step 5: gate (dev-loop evidence, not phase-exit evidence — B11).**
      `make test-db-up && make server-test-db` — note `internal/resume` is not
      yet in that target's package list; the Makefile handoff (Integration
      handoffs table; owner applies it once this task lands, formally reported
      in Task 12) is what turns this into phase-exit evidence. Until it lands,
      ALSO run
      `cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL=postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable go test ./internal/resume/... -race -count=1 -v`
      and record the non-skipped case tally as **interim** gate evidence (P1's
      exit-criteria convention) — per the owner's B11 ruling this local
      invocation is never a substitute for the landed Makefile edit plus a green
      CI run at phase exit.
- [x] **Step 6: commit** —
      `git commit -m "feat(resume): add resume store with cap enforcement and revision CAS" -- apps/server/internal/resume`

---

### Task 6a: Preserve a cleared contact value through validation and live writes (AC-DOC-009)

The traceability row still records this specific draft-permissive case as
missing. Close it before v1's released artifacts are declared complete.

**Files:** create `packages/schema/fixtures/draft-cleared-contact-value.json`;
modify the explicit Go/TS schema/store-validation tests and
`apps/server/internal/resume/{codec_test.go,store_test.go}`.

- [ ] **Step 1: failing shared-contract tests.** The fixture contains one valid
      contact/detail entry whose `value` is `""`; JSON Schema and both aggregate
      validators accept it, and Go/TS decoding preserves the entry rather than
      treating it as absent.
- [ ] **Step 2: failing codec/live-store tests.** Parts→document→parts preserves
      the present cleared contact exactly. Create, Get, SaveDocument, and Get
      against live Postgres preserve the item and empty value without
      fabricating a sentinel or dropping the array element.
- [ ] **Step 3: implement the smallest fixture/test wiring; green.** No schema
      rule changes are expected; a red production path is routed back to its
      owning implementation author.
- [ ] **Step 4: gate.** `make schema-check`; focused live resume tests with
      `-race -count=1`; `make server-build server-vet server-test`.
- [ ] **Step 5: independent defect review, then commit** the fixture and its
      explicit conformance/live-write tests.

---

### Task 6b: Mechanically restrict generated resume writes to the domain store

Closes owner correction 5's unenforced choke-point convention before phase exit.
This is an integration-owner task because it touches root policy/gates.

**Files:** modify `.semgrep.yml`, the root `Makefile`, and
`.github/workflows/ci.yml`; create `scripts/test-resume-write-chokepoint.sh`. Do
not edit generated sqlc code.

- [ ] **Step 1: failing policy regression.** The script creates temporary Go
      files (never tracked): an outside `apps/server/internal/api` caller of
      each forbidden generated method must initially pass, proving the gap; the
      same calls under `internal/resume` are the allowed control.
- [ ] **Step 2: add a project Semgrep rule** covering `CreateResume`,
      `DeleteResumeForUser`, `UpdateResumeDocumentCAS`, `UpdateResumeTitleCAS`,
      `BackfillResumeDocumentCAS`, `CreateIdempotencyRecord`,
      `DeleteIdempotencyRecordIfExpired`, and
      `DeleteExpiredIdempotencyRecordsForUser`, plus the lock-bearing
      `LockUserForResumeWrite`. Include `apps/server/**/*.go`; exclude only
      generated definitions in `internal/store/**` and authorized calls in
      `internal/resume/**`. Method-name additions in `queries.sql` must extend
      this list in the same reviewed change.
- [ ] **Step 3: make the regression executable.** Add an owner-applied
      `semgrep-policy-test` target that asserts the temporary outside fixture
      fails with the new rule and the inside fixture passes; leave no temporary
      file behind. The script also parses named blocks in `sql/queries.sql` and
      fails if any `INSERT`/`UPDATE`/`DELETE` targeting `resumes` or
      `idempotency_records`, or the resume-user `FOR UPDATE` lock, is absent
      from the rule's covered-method manifest. Run it in CI beside the offline
      Semgrep gate.
- [ ] **Step 4: gate.** `make semgrep-policy-test semgrep`; fresh repository
      scan proves no outside production/test caller; `make docs-lint` if policy
      documentation changes.
- [ ] **Step 5: independent security/defect review, then commit** only the
      policy, regression script, Makefile, and CI wiring.

---

### Task 7: Idempotency record store (replay / reject / rollback primitive)

Closes the store primitive in AC-SAVE-003 and supplies the substrate for
AC-SAVE-002, whose HTTP behavior P2B closes. Implements D11. Also the forward
contract from `phase-1-deferred.md`: the client's `csrf_rejected` retry **reuses
the same `Idempotency-Key`** — this primitive is what makes that retry safe
(same key + same body ⇒ replay, never a double mutation); record that sentence
in the package doc so P2B/P4 inherit it as written contract, not accident.

**Files:** create `apps/server/internal/resume/idempotency.go`,
`idempotency_test.go`.

**Interfaces.** Produces:

```go
package resume

const IdempotencyTTL = 24 * time.Hour // D11; flagged for review

type StoredResponse struct {
    Status int
    Body   json.RawMessage
}

var ErrIdempotencyKeyReuse = errors.New(
    "resume: idempotency key reused with a different request body")

type IdempotencyStore struct {
    pool *store.Pool
    q    *store.Queries
    now  func() time.Time
}

// Execute serializes all of a user's mutation transactions on the existing
// user-row lock before the live-key lookup. For concurrent committed same-key
// calls, only the leader invokes mutate; a follower replays or rejects the
// committed record. mutate may run again only after an earlier attempt rolled
// back or crashed, so it MUST perform every database write through the supplied
// qtx and MUST NOT perform non-transactional side effects. Flow (D11): committed
// preflight { reap this user's expired rows }; tx { lock user; delete-if-expired
// same-key row; lookup live record: matching hash → replay, different hash →
// key reuse, absent → mutate then insert record }. The unique constraint
// remains a fail-closed backstop.
func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
    route string, key uuid.UUID, bodyHash [32]byte,
    mutate func(qtx *store.Queries) (StoredResponse, error),
) (resp StoredResponse, replayed bool, err error)
```

- [x] **Step 1: failing sequential tests.** First call runs `mutate`, stores +
      returns response; second call same key+hash → replayed=true, returns bytes
      identical to the persisted PostgreSQL `jsonb` representation, `mutate` NOT
      invoked (spy counter), no new row. Because PostgreSQL normalizes `jsonb`,
      the first mutation result and replay must be JSON-semantically equivalent;
      byte identity is required between the stored row and every replay, not
      between an arbitrary caller-formatted first body and normalized storage;
      same key different hash → `ErrIdempotencyKeyReuse`, `mutate` not invoked,
      zero writes; `mutate` returning an error → nothing persisted (record row
      absent, mutation rolled back); expired record (injected clock past TTL) +
      same key → treated as fresh: old row replaced, new execution. Seed an
      unrelated expired record and prove the committed preflight removes it even
      when the current attempt ends in key reuse or a mutation error.
- [x] **Step 2: failing real-CAS convergence tests.** Two same-key callers start
      from the same resume revision and each callback invokes Task 6's real
      transaction-scoped SaveDocument core. Before the fix, the loser callback
      ran and surfaced `RevisionMismatch`; after locking, a same-hash follower
      skips its callback and replays the winner, while a different-hash follower
      skips its callback and returns `ErrIdempotencyKeyReuse`. Both cases prove
      exactly one document mutation and one idempotency record commit. Ordinary
      callback-error rollback remains covered by Step 1.
- [x] **Step 2b: failing composition test (B7).** A separate case runs two
      `Execute` calls with different idempotency keys for the same user, each
      `mutate` calling `createTx`, back to back — both resumes exist, cap
      accounting is correct (this is `resume.Store`'s real cap check running
      inside `IdempotencyStore`'s tx, not a bypass), and a call that would be a
      4th resume for that user still surfaces `ErrCapExceeded` from inside
      `mutate`, which `Execute` propagates without inserting an idempotency
      record (nothing to replay for a rejected mutation).
- [x] **Step 3: implement; green.** Injected `now` for TTL; SHA-256 is the
      caller's job (P2B hashes the raw body — keep the primitive
      transport-agnostic).
- [x] **Step 4: gate.** Same live-DB command + tally as Task 6 Step 5.
- [x] **Step 5: initial implementation commit** —
      `git commit -m "feat(resume): add transactional idempotency record store" -- apps/server/internal/resume`
- [x] **Review follow-up 1:** first independent review found the callback
      exactly-once overclaim and rollback-prone expiry reap.
- [x] **Review follow-up 2:** a fresh author added committed preflight reaping,
      error-path tests, and the transaction-only callback contract.
- [x] **Review follow-up 3:** fresh re-review confirmed those fixes but found
      that a concurrent real CAS callback can fail before the unique-insert
      replay path, so callers do not converge.
- [x] **Review follow-up 4a:** a fresh author serializes contenders before
      lookup/mutate and adds real tx-scoped CAS race coverage.
- [x] **Review follow-up 4b:** a new independent reviewer passes the result; the
      integration owner reruns the focused race test at `-race -count=10`.
- [x] **Review follow-up 4c:** synchronize the owner plan, commit the corrective
      diff (`22169e8`), and integrate the reviewed checkpoint (`5805ddc`).

---

> **Owner correction 4 (2026-08-03) — `Project` returns bytes, not
> `schema.Resume`.** Task 6's review found that the original typed-return
> signature forced `docmigrate` to decode, which forced either importing
> `resume` (an import cycle) or duplicating the decoder — and the duplicate that
> shipped used plain `json.Unmarshal`, dropping `DisallowUnknownFields` and the
> trailing-data checks that `DecodeParts` applies. That left `DecodeParts` with
> **zero production callers**, made Task 5's strict-decode suite guard dead
> code, and meant a stored part carrying a field the current Go struct does not
> declare would be silently dropped on read and then persisted lossily by the
> next `SaveDocument` — precisely the read/write disagreement strict decoding
> exists to prevent.
>
> The cycle was an artifact of the signature, not of the requirement. With
> `Project` returning parts, `docmigrate` imports nothing from `resume`, there
> is no duplicate decoder, and `internal/resume` keeps a single strict decode at
> the boundary. This also **restores consistency with D13**, which already says
> converters are `func(json.RawMessage) (json.RawMessage, error)` over the full
> assembled document precisely because "typed structs only exist for the current
> version" — a converter chain lifting a v1 document cannot decode it into the
> current Go type at all. Task 8 assembles, runs the chain over the full
> document bytes, and re-splits into the three parts (D4's own decomposition);
> the caller decodes once, strictly.

### Task 8: Doc-shape migration machinery — projection-only read, CAS write persistence, CAS backfill, wire-version declarations

Implements AC-DOC-010 and AC-DOC-012: the spec §3 doc-migration behavior and
wire-version machinery ("built in P2A … before a second version exists"). Task
2b is a hard prerequisite. **D12(ii) binding:** `docmigrate.go`'s package doc
records, verbatim, that every write path must persist the full document through
the codec — never a granular `jsonb_set`-style PATCH, which would let old-shape
content re-enter storage where the backfill CAS cannot see it. This is P2B's
binding-in-writing condition from D12; Task 12 forwards the sentence to the
owner alongside the other P2B forward-binding notes (as Task 7 does for the
idempotency retry contract).

**Files:** create
`apps/server/internal/resume/docmigrate/{docmigrate.go,backfill.go}`,
`docmigrate_test.go`, `backfill_test.go`, `export_test.go`; modify
`apps/server/internal/resume/store.go` (Get/List call `Projector.Project`;
SaveDocument persists at `CurrentVersion` — completing Task 6's stub).

**Interfaces.** Produces:

```go
package docmigrate

const CurrentVersion int32 = 1

// AcceptedVersions and EmittedVersions are distinct declared sets. With one
// released version both are {1}; a future release changes them deliberately.
// Callers receive copies so they cannot mutate the production declaration.
func AcceptedVersions() []int32
func EmittedVersions() []int32

// ConvertFunc converts one FULL canonical document by exactly one version.
type ConvertFunc func(doc json.RawMessage) (json.RawMessage, error)

// AdjacentConverters is keyed by its lower version N and supplies N→N+1 and
// N+1→N. Both functions are mandatory for every registered pair (D13).
type AdjacentConverters struct {
    Up   ConvertFunc
    Down ConvertFunc
}

// ValidateFunc validates one released-version document against that version's
// immutable schema. Production validators come from schema.RawSchemas.
type ValidateFunc func(doc json.RawMessage) error

type Projector struct { /* pairs, validators, accepted, emitted, current */ }

func NewProjector(pairs map[int32]AdjacentConverters,
    validators map[int32]ValidateFunc, accepted, emitted []int32,
    current int32) (*Projector, error)
func NewIdentityProjector() *Projector // production v1: no adjacent pairs

// Convert validates the source, walks adjacent pairs in either direction,
// validates each target, and fails closed on an unknown/undeclared version,
// missing direction, invalid converter result, or unavailable schema.
func (p *Projector) Convert(doc json.RawMessage, from, to int32) (json.RawMessage, error)

// AcceptWire projects a declared accepted wire version to CurrentVersion.
// EmitWire projects a current document to a declared emitted version.
func (p *Projector) AcceptWire(doc json.RawMessage, version int32) (
    current json.RawMessage, currentVersion int32, err error)
func (p *Projector) EmitWire(doc json.RawMessage, version int32) (json.RawMessage, error)

// Project is PURE (D18): stored parts+version in, current-version parts out.
// It never touches the database or decodes into schema types; internal/resume
// owns the one strict current-version decode (owner correction 4).
func (p *Projector) Project(personalDetails, content, customization json.RawMessage,
    storedVersion int32) (pd, c, cu json.RawMessage, err error)
```

```go
package resume // backfill lives with the store (needs Queries + validation)

type BackfillResult int
const (
    BackfillApplied BackfillResult = iota
    BackfillSkippedCurrent   // already at CurrentVersion
    BackfillLostRace         // observation stale (revision or version moved
                              // since read); no write occurred — RETRYABLE:
                              // re-observe and call BackfillOne again (B6).
                              // Never terminal; the row may still be behind.
)

// BackfillOne: read (version, revision, parts) → Project → validate →
// BackfillResumeDocumentCAS (WHERE id AND schema_version=$old AND
// revision=$observed — the spec's exact predicate). Revision and
// updated_at unchanged (D12). BackfillLostRace means the caller's
// observation went stale between read and CAS (e.g. a concurrent autosave
// OR a title-only write that bumps revision without touching
// schema_version) — it is a retry signal, not "row already current"; the
// (future) background job must re-observe schema_version and retry, not
// treat it as done. ListResumeIDsBelowSchemaVersion pages candidates for
// that job; the job itself is not built here — no scheduler exists until
// PI/P8 infrastructure.
func (s *Store) BackfillOne(ctx context.Context, id uuid.UUID) (BackfillResult, error)
```

- [ ] **Step 1: failing conversion/projection tests.** Identity v1 conversion
      and projection are byte-stable. With injected synthetic schemas and pairs,
      test `1→2`, `2→1`, `1→2→3`, and `3→2→1`; every step validates both its
      source and output. Constructor and conversion fail closed for a missing
      `Up` or `Down`, missing schema validator, unknown/undeclared version,
      invalid source, invalid JSON output, or output invalid for the target
      schema. Returned accepted/emitted slices cannot mutate internal state.
      **Projection purity:** run `Get` against a live row seeded at a synthetic
      old version, assert the returned doc is projected and the row's bytes,
      `revision`, and `updated_at` are bit-identical before/after (D18).
- [ ] **Step 2: failing old-client preparation and emission tests.** Against a
      synthetic current-v2 projector, `AcceptWire` accepts a v1 document and
      returns canonical target-validated v2 bytes plus the current version;
      `EmitWire` converts those same bytes to declared v1, validates immutable
      v1, and proves round-trip preservation of all v1 fields. Undeclared
      input/output versions and lossy conversion fail closed. This is the exact
      transport-agnostic boundary P2B consumes. P2B-owned AC-SAVE-004 adds the
      real HTTP/OpenAPI convert→full-document persist→emit proof; P2A does not
      invent a fake v2 store codec or bypass the typed v1 store to simulate it.
- [ ] **Step 3: failing backfill tests.** Old-version row → `BackfillApplied`:
      `schema_version` now current, parts rewritten, **revision and updated_at
      unchanged** (D12); already-current → `BackfillSkippedCurrent`, zero
      writes; stale observation (bump revision between read and CAS via a second
      connection) → `BackfillLostRace`, row untouched; backfill of a row whose
      projection fails validation → error, no write (a corrupt doc must surface,
      not silently persist). **D12(i), the actual proof of the argument:**
      `Get(id)` called immediately before and immediately after a successful
      `BackfillOne` on the same row returns **byte-identical** projected
      documents (marshal both, compare bytes) — this is the assertion the
      owner's ruling names as what makes "nothing observable changes" more than
      a claim. **B6 — title-only write causes a retryable, non-terminal lost
      race:** seed an old-version row; read it (observe `schema_version=vOld`,
      `revision=R`); call `SaveTitle` (which touches only `title` and bumps
      `revision` to `R+1`, never `schema_version`) between the read and the CAS;
      `BackfillOne` using the stale observation → `BackfillLostRace`, and the
      row's `schema_version` is **still `vOld`** (unlike the concurrent-autosave
      case, the row is not now current) — proving the result is a retry signal,
      not "already done"; a second `BackfillOne` (fresh read) on the same row
      then succeeds with `BackfillApplied`.
- [ ] **Step 3c: a discriminating test for the strict decode on the read path
      (added 2026-08-03 after Task 6's re-review).** Task 6 restored
      `DecodeParts` to the read path, but nothing yet _fails_ if it is removed:
      swapping it back for a plain `json.Unmarshal` at `projectRow` leaves every
      test in the package green — the exact blind spot that let the lax
      duplicate ship in the first place. Insert a row, then
      `UPDATE resumes SET personal_details = personal_details || '{"unknownField":1}'`
      via direct SQL, and assert `Get` returns an error rather than silently
      dropping the field. Without this, the invariant is structural but
      unguarded.
- [ ] **Step 4: implement; green.** `Store.Get`/`List` now project;
      `SaveDocument` persists current version (Task 6's tests still green — run
      them). **Owner ruling:** `List` is fail-closed and atomic: if any row
      cannot be projected or decoded, return `nil` plus a deterministic error
      and expose no partial list. Add the mixed-valid/corrupt-row test; a silent
      omission or partial result would make corruption look like user deletion.
- [ ] **Step 5: gate.** Live-DB command + tally (Task 6 Step 5's form), plus
      `make server-build server-vet server-test schema-check`.
- [ ] **Step 6: commit** —
      `git commit -m "feat(resume): add doc-shape projection, CAS backfill, and wire-version declarations" -- apps/server/internal/resume`

---

### Task 9: Blind adversarial suite A — write-safety and cap concurrency matrix

Mandated by the master plan's independence rule for concurrency: a **second,
fresh Sonnet 5 instance** derives these from the written contracts **before
reading any `internal/resume` implementation diff or author test**. Inputs the
blind author gets: spec §3 (cap + invariants bullets), §4 (write-safety
paragraph), `budgets.md`, traceability AC-DOC-001/AC-SAVE-003 plus the
data-layer substrate portions of P2B-owned AC-SAVE-001/002, D11, and this plan's
**Interfaces blocks only** (Tasks 6–7 signatures + typed errors). Inputs
withheld: `internal/resume/*.go`, `store_test.go`, `idempotency_test.go`,
`sql/queries.sql`. The author of Tasks 5–8 must not edit this suite; weakening
any assertion requires Opus 5 review by name.

**Files:** create `apps/server/internal/resume/writesafety_adversarial_test.go`.

Minimum matrix (the blind author may add, never subtract):

| Test                                                   | Assert                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestCreate_Concurrent_ExactlyThreeSucceed`            | 20 concurrent `Create` for one user → exactly 3 succeed, 17 `ErrCapExceeded`, row count 3; deterministic under `-race -count=20`                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `TestCreate_RawSQLBypass_StillCapped`                  | 3 rows via store, 4th via raw `INSERT` → SQLSTATE 23514 (the trigger is the enforcement, not the Go code)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `TestCreate_ConcurrentRawSQLBypass_StillCapped`        | **The trigger's own `FOR UPDATE` under concurrency, added 2026-08-03 (owner).** Two raw-SQL connections, no store layer: tx1 inserts the 3rd resume and holds; tx2's 4th insert must **block** (poll `pg_locks`/`pg_stat_activity` until observed blocked — no `time.Sleep`); tx1 commits; tx2 then fails `23514`/`resumes_user_cap_exceeded`. Deleting the trigger's `PERFORM … FOR UPDATE` line must make this test fail. Task 3's review found that line had **zero** behavioral coverage — the store's own lock masks it in every store-mediated test, so only a bypassing concurrent writer exercises it |
| `TestSaveDocument_ConcurrentSameRevision_OneWinner`    | N concurrent CAS at revision R → exactly one new revision R+1; every loser gets `*RevisionMismatchError` whose `Current` equals the winner's doc                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `TestSaveDocument_MismatchCarriesWinningDoc`           | loser's error payload byte-matches a fresh `Get` (the 412-body contract P2B will serialize)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `TestIdempotency_ConcurrentSameKey_OneMutationCommits` | N concurrent `Execute` same key+hash → the user-row lock admits one callback; followers replay its committed response; exactly one mutation is observable; no callback has a non-transactional side effect                                                                                                                                                                                                                                                                                                                                                                                                    |
| `TestIdempotency_MutationErrorRollsBack`               | a callback that performs a real transaction-scoped resume mutation and then returns an error leaves neither that mutation nor an idempotency record                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `TestIdempotency_DifferentBodyNeverExecutes`           | reuse with a different hash: `ErrIdempotencyKeyReuse` and zero DB deltas, even interleaved with valid replays                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `TestValidation_RejectionWritesNothing`                | oversized/invalid doc through `Create` and `SaveDocument` → full-row/rowcount equality before vs after (limit+1 at the transaction boundary, not just the validator)                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `TestNoExistenceOracle_WrongUserSameAsNotFound`        | across `Get`, `Delete`, `SaveDocument`, `SaveTitle`: calling with a real id owned by a different user returns byte-identical `ErrNotFound` (or, for the CAS methods, the same not-found path — never a distinguishable `*RevisionMismatchError`) as calling with a wholly nonexistent id — no response-shape difference an attacker could use as an existence oracle (D17)                                                                                                                                                                                                                                    |

- [ ] **Step 1 (blind author): write the suite from the contracts; run** —
      expected mostly green if Tasks 5–8 are correct; **any red is a real
      finding** routed to the implementer (never fixed by the suite author).
- [ ] **Step 2: gate.** `make test-db-up`, then the Task 6 Step 5 live-DB
      command; concurrency cases additionally `-count=20`.
- [ ] **Step 3: commit** (blind author's own commit) —
      `git commit -m "test(resume): add adversarial write-safety and cap concurrency suite" -- apps/server/internal/resume/writesafety_adversarial_test.go`

---

### Task 10: Blind adversarial suite B — doc-migration purity and CAS-vs-autosave races

Same independence protocol as Task 9, **different fresh instance** (not Task 9's
author, not Tasks 5–8's author). Inputs: spec §3 doc-migrations bullet +
wire-version row, D12/D13/D18 as written contracts, Task 8's Interfaces block.
Withheld: all `internal/resume` implementation and author tests. This is the
master plan's named "**CAS-vs-autosave race tests**" obligation.

**Files:** create `apps/server/internal/resume/docmigrate_adversarial_test.go`.

Minimum matrix:

| Test                                             | Assert                                                                                                                                                    |
| ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestGet_NeverWrites`                            | hammer `Get`/`List` on an old-version row concurrently; row bytes, `revision`, `updated_at` unchanged throughout (projection-only, under concurrency)     |
| `TestBackfill_LosesToConcurrentAutosave`         | interleave: backfill reads (vOld, rev R) → autosave commits rev R+1 → backfill CAS → 0 rows, `BackfillLostRace`, autosave's doc intact at current version |
| `TestAutosave_AfterBackfill_NoSpurious412`       | backfill applies (revision unchanged, D12) → autosave with pre-backfill revision R still succeeds — the exact user-visible property D12 exists to protect |
| `TestBackfill_ConcurrentWithItself_AppliesOnce`  | N concurrent `BackfillOne` on one row → one `BackfillApplied`, rest skipped/lost; final state valid, revision unchanged                                   |
| `TestBackfill_NeverPersistsInvalidProjection`    | synthetic converter emitting an invalid doc → error, row untouched                                                                                        |
| `TestProjection_UnknownStoredVersionFailsClosed` | stored version with no converter path → error from `Get`, never a silently un-projected doc                                                               |
| `TestList_OneBadProjectionFailsAtomically`       | one unprojectable row among valid rows → `nil, err`; no partial list or silent omission                                                                   |
| `TestWireConverters_BothDirectionsFailClosed`    | independently exercise synthetic v1⇄v2, old-client preparation, down-emission, source/target validation, and every missing-path arm                       |

- [ ] **Step 1 (blind author): write from the contracts; run; findings to the
      implementer.**
- [ ] **Step 2: gate.** Live-DB command, `-race -count=20` on the race cases.
- [ ] **Step 3: commit** —
      `git commit -m "test(resume): add adversarial doc-migration and backfill race suite" -- apps/server/internal/resume/docmigrate_adversarial_test.go`

---

### Task 11: Blind adversarial suite C — independently-derived size-bound limit+1 matrix

Same independence protocol as Tasks 9–10, a **third fresh instance** — not the
author of Task 9, not the author of Task 10, and not Task 5's author. Mandated
by the owner's B13 ruling: Task 5's own `bounds_test.go` and its schema-walk
completeness guard are both written by the same author who writes `validate.go`
— a self-consistent harness can still share the author's blind spot about which
bounds matter or how they're phrased. An independent author deriving the same
limit+1 matrix from the budget and the frozen schema alone, without reading Task
5's implementation or its own test file, is the check against that.

**Inputs the blind author gets:** `budgets.md`, spec §3 (the size-bounds bullet
and the aggregate-invariant bullet), `packages/schema/resume.schema.json` (the
frozen schema itself, read directly for every `maxLength`/`maxItems`/
`maxProperties` declaration — not Task 5's inventory of them), and this plan's
Task 5 **Interfaces block only** (`ValidateForStore`, `AssembleCanonical`,
`MaxDocumentBytes`, `ValidationError` — signatures, not bodies). **Inputs
withheld:** `apps/server/internal/resume/bounds_test.go`,
`apps/server/internal/resume/validate.go`, and every other Task 5 implementation
file. The author of Task 5 must not edit this suite; weakening any assertion
requires Opus 5 review by name.

**Files:** create `apps/server/internal/resume/bounds_adversarial_test.go`.

Minimum matrix (the blind author may add, never subtract): one limit/limit+1
pair per bound found by independently walking `resume.schema.json` plus the
`budgets.md` 512 KB total-document bound — total doc bytes, section count,
entries-per-section, personal-details count, and each distinct `maxLength` class
in the schema — each asserting `ValidateForStore` accepts the doc at the limit
and rejects it at limit+1 with a `*ValidationError`. A disagreement between this
suite's independently-derived matrix and Task 5's own bounds inventory (a bound
this suite finds that Task 5's completeness guard didn't, or vice versa) is
itself a blocking finding for Opus review, not something either author
reconciles unilaterally.

- [ ] **Step 1 (blind author): derive the matrix from `budgets.md` / spec §3 /
      `resume.schema.json` / Task 5's Interfaces block only; write the suite;
      run it** — expected mostly green if Task 5 is correct; any red, or any
      bound this suite exercises that Task 5's harness doesn't, is a real
      finding routed to Task 5's implementer, never fixed by the suite author.
- [ ] **Step 2: gate.** Pure unit tests, no DB:
      `cd apps/server && go build ./... && go vet ./... &&     go test ./internal/resume/... -run BoundsAdversarial -count=1 -v`,
      then the full `go test ./internal/resume/... -count=1`.
- [ ] **Step 3: commit** (blind author's own commit) —
      `git commit -m "test(resume): add independently-derived adversarial bounds suite" -- apps/server/internal/resume/bounds_adversarial_test.go`

---

### Task 12: Traceability closure, docs, and integration handoffs

**Files:** modify `docs/plans/traceability.md` (claimed rows only) and the
current-state architecture/runbook references affected by the landed store;
integration-owner-only shared-file edits remain handoffs.

- [ ] **Step 1:** fill test references for AC-DOC-001 (trigger + store + Suite A
      tests), AC-DOC-002 (Task 2 conformance + Task 5 pipeline),
      AC-DOC-003/004/007/008/009 (append live-write, limit+1, and
      cleared-contact references to the existing P0 evidence), AC-DOC-010
      (projection/CAS/backfill + Suite B), AC-DOC-011 (canonical 512 KB
      boundary + Suite C), AC-DOC-012 (immutable v1/types, both converter
      directions, old-client preparation/emission), and AC-SAVE-003 (Task 7 +
      Suite A). Remove every stale "not yet wired" annotation.
- [ ] **Step 2:** hand the integration owner, in one report: (a) any owner-only
      CI/Makefile edit still required for `server-test-db`, immutable released
      schemas, or generated-write-method restriction; (b) the P2B
      forward-binding notes: D14 customization allowlist, the idempotency retry
      contract, D12(ii) full-document persistence, and the real HTTP/OpenAPI
      AC-SAVE-004 old-client persist/emission test that consumes Task 8 rather
      than reimplementing it; (c) the global P8 retention sweep remains additive
      to Task 7's opportunistic per-user reap.
- [ ] **Step 3: gate.** `make docs-fmt && make docs-lint`.
- [ ] **Step 4: commit** —
      `git commit -m "docs(plans): close Phase 2A traceability rows" -- docs/plans/traceability.md`

---

## Integration handoffs (owner-applied, not worker-applied)

| Shared file                          | Needed change                                                                                            | When          |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------- | ------------- |
| `.github/workflows/ci.yml`           | Add exact released-schema/type append-only job beside migrations; add Semgrep policy regression          | Tasks 2b/6b   |
| root `Makefile`                      | Retain landed resume DB coverage; add `semgrep-policy-test`                                              | Task 6b/gate  |
| `docs/plans/implementation-plan.md`  | Drift-gate wording correction (script → `cmd/migrate/gen`); phase-status row when gates pass             | Task 1 / gate |
| `.semgrep.yml` + policy script       | Restrict direct use of named generated write methods to `internal/resume` and prove the negative control | Task 6b       |
| P2B plan/OpenAPI tests               | Full-document writes plus old-client accept/project/persist/emit behavior                                | P2B planning  |
| root `go.work.sum` (if materialized) | commit as lockfile                                                                                       | whenever      |

## Phase exit criteria

- [ ] `cd apps/server && go build ./... && go vet ./... && go test ./...     -race`
      clean (hermetic; DB-backed cases self-skip). **Hard exit condition (owner
      ruling B11): the Makefile handoff has landed — `server-test-db`'s package
      list includes `./internal/resume/...` — and `make server-test-db`
      (equivalently, CI's server gate) is green _with that package list_.** A
      local ad-hoc
      `REQUIRE_TEST_DB=1 … go test ./internal/resume/... -race -count=1 -v`
      invocation (the interim tally recorded at each task's Step 5 during Tasks
      6–10, before the Makefile edit landed) is dev-loop evidence only and is
      **never** phase-exit evidence on its own — B11 is explicit that a local
      invocation cannot stand in for CI running the package for real.
- [ ] `make sqlc-check`, `make data-drift`, `make server-migration-test`,
      `make schema-check` all green at the phase head; `make data-drift`'s
      trigger cross-check demonstrated red once against a deliberately perturbed
      function body (evidence retained, perturbation reverted).
- [ ] Migrations `00004`/`00005` appended after head `00003`; no existing
      migration file or hash modified except `atlas.sum` through the pinned
      Atlas; the migration harness's four scenarios green over the new head.
- [ ] 4th-resume insert rejected by the **database** (raw-SQL test) and by the
      store; 20-way concurrent create yields exactly 3, under `-race`.
- [ ] Resume titles accept empty and exactly 160 Unicode code points, reject 161
      before any transaction or write, and leave the row unchanged on rejection.
- [ ] Every size bound in the named-bound matrix has a passing limit and failing
      limit+1 case, and the schema-walk completeness guard passes (no schema
      bound unexercised).
- [ ] Entry-id uniqueness enforced whole-resume in Go and TS against the shared
      fixture; `make schema-check` proves no generated drift.
- [ ] The cleared-contact-value fixture is valid in Go/TS and round-trips
      unchanged through live Create/Get/SaveDocument/Get, closing AC-DOC-009.
- [ ] CAS mismatch returns the current revision + winning doc; concurrent
      same-revision writers produce exactly one winner (Suite A green).
- [ ] Idempotency: replay returns the stored response without re-execution;
      same-key concurrent CAS calls invoke one callback and converge on replay
      or key reuse; different body rejected with zero loser writes; an ordinary
      callback error rolls back its mutation and record (Suite A green).
- [ ] Reads never write (purity test green under concurrency); backfill CAS
      loses cleanly to autosave; autosave after backfill does not 412; a
      title-only write between read and CAS also yields a retryable
      `BackfillLostRace` with `schema_version` still old, and a second
      `BackfillOne` then applies (Suite B green; B6).
- [ ] `resume.v1.schema.json` is immutable and byte-equal to the current v1
      source; generated raw-schema registries cover each released version;
      accepted/emitted declarations and adjacent `Up`/`Down` conversion are
      tested, including synthetic old-client preparation to the current
      canonical shape and supported-version emission. Real HTTP persistence is
      P2B's AC-SAVE-004 gate.
- [ ] Suite C (Task 11) independently derived the same size-bound limit+1 matrix
      from `budgets.md`/spec §3/`resume.schema.json`/Task 5's Interfaces block
      alone, without reading `bounds_test.go` or `validate.go`; any disagreement
      with Task 5's own matrix was resolved before phase exit, not waved
      through.
- [ ] All three blind suites (A, B, C) were authored by fresh instances from the
      written contracts before reading implementation diffs, and were not edited
      by any implementation author; Opus 5 reviewed every task diff, blocking
      findings fixed and re-reviewed.
- [ ] Fresh-cache `golangci-lint run ./...`, `govulncheck ./...`, and offline
      Semgrep are green; direct calls to the generated resume write methods are
      mechanically restricted to `internal/resume`.
- [ ] Traceability rows AC-DOC-001/002/003/004/007/008/009/010/011/012 and
      AC-SAVE-003 are closed at the phase commit; integration handoffs are
      applied or explicitly assigned with an owner and downstream gate.
- [ ] The integration owner completes a design/implementation consistency review
      and records any contract correction before the phase is frozen.
- [ ] A fresh adversarial reviewer challenges the phase's assumptions and
      tradeoffs after implementation; blocking findings are fixed and reviewed.
- [ ] A fresh UAT worker runs `uat-phase-2a.md` at the exact candidate commit,
      edits no product/test/criteria files, and reports no FAIL or BLOCKED rows.
- [ ] A separate evidence reviewer samples artifacts and reruns a deterministic
      subset at the same commit. Any later product-code change invalidates every
      affected UAT row and triggers a rerun.
