# Task 7.1.7: Owner PDF download control

Let the owner download the saved resume from the editor.

## Authorities and ownership

Read AGENTS.md, `docs/design/product.md`, `docs/design/web.md`, the owner PDF
contract in task 7.1.5, and the existing editor save/publish coordinator.
Acceptance: AC-PDF-004. One author owns:

- `apps/web/app/components/editor/EditorShell.vue` and a new PDF download
  control.
- `apps/web/app/editor/pdfDownload.ts` and related new export-only modules.
- `apps/web/app/composables/useResumeEditor.ts`.
- Existing editor action/shell test files and new download tests under
  `apps/web/test/editor/`.

Root owns generated clients, manifests, browser harnesses, and all other paths.

## Behavior

Add an accessible Download PDF button beside Publish. Clicking flushes pending
saves through the existing coordinator and waits for accepted state. Do not
export through unresolved conflicts, failed saves, pending uncertain writes,
partial-template recovery, opaque-photo recovery, or a lost/changed session.
Draft completeness is not a download gate. Do not run publish validation or
publish the resume. Reuse existing save-state helpers where they fit.

Request exactly GET `/api/v1/resumes/<canonical UUID>/pdf`, same-origin with the
owner session, no query/body/schema/conditional headers. Display a pending state
and prevent duplicate submissions. Verify status, media type, no-store, and the
16,777,216-byte bound while reading the response before creating a Blob URL. Use
fixed `resume.pdf` filename; do not trust a server-provided filename or URL.
Abort on unmount, changed owner/session, or superseded editor. Revoke object
URLs and discard stale responses. Errors use short fixed messages and an
aria-live region; never show raw body or dependency errors. A rate limit or
render failure can be retried by a later deliberate click, with no automatic
retry loop.

The exported document is the saved snapshot selected by Go after queue
admission. Do not add a new client-side revision or page-count authority.
Preserve the narrow editor header and existing keyboard/focus behavior.

Write failing tests first for save ordering, blocked states, duplicate clicks,
status/body/media/bounds validation, session changes, cancellation, cleanup,
fixed filename, and accessibility labels. Use dependency injection for fetch,
object URLs, and download effects, following existing editor test patterns.

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/web && npx vitest run test/editor && npx eslint app/components/editor/EditorShell.vue app/components/editor/PDFDownloadButton.vue app/editor/pdfDownload.ts app/composables/useResumeEditor.ts test/editor'
```

If helper paths differ, report the exact adjusted ESLint paths. All heavy
commands use the shared lock. No Git, manifests, generated files, secrets,
containers, full CI, or recorded browser automation. Root authors the browser
proof after observing the live UI through Playwright MCP.

Report owned changes, RED evidence, exact checks, remaining uncertainty, and any
required root integration edits.
