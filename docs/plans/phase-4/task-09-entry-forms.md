# Task 09: All eight entry forms

**Owner:** One high-judgment web author.

**Authorities:** `editor-contract.md` Draft field behavior, generated current-v2
entry unions, Task 07 rich text, Task 08 shared fields, and D5/D12/D17.

**Acceptance:** AC-EDITOR-008 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/components/editor/forms/SectionPanel.vue`
- Create: `apps/web/app/components/editor/forms/entries/ProfileEntryFields.vue`
- Create: `apps/web/app/components/editor/forms/entries/WorkEntryFields.vue`
- Create:
  `apps/web/app/components/editor/forms/entries/EducationEntryFields.vue`
- Create: `apps/web/app/components/editor/forms/entries/SkillEntryFields.vue`
- Create: `apps/web/app/components/editor/forms/entries/LanguageEntryFields.vue`
- Create:
  `apps/web/app/components/editor/forms/entries/CertificateEntryFields.vue`
- Create: `apps/web/app/components/editor/forms/entries/ProjectEntryFields.vue`
- Create: `apps/web/app/components/editor/forms/entries/CustomEntryFields.vue`
- Create: `apps/web/test/editor/entry-forms.test.ts`

**Interfaces:** `SectionPanel` switches exhaustively on `section.sectionType`.
Every entry component accepts its generated entry type and emits Task 08
`FieldIntent`; the panel sends Task 01 intents through Task 05
`ResumeEditorActions.edit`. No component calls transport, structure, template,
or renderer modules.

- [ ] **Step 1: Write the all-eight shape RED test**

Test these exact fields beyond `id`/optional `isHidden`:

| Type        | Fields                                                                                 |
| ----------- | -------------------------------------------------------------------------------------- |
| profile     | rich `text`                                                                            |
| work        | `jobTitle`, `employer`, `employerLink`, `city`, `country`, `dates`, rich `description` |
| education   | `degree`, `school`, `schoolLink`, `city`, `country`, `dates`, rich `description`       |
| skill       | `name`, optional level 0–5, rich `infoHtml`                                            |
| language    | `name`, optional level 0–5                                                             |
| certificate | `title`, `titleLink`, `issuer`, one `date`, rich `description`                         |
| project     | `title`, `link`, `dates`, rich `description`                                           |
| custom      | `title`, `titleLink`, `subtitle`, `city`, `dates`, rich `description`                  |

Derive the expected property keys from generated fixtures/types and prove each
form has every key and no foreign key.

```ts
const actionsSpy = () =>
  ({ edit: vi.fn() }) as unknown as ResumeEditorActions & {
    edit: ReturnType<typeof vi.fn>;
  };
const sectionFixture = (sectionType: Section["sectionType"]): Section =>
  ({
    key: `${sectionType}-section`,
    sectionType,
    entries: [{ id: "entry-1" }],
  }) as Section;
const cases = [
  ["profile", ["text"]],
  [
    "work",
    [
      "jobTitle",
      "employer",
      "employerLink",
      "city",
      "country",
      "dates",
      "description",
    ],
  ],
  [
    "education",
    [
      "degree",
      "school",
      "schoolLink",
      "city",
      "country",
      "dates",
      "description",
    ],
  ],
  ["skill", ["name", "level", "infoHtml"]],
  ["language", ["name", "level"]],
  ["certificate", ["title", "titleLink", "issuer", "date", "description"]],
  ["project", ["title", "link", "dates", "description"]],
  [
    "custom",
    ["title", "titleLink", "subtitle", "city", "dates", "description"],
  ],
] as const;
it.each(cases)("renders only %s fields", (sectionType, fields) => {
  const wrapper = mount(SectionPanel, {
    props: { section: sectionFixture(sectionType), actions: actionsSpy() },
  });
  expect(
    wrapper
      .findAll("[data-entry-field]")
      .map((node) => node.attributes("data-entry-field")),
  ).toEqual(fields);
});
```

- [ ] **Step 2: Run the shape test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/entry-forms.test.ts)
```

Expected RED: FAIL because dispatcher/forms do not exist.

- [ ] **Step 3: Implement the minimal exhaustive dispatcher**

Use one exhaustive switch returning the typed component. Links use their exact
schema schemes; dates reuse Task 08; rich fields reuse Task 07; levels preserve
zero. Entry/section IDs are read-only text.

```ts
switch (props.section.sectionType) {
  case "profile":
    return ProfileEntryFields;
  case "work":
    return WorkEntryFields;
  case "education":
    return EducationEntryFields;
  case "skill":
    return SkillEntryFields;
  case "language":
    return LanguageEntryFields;
  case "certificate":
    return CertificateEntryFields;
  case "project":
    return ProjectEntryFields;
  case "custom":
    return CustomEntryFields;
  default:
    return assertNever(props.section);
}
```

- [ ] **Step 4: Rerun the shape test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the lifecycle/adversarial RED test**

For every type, test add with one injected UUID, field set/clear/unset, hidden
toggle, confirmed delete, accepted sanitization replacement, mapped/unmapped
server issue, draft-incomplete save, and no publish completeness call. Prove a
whole-entry request uses accepted siblings plus only the head intent.

```ts
it.each(cases)(
  "captures %s lifecycle intents without publish validation",
  async (sectionType) => {
    const actions = actionsSpy();
    const wrapper = mount(SectionPanel, {
      props: { section: sectionFixture(sectionType), actions },
    });
    await wrapper.get('[data-action="add-entry"]').trigger("click");
    await wrapper.get("[data-entry-field]").setValue("");
    await wrapper.get('[data-action="toggle-hidden"]').trigger("click");
    await wrapper.get('[data-action="delete-entry"]').trigger("click");
    expect(actions.edit.mock.calls.map(([intent]) => intent.kind)).toEqual([
      "entryUpsert",
      "entryField",
      "entryField",
      "entryDelete",
    ]);
    expect("publish" in actions).toBe(false);
  },
);
```

- [ ] **Step 6: Run the lifecycle test RED**

Run the Step 2 command. Expected RED: FAIL on the first missing lifecycle case.

- [ ] **Step 7: Implement minimal lifecycle capture**

Add emits `entryUpsert`, field change emits `entryField`, and delete emits
`entryDelete`. Generate IDs before capture. Materialize preview with Task 01
reducer; leave payload serialization to Task 03.

```ts
const add = () =>
  props.actions.edit({
    kind: "entryUpsert",
    sectionKey: props.section.key,
    entry: createEmptyEntry(props.section.sectionType, runtime.uuid()),
  });
const edit = (entryId: string, path: EntryFieldPath, value: Presence) =>
  props.actions.edit({
    kind: "entryField",
    sectionKey: props.section.key,
    entryId,
    path,
    value,
  });
const remove = (entryId: string) =>
  props.actions.edit({
    kind: "entryDelete",
    sectionKey: props.section.key,
    entryId,
  });
```

- [ ] **Step 8: Rerun the lifecycle test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/entry-forms.test.ts)
(cd apps/web && npx eslint app/components/editor/forms/SectionPanel.vue \
  app/components/editor/forms/entries test/editor/entry-forms.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add all resume entry forms`.
