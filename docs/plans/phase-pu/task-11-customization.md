# Task 11 — Customization panel

**Acceptance:** AC-UI-002, AC-UI-003, AC-UI-006.

**Depends on:** T07 (`InspectorPanel`), T03.

**Owned paths:** T11 paths in `file-structure.md`.

## Contract

- `CustomizationPanel.vue` keeps its script (delta commits, validation, local
  errors, issue focus) and renders `InspectorPanel` (`title="Customization"`,
  `titleId="customization-title"`) with five `Card` groups in this order:
  `Typography` (`font.family`, `font.baseSizePx`), `Colors` (the five
  `ColorField`s), `Spacing` (`spacing.sectionGap`, `spacing.entryGap`,
  `spacing.lineHeight`, then the `Page margins` switch group),
  `Headings and header` (`heading.style`, `heading.showRule`, then the `Header`
  switch group), and `Layout` (`layout.columns`, `layout.surfaceTarget` with its
  remove button, `sectionDisplay.skill.style`, `sectionDisplay.language.style`,
  `pageFormat`, `dateFormat`).
- Scalar fields render through `SelectField` (enumerations), `CheckboxField`
  (booleans), or `FormField` + `Input type="number"` (numbers) with the existing
  ids (`fieldId(path)`), `data-field` on the wrapper (`name`),
  `aria-invalid`/`aria-describedby` from the local error, and the error
  paragraph id `errorId(path)` with `data-error-for`.
- Labels come from a frozen map in the panel, not the raw path: `font.family`
  `Font family`, `font.baseSizePx` `Base size (px)`, `spacing.sectionGap`
  `Section gap`, `spacing.entryGap` `Entry gap`, `spacing.lineHeight`
  `Line height`, `heading.style` `Heading style`, `heading.showRule`
  `Show heading rule`, `layout.columns` `Columns`, `sectionDisplay.skill.style`
  `Skill display`, `sectionDisplay.language.style` `Language display`,
  `pageFormat` `Page format`, `dateFormat` `Date format`. Option labels stay the
  raw enumeration values.
- `OptionalCustomizationField.vue` is deleted. Each optional group is a
  `SwitchField` (`label` `Page margins` / `Header`, `data-action="page-margin"`
  / `data-action="header"` on the switch) whose `update:modelValue` calls
  `enablePageMargin`/`unsetPageMargin` or `enableHeader`/`unsetHeader`, followed
  by the group's fields inside a `div` that is hidden (`v-if`) while the group
  is absent.
- `ColorField.vue` keeps its script and renders `FormField` + `Input`
  (`type="text"`, `spellcheck="false"`, `inputmode="text"`) with a colour swatch
  (`span` with `style` `background-color` bound to the valid value) and, when
  not required, a `Button variant="ghost" size="sm"` `Remove` with the existing
  `data-action`. The `relatedTarget` guard stays.

## Hook changes

- `data-action="enable-page-margin"`, `unset-page-margin`, `enable-header`,
  `unset-header` become the switches `data-action="page-margin"` and
  `data-action="header"` (`aria-checked` reflects presence).
- Scalar labels change from raw paths to the map above; tests locate fields by
  `data-field`.

## Strings held

Everything under "Customization" in the retained hooks list except the raw path
labels.

## TDD cycle

- [ ] **RED.** In `test/editor/customization-controls.test.ts` keep every
      `[data-field="..."] select` / `input` selector and `setValue()` call
      (native selects and number inputs survive), replace checkbox `setValue`
      calls with clicks on `[data-field="..."] [role="checkbox"]`, replace the
      enable/unset button clicks with:

  ```ts
  it("enables and removes page margins through the switch", async () => {
    const { edit, wrapper } = mountPanel();
    const toggle = wrapper.get('[data-action="page-margin"]');
    expect(toggle.attributes("role")).toBe("switch");
    expect(toggle.attributes("aria-checked")).toBe("false");
    await toggle.trigger("click");
    expect(edit).toHaveBeenLastCalledWith({
      kind: "customization",
      deltas: [
        { op: "set", path: "spacing.pageMargin.x", value: 15 },
        { op: "set", path: "spacing.pageMargin.y", value: 15 },
      ],
    });
    await setCustomization(wrapper, {
      spacing: { pageMargin: { x: 15, y: 15 } },
    });
    await wrapper.get('[data-action="page-margin"]').trigger("click");
    expect(edit).toHaveBeenLastCalledWith({
      kind: "customization",
      deltas: [{ op: "unset", path: "spacing.pageMargin" }],
    });
  });

  it("labels scalar fields for people, not paths", () => {
    const { wrapper } = mountPanel();
    expect(wrapper.get('[data-field="font.family"] label').text()).toBe(
      "Font family",
    );
    expect(wrapper.text()).not.toContain("font.baseSizePx");
  });
  ```

  (`mountPanel` and `setCustomization` are the file's helpers; add
  `setCustomization` if absent as a `setProps` on a cloned record.)

- [ ] Run and watch the file fail:

  ```sh
  cd apps/web && npx vitest run test/editor/customization-controls.test.ts
  ```

- [ ] **Scalar field rendering.** Replace the `v-for="field in scalarFields"`
      block with a `renderField(field)` per group:

  ```vue
  <template v-for="field in group.fields" :key="field.path">
    <CheckboxField
      v-if="field.kind === 'boolean'"
      :id="fieldId(field.path)"
      :label="labelFor(field.path)"
      :model-value="displayValue(field.path, false) === true"
      :name="field.path"
      @update:model-value="commitBoolean(field, $event)"
    />
    <SelectField
      v-else-if="field.kind === 'enum'"
      :id="fieldId(field.path)"
      :error="localError(field.path) || undefined"
      :label="labelFor(field.path)"
      :model-value="displayValue(field.path, '') as string | number"
      :name="field.path"
      :options="
        valuesFor(field).map((value) => ({ value, label: String(value) }))
      "
      @update:model-value="commitEnum(field, $event)"
    />
    <FormField
      v-else
      v-slot="{ id, describedBy, invalid }"
      :id="fieldId(field.path)"
      :error="localError(field.path) || undefined"
      :label="labelFor(field.path)"
      :name="field.path"
    >
      <Input
        :id="id"
        :aria-describedby="describedBy"
        :aria-invalid="invalid"
        :max="field.maximum"
        :min="field.minimum"
        :model-value="displayValue(field.path, 0)"
        :step="field.kind === 'integer' ? 1 : 'any'"
        type="number"
        @change="changeField(field, $event)"
      />
    </FormField>
  </template>
  ```

  `commitBoolean(field, value)` and `commitEnum(field, value)` call the existing
  `isAllowed` and `commit` with the typed value (numeric enumerations cast with
  `Number`). The `FormField` error paragraph already carries
  `id="{fieldId}-error"`, `role="alert"`, and `data-error-for`, so the separate
  `<p>` blocks are deleted. Define
  `const GROUPS = [{ title: 'Typography', fields: [...] }, ...]` from
  `CUSTOMIZATION_FIELDS` by path.

- [ ] Rebuild the colour, margin, header, and layout blocks per the contract;
      delete `OptionalCustomizationField.vue`.

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/customization-controls.test.ts
  make web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T11 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs and the final label map. Suggested commit:
`feat(editor): rebuild the customization panel`.
