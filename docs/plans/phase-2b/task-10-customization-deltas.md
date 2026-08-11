# Task 10: Customization deltas from a fixed path allowlist

Design spec §4's `PATCH /resumes/{id}/customization` row ("list of
`{path, value}` deltas") and §3's size-bounds bullet, whose last clause is
**"customization delta paths from a fixed allowlist"**. P2A's D14 deferred that
allowlist here explicitly and noted it has no traceability row either way; this
task mints **AC-SAVE-005** and closes it.

**Tier:** High risk (an unbounded write-path selector is an injection surface).

**Files:** modify `apps/server/internal/resumeapi/customization.go` (replacing
Task 4's stub); create `customization_allowlist.go`, `customization_test.go`,
`customization_contract_test.go`.

## Behavior

The body is `{deltas: [{path, value}]}`, applied in order to the down-emitted
document's `customization` subtree, then validated once with the rest of the
document.

**The allowlist is a closed set of leaf paths, and it is not the whole
subtree.** `customization.layout.sections.main` and `.sidebar` are **excluded**:
they are the section-order authority (ADR 0009) and the aggregate invariant's
carrier, so they may only be written by `PATCH …/structure`. A delta targeting
them — or any prefix of them — is `422 customization_path_denied` with no write.
Everything else in the schema's `customization` definition is allowed at leaf
granularity: `font.*`, `colors.*`, `spacing.*` (including
`spacing.pageMargin.x`/`.y`), `heading.*`, `header.*`, `layout.columns`,
`layout.surfaceTarget`, `sectionDisplay.skill.style`,
`sectionDisplay.language.style`, `pageFormat`, `dateFormat`.

**The allowlist cannot drift from the schema.** It is a Go value with a parity
test that walks the embedded schema's `customization` definition, enumerates
every leaf path, and asserts `allowed ∪ deniedByDesign == schemaLeaves` with an
empty intersection. Adding a customization field to the schema then fails the
test until it is deliberately placed in one of the two sets — the same
faithfulness argument the codegen conformance test makes for `sectionType`.

## Steps

- [ ] **Step 1: failing parity test first.** Walk the schema, derive the leaf
      set, and assert the partition above. Assert it fails when a leaf is
      removed from both sets and when a path appears in both.
- [ ] **Step 2: failing denial tests.** A path not in the allowlist →
      `422     customization_path_denied` and **no write**: row bytes and
      `revision` unchanged, no idempotency record. Cover, at minimum: an unknown
      leaf; a **prefix** path (`colors`, `layout`) rather than a leaf; any
      `layout.sections…` path; `__proto__`, `constructor`, and `prototype`
      segments; a path with a `..` segment, an empty segment, a trailing dot, a
      leading dot, or an array index; a path over 256 bytes; a Unicode
      look-alike of an allowed segment. **A batch containing one denied path is
      rejected whole** — no partial application.
- [ ] **Step 3: failing value tests.** A value of the wrong type for its leaf (a
      string where the schema says number, a hex color that is not a hex color,
      `spacing.pageMargin.x` above its 0–40 mm range, `layout.columns: 3`) →
      `422 document_invalid` with a `details.issues` entry naming the path;
      `null` is rejected rather than silently deleting a key; the delta count is
      bounded, and a batch above the bound → `422`.
- [ ] **Step 4: failing application tests.** Deltas apply in order, so two
      deltas on the same path leave the last one; unrelated customization keys
      and the whole `content` subtree are **byte-identical** before and after;
      the document round-trips through a `GET` unchanged.
- [ ] **Step 5: failing envelope tests.** Stale `If-Match` → `412` with the
      winning document; replay returns the stored response; a different body
      under the same key → `409`.
- [ ] **Step 6: failing contract test.** Handler statuses, codes, and the delta
      schema agree with `docs/api/openapi.yaml`.
- [ ] **Step 7: implement; green.**
- [ ] **Step 8: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`;
      `make api-check`; `make semgrep`.
- [ ] **Step 9: commit** —
      `git commit -m "feat(resumeapi): add customization delta saves with a path allowlist" -- apps/server/internal/resumeapi`
- [ ] **Step 10: independent defect review**, asked specifically whether any
      accepted path can reach a document location outside `customization`.

## Acceptance mapping

| Row         | What this task contributes                                                     |
| ----------- | ------------------------------------------------------------------------------ |
| AC-SAVE-005 | The whole new row: fixed allowlist, denial without a write, parity with schema |
| AC-DOC-008  | Protects the layout aggregate by excluding `layout.sections` from deltas       |
| AC-SAVE-001 | Customization writes participate in the same `412` contract                    |
