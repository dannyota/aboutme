# Phase 2B — Resume HTTP API + media (implementation plan)

> **Draft for owner adoption (2026-08-11).** Written against Rev 7 of the master
> plan and ADR 0011's two-tier risk model. Nothing here has executed. Steps use
> checkboxes: `[ ]` is an open action, `[x]` records completed author work.
>
> **For agentic workers (once adopted):** one task per fresh worker, disjoint
> owned paths, no Git operations by workers. Every task's tests are written
> **before** implementation (TDD): write the failing test, observe the failure,
> implement, observe the pass, then review and commit. High-risk tasks add the
> two extra ADR 0011 passes — a blind test author and a fresh reviewer.

**Goal:** the authenticated resume HTTP surface. Every endpoint in design spec
§4's resume rows, behind the existing session/CSRF chain, with the full
write-safety contract on every mutation (`If-Match` → `412`, `Idempotency-Key`
replay/reject, size bounds enforced before any write, structured error details);
the granular autosave endpoints (entry, section, structure, personal-details,
customization deltas); rich-text sanitization at the HTTP boundary; the
wire-version accept/emit contract proven over the real HTTP and OpenAPI path
(AC-SAVE-004); and per-resume photo upload to S3-compatible object storage with
a pinned local substitute so the whole phase runs on this laptop.

**Not in this phase:** publish/slug (`POST /resumes/{id}/publish`, P5A), PDF
(P7A/P7B), SSE (`GET /events`, `/live/{slug}`, P6A), the public read surface
(`GET /public/resumes/{slug}`, P5A), the editor client (P4), and any AWS wiring
(PI). The account-level `users.avatar_key` has **no upload surface**: P1
populates it from the OAuth profile fetch (master plan, Phase 2B tasks).

## Base preconditions (all three are hard predecessors)

This plan is executable the moment these land, and not before.

| Input                               | Why P2B needs it                                                                                                                                                                                                                    |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **P2A complete**                    | Every route binds to `internal/resume`: the validated write choke point, revision CAS, the idempotency primitive, and — specifically — Task 8's wire-version machinery (`docmigrate.Projector`, `AcceptWire`/`EmitWire`, `Project`) |
| **P0F merged**                      | The pinned OpenAPI → TypeScript generator, committed client artifact, and the non-mutating drift gate. Every P2B route regenerates that client and leaves `make api-check` green                                                    |
| **P1.1 merged**                     | Auth-route rate limits, the CSRF-protected link/reauth `POST`, and the rotation reliability fix. P2B's routes ride the same session/CSRF chain and inherit its policy shape                                                         |
| _(soft)_ **P3 `internal/sanitize`** | Task 5 wires `sanitize.RichText` into the write boundary (AC-SEC-003's P2B half; P3's Task 2 says "wiring into write endpoints is P2B"). The delivery index runs P3 in step 04, ahead of P2B in step 05 — see open question Q1      |

**Execution base.** Start from `main` at or after the commit that closes the P2A
phase gates. Run `git rev-parse HEAD` and confirm that ancestry before working —
worktree-isolated workers have checked out stale commits before; verify, don't
assume.

**Migration head:** **P2B needs no new migration.** The photo object key lives
in the document (`personalDetails.photo.key`, already in the frozen schema),
idempotency records and the resume tables landed in P2A's `00004`/`00005`, and
no route introduces a new column, table, or index. If any task concludes
otherwise it **stops and reports** rather than authoring DDL: a migration would
become one serialized, prominently flagged task through the integration owner
(decision [D13](decisions.md)).

**sqlc queries: exactly one addition, in exactly one lane.** P2A's statements
cover every read and write here with one exception — `UpdateResumeTitleCAS` sets
`title` only, so changing a resume's `lng` after creation has no statement.
**Task 2 is the sole holder of the `apps/server/sql/queries.sql` + regenerated
`apps/server/internal/store/**` window** and adds `UpdateResumeMetadataCAS`
(title and `lng` together, same CAS predicate). No migration is involved: both
columns already exist. Any other task that believes it needs a query stops and
reports; a second window is granted by the integration owner only after the
first closes.

## Contents

Reference sections of this plan:

- [Design decisions this plan makes beyond the spec](decisions.md)
- [The HTTP write path this phase builds](http-contract.md)
- [File structure produced by this phase](file-structure.md)
- [Integration handoffs (owner-applied, not worker-applied)](integration-handoffs.md)
- [Phase exit criteria](exit-criteria.md)

One file per task, in execution order:

- [Task 1: The whole P2B OpenAPI surface, contract-first](task-01-openapi-contract.md)
- [Task 2: Transaction seam on `internal/resume` for HTTP callers](task-02-store-transaction-seam.md)
- [Task 3: Media storage substrate + pinned local S3-compatible service](task-03-media-storage-substrate.md)
- [Task 4: Write-safety HTTP kernel, route table, and per-route policy](task-04-write-safety-kernel.md)
- [Task 5: Rich-text sanitization at the HTTP write boundary (AC-SEC-003)](task-05-sanitize-write-boundary.md)
- [Task 6: Resume CRUD — list, create (cap), read, rename, delete](task-06-resume-crud.md)
- [Task 7: Entry upsert/delete and section metadata/order](task-07-entries-and-sections.md)
- [Task 8: `PATCH /resumes/{id}/structure` — the transactional endpoint](task-08-structure-endpoint.md)
- [Task 9: Personal details + the old-client wire-version proof (AC-SAVE-004)](task-09-personal-details-and-wire-version.md)
- [Task 10: Customization deltas from a fixed path allowlist](task-10-customization-deltas.md)
- [Task 11: Per-resume photo upload, read, replace, and delete](task-11-photo-endpoints.md)
- [Task 12: Blind adversarial suite D — auth, CSRF, and cross-user authz](task-12-blind-suite-authz-csrf.md)
- [Task 13: Blind adversarial suite E — HTTP write safety and wire version](task-13-blind-suite-write-safety.md)
- [Task 14: Blind adversarial suite F — bounds, hostile payloads, media](task-14-blind-suite-bounds-media.md)
- [Task 15: Traceability closure, new AC rows, docs, and handoffs](task-15-traceability-closure.md)

## Task status index

The owner-facing execution ledger. A high-risk task is not complete until its
independent defect review is green; implementation alone is reported separately.

| Task | Deliverable                                        | Tier   | Wave / lane | State   |
| ---- | -------------------------------------------------- | ------ | ----------- | ------- |
| 1    | Full P2B OpenAPI surface + examples + client regen | Normal | W1 / A      | Pending |
| 2    | Exported transaction seam on `resume.Store`        | High   | W1 / B      | Pending |
| 3    | `internal/media` + local S3-compatible service     | High   | W1 / C      | Pending |
| 4    | Write-safety kernel, route table, route policies   | High   | W2 / D      | Pending |
| 5    | Sanitizer wiring at the write boundary             | High   | W2 / E      | Pending |
| 6    | `GET/POST /resumes`, `GET/PATCH/DELETE {id}`       | High   | W3 / F      | Pending |
| 7    | Entry upsert/delete + section metadata/order       | High   | W3 / G      | Pending |
| 8    | `PATCH /resumes/{id}/structure`                    | High   | W3 / H      | Pending |
| 9    | `PATCH …/personal-details` + AC-SAVE-004 proof     | High   | W3 / I      | Pending |
| 10   | `PATCH …/customization` + path allowlist           | High   | W3 / J      | Pending |
| 11   | `POST/GET/DELETE …/photo`                          | High   | W3 / K      | Pending |
| 12   | Blind suite D — auth/CSRF/authz matrix             | High   | W4 / L      | Pending |
| 13   | Blind suite E — write safety, idempotency, wire    | High   | W4 / M      | Pending |
| 14   | Blind suite F — bounds, hostile payloads, media    | High   | W4 / N      | Pending |
| 15   | Traceability closure, new rows, docs, handoffs     | Normal | W5 / O      | Pending |
| Gate | ADR 0011 phase defect review + phase acceptance    | —      | W6          | Pending |

## Parallel waves and lane discipline

Lanes inside a wave have **disjoint owned file sets** and are dispatched
together in one message. A wave does not start until the previous wave is
integrated and `make check` is green.

| Wave | Lanes (parallel)          | Gate before the next wave                                                                                        |
| ---- | ------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| W1   | A (T1), B (T2), C (T3)    | Owner contract review of the OpenAPI diff; `make api-check server-build server-vet` green                        |
| W2   | D (T4), E (T5)            | **Lands as one unit** — T4's persist helper calls the function T5's file defines, so neither compiles alone (D5) |
| W3   | F–K (T6–T11)              | Every route's own tests + its OpenAPI conformance test green; `make check` green                                 |
| W4   | L (T12), M (T13), N (T14) | Three separate fresh blind authors; any red is a real finding routed to an implementer, never fixed by the suite |
| W5   | O (T15)                   | Traceability rows closed and handoffs applied or assigned                                                        |
| W6   | Phase gates               | See [exit-criteria.md](exit-criteria.md)                                                                         |

**Serialized paths (never two lanes at once):** `apps/server/sql/queries.sql`
and the regenerated `apps/server/internal/store/**` (Task 2 only);
`apps/server/migrations/**` (**no change at all** this phase);
`docs/api/openapi.yaml` (Task 1 only); the root `Makefile`;
`.github/workflows/ci.yml`; `apps/server/go.mod`/`go.sum` (Task 3 only);
`apps/web/app/api/generated/**` (Task 1 only).

**OpenAPI coordination rule — contract-first, one task, before any handler.**
Task 1 lands the entire P2B path/schema/parameter surface with examples and
regenerates the TypeScript client once. Every later task then owns only Go code
and tests, adds a Go conformance test asserting its handler agrees with the
committed document, and **must not edit `docs/api/openapi.yaml`**. This is
faster than integrating N per-task contract diffs (which would serialize the
document, re-run the drift gate N times, and regenerate the client N times) and
it matches the repo's own rule that a contract change is a dedicated reviewed
commit, never a side effect. The cost — the contract can drift ahead of the
implementation — is paid by the per-route conformance tests and by an explicit
amendment path: a route task that finds the contract wrong stops, reports the
exact needed diff, and the integration owner lands it as a separate reviewed
commit before that task resumes. See [D1](decisions.md).

## Traceability rows claimed by this phase

| ID          | Statement                                                                                                                                | Claimed by     |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| AC-SAVE-001 | Stale `If-Match` → `412` with current revision (and document)                                                                            | Tasks 4, 13    |
| AC-SAVE-002 | Idempotent replay returns stored response; different body rejected                                                                       | Tasks 4, 13    |
| AC-SAVE-004 | Old-version client document accepted → projected → persisted at current → emitted at a declared version, over the real HTTP/OpenAPI path | Tasks 4, 9, 13 |
| AC-SEC-003  | P2B half — `sanitize.RichText` wired into every rich-text write path                                                                     | Tasks 5, 14    |

Referenced but **not** re-owned (P2A owns the row; P2B adds HTTP-level evidence
to it): AC-DOC-001 (4th resume rejected through the API), AC-DOC-004 /
AC-DOC-011 (bounds rejected before any write), AC-DOC-010 / AC-DOC-012
(projection and wire versions), AC-SAVE-003 (the store primitive under
AC-SAVE-002). AC-API-001 stays P0F's; P2B keeps its drift gate green.

### New rows this phase mints

The master plan names **media/avatar upload** as an ownership gap whose rows
must be minted during the owning phase's plan refresh, before dispatch. Task 15
adds them; they are listed here so the gate can check the plan against the
matrix. `AC-MEDIA` is a new prefix and needs a new `../traceability/ac-media.md`
plus an index/count update in `../traceability/README.md`.

| ID           | Statement                                                                                                                                                                                                                                        |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| AC-MEDIA-001 | Per-resume photo upload is owner-only, bounded by declared size and content-type limits, and rejects an oversized or wrong-type body before any object is written                                                                                |
| AC-MEDIA-002 | The stored object key is **server-derived**, never client-supplied; it satisfies the schema's key pattern and the traversal check, and carries an unguessable random component                                                                   |
| AC-MEDIA-003 | Replacing a photo, deleting a photo, and deleting a resume remove the stored objects; no orphan object remains reachable after a successful delete                                                                                               |
| AC-MEDIA-004 | One storage contract, two backends: the filesystem backend (native dev, unit tests) and the S3-compatible backend pass the identical conformance suite, and the S3 path is proven against the pinned local service fail-closed at the phase gate |
| AC-MEDIA-005 | The account-level `users.avatar_key` has no upload surface in v1: it is populated from the OAuth profile fetch (P1) and is distinct from the per-resume photo                                                                                    |
| AC-SAVE-005  | `PATCH /resumes/{id}/customization` accepts delta paths only from a fixed allowlist; an unlisted path (including any `layout.sections` path) is rejected with no write — closes the boundary P2A's D14 deferred here with no row either way      |

## Open questions for the owner

Each blocks something named; none blocks writing the plan.

1. **Q1 — P3 ordering.** Task 5 needs `internal/sanitize` from P3 Task 2; the
   delivery index runs P3 in step 04 ahead of P2B, so is that guaranteed, or
   should Task 5 be carved out as a P2B follow-up if P2B dispatches first?
2. **Q2 — the missing `/assets` row.** Spec §6 says media sits in S3 behind
   CloudFront `/assets`, but §2's authoritative route table has no such row, so
   does the spec gain it now (with P5A/PI owning it) while P2B ships the
   owner-only proxy read of [D12](decisions.md)?
3. **Q3 — budget numbers.** Ratify photo upload ≤ 2 MiB, resume reads 600/min,
   resume writes 240/min, and media uploads 20/h into `../budgets.md` before
   dispatch, or supply different numbers.
4. **Q4 — the one frozen-contract amendment.** RATIFIED by the owner 2026-08-11:
   the additive optional `Error.details` object of [D7](decisions.md) is
   approved; it lands as the dedicated reviewed contract change in Task 1 with
   OpenAPI examples and client regeneration.
5. **Q5 — MinIO's licence.** RATIFIED by the owner 2026-08-11: MinIO in
   `deploy/compose.yml` is approved — its AGPL-3.0 licence matches the project's
   own, pinned and fully qualified per the compose conventions.
6. **Q6 — `lng` after creation.** Should `PATCH /resumes/{id}` accept `lng` (the
   reason Task 2 adds the phase's single sqlc query), or is changing a resume's
   language deferred with the rest of i18n?

## Authority

**Spec:** [`../../specs/aboutme-design.md`](../../specs/aboutme-design.md) — §3
(data model; the aggregate-invariant bullet and its "one transactional endpoint"
rule; doc-shape migrations = projection-only read + CAS write; size bounds incl.
"customization delta paths from a fixed allowlist";
draft-permissive/publish-strict, where P2B implements **draft** only; the
wire-version-compatibility row: "P2B binds that machinery to the real HTTP path
and proves an old-client write is projected, target-validated, persisted as the
complete current document, and emitted in a declared supported version"), §4
(every resume endpoint row; the write-safety paragraph; the envelope), and §5 as
it constrains API shapes (autosave = one coalesced debounced PATCH; the
sanitizer contract; per-resume photo with CSS crop).

**Master plan:** [`../implementation-plan.md`](../implementation-plan.md) —
"Phase 2B — Resume HTTP API + media" (exit + task list + phase acceptance),
"Global constraints", "Integration discipline", the Rev 7 gate structure, and
the numbered delivery index (P2B is step 05, waiting on 03 + P0F + P1.1).

**ADRs:**
[`../../adr/0011-risk-tiered-delivery-gates.md`](../../adr/0011-risk-tiered-delivery-gates.md)
(risk tiers — auth/authz, CSRF, concurrency/CAS/idempotency, sanitizer, and
resource bounds are all high risk, so nearly every task here carries the blind
author + fresh reviewer passes; two phase gates, not five; `make ci` is the gate
of record),
[`../../adr/0010-goose-only-migrations.md`](../../adr/0010-goose-only-migrations.md)
(migrations are goose-only and `migrations/` is the single schema source — P2B
adds none),
[`../../adr/0005-draft-permissive-documents.md`](../../adr/0005-draft-permissive-documents.md),
[`../../adr/0009-section-order-authority.md`](../../adr/0009-section-order-authority.md)
(section order authority is `customization.layout.sections` — why `/structure`
writes both columns together).

**Predecessor plan:** [`../phase-2a/README.md`](../phase-2a/README.md) and in
particular [`../phase-2a/write-path.md`](../phase-2a/write-path.md) (the store
write path P2B fronts), [`../phase-2a/decisions.md`](../phase-2a/decisions.md)
(P2A D12's binding rule that **every** write persists the full document through
the codec — no `jsonb_set`-style granular write; D14's customization-allowlist
handoff; D16's "strict decode remains P2B's ingress guard"; D17's no-existence-
oracle rule),
[`../phase-2a/task-07-idempotency-store.md`](../phase-2a/task-07-idempotency-store.md),
and
[`../phase-2a/task-08-doc-shape-migration.md`](../phase-2a/task-08-doc-shape-migration.md).

**Deferred-work contract:** [`../phase-1-deferred.md`](../phase-1-deferred.md) —
"Forward-binding decisions": `multipart/form-data` is permitted for the media
route on exact-Origin + synchronizer-token grounds (DD-C6's Content-Type gate is
not the load-bearing CSRF control); and the `csrf_rejected` retry **must reuse
the same `Idempotency-Key`**, a contract that lands here and in P4.

**Budgets:** [`../budgets.md`](../budgets.md) — request body ≤ 256 KB (P0B
middleware), resume document total ≤ 512 KB (P2A store), idempotency TTL 24 h.
P2B needs **four new numbers** landed there by the owner before dispatch: the
photo upload limit, the resume read and write rate limits, and the media upload
rate limit (see [integration-handoffs.md](integration-handoffs.md)).

**Contract:** [`../../api/openapi.yaml`](../../api/openapi.yaml) — P2B
**extends** it. The write-safety envelope, `Revision`, `IfMatch`,
`IdempotencyKey`, `sessionCookie`, and `csrfToken` already exist there and are
reused verbatim, never redefined.

## Global constraints (inherited, plus phase-specific)

- Latest stable, pinned exactly; commit the resolved `go.mod`/`go.sum` and
  lockfiles; never hand-write a version.
- Google Go style; `gofmt`/`goimports`; table-driven tests; Conventional
  Commits; no AI/agent mentions and no trailers in commit messages.
- Determinism: inject `now func() time.Time` anywhere time matters; no
  `time.Sleep`-based assertions; concurrency tests pass under `-race -count=20`,
  not "usually" (flaky = broken).
- `apps/server` keeps passing `go build ./... && go vet ./... && go test ./...`
  after every task (hermetic — DB-backed and object-storage-backed cases
  self-skip without their DSN/endpoint).
- Generated artifacts (`internal/store/*.go`, `packages/schema/gen/**`,
  `apps/web/app/api/generated/**`) are committed but never hand-edited.
- Stage only explicit owned paths (`git add -- <paths>`); inspect
  `git diff --cached --name-only` before every commit; never stage `.env`,
  `CLAUDE.md`, `AGENTS.md`, or another worker's files.
- **Every route, without exception:** the `{data}` / `{error}` envelope;
  `RequireSession` then `RequireCSRF`; a declared rate-limit policy; a
  regenerated TypeScript client with the drift gate green; bounds enforced
  **before** any write; structured error codes from the closed vocabulary in
  [D8](decisions.md). **Every write, without exception:** `If-Match` (except
  create — [D6](decisions.md)) and `Idempotency-Key`.
