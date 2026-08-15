# Task 12: Template and partial-recovery controls

**Owner:** One web author.

**Authorities:** `editor-contract.md` Template application, all of
`template-group-contract.md`, Task 02 interfaces, ADR 0008, and D14/D17.

**Acceptance:** AC-EDITOR-012 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/components/editor/templates/TemplatePanel.vue`
- Create: `apps/web/app/components/editor/templates/TemplatePartialDialog.vue`
- Create: `apps/web/test/editor/template-panel.test.ts`

**Interfaces:** Panels call Task 05 `ResumeEditorActions.applyTemplate` and Task
02 recovery/undo functions. They do not call `captureTemplateGroup` directly,
issue child requests, or recreate group rules.

- [x] **Step 1: Write the apply/status RED test**

Render every `TEMPLATES` item. Prove No changes, content preservation, immediate
optimistic result, page/date warnings, base size 10 warning, margins below 5 mm,
dirty/saving/final saved labels, and no stored template identity. Saved requires
one complete final response.

```ts
it("delegates one preset and renders only the returned group state", async () => {
  const applyTemplate = vi.fn().mockReturnValue({ kind: "enqueued", group });
  const wrapper = mount(TemplatePanel, {
    props: { actions: { applyTemplate }, state: queued },
  });
  await wrapper.get(`[data-template="${TEMPLATES[0]!.id}"]`).trigger("click");
  expect(applyTemplate).toHaveBeenCalledOnce();
  expect(applyTemplate).toHaveBeenCalledWith(TEMPLATES[0]);
  expect(wrapper.get('[role="status"]').text()).toBe("Saving template");
});
```

- [x] **Step 2: Run the apply/status test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/template-panel.test.ts)
```

Expected RED: FAIL because template components do not exist.

- [x] **Step 3: Implement minimal apply/status binding**

On Apply, call `actions.applyTemplate(preset)` once. Announce `No changes` for
`no-change`; render the captured intended final snapshot for `enqueued`; retain
the form for `blocked`. Status is a direct exhaustive switch over Task 02
`TemplateGroupState`.

```ts
const apply = (preset: Readonly<TemplatePreset>) => {
  const result = props.actions.applyTemplate(preset);
  if (result.kind === "no-change") notice.value = "No changes";
  if (result.kind === "enqueued") preview.value = result.group.intendedFinal;
};
```

- [x] **Step 4: Rerun the apply/status test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [x] **Step 5: Write the partial/recovery/undo RED test**

Partial displays accepted subset and intended final, with exactly Retry
remaining, Restore pre-apply, Keep partial. Undo appears only for the latest
complete group and disappears on placement/customization/content key/type
change, but survives unrelated entry-field edit. Cover keyboard dialog/focus
return and safe text.

```ts
it.each([
  ["retry-remaining", "enqueue"],
  ["restore-pre-apply", "enqueue"],
  ["keep-partial", "keep-partial"],
] as const)("maps %s recovery to %s", async (action, expected) => {
  recoverTemplateGroup.mockReturnValue({
    kind: expected,
    ...(expected === "enqueue" ? { group } : {}),
  });
  const wrapper = mount(TemplatePartialDialog, {
    props: { group, state: partial, latest },
  });
  await wrapper.get(`[data-action="${action}"]`).trigger("click");
  expect(recoverTemplateGroup).toHaveBeenCalledWith(
    group,
    partial,
    latest,
    action,
  );
});
```

- [x] **Step 6: Run the recovery/undo test RED**

Run the Step 2 command. Expected RED: FAIL on the first missing recovery
control.

- [x] **Step 7: Implement minimal recovery binding**

Each button requests a Task 02 transition. Enqueue only an `enqueue` result;
close on `keep-partial`; keep dialog open with safe reason on `unavailable`.
Undo is a newly captured reverse group, not client history.

```ts
const recover = (
  action: "retry-remaining" | "restore-pre-apply" | "keep-partial",
) => {
  const result = recoverTemplateGroup(
    props.group,
    props.state,
    props.latest,
    action,
  );
  if (result.kind === "enqueue") emit("enqueue", result.group);
  if (result.kind === "keep-partial") emit("close");
  if (result.kind === "unavailable") reason.value = result.reason;
};
```

- [x] **Step 8: Rerun the recovery/undo test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [x] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/template-panel.test.ts)
(cd apps/web && npx eslint app/components/editor/templates \
  test/editor/template-panel.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add template recovery controls`.
