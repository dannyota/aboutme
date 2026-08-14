# Task 11: Customization controls

**Owner:** One high-judgment web author.

**Authorities:** `editor-contract.md` Customization controls, current-v2 schema,
current customization OpenAPI allowlist, and D12/D17/D20.

**Acceptance:** AC-EDITOR-010 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/components/editor/customization/fields.ts`
- Create: `apps/web/app/components/editor/customization/CustomizationPanel.vue`
- Create: `apps/web/app/components/editor/customization/ColorField.vue`
- Create:
  `apps/web/app/components/editor/customization/OptionalCustomizationField.vue`
- Create: `apps/web/test/editor/customization-controls.test.ts`

**Interfaces:**

```ts
export interface CustomizationField {
  readonly path: CustomizationSetPath;
  readonly kind: "enum" | "integer" | "number" | "boolean" | "color";
  readonly required: boolean;
  readonly values?: readonly (string | number)[];
  readonly minimum?: number;
  readonly maximum?: number;
}
export const CUSTOMIZATION_FIELDS: readonly CustomizationField[];
```

Panels send only Task 01 `CustomizationDelta[]` through
`ResumeEditorActions.edit`.

- [ ] **Step 1: Write the schema-coverage RED test**

Resolve `properties.customization.$ref` to `$defs.customization`, walk object
properties, exclude `layout.sections`, and compare the leaf set exactly to:

```ts
const paths = [
  "font.family",
  "font.baseSizePx",
  "colors.primary",
  "colors.text",
  "colors.background",
  "colors.accent",
  "colors.surface",
  "spacing.sectionGap",
  "spacing.entryGap",
  "spacing.lineHeight",
  "spacing.pageMargin.x",
  "spacing.pageMargin.y",
  "heading.style",
  "heading.showRule",
  "header.align",
  "header.detailsLayout",
  "header.iconStyle",
  "layout.columns",
  "layout.surfaceTarget",
  "sectionDisplay.skill.style",
  "sectionDisplay.language.style",
  "pageFormat",
  "dateFormat",
] as const;
```

Require enum/numeric metadata to match schema and no path to equal or prefix
`layout.sections`.

```ts
it("equals the generated customization leaf set", () => {
  expect(CUSTOMIZATION_FIELDS.map(({ path }) => path)).toEqual(paths);
  expect(
    CUSTOMIZATION_FIELDS.some(({ path }) => path.startsWith("layout.sections")),
  ).toBe(false);
  expect(
    CUSTOMIZATION_FIELDS.find(({ path }) => path === "font.baseSizePx"),
  ).toMatchObject({
    kind: "integer",
    minimum: 8,
    maximum: 12,
  });
});
```

- [ ] **Step 2: Run the schema-coverage test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/customization-controls.test.ts)
```

Expected RED: FAIL because field metadata does not exist.

- [ ] **Step 3: Implement minimal schema metadata extraction**

At module initialization, resolve local `$ref` entries, infer primitive kind,
copy enum/min/max, and throw a stable error when a listed path is absent. Do not
invent bounds.

```ts
export const CUSTOMIZATION_FIELDS = paths.map((path) => {
  const leaf = resolveCustomizationLeaf(currentSchema, path);
  if (!leaf) throw new Error(`missing customization schema path: ${path}`);
  return Object.freeze({ path, ...primitiveMetadata(leaf) });
});
```

- [ ] **Step 4: Rerun the schema-coverage test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the control/delta RED test**

Required leaves emit `set`. Accent, surface, and surfaceTarget emit direct
set/unset. Absent pageMargin displays 15/15 and absent header displays
left/inline/outline without a command. Explicit Enable emits all required child
sets in one command; Unset emits only parent `spacing.pageMargin` or `header`.
Column count preserves placement. Colors remain visible text inputs. Prove
keyboard/focus/error behavior.

```ts
it.each([
  ["accent", [{ op: "unset", path: "colors.accent" }]],
  [
    "enable-page-margin",
    [
      { op: "set", path: "spacing.pageMargin.x", value: 15 },
      { op: "set", path: "spacing.pageMargin.y", value: 15 },
    ],
  ],
  ["unset-header", [{ op: "unset", path: "header" }]],
] as const)("emits the exact %s delta", async (action, deltas) => {
  const edit = vi.fn();
  const wrapper = mount(CustomizationPanel, {
    props: { record, actions: { edit } },
  });
  await wrapper.get(`[data-action="${action}"]`).trigger("click");
  expect(edit).toHaveBeenCalledWith({ kind: "customization", deltas });
});
```

- [ ] **Step 6: Run the control/delta test RED**

Run the Step 2 command. Expected RED: FAIL because controls are absent.

- [ ] **Step 7: Implement minimal delta capture**

Convert one user action to one ordered delta array, capture it against current,
then enqueue. Never write renderer fallback on mount. Reject local invalid input
without changing preview; server rejection remains authoritative.

```ts
const commit = (deltas: readonly CustomizationDelta[]) =>
  props.actions.edit({ kind: "customization", deltas });
```

- [ ] **Step 8: Rerun the control/delta test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/customization-controls.test.ts)
(cd apps/web && npx eslint app/components/editor/customization \
  test/editor/customization-controls.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add customization controls`.
