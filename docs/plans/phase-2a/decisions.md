# Design decisions this plan makes beyond the spec

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

## D7 — Trigger mechanics and race safety

Use a `BEFORE INSERT OR UPDATE OF user_id` trigger. Its function first locks the
owner row (`PERFORM 1 FROM users WHERE id = NEW.user_id FOR UPDATE`) and then
counts, so it remains race-safe for writers bypassing the store. Store creates
take the same lock in the same order. A cap violation raises SQLSTATE `23514`
with message `resumes_user_cap_exceeded`; the store maps only that exact pair to
`ErrResumeCapExceeded`.

## D8 — Migration authorship

Generate `00004_add_resume_tables.sql` with `make migrate-gen`; hand-author
`00005_add_resume_cap_trigger.sql` for function/trigger DDL Atlas Community
cannot diff, following `00001_extensions.sql`; refresh `atlas.sum` with the
pinned Atlas. D9 makes the hand-authored object drift-safe.

## D9 — Drift-gate extension

`cmd/migrate/gen` broadens its unconditional keyword net, but permits FUNCTION
and TRIGGER only when each normalized declaration in `schema.sql` matches the
last same-object declaration in ordered migration `Up` sections, in both
directions. Drops, alters, views, sequences, procedures, rules, policies, and
all unsupported variants still fail closed. Task 1 pins statement extraction and
normalization details.

## D10 — Total-document measurement

Measure the 512 KB limit on canonical assembled JSON, including the injected
`schemaVersion`. This is deterministic, independent of jsonb representation, and
protects later granular writes even when each HTTP body is below 256 KB.

## D11 — Idempotency record and retention

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

## D12 — CAS backfill visibility

Backfill changes neither `revision` nor `updated_at`: all readers already see
the projected current document, so the storage rewrite is not observable. Its
predicate is `id + old schema_version + observed revision`. Task 8 must prove
byte-identical projected reads before/after backfill. P2B must persist the full
document through the codec; granular `jsonb_set` writes are forbidden because
they could reintroduce old shape outside the backfill CAS.

## D13 — Bidirectional adjacent converters

Each adjacent pair registers explicit `Up` and `Down` functions over full
canonical JSON. Production v1 has no pair; synthetic v1⇄v2 tests prove both
directions, old-client preparation to the current canonical shape,
supported-version emission, and fail-closed missing paths. Accepted input and
emitted output version sets are declared separately. Real HTTP persistence is
P2B's AC-SAVE-004 gate.

## D14 — Customization delta boundary

P2B owns the fixed allowlist for `PATCH …/customization` because it bounds the
request delta. P2A still validates the complete stored aggregate on every write.
The P2B handoff and traceability gap remain explicit.

## D15 — Slug tombstones

Use a uuidv7 surrogate primary key, unique slug, nullable
`released_by_user_id … ON DELETE SET NULL`, and `released_at`. Do not store
`expires_at`; P5A applies the authoritative 180-day claim-time rule and owns
tombstone queries/re-release behavior.

## D16 — Store validation choke point

Write APIs take typed `schema.Resume`, re-marshal canonical JSON, then run
JSON-Schema validation, the total-size bound, and aggregate validation. Strict
decode remains P2B's ingress guard. Generated sqlc write methods remain a
convention until the phase-exit lint restriction lands.

## D17 — Ownership scoping

Every per-resume query includes `id + user_id`. Wrong-owner and missing rows map
to the same `ErrNotFound`, including CAS methods, so the store exposes no
existence oracle.

## D18 — Pure projected reads

`Get` and `List` project stored documents to `CurrentVersion` without writes and
also expose the stored version for backfill progress. Tests pin unchanged row
bytes, revision, and `updated_at`; one unprojectable List row fails the whole
operation with no partial result.

## D19 — Immutable released schemas

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
