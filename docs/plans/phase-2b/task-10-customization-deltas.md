# Task 10: Customization deltas from a fixed path allowlist

Implements `PATCH /resumes/{id}/customization` from the
[endpoint groups](../../design/api.md#endpoint-groups) as an ordered list of
explicit `set` and `unset` commands. The
[aggregate bounds](../../design/data.md#bounds-and-invariants) require paths
from a fixed allowlist. P2A's D14 deferred that allowlist here explicitly.
AC-SAVE-005 exists before dispatch; this task supplies its implementation and
evidence.

**Tier:** High risk (an unbounded write-path selector is an injection surface).

**Files:** modify `apps/server/internal/resumeapi/customization.go` (replacing
Task 4's stub); create `customization_allowlist.go`, `customization_test.go`,
`customization_contract_test.go`.

## Behavior

The body is an ordered union:

- `{op: "set", path, value}` sets one schema leaf; `value` is required and
  cannot be `null`;
- `{op: "unset", path}` removes one schema property whose parent does not
  require it; `value` is forbidden.

The commands apply in order to the down-emitted document's `customization`
subtree, then validate once with the rest of the document. This restores true
absence for optional colors and presentation objects without inventing an empty
string or null sentinel.

**The operation-specific allowlists are closed, and they do not expose the whole
subtree.** `customization.layout.sections.main` and `.sidebar` are **excluded**:
they are the section-order authority (ADR 0009) and the aggregate invariant's
carrier, so they may only be written by `PATCH …/structure`. A command targeting
them — or any prefix of them — is `422 customization_path_denied` with no write.
`set` accepts every other schema leaf: `font.*`, `colors.*`, `spacing.*`
(including `spacing.pageMargin.x`/`.y`), `heading.*`, `header.*`,
`layout.columns`, `layout.surfaceTarget`, `sectionDisplay.skill.style`,
`sectionDisplay.language.style`, `pageFormat`, and `dateFormat`.

**The allowlists cannot drift from the schema.** A Go value and parity test walk
the embedded `customization` definition. Settable paths equal schema leaves
minus the design-denied placement paths. Unsettable paths equal optional leaf
properties plus optional object roots such as `spacing.pageMargin` and `header`;
placement paths remain denied. Optional leaves such as `colors.accent` occur in
both path sets because they support both operations. The disjoint units are
`(op, path)` pairs, not path strings. A schema change fails until every new pair
is classified.

## Steps

- [x] **Step 1: failing parity test first.** Walk the schema and assert the
      exact settable and unsettable path sets above. Assert it fails when a
      required pair is missing, an undeclared pair appears, a required property
      becomes unsettable, or an optional object root cannot be unset. Do not
      reject the intentional intersection for optional leaves.
- [ ] **Step 2: failing denial tests.** A path not in the allowlist →
      `422 customization_path_denied` and **no write**: row bytes and `revision`
      unchanged, no idempotency record. Cover, at minimum: an unknown leaf; a
      **prefix** path (`colors`, `layout`) rather than a leaf; any
      `layout.sections…` path; `__proto__`, `constructor`, and `prototype`
      segments; a path with a `..` segment, an empty segment, a trailing dot, a
      leading dot, or an array index; a path over 256 bytes; a Unicode
      look-alike of an allowed segment. **A batch containing one denied path is
      rejected whole** — no partial application.
- [x] **Step 3: failing value tests.** A value of the wrong type for its leaf (a
      string where the schema says number, a hex color that is not a hex color,
      `spacing.pageMargin.x` above its 0–40 mm range, `layout.columns: 3`) →
      `422 document_invalid` with a `details.issues` entry naming the path;
      `set` with null or no value, `unset` with a value, unset of a required
      property, and either operation on a denied path are rejected. Exactly 100
      deltas are admitted for validation and 101 → `422` before application.
- [ ] **Step 4: failing application tests.** Deltas apply in order, so two
      deltas on the same path leave the last one; unrelated customization keys
      and the whole `content` subtree are **byte-identical** before and after;
      the document round-trips through a `GET` unchanged. Set then unset the
      optional leaves `colors.accent`, `colors.surface`, and
      `layout.surfaceTarget`. Build each optional object `spacing.pageMargin`
      and `header` from its leaves, then unset its root. Assert each removed
      property is absent, not materialized as null or a fallback. `set` never
      accepts an object root. Cover unset then complete reconstruction of each
      optional object by setting all its required leaves in one ordered batch;
      validation runs only after the full batch, so no invalid half-object is
      persisted.
- [x] **Step 5: failing envelope tests.** Stale `If-Match` → `412` with the
      winning document; replay returns the stored response; a different body
      under the same key → `409`.
- [ ] **Step 6: failing contract test.** Handler statuses, codes, and the delta
      schema agree with `docs/api/openapi.yaml`.
- [x] **Step 7: implement; green.**
- [ ] **Step 8: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      `make api-check`. Connected `make scan` runs once at the unchanged phase
      candidate.
- [ ] **Step 9: handoff.** Report the owned paths, failing-test evidence, exact
      checks, and allowed/denied parity sets to the integration owner. Do not
      stage or commit.

## Implementation record

The customization handler and fixed allowlists are implemented in
`customization.go` and `customization_allowlist.go`.
`TestCustomizationAllowlistMatchesEmbeddedSchema` covers schema parity,
optional-leaf overlap, optional object roots, undeclared pairs, and required
properties. `TestCustomizationHTTPAtomicDenialValidationReplayAndCAS` and
`TestCustomizationDeltaCountAndUnionBoundaries` cover value kinds and ranges,
union rules, 100/101 bounds, no-write rejection, replay, key reuse, and stale
CAS. No historical RED transcript is retained.

`TestCustomizationDeniedPathsRejectWholeBatch` covers the full hostile-path
matrix at the apply layer, while the live HTTP test proves no-write behavior for
a representative denied batch; the full matrix is not driven through HTTP.
`TestCustomizationDeltasApplyInOrderAndPreserveUnrelatedSubtrees` covers ordered
application, absence, reconstruction, and unrelated-subtree isolation, but not
reconstruction through the live route from each absent optional root. The
contract test checks the union and status surface but not every OpenAPI error
branch. Steps 2, 4, and 6 remain open, as do the gate/handoff Steps 8–9 because
the exact Step 8 gate is not recorded. Connected `make scan`,
unchanged-candidate CI, and fresh review remain phase-owned.

**Phase-review focus:** At W4, the one fresh phase reviewer checks whether any
accepted path can reach a document location outside `customization`. The same
reviewer confirms fixes.

## Acceptance mapping

| Row         | What this task contributes                                                     |
| ----------- | ------------------------------------------------------------------------------ |
| AC-SAVE-005 | The whole new row: fixed allowlist, denial without a write, parity with schema |
| AC-DOC-008  | Protects the layout aggregate by excluding `layout.sections` from deltas       |
| AC-SAVE-001 | Customization writes participate in the same `412` contract                    |
