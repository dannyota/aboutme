# Task 6: Resume CRUD — list, create (cap), read, rename, delete

Design spec §4's `GET/POST /resumes` and `GET/PATCH/DELETE /resumes/{id}` rows:
"CRUD; create enforces 3-resume cap".

**Tier:** High risk (authorization, cap concurrency, CAS).

**Files:** modify `apps/server/internal/resumeapi/resumes.go` (replacing Task
4's stub); create `resumes_test.go`, `resumes_contract_test.go`.

## Behavior

| Operation              | Contract                                                                                                                                                                                          |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /resumes`         | The caller's own resumes as **summaries** (no document): id, title, revision as a string, `live`, `slug`, `schemaVersion`, timestamps. Ordered as the store orders them. Never another user's row |
| `POST /resumes`        | Creates from an optional seed document; a 4th is `409 resume_cap_exceeded`. `Idempotency-Key` required, `If-Match` rejected (D6). `201` with `Location` and the full resume                       |
| `GET /resumes/{id}`    | The resume with its document, projected to current and emitted at the caller's declared version; `ETag: "r<revision>"`                                                                            |
| `PATCH /resumes/{id}`  | `{title?, lng?}`; absent key means "unchanged", `""` means "cleared" (spec §3's absence rule). Uses `SaveMetadataTx`                                                                              |
| `DELETE /resumes/{id}` | `204`, no body. Sweeps the resume's media prefix (see below)                                                                                                                                      |

**Delete sweeps media.** The handler calls
`blobs.DeletePrefix(ctx, "resumes/"+id.String()+"/")` **after** the row delete
commits, and a sweep failure is logged, not surfaced: the resume is gone either
way, and the orphan is collected by the next sweep. Doing it here rather than in
Task 11 keeps the two tasks' files disjoint (the interface comes from Task 3).

## Steps

- [ ] **Step 1: failing authorization tests first.** Every operation with no
      session → `401 session_required`; every mutation with a session but no
      CSRF token → `403 csrf_rejected`; every per-resume operation against a
      **real id owned by another user** returns a response byte-identical to the
      same operation against a wholly nonexistent id — status, code, message,
      and headers (P2A D17: no existence oracle). `GET /resumes` for a user with
      no resumes is an empty list, never `404`.
- [ ] **Step 2: failing cap tests.** Creating a 4th →
      `409     resume_cap_exceeded`, with three rows still present and no
      idempotency record written for the rejected mutation. Twenty concurrent
      creates over HTTP for one user → exactly 3 succeed and 17 are `409`,
      deterministic under `-race -count=20`; the database row count is 3
      (AC-DOC-001's HTTP evidence — the trigger remains the enforcement).
- [ ] **Step 3: failing write-envelope tests.** A stale `If-Match` on `PATCH` →
      `412` carrying the current revision and document; the same
      `Idempotency-Key` and body replays the stored `201`/`200` without creating
      a second resume; a different body under the same key → `409`. Deleting an
      already-deleted resume is `404`, and a replayed delete returns the stored
      `204`.
- [ ] **Step 4: failing draft-permissive tests.** Creating with no seed document
      yields a document that is valid at the draft level and reloads unchanged;
      a title of `""` is accepted (clearing a title to retype must not block a
      write); 160 code points accepted, 161 → `422`; `lng` of 35 characters
      accepted, 36 rejected, and an unset `lng` round-trips as absent.
- [ ] **Step 5: failing contract test.** `resumes_contract_test.go` reads
      `docs/api/openapi.yaml` and asserts, for each of the five operations, that
      the handler's status codes, media types, envelope shape, and error codes
      match the document — the same pattern
      `internal/api/health_contract_test.go` establishes. A contract/handler
      disagreement stops this task and goes to the owner as an amendment (D1).
- [ ] **Step 6: implement; green.**
- [ ] **Step 7: failing media-sweep test.** Put two objects under the resume's
      prefix and one under another resume's; delete the resume; assert the first
      two are gone and the third remains; assert a backend error during the
      sweep does not turn a successful delete into a `500`.
- [ ] **Step 8: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`,
      concurrency cases at `-count=20`; `make api-check`.
- [ ] **Step 9: commit** —
      `git commit -m "feat(resumeapi): add resume CRUD endpoints" -- apps/server/internal/resumeapi`
- [ ] **Step 10: independent defect review.**

## Acceptance mapping

| Row          | What this task contributes                                           |
| ------------ | -------------------------------------------------------------------- |
| AC-DOC-001   | HTTP evidence that the 4th resume is rejected, including under races |
| AC-SAVE-001  | `412` on a stale rename                                              |
| AC-SAVE-002  | Replay and reuse over create, rename, and delete                     |
| AC-MEDIA-003 | Resume delete removes the resume's stored objects                    |
