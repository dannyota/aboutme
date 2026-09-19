# Resume list and creation brief

Role: frontend. Model: `gpt-5.6-terra`. Start only after the locale foundation
report is verified.

## Objective and authority

Localize the resume list, create dialog, and `/app/new` while preserving every
resume, sample, title, and captured resume-language value. Read `AGENTS.md`, the
accepted localization design and ADR, the template contract, `AC-EDITOR-018`,
and `AC-UI-014`.

## Owned paths

The list author also owns an optional close-label prop in
`apps/web/app/components/app/FormDialog.vue` and
`apps/web/app/components/ui/dialog/DialogContent.vue`. Workspace callers pass
localized close copy; all other callers keep the existing English default.

- Create `apps/web/app/i18n/resume-list.ts`.
- Create `apps/web/app/i18n/resume-create.ts`.
- Modify `apps/web/app/pages/app/resumes/index.vue`.
- Modify `apps/web/app/pages/app/new.vue`.
- Modify `apps/web/app/components/editor/list/ResumeList.vue`.
- Modify `apps/web/app/components/editor/list/CreateResumeDialog.vue`.
- Modify `apps/web/app/components/editor/list/RenameResumeDialog.vue`.
- Modify `apps/web/app/components/editor/list/DeleteResumeDialog.vue`.
- Modify `apps/web/app/components/editor/resumeLanguage.ts`.
- Modify `apps/web/app/composables/useResumeList.ts`.
- Modify `apps/web/app/utils/relativeTime.ts`.
- Modify `apps/web/test/app/new.test.ts`.
- Modify `apps/web/test/app/relative-time.test.ts`.
- Modify `apps/web/test/editor/resume-list.test.ts`.
- Modify `apps/web/test/templates/start.test.ts`.

Do not edit shared shell, other editor components, API or coordinator files,
samples, template data, renderer code, or the source manifest.

## Interfaces and behavior

- Export typed `resumeListCopy` and `resumeCreateCopy` catalogs.
- Replace `createStatusMessage()` with `createNotice()` and replace stored
  `actionMessage` with `actionNotice` using the exact unions in the main plan.
  Components map notices to current-locale copy at render time.
- `resumeLanguage.ts` exports locale-aware option, hint, and validation copy.
  Values remain `vi`, `en`, `OTHER_LANGUAGE`, and `UNSET_LANGUAGE`.
- `formatRelativeTime(iso, now, locale = 'en')` adds Vietnamese output while
  preserving the English default used by settings.
- The literal confirmation token remains `DELETE` in both languages. User titles
  render only through text bindings.
- `/app/new` captures the opening locale in a nonreactive value for blank
  starts. Sample query language remains authoritative. Locale changes must not
  rerun `startDocument()`, reset `title`, or alter the create payload.
- Create-dialog validation and uncertain outcomes store codes or booleans, not
  translated strings, so open errors react to locale changes.

## Test-first cycle and checks

1. Run `git status --short`. Add failing tests for both locales, invalid-cookie
   default, relative times, `DELETE`, safe hostile titles, cap and unknown
   outcomes, and blank and sample toggle invariants.
2. In the sample and blank tests, snapshot the title, selected language, sample
   identity, document object, and create arguments before and after a toggle.
   Assert no create call occurs from the toggle.
3. Run
   `cd apps/web && npx vitest run test/app/new.test.ts test/app/relative-time.test.ts test/editor/resume-list.test.ts test/templates/start.test.ts`.
   Expect failures on Vietnamese copy and locale-toggle preservation.
4. Implement the smallest change, then rerun the same command and require PASS.

## Definition of done and report

List, loading, empty, menu, create, rename, delete, validation, retry, and limit
states render in both languages. Blank and sample data remain unchanged after a
toggle. Report exact files, checks and results, skipped checks with reason,
evidence, and open items. Do not perform Git operations. Use short plain text
with no em dash. Do not put plan or task IDs in code, tests, or comments.
