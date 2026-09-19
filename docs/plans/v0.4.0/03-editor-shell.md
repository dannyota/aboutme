# Editor shell and state brief

Role: frontend. Model: `gpt-5.6-terra`. Start after the locale foundation report
is verified.

## Objective and authority

Localize editor route chrome, page state, save state, errors, conflicts, preview
controls, and the editor language control without remounting editor state. Read
`AGENTS.md`, the accepted localization design and ADR, `docs/design/web.md`,
`AC-EDITOR-018`, and `AC-UI-014`.

## Owned paths

- Create `apps/web/app/i18n/editor-shell.ts`.
- Modify `apps/web/app/pages/app/resumes/[id].vue`.
- Modify `apps/web/app/components/editor/EditorShell.vue`.
- Modify `apps/web/app/components/editor/ErrorSummary.vue`.
- Modify `apps/web/app/components/editor/ConflictPanel.vue`.
- Modify `apps/web/app/components/editor/SaveStatus.vue`.
- Modify `apps/web/app/components/editor/PreviewToolbar.vue`.
- Modify `apps/web/app/components/editor/EditorPreview.vue`.
- Modify `apps/web/test/editor/editor-shell.test.ts`.
- Modify `apps/web/test/editor/accessibility.test.ts`.
- Modify `apps/web/test/editor/conflicts.test.ts`.
- Modify `apps/web/test/editor/editor-preview.test.ts`.
- Modify `apps/web/test/editor/navigation-guard.test.ts`.
- Modify `apps/web/test/nuxt/editor-config.test.ts`.

Do not edit field, customization, structure, template, photo, rich-text,
publish, PDF, controller, or shared-shell files.

## Interfaces and behavior

- Export typed `editorShellCopy`. It owns toolbar, outline, preview, load,
  retry, save, conflict, issue-summary, focus action, and accessibility copy.
- Use `LocaleToggle` with `test-id="workspace-locale"` in the editor top bar.
- Generic loading titles use `workspaceTitles[locale].editor`. A loaded editor
  keeps the authored resume title in `<page> · aboutme` without translation.
- Error and conflict functions accept semantic codes or command kinds. Unknown
  validation codes use localized generic safe text. Never render API messages.
- Keep authored section names and resume preview text unchanged. The preview
  root retains `metadata.lng`; only surrounding controls use interface locale.
- A toggle updates a visible issue, conflict, load failure, preview status, and
  save announcement while preserving focus, open inspector, narrow view, record
  identity, drafts, commands, and revision. It sends no request.

## Test-first cycle and checks

1. Run `git status --short`. Add failing Vietnamese and English tests plus a
   mounted toggle test with a dirty draft, focused control, visible issue, open
   conflict, and mocked write transport.
2. Run
   `cd apps/web && npx vitest run test/editor/editor-shell.test.ts test/editor/accessibility.test.ts test/editor/conflicts.test.ts test/editor/editor-preview.test.ts test/editor/navigation-guard.test.ts test/nuxt/editor-config.test.ts`.
   Expect failures for Vietnamese labels, reactive state copy, and editor title.
3. Implement the smallest component-only mapping. Keep action identifiers and
   `data-*` hooks unchanged.
4. Rerun the same command and require PASS.

## Definition of done and report

All shell and state copy reacts safely. Resume content and preview language do
not change. Report exact files, checks and results, skipped checks with reason,
evidence, and open items. Do not perform Git operations. Use short plain text
with no em dash. Do not put plan or task IDs in code, tests, or comments.
