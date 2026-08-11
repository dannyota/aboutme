# Integration handoffs (owner-applied, not worker-applied)

Shared integration files belong to the main session alone. A worker that needs
one of these changed reports the exact change and stops.

| Shared file                     | Needed change                                                                                                                                                                                                                  | When           |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------- |
| `../budgets.md`                 | **Four new rows, before dispatch** (prerequisite, not a P2B worker edit): photo upload ≤ 2 MiB; resume read limit 600/min; resume write limit 240/min; media upload 20/h — all keyed account+IP, all enforced in P2B (D9, D14) | Before wave 1  |
| `../budgets.md`                 | Provenance paragraph for those four numbers, in the style of the existing P2A provenance note                                                                                                                                  | Before wave 1  |
| root `Makefile`                 | `test-s3-up` / `test-s3-down` (pinned MinIO + one-shot bucket, mirroring `test-db-up`); add `./internal/media/...` and `./internal/resumeapi/...` to `server-test-db`; add `REQUIRE_TEST_S3=1` to the gate invocation          | Task 3, Task 4 |
| `.github/workflows/ci.yml`      | Object-storage service (or the documented standing exception) for the S3 conformance job; keep the OpenAPI drift job green over the new surface                                                                                | Task 3, Task 1 |
| `scripts/ci.sh`                 | Include the media and resumeapi suites in the local gate of record                                                                                                                                                             | Task 4         |
| `../traceability/README.md`     | New `AC-MEDIA` prefix row, file link, and corrected totals (12 → 13 prefixes)                                                                                                                                                  | Task 15        |
| `../implementation-plan.md`     | Phase-status row when the gates pass; strike media/avatar upload from the named traceability gaps                                                                                                                              | Gate           |
| `../../specs/aboutme-design.md` | Owner decision on the missing `/assets` row in the §2 authoritative route table (open question Q2) — a spec edit or an explicit deferral to P5A/PI                                                                             | Before Task 11 |
| `docs/api/openapi.yaml`         | Any contract amendment a route task finds necessary lands as a **separate reviewed commit** by the owner, never inside a route task's diff (D1)                                                                                | On demand      |
| `apps/server/go.mod` / `go.sum` | AWS SDK for Go v2 pin — one exclusive window, Task 3 only                                                                                                                                                                      | Task 3         |

## Forward handoffs this phase creates

| To phase | Handoff                                                                                                                                                                                                                  |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| P4       | `useApi` must hard-code `server: false` (P1's DD-C9); the autosave retry after a `csrf_rejected` **must reuse the same `Idempotency-Key`** — P2B makes that safe and the contract is written in the kernel's package doc |
| P4       | The `412` body's `details.document` is the rebase input the editor's conflict path consumes; its shape is fixed by D7                                                                                                    |
| P5A      | The public photo read path. P2B serves the owner-only route; the public/CDN surface arrives with the public resume page and needs the `/assets` question (Q2) answered first                                             |
| P5A      | Publish endpoints reuse this phase's kernel verbatim (`If-Match`, idempotency, error vocabulary); `409` is already reserved for slug conflicts                                                                           |
| PI       | Media configuration is endpoint/credential-only: PI points `MEDIA_*` at real S3 and changes no application code (D10)                                                                                                    |
| P8-priv  | Account deletion must sweep every `resumes/{resumeID}/` prefix for the deleted user's resumes; P2B's sweep helper is the reusable piece                                                                                  |
