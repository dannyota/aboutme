# Task 07 — Editor shell

**Acceptance:** AC-UI-002, AC-UI-004, AC-UI-005, AC-UI-006.

**Depends on:** T02 (preview projection), T03.

**Owned paths:** T07 paths in `file-structure.md`.

## Contract

- `EditorShell.vue` keeps its script (inspector state, outline, save state,
  issue focus) and its four-region grid, now as Tailwind utilities:
  `grid-cols-[4rem_16.5rem_minmax(32rem,1fr)_22rem]` with
  `grid-rows-[4rem_minmax(0,1fr)]` and the `max-[72rem]:` narrow variant that
  collapses to `grid-cols-[4rem_minmax(0,1fr)]`. `data-region`,
  `data-responsive-region`, and `data-narrow-active` stay on the same elements;
  the narrow rule `[data-responsive-region][data-narrow-active="false"]` becomes
  the classes
  `max-[72rem]:data-[narrow-active=false]:invisible max-[72rem]:data-[narrow-active=false]:pointer-events-none`
  so both regions stay mounted.
- Top bar: brand link, the title in `truncate text-sm font-semibold` with
  `data-resume-title`, `SaveStatus`, the two view buttons (`Button` with
  `variant` `default` when pressed else `outline`, `size="sm"`, `aria-pressed`,
  `data-action`), `AccountMenu`, `ThemeToggle`. It remains one row inside the
  fixed `4rem` shell row at the `max-[72rem]` breakpoint; no child creates an
  implicit second row.
- Rail: `IconButton`s with the existing `aria-label`s, `pressed` bound to the
  inspector kind, and `data-action="open-{kind}"` where kind is `document`,
  `structure`, `design`, `templates`, `photo`; the settings link is `NuxtLink`
  with `buttonVariants({ variant: 'ghost', size: 'icon' })` and
  `aria-label="Account settings"`.
- Outline: heading `Resume`, the `+` becomes `IconButton label="Add section"`,
  items are `Button variant="ghost"` full width with `justify-start`,
  `data-outline-key`, `aria-current`, and
  `aria-[current=page]:bg-positive/15 aria-[current=page]:text-foreground`; the
  footer `+ Add section` is `Button variant="outline"`.
- Preview region: a `grid-rows-[auto_minmax(0,1fr)]` grid holds `PreviewToolbar`
  above `EditorPreview`, so the preview consumes the remaining height without
  extending below the shell; `EditorPreview` gains a `zoom: 'fit' | 'full'` prop
  and applies `[zoom:0.84] max-[72rem]:[zoom:0.72]` or `[zoom:1]` on the
  document wrapper; the canvas is `overflow-auto bg-muted p-6` and the document
  wrapper `mx-auto w-fit shadow-md`. The photo notice from T02 moves into the
  toolbar `Badge`; `EditorPreview` keeps only the render-failure notice.
- Inspector: `overflow-auto border-l bg-card p-4` holding `ErrorSummary`,
  `ConflictPanel`, then the active panel. `ErrorSummary` renders
  `StatusBanner kind="error" title="Check these fields"` with the issue buttons
  as `Button variant="link" size="sm"`; `ConflictPanel` renders
  `StatusBanner kind="info" title="Review changes"` with one `article` per
  conflict (`data-conflict`) and its two buttons (`Button size="sm"`,
  `data-action` unchanged).
- `SaveStatus` renders `Badge variant="outline"` with `role="status"`,
  `data-state`, the icon, and the text, `text-positive` for `saved`/`idle`.
- Session loss: `AlertDialog :open="record.sessionLost"` that ignores Escape,
  titled `Sign in to continue editing`, description
  `Your unsaved work is still open in this tab.`, footer:
  `<a href="/login" target="_blank" rel="noopener noreferrer">` styled with
  `buttonVariants({ variant: 'outline' })` (`Open sign-in in another tab`),
  `Button data-action="resume-after-auth"` (`Resume after sign-in`), and
  `Button variant="ghost"` (`Discard and sign in`).
- `InspectorPanel.vue` and `PreviewToolbar.vue` follow the contracts file.
- `pages/app/resumes/[id].vue` renders `LoadingState label="Loading editor"` and
  the two error states through `EmptyState` with a `Button` (`Try again`) or a
  `NuxtLink` (`Back to resumes`).

## Hook changes

- Rail buttons gain `data-action="open-*"`; their `aria-label`s and
  `aria-pressed` are unchanged.
- `Loading editor…` becomes the `LoadingState` label `Loading editor`
  (`role="status"` retained).
- Class hooks `.editor-account-actions`, `.account-control`, `.theme-toggle` in
  `editor-shell.test.ts` become `[data-testid="account-menu"]` and
  `button[aria-label^="Switch to"]`.

## Strings held

Everything under "Editor shell" in the retained hooks list, the eight
`SaveStatus` texts, every `ConflictPanel` label, `Check these fields`,
`Review changes`, `Accept latest`,
`Preview is temporarily unavailable. Your edits are still safe.`,
`Resume unavailable`, `This resume is not available.`, `Editor unavailable`,
`We could not open this resume. Try again.`, `Try again`, `Back to resumes`.

## TDD cycle

- [ ] **RED.** In `test/editor/editor-shell.test.ts` replace the three class
      selectors as listed and add:

  ```ts
  it("renders the rail as pressed icon buttons with tooltips", async () => {
    const wrapper = mountShell();
    const design = wrapper.get(
      '[data-region="app-rail"] [aria-label="Design"]',
    );
    expect(design.attributes("aria-pressed")).toBe("false");
    await design.trigger("click");
    expect(design.attributes("aria-pressed")).toBe("true");
    expect(wrapper.get("#customization-title").text()).toBe("Customization");
  });

  it("keeps the session-lost dialog open on Escape", async () => {
    const wrapper = mountShell(
      { sessionLost: true },
      { attachTo: document.body },
    );
    await nextTick();
    const dialog = document.body.querySelector('[role="alertdialog"]')!;
    expect(dialog.textContent).toContain("Sign in to continue editing");
    dialog.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    await nextTick();
    expect(document.body.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(
      document.body.querySelector('[data-action="resume-after-auth"]'),
    ).not.toBeNull();
    wrapper.unmount();
  });
  ```

  using the file's existing `mountShell` helper (extend it to accept record
  overrides and mount options if it does not). In
  `test/editor/editor-preview.test.ts` switch `shallowMount` to
  `mount(EditorPreview, { global: { stubs: { ResumeDocument: true } } })` and
  keep every projection and renderer assertion. Add the two toolbar photo-state
  cases to `editor-shell.test.ts`, where `PreviewToolbar` is rendered:
  `[data-photo-state]` contains `Photo is loading` / `Photo unavailable`. In
  `test/editor/accessibility.test.ts` keep every assertion; the `ErrorSummary`
  focus case now expects `document.activeElement` to be the banner
  (`wrapper.element`), which is unchanged.

- [ ] Run and watch the changed cases fail:

  ```sh
  cd apps/web && npx vitest run test/editor/editor-shell.test.ts test/editor/editor-preview.test.ts test/editor/accessibility.test.ts
  ```

- [ ] **PreviewToolbar.vue:**

  ```vue
  <script setup lang="ts">
  import { Badge } from "@/components/ui/badge";
  import { Button } from "@/components/ui/button";
  import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

  const props = defineProps<{
    readonly estimatedPages: number | null;
    readonly zoom: "fit" | "full";
    readonly photoState: "ready" | "loading" | "unavailable" | "none";
  }>();
  const emit = defineEmits<{
    "update:zoom": [zoom: "fit" | "full"];
    openPhoto: [];
  }>();

  const photoText = (): string =>
    props.photoState === "loading"
      ? "Photo is loading. The preview is shown without it."
      : props.photoState === "unavailable"
        ? "Photo unavailable. The preview is shown without it."
        : "";

  function onZoom(value: string | string[] | undefined): void {
    if (value === "fit" || value === "full") emit("update:zoom", value);
  }
  </script>

  <template>
    <div
      class="flex min-h-11 items-center gap-3 border-b border-border bg-card px-4 text-sm"
    >
      <h2 id="editor-preview-title" class="font-semibold">Preview</h2>
      <p class="flex items-center gap-1.5 text-muted-foreground">
        <span data-estimated-pages-label>Estimated pages</span>
        <output aria-label="Estimated page count">{{
          estimatedPages ?? "—"
        }}</output>
      </p>
      <template v-if="photoText() !== ''">
        <Badge data-photo-state role="status" variant="outline">{{
          photoText()
        }}</Badge>
        <Button size="sm" variant="link" @click="emit('openPhoto')"
          >Open photo panel</Button
        >
      </template>
      <ToggleGroup
        aria-label="Preview zoom"
        class="ml-auto"
        :model-value="zoom"
        size="sm"
        type="single"
        variant="outline"
        @update:model-value="onZoom"
      >
        <ToggleGroupItem value="fit">Fit</ToggleGroupItem>
        <ToggleGroupItem value="full">100%</ToggleGroupItem>
      </ToggleGroup>
    </div>
  </template>
  ```

- [ ] **InspectorPanel.vue** per the contract; **SaveStatus.vue**,
      **ErrorSummary.vue**, **ConflictPanel.vue** per the contract (scripts
      unchanged).

- [ ] **EditorShell.vue template.** Rebuild it per the contract. Wire
      `PreviewToolbar` with `:estimated-pages` lifted from `EditorPreview`
      through a new `@pages="estimatedPages = $event"` emit on `EditorPreview`,
      `:photo-state="photoStateFor(record.photoRead, document.personalDetails.photo !== undefined)"`,
      `v-model:zoom="zoom"`, and `@open-photo="inspector = { kind: 'photo' }"`.

- [ ] **`[id].vue` template** per the contract.

- [ ] Delete the rules listed for T07 from `app/assets/css/editor.css`. Also
      delete the root `.editor-inspector` layout rule, including its obsolete
      `grid-area: inspector`; retain the descendant inspector form rules until
      their owning surface tasks replace them.

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/editor-shell.test.ts test/editor/editor-preview.test.ts test/editor/accessibility.test.ts test/editor/conflicts.test.ts
  make -C ../.. web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T07 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs, the `EditorPreview` prop and emit list, and the
`mountShell` signature. Suggested commit:
`feat(editor): rebuild the editor shell on the shared components`.
