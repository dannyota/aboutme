# Task 8: `PATCH /resumes/{id}/structure` — the transactional endpoint

Implements the [aggregate invariant](../../design/data.md#bounds-and-invariants)
and the structure endpoint: **the only way to create, delete, move, or reorder a
section**. It writes `content` and `customization.layout` in ONE transaction "so
the exactly-once placement invariant is never observably broken", takes
`If-Match` and an idempotency key like every other write, and "the whole command
applies or none of it does".

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

`index` is a zero-based insertion position. `createSection` measures the target
column before insertion: a length `N` accepts `0..N`, and `N` appends.
`moveSection` removes the key from its current column first, then measures the
target: its resulting length `N` accepts `0..N`. The same rule governs a move
within one column, so the bound is measured on the shortened array and the key's
final index is exactly the supplied value.

Each command sees the content and layout produced by every prior command in the
request. Applying commands in order, then validating the assembled aggregate
once, makes a partially valid batch impossible: no intermediate state is
persisted. A non-integer index is `400 request_invalid`. An integer below zero
or above its command-time upper bound, or any other semantically invalid
command, rejects the whole batch with `422 document_invalid`.

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
      already exists → `422`; `deleteSection` of an unknown key → `422`; any
      integer index below zero or above its command-time upper bound →
      `422 document_invalid`; a non-integer index → `400 request_invalid`;
      `reorderColumn` whose `keys` add, drop, or duplicate a member → `422`; a
      25th section → `422` (the 24-section bound); an unknown `sectionType` →
      `422`; exactly 100 ordered commands are admitted for validation and 101
      are rejected before application. In every case the stored row is
      **byte-identical** before and after, `revision` is unchanged, and no
      idempotency record exists.
- [ ] **Step 3: failing all-or-nothing test.** A batch whose first command is
      valid and whose second is not leaves the document exactly as it was — the
      spec's "the whole command applies or none of it does". Assert on stored
      bytes, not on a re-read through the API.
- [ ] **Step 4: failing index and sequencing tests.** For a target of length
      `N`, create at `0` and `N` succeeds while `N + 1` fails. From
      `main: [a,b,c]`, moving `b` to `main` index `2` yields `[a,c,b]`; index
      `3` fails because removal leaves length `2`. Moving to another column at
      that column's length appends. A single batch can create a key and then
      move or reorder that key because the later command sees the earlier
      result. Assert exact arrays, `422 document_invalid` for dynamic range
      failures, and rollback of the whole batch.
- [ ] **Step 5: failing one-column test.** `columns: 1` with a populated
      `sidebar` is valid under the
      [template contract](../../design/templates/contract.md), so a
      `moveSection` into `sidebar` while `columns == 1` is **accepted**, not
      rejected; toggling `columns` does not rewrite placement. This is the case
      a naive implementation gets wrong by "helpfully" flattening the sidebar.
- [ ] **Step 6: failing concurrency test.** Two structure commands at the same
      revision → exactly one winner, one `412` whose `details.document` is the
      winner's; a structure command concurrent with an entry upsert on the same
      resume also resolves to one winner, with no interleaved half-state
      observable. Deterministic under `-race -count=20`.
- [ ] **Step 7: failing contract test.** Handler statuses, codes, index bounds,
      remove-before-measure behavior, and sequential command semantics agree
      with `docs/api/openapi.yaml`.
- [ ] **Step 8: implement; green.** One `mutate` call; commands applied to the
      generic tree; a single validation of the assembled aggregate; one CAS
      write of the complete document.
- [ ] **Step 9: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      repeat that package command with `-race -count=20`, then run
      `make api-check`.
- [ ] **Step 10: handoff.** Report the owned paths, failing-test evidence, exact
      checks, property-test seed policy, and concurrency results to the
      integration owner. Do not stage or commit.

**Phase-review focus:** At W4, the one fresh phase reviewer checks whether any
command sequence can commit a document that violates exactly-once placement. The
same reviewer confirms fixes.

## Acceptance mapping

| Row         | What this task contributes                                         |
| ----------- | ------------------------------------------------------------------ |
| AC-DOC-008  | HTTP evidence for the exactly-once layout aggregate on live writes |
| AC-DOC-004  | The 24-section bound rejected at limit+1 through the real handler  |
| AC-SAVE-001 | Structural writes participate in the same `412` contract           |
