# 0008: Go is the sole SSR sanitization authority; DOMPurify is client-only

Status: Accepted (2026-08-11).

## Context

The design named two sanitizers, bluemonday (write) and DOMPurify (render), and
one allowlist, but not where each runs. Public resume pages are server-rendered,
so a plain reading put DOMPurify inside the SSR path.

DOMPurify needs a DOM. Running it under Node means shipping `jsdom` as a
production dependency, which drags an HTTP, cookie, and WebSocket stack into the
one process that renders untrusted documents, in a product whose print browser
has no outbound network access.

An SSR DOMPurify pass would also be the only pass. Vue's hydration re-applies
event handlers, value props, and `.prop` keys, never `innerHTML`, so on an SSR
page whatever the server serialized is what the browser keeps.

## Decision

**Go (bluemonday) is the sole sanitization authority for anything SSR renders.
DOMPurify is client-only.**

- The write path runs `sanitize.RichText` on every rich-text write, and the Go
  public read path re-sanitizes as defense in depth, so a document stored before
  an allowlist change cannot be served under the old rules.
- The renderer's `RichText` primitive calls DOMPurify under `import.meta.client`
  only, and always before any `innerHTML` assignment. On SSR it passes the
  string through. The client pass guards surfaces where the server did not
  produce the markup: the editor preview and the SSE refetch re-render.
- The built server bundle contains no `dompurify` and no `jsdom`. `jsdom` stays
  a test-only dev dependency. This is asserted against the built output.
- The shared hostile corpus keeps all four surfaces. SSR is a surface to prove
  neutralization on: the committed bluemonday-output artifact is fed through
  `renderToString` and the neutralization predicate is asserted on the SSR
  output. DOMPurify is conformance-tested against the same corpus in the client
  environment and must be a fixed point over the bluemonday-output artifact.

The [web rich-text design](../design/web.md#rich-text),
[template contract §5.5](../design/templates/contract.md#55-rich-text),
`packages/schema/README.md`, the sanitizer allowlist, the hostile corpus, and
`.semgrep.yml` describe DOMPurify as the client render-path sanitizer.

## Consequences

- The SSR path has two parser boundaries (`x/net/html`, then the browser)
  instead of three. That cost is accepted knowingly; the corpus proves
  neutralization on the SSR output rather than assuming a second sanitizer would
  catch what the first missed.
- The Go public-read re-sanitize is required. Without it, Go's write-time pass
  is the only barrier.
- The build assertion that the server bundle contains no `dompurify` stays and
  must not be weakened to a source-level grep.
- Assigning `innerHTML` from server-provided HTML on a client path without the
  DOMPurify pass is a defect; the `.semgrep.yml` rules exist to catch it.
- The CSP backstop is unaffected. The renderer-surface baseline CSP stays as the
  [rich-text design](../design/web.md#rich-text) defines it.
- Server-side DOMPurify needs a new ADR that justifies `jsdom` against the
  no-outbound-network posture.

## History

Former ADR 0012 (2026-08-11), unchanged in substance. It corrected the design
text in place at the time.
