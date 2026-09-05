# Task 7.1.3: Capability-gated print SSR

Implement the Nuxt side of the [private print contract](print-contract.md).
Authorities: ADR 0023, web/security design, and template print behavior.
Acceptance: `AC-PDF-002`, `AC-PDF-004`, and `AC-SEC-001`.

Owner: one Sol author. Exclusive paths:

- `apps/web/server/routes/print/[id].get.ts`.
- `apps/web/server/utils/print/` and `apps/web/server/workers/print/`.
- `apps/web/app/components/print/` and `apps/web/test/print/`.

No changes to manifests, Nuxt/Vitest configuration, generated files, shared
renderer components, public worker, Git, containers, or credentials. Report
required build wiring for the integration owner. Existing renderer CSS and fonts
must be reused; do not create a second layout implementation.

## Interfaces

Export `PrintEnvelope` and `decodePrintEnvelope(source: string): PrintEnvelope`
from `server/utils/print/envelope.ts`,
`renderPrintResume(envelope): Promise<string>` from
`server/workers/print/render.ts`, and a bounded `runPrintWorker` using the
existing public-worker lifecycle pattern. Expose a testable handler factory and
redemption client factory with injected transport, configured origin, and abort
signal. Production must use the closed allowlist in the private contract.

The integration owner supplies build aliases `#print-document-validator`,
`#print-worker-url`, and static assets `/_nuxt/assets/print.css` and
`/_nuxt/assets/print-fonts.css`. The validator is generated from the existing
OpenAPI `PublicResumeDocument` subtree. Do not copy its schema by hand. The
worker bundles the existing SSR sanitizer boundary in place of DOMPurify.

Use the private payload's document with `PublicResumeApp` or the same renderer
components. No synthetic public slug, session, or public URL is needed. Adapt
its data photo to the renderer's explicit photo context; no storage key crosses
the wire. The root includes print-document and revision markers.

## Test cycle

- [ ] Write and observe a failing route test: a bare resume ID returns the
      generic print 404 without calling redemption or starting a worker.
- [ ] Add one-use successful redemption through a controlled HTTP transport.
- [ ] Implement exact headers, paths, methods, caps, direct-origin restriction,
      cookie/redirect refusal, cancellation, and generic errors from the
      contract.
- [ ] Test duplicate headers and JSON keys, unknown fields, wrong resume ID, bad
      revision/schema/language, non-data photo URLs, malformed base64, photo
      overflow, and maximum body/HTML plus one byte.
- [ ] Test hostile content neutralization using Go-sanitized corpus input,
      frozen input, renderer parity, absent client sanitizer, and script-free
      HTML/CSP.
- [ ] Test worker cancellation, deadline, malformed/duplicate result messages,
      process exit, and late delivery. A failed call must join its worker.
- [ ] Run the narrow checks from repository root; build-dependent tests may
      await the integration owner's aliases, but report them explicitly:

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/web && npx vitest run test/print && npx eslint server/utils/print server/workers/print server/routes/print app/components/print test/print'
```

Report exact failing-first and passing commands, changed files, required build
wiring, and any unresolved interface. No Git operations or full CI.
