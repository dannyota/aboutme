# Task 07: Closed ProseMirror rich text

**Owner:** One high-judgment web author.

**Authorities:** `editor-contract.md` Rich-text contract, `design.md` Security,
ADR 0005, sanitizer allowlist v1, and D13/D17.

**Acceptance:** AC-EDITOR-011 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/components/editor/richtext/schema.ts`
- Create: `apps/web/app/components/editor/richtext/serialize.ts`
- Create: `apps/web/app/components/editor/richtext/RichTextEditor.vue`
- Create: `apps/web/test/editor/rich-text.test.ts`

**Interfaces:**

```ts
export const richTextSchema: Schema;
export function parseRichTextHTML(html: string): PMNode;
export function serializeRichText(node: PMNode): string;
```

`RichTextEditor` accepts `modelValue: string` and emits `update:modelValue` only
for a distinct sanitized serialization. It imports no store or transport.

- [ ] **Step 1: Write the closed-schema/parser RED test**

Require exactly doc, paragraph, text, hard_break, ordered_list, bullet_list, and
list_item nodes; strong, em, underline, link marks; and only href/rel/target
link attributes. Reject headings, image, table, media, style, class, and
arbitrary attributes. Empty serializes as `""`.

```ts
it("exports only the sanitizer-v1 vocabulary", () => {
  expect(Object.keys(richTextSchema.nodes)).toEqual([
    "doc",
    "paragraph",
    "text",
    "hard_break",
    "ordered_list",
    "bullet_list",
    "list_item",
  ]);
  expect(Object.keys(richTextSchema.marks)).toEqual([
    "strong",
    "em",
    "underline",
    "link",
  ]);
  expect(
    serializeRichText(parseRichTextHTML("<h1>x</h1><img src=x><p></p>")),
  ).toBe("");
});
```

- [ ] **Step 2: Run the schema/parser test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/rich-text.test.ts)
```

Expected RED: FAIL because schema/parser/serializer are absent.

- [ ] **Step 3: Implement the minimal schema/parser/serializer**

Build the schema from `ALLOWED_TAGS`, `ALLOWED_ATTRIBUTES`,
`ALLOWED_URL_SCHEMES`, and `EXTERNAL_REL`. Parse only this order:

```ts
const sanitized = sanitizeRichText(html);
const fragment = new DOMParser().parseFromString(sanitized, "text/html");
return PMDOMParser.fromSchema(richTextSchema).parse(fragment.body);
```

Serializer rewrites external link attributes to exact
`rel="noopener noreferrer"`, preserves optional `_blank`, emits `<br>`, and
normalizes one empty paragraph to `""`.

- [ ] **Step 4: Rerun the schema/parser test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the hostile-input/interaction RED test**

Iterate the committed hostile corpus. Admit exact lowercase `https:`, `mailto:`,
and `tel:` only. Reject relative, protocol-relative, mixed/control schemes,
javascript/data, unsupported nodes, file/image paste, and file/image drop while
retaining plain-text paste. Exercise every toolbar action by button and
keyboard. Focus/blur on absent empty content emits nothing; clearing present
content emits `""`; accepted server text updates without a loop.

```ts
it.each([
  ["<a href='javascript:alert(1)'>x</a>", "x"],
  ["<img src='data:image/png;base64,AA'>", ""],
  ["<p style='color:red'>ok</p>", "<p>ok</p>"],
])("sanitizes hostile input before parsing", (input, output) => {
  expect(serializeRichText(parseRichTextHTML(input))).toBe(output);
});
it("blocks file paste", () => {
  const wrapper = mount(RichTextEditor, { props: { modelValue: "" } });
  const transfer = new DataTransfer();
  transfer.items.add(new File(["x"], "x.png", { type: "image/png" }));
  const event = new ClipboardEvent("paste", {
    clipboardData: transfer,
    cancelable: true,
  });
  wrapper.get('[contenteditable="true"]').element.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  expect(wrapper.emitted("update:modelValue")).toBeUndefined();
});
```

- [ ] **Step 6: Run the hostile-input test RED**

Run the Step 2 command. Expected RED: FAIL at the first missing editor case.

- [ ] **Step 7: Implement minimal editor transaction rules**

On paste, sanitize HTML before `parseSlice`; on drop/paste with any file, call
`preventDefault` and insert nothing. Serialize after each document-changing
transaction, compare to the last input, and emit once. Reconfigure state from a
changed accepted prop without dispatching a transaction.

```ts
handlePaste(view, event) {
  if ([...(event.clipboardData?.files ?? [])].length > 0) {
    event.preventDefault();
    return true;
  }
  const html = event.clipboardData?.getData("text/html");
  if (!html) return false;
  event.preventDefault();
  view.dispatch(view.state.tr.replaceSelection(parseRichTextHTML(html).slice(0)));
  return true;
}
```

- [ ] **Step 8: Rerun the hostile-input test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/rich-text.test.ts)
(cd apps/web && npx eslint app/components/editor/richtext \
  test/editor/rich-text.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add closed rich text editing`.
