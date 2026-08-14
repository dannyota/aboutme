# Task 10: Section and entry structure controls

**Owner:** One high-judgment web author.

**Authorities:** `editor-contract.md` Section and entry structure,
`mutation-contract.md` structure target/context rows, ADR 0009, and D5/D12/D17.

**Acceptance:** AC-EDITOR-009 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/components/editor/structure/StructurePanel.vue`
- Create: `apps/web/app/components/editor/structure/SectionControls.vue`
- Create: `apps/web/app/components/editor/structure/EntryOrderControls.vue`
- Create: `apps/web/test/editor/structure-controls.test.ts`

**Interfaces:** Panels read Task 05 `ResumeEditorActions.record` and send Task
01 `structure`, `sectionMetadata`, or `entryReorder` intents through `edit`.
They never emit customization deltas or call transport.

- [ ] **Step 1: Write the placement RED test**

Prove create receives one injected key and emits type/column/index plus optional
name/icon; delete confirms current section; move up/down/main/sidebar uses
remove-then-insert indices; reorder emits the complete column permutation; entry
order emits `entryReorder`; changed identity/context reopens against latest.
Every action must be a visible-focus button and keyboard operable.

```ts
it("captures a remove-then-insert move through edit", async () => {
  const edit = vi.fn();
  const wrapper = mount(StructurePanel, {
    props: { record, actions: { edit } },
  });
  await wrapper
    .get('[data-section="work"] [data-action="move-sidebar"]')
    .trigger("click");
  expect(edit).toHaveBeenCalledWith({
    kind: "structure",
    commands: [{ op: "moveSection", key: "work", column: "sidebar", index: 1 }],
  });
});
```

- [ ] **Step 2: Run the placement test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/structure-controls.test.ts)
```

Expected RED: FAIL because structure components do not exist.

- [ ] **Step 3: Implement minimal placement transitions**

Read order only from `current.document.customization.layout.sections`. Calculate
a move by removing the source key first, then clamp only against the resulting
target length:

```ts
const without = source.filter((key) => key !== movedKey);
const boundedIndex = Math.min(requestedIndex, targetAfterRemoval.length);
actions.edit({
  kind: "structure",
  commands: [{ op: "moveSection", key: movedKey, column, index: boundedIndex }],
});
```

Do not add drag-and-drop. Boundary buttons alone are complete v1 behavior.

- [ ] **Step 4: Rerun the placement test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the endpoint/identity RED test**

Assert section create/delete/move/reorder use only `updateResumeStructure`;
displayName/icon/entry order use only `updateResumeSection`; column count is
absent. Cover custom UUID key, built-in key, duplicate placement,
missing/retyped section, changed entry membership, destructive reconfirmation,
and issue focus.

```ts
it.each([
  ["create", "structure"],
  ["delete", "structure"],
  ["move", "structure"],
  ["displayName", "sectionMetadata"],
  ["iconKey", "sectionMetadata"],
  ["entryOrder", "entryReorder"],
] as const)("maps %s only to %s", async (action, kind) => {
  const edit = vi.fn();
  const wrapper = mount(StructurePanel, {
    props: { record, actions: { edit } },
  });
  await wrapper.get(`[data-action="${action}"]`).trigger("click");
  expect(edit).toHaveBeenLastCalledWith(expect.objectContaining({ kind }));
  expect(edit).not.toHaveBeenCalledWith(
    expect.objectContaining({ kind: "customization" }),
  );
});
```

- [ ] **Step 6: Run the endpoint/identity test RED**

Run the Step 2 command. Expected RED: FAIL on the first missing boundary.

- [ ] **Step 7: Implement minimal identity guards**

Before enqueue, project exact key/type or member set from current. Disable only
impossible moves. On reconciliation conflict, show Reopen placement/order and
recapture from the latest complete state; never generic Apply mine.

```ts
const reopen = async (conflictId: string) => {
  await actions.acceptLatest(conflictId);
  focusSection(conflictSectionKey(conflictId));
};
```

- [ ] **Step 8: Rerun the endpoint/identity test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/structure-controls.test.ts)
(cd apps/web && npx eslint app/components/editor/structure \
  test/editor/structure-controls.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add structure controls`.
