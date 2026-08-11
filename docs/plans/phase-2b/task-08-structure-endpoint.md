# Task 8: `PATCH /resumes/{id}/structure` — the transactional endpoint

Design spec §3's aggregate-invariant bullet and §4's structure row: **the only
way to create, delete, move, or reorder a section**. It writes `content` and
`customization.layout` in ONE transaction "so the exactly-once placement
invariant is never observably broken", takes `If-Match` and an idempotency key
like every other write, and "the whole command applies or none of it does".

**Tier:** High risk (aggregate invariant, transactional command, CAS).

**Files:** modify `apps/server/internal/resumeapi/structure.go` (replacing Task
4's stub); create `structure_test.go`, `structure_contract_test.go`.

## Command shape

One request carries an **ordered list of commands** applied in order to the
down-emitted document, then validated once as a whole:

| Command         | Payload                                                                                      | Effect                                                                                       |
| --------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `createSection` | `key`, `sectionType`, optional `displayName`/`iconKey`, `column` (`main`/`sidebar`), `index` | Adds the section to `content` **and** places its key exactly once in the chosen layout array |
| `deleteSection` | `key`                                                                                        | Removes it from `content` **and** from whichever layout array holds it                       |
| `moveSection`   | `key`, `column`, `index`                                                                     | Removes the key from its current array and inserts it at `index` in the target               |
| `reorderColumn` | `column`, `keys`                                                                             | Replaces one layout array with a permutation of its current members                          |

Applying commands in order, then validating the assembled aggregate once, is
what makes a partially valid batch impossible: an intermediate state that
violates exactly-once is never persisted, and a batch whose **final** state is
invalid is rejected whole with `422 document_invalid`.

Rejected alternative: one command per request. It would make the editor's
"delete this section" a two-request operation with a visible half-state, which
is the exact failure the spec's single-endpoint rule exists to prevent.

## Steps

- [ ] **Step 1: failing invariant tests first.** After every command sequence,
      every `content` key appears **exactly once** across
      `customization.layout.sections.main` and `.sidebar`, the arrays are
      deduplicated, and no array references a missing key. Assert this as a
      property over a generated set of command sequences, not only over
      hand-picked cases.
- [ ] **Step 2: failing rejection tests.** `createSection` with a key that
      already exists → `422`; `deleteSection` of an unknown key → `422`;
      `moveSection` to an out-of-range index → `422`; `reorderColumn` whose
      `keys` add, drop, or duplicate a member → `422`; a 25th section → `422`
      (the 24-section bound); an unknown `sectionType` → `422`. In every case
      the stored row is **byte-identical** before and after, `revision` is
      unchanged, and no idempotency record exists.
- [ ] **Step 3: failing all-or-nothing test.** A batch whose first command is
      valid and whose second is not leaves the document exactly as it was — the
      spec's "the whole command applies or none of it does". Assert on stored
      bytes, not on a re-read through the API.
- [ ] **Step 4: failing one-column test.** `columns: 1` with a populated
      `sidebar` is valid by design (spec §5's one-column placement decision), so
      a `moveSection` into `sidebar` while `columns == 1` is **accepted**, not
      rejected; toggling `columns` does not rewrite placement. This is the case
      a naive implementation gets wrong by "helpfully" flattening the sidebar.
- [ ] **Step 5: failing concurrency test.** Two structure commands at the same
      revision → exactly one winner, one `412` whose `details.document` is the
      winner's; a structure command concurrent with an entry upsert on the same
      resume also resolves to one winner, with no interleaved half-state
      observable. Deterministic under `-race -count=20`.
- [ ] **Step 6: failing contract test.** Handler statuses, codes, and the
      command schema agree with `docs/api/openapi.yaml`.
- [ ] **Step 7: implement; green.** One `mutate` call; commands applied to the
      generic tree; a single validation of the assembled aggregate; one CAS
      write of the complete document.
- [ ] **Step 8: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`,
      concurrency at `-count=20`; `make api-check`.
- [ ] **Step 9: commit** —
      `git commit -m "feat(resumeapi): add the transactional structure endpoint" -- apps/server/internal/resumeapi`
- [ ] **Step 10: independent defect review**, asked specifically whether any
      command sequence can commit a document violating exactly-once placement.

## Acceptance mapping

| Row         | What this task contributes                                         |
| ----------- | ------------------------------------------------------------------ |
| AC-DOC-008  | HTTP evidence for the exactly-once layout aggregate on live writes |
| AC-DOC-004  | The 24-section bound rejected at limit+1 through the real handler  |
| AC-SAVE-001 | Structural writes participate in the same `412` contract           |
