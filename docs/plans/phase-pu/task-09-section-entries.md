# Task 09 — Section panel, entry forms, dates, rich text

**Acceptance:** AC-UI-002, AC-UI-003, AC-UI-006.

**Depends on:** T03 and T07 (`InspectorPanel`). Runs beside T08 after T07.

**Owned paths:** T09 paths in `file-structure.md`.

## Contract

- `SectionPanel.vue` renders `InspectorPanel` with the section heading, the
  `Add entry` button (`data-action="add-entry"`, `size="sm"`) in the `actions`
  slot, the issue list as `StatusBanner kind="error"` with
  `aria-label="Section issues"`, `tabindex="-1"`, and `Button variant="link"`
  issue buttons (`data-issue`), then one `EntryCard` per entry. The card title
  is `entryLabel`, `entryId`, `hidden`, `index`, and `count` are bound;
  `toggleHidden` calls the existing function; `moveUp`/`moveDown` emit an
  `entryReorder` command with the permuted id list (same permutation rule as
  `EntryOrderControls`); `delete` opens the `ConfirmDialog`
  (`title="Delete entry"`, `description` `Delete {label}?`,
  `confirmLabel="Delete"`, `destructive`,
  `confirmAction="confirm-delete-entry"`, `cancelAction="cancel-delete-entry"`).
  The rebind check in `confirmDelete` is unchanged. `data-section-key`,
  `data-section-id-text`, `data-entry-id-text` stay.
- Each `*EntryFields.vue` replaces `OptionalField` with `TextField`
  (`data-entry-field` attribute on the wrapper, labels unchanged),
  `RichTextEditor` unchanged in usage, selects with `SelectField`
  (`data-entry-field` on the wrapper), and `DateRangeField`/`YearMonthField` as
  rebuilt here. Every `textIntent` helper maps an empty string to
  `{ kind: 'unset' }`; no `clear`.
- `EntryLinkField.vue` wraps `TextField type="url"` and keeps `isLink`; an
  invalid link sets a local `error`
  (`Enter an https:// link, or a mailto: or tel: address.`) and emits nothing.
- `DateRangeField.vue` and `YearMonthField.vue` keep their scripts and render a
  `<fieldset>`-free `div role="group" :aria-labelledby` with a
  `text-sm font-medium` heading (`Date range` / the label), the parts as
  `FormField` + `Input inputmode="numeric"` with `data-part` on the input,
  `Present` as `CheckboxField` (`data-part="present"` on the checkbox), the
  remove action as `Button variant="ghost" size="sm"` (`data-action="unset"`,
  text `Remove date range` / `Remove date`), and the error as the group's
  `role="alert"` paragraph with `data-error="date-order"` and the existing ids.
- `RichTextEditor.vue` keeps its script and the ProseMirror content root
  (`ref="editorRoot"`). The toolbar is a
  `div role="toolbar" aria-label="Rich-text controls"` of
  `IconButton size="icon-sm"` items with the existing `aria-label`s and
  `aria-keyshortcuts`, icons `Pilcrow`, `CornerDownLeft`, `Bold`, `Italic`,
  `Underline`, `ListOrdered`, `List`, `Link`, `Unlink`. The content root gets
  `min-h-24 rounded-md border border-input bg-background px-3 py-2 text-sm focus-within:ring-2 focus-within:ring-ring [&_p]:my-1 [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5 [&_a]:underline`
  because the chrome reset removes list styling.

## Hook changes

- `data-action="set|clear|unset"` on text fields are gone (U4).
- `data-action="delete-entry"` moves onto the `EntryCard` icon button; the
  confirmation is an `alertdialog` on `document.body`; `data-delete-dialog` is
  gone.
- `data-action="toggle-hidden"` is on a `button[role="switch"]` inside the card
  header; tests click it and read `aria-checked`.
- New: `data-action="entry-up"` and `data-action="entry-down"` on the card.
- `[data-part="present"]` is a `button[role="checkbox"]`.

## Strings held

Everything under "Section panel and entries" in the retained hooks list.

## TDD cycle

- [ ] **RED.** Create `test/editor/date-fields.test.ts` from the two date
      describe blocks that T08 removes from `personal-details.test.ts`, changing
      `[data-part="present"]` from `setValue(true)` to `trigger('click')` and
      keeping every intent assertion. In `test/editor/entry-forms.test.ts`
      replace `findAll('input')` counts with label lookups
      (`wrapper.get('[data-entry-field="jobTitle"] [data-field-input]')`),
      replace `[data-action="unset"]` clicks on text fields with
      `setValue('')` + `trigger('blur')`, and add:

  ```ts
  it("maps an emptied description to unset without a clear intent", async () => {
    const wrapper = mount(WorkEntryFields, {
      props: { entry: { id: "e1", description: "<p>Old</p>" } },
    });
    wrapper.getComponent(RichTextEditor).vm.$emit("update:modelValue", "");
    expect(wrapper.emitted("field")?.at(-1)?.[0]).toEqual({
      path: "description",
      intent: { kind: "unset" },
    });
  });

  it("confirms entry deletion through the alert dialog", async () => {
    const { edit, wrapper } = mountSection(
      "work",
      [{ id: "e1", jobTitle: "Dev" }],
      { attachTo: document.body },
    );
    await wrapper
      .get('[data-entry-id="e1"] [data-action="delete-entry"]')
      .trigger("click");
    await nextTick();
    document.body
      .querySelector<HTMLButtonElement>('[data-action="confirm-delete-entry"]')!
      .click();
    expect(edit).toHaveBeenLastCalledWith({
      kind: "entryDelete",
      sectionKey: "work",
      entryId: "e1",
    });
    wrapper.unmount();
  });

  it("reorders entries from the card controls", async () => {
    const { edit, wrapper } = mountSection("work", [
      { id: "e1" },
      { id: "e2" },
    ]);
    await wrapper
      .get('[data-entry-id="e2"] [data-action="entry-up"]')
      .trigger("click");
    expect(edit).toHaveBeenLastCalledWith({
      kind: "entryReorder",
      sectionKey: "work",
      entryIds: ["e2", "e1"],
    });
  });
  ```

  (`mountSection` is the file's existing helper; extend it with mount options.)
  In `test/editor/rich-text.test.ts` keep every assertion; the toolbar buttons
  are still found by `aria-label`.

- [ ] Run and watch the files fail:

  ```sh
  cd apps/web && npx vitest run test/editor/entry-forms.test.ts test/editor/rich-text.test.ts test/editor/date-fields.test.ts
  ```

- [ ] **EntryCard wiring in SectionPanel.vue:**

  ```vue
  <EntryCard
    v-for="(entry, index) in section.entries"
    :key="entry.id"
    :count="section.entries.length"
    :entry-id="entry.id"
    :hidden="entry.isHidden ?? false"
    :index="index"
    :title="entryLabel(entry, index)"
    @delete="openDelete(entry, index)"
    @move-down="reorder(entry.id, 1)"
    @move-up="reorder(entry.id, -1)"
    @toggle-hidden="toggleHidden(entry.id, entry.isHidden)"
  >
    <p class="text-xs text-muted-foreground" data-entry-id-text>{{ entry.id }}</p>
    <component
      :is="selectedComponent"
      :entry="entry"
      @field="edit(entry.id, $event.path, $event.intent)"
    />
  </EntryCard>
  ```

  with

  ```ts
  function reorder(entryId: string, direction: -1 | 1): void {
    const ids = props.section.entries.map((entry) => entry.id);
    const index = ids.indexOf(entryId);
    const next = index + direction;
    if (index < 0 || next < 0 || next >= ids.length) return;
    const [moved] = ids.splice(index, 1);
    if (moved === undefined) return;
    ids.splice(next, 0, moved);
    props.actions.edit({
      kind: "entryReorder",
      sectionKey: props.sectionKey,
      entryIds: ids,
    });
  }
  ```

  `openDelete` no longer needs the event; it stores the entry, index, and
  section type, and `closeDelete` only clears it (the dialog returns focus).

- [ ] **Entry fields.** For every `*EntryFields.vue` replace each
      `OptionalField` with:

  ```vue
  <TextField
    data-entry-field="jobTitle"
    label="Job title"
    :model-value="entry.jobTitle"
    @intent="emit('field', { path: 'jobTitle', intent: $event })"
  />
  ```

  and each `textIntent` helper with:

  ```ts
  function textIntent(value: string): FieldIntent<string> {
    return value === "" ? { kind: "unset" } : { kind: "set", value };
  }
  ```

  Level selects become `SelectField` with options `Not set` (`''`) and `0`–`5`;
  the `update:modelValue` handler keeps the existing numeric mapping.

- [ ] Rebuild `DateRangeField.vue`, `YearMonthField.vue`, `EntryLinkField.vue`,
      and the `RichTextEditor.vue` template per the contract.

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/entry-forms.test.ts test/editor/rich-text.test.ts test/editor/date-fields.test.ts
  make -C ../.. web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T09 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs and the final list of `data-action` values on
`EntryCard`. Suggested commit:
`feat(editor): rebuild section entries on the shared fields`.
