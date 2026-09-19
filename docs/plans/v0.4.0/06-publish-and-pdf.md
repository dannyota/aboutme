# Publish and PDF brief

Role: frontend. Model: `gpt-5.6-terra`. Start after locale foundation and the
page-size copy interface are verified.

## Objective and authority

Localize every publish and owner PDF state while preserving publish commands,
privacy choices, URLs, PDF requests, and returned bytes. Read `AGENTS.md`, the
accepted localization design and ADR, publish and export design,
`AC-EDITOR-018`, and `AC-UI-014`.

## Owned paths

- Create `apps/web/app/i18n/publish.ts`.
- Create `apps/web/app/i18n/pdf.ts`.
- Modify `apps/web/app/components/editor/PublishDialog.vue`.
- Modify `apps/web/app/components/editor/PublishPageFields.vue`.
- Modify `apps/web/app/components/editor/PDFDownloadButton.vue`.
- Modify `apps/web/app/editor/pdfDownload.ts`.
- Modify `apps/web/test/editor/publish-dialog.test.ts`.
- Modify `apps/web/test/editor/publish-controller.test.ts`.
- Modify `apps/web/test/editor/pdf-download.test.ts`.

Do not edit publish API or controller behavior, editor shell, page settings,
renderer, print route, public page, commands, schema, or source manifest.

## Interfaces and behavior

- Export typed `publishCopy` and `pdfCopy`. Consume page-size labels from the
  control-panel task.
- Keep `PublishControllerState`, issue paths, issue codes, auth method, frozen
  attempt, slug, discovery, download choice, emoji, title, and canonical URL
  semantic. Map them to copy only in components.
- Change `PdfDownloadState` to the exact code union in the main plan. Preserve
  status mapping, abort behavior, byte limits, response validation, filename,
  request count, and retry timing.
- Translate pending, blocked, rate-limited, session-ended, provider, uncertain,
  retry, revoke, success, copy-link, and accessible announcement states.
- A locale toggle with an open dialog or shown outcome changes copy without
  replaying save, reauthentication, publish, revoke, copy, or PDF download.
- Unknown issue and failure codes use localized generic safe copy. Never render
  server messages or interpolate user data as HTML.

## Test-first cycle and checks

1. Run `git status --short`. Extend state tables to assert Vietnamese and
   English copy for every existing publish and PDF state.
2. Add open-dialog toggle tests. Freeze exact publish command objects, PDF
   requests, copied URLs, and downloaded byte arrays before and after toggles.
3. Add hostile server-message and unknown-code tests that assert generic text
   and no HTML elements from the message.
4. Run
   `cd apps/web && npx vitest run test/editor/publish-dialog.test.ts test/editor/publish-controller.test.ts test/editor/pdf-download.test.ts`.
   Expect failures for Vietnamese states and stored English PDF messages.
5. Implement the smallest mapping, then rerun the same command and require PASS.

## Definition of done and report

Every publish and PDF state is bilingual and reactive. Commands, URLs, privacy,
requests, and bytes match English behavior exactly. Report exact files, checks
and results, skipped checks with reason, evidence, and open items. Do not
perform Git operations. Use short plain text with no em dash. Do not put plan or
task IDs in code, tests, or comments.
