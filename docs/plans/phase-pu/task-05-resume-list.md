# Task 05 — Resume list and dialogs

**Acceptance:** AC-UI-002, AC-UI-005, AC-UI-006.

**Depends on:** T03.

**Owned paths:** T05 paths in `file-structure.md`.

## Contract

- `pages/app/resumes/index.vue` renders `PageHeader` (`Resumes`, description
  `Up to three private resumes. Publishing is a separate step.`) with the
  `Create resume` button (`data-testid="create-resume"`) in its `actions` slot,
  then one of `LoadingState`, `StatusBanner kind="error"`, `EmptyState`, or
  `ResumeList`.
- `ResumeList.vue` is a `Table` with columns `Title`, `Updated`, and a
  right-aligned actions cell. Each row keeps `data-testid="resume-row-{id}"`;
  the title is a `NuxtLink` to the editor; the actions cell holds two
  `Button variant="ghost" size="sm"` buttons `Rename` and `Delete` with the
  existing `aria-label`s. The removal-focus behavior (focus the next row's first
  button, else `Create resume`) is kept.
- `CreateResumeDialog.vue` and `RenameResumeDialog.vue` render through
  `FormDialog`; `DeleteResumeDialog.vue` renders through `ConfirmDialog` with
  `destructive`, `confirmText` = the item title, `confirmInputLabel`
  `Current title`, `confirmLabel` `Delete`. The create dialog's language choice
  is a `RadioGroup` with the three existing options and the conditional
  `Language value` field; the opaque-create branch keeps `Refresh list` and
  `Abandon`.
- The `useResumeList` composable and the page's script are unchanged.

## Hook changes

- `Enter the current title to enable deletion` status text is replaced by the
  disabled confirm button; no test asserts the old text.
- The loading and empty texts stay `Loading resumes.` and `No resumes yet.`.

## Strings held

`Create resume`, `Rename`, `Delete`, `Create`, `Save`, `Cancel`, `Refresh list`,
`Abandon`, `Rename resume`, `Delete resume`, `Title`, `Current title`,
`Language`, `Leave unchanged`, `Clear language`, `Set language`,
`Language value`, `No resumes yet.`,
`We could not confirm whether this resume was created.`,
`Resumes are unavailable. Try again.`, `Checking your session.`,
`Loading resumes.`

## TDD cycle

- [ ] **RED.** In `test/editor/resume-list.test.ts` rewrite the dialog cases to
      the body-query form and add the table case:

  ```ts
  it("renders rows in a table with accessible actions", () => {
    const wrapper = mount(ResumeList, {
      props: {
        items: [
          summary({
            id: "r1",
            title: "First",
            updatedAt: "2026-01-01T00:00:00Z",
          }),
        ],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
    });
    const row = wrapper.get('[data-testid="resume-row-r1"]');
    expect(row.element.tagName).toBe("TR");
    expect(row.get("a").attributes("href")).toBe("/app/resumes/r1");
    expect(row.get('[aria-label="Rename First"]').text()).toBe("Rename");
    expect(row.get('[aria-label="Delete First"]').text()).toBe("Delete");
  });

  it("gates deletion on the exact current title", async () => {
    const wrapper = mount(DeleteResumeDialog, {
      attachTo: document.body,
      props: { item: { id: "r1", title: "First" }, busy: false },
    });
    await nextTick();
    const confirm = document.body.querySelector<HTMLButtonElement>(
      '[data-action="confirm-delete"]',
    )!;
    expect(confirm.disabled).toBe(true);
    const input = document.body.querySelector<HTMLInputElement>(
      '[role="alertdialog"] input',
    )!;
    expect(
      document.body.querySelector(`label[for="${input.id}"]`)!.textContent,
    ).toContain("Current title");
    input.value = "First";
    input.dispatchEvent(new Event("input"));
    await nextTick();
    expect(confirm.disabled).toBe(false);
    confirm.click();
    expect(wrapper.emitted("submit")).toEqual([["r1", "First"]]);
    wrapper.unmount();
  });
  ```

  `summary()` is the file's existing `ResumeSummary` fixture builder (it extends
  `ResumeMetadata`, so `updatedAt` exists). Keep every other case, replacing
  `wrapper.get('[role="dialog"] ...')` with `document.body.querySelector(...)`
  and `mount(..., { attachTo: document.body })`. Keep the focus-return cases.

- [ ] Run and watch the file fail:

  ```sh
  cd apps/web && npx vitest run test/editor/resume-list.test.ts
  ```

- [ ] **ResumeList.vue** template:

  ```vue
  <template>
    <section ref="root" aria-labelledby="page-title">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Title</TableHead>
            <TableHead class="w-40">Updated</TableHead>
            <TableHead class="w-40 text-right">
              <span class="sr-only">Actions</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody aria-label="Your resumes">
          <TableRow
            v-for="item in items"
            :key="item.id"
            :data-testid="`resume-row-${item.id}`"
          >
            <TableCell class="font-medium">
              <NuxtLink
                class="hover:underline"
                :to="`/app/resumes/${encodeURIComponent(item.id)}`"
              >
                {{ item.title }}
              </NuxtLink>
            </TableCell>
            <TableCell class="text-muted-foreground">
              <time :datetime="item.updatedAt">{{
                formatUpdated(item.updatedAt)
              }}</time>
            </TableCell>
            <TableCell class="text-right">
              <Button
                :aria-label="`Rename ${item.title}`"
                :disabled="busyIds.includes(item.id)"
                size="sm"
                variant="ghost"
                @click="$emit('rename', item)"
              >
                Rename
              </Button>
              <Button
                :aria-label="`Delete ${item.title}`"
                class="text-destructive"
                :disabled="busyIds.includes(item.id)"
                size="sm"
                variant="ghost"
                @click="$emit('remove', item)"
              >
                Delete
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </section>
  </template>
  ```

  `formatUpdated` uses
  `new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeZone: 'UTC' })`
  (this is chrome, not renderer, so `Intl` is allowed). If `ResumeSummary` has
  no `updatedAt`, drop the column and its test line and record it. The
  removal-focus selector becomes `[data-testid="resume-row-${id}"] button`.

- [ ] **Page template:** `PageHeader` with the create `Button` in `#actions`
      (`data-testid="create-resume"`), then the state branches; the empty state
      is
      `EmptyState title="No resumes yet." description="Create your first resume to start editing."`
      with a second `Create resume` button in its `action` slot carrying no
      testid. The status paragraph at the bottom becomes
      `StatusBanner kind="info"` with `role="status"`.

- [ ] **Dialogs:** `CreateResumeDialog` → `FormDialog` (`title="Create resume"`,
      `description="Create a new private resume."`, `submitLabel="Create"`,
      `:busy`), body: `FormField` + `Input` for `Title` (`name="title"`,
      `required`), then `RadioGroup` with `Label`s `Leave unchanged`,
      `Clear language`, `Set language`, and the conditional `Language value`
      field; the opaque branch replaces the body with the alert text and uses
      the `footer` slot for `Refresh list` and `Abandon`. `RenameResumeDialog` →
      `FormDialog` (`title="Rename resume"`, `submitLabel="Save"`).
      `DeleteResumeDialog` → `ConfirmDialog` as in the contract with
      `confirmAction="confirm-delete"` and `cancelAction="cancel-delete"`.

- [ ] Delete the `.resume-list*` and `.app-page [role='dialog']` rules from
      `app/assets/css/app.css`.

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/resume-list.test.ts
  make web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T05 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs. Suggested commit:
`feat(web): rebuild the resume list on the shared components`.
