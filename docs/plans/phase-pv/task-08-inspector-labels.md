# PV T08 — inspector labels and grouping

## Contract

Make every inspector label a person's word and group the customization panel. No
intent, path, or commit behavior changes; PU's commit-on-blur fields stay.

### Label map

Create `apps/web/app/components/editor/customization/labels.ts`:

```ts
export const FIELD_LABELS: Readonly<Record<string, string>> = {
  "font.family": "Font",
  "font.baseSizePx": "Base size (px)",
  "spacing.sectionGap": "Section gap",
  "spacing.entryGap": "Entry gap",
  "spacing.lineHeight": "Line height",
  "heading.style": "Heading style",
  "heading.showRule": "Heading rule",
  "layout.columns": "Columns",
  "sectionDisplay.skill.style": "Skill display",
  "sectionDisplay.language.style": "Language display",
  pageFormat: "Page size",
  pageMargin: "Page margin",
  // one entry per scalar path in fields.ts; the test enumerates them
};
export const FIELD_GROUPS: ReadonlyArray<{
  title: string;
  paths: readonly string[];
}> = [
  { title: "Type", paths: ["font.family", "font.baseSizePx"] },
  {
    title: "Spacing",
    paths: [
      "spacing.sectionGap",
      "spacing.entryGap",
      "spacing.lineHeight",
      "pageMargin",
    ],
  },
  { title: "Headings", paths: ["heading.style", "heading.showRule"] },
  {
    title: "Layout",
    paths: [
      "layout.columns",
      "pageFormat",
      "sectionDisplay.skill.style",
      "sectionDisplay.language.style",
    ],
  },
];
export function enumLabel(
  path: string,
  value: string | number | boolean,
): string;
```

`enumLabel` returns the font's `displayName` from
`app/assets/fonts/catalog.json` for `font.family`, capitalizes single-word enums
("bar" → "Bar", "dots" → "Dots", "uppercase" → "Uppercase"), maps `letter` →
"Letter" and `a4` → "A4", numbers to their string, and falls back to
`String(value)`. Every path in `fields.ts`'s `scalarFields` must have a label;
the test fails otherwise.

### Panels

- `CustomizationPanel.vue`: render one `fieldset` per `FIELD_GROUPS` entry with
  a `legend` (`text-sm font-medium`), then a final `Colors` fieldset holding the
  existing `ColorField`s in order (Primary, Text, Background, Accent, Surface).
  Each field keeps its `data-field` wrapper and id; the label text comes from
  `FIELD_LABELS`; `SelectField` options show `enumLabel`. Booleans are
  `SwitchField`s. Numbers stay `type="number"` with the same bounds.
- `PersonalDetailsPanel.vue` / `ContactList.vue`: each contact detail is one
  row: `SelectField` (type) and `TextField` (value) side by side on the 8 px
  module, then an `IconButton aria-label="More options for contact detail {n}"`
  opening a `DropdownMenu` with "Set label…" (reveals the label field inline),
  "Hide this detail" (checkbox item, keeps `Hide this detail` text), "Move up",
  "Move down", "Remove detail". Every current `data-action` and `aria-label`
  survives on the menu items.
- Section entry forms keep PU's field layout; their title strings are held.

Strings held: every label and button text asserted by
`test/editor/customization-panel.test.ts`,
`test/editor/personal-details.test.ts`, `editor.spec.ts`, and
`editor-fixtures.ts`, except the customization labels and enum values listed in
the map (hook change: tests that matched `font.family` now match "Font"; tests
that selected the option text `be-vietnam-pro` now select "Be Vietnam Pro" while
the `<option value>` stays `be-vietnam-pro`).

## TDD cases

Write `test/editor/field-labels.test.ts` first: every `scalarFields` path has a
`FIELD_LABELS` entry and belongs to exactly one group; `enumLabel` maps the
catalog IDs to display names, the style enums to capitalized words, and an
unknown value to itself. Update `customization-panel.test.ts` for the group
legends, label text, option labels versus values, and unchanged commit behavior.
Update `personal-details.test.ts` for the row layout and the menu actions
emitting the same intents as before.

## Ownership and checks

Owned paths:

- `apps/web/app/components/editor/customization/CustomizationPanel.vue`,
  `labels.ts`
- `apps/web/app/components/editor/forms/PersonalDetailsPanel.vue`,
  `ContactList.vue`
- `apps/web/test/editor/customization-panel.test.ts`,
  `test/editor/personal-details.test.ts`, `test/editor/field-labels.test.ts`

Acceptance: `AC-UI-003` re-proof, `AC-UI-006`.

Run:

```sh
cd apps/web
npx vitest run test/editor/customization-panel.test.ts test/editor/personal-details.test.ts test/editor/field-labels.test.ts
npx eslint app/components/editor/customization app/components/editor/forms test/editor
npx vue-tsc --noEmit
```

Do not edit `fields.ts` semantics, `fieldIntent.ts`, `app/editor/**`, or Git
state. Report the first failing test, exact commands, and every hook change.
