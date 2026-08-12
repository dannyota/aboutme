# Task 2: Go sanitizer (`apps/server/internal/sanitize`)

Satisfies the bluemonday half of **AC-SEC-001** and **AC-SEC-003**. This task
ships the package and its proof. P2B owns write wiring, P5A owns public-read
re-sanitizing, and P7A owns re-sanitizing the read that feeds internal print SSR
(D20).

**Files:** create
`apps/server/internal/sanitize/{sanitize.go,sanitize_test.go,conformance_test.go,testdata/corpus-output.golden.json}`
and
`apps/server/internal/sanitize/sanitizetest/{predicate.go,predicate_test.go}`.
This worker does not touch module files.

After Task 4 freezes the runtime sanitizer suite and before this task starts,
the integration owner holds the exclusive module-file window and runs exactly:

```sh
(cd apps/server && go get github.com/microcosm-cc/bluemonday@v1.0.27 golang.org/x/net@v0.57.0)
```

The owner reviews the resulting `apps/server/go.mod`, `apps/server/go.sum`, and
root `go.work.sum` diff, records the command output, and hands off only after
both versions are exact. After this task, the same owner runs
`(cd apps/server && go mod tidy && go mod verify)` and reviews all three files
again. `go.work.sum` is a root lockfile and remains integration-owner-only.

Task 4's Go adversarial suite must already be frozen. This author records its
expected failure before implementation and never edits that file.

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

## Downstream conformance handoff

P5A and P7A each add a production-boundary test rather than calling this package
only in isolation. The P7A test seeds a stored document containing an
older-policy hostile fragment, loads it through the exact document-read path
used by the internal print job, and asserts that every rich-text field equals
`RichText` output before Nuxt SSR receives it. It also proves the read leaves
stored bytes and the user-visible revision unchanged. A direct sanitized test
fixture or a renderer-only test does not close this handoff.

- [x] **Step 1: Failing conformance test.** `conformance_test.go` iterates
      `schemagen.HostileCorpus`, runs `RichText`, parses output with
      `x/net/html`, and asserts the **neutralization predicate** (D2 a): no node
      whose tag ∉ AllowedTags; no attribute outside the per-tag allowlist; no
      attribute name with a forbidden prefix; every `href` parses with an
      explicit scheme ∈ AllowedURLSchemes (protocol-relative and relative
      rejected); every `<a>` rel exactly `noopener noreferrer`; `target` only
      `_blank`. The predicate lives in the exported Go test-helper package
      `apps/server/internal/sanitize/sanitizetest`, shared only by Go
      **author-side** sanitizer and boundary suites. Web author suites use the
      separate TypeScript helper owned by Task 3. Task 4's blind suite must
      **not** import or read either helper (B4) — it authors its own predicates
      from the spec and allowlist data. The Go helper also gets a **negative
      control** in this task: run it against raw corpus entries whose parsed
      tree violates the predicate and hand-built violations (a `<script>`
      element, an `on*` attribute, a `javascript:` href, a forged/absent `rel`)
      and assert it rejects every one. Bare hostile-looking text with no active
      node or attribute remains safe text. A predicate that vacuously accepts
      must fail the suite, never silently bless the sanitizer. Run → **FAIL**
      (package absent).
- [x] **Step 2: Implement.** Build the bluemonday policy **from the generated
      constants** in a package-level constructor — iterating
      `AllowedTags`/`AllowedAttributes`/`AllowedURLSchemes`, never a literal
      list. Add `RequireParseableURLs(true)`, reject relative URLs, and a
      post-pass that normalizes `rel`/`target` per D4 (bluemonday's built-ins
      append rel tokens in their own order; normalize to the exact D4 string so
      the DOMPurify fixed point in Task 3 can hold). Run → **PASS**.
- [x] **Step 3: Preservation + idempotence.** Table-driven positive tests:
      benign input using every allowed tag survives with text content intact;
      `sanitize(sanitize(x)) == sanitize(x)` across the corpus and the benign
      table; malformed/truncated HTML never panics (add `FuzzRichText` seed
      corpus from the hostile payloads — go fuzz seeds only, deterministic in
      CI).
- [x] **Step 4: Freeze the corpus-output artifact.** A test regenerates
      `testdata/corpus-output.golden.json` (payload id → `RichText(payload)`)
      when `UPDATE_GOLDEN=1`, otherwise asserts byte-equality with the committed
      file. This artifact is Task 3's cross-check input.
- [ ] **Step 5: Gate.** Run the focused sanitizer tests and hand off. The
      integration owner runs the post-task module command above, then runs
      `make server-build server-vet server-test` against the final module graph.
      Report both owned-path diffs and every exact command. Connected Semgrep
      runs once through `make scan` at the unchanged phase candidate.
