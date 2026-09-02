# Task 08 — Personal details and contacts

**Acceptance:** AC-UI-002, AC-UI-003, AC-UI-006.

**Depends on:** T03 and T07 (`InspectorPanel`). Runs beside T09 after T07.

**Owned paths:** T08 paths in `file-structure.md`.

## Contract

- `PersonalDetailsPanel.vue` renders `InspectorPanel`
  (`title="Personal details"`, `titleId="personal-details-title"`) with two
  `TextField`s (`name="fullName"` label `Full name`, `name="headline"` label
  `Headline`) whose `error` is the mapped issue message, then `ContactList`. The
  issue list keeps its `data-issue` buttons (`Button variant="link" size="sm"`).
  `editText` maps `set` to `{ present: true, value }` and `unset` to
  `{ present: false }`; there is no `clear` branch.
- `ContactList.vue` keeps its script (array replace semantics, URL check,
  16-item limit) and renders a `Card` per detail (`data-detail-index`,
  `data-detail-id` on the title) with: `SelectField` label `Type`
  (`controlAttrs: { 'data-detail-type': '' }`), `TextField` label `Label`
  (`controlAttrs: { 'data-detail-label': '' }`; `set` → `changeLabel`, `unset` →
  `unsetLabel`), `TextField` label `Value`
  (`controlAttrs: { 'data-detail-value': '' }`, `error` when `urlError` matches;
  `errorAttrs: { 'data-error': 'contact-url' }` when it matches; `set` →
  `changeValue`, `unset` → `changeValue(id, '')`), `CheckboxField` label
  `Hide this detail` (`data-detail-is-hidden` on the checkbox), and a
  right-aligned `IconButton` row: `Move up` (`data-action="move-detail-up"`),
  `Move down` (`data-action="move-detail-down"`), `Remove detail`
  (`data-action="remove-detail"`). Below the cards:
  `Button variant="outline" size="sm"` `Add detail` (`data-action="add-detail"`)
  and `Button variant="ghost" size="sm"` `Remove contact list`
  (`data-action="unset-details"`). The limit error is
  `StatusBanner kind="error"` with `data-error="detail-limit"`.

## Hook changes

- `data-action="set|clear|unset"` buttons under text fields are gone; the intent
  comes from blur, Enter, and Escape (U4).
- `data-action="unset-detail-label"` is gone; emptying the label unsets it.
- The URL error keeps `data-error="contact-url"` and `role="alert"` but renders
  as the `Value` field's `FormField` error.
- `data-detail-is-hidden` is on a `button[role="checkbox"]`; tests click it and
  read `aria-checked`.

## Strings held

Everything under "Personal details" in the retained hooks list except
`Remove label`.

## TDD cycle

- [ ] **RED.** In `test/editor/personal-details.test.ts` delete the
      `OptionalField`, `DateRangeField`, and `YearMonthField` describe blocks
      (T09 owns their replacements) and rewrite the panel and contact cases:

  ```ts
  function panel(personal: PersonalDetails = {}) {
    const edit = vi.fn(() => ({ kind: "enqueued" as const }));
    const actions = {
      edit,
      createEntityId: () => "id-1",
      record: computed(() => undefined),
    } as unknown as ResumeEditorActions;
    return {
      edit,
      wrapper: mount(PersonalDetailsPanel, { props: { actions, personal } }),
    };
  }

  describe("PersonalDetailsPanel", () => {
    it("sets a typed name on blur and unsets an emptied one", async () => {
      const { edit, wrapper } = panel({ fullName: "Ada" });
      const name = wrapper.get('[data-field="fullName"] [data-field-input]');
      await name.setValue("Ada Lovelace");
      await name.trigger("blur");
      expect(edit).toHaveBeenLastCalledWith({
        kind: "personalField",
        path: "fullName",
        value: { present: true, value: "Ada Lovelace" },
      });
      await wrapper.setProps({ personal: { fullName: "Ada Lovelace" } });
      await name.setValue("");
      await name.trigger("blur");
      expect(edit).toHaveBeenLastCalledWith({
        kind: "personalField",
        path: "fullName",
        value: { present: false },
      });
    });

    it("never emits a clear intent", async () => {
      const { edit, wrapper } = panel({ headline: "Engineer" });
      const headline = wrapper.get(
        '[data-field="headline"] [data-field-input]',
      );
      await headline.setValue("");
      await headline.trigger("keydown", { key: "Enter" });
      expect(
        edit.mock.calls.every(
          ([command]) =>
            !("value" in command) ||
            command.value.present === false ||
            command.value.value !== "",
        ),
      ).toBe(true);
    });
  });

  describe("ContactList", () => {
    it("unsets a label by emptying it and toggles hidden by role", async () => {
      const wrapper = mount(ContactList, {
        props: {
          createEntityId: () => "c-2",
          details: [
            {
              id: "c-1",
              type: "email",
              value: "a@b.c",
              label: "Work",
              isHidden: false,
            },
          ],
        },
      });
      const label = wrapper.get("[data-detail-label]");
      await label.setValue("");
      await label.trigger("blur");
      expect(wrapper.emitted("change")?.at(-1)?.[0]).toEqual([
        { id: "c-1", type: "email", value: "a@b.c", isHidden: false },
      ]);
      const hidden = wrapper.get("[data-detail-is-hidden]");
      expect(hidden.attributes("role")).toBe("checkbox");
      await hidden.trigger("click");
      expect(wrapper.emitted("change")?.at(-1)?.[0]).toEqual([
        { id: "c-1", type: "email", value: "a@b.c", isHidden: true },
      ]);
    });

    it("rejects a non-https value for a web profile", async () => {
      const wrapper = mount(ContactList, {
        props: {
          createEntityId: () => "c-2",
          details: [{ id: "c-1", type: "website", value: "", isHidden: false }],
        },
      });
      const value = wrapper.get("[data-detail-value]");
      await value.setValue("http://example.com");
      await value.trigger("blur");
      expect(wrapper.emitted("change")).toBeUndefined();
      expect(wrapper.get('[data-error="contact-url"]').attributes("role")).toBe(
        "alert",
      );
    });
  });
  ```

  Keep the existing move, remove, add, limit, and type-change cases, replacing
  `select`/`input` tag selectors with the `data-detail-*` hooks and
  `input[type="checkbox"]` with `[data-detail-is-hidden]` clicks.

- [ ] Run and watch the file fail:

  ```sh
  cd apps/web && npx vitest run test/editor/personal-details.test.ts
  ```

- [ ] **PersonalDetailsPanel.vue template:**

  ```vue
  <template>
    <InspectorPanel
      ref="panel"
      title="Personal details"
      title-id="personal-details-title"
    >
      <TextField
        :error="issueFor('fullName')"
        label="Full name"
        :model-value="personal.fullName"
        name="fullName"
        @intent="editText('fullName', $event)"
      />
      <TextField
        :error="issueFor('headline')"
        label="Headline"
        :model-value="personal.headline"
        name="headline"
        @intent="editText('headline', $event)"
      />
      <ContactList
        :create-entity-id="actions.createEntityId"
        :details="personal.details"
        @change="editDetails"
        @unset="unsetDetails"
      />
      <ul
        v-if="contactIssues.length > 0 || unmappedIssues.length > 0"
        class="grid gap-1"
      >
        <li v-for="issue in contactIssues" :key="`${issue.path}:${issue.code}`">
          <Button
            :data-issue="issue.path"
            size="sm"
            variant="link"
            @click="focusField(issue.path)"
          >
            {{ messageForCode(issue.code) }}
          </Button>
        </li>
        <li
          v-for="issue in unmappedIssues"
          :key="`${issue.path}:${issue.code}`"
          class="text-sm text-destructive"
        >
          {{ messageForCode(issue.code) }}
        </li>
      </ul>
    </InspectorPanel>
  </template>
  ```

  `focusField` selectors become `[data-field="${field}"] [data-field-input]` and
  `[data-detail-index="${index}"] [data-detail-${field}]`. The `panel` ref reads
  the component's `$el`.

- [ ] **ContactList.vue template** per the contract; the `Type` options are the
      eight existing `{ value, label }` pairs. Delete the `unsetLabel` button
      but keep the function (the `Label` field's `unset` intent calls it).

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/personal-details.test.ts
  make -C ../.. web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T08 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs. Suggested commit:
`feat(editor): rebuild personal details on the shared fields`.
