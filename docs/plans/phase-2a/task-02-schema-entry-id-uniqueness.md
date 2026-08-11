# Task 2: Schema-package additions — entry-id uniqueness (AC-DOC-002) + embedded raw schema (D2)

Post-P0 contract-adjacent change: dedicated reviewed commit(s), regeneration
included, per the master plan's contract-change rule. Does **not** touch
`resume.schema.json` itself. **Serialized (B10):** every file below is on the
P3-contested list; this task runs only inside an owner-granted
exclusive-ownership window on `packages/schema/**`.

**Files:** modify `packages/schema/validation/store.ts`,
`packages/schema/test/store-validation.test.ts`,
`packages/schema/gen/go/store_validate.go`,
`packages/schema/gen/go/store_validate_test.go`,
`packages/schema/scripts/generate.mjs` (its byte-compare coverage lives in the
existing `packages/schema/test/gen.test.ts`); create (generated, committed)
`packages/schema/gen/go/rawschema.go`; create
`packages/schema/gen/go/rawschema_test.go`.

**Interfaces.** Produces:
`ValidateEntryIDUniqueness(content map[string]Section) []ValidationIssue` (Go) /
`validateEntryIdUniqueness` (TS), both folded into `ValidateDocument`/the TS
aggregate entry point; `schema.RawSchema []byte` in generated `rawschema.go` (D2
— a Go source constant with the `DO NOT EDIT` header, not an embedded `.json`
file).

- [x] **Step 1: failing conformance tests, both languages.** Go:
      `TestValidateDocument_DuplicateEntryID` consuming
      `fixtures/store/invalid-duplicate-entry-id.json` (duplicate ids in
      **different sections** — the whole-resume rule, not per-section); TS: the
      mirror case in `store-validation.test.ts`. Add one green case (same id
      nowhere duplicated) and one cross-section-duplicate fixture already exists
      — verify it encodes the cross-section case; if it is same-section-only,
      add a second fixture rather than editing it. Run
      `cd packages/schema && npm test` and
      `cd packages/schema/gen/go && go test ./...` → **FAIL**.
- [x] **Step 2: implement both halves; green.** Deterministic issue ordering
      (sort by path) like the existing validators.
- [x] **Step 3: failing raw-schema test.** `rawschema_test.go`: read
      `../../resume.schema.json` at test time and assert `schema.RawSchema`
      byte-equals it — this one test closes the copy-drift loop from the Go
      side; the existing `gen.test.ts` byte-compare covers it from the generator
      side unchanged. Run → **FAIL** (`RawSchema` undefined). Extend
      `generate.mjs` to emit `rawschema.go` (generated header, `DO NOT EDIT`);
      run `make schema-gen`; commit generated output; green.
- [x] **Step 4: gate.** `make schema-check` (regenerates via npm ci + vitest,
      incl. `gen.test.ts`; proves no drift) and
      `cd packages/schema/gen/go && go test ./...`.
- [x] **Step 5: commit** —
      `git commit -m "feat(schema): enforce whole-resume entry-id uniqueness and generate the raw-schema Go constant" -- packages/schema`
