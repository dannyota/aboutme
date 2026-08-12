# Task 6: Resume CRUD — list, create (cap), read, rename, delete

Implements the [resume endpoint group](../../design/api.md#endpoint-groups):
CRUD with the three-resume cap on create.

**Tier:** High risk (authorization, cap concurrency, CAS).

**Files:** modify `apps/server/internal/resumeapi/resumes.go` (replacing Task
4's stub); create `resumes_test.go`, `resumes_contract_test.go`.

## Behavior

| Operation              | Contract                                                                                                                                                                                                                                                                                  |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /resumes`         | The caller's own resumes as **summaries** (no document): id, title, revision as a string, `live`, `slug`, `schemaVersion`, timestamps. Ordered as the store orders them. Emits the schema-version header and no ETag. Never another user's row                                            |
| `POST /resumes`        | Creates from an optional seed document; any seed `personalDetails.photo` is `422 document_invalid` because only the server may create photo metadata. A 4th is `409 resume_cap_exceeded`. `Idempotency-Key` required, `If-Match` rejected (D6). `201` with `Location` and the full resume |
| `GET /resumes/{id}`    | The resume with its document, projected to current and emitted at the caller's declared version; `ETag: "r<revision>"`                                                                                                                                                                    |
| `PATCH /resumes/{id}`  | `{title?, lng?}`; absent key means "unchanged", `""` means "cleared" under the [resume aggregate](../../design/data.md#resume-aggregate). Projects, sanitizes, and validates the complete current aggregate, then uses `SaveMetadataAndDocumentTx`                                        |
| `DELETE /resumes/{id}` | `204`, no body. After row commit, deletes only the transaction-returned photo key that passes D11's expected-resume validator                                                                                                                                                             |

**Delete handles media without unbounded work.** Inside the idempotent database
transaction, the handler reads the current photo key and deletes the resume.
After a definite commit it validates that transaction-returned key against D11's
exact grammar and expected resume ID, then deletes that exact key. An invalid or
cross-resume key makes no backend call; cleanup records a safe metric and leaves
the bytes for the reference-aware sweep. A missing object is success; another
backend error is logged and measured. Neither cleanup failure changes the stored
`204`. An ambiguous commit does not delete bytes because the row may still
reference them. A replay performs no cleanup.

## Steps

- [ ] **Step 1: failing authorization tests first.** Every operation with no
      session → `401 session_required`; every mutation with a session but no
      CSRF token → `403 csrf_rejected`; every per-resume operation against a
      **real id owned by another user** matches the same operation against a
      wholly nonexistent id in status, body, and stable security/cache/content
      headers (P2A D17: no existence oracle). Exclude request-scoped `Date` and
      `X-Request-Id`, and separately assert both responses carry valid distinct
      request IDs. `GET /resumes` for a user with no resumes is an empty list,
      never `404`.
- [ ] **Step 2: failing cap tests.** Creating a 4th → `409 resume_cap_exceeded`,
      with three rows still present and no idempotency record written for the
      rejected mutation. Twenty concurrent creates over HTTP for one user →
      exactly 3 succeed and 17 are `409`, deterministic under `-race -count=20`;
      the database row count is 3 (AC-DOC-001's HTTP evidence — the trigger
      remains the enforcement).
- [ ] **Step 3: failing write-envelope tests.** A stale `If-Match` on `PATCH` →
      `412` carrying the current revision and document; the same
      `Idempotency-Key` and body replays the stored `201`/`200` without creating
      a second resume; a different body under the same key → `409`. Deleting an
      already-deleted resume is `404`, and a replayed delete returns the stored
      `204`.
- [ ] **Step 4: failing draft-permissive tests.** Creating with no seed document
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
      photo and assert that exact object is removed after commit. An unrelated
      candidate under the same prefix and a neighbour resume's object remain for
      bounded orphan cleanup. Backend failure does not turn a committed delete
      into `500`; it emits the required failure metric. A malformed or cross-
      resume transaction-returned key also leaves the committed `204` unchanged,
      emits no key value, and causes zero backend calls. Race a prior read
      against a transaction-time photo-key change and prove the cleanup intent
      uses only the key returned by `DeleteTx`; a replay executes no cleanup.
- [ ] **Step 7: implement; green.**
- [ ] **Step 8: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      repeat that package command with `-race -count=20`, then run
      `make api-check`.
- [ ] **Step 9: handoff.** Report the owned paths, failing-test evidence, exact
      checks, and media-cleanup state matrix to the integration owner. Do not
      stage or commit.
- [ ] **Step 10: independent defect review.**

## Acceptance mapping

| Row          | What this task contributes                                               |
| ------------ | ------------------------------------------------------------------------ |
| AC-DOC-001   | HTTP evidence that the 4th resume is rejected, including under races     |
| AC-SAVE-001  | `412` on a stale rename                                                  |
| AC-SAVE-002  | Replay and reuse over create, rename, and delete                         |
| AC-MEDIA-003 | Resume delete removes the resume's stored objects                        |
| AC-MEDIA-006 | Ambiguous commits and failed cleanup never delete a possibly live object |
