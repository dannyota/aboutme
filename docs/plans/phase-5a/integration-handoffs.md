# Phase 5A integration handoffs

## Serialized integration-owner windows

The root-owned cross-phase order is Phase 4 T00 → Phase 5A T00 → Phase 5A T01 →
Phase 5A T04 → dependent Phase 4 transport/public work; Phase 5A T09/W4b is
after Phase 4 T00 and before final browser windows; Phase 4 T15 precedes Phase
5A T12. Disjoint work may overlap, but no phase-local owner may invert those
shared-path windows.

| Window                      | Owned change                                                                                                                                                                                                                             | Release gate                                                                                               |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| W0a T00 topology/routes     | After Phase 4 T00: exact 14-row registry/dispatch; two source manifests/digests; render network/fixed edge bind; Compose/native/HTTPS origins; Web server copy; verified Caddy inline/mount/hash; browser/network denial and ETag parity | Generator/topology/HTTPS/browser/toolchain/CI-adversarial/Make-safety checks, then `make route-table-test` |
| W0b T01 migration/sqlc      | Migration 00007, SQL queries, generated store, rollback/concurrency proof                                                                                                                                                                | `make sqlc-check server-test-db server-test-integration server-migration-test` with live tests `-count=1`  |
| W0c T04 OpenAPI/client      | After T01: owner flags, publish/public schemas and routes, generated TypeScript                                                                                                                                                          | `make api-check web-typecheck`                                                                             |
| W4b Web worker dependencies | After Phase 4 T00 and before final browser windows: exact `npm install --save-dev --save-exact vite@8.2.0 @vitejs/plugin-vue@6.0.8` in `apps/web`; regenerated lockfile                                                                  | Resolved versions reported; `make web-lint web-typecheck web-test web-build`                               |
| W6 T11 composition          | Router/config/main readiness and public dispatch after all Go interfaces land                                                                                                                                                            | Focused Go tests plus `make server-build server-vet server-test`                                           |
| W7 T12 evidence             | After Phase 4 T15: fixture command, capture script, root Make target; preserve all Phase 4 HTTPS scenarios                                                                                                                               | Static script tests, then one `make p5a-native-http-check`                                                 |
| W8 records                  | Master plan/index, architecture, runbook, final trace states/evidence                                                                                                                                                                    | Commit records locally; focused Prettier/markdownlint; reread commit; then fresh review                    |

No W1–W5 worker edits a manifest, lockfile, migration, generated store/client,
Compose/Caddy/native harness, root Makefile, or master record.

## Exact handoffs

The producer and every consumer use these names unchanged:

| Producer | Consumers      | Exact interface                                                                                                                        |
| -------- | -------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| 00       | 06, 11         | `publicroots.Reserved(string) bool`; generated dispatch fixture and Caddy fragment                                                     |
| 01       | 05, 07, 08, 11 | Task 01 exact `store.PublicReadQueries`/`PublicMutationQueries`, generated parameter/result types, and compile assertion               |
| 02       | 05, 07, 11     | D3 `Representation`, constants, `ResumeTarget`, `Plan`, `CommittedState`, recovery types, `Coordinator`, `Lease`, and `Transition`     |
| 03       | 07             | D4 existing `StoredResponse`/`CommitOutcome`/`ExecuteResult`/`Execute` plus exact `RecheckDecision`, `RecheckResult`, and `Recheck`    |
| 04       | 05, 06, 09     | OpenAPI `PublicResume`/document leaf wire authority and `PublishResumeRequest`                                                         |
| 05       | 08, 10, 11     | D2 `PublicOrigin`, details presence, `PublicResume`, `Snapshot`, `Project`, `Reader`; D10 cache and selected-response signatures       |
| 06       | 07             | `publishInput`, `publishPrepared`, `decodePublish`, `validatePublish`, `slugAttemptLimiter` in package `resumeapi`                     |
| 07       | 11             | Transition-aware `resumeapi.Service`, registered publish route, recovery resolver, and exact `Options.Coordinator`/`RecoveryPool` seam |
| 08       | 09, 10         | D5 format functions and `JSONLDResult`; shared exact goldens                                                                           |
| 09       | 10             | D7 four-field TypeScript request and worker runtime; external asset paths/build digest inputs                                          |
| 10       | 11             | D6 `RenderOrigin`, `PublicRenderRequest`, `Result`, `Client`; D10 `HTMLDependencies` and `NewHTMLHandler`                              |
| 11       | 12             | D8 `PublicRoutes`, `ReadinessDependencies`, `NewReadiness`, composite readiness, and complete server composition                       |

An interface mismatch returns to its producer. Consumers do not alias, rename,
wrap around, or locally re-declare it.

## Task report format

```text
Phase/task: P5A TNN — exact task title
Owned paths: complete path list
Acceptance: AC IDs; primitive/integration race coverage
RED: exact command, failing assertion, expected reason
GREEN: exact command and result
Adversarial cases: named cases proved
Shared edits requested: exact reserved path/edit, or "none"
Unrun checks: exact command, reason, remaining uncertainty, or "none"
Risks/notes: remaining fact, or "none"
Suggested commit: Conventional Commit subject
```

The owner rereads each owned diff and reruns its key GREEN command before
staging exact paths. Workers do not run `make ci`, `make scan`, or Git.

## Record and review order

Before T00 dispatch, the root integration owner commits this plan and marks its
graph active in the master plan/index. Completion follows this separate order:

1. Accept T00–T12 reports and focused checks.
2. Update the master plan/index to actual completion and update architecture,
   runbook, trace states, and evidence.
3. Run focused document checks, commit those completion records locally, and
   verify the candidate includes that commit.
4. Dispatch one fresh reviewer who authored none of T00–T12.
5. Return findings to the owning author; the same reviewer confirms fixes.
6. At one unchanged candidate commit, run exit checklist, `make ci`, and
   connected `make scan` alone.

The fresh reviewer confirms CSRF/reauth, slug locks/tombstones, CAS/idempotency,
all 22 race rows, projection/sanitizer privacy, shared drain deadline, ambiguous
recovery/readiness, media privacy, untrusted render topology, worker termination
and join, exact bytes/CSP/ETags, dispatch order, cache keys, and traceability.
