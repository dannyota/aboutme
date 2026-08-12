# Phase 2A — Resume domain and store

Status: **Corrective verification in progress** (2026-08-12).

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
| 3    | Resume tables, constraints, and cap trigger       | Correction re-reviewed       |
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
| 12   | Traceability, docs, and handoffs                  | Landed                       |
| Gate | Phase defect review                               | Reopened                     |
| Gate | Exact-candidate phase acceptance                  | Pending revision 5           |

Task 12's traceability, architecture documentation, and named downstream
handoffs are landed. A post-closure review found that the cap trigger rejects a
no-op owner assignment at the cap and that revision 2 acceptance lacks its
required persisted per-row report. The pre-UAT migration correction is landed
and re-reviewed. The current candidate still needs a fresh phase review, the
owner gates, catalog revision 5 acceptance, and a documentation-only closure
commit before P2A returns to complete.

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

## Prior candidate gate sequence

1. A fresh reviewer inspected the complete phase for defects, design fit,
   interface stability, assumptions, and traceability.
2. Blocking findings were fixed and independently re-reviewed.
3. The integration owner pinned one unchanged candidate and froze
   [acceptance catalog revision 2](acceptance-catalog-r2.md).
4. `make ci` and connected `make scan` passed without concurrent heavy workers.
5. The required per-row acceptance report was not persisted. Revisions 3 and 4
   preserve the correction history. Revision 5 is active because it fixes R4's
   shell-specific capture-variable defect without changing any criterion.

The revision 5 run at `262a5b3668abab5951efd8de674ff54cb9f6c096` passed the
fresh defect review, `make ci`, connected `make scan`, and acceptance captures
03–11. Its worker process exited while assembling the report, before the report
and capture 12 existed. The zero-retry rule makes that attempt final and failed;
none of its otherwise-passing evidence closes the phase. The next run at
`bf45983a4c120dfa40f46230d736a26686784ab4` also passed every product and
security capture through 11, but its report table contained an unescaped pipe;
the required Markdown lint stopped the process before the report hash and
capture 12. That attempt is also final and failed. The next run uses a new
candidate and a new empty evidence directory.

## Corrective closure sequence

1. Commit a clean candidate containing catalog revision 5 and the Suite A/B/C
   provenance record. AC-DOC-001 remains `PENDING` until acceptance passes.
2. Record a fresh exact-candidate phase-defect review. Run `make ci` and
   connected `make scan` once at that unchanged candidate.
3. A fresh read-only worker runs catalog revision 5 and returns its external
   report. Any failed, blocked, missing, changed-candidate, or retry result
   fails closure.
4. If every row passes, persist the report and change traceability and active
   status pages in a second documentation-only closure commit. That commit cites
   the tested candidate and does not claim it was itself gated.

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
