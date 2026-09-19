# Editor control panels brief

Role: frontend. Model: `gpt-5.6-terra`. Start after the locale foundation report
is verified.

## Objective and authority

Localize customization, page settings, structure, templates, photo, crop, and
rich-text controls without changing resume values or renderer behavior. Read
`AGENTS.md`, the accepted localization design and ADR, template contract,
`docs/design/web.md`, `AC-EDITOR-018`, and `AC-UI-014`.

## Owned paths

- Create `apps/web/app/i18n/editor-controls.ts`.
- Modify `apps/web/app/components/editor/customization/ColorField.vue`.
- Modify `apps/web/app/components/editor/customization/CustomizationPanel.vue`.
- Modify `apps/web/app/components/editor/customization/PageSettings.vue`.
- Modify `apps/web/app/components/editor/customization/labels.ts`.
- Modify `apps/web/app/components/editor/customization/pageSettings.ts`.
- Modify `apps/web/app/components/editor/structure/EntryOrderControls.vue`.
- Modify `apps/web/app/components/editor/structure/SectionControls.vue`.
- Modify `apps/web/app/components/editor/structure/StructurePanel.vue`.
- Modify `apps/web/app/components/editor/templates/TemplatePanel.vue`.
- Modify `apps/web/app/components/editor/templates/TemplatePartialDialog.vue`.
- Modify `apps/web/app/components/editor/templates/TemplateThumbnail.vue`.
- Modify `apps/web/app/components/editor/photo/CropEditor.vue`.
- Modify `apps/web/app/components/editor/photo/PhotoPanel.vue`.
- Modify `apps/web/app/components/editor/richtext/RichTextEditor.vue`.
- Modify `apps/web/test/editor/customization-controls.test.ts`.
- Modify `apps/web/test/editor/page-settings.test.ts`.
- Modify `apps/web/test/editor/structure-controls.test.ts`.
- Modify `apps/web/test/editor/template-panel.test.ts`.
- Modify `apps/web/test/editor/template-thumbnail.test.ts`.
- Modify `apps/web/test/editor/photo-panel.test.ts`.
- Modify `apps/web/test/editor/photo-crop.test.ts`.
- Modify `apps/web/test/editor/photo-position-control.test.ts`.
- Modify `apps/web/test/editor/rich-text.test.ts`.

Do not edit schema enums, template definitions, renderer code, section content,
editor shell, publish, PDF, commands, stores, or source manifest.

## Interfaces and behavior

- Export typed `editorControlsCopy`. Export locale-aware page-size label and
  help functions for the PDF task while retaining `PageFormat` and stable short
  values such as `A4` and `Letter`.
- Separate group identifiers from translated group headings. Logic must compare
  stable IDs, never translated labels.
- Translate display names for enum values without changing the values sent in
  customization commands.
- Preserve template IDs and names, authored section headings, layout members,
  colors, dimensions, photo bytes and crop, rich-text HTML, selection, and held
  edits across a locale toggle.
- A shown code-based customization error, photo status, or crop error updates
  reactively. Unknown codes use generic safe copy.

## Test-first cycle and checks

1. Run `git status --short`. Add failing table-driven tests for both locales and
   stable value-label pairs.
2. Add toggle tests with an open template dialog, custom value, photo/crop
   state, and rich-text selection. Assert no emitted command or value changes.
3. Run
   `cd apps/web && npx vitest run test/editor/customization-controls.test.ts test/editor/page-settings.test.ts test/editor/structure-controls.test.ts test/editor/template-panel.test.ts test/editor/template-thumbnail.test.ts test/editor/photo-panel.test.ts test/editor/photo-crop.test.ts test/editor/photo-position-control.test.ts test/editor/rich-text.test.ts`.
   Expect failures for Vietnamese copy and reactive visible states.
4. Implement the smallest mapping, then rerun the same command and require PASS.

## Definition of done and report

Every owned control is bilingual and emits the same semantic values in both
locales. Report exact files, checks and results, skipped checks with reason,
evidence, and open items. Do not perform Git operations. Use short plain text
with no em dash. Do not put plan or task IDs in code, tests, or comments.
