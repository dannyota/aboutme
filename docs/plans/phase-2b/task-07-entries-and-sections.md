# Task 7: Entry upsert/delete and section metadata/order

Design spec §4's three granular content rows:
`PATCH /resumes/{id}/entries/{sectionKey}` ("upsert ONE entry — identity =
`entry.id` in body"), `DELETE /resumes/{id}/entries/{sectionKey}/{entryId}`, and
`PATCH /resumes/{id}/sections/{sectionKey}` ("name / icon / entry order (content
only; **never** changes section placement)").

**Tier:** High risk (authorization, CAS, aggregate invariants).

**Files:** modify `apps/server/internal/resumeapi/entries.go` and `sections.go`
(replacing Task 4's stubs); create `entries_test.go`, `sections_test.go`,
`entries_contract_test.go`.

## Behavior

- **Entry upsert.** The body is one entry at the caller's declared wire version.
  Identity is `entry.id`: present in the section → replace in place, preserving
  its index; absent → append. The entry is validated against the declared
  version's schema for the section's `sectionType` **before** it is placed, so a
  cross-type entry is `422 document_invalid` rather than a document that fails
  validation later with a confusing path.
- **Entry delete.** Removes by `entryId` within `sectionKey`; an unknown entry
  id or section key is `404 resume_not_found` — the same code, so the endpoint
  is not an entry-existence oracle for a resume the caller does not own. For a
  resume the caller **does** own, an unknown entry is still `404`, and the
  response is identical in both cases.
- **Section metadata.** `displayName`, `iconKey`, and `entryOrder` (a
  permutation of the section's existing entry ids). It never touches
  `customization.layout.sections`: that is `PATCH …/structure`'s alone (ADR 0009
  makes `customization.layout.sections` the section-order authority, so a
  metadata write that reordered placement would silently contradict it).

Both endpoints go through the kernel's `mutate`, so `If-Match`,
`Idempotency-Key`, sanitization, validation, bounds, and the CAS write are not
re-implemented here.

## Steps

- [ ] **Step 1: failing upsert tests first.** New id appends and returns the new
      revision; an existing id replaces **in place** (index unchanged, and the
      other entries byte-identical); a half-typed entry with only `id` and the
      section's discriminator persists and reloads exactly as typed (spec §3's
      draft level); an entry whose `id` collides with an entry in a
      **different** section → `422 document_invalid` naming AC-DOC-002's
      whole-resume uniqueness rule; an entry of the wrong shape for the
      section's `sectionType` → `422`; a 65th entry in a section → `422` with
      the row unchanged.
- [ ] **Step 2: failing delete tests.** Deleting an existing entry removes
      exactly it and bumps the revision; deleting the last entry leaves an empty
      but present section (a freshly emptied section must stay valid — spec §3's
      draft permissiveness); deleting an unknown entry id → `404` with **no**
      revision bump and no idempotency record; a replayed delete returns the
      stored response.
- [ ] **Step 3: failing section-metadata tests.** `displayName` and `iconKey`
      set, cleared to `""`, and left absent behave per spec §3's absence rule;
      `entryOrder` reorders entries and rejects a permutation that adds, drops,
      or duplicates an id with `422`; the customization document is
      **byte-identical** before and after every one of these writes — the test
      that makes "never changes section placement" checkable.
- [ ] **Step 4: failing write-envelope tests.** Stale `If-Match` → `412` with
      the winning document; two concurrent upserts of different entries at the
      same revision → exactly one winner and one `412`, and the loser's retry at
      the new revision succeeds with both entries present. This is the autosave
      collision the editor will hit constantly; assert it under
      `-race -count=20`.
- [ ] **Step 5: failing contract test.** Handler statuses, codes, and shapes
      agree with `docs/api/openapi.yaml` for all three operations.
- [ ] **Step 6: implement; green.**
- [ ] **Step 7: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`,
      concurrency at `-count=20`; `make api-check`.
- [ ] **Step 8: commit** —
      `git commit -m "feat(resumeapi): add entry and section granular save endpoints" -- apps/server/internal/resumeapi`
- [ ] **Step 9: independent defect review.**

## Acceptance mapping

| Row         | What this task contributes                                                 |
| ----------- | -------------------------------------------------------------------------- |
| AC-DOC-002  | HTTP evidence that whole-resume entry-id uniqueness is enforced on writes  |
| AC-DOC-004  | Per-section entry-count bound rejected at limit+1 through the real handler |
| AC-DOC-005  | Draft-permissive autosave proven end to end                                |
| AC-SAVE-001 | Concurrent granular saves produce one winner and a usable `412`            |
