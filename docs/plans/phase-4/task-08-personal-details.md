# Task 08: Personal details and shared draft fields

**Owner:** One web author.

**Authorities:** `editor-contract.md` Draft field behavior, generated current-v2
types, current personal-details OpenAPI operation, and D5/D12/D17.

**Acceptance:** AC-EDITOR-008 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/components/editor/forms/OptionalField.vue`
- Create: `apps/web/app/components/editor/forms/YearMonthField.vue`
- Create: `apps/web/app/components/editor/forms/DateRangeField.vue`
- Create: `apps/web/app/components/editor/forms/PersonalDetailsPanel.vue`
- Create: `apps/web/app/components/editor/forms/ContactList.vue`
- Create: `apps/web/app/components/editor/forms/fieldIntent.ts`
- Create: `apps/web/test/editor/personal-details.test.ts`

**Interfaces:** `fieldIntent.ts` is the only definition site for shared form
intent:

```ts
export type FieldIntent<T> =
  | { readonly kind: "set"; readonly value: T }
  | { readonly kind: "clear"; readonly value: "" }
  | { readonly kind: "unset" };
```

Controls emit intent, not API payloads. `PersonalDetailsPanel` converts intent
to Task 01 `AtomicCommandIntent` and calls Task 05 `ResumeEditorActions.edit`.
New contact IDs come only from `ResumeEditorActions.createEntityId()`, which
delegates to the injected editor runtime. Forms do not receive a runtime or UUID
callback.

- [x] **Step 1: Write the shared field/date RED test**

```ts
it.each([
  ["untouched", undefined],
  ["set", { kind: "set", value: "Ada" }],
  ["clear", { kind: "clear", value: "" }],
  ["unset", { kind: "unset" }],
] as const)("emits the %s transition exactly", async (action, expected) => {
  const wrapper = mount(OptionalField, { props: { modelValue: undefined } });
  if (action !== "untouched")
    await wrapper.get(`[data-action="${action}"]`).trigger("click");
  await wrapper.get("input").trigger("blur");
  expect(wrapper.emitted("intent")?.at(-1)?.[0]).toEqual(expected);
});
```

Add exact `YearMonth` and `DateRange` rows, including zero-preserving month,
presence, null end, and local start-after-end display.

- [x] **Step 2: Run the shared-field test RED**

```sh
(cd apps/web && npx vitest run test/editor/personal-details.test.ts)
```

Expected RED: FAIL because shared fields do not exist.

- [x] **Step 3: Implement minimal shared field transitions**

```ts
const dirty = ref(false);
const choose = (intent: FieldIntent<string>) => {
  dirty.value = true;
  pending.value = intent;
};
const commit = () => {
  if (!dirty.value || !pending.value) return;
  emit("intent", pending.value);
  dirty.value = false;
};
```

Clear is rendered only where empty string is schema-valid. Unset removes the
property. Reject non-finite/fractional year/month before capture.

- [x] **Step 4: Rerun the shared-field test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [x] **Step 5: Write the personal/contact RED test**

```ts
it("captures ordered contact edits through the action boundary", async () => {
  const edit = vi.fn();
  const actions = { edit } as unknown as ResumeEditorActions;
  const wrapper = mount(PersonalDetailsPanel, { props: { personal, actions } });
  await wrapper.get('[data-action="add-detail"]').trigger("click");
  await wrapper.get("[data-detail-value]").setValue("https://example.test");
  await wrapper.get('[data-action="move-detail-down"]').trigger("click");
  expect(edit).toHaveBeenCalledWith(
    expect.objectContaining({ kind: "personalField" }),
  );
});
```

Add assertions for fullName/headline, immutable IDs, set/clear/unset, all
contact types, lowercase `https://` for web profiles, issues, and exactly one
`actions.createEntityId()` call.

- [x] **Step 6: Run the personal/contact test RED**

Run the Step 2 command. Expected RED: FAIL because panel capture is absent.

- [x] **Step 7: Implement minimal personal/contact capture**

```ts
const commitDetails = (details: readonly PersonalDetail[]) =>
  props.actions.edit({
    kind: "personalField",
    path: "details",
    value: { present: true, value: details },
  });
```

Construct details from accepted siblings plus the action. IDs render as text,
never inputs.

- [x] **Step 8: Rerun the personal/contact test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [x] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/personal-details.test.ts)
(cd apps/web && npx eslint app/components/editor/forms/{OptionalField,YearMonthField,DateRangeField,PersonalDetailsPanel,ContactList}.vue \
  app/components/editor/forms/fieldIntent.ts \
  test/editor/personal-details.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add personal detail controls`.
