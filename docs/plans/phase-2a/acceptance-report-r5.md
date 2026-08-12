# Phase 2A automated acceptance report — revision 5

Verdict: **PASS**. All 13 frozen rows passed at candidate `2ce66d36b7aab2f9814c4e894b937c5e80bcb520`
with zero retries, zero manual data mutations, and no repository change.

## Run identity

| Field                    | Recorded value                                                                                                                         |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| Candidate                | `2ce66d36b7aab2f9814c4e894b937c5e80bcb520`                                                                                             |
| Branch                   | `main`                                                                                                                                 |
| Worktree                 | Clean before and after owner gates, at worker start, and at final audit                                                                |
| R4 catalog SHA-256       | `5657b26db6e806d0ae76127e6a9de1f4db52a838dd0bb388fb03723baf51557b`                                                                     |
| R5 catalog SHA-256       | `364e28ad85d1baf61974d27bea162c76ebeaa0b819ea6bf972c787b84e56402b`                                                                     |
| Suite provenance SHA-256 | `99a62d7f471f718b5b1d5beea64a7d9e6abb3ca1b4007d01ec60bc3da3ff3e9d`                                                                     |
| Owner start              | `2026-08-12T21:47:42+07:00`                                                                                                            |
| Worker start             | `2026-08-12T22:03:32+07:00`                                                                                                            |
| Worker capture end       | `2026-08-12T22:03:54+07:00`                                                                                                            |
| Shells                   | Owner and worker each used GNU Bash 5.3.9; the worker used one process                                                                 |
| Retry count              | `0`                                                                                                                                    |
| Manual mutation count    | `0`                                                                                                                                    |
| State changes            | `make test-db-up` reused the running shared database; no stop, recreation, repository edit, remote access, or manual database mutation |

## Tool and database identity

The pinned checks recorded Node.js 24.19.0, Go 1.26.5, sqlc 1.31.1,
golangci-lint 2.12.2, govulncheck 1.6.0, Caddy 2.11.4, Semgrep 1.172.0,
and gitleaks 8.30.1.

Capture 04 recorded the running `aboutme-test-db` image ID and digest, its
536870912-byte memory limit, host port 20432, readiness, and migration head
`5\|true`. Capture 11 recorded the same running container, memory limit, port,
and migration head at the final identity audit.

## Capture record

Every command below recorded literal `exit_status: 0`.

| Evidence                   | Started                     | Ended                       | Status | SHA-256                                                            |
| -------------------------- | --------------------------- | --------------------------- | -----: | ------------------------------------------------------------------ |
| `00-defect-review.md`      | 2026-08-12                  | 2026-08-12                  |   PASS | `3cc9019bbdbe0b822ea8e8e656a8131f32c0e29d3a98455e160077987dbd1675` |
| `01-ci.log`                | `2026-08-12T21:47:42+07:00` | `2026-08-12T21:48:36+07:00` |    `0` | `c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953` |
| `02-scan.log`              | `2026-08-12T21:48:36+07:00` | `2026-08-12T21:50:33+07:00` |    `0` | `ceee6a9d57bc76575cd8fae9dfe6a17fd9c40bd0a17ef94593b371a50cfcfddb` |
| `03-identity.log`          | `2026-08-12T22:03:32+07:00` | `2026-08-12T22:03:33+07:00` |    `0` | `389ea134596e9b5e7c1f4145b640f1b289b338669c728e2328c89b1de08f66da` |
| `04-database.log`          | `2026-08-12T22:03:33+07:00` | `2026-08-12T22:03:33+07:00` |    `0` | `27f5a772c5abde558ef599d2b3e5ad7a69eaa148daa532ca342ce00448ca6647` |
| `05-cap.log`               | `2026-08-12T22:03:33+07:00` | `2026-08-12T22:03:33+07:00` |    `0` | `9fbfd49af4c4ab3bdb1ddad989acadc8e3aaeb0c49c1ea0c8a83651fe35f7cb5` |
| `06-suite-a.log`           | `2026-08-12T22:03:33+07:00` | `2026-08-12T22:03:41+07:00` |    `0` | `4054507296f862377ce4ed757a2862de20db175729674bba5bb6090c90121312` |
| `07-suite-b.log`           | `2026-08-12T22:03:41+07:00` | `2026-08-12T22:03:49+07:00` |    `0` | `ad5f8c754c78f3fe716f47d2d1454057c546c6a98a3a4d3417afd4de5584ea96` |
| `08-cas-bounds.log`        | `2026-08-12T22:03:49+07:00` | `2026-08-12T22:03:50+07:00` |    `0` | `dbceaaa8aa75c1010a90444ca64dfdc2cbfb3668223831374989e671e22a89c3` |
| `09-security-contract.log` | `2026-08-12T22:03:50+07:00` | `2026-08-12T22:03:52+07:00` |    `0` | `f0ef2b4aac6e4ceaed99a7a5ac679777e13a55dd014cafefb237c4a40afcb5ce` |
| `10-closure.log`           | `2026-08-12T22:03:52+07:00` | `2026-08-12T22:03:53+07:00` |    `0` | `f3da0f07b8607dbc4ff5f528292adb6b6e983ae1d6944fe677d629a8ac1ff6c7` |
| `11-secret-final.log`      | `2026-08-12T22:03:53+07:00` | `2026-08-12T22:03:54+07:00` |    `0` | `b3cfdfc25eab78142055e0f23945bbc066bbbdeb00c91dc1950cbde924681627` |

## Acceptance rows

| ID        | Observed result                                                                                                                                                             | Evidence                                                                                                                                                                                                                                                                                                                                                               | Verdict |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| P2A-R5-01 | The single owner `make ci` passed every local and live-database group at the unchanged clean candidate.                                                                     | `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`)                                                                                                                                                                                                                                                                                       | PASS    |
| P2A-R5-02 | Migration and sqlc gates reached corrected migration 00005; the database was at `5\|true`; pre-UAT and future-marker guards passed.                                         | `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`); `04-database.log` (`27f5a772c5abde558ef599d2b3e5ad7a69eaa148daa532ca342ce00448ca6647`); `10-closure.log` (`f3da0f07b8607dbc4ff5f528292adb6b6e983ae1d6944fe677d629a8ac1ff6c7`)                                                                                                        | PASS    |
| P2A-R5-03 | Focused cap tests passed; all 20 concurrent store and raw-SQL iterations enforced exactly three resumes.                                                                    | `05-cap.log` (`9fbfd49af4c4ab3bdb1ddad989acadc8e3aaeb0c49c1ea0c8a83651fe35f7cb5`); `06-suite-a.log` (`4054507296f862377ce4ed757a2862de20db175729674bba5bb6090c90121312`)                                                                                                                                                                                               | PASS    |
| P2A-R5-04 | Full store validation and no-oracle suites passed; focused bounds and stale-title CAS checks passed without writes.                                                         | `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`); `08-cas-bounds.log` (`dbceaaa8aa75c1010a90444ca64dfdc2cbfb3668223831374989e671e22a89c3`)                                                                                                                                                                                             | PASS    |
| P2A-R5-05 | Schema, Go, TypeScript, live-store, bounds, completeness, corpus, UTF-8, and canonical-byte gates passed.                                                                   | `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`); `08-cas-bounds.log` (`dbceaaa8aa75c1010a90444ca64dfdc2cbfb3668223831374989e671e22a89c3`)                                                                                                                                                                                             | PASS    |
| P2A-R5-06 | Document CAS had one winner and current-truth losers in 20 iterations; sequential stale-title CAS returned current truth.                                                   | `06-suite-a.log` (`4054507296f862377ce4ed757a2862de20db175729674bba5bb6090c90121312`); `08-cas-bounds.log` (`dbceaaa8aa75c1010a90444ca64dfdc2cbfb3668223831374989e671e22a89c3`)                                                                                                                                                                                        | PASS    |
| P2A-R5-07 | Idempotency replay, reuse, concurrency, rollback, cap composition, cleanup, and different-body non-execution passed.                                                        | `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`); `06-suite-a.log` (`4054507296f862377ce4ed757a2862de20db175729674bba5bb6090c90121312`)                                                                                                                                                                                                | PASS    |
| P2A-R5-08 | Projection reads stayed pure; all focused backfill races passed in 20 iterations; the pure projector suite passed.                                                          | `07-suite-b.log` (`ad5f8c754c78f3fe716f47d2d1454057c546c6a98a3a4d3417afd4de5584ea96`)                                                                                                                                                                                                                                                                                  | PASS    |
| P2A-R5-09 | Released-v1 guards, registries, converters, persistence, declaration independence, and unknown/lossy rejection passed.                                                      | `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`); `07-suite-b.log` (`ad5f8c754c78f3fe716f47d2d1454057c546c6a98a3a4d3417afd4de5584ea96`)                                                                                                                                                                                                | PASS    |
| P2A-R5-10 | Current and delivered suite hashes, retained history, and ancestry matched; Suite A/B stress and Suite C passed.                                                            | `06-suite-a.log` (`4054507296f862377ce4ed757a2862de20db175729674bba5bb6090c90121312`); `07-suite-b.log` (`ad5f8c754c78f3fe716f47d2d1454057c546c6a98a3a4d3417afd4de5584ea96`); `08-cas-bounds.log` (`dbceaaa8aa75c1010a90444ca64dfdc2cbfb3668223831374989e671e22a89c3`); `10-closure.log` (`f3da0f07b8607dbc4ff5f528292adb6b6e983ae1d6944fe677d629a8ac1ff6c7`)          | PASS    |
| P2A-R5-11 | Fresh review, one CI run, and one connected scan passed without retry. Semgrep used Code, SCA, and the Pro Secrets engine with zero findings; full-history gitleaks passed. | `00-defect-review.md` (`3cc9019bbdbe0b822ea8e8e656a8131f32c0e29d3a98455e160077987dbd1675`); `01-ci.log` (`c966b3d3af94723c03ed579cf5c4c70dd0912da074e2aa030d3ac8a76feb2953`); `02-scan.log` (`ceee6a9d57bc76575cd8fae9dfe6a17fd9c40bd0a17ef94593b371a50cfcfddb`)                                                                                                       | PASS    |
| P2A-R5-12 | The write-path boundary, evidence-rich traceability, and named P2B/P8 handoffs were complete; AC-DOC-001 awaited only this run.                                             | `00-defect-review.md` (`3cc9019bbdbe0b822ea8e8e656a8131f32c0e29d3a98455e160077987dbd1675`); `10-closure.log` (`f3da0f07b8607dbc4ff5f528292adb6b6e983ae1d6944fe677d629a8ac1ff6c7`)                                                                                                                                                                                      | PASS    |
| P2A-R5-13 | All dependency inputs were selected; scan, workflow, migration, schema, source-history, evidence-secret, and exact-value audits passed.                                     | `02-scan.log` (`ceee6a9d57bc76575cd8fae9dfe6a17fd9c40bd0a17ef94593b371a50cfcfddb`); `09-security-contract.log` (`f0ef2b4aac6e4ceaed99a7a5ac679777e13a55dd014cafefb237c4a40afcb5ce`); `10-closure.log` (`f3da0f07b8607dbc4ff5f528292adb6b6e983ae1d6944fe677d629a8ac1ff6c7`); `11-secret-final.log` (`b3cfdfc25eab78142055e0f23945bbc066bbbdeb00c91dc1950cbde924681627`) | PASS    |

This report covers captures through 11. Capture 12 and the final manifest record
its immutable hash and the post-report secret audit.
