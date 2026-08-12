# Phase 2B — Resume HTTP API and media

Status: **Draft revision 2; not dispatchable** (2026-08-12).

This phase adds the authenticated resume HTTP surface, granular autosave
commands, full write-safety enforcement, document-version negotiation, and
private resume photos. It does not add publishing, public resume reads, SSE, the
editor, PDF generation, or cloud resources.

Draft revision 2 integrates [Draft v4 design](../../design/README.md), proposed
ADRs 0014–0019, the two-gate delivery model, and the media acceptance rows and
budgets that were missing from revision 1. A fresh plan review must close all
blocking findings before adoption.

## Hard preconditions

| Input                         | Required state                                                                                              |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------- |
| P2A                           | Task 12, phase defect review, and phase acceptance passed                                                   |
| P0F                           | Generated TypeScript client correction reviewed and accepted                                                |
| P1.1                          | Server, settings UI, OpenAPI prose, tests, and phase evidence closed                                        |
| P3                            | All tasks and both phase gates passed; current document version is v2                                       |
| Design decisions              | Draft v4 and proposed ADRs 0014–0019 approved                                                               |
| P2B plan                      | Independent review green; this page marked adopted                                                          |
| Numeric and acceptance inputs | `budgets.md`, AC-MEDIA-001…009, and AC-SAVE-005 frozen                                                      |
| Media benchmark manifest      | Task 11 Step 7 path, fixture hashes, local profile, production Graviton target, command, and samples frozen |

P3 Task 2 is the direct code dependency on P2B Task 5: its reviewed Go
`sanitize.RichText` implementation and generated allowlist must exist before
Task 5 starts. The whole P3 phase is still a dispatch precondition. P2B starts
only after the production projector accepts v1 and v2, emits both versions, and
stores v2 as current. P2B does not duplicate or defer the sanitizer.

## Fixed scope decisions

- `POST /resumes` requires an idempotency key and rejects `If-Match`. Every
  mutation of an existing resume requires both.
- Idempotency compares method, concrete target, resolved wire version,
  precondition, and bounded JSON or file bytes. Multipart framing and filename
  do not change photo identity.
- The optional `X-Resume-Schema-Version` header selects a supported wire version
  on every JSON resume read and every mutation. Absence means current. Binary
  photo GET is outside that contract; JSON resume responses and bodyless child
  deletes state the emitted version.
- `Error.details` is the structured location for validation issues, accepted
  versions, and the current document on revision mismatch.
- `PATCH /resumes/{id}` may change both `title` and `lng`. Task 2 owns the
  serialized migration/query window for full-aggregate metadata CAS,
  revision-CAS delete, and bounded idempotency retention; no other task changes
  those paths.
- Photo storage is private. Owner and future public reads pass through Go. There
  is no direct `/assets` route in v1.
- `PATCH /resumes/{id}/photo` changes or clears only the normalized crop. It
  preserves the photo key read inside the CAS transaction and performs no
  object-store I/O.
- Uploads accept decoder-confirmed static JPEG, PNG, or WebP. Source bytes,
  dimensions, pixels, time, concurrency, and memory are bounded. Only a
  metadata-free normalized JPEG or PNG reaches object storage.
- Photo upload bypasses the buffering JSON body middleware. Session, CSRF,
  route-rate, header, and permit checks precede one bounded streaming read.
- Filesystem and S3 backends implement one contract. A pinned local
  S3-compatible service proves the production protocol without AWS.
- Object upload precedes the database update. Failure compensates by deleting
  the new object. A crash orphan remains unreachable and is removed by the
  bounded scheduled sweep in AC-MEDIA-007.

[Decisions](decisions.md) records the mechanism and rejected alternatives.
[ADR 0019](../../adr/0019-private-media-delivery.md) owns the media trust and
failure boundary.

## Migration and shared-file rules

P2B adds only migration 00006 for bounded idempotency retention. Photo keys
already live in the document. Any other schema change stops for a new serialized
design and migration review.

The integration owner authors Task 2's exclusive migration,
`apps/server/sql/queries.sql`, and regenerated `internal/store/**` window. Task
1 alone owns OpenAPI; the integration owner regenerates the web client. Task 3
authors dependency-source changes; the integration owner owns lockfile writes.
The integration owner owns the root Makefile, workflows, and shared scripts.

## Task index

| Task | Deliverable                                        | Tier   | State   |
| ---- | -------------------------------------------------- | ------ | ------- |
| 1    | Complete OpenAPI contract and client regeneration  | Normal | Pending |
| 2    | Transaction seam and metadata CAS query            | High   | Pending |
| 3    | Media backends and local S3-compatible service     | High   | Pending |
| 4    | Write-safety kernel, routes, and policies          | High   | Pending |
| 5    | Sanitizer walk at the aggregate write boundary     | High   | Pending |
| 6    | Resume list, create, read, metadata update, delete | High   | Pending |
| 7    | Entry and section metadata commands                | High   | Pending |
| 8    | Atomic section structure command                   | High   | Pending |
| 9    | Personal details and wire-version proof            | High   | Pending |
| 10   | Fixed-allowlist customization deltas               | High   | Pending |
| 11   | Photo upload, read, crop, replace, and delete      | High   | Pending |
| 12   | Independent auth, CSRF, and authorization suite    | High   | Pending |
| 13   | Independent write-safety and wire-version suite    | High   | Pending |
| 14   | Independent bounds, hostile-input, and media suite | High   | Pending |
| 15   | Traceability evidence, docs, and handoffs          | Normal | Pending |
| Gate | Phase defect review and phase acceptance           | —      | Pending |

Task details:

- [Contract](task-01-openapi-contract.md),
  [store seam](task-02-store-transaction-seam.md),
  [media substrate](task-03-media-storage-substrate.md),
  [write kernel](task-04-write-safety-kernel.md), and
  [sanitizer wiring](task-05-sanitize-write-boundary.md)
- [Route tasks 6–11](task-06-resume-crud.md),
  [7](task-07-entries-and-sections.md), [8](task-08-structure-endpoint.md),
  [9](task-09-personal-details-and-wire-version.md),
  [10](task-10-customization-deltas.md), [11](task-11-photo-endpoints.md)
- [Independent suites 12–14](task-12-blind-suite-authz-csrf.md),
  [13](task-13-blind-suite-write-safety.md),
  [14](task-14-blind-suite-bounds-media.md), and
  [closure task 15](task-15-traceability-closure.md)

## Execution waves

| Wave | Parallel work                                      | Gate before next wave                                 |
| ---- | -------------------------------------------------- | ----------------------------------------------------- |
| W0   | T1 contract                                        | Contract review and API drift gate                    |
| WF   | T12–T14 independent authors freeze test-only diffs | Focused expected failures recorded; files frozen      |
| W1   | T2 store seam and T3 media                         | Affected checks and high-risk review pass             |
| W2   | T4 kernel and T5 sanitizer wiring                  | Integrate together; choke-point tests and review pass |
| W3   | T6–T11 disjoint route files                        | Route tests pass; every construction-only 501 is gone |
| W4   | Run frozen T12–T14 suites; authors fix findings    | Full suites pass; fixes receive independent re-review |
| W5   | T15 traceability and handoffs                      | Every owned row has state and exact evidence          |
| W6   | Two phase gates                                    | Candidate is unchanged and every criterion passes     |

W3 file authoring may run in parallel, but its build, race, database, and S3
verification commands queue in at most two batches of three workers. The
integration owner's full `make ci` runs alone.

After all six W3 files land, W4 begins when the integration owner runs Task 13's
frozen `writesafety_adversarial_test.go` with its exact focused live-database
command from Task 9. Its named wire-version case proves a v1 entry upsert
down-emits, applies, up-accepts, and persists current v2. The frozen media cases
prove crop preserves its transaction-read key without object I/O and whole-
resume delete removes only its canonical transaction-returned photo after
commit; an invalid or cross-resume key makes no backend call. Individual W3 task
gates do not depend on a sibling task that may still be in progress.

WF occurs before any T2–T11 implementation author reads an implementation diff.
The blind authors receive only approved authorities and plan interfaces. Their
files remain frozen through W4; findings return to the implementation authors,
and fresh reviewers re-review fixes.

OpenAPI lands contract-first in T1. Later handlers add conformance tests but do
not edit the contract. If implementation proves it wrong, the task stops and the
integration owner lands a dedicated reviewed contract correction.

## Acceptance ownership

P2B owns AC-SAVE-001/002/004/005, the P2B half of AC-SEC-003, and
AC-MEDIA-001…006/008/009. AC-MEDIA-007 belongs to P8-priv; P2B supplies its
paginated backend seam. P2B adds HTTP evidence without re-owning P2A document
rows. These rows exist before dispatch; Task 15 fills evidence and state rather
than creating criteria after the implementation.

The phase exit is defined in [exit criteria](exit-criteria.md). A fresh catalog
author owns `acceptance-catalog-r1.md`, derives it from the approved design,
ADRs, budgets, and traceability rows, and freezes it before W6. The acceptance
worker never edits it. A future correction uses the next revision; it never
rewrites a completed catalog.

## Reference documents

- [API design](../../design/api.md), [data design](../../design/data.md), and
  [deployment design](../../design/deployment.md)
- [HTTP write path](http-contract.md), [file structure](file-structure.md), and
  [integration handoffs](integration-handoffs.md)
- [Numeric budgets](../budgets.md) and [traceability](../traceability/README.md)
- [ADR 0011](../../adr/0011-risk-tiered-delivery-gates.md),
  [ADR 0016](../../adr/0016-transactional-idempotency.md), and
  [ADR 0019](../../adr/0019-private-media-delivery.md)
