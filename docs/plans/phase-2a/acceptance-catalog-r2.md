# Phase 2A automated acceptance catalog — revision 2

Status: **Frozen** (2026-08-12). Corrections require revision 3; this file does
not change during a run.

This revision replaces the legacy [`../uat-phase-2a.md`](../uat-phase-2a.md) for
future runs. The old catalog remains immutable. Revision 2 removes retired Atlas
checks, does not stop the shared database, uses the two-gate model from ADR
0011, and retains the independently derived Suite C bounds matrix beside the
author TDD matrix.

## Run rules

Record the commit, branch, clean status, tool versions, database image digest,
migration head, configuration names without values, start/end times, state
changes, and retry count. Use the shared test database through
`make test-db-up`; never stop it. Every live test must run with `-count=1` and
must fail closed rather than skip when its required service is absent.

The worker may run checks and read evidence. It must not edit code, tests,
fixtures, snapshots, seeds, criteria, or this catalog.

## Criteria

| ID        | Expected result                                                                                     | Acceptance ownership                |
| --------- | --------------------------------------------------------------------------------------------------- | ----------------------------------- |
| P2A-R2-01 | `make server-test-db` runs every required resume/store suite with no skip and fails if DB is absent | All P2A rows                        |
| P2A-R2-02 | Migration and sqlc checks reach `00005`; released migrations are unchanged                          | AC-DOC-001                          |
| P2A-R2-03 | Direct SQL and concurrent creation permit exactly three resumes                                     | AC-DOC-001                          |
| P2A-R2-04 | Title limits and owner/unknown probes reject without any row change                                 | AC-DOC-001                          |
| P2A-R2-05 | Go and TypeScript enforce every document and aggregate bound, including cleared values              | AC-DOC-002/003/004/007/008/009/011  |
| P2A-R2-06 | Concurrent revision CAS has one winner; every loser receives current truth                          | AC-SAVE-001                         |
| P2A-R2-07 | Idempotency replay, reuse rejection, concurrency, rollback, and expiry cleanup are transactional    | AC-SAVE-003                         |
| P2A-R2-08 | Projection is read-pure; backfill preserves revision and loses safely to concurrent writes          | AC-DOC-010                          |
| P2A-R2-09 | v1 schema/types are immutable; registries, converters, and unknown-version rejection pass           | AC-DOC-012                          |
| P2A-R2-10 | Independent suites A, B, and C retain author independence and pass their specified gates            | AC-DOC-001/004/010/011, AC-SAVE-003 |
| P2A-R2-11 | `make ci` and `make scan` pass at the exact candidate commit                                        | All P2A rows                        |
| P2A-R2-12 | Write-path policy, traceability states, evidence links, and P2B handoffs are closed                 | All P2A rows                        |
| P2A-R2-13 | Released migrations/schemas are append-only and no secret appears in source, logs, or evidence      | Supply chain/privacy                |

## Report

Report one row per criterion with expected result, observed result, exact
commands or queries, evidence path, and `PASS | FAIL | BLOCKED`. Record every
retry, container change, seed, and manual database mutation. Missing or
cross-commit evidence fails. Never replace a required check with a weaker one.
