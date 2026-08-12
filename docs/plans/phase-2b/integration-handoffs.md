# Phase 2B integration handoffs

Shared integration files belong to the integration owner. Workers report the
exact requested change and stop at that boundary.

## Required inputs at dispatch

- Photo and route-rate limits plus deletion-queue and orphan-sweep bounds are in
  [`../budgets.md`](../budgets.md).
- AC-MEDIA-001…009 and AC-SAVE-005 exist before dispatch.
- The design chooses private live-gated media and no direct `/assets` route.
- `Error.details`, the local S3-compatible service, and mutable resume `lng`
  metadata are adopted plan inputs.
- P3 Task 2's Go rich-text sanitizer is landed, so P2B work that depends only on
  it may proceed in parallel with the rest of P3. P3 Task 5B releases v2 and the
  real v1↔v2 converters before P2B Task 9 and Task 1's remaining version cases
  close. P3 renderer tests own context-safe escaping of plain-text fields; P2B
  preserves those fields as text and sanitizes only schema-declared rich text.

## Shared changes during execution

| File                                       | Change                                                                                                                                                   | Owner / time                              |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- |
| Root `Makefile`                            | Add D10's exact `test-s3-up`, `test-s3-down`, and fail-closed `server-test-s3` targets after T3; add `server-test-p2b` and `server-test-p2b-s3` after T4 | Integration owner before each author gate |
| `.github/workflows/ci.yml`                 | Run S3 conformance with the pinned service                                                                                                               | Integration owner after T3                |
| `scripts/ci.sh`                            | Include media and resume API suites                                                                                                                      | Integration owner after T4                |
| `docs/api/openapi.yaml`                    | Dedicated correction if a route finds T1's contract wrong                                                                                                | Integration owner on demand               |
| `apps/web/app/api/generated/openapi.ts`    | Regenerate from T1's accepted OpenAPI source                                                                                                             | Integration owner after T1                |
| `apps/server/go.mod`, `apps/server/go.sum` | Apply T3's exact AWS SDK and image/text dependency report together                                                                                       | Integration owner exclusive window        |
| `../implementation-plan.md`                | Record P2B state after its phase review and exit gate                                                                                                    | Integration owner at gate                 |

`server-test-p2b` runs, from `apps/server`,
`REQUIRE_TEST_DB=1 TEST_DATABASE_URL=${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} TEST_MEDIA_BACKEND=fs go test ./internal/resumeapi/... -race -count=1 -v`.
`server-test-p2b-s3` sources only `.dev/test-s3.env`, changes
`TEST_MEDIA_BACKEND` to `s3`, and runs the same command. `server-test-s3`
sources that file and runs
`REQUIRE_TEST_S3=1 go test ./internal/media/... -race -count=1 -v`. All three
fail when required state is absent. None starts or stops the shared database.

## Forward handoffs

| Phase   | Contract                                                                                                                                                                                                                                                                                                   |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P4      | Authenticated fetches are client-only; CSRF retry reuses the same idempotency key                                                                                                                                                                                                                          |
| P4      | `412 details.document` is the editor rebase input                                                                                                                                                                                                                                                          |
| P5A     | `GET /public/resumes/{slug}/photo` applies the same live-state and generic-404 boundary                                                                                                                                                                                                                    |
| P5A     | Publish commands reuse the write-safety kernel; a P5A migration adds the durable singleton discovery generation, every membership mutation advances it in the same transaction after bounded lease cancellation, and public HTML uses the Caddy-denied direct Nuxt `POST /internal-render/public` contract |
| P7A     | Chromium rendering and photo intake share the one task-wide heavy-work permit                                                                                                                                                                                                                              |
| PI      | Production points the S3 backend at private S3; it does not add public object routing                                                                                                                                                                                                                      |
| P8-priv | Drain exact-key deletion jobs within 24 hours; reconcile remaining orphans with `ListPage`                                                                                                                                                                                                                 |
