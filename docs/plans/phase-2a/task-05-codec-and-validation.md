# Task 5: Document codec + validation pipeline — every size bound with a limit+1 test

Wires AC-DOC-003 / AC-DOC-007 / AC-DOC-008 into live writes; closes AC-DOC-002
at the write path; and closes AC-DOC-004 / AC-DOC-011 by enforcing the 512 KB
store budget and every schema bound with a limit+1 test.

**Files:** create `apps/server/internal/resume/{codec.go,validate.go}`,
`codec_test.go`, `validate_test.go`, `bounds_test.go`, `export_test.go`; modify
`apps/server/go.mod`/`go.sum` (add `github.com/santhosh-tekuri/jsonschema/v6` at
latest stable, pinned — **serialized per B10**, exclusive window required);
create (generated, committed) `packages/schema/fixtures/bounds/` +
`manifest.json` and `packages/schema/test/bounds-parity.test.ts` (**also
B10-serialized**).

**D1 adoption conditions bound to this task** (owner-ruled, all verified by test
or recorded evidence, none optional):

- Format assertion **enabled and pinned**, configured to match ajv's posture in
  `packages/schema` exactly (verify what ajv asserts for `format: uuid` /
  `format: uri` there first; whatever it is, both sides must agree — the parity
  test is the enforcement).
- Compiler constructed with **no URL loader**: resolving any external `$ref`
  fails. One test proves it (compile a schema with a remote `$ref` → error).
- Compiled **once at package init** from `schema.RawSchema`; compilation failure
  is a **hard startup failure** (panic in `init`/`MustCompile` style), never
  lazy, never per-call. One test proves the compiled schema is reused (pointer
  identity or init-once counter via export_test seam).
- The task report records the **transitive dependency delta** (`go mod graph`
  before/after) for the Opus review.
- **Verdict parity (D1(e))** is what makes "one schema, both languages" true —
  Step 3b below, not the shared file itself.

**Validation scope (D19):** `ValidateForStore` validates at `CurrentVersion`
only; its input is always a to-be-persisted current-version document or a
completed projection. Pre-current intermediate shapes never pass through it —
Task 8's synthetic-converter seam is the test that exercises that boundary.

**Interfaces.** Produces:

```go
package resume

const MaxDocumentBytes = 512 * 1024 // budgets.md: resume doc total, P2A store

// AssembleCanonical injects schemaVersion (D4) and marshals the canonical
// full document; DecodeParts strict-decodes the three stored jsonb parts.
func AssembleCanonical(doc schema.Resume) ([]byte, error)
func DecodeParts(personalDetails, content, customization json.RawMessage,
    schemaVersion int32) (schema.Resume, error)
// encodeParts is UNEXPORTED (owner correction 5): it is the only way to
// produce the three jsonb values, so keeping it package-private is the half
// of the D16 choke point that can actually be enforced. Tests reach it
// through export_test.go. AssembleCanonical stays exported — it marshals
// and never writes, and Task 11's blind suite consumes it by name.
func encodeParts(doc schema.Resume) (personalDetails, content,
    customization json.RawMessage, err error)

type ValidationError struct{ Issues []string } // stable, sorted, path-first
func (e *ValidationError) Error() string

// ValidateForStore is the single write-path choke point (D16/D1):
// canonical marshal → JSON-Schema validation (embedded schema.RawSchema) →
// MaxDocumentBytes → schema.ValidateDocument (incl. Task 2's entry-id
// uniqueness). Returns *ValidationError or nil.
func ValidateForStore(doc schema.Resume) error
```

- [x] **Step 1: failing codec round-trip tests.** Parts→doc→parts byte-stable
      for `packages/schema/fixtures/{minimal,full,draft-*}.json`
      (draft-permissiveness preserved: absent vs `""` distinguishable after a
      round trip — the spec's "never fabricate a sentinel" rule as a test);
      parts never contain a `schemaVersion` key (D4); unknown field in a stored
      part → decode error (strict).
- [x] **Step 2: failing pipeline tests.** Every `fixtures/store/invalid-*`
      fixture rejected by `ValidateForStore` with a matching issue; every valid
      fixture accepted; issues deterministic across runs.
- [x] **Step 3: the bounds harness (`bounds_test.go`) — failing first.** Two
      layers: 1. **Named-bound matrix**, one limit / limit+1 pair per bound:
      total doc `512*1024` bytes (construct via rich-text padding; +1 byte →
      rejected); 24 sections / 25; 64 entries in one section / 65; 16 personal
      details / 17; rich text 16384 bytes / 16385 (byte-exact, e.g. `é` padding
      per AC-DOC-007); and one pair per distinct `maxLength` class in the schema
      (36 sectionKey, 40 label, 64 iconKey, 80 displayName, 120
      city/country/name, 160 fullName/headline/jobTitle/…/title/subtitle, 256
      detail value, 512 photo key, 2048 link, 16384 richText code points). 2.
      **Completeness guard:** parse `schema.RawSchema` in the test, walk it for
      every `maxLength`/`maxItems`/`maxProperties` declaration, and assert the
      harness's exercised-bounds inventory covers each (path → limit). A future
      schema bound without a limit+1 test fails this guard loudly instead of
      silently shipping unenforced. This also closes AC-DOC-004's recorded
      partial-coverage note at the live-write layer (the P0 ajv fixture gap
      itself stays P0's row — see the companion note).
- [x] **Step 3b: the cross-language verdict-parity corpus (D1(e)) — failing
      first.** The Go bounds harness **emits its generated matrix documents** as
      a committed corpus: `packages/schema/fixtures/bounds/*.json` plus
      `manifest.json` rows
      `{file, boundPath, limit, expect: "valid"|     "invalid"}` (regenerated
      deterministically; `bounds_test.go` fails on drift against the committed
      corpus, same discipline as every generated artifact). Two consumers assert
      verdicts: `bounds_test.go` runs `jsonschema/v6` over the corpus **and
      every existing `packages/schema/fixtures/**` fixture** (valid/invalid by
      naming convention + the store-fixture expectations); the new
      `packages/schema/test/bounds-parity.test.ts` runs **ajv** over the
      identical corpus + fixtures and asserts the same verdicts. A disagreement
      anywhere — either direction — is a red build: this test, not the shared
      schema file, is what makes "one schema, both languages" true.
      (Store-layer-only rejections — entry-id duplicates, byte-length, layout
      aggregate — are marked `expect: "valid"` at the JSON-Schema layer in the
      manifest, with the store verdict as a separate column, so the two layers
      can never be conflated.)
- [x] **Step 4: implement (`jsonschema/v6` compiled once at init from
      `schema.RawSchema` per the D1 conditions, package-level, immutable); all
      green.** These are pure unit tests — no DB. Record the `go mod     graph`
      delta in the task report.
- [x] **Step 5: gate.**
      `cd apps/server && go build ./... && go vet ./... &&     go test ./internal/resume/... -count=1`;
      `make server-build server-vet     server-test`; `make schema-check` (the
      parity vitest rides in it).
- [x] **Step 6: commit** —
      `git commit -m "feat(resume): add document codec and full-bounds store validation pipeline" -- apps/server/internal/resume apps/server/go.mod apps/server/go.sum packages/schema/fixtures/bounds packages/schema/test/bounds-parity.test.ts`
