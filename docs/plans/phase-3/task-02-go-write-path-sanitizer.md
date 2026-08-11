# Task 2: Go write-path sanitizer (`apps/server/internal/sanitize`)

Satisfies the bluemonday half of **AC-SEC-001** and **AC-SEC-003**. Wiring into
write endpoints is P2B (D20) — this task ships the package and its proof.

**Files:** create
`apps/server/internal/sanitize/{sanitize.go,sanitize_test.go, conformance_test.go,testdata/corpus-output.golden.json}`;
modify `apps/server/go.mod`/`go.sum` (add
`github.com/microcosm-cc/bluemonday@latest` pinned; `golang.org/x/net` for the
test-side HTML parser).

**Interfaces (produced):**

```go
package sanitize

// AllowlistVersion the policy is built against (== schema constant).
const AllowlistVersion = schemagen.SanitizerAllowlistVersion

// RichText sanitizes one rich-text HTML fragment per the generated
// allowlist. Output invariants (conformance-tested): only allowed tags;
// per-tag attribute allowlist; URL schemes https/mailto/tel with no
// relative or protocol-relative URLs; every <a> carries exactly
// rel="noopener noreferrer" (D4, token order normalized); target only
// ever "_blank". Idempotent: RichText(RichText(x)) == RichText(x).
func RichText(html string) string
```

- [ ] **Step 1: Failing conformance test.** `conformance_test.go` iterates
      `schemagen.HostileCorpus`, runs `RichText`, parses output with
      `x/net/html`, and asserts the **neutralization predicate** (D2 a): no node
      whose tag ∉ AllowedTags; no attribute outside the per-tag allowlist; no
      attribute name with a forbidden prefix; every `href` parses with an
      explicit scheme ∈ AllowedURLSchemes (protocol-relative and relative
      rejected); every `<a>` rel exactly `noopener noreferrer`; `target` only
      `_blank`. The predicate lives in an exported test helper
      (`sanitize/sanitizetest`) shared by the **author-side** suites (Tasks 2,
      3, 9, 11). Task 4's blind suite must **not** import or read it (B4) — it
      authors its own predicate from the spec and allowlist data. This helper
      also gets a **negative control** in this task: run it against raw corpus
      payloads and hand-built violations (a `<script>` element, an `on*`
      attribute, a `javascript:` href, a forged/absent `rel`) and assert it
      **rejects** every one — a predicate that vacuously accepts must fail the
      suite, never silently bless the sanitizer. Run → **FAIL** (package
      absent).
- [ ] **Step 2: Implement.** Build the bluemonday policy **from the generated
      constants** in a package-level constructor — iterating
      `AllowedTags`/`AllowedAttributes`/`AllowedURLSchemes`, never a literal
      list. Add `RequireParseableURLs(true)`, reject relative URLs, and a
      post-pass that normalizes `rel`/`target` per D4 (bluemonday's built-ins
      append rel tokens in their own order; normalize to the exact D4 string so
      the DOMPurify fixed point in Task 3 can hold). Run → **PASS**.
- [ ] **Step 3: Preservation + idempotence.** Table-driven positive tests:
      benign input using every allowed tag survives with text content intact;
      `sanitize(sanitize(x)) == sanitize(x)` across the corpus and the benign
      table; malformed/truncated HTML never panics (add `FuzzRichText` seed
      corpus from the hostile payloads — go fuzz seeds only, deterministic in
      CI).
- [ ] **Step 4: Commit the corpus-output artifact.** A test regenerates
      `testdata/corpus-output.golden.json` (payload id → `RichText(payload)`)
      when `UPDATE_GOLDEN=1`, otherwise asserts byte-equality with the committed
      file. This artifact is Task 3's cross-check input.
- [ ] **Step 5: Gate + commit.** `make server-build server-vet server-test`,
      `make semgrep`. Commit `apps/server/internal/sanitize`, `go.mod`, `go.sum`
      only.
