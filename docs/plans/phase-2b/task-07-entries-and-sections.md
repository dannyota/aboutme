# Task 7: Entry upsert/delete and section metadata/order

Implements three
[granular content endpoints](../../design/api.md#endpoint-groups):
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

- [x] **Step 1: failing upsert tests first.** New id appends and returns the new
      revision; an existing id replaces **in place** (index unchanged, and the
      other entries byte-identical); a half-typed entry with only `id` and the
      section's discriminator persists and reloads exactly as typed under the
      [draft policy](../../design/data.md#draft-and-publish-validation); an
      entry whose `id` collides with an entry in a **different** section →
      `422 document_invalid` naming AC-DOC-002's whole-resume uniqueness rule;
      an entry of the wrong shape for the section's `sectionType` → `422`; a
      65th entry in a section → `422` with the row unchanged.
- [x] **Step 2: failing delete tests.** Deleting an existing entry removes
      exactly it and bumps the revision; deleting the last entry leaves an empty
      but present section (a freshly emptied section stays valid under the
      [draft policy](../../design/data.md#draft-and-publish-validation));
      deleting an unknown entry id → `404` with **no** revision bump and no
      idempotency record; a replayed delete returns the stored response.
- [ ] **Step 3: failing section-metadata tests.** `displayName` sets or clears
      to `""`; `iconKey` sets or clears with `null`; leaving either absent
      preserves the
      [aggregate's absence rule](../../design/data.md#resume-aggregate);
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
- [x] **Step 6: implement; green.**
- [ ] **Step 7: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      repeat that package command with `-race -count=20`, then run
      `make api-check`.
- [ ] **Step 8: handoff.** Report the owned paths, failing-test evidence, exact
      checks, and concurrency results to the integration owner. Do not stage or
      commit.

## Implementation record

Entry upsert/delete and section metadata handlers are implemented in
`entries.go` and `sections.go`. `TestEntryUpsertAppendsAndReplacesInPlace`,
`TestEntryUpsertPreservesDraftAndRejectsWrongShapeCollisionAndLimit`, and
`TestEntryHandlerRejectsWrongShapeCollisionAnd65thWithoutWrite` cover append,
in-place replacement, draft entries, whole-resume identity, type, and count
bounds. `TestEntryUpsertDeleteContractAndReplay`,
`TestEntryDeleteLastPersistsPresentEmptySection`,
`TestEntryUnknownDeleteWritesNothing`, and
`TestEntryDeleteNoOracleResponseIsIdenticalForUnknownAndForeign` cover delete,
replay, empty-section persistence, no-write rejection, and no-oracle behavior.
No historical RED transcript is retained.

`TestSectionPatchMetadataOrderAndPlacementIsolation` proves the metadata and
entry-order rules on one combined mutation. It does not prove byte-identical
customization for each separate live HTTP write, and the live section route does
not prove a rejected permutation leaves the stored row unchanged. The contract
tests exercise the live shapes and codes but do not mechanically compare every
OpenAPI branch. The concurrent-entry test covers one-winner CAS and retry, but
the required `-race -count=20` run has no retained task record. Steps 3–5 and
the gate/handoff Steps 7–8 therefore remain open. The connected scan,
unchanged-candidate CI, and fresh review remain phase-owned.

**Phase-review focus:** At W4, the one fresh phase reviewer checks whole-resume
entry identity, section-placement isolation, CAS, and autosave concurrency for
this route group. The same reviewer confirms fixes.

## Acceptance mapping

| Row         | What this task contributes                                                 |
| ----------- | -------------------------------------------------------------------------- |
| AC-DOC-002  | HTTP evidence that whole-resume entry-id uniqueness is enforced on writes  |
| AC-DOC-004  | Per-section entry-count bound rejected at limit+1 through the real handler |
| AC-DOC-005  | Draft-permissive autosave proven end to end                                |
| AC-SAVE-001 | Concurrent granular saves produce one winner and a usable `412`            |
