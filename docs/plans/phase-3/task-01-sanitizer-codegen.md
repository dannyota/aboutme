# Task 1: Sanitizer allowlist + hostile corpus codegen

Satisfies the mechanism half of **AC-SEC-003** (single source of truth,
mechanically propagated). No new acceptance ID needed — AC-SEC-003's row is
updated with these test references.

**Files:** modify `packages/schema/scripts/generate.mjs`,
`packages/schema/package.json` (add `"./sanitizer"` export); create (generated,
committed) `packages/schema/gen/ts/sanitizer.ts`,
`packages/schema/gen/go/sanitizer.go`; extend `packages/schema/test/gen.test.ts`
and `test/sanitizer-corpus.test.ts`.

**Interfaces (produced):**

```ts
// gen/ts/sanitizer.ts — generated from validation/*.json. DO NOT EDIT.
export const SANITIZER_ALLOWLIST_VERSION: 1;
export const ALLOWED_TAGS: readonly string[];
export const ALLOWED_ATTRIBUTES: Readonly<Record<string, readonly string[]>>;
export const ALLOWED_URL_SCHEMES: readonly string[];
export const FORBIDDEN_TAGS: readonly string[];
export const FORBIDDEN_ATTRIBUTE_PREFIXES: readonly string[];
export const FORBIDDEN_URL_SCHEMES: readonly string[];
export const EXTERNAL_REL: "noopener noreferrer";
export interface HostilePayload {
  id: string;
  category: string;
  payload: string;
}
export const HOSTILE_CORPUS: readonly HostilePayload[];
```

```go
// gen/go/sanitizer.go — same shapes: SanitizerAllowlistVersion, AllowedTags,
// AllowedAttributes map[string][]string, AllowedURLSchemes, Forbidden*,
// ExternalRel, HostilePayload struct, HostileCorpus slice.
```

- [ ] **Step 1: Failing faithfulness test.** In `test/gen.test.ts` (or a new
      `sanitizer-gen.test.ts`): import `gen/ts/sanitizer.ts`, parse the two
      validation JSONs independently, assert deep equality (tags, attributes,
      schemes, forbidden sets, rel value, every corpus payload by id) and
      `SANITIZER_ALLOWLIST_VERSION === resume.schema.json's     sanitizerAllowlistVersion const`.
      Run `npm test` → **FAIL** (module absent).
- [ ] **Step 2: Extend `generate.mjs`.** Emit both artifacts from the JSON
      sources (sorted keys, stable ordering — same determinism bar as the
      existing outputs). Wire the Go emission into the same run so
      `npm run generate` produces everything. Regenerate; Step 1 passes.
- [ ] **Step 3: Drift + Go compile.** Extend the byte-compare drift test to the
      two new outputs. `cd apps/server && go build ./... && go test ./...`
      (workspace picks up gen/go). A Go-side test in `gen/go`
      (`sanitizer_test.go`, hand-written like `section.go`'s tests) re-parses
      the JSON with `encoding/json` and asserts equality with the generated
      constants — the same faithfulness check in the second language.
- [ ] **Step 4: Gate + commit.** `make schema-check`, then commit
      `packages/schema` paths only.
