# Task 10.16 — Complete product UAT

**Owner:** integration owner, using the scripted hosted harness. **Inputs:**
task 10.15 deployment, task 10.14 harness, the owner's SES handoff, the
candidate commit and image digests, and stable acceptance IDs.

## Workflows

| Area              | Required acceptance                                                                                                                                            |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Password and mail | Register, verify, sign in/out, reset, recent reauthentication, sessions/revocation, real SES delivery and expected failure handling                            |
| v1 flags          | Provider login absent when disabled; server starts without unused provider credentials; MCP enabled                                                            |
| MCP               | Discovery, registration, consent, token exchange/rotation/revoke, authenticated resume changes, denied expired/revoked grants; edge preserves Bearer semantics |
| Editing           | Create/rename/delete, structure and content, autosave/CAS races, reload/refetch, photos, templates, mobile layout, and accessibility                           |
| Publication       | Publish, rename, discoverability, download flag, public HTML/JSON/photo/Markdown/PDF/images; unpublish and deletion revoke later reads                         |
| Realtime          | Owner/public SSE, reconnect refetch, slow-client bounds, polling fallback, and stream closure on unpublish                                                     |
| Rendering         | Owner and public exports, deterministic fonts/photos, sanitizer conformance, timeout/cancellation, queue limits, and no renderer outbound network              |
| Privacy           | Account export/deletion, session and grant revocation, exact-key media cleanup, retention, orphan reconciliation, and idempotency expiry                       |

Use the six established visual presets: `classic-serif`, `engineer-compact`,
`modern-sidebar`, `executive-band`, `consulting-formal`, and `academic-dense`.
Changing the set requires its renderer screenshot contract to change too.

## Execution

1. Verify the deployed commit, image digests, migration baseline/head, UAT
   hostname, account/environment identity, and Phase 9 spending/lifetime limits.
2. Run each workflow through real browser actions where a UI exists. MCP uses
   its protocol client; API and read-only database checks may verify results.
3. Record expected/observed outcomes and `PASS`, `FAIL`, or `BLOCKED`, with
   private evidence described in [task 10.17](evidence.md). Missing evidence and
   blocked required workflows prevent phase exit.
4. Preserve failures. Fix defects with regression tests and a new candidate;
   repeat affected checks and every now-stale UAT result. Never retry a flaky
   test into a reported pass.
5. Remove only this run's disposable fixtures using the reviewed cleanup path.
   Record remaining resources and their planned lifetime for task 10.17.

Before dispatch, bind these workflows to exact spec files, commands, acceptance
IDs, and fixture ownership. Record corrections to wrong criteria in this phase;
there is no frozen acceptance catalog or separate evidence-export subsystem.
