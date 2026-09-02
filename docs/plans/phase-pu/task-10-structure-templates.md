# Task 10 — Structure and template panels

**Acceptance:** AC-UI-002, AC-UI-005, AC-UI-006.

**Depends on:** T07 (`InspectorPanel`), T03.

**Owned paths:** T10 paths in `file-structure.md`.

## Contract

- `StructurePanel.vue` keeps its script except the delete-dialog focus code and
  renders `InspectorPanel` (`title="Sections"`, `titleId="structure-title"`).
  The create form is a `Card` titled `Add a section` with
  `data-testid="section-create-form"` on its form and `SelectField`
  `Section type` (`controlAttrs: { 'data-action': 'section-type' }`),
  `SelectField` `Column`, then `FormField` + `Input` with `v-model` for the
  plain `Section name` and `Icon key` drafts, and
  `Button type="submit" data-action="create"` `Add section`. Status and issue
  texts render through `StatusBanner`. Each placed section is a `Card`
  (`data-section`) holding `SectionControls` and `EntryOrderControls`; the
  delete confirmation is `ConfirmDialog` (`title="Delete section"`, description
  `This permanently deletes {key} and its entries.`,
  `confirmLabel="Delete section"`, `destructive`,
  `confirmAction="confirm-delete"`, `cancelAction="cancel-delete"`); the two
  conflict notices are `StatusBanner kind="info"` with their buttons.
- `SectionControls.vue` renders a header row with the key as a `Badge`,
  `FormField` + `Input` for `Section name` (`data-action="displayName"` on the
  input, commit on `change` as today) and `Icon key` (`data-action="iconKey"`),
  and a `div aria-label="Section placement controls"` of
  `Button size="sm" variant="outline"` items with the existing `data-action`
  values and texts; `disabled` propagates to every control.
- `EntryOrderControls.vue` renders
  `div data-entry-order aria-label="Entry order"` with an `ol` of rows: the id
  in `text-xs text-muted-foreground` and two `IconButton`s `Move entry up`
  (`data-action="entry-up"`) and `Move entry down` (`data-action="entry-down"`).
- `TemplatePanel.vue` keeps its script and renders `InspectorPanel`
  (`title="Templates"`, `titleId="template-title"`), the status as
  `StatusBanner kind="info"`, and `ul aria-label="Template presets"` of `Card`s
  (`data-template`) each with the name as `CardTitle`, the description, the
  warnings as `ul aria-label="Template warnings"` in
  `text-xs text-muted-foreground`, and an `Apply` `Button size="sm"` in
  `CardFooter`. The whole card is no longer clickable (`@click.self` removed;
  the button is the only trigger). `data-template-preview` and `undo-template`
  stay.
- `TemplatePartialDialog.vue` renders `AlertDialog :open="true"` with the title
  `Template changes need review`, the state message, the progress
  `ul aria-label="Template change progress"`, the reason as
  `StatusBanner kind="error"`, and three footer `Button`s with the existing
  `data-action` values; Escape emits no recovery action and does not dismiss the
  controlled dialog (the parent unmounts it only when the state changes), and
  initial focus is `Retry remaining`.

## Hook changes

- Delete-section confirmation is an `alertdialog` on `document.body`.
- The template card itself no longer applies on click; `Apply` does.
- Section name and icon key inputs sit inside `FormField` wrappers; their
  `data-action` stays on the input.

## Strings held

Everything under "Structure and templates" in the retained hooks list.

## TDD cycle

- [ ] **RED.** In `test/editor/structure-controls.test.ts` change every
      `wrapper.get('select')` to `wrapper.get('[data-action="section-type"]')`
      or `wrapper.get('[data-field="column"] select')` (give the column
      `SelectField` `name="column"`), keep `setValue()` on them, rewrite the
      delete-dialog cases to `attachTo: document.body` + `document.body`
      queries, and add:

  ```ts
  it("creates a section from the card form and clears the drafts", async () => {
    const { edit, wrapper } = mountStructure();
    await wrapper.get('[data-action="section-type"]').setValue("project");
    await wrapper
      .get('[data-field="displayName"] input')
      .setValue("Side projects");
    await wrapper.get('[data-testid="section-create-form"]').trigger("submit");
    expect(edit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        kind: "structure",
        commands: [
          expect.objectContaining({
            op: "createSection",
            key: "project",
            displayName: "Side projects",
          }),
        ],
      }),
    );
    expect(
      (
        wrapper.get('[data-field="displayName"] input')
          .element as HTMLInputElement
      ).value,
    ).toBe("");
  });
  ```

  In `test/editor/template-panel.test.ts` replace any card `click` with a click
  on `[data-template="{id}"] button` and rewrite the partial-dialog cases to
  body queries; keep every assertion on texts, `data-template-preview`, and
  `undo-template`.

- [ ] Run and watch the files fail:

  ```sh
  cd apps/web && npx vitest run test/editor/structure-controls.test.ts test/editor/template-panel.test.ts
  ```

- [ ] **SectionControls.vue template:**

  ```vue
  <template>
    <div class="grid gap-3">
      <div class="flex items-center justify-between gap-2">
        <Badge variant="outline">{{ sectionKey }}</Badge>
      </div>
      <FormField v-slot="{ id }" label="Section name" name="displayName">
        <Input
          :id="id"
          data-action="displayName"
          :disabled="disabled"
          :model-value="section.displayName ?? ''"
          @change="changeDisplayName"
        />
      </FormField>
      <FormField v-slot="{ id }" label="Icon key" name="iconKey">
        <Input
          :id="id"
          data-action="iconKey"
          :disabled="disabled"
          :model-value="section.iconKey ?? ''"
          @change="changeIconKey"
        />
      </FormField>
      <div aria-label="Section placement controls" class="flex flex-wrap gap-2">
        <Button
          data-action="move-up"
          :disabled="disabled || index === 0"
          size="sm"
          variant="outline"
          @click="emit('move', { ...action(), column, index: index - 1 })"
          >Move up</Button
        >
        <Button
          data-action="move-down"
          :disabled="disabled || index === sectionCount - 1"
          size="sm"
          variant="outline"
          @click="emit('move', { ...action(), column, index: index + 1 })"
          >Move down</Button
        >
        <Button
          v-if="column === 'sidebar'"
          data-action="move-main"
          :disabled="disabled"
          size="sm"
          variant="outline"
          @click="emit('move', { ...action(), column: 'main', index: 0 })"
          >Move to main</Button
        >
        <Button
          v-else
          data-action="move-sidebar"
          :disabled="disabled"
          size="sm"
          variant="outline"
          @click="
            emit('move', {
              ...action(),
              column: 'sidebar',
              index: sidebarCount,
            })
          "
          >Move to sidebar</Button
        >
        <Button
          data-action="reorder"
          :disabled="disabled || index === 0"
          size="sm"
          variant="outline"
          @click="emit('reorder', { ...action(), column, index: 0 })"
          >Move to start</Button
        >
        <Button
          class="text-destructive"
          data-action="delete"
          :disabled="disabled"
          size="sm"
          variant="ghost"
          @click="emit('delete', action())"
          >Delete section</Button
        >
      </div>
    </div>
  </template>
  ```

  `changeDisplayName` and `changeIconKey` keep reading `event.target.value`.

- [ ] Rebuild `StructurePanel.vue`, `EntryOrderControls.vue`,
      `TemplatePanel.vue`, and `TemplatePartialDialog.vue` templates per the
      contract. In `StructurePanel.vue` delete `deleteDialog`,
      `deleteReturnFocus`, `confirmDeleteButton`, and `onDeleteDialogKeydown`;
      `closeDelete` becomes `pendingDelete.value = null`.

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/structure-controls.test.ts test/editor/template-panel.test.ts
  make -C ../.. web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T10 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs. Suggested commit:
`feat(editor): rebuild structure and template panels`.
