# Phase 2B — Resume HTTP API and media

Status: **Revision 3, adopted and dispatchable** (2026-08-12).

This phase adds the authenticated resume HTTP surface, granular autosave
commands, full write-safety enforcement, document-version negotiation, and
private resume photos. It does not add publishing, public resume reads, SSE, the
editor, PDF generation, or cloud resources.

Revision 3 integrates the approved [v4 design](../../design/README.md), ADRs
0014–0019 and 0024, and the media acceptance rows and budgets.

## Preconditions

P0F, P1.1, and P2A are complete, so this phase runs as the server lane beside
P3's renderer lane. The two lanes own disjoint paths: P2B owns `apps/server/**`
and `docs/api/openapi.yaml` and never edits `packages/schema/**`.

| Input                    | Required state                                                               |
| ------------------------ | ---------------------------------------------------------------------------- |
| Go sanitizer package     | Landed in P3 Task 2; Task 5 wires it at the write boundary                   |
| Document schema v2       | Released by P3 Task 5B — the only cross-lane sync point, needed by Task 9    |
| Numeric inputs           | `budgets.md`, AC-MEDIA-001…009, and AC-SAVE-005                              |
| Media benchmark manifest | Task 11 fixture hashes, local profile, Graviton target, command, and samples |

Tasks 1–8 and 10–11 depend only on the landed sanitizer. Task 9 and the
version-negotiation cases in Task 1 wait for schema v2. P2B does not duplicate
or defer the sanitizer.

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
- Object upload precedes the database update. A proved-created candidate is
  compensated after a definite database failure. A remote `Put` with an unknown
  outcome is never deleted by the request and never enters a database mutation;
  a crash or unknown orphan stays private until the bounded scheduled sweep in
  AC-MEDIA-007 proves it unreferenced.
- Photo replacement and deletion commit reference revocation with one exact-key
  deletion job. The job is cleanup state, not a second media ownership model.

[Decisions](decisions.md) records the mechanism and rejected alternatives.
[ADR 0019](../../adr/0019-private-media-delivery.md) owns the media trust and
failure boundary.

## Migration and shared-file rules

P2B adds only migration 00006 for bounded idempotency retention and the durable
media deletion-job ledger. Photo ownership stays in the document. Any other
schema change stops for a new serialized design and migration review.

The integration owner authors Task 2's exclusive migration,
`apps/server/sql/queries.sql`, and regenerated `internal/store/**` window. Task
1 alone owns OpenAPI; the integration owner regenerates the web client. Task 3
reports exact dependency changes; the integration owner applies `go.mod` and
`go.sum` together in the exclusive dependency window. The integration owner owns
the root Makefile, workflows, manifests, and shared scripts.

## Task index

| Task | Deliverable                                        | Tier   | State   |
| ---- | -------------------------------------------------- | ------ | ------- |
| 1    | Complete OpenAPI contract and client regeneration  | Normal | Landed  |
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
| Gate | Phase review and exit checklist                    | —      | Pending |

The adversarial suites are folded into the tasks that own the behavior; their
cases live in [adversarial coverage](adversarial-coverage.md).

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

## Execution waves

| Wave | Parallel work                     | Gate before next wave                                 |
| ---- | --------------------------------- | ----------------------------------------------------- |
| W0   | T1 contract                       | Contract review and API drift gate                    |
| W1   | T2 store seam and T3 media        | Affected checks pass                                  |
| W2   | T4 kernel and T5 sanitizer wiring | Integrate together; choke-point tests pass            |
| W3   | T6–T11 disjoint route files       | Route tests pass; every construction-only 501 is gone |
| W4   | Phase review and exit checklist   | Candidate is unchanged and every criterion passes     |

W3 file authoring runs in parallel, but its build, race, database, and S3
verification commands queue in at most two batches of three workers. The
integration owner's full `make ci` runs alone. Individual W3 task gates do not
depend on a sibling task that may still be in progress.

Each task carries its slice of [adversarial coverage](adversarial-coverage.md).
The wire-version case proves a v1 entry upsert down-emits, applies, up-accepts,
and persists current v2. The media cases prove crop preserves its
transaction-read key without object I/O, and whole-resume delete removes only
its canonical transaction-returned photo after commit while an invalid or
cross-resume key makes no backend call.

OpenAPI lands contract-first in T1. Later handlers add conformance tests but do
not edit the contract. If implementation proves it wrong, the task stops and the
integration owner lands a dedicated reviewed contract correction.

## Acceptance ownership

P2B owns AC-SAVE-001/002/004/005, the P2B half of AC-SEC-003,
AC-MEDIA-001/002/004/005/008/009, and the P2B slices of the cross-phase
AC-MEDIA-003/006 rows. P8-priv closes AC-MEDIA-003/006/007; P2B supplies their
transactional queue and paginated backend seams. P2B adds HTTP evidence without
re-owning P2A document rows. These rows exist before dispatch; W4 fills evidence
and state rather than creating criteria after the implementation.

The phase exit is [exit criteria](exit-criteria.md), run with `make ci` and
connected `make scan` at one unchanged candidate commit.

## Reference documents

- [API design](../../design/api.md), [data design](../../design/data.md), and
  [deployment design](../../design/deployment.md)
- [HTTP write path](http-contract.md), [file structure](file-structure.md), and
  [integration handoffs](integration-handoffs.md)
- [Numeric budgets](../budgets.md) and [traceability](../traceability/README.md)
- [ADR 0011](../../adr/0011-risk-tiered-delivery-gates.md),
  [ADR 0016](../../adr/0016-transactional-idempotency.md), and
  [ADR 0019](../../adr/0019-private-media-delivery.md)
