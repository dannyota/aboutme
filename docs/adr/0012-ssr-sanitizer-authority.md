# 0012 — Go is the sole SSR sanitization authority; DOMPurify is client-only

Status: Accepted (2026-08-11)

## Context

The spec names two sanitizers and one allowlist, but never says where each one
executes. Design spec §5 lists `RichText` "w/ DOMPurify re-sanitize" as a
renderer primitive with no client/server carve-out, and its sanitizer contract
pairs "bluemonday (write) and DOMPurify (render)" against a shared hostile
corpus. Public resume pages are server-rendered, so a plain reading of "render"
puts DOMPurify inside the SSR path.

Three further documents restate that reading rather than resolve it:
`docs/specs/templates/contract.md` §5.5 ("The renderer re-sanitizes with
DOMPurify against the same versioned allowlist"),
`docs/plans/implementation-plan.md` (the security-testing row names the corpus
surfaces as "bluemonday+DOMPurify+SSR+real browser"), and the `packages/schema`
validation artifacts, which describe DOMPurify as the "render path" without
qualification. Phase 3 ruled the opposite way in
`docs/plans/phase-3/decisions.md` D3, and the whole of
`task-03-client-render-sanitizer.md` is written to that ruling — but D3 is a
plan-level note, the master plan still lists the question as a pre-dispatch
blocker, and no ADR exists on sanitizer authority. The contradiction is
therefore live in four places at once, and it decides a production dependency,
so it cannot be left to the implementer.

Two facts settle it.

DOMPurify needs a DOM. Running it under Node means shipping `jsdom` as a
production dependency, which drags an HTTP, cookie, and WebSocket stack into the
SSR render path of a product whose stated posture is that the print browser has
no outbound network access. That is a large, network-capable surface added to
the one process that renders untrusted documents.

An SSR DOMPurify pass would also be the only pass. Vue's `hydrateElement`
re-applies event handlers, value props, and `.prop` keys — never `innerHTML` —
so on an SSR'd page whatever the server serialized is what Blink keeps, and a
client-side pass contributes nothing there. The earlier hydration rationale for
requiring DOMPurify on both sides was factually wrong.

## Decision

**Go (bluemonday) is the sole sanitization authority for anything SSR renders.
DOMPurify is client-only.**

- P2B wires `sanitize.RichText` into every rich-text write path, and the Go
  **public read path re-sanitizes** as defense in depth, so a document stored
  before an allowlist change cannot be served under the old rules.
- The renderer's `RichText` primitive calls DOMPurify under `import.meta.client`
  only, and always **before** any `innerHTML` assignment. On SSR it passes the
  string through. The client pass guards the surfaces where the server did not
  produce the markup: P4's editor preview and P6B's SSE-refetch re-render.
- **The built server bundle contains no `dompurify` and no `jsdom`.** `jsdom`
  stays a test-only devDependency (the Vitest environment). This is asserted
  against the built output, not merely intended.
- The shared hostile corpus keeps all four surfaces. "SSR" is a surface to
  **prove neutralization on**, not a place DOMPurify must execute: the committed
  bluemonday-output artifact is fed through `renderToString` and the
  neutralization predicate is asserted on the SSR output. DOMPurify remains
  conformance-tested against the same corpus in the client environment, and must
  be a fixed point over the bluemonday-output artifact.

Each of the following disagreed with this decision and is corrected by this ADR:

- `docs/specs/aboutme-design.md` §5 — the renderer-tree line ("`RichText` w/
  DOMPurify re-sanitize") and the sanitizer-contract line ("bluemonday (write)
  and DOMPurify (render)"). Both are edited in the same change; the design spec
  is `DRAFT v3` and this is the design owner's ruling, so the correction lands
  in the text rather than only here.
- `docs/specs/templates/contract.md` §5.5 — "the renderer re-sanitizes with
  DOMPurify" is scoped to the client render path. Edited in the same change.
- `docs/plans/implementation-plan.md` — the security-testing row and the hostile
  corpus paragraph list "bluemonday+DOMPurify+SSR+real browser". The four
  surfaces stand; "SSR" is not to be read as DOMPurify executing under Node.
- `packages/schema/README.md`,
  `packages/schema/validation/sanitizer-allowlist.v1.json`,
  `packages/schema/validation/hostile-corpus.json`, and `.semgrep.yml` — each
  describes DOMPurify as the "render path" sanitizer. True of the **client**
  render path only.
- `docs/plans/phase-3/decisions.md` D3 reached this conclusion first. This ADR
  ratifies it and makes it authority rather than a plan note.

## Consequences

- The SSR path loses one parser boundary. Three parsers in series (`x/net/html`
  → parse5 → Blink) is where mutation-XSS lives, and this decision leaves two
  (`x/net/html` → Blink). That cost is accepted knowingly. The mitigation is
  that the shared hostile corpus proves neutralization **on the SSR output** —
  bluemonday's committed output through `renderToString` — rather than assuming
  a second sanitizer would have caught what the first missed.
- The Go public-read re-sanitize stops being an "owner-landing item" and becomes
  a required P2B/P5A deliverable. Without it, the defense-in-depth half of this
  decision does not exist and Go's write-time pass is the only barrier.
- Phase 3's Task 3 keeps its interface and its build assertion that the server
  bundle contains no `dompurify`; that assertion is now the mechanical check for
  this ADR and must not be weakened to a source-level grep.
- Assigning `innerHTML` from server-provided HTML on a client path without the
  DOMPurify pass is a defect this ADR names in advance, and is what the
  `.semgrep.yml` rules exist to catch.
- The CSP backstop is unaffected. It was never load-bearing for this split, and
  the renderer-surface baseline CSP stays as phase 3's D5 defines it.
- If a future change needs DOMPurify on the server, it needs a new ADR and it
  needs to justify the `jsdom` dependency against the no-outbound-network
  posture — not merely observe that the spec once implied it.
