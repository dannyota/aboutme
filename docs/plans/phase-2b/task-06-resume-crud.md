# Task 6: Resume CRUD — list, create (cap), read, rename, delete

Implements the [resume endpoint group](../../design/api.md#endpoint-groups):
CRUD with the three-resume cap on create.

**Tier:** High risk (authorization, cap concurrency, CAS).

**Files:** modify `apps/server/internal/resumeapi/resumes.go` (replacing Task
4's stub); create `resumes_test.go`, `resumes_contract_test.go`.

## Behavior

| Operation              | Contract                                                                                                                                                                                                                                                                                                                                                     |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET /resumes`         | The caller's own resumes as **summaries** (no document): id, title, revision as a string, `live`, `slug`, `schemaVersion`, timestamps. Ordered as the store orders them. Emits the schema-version header and no ETag. Never another user's row                                                                                                               |
| `POST /resumes`        | Creates from `{title, lng?, document?}`. The optional `document` is the seed at the declared wire version; any seed `personalDetails.photo` is `422 document_invalid` because only the server may create photo metadata. A 4th is `409 resume_cap_exceeded`. `Idempotency-Key` required, `If-Match` rejected (D6). `201` with `Location` and the full resume |
| `GET /resumes/{id}`    | The resume with its document, projected to current and emitted at the caller's declared version; `ETag: "r<revision>"`                                                                                                                                                                                                                                       |
| `PATCH /resumes/{id}`  | `{title?, lng?}`; absent key means "unchanged", `""` means "cleared" under the [resume aggregate](../../design/data.md#resume-aggregate). Projects, sanitizes, and validates the complete current aggregate, then uses `SaveMetadataAndDocumentTx`                                                                                                           |
| `DELETE /resumes/{id}` | `204`, no body. The transaction validates its returned photo key and enqueues exact-key cleanup while deleting the row                                                                                                                                                                                                                                       |

**Delete handles media without unbounded work.** Inside the idempotent database
transaction, the handler reads and validates the current photo key, deletes the
resume, and enqueues that exact key. An invalid or cross-resume key rolls back
both changes and makes no backend call. A queue write failure also rolls back.
After commit the old object is inaccessible because every read is
reference-gated. A replay performs no callback and creates no duplicate job.

## Steps

- [x] **Step 1: failing authorization tests first.** Every operation with no
      session → `401 session_required`; every mutation with a session but no
      CSRF token → `403 csrf_rejected`; every per-resume operation against a
      **real id owned by another user** matches the same operation against a
      wholly nonexistent id in status, body, and stable security/cache/content
      headers (P2A D17: no existence oracle). Exclude request-scoped `Date` and
      `X-Request-Id`, and separately assert both responses carry valid distinct
      request IDs. `GET /resumes` for a user with no resumes is an empty list,
      never `404`.
- [x] **Step 2: failing cap tests.** Creating a 4th → `409 resume_cap_exceeded`,
      with three rows still present and no idempotency record written for the
      rejected mutation. Twenty concurrent creates over HTTP for one user →
      exactly 3 succeed and 17 are `409`, deterministic under `-race -count=20`;
      the database row count is 3 (AC-DOC-001's HTTP evidence — the trigger
      remains the enforcement).
- [x] **Step 3: failing write-envelope tests.** A stale `If-Match` on `PATCH` →
      `412` carrying the current revision and document; the same
      `Idempotency-Key` and body replays the stored `201`/`200` without creating
      a second resume; a different body under the same key → `409`. Deleting an
      already-deleted resume is `404`, and a replayed delete returns the stored
      `204`.
- [x] **Step 4: failing draft-permissive tests.** Creating with no seed document
      yields a document that is valid at the draft level and reloads unchanged;
      a seed carrying `personalDetails.photo` in any accepted wire version is
      `422 document_invalid` with no resume row, idempotency record, or object;
      a title of `""` is accepted (clearing a title to retype must not block a
      write); 160 code points accepted, 161 → `422`. For `lng`, null and `""`
      clear it; valid non-empty input is parsed and canonicalized before the
      35-character check; invalid or canonicalized-overlong input is rejected
      before persistence. Reads project null, empty, invalid legacy, and
      canonicalized-overlong legacy values to `und`; bounded valid legacy values
      return their canonical tag. Seed an old v1 stored row containing hostile
      rich text; a metadata-only PATCH projects it to current v2, sanitizes and
      validates the complete aggregate, and persists v2 document parts and
      schema version in the same revision bump.
- [ ] **Step 5: failing contract test.** `resumes_contract_test.go` reads
      `docs/api/openapi.yaml` and asserts, for each of the five operations, that
      the handler's status codes, media types, envelope shape, and error codes
      match the document — the same pattern
      `internal/api/health_contract_test.go` establishes. A contract/handler
      disagreement stops this task and goes to the owner as an amendment (D1).
- [ ] **Step 6: failing media cleanup test.** Delete a resume with a current
      photo and assert the row deletion and one exact-key job commit together.
      The object remains private until the worker runs. An unrelated candidate
      under the same prefix and a neighbour resume's object get no job. Queue
      failure, malformed key, or cross-resume key rolls back the delete, emits
      no key value, and causes zero backend calls. Race a prior read against a
      transaction-time photo-key change and prove only the key returned by
      `DeleteTx` is enqueued; a replay creates no duplicate job.
- [x] **Step 7: implement; green.**
- [ ] **Step 8: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      repeat that package command with `-race -count=20`, then run
      `make api-check`.
- [ ] **Step 9: handoff.** Report the owned paths, failing-test evidence, exact
      checks, and media-cleanup state matrix to the integration owner. Do not
      stage or commit.

## Implementation record

The five CRUD handlers are implemented in `resumes.go`. Current regression
evidence covers authorization and no-oracle behavior
(`TestResumeAuthorization_SessionAndCSRFRequired` and
`TestResumeAuthorization_NoExistenceOracle`), the 20-request cap race
(`TestResumeCreate_CapAndConcurrentHTTPEnforcement`), the CRUD/CAS/replay
envelope (`TestResumeCRUD_LifecycleAndWriteEnvelope`), and seed, language,
legacy-upgrade, sanitizing, and bound behavior
(`TestResumeCreate_SeedVersionsPhotoRejectionAndBounds` and
`TestResumeMetadata_LanguageProjectionAndCompleteLegacyUpgrade`). No historical
RED transcript is retained, so this record does not claim one.

`TestResumeCRUD_OpenAPIContract` supplies document-level contract checks, but it
does not exhaustively compare every live handler error and media response with
OpenAPI. The exact-key cleanup tests prove transactional enqueue, rollback,
transaction-time key selection, and same-prefix isolation, but do not directly
prove zero backend calls for invalid keys, neighbour-resume object isolation, or
a photo-bearing delete replay producing no duplicate job. Steps 5 and 6 remain
open for those clauses. The exact Step 8 gate and its `-race -count=20` repeat
have no retained task record, so Steps 7–9 remain open. The connected scan,
unchanged-candidate CI, and fresh review remain phase-owned.

**Phase-review focus:** At W4, the one fresh phase reviewer checks
authorization, cap concurrency, CAS, idempotency, and exact-key media cleanup
for this route group. The same reviewer confirms fixes.

## Acceptance mapping

| Row          | What this task contributes                                               |
| ------------ | ------------------------------------------------------------------------ |
| AC-DOC-001   | HTTP evidence that the 4th resume is rejected, including under races     |
| AC-SAVE-001  | `412` on a stale rename                                                  |
| AC-SAVE-002  | Replay and reuse over create, rename, and delete                         |
| AC-MEDIA-003 | Resume delete commits reference revocation with one exact-key job        |
| AC-MEDIA-006 | Ambiguous commits and queue failures never target a possibly live object |
