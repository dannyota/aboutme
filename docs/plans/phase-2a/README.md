# Phase 2A — Resume domain and store

Status: **Task 12 and phase gates remain** (verified at `c67e47d`, 2026-08-12).

This phase owns the resume relational schema, validated aggregate store,
revision compare-and-swap (CAS), transactional idempotency, immutable document
versions, pure projection, and CAS backfill. It has no HTTP surface.

The phase began under a declarative SQL and Atlas plan.
[ADR 0010](../../adr/0010-goose-only-migrations.md) retired that workflow.
Released task records still describe it as history; current work uses
hand-written goose migrations as the only relational schema source.

## Current state

| Task | Deliverable                                       | State                        |
| ---- | ------------------------------------------------- | ---------------------------- |
| 1    | Historical Atlas drift-gate extension             | Superseded by ADR 0010       |
| 2    | Shared validation and embedded current schema     | Landed                       |
| 2b   | Immutable v1 schema and released-version registry | Landed                       |
| 3    | Resume tables, constraints, and cap trigger       | Landed                       |
| 4    | sqlc queries and generated store                  | Landed                       |
| 5    | Codec, validation, and bounds                     | Landed                       |
| 6    | Owner-scoped store and revision CAS               | Landed                       |
| 6a   | Cleared-contact round trip                        | Landed                       |
| 6b   | Generated-write boundary review                   | Superseded; review rule only |
| 7    | Transactional idempotency                         | Landed                       |
| 8    | Projection, converters, and CAS backfill          | Landed                       |
| 9    | Independent write-safety suite                    | Landed                       |
| 10   | Independent document-migration suite              | Landed                       |
| 11   | Bounds matrix and completeness guard              | Landed                       |
| 12   | Traceability, docs, and handoffs                  | Pending                      |
| Gate | Phase defect review and phase acceptance          | Pending                      |

`Landed` does not claim that the phase gates passed. P2B and P3 remain blocked
on this phase until Task 12 and both gates close at the exact candidate commit.

The production document registry currently contains only v1. P2A supplies pure
projection, adjacent-converter machinery tested with synthetic later versions,
and the transport-independent `AcceptWire`/`EmitWire` boundary. Its production
v1 test composes that boundary with complete-document store persistence. P2B
must consume it through the real old-version HTTP/OpenAPI contract after P3
releases v2; P2A does not simulate that missing HTTP path.

P2A's landed idempotency API is the transaction primitive that AC-SAVE-003 owns.
The Draft v4 operation tuple and semantic fingerprint, deterministic response
headers, bounded request-path cleanup and capacity accounting, and HTTP replay
contract remain P2B work. The authoritative hourly global expiry sweep remains
P8 privacy work.

## Remaining order

1. Update every P2A-owned traceability row with an explicit state and concrete
   test evidence. Verify every owner-only shared-file need and assign every
   forward handoff to a named owner and downstream gate.
2. Run the affected local checks and `make ci` once at the candidate commit.
3. Have a fresh reviewer inspect the complete phase diff for defects, design
   fit, interface stability, assumptions, and traceability.
4. Fix blocking findings and obtain independent re-review.
5. Freeze [acceptance catalog revision 2](acceptance-catalog-r2.md). A fresh
   acceptance worker runs it without editing code, tests, fixtures, or criteria.

## Authorities

- [Data design](../../design/data.md)
- [API write-safety design](../../design/api.md)
- [Document-version ADR](../../adr/0017-resume-document-versioning.md)
- [Idempotency ADR](../../adr/0016-transactional-idempotency.md)
- [Numeric budgets](../budgets.md)
- [Traceability](../traceability/README.md)

The detailed task files below are execution records. Completed files stay as
written even where a later ADR superseded their workflow.

## Task files

- [Decisions](decisions.md), [write path](write-path.md),
  [file structure](file-structure.md),
  [integration handoffs](integration-handoffs.md), and
  [exit criteria](exit-criteria.md)
- [Tasks 1–5](task-01-drift-gate.md),
  [2](task-02-schema-entry-id-uniqueness.md),
  [2b](task-02b-immutable-v1-schema.md),
  [3](task-03-resume-tables-and-migrations.md), [4](task-04-sqlc-queries.md),
  [5](task-05-codec-and-validation.md)
- [Tasks 6–8](task-06-resume-store.md), [6a](task-06a-cleared-contact-value.md),
  [6b](task-06b-write-chokepoint.md), [7](task-07-idempotency-store.md),
  [8](task-08-doc-shape-migration.md)
- [Independent suites 9](task-09-blind-suite-write-safety.md) and
  [10](task-10-blind-suite-doc-migration.md),
  [bounds task 11](task-11-blind-suite-bounds.md), and
  [closure task 12](task-12-traceability-closure.md)

## Acceptance ownership

P2A owns AC-DOC-001/002/003/004/007/008/009/010/011/012 and AC-SAVE-003. Its
store CAS tests are prerequisite evidence for P2B-owned AC-SAVE-001. P2B owns
the HTTP retry contract in AC-SAVE-002, real HTTP/OpenAPI old-version
persist/emission proof in AC-SAVE-004, and customization-delta allowlist in
AC-SAVE-005. P8 privacy owns the authoritative global idempotency-retention
sweep; P2A's active-user reap is only opportunistic.
