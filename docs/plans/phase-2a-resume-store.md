# Phase 2A — Resume domain & store (implementation plan)

> **Adopted Rev 2 (2026-08-02)** by the integration owner after an independent
> adversarial plan review (ADOPT WITH CHANGES, 13 blocking findings applied and
> audited). Acceptance rows `AC-DOC-010`, `AC-DOC-011`, `AC-DOC-012` and
> `AC-SAVE-003` are minted in `traceability.md`. Originally — revision after the
> independent adversarial review (ADOPT WITH CHANGES) and the integration
> owner's rulings. Changelog: D1 hardened (compiler conditions, verdict-parity
> corpus, rejected-alternative recorded), D2 → generated Go source constant,
> D5/D11 constants ratified (owner lands them in `budgets.md`), D6 →
> owner-authored ADR prerequisite, D11 + opportunistic reaping in-phase, D12
> argument replaced + two binding conditions, new D19 (schema validation is
> CurrentVersion-only; no `resume.v<N>.schema.json` convention yet), blind Suite
> C added (Task 11) + D17 row added to Suite A, serialized-artifact list
> extended for P3 collisions, `server-test-db`+CI made a hard exit criterion,
> traceability IDs minted by the owner at adoption. Also: D9 hardened (statement
> extraction scoped to `-- +goose Up` only; the normalization pipeline pinned as
> five ordered, individually-tested stages; `ALTER FUNCTION/TRIGGER`,
> `ALTER TABLE … {ENABLE|DISABLE} TRIGGER`, `PROCEDURE`, `RULE`, `POLICY` added
> to the unconditional-reject net); Task 3's DDL landing split into tables-first
> (Step 2a) then function+trigger+ `00005` together (Step 2b) — the only order
> Task 1's own gate permits; `BackfillLostRace` redefined as retryable
> everywhere it's described (comment, mermaid diagram, a new Task 8 test); Task
> 6 gained tx-scoped store cores (`createTx`/`saveDocumentTx`/`saveTitleTx`) so
> Task 7's `Execute` composes with the real `Create`; D12's two binding
> conditions now have an actual Task 8 test and a package-doc-recorded binding,
> not just prose; traceability closure renumbered Task 11 → Task 12 to make room
> for Suite C. Nothing here is authorized for execution until the plan is
> adopted into `docs/plans/` (where it must pass `make docs-fmt` /
> `make docs-lint`).
>
> **For agentic workers (once adopted):** execute with
> superpowers:subagent-driven-development, one task per fresh subagent, Opus 5
> defect review between tasks. Steps are `- [ ]`. Every task's tests are written
> **before** its implementation (TDD): write the failing test, run it and see it
> fail, implement, run it and see it pass, commit.

**Goal:** the resume domain's data layer — `resumes`, `slug_tombstones`, and
`idempotency_records` tables with every spec §3 constraint and the DB-enforced
3-resume cap trigger; a single validated store layer all resume writes pass
through (JSON-Schema bounds, aggregate invariants, entry-id uniqueness, date
ranges, byte-exact size bounds — every bound with a limit+1 test); revision CAS
write safety; the idempotency record primitive; and the doc-shape migration
machinery (projection-only on read, CAS on write, CAS backfill) — all proven
against a live Postgres via the shared migration-applying test helper. No HTTP
surface: P2B owns endpoints, media, and OpenAPI changes.

**Base:** `main`, commit `ad357d3` ("Merge Phase 1: authentication and
sessions", 2026-08-02). Workers must run `git rev-parse HEAD` and confirm their
worktree is at this base or a descendant before starting — worktree agents have
checked out stale commits before; verify, don't assume.

**Migration head this plan targets: `00003_add_sessions_rotated_from`.** P2A
migrations **append** (`00004…`) and never edit an existing migration file or
`atlas.sum` by hand outside the pipeline described in Task 3.
`docs/plans/phase-1-deferred.md` §P1.1 wants a bounded auth follow-up landed
"before P2A takes the migration head"; as scoped there it needs **no
migration**, but if the integration owner lands one first, this plan's migration
numbers shift by one and Task 3 rebases and re-runs `make server-migration-test`
— the plan text is otherwise unaffected. Schema-head changes are serialized
through the integration owner; one merges at a time.

**Spec:** `../specs/aboutme-design.md` §3 — the `resumes` and `slug_tombstones`
rows of the data-model table; "Relational constraints & store-layer invariants"
(all bullets); "Entry fields per sectionType"; "Optionality: draft-permissive,
publish-strict"; the aggregate invariant bullet; the doc-shape-migrations
bullet; "Schema management" (declarative pattern + the **wire-version
compatibility** row: "the machinery is built in P2A alongside doc migrations,
before a second version exists"); §4 "Write-safety" (revision CAS + idempotency
semantics — the HTTP surface itself is P2B). **Master plan:**
`implementation-plan.md` "Phase 2A — Resume domain & store" including the
carried drift-gate limitation note, plus "Global constraints", "Agent workflow",
"Integration discipline", "Testing strategy" (the Write-safety/concurrency row
is owned here). **Budgets:** `budgets.md` — request body ≤ 256 KB (P0B
middleware, already enforced, not re-implemented here) and **resume document
total ≤ 512 KB (P2A store)**. **Traceability:** claims and gaps in the table
below.

## Traceability rows claimed by this phase

| ID         | Statement                                                        | Claimed by      |
| ---------- | ---------------------------------------------------------------- | --------------- |
| AC-DOC-001 | Max 3 resumes per user, DB-enforced                              | Tasks 3, 6, 9   |
| AC-DOC-002 | Entry ids unique across the whole resume                         | Tasks 2, 5      |
| AC-DOC-003 | Date ranges (start ≤ end) — "not yet wired into live writes"     | Task 5 (wiring) |
| AC-DOC-007 | Rich text ≤ 16 KB UTF-8 bytes — "not yet wired into live writes" | Task 5 (wiring) |
| AC-DOC-008 | Layout exactly-once aggregate — "not yet wired into live writes" | Task 5 (wiring) |

**Rows this scope needs that do not exist.** Per the owner's B12 ruling, the
integration owner **mints the four IDs at plan adoption**; Task 12 then only
fills their test references. Until adoption they are referenced below as
`AC-P2A-NEW-1…4` placeholders (replace at adoption; never commit a placeholder
to `traceability.md`):

- **AC-P2A-NEW-1** — Doc-shape migration is projection-only on read / CAS on
  write / CAS backfill (spec §3). The master plan's "Spec traceability" section
  _says_ "doc-migration CAS race (P2A)" is assigned, but `traceability.md` has
  no such row — a master-plan/traceability disagreement to repair together.
- **AC-P2A-NEW-2** — Resume document total ≤ 512 KB enforced at the store
  (budgets row exists; no AC row).
- **AC-P2A-NEW-3** — Wire-version compatibility machinery (spec §3 "Schema
  management" table row) — accepted/emitted version declarations + converter
  registry.
- **AC-P2A-NEW-4** — Idempotency **store primitive** semantics
  (replay/reject/rollback at the data layer). AC-SAVE-002 exists but is
  P2B/HTTP-scoped; the owner may extend its row or mint this one.
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
  (fails closed on a missing DSN) and passes `-race -count=1`. **Its package
  list is currently
  `./internal/auth/... ./internal/store/... ./internal/user/...` and does not
  include `./internal/resume/...`** — the Makefile is an integration-owner-only
  file, so Task 12 reports the exact one-line change instead of editing it (see
  "Integration handoffs").
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

| D7 | Trigger mechanics: `AFTER` vs `BEFORE`, race-safety, error surfacing |
`BEFORE INSERT OR UPDATE OF user_id` trigger whose function **first locks the
owner row** (`PERFORM 1 FROM users WHERE id = NEW.user_id FOR UPDATE`) then
counts — the trigger alone is race-proof even for writers that bypass the store;
the store's create tx takes the same user-row `FOR UPDATE` first anyway (spec:
belt and suspenders; identical lock order, no deadlock). Cap violation raises
`ERRCODE 'check_violation'` (23514) with `MESSAGE 'resumes_user_cap_exceeded'`;
the store maps exactly that (code + message) to `ErrResumeCapExceeded` | | D8 |
Migration authorship for DDL Atlas community silently drops | Two files:
`00004_add_resume_tables.sql` **generated** by `make migrate-gen`
(tables/constraints/indexes — all Atlas-diffable), then
`00005_add_resume_cap_trigger.sql` **hand-written** goose SQL (function +
trigger), following `00001_extensions.sql`'s precedent; `atlas.sum` refreshed
via the pinned Atlas `migrate hash` (Task 3). Task 1's cross-check is what makes
the hand-written file drift-safe | | D9 | The drift-gate extension's shape
(master plan: keyword-reject → "real cross-check") | In `cmd/migrate/gen`: (a)
broaden the keyword net to modifier variants (`MATERIALIZED`, `CONSTRAINT`,
`TEMP`/`TEMPORARY`, `UNLOGGED`, `RECURSIVE`); (b) for **FUNCTION and TRIGGER
only** (the classes P2A introduces), replace unconditional rejection with a
statement-level cross-check: whitespace-normalized
`CREATE [OR REPLACE] FUNCTION/TRIGGER` statements declared in `schema.sql` must
each match the **last** occurrence of that object's statement across ordered
migrations, and every such migration statement's name must exist in
`schema.sql`; any `DROP FUNCTION/TRIGGER` in a migration stays rejected until a
phase needs it. VIEW/SEQUENCE/materialized/temp/unlogged/recursive remain
unconditionally rejected, per `checkNoUndiffableObjects`'s recorded intent
("extend … for the object it introduces") | | D10 | Where the ≤ 512 KB
total-document bound is measured | On the **canonical assembled JSON** (the
marshaled full document including the injected `schemaVersion`), byte length ≤
`512 * 1024`. Deterministic, independent of jsonb's internal representation, and
covers growth via future granular PATCHes that the 256 KB request-body cap
cannot (a doc can legally exceed one request's size) | | D11 | Idempotency
record shape and semantics (spec gives keying + replay/reject, not mechanics) |
Table `idempotency_records`, `UNIQUE (user_id, route, idempotency_key)`,
`request_hash` = SHA-256 of the raw request body, stored `response_status int` +
`response_body jsonb`, `expires_at` = `created_at` + **24 h TTL (ratified
2026-08-02; owner lands it in `budgets.md`)**. Execution contract: run the
mutation and insert the record **in one tx**; a concurrent duplicate hits the
unique index at commit, **rolls back its entire tx** (mutation included), then
re-reads: hash match → replay stored response; hash mismatch →
`ErrIdempotencyKeyReuse`, no write. Expired rows are deleted-then-reinserted
in-tx (treated as absent). **Retention (owner ruling — not deferred):
`response_body` holds user content, materially unlike `oauth_transactions`, so a
TTL nothing enforces is not a TTL. P2A adds opportunistic reaping — every
`Execute` deletes the calling user's expired rows in the same tx before
inserting** (Task 7); a global sweep can still join P8-priv's jobs later | | D12
| Backfill CAS: does persisting a doc-shape migration bump `revision`? | **No**
(owner-upheld, with the argument replaced per the review). The decisive reason:
under D18 every read is projected to `CurrentVersion`, and under D16 every write
re-serializes the whole document — so a backfill changes storage to something
**byte-identical to what every reader was already being served**. Nothing
observable changes; there is nothing to signal, so `revision` (and `updated_at`)
must not move. Guarded by
`WHERE id=$1 AND schema_version=$old AND revision=$observed`. Two attached
conditions, both mandatory: **(i)** Task 8 asserts `Get(id)` returns a
**byte-identical projected document before and after `BackfillOne`** — that
assertion is the actual proof of the argument; **(ii)** P2B is **bound in
writing** (Integration handoffs + `internal/resume` package doc): every write
path must persist the full document through the codec — a granular
`jsonb_set`-style PATCH would let old-shape content re-enter storage where the
backfill CAS cannot see it | | D13 | Converter representation when only v1
exists | Converters are `func(json.RawMessage) (json.RawMessage, error)` over
the full assembled doc (version-agnostic — typed structs only exist for the
current version), held in a `Projector` built by constructor injection.
Production wiring passes an **empty** table + `CurrentVersion = 1`; tests inject
synthetic converters to prove the machinery. Accepted/emitted version
declarations (spec §3 wire-version row) are exported constants/functions with
tests, so P2B's content negotiation and any future v2 build on a written
contract | | D14 | "Customization delta paths from a fixed allowlist" — store
layer or request layer? | Deferred to **P2B** with the `PATCH …/customization`
endpoint: the allowlist bounds _request shape_ (delta paths), while the stored
aggregate is already fully validated here on every write (schema
`additionalProperties: false` + bounds via D1). Flagged as a scope boundary, and
as a traceability gap (see above) so P2B cannot silently drop it | | D15 |
`slug_tombstones` columns vs the repo's surrogate-PK convention; re-release
semantics | Surrogate `id uuid PRIMARY KEY DEFAULT uuidv7()` + `UNIQUE (slug)`
(conventions: natural key = UNIQUE),
`released_by_user_id uuid NULL REFERENCES users ON DELETE SET NULL`
(spec-mandated), `released_at timestamptz NOT NULL DEFAULT now()`. **No
`expires_at` column** — the 180-day window is claim-time logic owned by P5A;
storing a precomputed expiry would fork that constant. P2A ships the table +
constraint tests only; tombstone _queries_ (incl. the re-release upsert) are
P5A's contract to define — not invented here | | D16 | Store validation entry
point: raw bytes or typed value? | The store's write API takes a typed
`schema.Resume`; the store **re-marshals to canonical JSON** and runs the full
pipeline (D1 schema validation → size bound → `schema.ValidateDocument` →
entry-id uniqueness) on that canonical form. One choke point regardless of how a
caller decoded its input; strict decode (`DisallowUnknownFields`) remains the
ingress guard for P2B | | D17 | Ownership scoping at the data layer | Every
per-resume query is keyed `WHERE id = $1 AND user_id = $2` (`GetResumeForUser`,
`DeleteResumeForUser`, CAS updates) — an authorization mistake in P2B cannot
reach another user's row through this layer; "not yours" and "not found" are the
same `ErrNotFound` (no existence oracle, matching DD-C5's reasoning) | | D18 |
Doc reads of not-yet-backfilled rows | `Get`/`List` return the doc **projected
to `CurrentVersion`** (pure, never writes — verified by test asserting row bytes
and `revision`/`updated_at` unchanged after a read) plus the row's stored
`schema_version` so callers can observe backfill progress. The renderer contract
(§5: "renderer handles current schema_version only; server projects first") is
what this satisfies | | D19 | `resume.schema.json` pins
`schemaVersion: {"const": 1}` — what does JSON-Schema validation mean for a
not-yet-current stored doc, and does P2A establish the spec's immutable
`resume.v<N>.schema.json` file convention? (owner ruling on B5, option a) |
**`ValidateForStore` validates at `CurrentVersion` only.** Its input is always a
document the store is about to persist at `CurrentVersion` (D16 create/save) or
a projector output already lifted to `CurrentVersion` (D18 reads, Task 8
backfill) — so the single embedded schema is, by construction, always the right
one, which is why D2's one embedded constant is sufficient. A projector's
**pre-current intermediate output is explicitly outside the schema-validation
path**; the seam that proves this is Task 8's synthetic-converter tests
(`NewProjector` injection in `docmigrate/export_test.go`), which exercise stored
versions the schema would reject. The `resume.v<N>.schema.json` per-version
immutable-file convention is **deliberately not established now**: there is no
version 2 to serve, and inventing the file convention before a real second
version exists is speculative contract-writing — it is recorded here as the
decision the first v2 change must make, alongside extending `AcceptedVersions()`
|

## File structure produced by this phase

| File                                                                                                                  | Responsibility                                                                                                                |
| --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `apps/server/cmd/migrate/gen/main.go` (modify) + tests                                                                | D9 drift-gate extension (Task 1)                                                                                              |
| `packages/schema/validation/store.ts`, `gen/go/store_validate.go` (+tests) (modify)                                   | Entry-id uniqueness validator, both halves (Task 2)                                                                           |
| `packages/schema/scripts/generate.mjs` (modify), `gen/go/rawschema.go` (generated)                                    | D2 raw-schema Go source constant (Task 2); byte-compare coverage via existing `packages/schema/test/gen.test.ts`              |
| `packages/schema/fixtures/bounds/` (generated corpus + `manifest.json`), `packages/schema/test/bounds-parity.test.ts` | D1(e) cross-language verdict-parity corpus: ajv and jsonschema/v6 must agree on every fixture + bounds document (Tasks 5, 11) |
| `apps/server/sql/schema.sql` (append)                                                                                 | `resumes`, `slug_tombstones`, `idempotency_records`, cap function + trigger (Task 3)                                          |
| `apps/server/migrations/00004_add_resume_tables.sql` (generated)                                                      | Tables/constraints/indexes (Task 3)                                                                                           |
| `apps/server/migrations/00005_add_resume_cap_trigger.sql` (hand-written) + `atlas.sum`                                | Function + trigger DDL Atlas cannot diff (Task 3)                                                                             |
| `apps/server/migrations/resume_schema_test.go`                                                                        | Migrated-DB constraint/trigger existence + behavior tests (Task 3)                                                            |
| `apps/server/sql/queries.sql` (append), `apps/server/internal/store/*.go` (regenerated)                               | sqlc queries + generated types (Task 4)                                                                                       |
| `apps/server/internal/resume/{resume.go,codec.go,validate.go,store.go}` + tests                                       | Domain type, codec (D4), validation pipeline (D16), store API (Tasks 5–6)                                                     |
| `apps/server/internal/resume/bounds_test.go`                                                                          | The schema-driven limit+1 harness (Task 5)                                                                                    |
| `apps/server/internal/resume/idempotency.go` + tests                                                                  | D11 idempotency primitive (Task 7)                                                                                            |
| `apps/server/internal/resume/docmigrate/{docmigrate.go,backfill.go}` + tests                                          | D12/D13/D18 projection + CAS backfill + wire-version declarations (Task 8)                                                    |
| `apps/server/internal/resume/{writesafety,docmigrate,bounds}_adversarial_test.go`                                     | Blind suites A, B, and C (Tasks 9–11; three separate fresh authors)                                                           |
| `apps/server/go.mod`/`go.sum` (modify)                                                                                | `santhosh-tekuri/jsonschema/v6` pin (Task 5; serialized per B10)                                                              |
| `docs/plans/traceability.md` (modify)                                                                                 | Row closure against owner-minted IDs (Task 12)                                                                                |

Not touched by this phase: root `Makefile` (owner-only; Task 12 reports the
needed edit), `docs/api/openapi.yaml`, `apps/web/**`, `deploy/**`,
`packages/schema/resume.schema.json` itself (frozen).

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

- [ ] **Step 1: failing tests for the broadened keyword net.** Table-driven over
      schema texts containing each M-NEW variant → all must be detected (today
      `CREATE MATERIALIZED VIEW x` passes silently — assert the red). Extend the
      table with the B4 additions, each asserted unconditionally rejected with
      no cross-check path: `CREATE PROCEDURE`, `CREATE RULE`, `CREATE POLICY`,
      `ALTER FUNCTION`, `ALTER TRIGGER`, `ALTER TABLE …     ENABLE TRIGGER …`,
      `ALTER TABLE … DISABLE TRIGGER …`.
- [ ] **Step 2: failing tests for the FUNCTION/TRIGGER cross-check.** Cases:
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
- [ ] **Step 3: implement; all red tests green.** Keep
      `checkExtensionDeclarations` untouched.
- [ ] **Step 4: gate.**
      `cd apps/server && go build ./... && go vet ./... &&     go test ./cmd/migrate/gen/... -count=1`;
      then `make test-db-up &&     make data-drift` (must still pass clean at
      head — the check is a no-op until Task 3 adds objects) and
      `make server-migration-test`.
- [ ] **Step 5: commit** —
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

- [ ] **Step 1: failing conformance tests, both languages.** Go:
      `TestValidateDocument_DuplicateEntryID` consuming
      `fixtures/store/invalid-duplicate-entry-id.json` (duplicate ids in
      **different sections** — the whole-resume rule, not per-section); TS: the
      mirror case in `store-validation.test.ts`. Add one green case (same id
      nowhere duplicated) and one cross-section-duplicate fixture already exists
      — verify it encodes the cross-section case; if it is same-section-only,
      add a second fixture rather than editing it. Run
      `cd packages/schema && npm test` and
      `cd packages/schema/gen/go && go test ./...` → **FAIL**.
- [ ] **Step 2: implement both halves; green.** Deterministic issue ordering
      (sort by path) like the existing validators.
- [ ] **Step 3: failing raw-schema test.** `rawschema_test.go`: read
      `../../resume.schema.json` at test time and assert `schema.RawSchema`
      byte-equals it — this one test closes the copy-drift loop from the Go
      side; the existing `gen.test.ts` byte-compare covers it from the generator
      side unchanged. Run → **FAIL** (`RawSchema` undefined). Extend
      `generate.mjs` to emit `rawschema.go` (generated header, `DO NOT EDIT`);
      run `make schema-gen`; commit generated output; green.
- [ ] **Step 4: gate.** `make schema-check` (regenerates via npm ci + vitest,
      incl. `gen.test.ts`; proves no drift) and
      `cd packages/schema/gen/go && go test ./...`.
- [ ] **Step 5: commit** —
      `git commit -m "feat(schema): enforce whole-resume entry-id uniqueness and generate the raw-schema Go constant" -- packages/schema`

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

- [ ] **Step 0 (spike, minutes, before anything):** append a minimal
      `CREATE FUNCTION … $$…$$; CREATE TRIGGER …` pair to a scratch copy of
      `schema.sql` and run `sqlc generate` against it. sqlc's pg_query-based
      parser is expected to accept plpgsql DDL and ignore it; **if it errors,
      STOP** — that is a missing tooling decision for the integration owner
      (splitting schema.sql would fork the single source of truth and is not
      this plan's call). Do not improvise.
- [ ] **Step 1: failing migrated-DB tests.** `resume_schema_test.go`, using
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

- [ ] **Step 2a: append tables + indexes only; generate `00004`.** Append just
      the three `CREATE TABLE …`/`CREATE INDEX …` statements above (no function,
      no trigger) to `sql/schema.sql`. `make test-db-up` then `make migrate-gen`
      — inspect the generated `00004_add_resume_tables.sql`: it must contain the
      three tables, constraints, and indexes, and there is nothing else in
      `schema.sql` yet for it to omit. Rename per the tool's output convention
      (the pipeline numbers it).
- [ ] **Step 2b: append function + trigger, and hand-write `00005`, in the same
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
- [ ] **Step 3: green.** Step 1's tests pass. Then the full data gates:
      `make sqlc-check` (no query changes yet — must stay clean),
      `make server-migration-test` (harness picks up 00004/00005 in all four
      scenarios), `make data-drift` (Task 1's cross-check now proves
      schema.sql's fn/trigger match 00005 — also run the red case once locally:
      perturb one token of the function body in `schema.sql`, confirm
      `make data-drift` fails, revert).
- [ ] **Step 4: commit** —
      `git commit -m "feat(resume): add resumes, slug_tombstones, idempotency_records tables and 3-resume cap trigger" -- apps/server/sql/schema.sql apps/server/migrations`

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
UPDATE resumes
SET personal_details = $4, content = $5, customization = $6,
    schema_version = $7
WHERE id = $1 AND schema_version = $2 AND revision = $3;

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
-- D11 opportunistic reaping (owner ruling): every Execute deletes the
-- calling user's expired rows in the same tx before inserting, so the TTL
-- is enforced by normal traffic, not by a job that doesn't exist yet.
DELETE FROM idempotency_records
WHERE user_id = $1 AND expires_at <= $2;
```

Note `BackfillResumeDocumentCAS` deliberately does **not** touch `revision` or
`updated_at` (D12; `updated_at` tracks user-visible changes for the same reason)
and is not user-scoped (a system job). No `slug_tombstones` queries (D15 — P5A
defines that contract).

- [ ] **Step 1: failing compile-time shape test** pinning the generated contract
      later tasks build on:
      `store.Resume{ID, UserID uuid.UUID, Title string, Slug *string, Live,     DownloadEnabled, SeoGeoEnabled bool, SchemaVersion int32, Revision     int64, Lng *string, PersonalDetails, Content, Customization     json.RawMessage, CreatedAt, UpdatedAt time.Time}`
      and `store.IdempotencyRecord{…}` (per the committed sqlc.yaml overrides:
      pointers for nullables, `json.RawMessage` for jsonb). Run
      `cd apps/server && go test ./internal/resume/...` → **FAIL**. If sqlc's
      rename of `seo_geo_enabled` is awkward (e.g. `SeoGeoEnabled` vs
      `SEOGeoEnabled`), add a `rename:` entry — decide in review, then pin it in
      this test.
- [ ] **Step 2: append queries, `make sqlc-gen`, commit generated output; Step 1
      compiles green.**
- [ ] **Step 3: gate.** `make sqlc-check` (regenerate → no diff),
      `make server-build server-vet server-test`, `make data-drift`.
- [ ] **Step 4: commit** —
      `git commit -m "feat(resume): add resume and idempotency sqlc queries" -- apps/server/sql/queries.sql apps/server/internal/store apps/server/internal/resume`

---

### Task 5: Document codec + validation pipeline — every size bound with a limit+1 test

Wires AC-DOC-003 / AC-DOC-007 / AC-DOC-008 into live writes; closes AC-DOC-002
at the write path; enforces the budgets 512 KB row and every schema bound (the
master plan's "**all size bounds with limit+1 tests**").

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
func EncodeParts(doc schema.Resume) (personalDetails, content,
    customization json.RawMessage, err error)

type ValidationError struct{ Issues []string } // stable, sorted, path-first
func (e *ValidationError) Error() string

// ValidateForStore is the single write-path choke point (D16/D1):
// canonical marshal → JSON-Schema validation (embedded schema.RawSchema) →
// MaxDocumentBytes → schema.ValidateDocument (incl. Task 2's entry-id
// uniqueness). Returns *ValidationError or nil.
func ValidateForStore(doc schema.Resume) error
```

- [ ] **Step 1: failing codec round-trip tests.** Parts→doc→parts byte-stable
      for `packages/schema/fixtures/{minimal,full,draft-*}.json`
      (draft-permissiveness preserved: absent vs `""` distinguishable after a
      round trip — the spec's "never fabricate a sentinel" rule as a test);
      parts never contain a `schemaVersion` key (D4); unknown field in a stored
      part → decode error (strict).
- [ ] **Step 2: failing pipeline tests.** Every `fixtures/store/invalid-*`
      fixture rejected by `ValidateForStore` with a matching issue; every valid
      fixture accepted; issues deterministic across runs.
- [ ] **Step 3: the bounds harness (`bounds_test.go`) — failing first.** Two
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
- [ ] **Step 3b: the cross-language verdict-parity corpus (D1(e)) — failing
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
- [ ] **Step 4: implement (`jsonschema/v6` compiled once at init from
      `schema.RawSchema` per the D1 conditions, package-level, immutable); all
      green.** These are pure unit tests — no DB. Record the `go mod     graph`
      delta in the task report.
- [ ] **Step 5: gate.**
      `cd apps/server && go build ./... && go vet ./... &&     go test ./internal/resume/... -count=1`;
      `make server-build server-vet     server-test`; `make schema-check` (the
      parity vitest rides in it).
- [ ] **Step 6: commit** —
      `git commit -m "feat(resume): add document codec and full-bounds store validation pipeline" -- apps/server/internal/resume apps/server/go.mod apps/server/go.sum packages/schema/fixtures/bounds packages/schema/test/bounds-parity.test.ts`

---

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
)

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

- [ ] **Step 1: failing happy-path integration tests** (all via
      `testutil.RequireMigratedTestDatabaseURL`, table-driven): create → get
      round-trip (doc byte-stable through codec; revision 1; defaults
      live=false/download=true/seo=false); list ordering stable
      (`created_at, id`); delete → `ErrNotFound` on re-get; get/delete with the
      wrong user → `ErrNotFound` (D17 — the other user's row untouched, assert
      full-row equality before/after).
- [ ] **Step 2: failing cap tests.** 3 creates succeed, 4th → `ErrCapExceeded`;
      delete one → create succeeds again; a second user is unaffected. (The
      N-way concurrency race is Suite A's, Task 9 — the author writes the
      sequential cases only; do not pre-empt the blind suite.)
- [ ] **Step 3: failing CAS tests.** Save with correct revision → revision 2,
      doc updated; stale revision → `*RevisionMismatchError` with current
      revision + current doc (assert the doc is the _winning_ content); unknown
      id → `ErrNotFound`; invalid doc → `*ValidationError`, row untouched
      (full-row comparison — validation must run before any write); `SaveTitle`
      same matrix.
- [ ] **Step 4: implement; green.** Implement the `…Tx` cores first (B7:
      `createTx`/`saveDocumentTx`/`saveTitleTx` — no tx management inside them),
      then `Create`/`SaveDocument`/`SaveTitle` as thin begin-tx/`WithTx`/commit
      wrappers around them; re-run Steps 1–3's tests unmodified against the
      wrapper form (they must still pass — the split is an internal refactor,
      not a behavior change). pgx error mapping via
      `pgconn.PgError{Code: "23514", Message: "resumes_user_cap_exceeded"}`
      (exact match on both — D7).
- [ ] **Step 5: gate (dev-loop evidence, not phase-exit evidence — B11).**
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
- [ ] **Step 6: commit** —
      `git commit -m "feat(resume): add resume store with cap enforcement and revision CAS" -- apps/server/internal/resume`

---

### Task 7: Idempotency record store (replay / reject / rollback primitive)

Store-level substrate for AC-SAVE-002 (P2B closes the HTTP row). Implements D11.
Also the forward contract from `phase-1-deferred.md`: the client's
`csrf_rejected` retry **reuses the same `Idempotency-Key`** — this primitive is
what makes that retry safe (same key + same body ⇒ replay, never a double
mutation); record that sentence in the package doc so P2B/P4 inherit it as
written contract, not accident.

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

// Execute runs mutate exactly once per (userID, route, key). mutate MUST
// perform every write through the supplied tx — the rollback arm depends
// on it. Flow (D11): tx { reap this user's expired rows (opportunistic,
// owner ruling — response_body holds user content, so the TTL is enforced
// by normal traffic, not a future job); delete-if-expired same-key row;
// mutate; insert record }; unique violation on insert → ROLL BACK
// EVERYTHING, re-read committed record: hash equal → (stored,
// replayed=true, nil); else → ErrIdempotencyKeyReuse.
func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
    route string, key uuid.UUID, bodyHash [32]byte,
    mutate func(qtx *store.Queries) (StoredResponse, error),
) (resp StoredResponse, replayed bool, err error)
```

- [ ] **Step 1: failing sequential tests.** First call runs `mutate`, stores +
      returns response; second call same key+hash → replayed=true, stored
      response byte-identical, `mutate` NOT invoked (spy counter), no new row;
      same key different hash → `ErrIdempotencyKeyReuse`, `mutate` not invoked,
      zero writes; `mutate` returning an error → nothing persisted (record row
      absent, mutation rolled back); expired record (injected clock past TTL) +
      same key → treated as fresh: old row replaced, new execution.
- [ ] **Step 2: failing rollback test.** `mutate` inserts a real resume row by
      calling **`(*resume.Store).createTx(ctx, qtx, …)` directly (B7's tx-scoped
      core — not a hand-rolled `INSERT` stand-in)**, proving `Execute` actually
      composes with Task 6's real `Create` logic, cap check included, inside one
      transaction; force the duplicate-insert path (pre-seed the record from a
      competing connection mid-flight or sequentially construct the conflict);
      assert the loser's resume insert is **gone** after rollback and the
      returned response is the winner's. (The true concurrent race is Suite
      A's.)
- [ ] **Step 2b: failing composition test (B7).** A second case building
      directly on Step 2's wiring: two `Execute` calls with different
      idempotency keys, same user, each `mutate` calling `createTx`, back to
      back — both resumes exist, cap accounting is correct (this is
      `resume.Store`'s real cap check running inside `IdempotencyStore`'s tx,
      not a bypass), and a call that would be a 4th resume for that user still
      surfaces `ErrCapExceeded` from inside `mutate`, which `Execute` propagates
      without inserting an idempotency record (nothing to replay for a rejected
      mutation).
- [ ] **Step 3: implement; green.** Injected `now` for TTL; SHA-256 is the
      caller's job (P2B hashes the raw body — keep the primitive
      transport-agnostic).
- [ ] **Step 4: gate.** Same live-DB command + tally as Task 6 Step 5.
- [ ] **Step 5: commit** —
      `git commit -m "feat(resume): add transactional idempotency record store" -- apps/server/internal/resume`

---

### Task 8: Doc-shape migration machinery — projection-only read, CAS write persistence, CAS backfill, wire-version declarations

Implements the spec §3 doc-migrations bullet and the wire-version machinery row
("built in P2A … before a second version exists"). No traceability row exists
yet — see the gap table; do not invent an ID. **D12(ii) binding:**
`docmigrate.go`'s package doc records, verbatim, that every write path must
persist the full document through the codec — never a granular `jsonb_set`-style
PATCH, which would let old-shape content re-enter storage where the backfill CAS
cannot see it. This is P2B's binding-in-writing condition from D12; Task 12
forwards the sentence to the owner alongside the other P2B forward-binding notes
(as Task 7 does for the idempotency retry contract).

**Files:** create
`apps/server/internal/resume/docmigrate/{docmigrate.go,backfill.go}`,
`docmigrate_test.go`, `backfill_test.go`, `export_test.go`; modify
`apps/server/internal/resume/store.go` (Get/List call `Projector.Project`;
SaveDocument persists at `CurrentVersion` — completing Task 6's stub).

**Interfaces.** Produces:

```go
package docmigrate

const CurrentVersion int32 = 1

// AcceptedVersions / EmittedVersion are the spec's wire-version
// declarations: which stored/wire doc versions this server accepts and
// which it emits. With one released version both are {1}/1; adding v2
// extends these + registers a converter — a written contract, tested, so
// the machinery exists before it is needed.
func AcceptedVersions() []int32
func EmittedVersion() int32

// ConvertFunc lifts a FULL canonical document from version N to N+1 (D13).
type ConvertFunc func(doc json.RawMessage) (json.RawMessage, error)

type Projector struct { /* convs map[int32]ConvertFunc; current int32 */ }

func NewProjector(convs map[int32]ConvertFunc, current int32) (*Projector, error)
func NewIdentityProjector() *Projector // production wiring today: no convs

// Project is PURE (D18): parts+version in, current-version typed doc out.
// It never touches the database.
func (p *Projector) Project(personalDetails, content, customization json.RawMessage,
    storedVersion int32) (schema.Resume, error)
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

- [ ] **Step 1: failing projection tests.** Identity projector: v1 parts →
      identical doc. Synthetic converter (test-only, injected via
      `NewProjector`): register a fake `1→2` converter with `current=2`, feed v1
      parts, assert converted output; chain `1→2→3`; missing converter for a
      stored version → error (fail closed, never a silent passthrough);
      converter returning invalid JSON → error. **Projection purity:** run `Get`
      against a live row seeded at a synthetic old version (insert the row with
      `schema_version` overridden via direct SQL), assert the returned doc is
      projected AND the row's bytes, `revision`, and `updated_at` are
      bit-identical before/after (D18) — the "projection-only on read, never
      writes during GET" clause as a test.
- [ ] **Step 2: failing write-persist test.** `SaveDocument` on that old-version
      row persists at `CurrentVersion` (per spec: "persisted only when a user
      write occurs, transactional") and bumps revision once.
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
- [ ] **Step 4: implement; green.** `Store.Get`/`List` now project;
      `SaveDocument` persists current version (Task 6's tests still green — run
      them).
- [ ] **Step 5: gate.** Live-DB command + tally (Task 6 Step 5's form), plus
      `make server-build server-vet server-test`.
- [ ] **Step 6: commit** —
      `git commit -m "feat(resume): add doc-shape projection, CAS backfill, and wire-version declarations" -- apps/server/internal/resume`

---

### Task 9: Blind adversarial suite A — write-safety and cap concurrency matrix

Mandated by the master plan's independence rule for concurrency: a **second,
fresh Sonnet 5 instance** derives these from the written contracts **before
reading any `internal/resume` implementation diff or author test**. Inputs the
blind author gets: spec §3 (cap + invariants bullets), §4 (write-safety
paragraph), `budgets.md`, traceability AC-DOC-001/AC-SAVE-001/AC-SAVE-002
statements, and this plan's **Interfaces blocks only** (Tasks 6–7 signatures +
typed errors). Inputs withheld: `internal/resume/*.go`, `store_test.go`,
`idempotency_test.go`, `sql/queries.sql`. The author of Tasks 5–8 must not edit
this suite; weakening any assertion requires Opus 5 review by name.

**Files:** create `apps/server/internal/resume/writesafety_adversarial_test.go`.

Minimum matrix (the blind author may add, never subtract):

| Test                                                 | Assert                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestCreate_Concurrent_ExactlyThreeSucceed`          | 20 concurrent `Create` for one user → exactly 3 succeed, 17 `ErrCapExceeded`, row count 3; deterministic under `-race -count=20`                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `TestCreate_RawSQLBypass_StillCapped`                | 3 rows via store, 4th via raw `INSERT` → SQLSTATE 23514 (the trigger is the enforcement, not the Go code)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `TestCreate_ConcurrentRawSQLBypass_StillCapped`      | **The trigger's own `FOR UPDATE` under concurrency, added 2026-08-03 (owner).** Two raw-SQL connections, no store layer: tx1 inserts the 3rd resume and holds; tx2's 4th insert must **block** (poll `pg_locks`/`pg_stat_activity` until observed blocked — no `time.Sleep`); tx1 commits; tx2 then fails `23514`/`resumes_user_cap_exceeded`. Deleting the trigger's `PERFORM … FOR UPDATE` line must make this test fail. Task 3's review found that line had **zero** behavioral coverage — the store's own lock masks it in every store-mediated test, so only a bypassing concurrent writer exercises it |
| `TestSaveDocument_ConcurrentSameRevision_OneWinner`  | N concurrent CAS at revision R → exactly one new revision R+1; every loser gets `*RevisionMismatchError` whose `Current` equals the winner's doc                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `TestSaveDocument_MismatchCarriesWinningDoc`         | loser's error payload byte-matches a fresh `Get` (the 412-body contract P2B will serialize)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `TestIdempotency_ConcurrentSameKey_MutationRunsOnce` | N concurrent `Execute` same key+hash → one mutation execution observable in the DB; all callers converge on one stored response                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `TestIdempotency_LoserMutationRolledBack`            | after the race, exactly ONE resume row exists from the mutations (the rollback arm — no orphan writes)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `TestIdempotency_DifferentBodyNeverExecutes`         | reuse with a different hash: `ErrIdempotencyKeyReuse` and zero DB deltas, even interleaved with valid replays                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `TestValidation_RejectionWritesNothing`              | oversized/invalid doc through `Create` and `SaveDocument` → full-row/rowcount equality before vs after (limit+1 at the transaction boundary, not just the validator)                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `TestNoExistenceOracle_WrongUserSameAsNotFound`      | across `Get`, `Delete`, `SaveDocument`, `SaveTitle`: calling with a real id owned by a different user returns byte-identical `ErrNotFound` (or, for the CAS methods, the same not-found path — never a distinguishable `*RevisionMismatchError`) as calling with a wholly nonexistent id — no response-shape difference an attacker could use as an existence oracle (D17)                                                                                                                                                                                                                                    |

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

**Files:** modify `docs/plans/traceability.md` (claimed rows only; new rows as
proposals to the owner); no other doc edits without owner direction.

- [ ] **Step 1:** fill test references for AC-DOC-001 (trigger + store + Suite A
      tests), AC-DOC-002 (Task 2 conformance + Task 5 pipeline),
      AC-DOC-003/007/008 (append the live-write wiring references to the
      existing P0 references; the rows' "not yet wired, P2A" annotations come
      out).
- [ ] **Step 2:** hand the integration owner, in one report: (a) the proposed
      new traceability rows (doc-migration CAS, 512 KB store bound, wire-version
      machinery, store-level idempotency — exact statement text drafted, IDs
      left to the owner); (b) the Makefile edit — `server-test-db`'s package
      list gains `./internal/resume/...`; (c) the master-plan correction
      (drift-gate location is `cmd/migrate/gen`, not the shell script); (d) the
      P2B forward-binding notes (D14 customization allowlist; the idempotency
      retry contract sentence from Task 7; the D12(ii) full-document-persistence
      binding recorded in Task 8's `docmigrate.go` package doc).
- [ ] **Step 3: gate.** `make docs-fmt && make docs-lint`.
- [ ] **Step 4: commit** —
      `git commit -m "docs(plans): close Phase 2A traceability rows" -- docs/plans/traceability.md`

---

## Integration handoffs (owner-applied, not worker-applied)

| Shared file                           | Needed change                                                                                | When          |
| ------------------------------------- | -------------------------------------------------------------------------------------------- | ------------- |
| root `Makefile`                       | `server-test-db` package list += `./internal/resume/...`                                     | with Task 6   |
| `docs/plans/implementation-plan.md`   | Drift-gate wording correction (script → `cmd/migrate/gen`); phase-status row when gates pass | Task 1 / gate |
| `docs/plans/traceability.md` new rows | Four proposed rows (Task 12 Step 2(a))                                                       | Task 12       |
| root `go.work.sum` (if materialized)  | commit as lockfile                                                                           | whenever      |

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
- [ ] Every size bound in the named-bound matrix has a passing limit and failing
      limit+1 case, and the schema-walk completeness guard passes (no schema
      bound unexercised).
- [ ] Entry-id uniqueness enforced whole-resume in Go and TS against the shared
      fixture; `make schema-check` proves no generated drift.
- [ ] CAS mismatch returns the current revision + winning doc; concurrent
      same-revision writers produce exactly one winner (Suite A green).
- [ ] Idempotency: replay returns the stored response without re-execution;
      different body rejected with zero writes; the rollback race leaves exactly
      one mutation (Suite A green).
- [ ] Reads never write (purity test green under concurrency); backfill CAS
      loses cleanly to autosave; autosave after backfill does not 412; a
      title-only write between read and CAS also yields a retryable
      `BackfillLostRace` with `schema_version` still old, and a second
      `BackfillOne` then applies (Suite B green; B6).
- [ ] Suite C (Task 11) independently derived the same size-bound limit+1 matrix
      from `budgets.md`/spec §3/`resume.schema.json`/Task 5's Interfaces block
      alone, without reading `bounds_test.go` or `validate.go`; any disagreement
      with Task 5's own matrix was resolved before phase exit, not waved
      through.
- [ ] All three blind suites (A, B, C) were authored by fresh instances from the
      written contracts before reading implementation diffs, and were not edited
      by any implementation author; Opus 5 reviewed every task diff, blocking
      findings fixed and re-reviewed.
- [ ] Traceability rows AC-DOC-001/002/003/007/008 closed; the four new-row
      proposals and the Makefile/master-plan handoffs delivered to the
      integration owner.
