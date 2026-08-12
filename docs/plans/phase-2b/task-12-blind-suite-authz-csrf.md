# Task 12: Blind adversarial suite D — auth, CSRF, and cross-user authz

ADR 0011 puts authentication, authorization, and CSRF in the high-risk tier, so
a **fresh worker** derives this suite from the written contracts **before
reading any `internal/resumeapi` implementation diff or author test**.

**Inputs the blind author gets:** [security design](../../design/security.md),
[API conventions and endpoints](../../design/api.md), `docs/api/openapi.yaml` at
the phase head, this plan's [decisions.md](decisions.md) error vocabulary, the
traceability rows AC-SEC-002 and AC-AUTH-\*, and the **Interfaces blocks only**
of Tasks 3, 4, and 6–11. Task 3's configuration names and D10's no-secret-log
contract are included; its implementation and author tests remain withheld.

**Inputs withheld:** every non-test `.go` file under `internal/resumeapi`, and
every author test in that package. The kernel's shared harness
(`testutil_test.go`) may be used — it builds the server, it does not encode the
behavior under test.

**Files:** create `apps/server/internal/resumeapi/authz_adversarial_test.go`. No
implementation author may edit this file; weakening any assertion requires a
named independent review.

## Minimum matrix (the blind author may add, never subtract)

| Test                                                    | Assert                                                                                                                                                                                                                                                                                                                             |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestEveryRoute_NoSession_401`                          | Enumerate every route from the OpenAPI document (not a transcribed list) and drive each with no cookie: `401 session_required`, no state change, `Cache-Control: no-store`                                                                                                                                                         |
| `TestEveryMutation_CSRFMatrix`                          | For each mutating route: missing token, wrong token, token of the wrong length, correct token with a foreign `Origin`, no `Origin` with a foreign `Referer`, no `Origin` and no `Referer`, and a `Content-Type` of `text/plain` — every one `403 csrf_rejected` with an identical body, and zero database or object-storage deltas |
| `TestMultipartOnlyOnPhotoUpload`                        | `multipart/form-data` is accepted on the upload route and rejected on every other mutating route; conversely a JSON body on the upload route is rejected                                                                                                                                                                           |
| `TestPhotoMethodPoliciesStayDistinct`                   | Owner photo GET is authenticated but CSRF-free; POST uses multipart plus the upload limiter; PATCH crop uses strict JSON plus the ordinary write limiter and no upload permit; DELETE is bodyless. Each wrong media type or missing CSRF case fails before idempotency inspection, database change, or object I/O                  |
| `TestCrossUser_EveryRoute_IndistinguishableFromMissing` | For every per-resume route, user B's request against user A's real id matches a random UUID in status, body, and stable security/cache/content headers. Exclude request-scoped `Date` and `X-Request-Id`; separately require valid distinct request IDs on both responses—no existence oracle (P2A D17)                            |
| `TestCrossUser_NoStateLeak`                             | None of those cross-user attempts changes A's row, revision, `updated_at`, or objects; and none of them consumes a rate-limit budget belonging to A                                                                                                                                                                                |
| `TestSessionRevokedBetweenRequests`                     | A session revoked between two requests makes the second return `401` with no mutation. Mid-request revocation is outside the current contract and is not inferred by this suite                                                                                                                                                    |
| `TestGetSafeMethodsBypassCSRFButNotAuth`                | `GET` routes pass without a CSRF token but still require a session; `HEAD` and `OPTIONS` never mutate                                                                                                                                                                                                                              |
| `TestPathTraversalInRouteParams`                        | `id`, `sectionKey`, and `entryId` values containing `..`, `%2e%2e`, a NUL, a newline, an over-long string, and a non-UUID are rejected with a `400`/`404` from the vocabulary — never a `500`, never a database error surfaced to the client                                                                                       |
| `TestNoRouteAnswers501`                                 | No route returns the Task 4 stub status at the phase head                                                                                                                                                                                                                                                                          |
| `TestErrorBodiesLeakNothing`                            | No error body or header contains a SQL fragment, a stack frame, an object key, a bucket name, an internal host, or another user's identifier                                                                                                                                                                                       |
| `TestStorageSecretsNeverLeak`                           | Configure unique sentinel S3 access-key and secret values, then force startup validation, signing, `Put`, `Get`, and `Delete` failures. Neither sentinel appears in an HTTP response, response header, captured structured log, metric label, panic text, or returned config/backend error                                         |

## Steps

- [ ] **Step 1 (blind author): write the suite from the contracts; run.** Mostly
      green is expected if waves 2–3 are correct; **any red is a real finding**
      routed to an implementation author, never fixed by this author.
- [ ] **Step 2: gate.** Run `make test-db-up`, then
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`.
      Repeat that package command with `-race -count=20`.
- [ ] **Step 3: handoff.** Report the owned test file, first-run findings, exact
      checks, and captured leak surfaces to the integration owner. Do not stage
      or commit.
- [ ] **Step 4: attest independence** in the task report: which inputs were
      read, in what order, and that no implementation diff was opened first.

## Acceptance mapping

| Row          | What this task contributes                                   |
| ------------ | ------------------------------------------------------------ |
| AC-SEC-002   | Extends P1's CSRF evidence across the whole resume surface   |
| AC-DOC-001   | Cross-user probes cannot observe another user's resume count |
| AC-MEDIA-001 | Owner-only enforcement on all four media operations          |
| AC-MEDIA-004 | Independent evidence that storage credentials do not leak    |
