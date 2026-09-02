# Task 12 — Photo panel and crop editor

**Acceptance:** AC-UI-002, AC-UI-004, AC-UI-005, AC-UI-006.

**Depends on:** T07 (`InspectorPanel`), T03.

**Owned paths:** T12 paths in `file-structure.md`.

## Contract

- `PhotoPanel.vue` keeps its script except the delete-dialog focus code and
  renders `InspectorPanel` (`title="Photo"`, `titleId="photo-title"`) with:
  1. The preview block `div[data-photo-preview][aria-live="polite"]`: a
     `size-32 rounded-lg border bg-muted` frame that shows the `img` only when
     the read is `ready` for the current key, else a `ImageOff` icon, with the
     `previewText()` line beneath in `text-sm text-muted-foreground`.
  2. Status and delete-status lines through `StatusBanner` (`kind="error"` for
     failures, `kind="info"` for `Uploading photo.`), keeping `role="status"`
     for non-error texts.
  3. The upload control: a `Label` styled as a dropzone
     (`flex cursor-pointer flex-col items-center gap-1 rounded-lg border border-dashed p-4 text-sm hover:bg-accent`)
     wrapping the native `input type="file" accept="image/jpeg,image/png"` with
     class `sr-only`, text `Upload photo` or `Replace photo` and the hint
     `JPEG or PNG, up to 2 MB.`
  4. `Delete photo` as `Button variant="ghost" size="sm"`
     (`data-action="delete"`) opening `ConfirmDialog` (`title="Delete photo"`,
     `description="Delete the current photo?"`, `confirmLabel="Delete photo"`,
     `destructive`, `confirmAction="confirm-delete"`,
     `cancelAction="cancel-delete"`); the rebind check in `confirmDelete` stays.
  5. The opaque-outcome block `div[data-photo-outcome]` as `Card` with the two
     texts, the replacement file `Label` (`Select a replacement photo`, same
     dropzone style), and `Keep observed photo` / `Replace photo` buttons with
     their `data-action` values.
  6. `Retry photo request` (`data-action="retry-photo"`) and the crop conflict
     block (`StatusBanner kind="info"` with `Reopen crop`).
  7. `CropEditor` inside a `Card` when the read is ready.
- `CropEditor.vue` keeps its script. The stage keeps `data-crop-stage`,
  `role="application"`, `aria-label="Crop position"`, `tabindex="0"` and gets
  `relative aspect-square max-w-64 overflow-hidden rounded-md border bg-muted focus-visible:ring-2`;
  the rectangle keeps `data-crop-rectangle` with
  `absolute border-2 border-positive bg-positive/10`. The four numbers render
  through `FormField` + `Input type="number"` in a `grid grid-cols-2 gap-2` with
  labels `X`, `Y`, `Width`, `Height`; the error through the group's
  `role="alert"` paragraph; `Save crop` is `Button type="submit" size="sm"` and
  `Clear crop` `Button variant="ghost" size="sm" data-action="clear-crop"`.

## Hook changes

- The delete confirmation is an `alertdialog` on `document.body`; the
  `role="alertdialog"` element inside the panel is gone.
- `input[type="file"]` stays native and keeps its labels; it is visually hidden
  (`sr-only`), not removed.

## Strings held

Everything under "Photo" in the retained hooks list.

## TDD cycle

- [ ] **RED.** In `test/editor/photo-panel.test.ts` keep every assertion on
      `[data-photo-preview]`, `[data-photo-outcome]`, `data-action` buttons,
      status texts, and `input[type="file"]` (`getByLabel` equivalents:
      `wrapper.get('input[type="file"]')` still works because the input is in
      the DOM). Rewrite the delete-dialog cases to `attachTo: document.body` +
      `document.body.querySelector('[role="alertdialog"]')` and keep the rebind
      case (`deleteStatus` text
      `This photo changed. Reopen deletion and confirm again.`). Add:

  ```ts
  it("shows the frame without an image until the read is ready", () => {
    const { wrapper } = mountPanel({
      photoRead: { kind: "loading", binding: "k", generation: 1 },
    });
    const preview = wrapper.get("[data-photo-preview]");
    expect(preview.find("img").exists()).toBe(false);
    expect(preview.text()).toContain("Photo preview is loading.");
    expect(
      wrapper
        .find(
          'button:not([data-slot="button"]):not([role="checkbox"]):not([role="switch"])',
        )
        .exists(),
    ).toBe(false);
  });
  ```

  using the file's `mountPanel` helper.

- [ ] Run and watch the file fail:

  ```sh
  cd apps/web && npx vitest run test/editor/photo-panel.test.ts
  ```

- [ ] **Upload control:**

  ```vue
  <Label
    class="flex cursor-pointer flex-col items-center gap-1 rounded-lg border border-dashed border-border p-4 text-center text-sm hover:bg-accent"
    :for="uploadId"
  >
    <Upload aria-hidden="true" class="size-5 text-muted-foreground" />
    <span class="font-medium">{{ photo === undefined ? 'Upload photo' : 'Replace photo' }}</span>
    <span class="text-xs text-muted-foreground">JPEG or PNG, up to 2 MB.</span>
    <input
      :id="uploadId"
      accept="image/jpeg,image/png"
      class="sr-only"
      type="file"
      @change="upload"
    >
  </Label>
  ```

  with `uploadId` computed as the string `photo-upload-` followed by `useId()`.
  Because the file input is inside its label, `getByLabel` lookups for
  `Upload photo` and `Replace photo` keep resolving in the browser proofs.

- [ ] Rebuild the rest of `PhotoPanel.vue` and `CropEditor.vue` per the
      contract; delete `trapDeleteFocus`, `confirmDeleteButton`, `deleteOpener`,
      and the `nextTick` focus calls (the dialog owns focus).

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/editor/photo-panel.test.ts test/editor/photo-controller.test.ts
  make web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T12 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs. Suggested commit:
`feat(editor): rebuild the photo panel and crop editor`.
