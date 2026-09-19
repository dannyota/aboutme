# Editor fields brief

Role: frontend. Model: `gpt-5.6-terra`. Start after the list task's
`resumeLanguage.ts` interface is verified.

## Objective and authority

Localize personal details, resume-language controls, section fields, entry
fields, dates, links, levels, and entry-card actions. Read `AGENTS.md`, the
accepted localization design and ADR, `docs/design/web.md`, `AC-EDITOR-018`, and
`AC-UI-014`.

## Owned paths

- Create `apps/web/app/i18n/editor-fields.ts`.
- Modify `apps/web/app/components/editor/EntryCard.vue`.
- Modify `apps/web/app/components/editor/forms/ContactList.vue`.
- Modify `apps/web/app/components/editor/forms/DateRangeField.vue`.
- Modify `apps/web/app/components/editor/forms/PersonalDetailsPanel.vue`.
- Modify `apps/web/app/components/editor/forms/ResumeLanguageField.vue`.
- Modify `apps/web/app/components/editor/forms/SectionPanel.vue`.
- Modify `apps/web/app/components/editor/forms/YearMonthField.vue`.
- Modify
  `apps/web/app/components/editor/forms/entries/CertificateEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/CustomEntryFields.vue`.
- Modify
  `apps/web/app/components/editor/forms/entries/EducationEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/EntryLinkField.vue`.
- Modify `apps/web/app/components/editor/forms/entries/LanguageEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/ProfileEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/ProjectEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/SkillEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/WorkEntryFields.vue`.
- Modify `apps/web/app/components/editor/forms/entries/levels.ts`.
- Modify `apps/web/test/editor/field-labels.test.ts`.
- Modify `apps/web/test/editor/personal-details.test.ts`.
- Modify `apps/web/test/editor/date-fields.test.ts`.
- Modify `apps/web/test/editor/entry-forms.test.ts`.
- Modify `apps/web/test/editor/resume-language.test.ts`.
- Modify `apps/web/test/editor/field-drafts.test.ts` for locale-aware fixtures.

Do not edit `resumeLanguage.ts`, editor shell, control panels, renderer,
commands, stores, API code, or source manifest.

## Interfaces and behavior

- Export typed `editorFieldsCopy`. Keep field paths, command kinds, section
  keys, contact types, language-level values, link values, and dates semantic.
- Consume the locale-aware resume-language interface from the list task.
- Store validation codes or invalid booleans. Compute shown error, hint,
  placeholder, option, toolbar, and accessible copy from current locale.
- Interface locale never changes `metadata.lng`. An explicit resume-language
  edit keeps the existing command and does not translate authored fields.
- Date drafts, invalid partial dates, focused input, rich text held by a child,
  and selection survive a locale toggle. The toggle sends no edit command.
- User-entered labels, custom headings, URLs, and field values render as text
  and remain byte-for-byte equal.

## Test-first cycle and checks

1. Run `git status --short`. Add failing table-driven copy tests for every field
   group and both locales.
2. Add a mounted toggle test for an invalid partial date and a focused dirty
   field. Assert value, selection, focus, emitted intents, and resume language
   are unchanged while error and label copy change.
3. Run
   `cd apps/web && npx vitest run test/editor/field-labels.test.ts test/editor/personal-details.test.ts test/editor/date-fields.test.ts test/editor/entry-forms.test.ts test/editor/resume-language.test.ts`.
   Expect failures for Vietnamese labels and reactive draft errors.
4. Implement the smallest copy mapping, then rerun the same command and require
   PASS.

## Definition of done and report

All field labels, hints, options, validation, and accessible controls are
bilingual with unchanged commands and data. Report exact files, checks and
results, skipped checks with reason, evidence, and open items. Do not perform
Git operations. Use short plain text with no em dash. Do not put plan or task
IDs in code, tests, or comments.
