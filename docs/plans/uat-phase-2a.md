# Phase 2A — UAT acceptance catalog

> **Authored by the integration owner before the run and immutable during a
> run.** The UAT worker executes this catalog and reports results; it must not
> edit this file, product code, tests, snapshots, seeds, or acceptance criteria.
> `BLOCKED` counts as `FAIL`. See `implementation-plan.md` ("UAT report
> contract").

Phase 2A ships the resume data layer, not HTTP handlers or editor UI. This
catalog therefore probes the real migrated Postgres schema and public Go store
interfaces. HTTP write behavior, media, and browser flows remain P2B/P4 scope.

## Run preconditions

Record commit SHA, branch, clean `git status --short --branch`, Go/Node/podman
versions, container image digests, migration head, and configuration variable
names (never values). All evidence must come from that exact commit. A later
product-code commit invalidates every row that exercises a changed path. The
integration owner runs `make docs-fmt` before pinning the candidate; the UAT
worker performs only read-only/check targets and starts from a clean tree.

Use the dedicated test database, not the development database. Run
`make test-db-down`, then `make test-db-up`, and prove `REQUIRE_TEST_DB=1` is in
effect. Record every skip, retry, container restart, seed, and manual mutation.

## Acceptance scenarios

| ID         | Scenario                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Acceptance IDs                                              |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| UAT-P2A-01 | **The live acceptance suites actually run.** Run `make server-test-db`; record PASS/SKIP/FAIL counts. Any skipped resume, store, migration, or concurrency test fails this row. With the test database unavailable, the same target must fail closed rather than replaying cached success.                                                                                                                                                                           | all                                                         |
| UAT-P2A-02 | **Schema reaches the expected head from empty and previous head.** `00004_add_resume_tables` and `00005_add_resume_cap_trigger` exist after migration; `make server-migration-test`, `make sqlc-check`, and `make data-drift` pass with `-count=1` live-DB behavior.                                                                                                                                                                                                 | AC-DOC-001                                                  |
| UAT-P2A-03 | **Database constraints are real.** Direct SQL proves every new FK/check/unique constraint, the 3-resume trigger, and the raw-SQL bypass/concurrent-insert cases. Exactly three of 20 concurrent creates succeed and the final count is three.                                                                                                                                                                                                                        | AC-DOC-001                                                  |
| UAT-P2A-04 | **Title and ownership boundaries fail without writes.** Create/SaveTitle accept empty and exactly 160 Unicode code points, reject 161, and preserve row count/full row on rejection. For Get/Delete/SaveDocument/SaveTitle, a wrong owner is indistinguishable from an unknown id.                                                                                                                                                                                   | D5/`budgets.md`; D17 security invariant                     |
| UAT-P2A-05 | **Every document constraint reaches the live write choke point.** The schema-derived matrix accepts each bound and rejects limit+1, including canonical total size 512 KB/512 KB+1, multibyte rich text, sections, entries, details, and all distinct string bounds. Go and TS verdicts match the committed shared corpus; the present cleared-contact entry/value round-trips through live writes; rejected Create/SaveDocument operations change no row.           | AC-DOC-002/003/004/007/008/009/011                          |
| UAT-P2A-06 | **Revision CAS has one winner and carries truth to losers.** Concurrent same-revision SaveDocument calls yield exactly one R+1 winner; every loser receives the winning current revision/document. Title-only CAS behaves equivalently, and nonexistent/wrong-owner rows reveal no existence oracle.                                                                                                                                                                 | AC-SAVE-001                                                 |
| UAT-P2A-07 | **Idempotency is transactional.** A repeated key/hash never reruns mutation and returns the persisted response representation; same-key concurrent real CAS requests serialize and converge on that response; a different hash converges on key reuse with zero loser writes; mutation failure leaves neither mutation nor record; expired records are replaced and the caller's expired records remain reaped even on error.                                        | AC-SAVE-003                                                 |
| UAT-P2A-08 | **Projection reads are pure and backfill races are safe.** Reads of old rows return current-shape documents without changing bytes/revision/time; SaveDocument persists current shape; backfill uses schema-version+revision CAS, leaves user-visible revision/time unchanged, loses cleanly to autosave/title changes, and succeeds on a fresh retry. A bad row makes List fail atomically with no partial result.                                                  | AC-DOC-010                                                  |
| UAT-P2A-09 | **Wire compatibility exists from v1.** The immutable v1 schema, retained version-scoped Go/TS types, and registries are present and append-only. Accepted/emitted sets are explicit. Synthetic v1⇄v2 adjacent converters validate source and target; an old-client input is prepared as current canonical shape without field loss, declared down-emission succeeds, and every missing/unknown path fails closed. Real HTTP persistence is the P2B AC-SAVE-004 gate. | AC-DOC-012                                                  |
| UAT-P2A-10 | **Independent suites remain independent.** Suites A, B, and C exist, pass under their specified race/count gates, and repository history/evidence shows three fresh authors derived them from the frozen inputs before reading implementation tests. Any unexplained edit by an implementation author fails this row.                                                                                                                                                | AC-DOC-001/004/010/011, AC-SAVE-001/003                     |
| UAT-P2A-11 | **Full quality gate.** Run `make server-build server-vet server-test server-test-db sqlc-check data-drift server-migration-test schema-check semgrep-policy-test semgrep docs-lint`. From `apps/server`, run `GOLANGCI_LINT_CACHE="$(mktemp -d)" GOCACHE="$(mktemp -d)" golangci-lint run ./...` and `govulncheck ./...`. Record exact output; any warning treated as success by a wrapper must be investigated.                                                     | all                                                         |
| UAT-P2A-12 | **Write-path and traceability closure.** A mechanical restriction prevents packages outside `internal/resume` from calling generated resume write methods. Traceability contains concrete passing test references for every P2A-owned row, and P2B handoffs name full-document persistence, customization allowlisting, idempotent CSRF retry, and HTTP old-client compatibility.                                                                                    | AC-DOC-001/002/003/004/007/008/009/010/011/012, AC-SAVE-003 |
| UAT-P2A-13 | **Released contracts are append-only and secrets stay absent.** Compared with the phase base, migration SQL and `resume.v*.schema.json` have only allowed additions; no released schema/migration was modified/deleted/renamed. Every released schema retains derived Go/TS types and `schema-check` proves regeneration is clean. `.env` is untracked, and no credential/token value appears in tracked files, logs, or evidence artifacts.                         | supply-chain and privacy gates                              |

## Reporting

Report one row per ID with expected result, observed result, exact command or
query, evidence path, and `PASS` / `FAIL` / `BLOCKED`. Missing evidence,
undisclosed retries or state changes, skipped required tests, unexplained error
output, or evidence from a different commit fails the row. Never substitute a
weaker check: if the required environment or provenance cannot be established,
record `BLOCKED`, which counts as `FAIL`.

## Corrections log

Corrections apply only to future runs. A completed run's verdicts never change;
an unsatisfiable criterion remains `BLOCKED` for that run and may be corrected
only afterward with written rationale and independent adjudication.
