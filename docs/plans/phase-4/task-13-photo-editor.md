# Task 13: Private photo editor

**Owner:** One high-judgment web author.

**Authorities:** `editor-contract.md` Photo lifecycle and Conflict controls,
`design.md` Preview boundary/Security, `mutation-contract.md` opaque upload and
photo projections, ADR 0019 as consumed by P2B, and D15/D17.

**Acceptance:** AC-EDITOR-013 and AC-EDITOR-015.

**Files:**

- Create: `apps/web/app/editor/photoController.ts`
- Create: `apps/web/app/components/editor/photo/PhotoPanel.vue`
- Create: `apps/web/app/components/editor/photo/CropEditor.vue`
- Create: `apps/web/test/editor/photo-controller.test.ts`
- Create: `apps/web/test/editor/photo-panel.test.ts`

**Interfaces:**

```ts
export interface PhotoController {
  sync(accepted: AcceptedResume): Promise<void>;
  clear(): void;
}
export interface PhotoDataCodec {
  toDataURL(
    bytes: Uint8Array,
    mime: "image/jpeg" | "image/png",
  ): Promise<string>;
}
export function createPhotoController(deps: {
  api: ResumeApi;
  store: ReturnType<typeof useResumeStore>;
  codec: PhotoDataCodec;
}): PhotoController;
```

The controller calls only Task 03 `readOwnerPhoto` and Task 04 `setPhotoRead`.
The panel sends atomic intents through Task 05 `ResumeEditorActions.edit` and
never derives a URL from `photo.key`.

- [x] **Step 1: Write the generation/binding RED test**

```ts
const deferred = <T>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
};
const withPhoto = (value: AcceptedResume, key: string): AcceptedResume => ({
  ...value,
  document: {
    ...value.document,
    personalDetails: { ...value.document.personalDetails, photo: { key } },
  },
});
const bytes = (value: string): OwnerPhotoReadResult => ({
  kind: "bytes",
  mime: "image/png",
  etag: '"photo-1"' as ObjectETag,
  bytes: new TextEncoder().encode(value),
});
it("does not mutate after a stale generation resolves", async () => {
  const accepted = acceptedFixture();
  const resumeId = accepted.metadata.id;
  setActivePinia(createPinia());
  const store = useResumeStore();
  store.initialize(accepted);
  const readOwnerPhoto = vi.fn();
  const api = { readOwnerPhoto } as unknown as ResumeApi;
  const codec: PhotoDataCodec = {
    toDataURL: async (value, mime) => `data:${mime};base64,${value.length}`,
  };
  const first = deferred<OwnerPhotoReadResult>();
  readOwnerPhoto
    .mockReturnValueOnce(first.promise)
    .mockResolvedValueOnce(bytes("new"));
  const controller = createPhotoController({ api, store, codec });
  const oldSync = controller.sync(withPhoto(accepted, "old"));
  await controller.sync(withPhoto(accepted, "new"));
  const afterNew = structuredClone(store.recordFor(resumeId)!.photoRead);
  first.resolve(bytes("old"));
  await oldSync;
  expect(store.recordFor(resumeId)!.photoRead).toEqual(afterNew);
});
```

Add rows for absent metadata, conditional `304`, invalid MIME/tag, failure,
delete, and clear. Every late-generation row records state before resolution and
asserts byte-for-byte equality after resolution.

- [x] **Step 2: Run the generation/binding test RED**

```sh
(cd apps/web && npx vitest run test/editor/photo-controller.test.ts)
```

Expected RED: FAIL because controller does not exist.

- [x] **Step 3: Implement minimal keyed read state**

Capture the requested binding and a monotonically increasing generation:

```ts
const generation = ++activeGeneration;
store.setPhotoRead(resumeId, { kind: "loading", binding, generation });
const result = await api.readOwnerPhoto(resumeId, priorTag);
if (generation !== activeGeneration) return;
const current = store.recordFor(resumeId)?.photoRead;
if (
  current?.kind !== "loading" ||
  current.generation !== generation ||
  current.binding !== binding
)
  return;
if (acceptedPhotoKey() !== binding) {
  store.setPhotoRead(resumeId, {
    kind: "suspended",
    binding,
    generation,
    reason: "binding-mismatch",
  });
  return;
}
adoptPhotoResult(result, binding);
```

Encode only accepted bytes. A stale generation or replaced store state returns
without mutation; only the still-current loading generation may write ready,
read-failed, or binding-mismatch. Clear data on replace/delete/mismatch/unmount.

- [x] **Step 4: Rerun the generation/binding test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [x] **Step 5: Write the upload/cutoff RED test**

Before acceptance assert zero FileReader, Image, canvas, object URL, data URL,
storage, or preview operation. Test original file bytes, progress, replacement
clearing crop after acceptance, confirmed delete, source-photo reconfirmation,
and opaque unknown outcome. Map
type/size/invalid/busy/rate/network/read/session/ revision failures to safe
text; show valid Retry-After. After cutoff, render observed unchanged/changed/
unavailable without claiming success and offer exactly Keep observed photo and
Replace photo. Keep drops the old intent; Replace captures a new file command
and key; neither reuses the expired attempt.

```ts
it.each([
  ["keep-observed", undefined],
  ["replace", new File(["new"], "new.png", { type: "image/png" })],
] as const)("resolves a cutoff with %s", async (kind, file) => {
  const resolveOpaquePhoto = vi.fn();
  const actions = { resolveOpaquePhoto } as unknown as ResumeEditorActions;
  const wrapper = mount(PhotoPanel, {
    props: { record: cutoffRecord, actions },
  });
  if (file) await wrapper.get('input[type="file"]').setValue(file);
  await wrapper.get(`[data-action="${kind}"]`).trigger("click");
  expect(resolveOpaquePhoto).toHaveBeenCalledWith(
    cutoffRecord.opaquePhotoOutcome!.command.id,
    file ? { kind, file } : { kind },
  );
});
```

- [x] **Step 6: Run the upload/cutoff test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/photo-panel.test.ts)
```

Expected RED: FAIL because photo controls do not exist.

- [x] **Step 7: Implement minimal lifecycle controls**

The file input creates one `photoUpload` command holding the original `File`. Do
not inspect bytes in the panel. Replacement preview remains suspended until a
new accepted key and owner read bind. Delete enqueues only after exact current
photo confirmation.

```ts
const upload = (file: File) =>
  props.actions.edit({ kind: "photoUpload", file });
const keepObserved = () =>
  props.actions.resolveOpaquePhoto(
    props.record.opaquePhotoOutcome!.command.id,
    { kind: "keep-observed" },
  );
const replace = (file: File) =>
  props.actions.resolveOpaquePhoto(
    props.record.opaquePhotoOutcome!.command.id,
    { kind: "replace", file },
  );
```

- [x] **Step 8: Rerun the upload/cutoff test GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [x] **Step 9: Write the crop/suspension RED test**

Test pointer changes and four labelled numeric inputs for normalized x/y/width/
height, exact bounds, `{crop: rectangle}` versus `{crop: null}`, keyboard-only
operation, optimistic rectangle, same-photo safe override, and changed-photo
dedicated conflict. Loading/read failure/mismatch suspends preview while forms
and replace/delete stay enabled.

```ts
it("captures normalized crop against the accepted photo binding", async () => {
  const edit = vi.fn();
  const wrapper = mount(CropEditor, {
    props: { photoKey: "photo-a", actions: { edit } },
  });
  for (const [name, value] of [
    ["x", "0"],
    ["y", "0.25"],
    ["width", "1"],
    ["height", "0.75"],
  ])
    await wrapper.get(`[name="${name}"]`).setValue(value);
  await wrapper.get("form").trigger("submit");
  expect(edit).toHaveBeenCalledWith({
    kind: "photoCrop",
    crop: { x: 0, y: 0.25, width: 1, height: 0.75 },
  });
});
```

- [x] **Step 10: Run the crop/suspension test RED**

Run the Step 6 command. Expected RED: FAIL on the first missing crop or
suspension behavior.

- [x] **Step 11: Implement minimal crop binding**

Keep numeric draft strings separate until all four parse to finite bounded JSON
numbers. Capture `photoCrop` against exact accepted photo-key context. A changed
key offers Reopen crop only, never generic Apply mine.

```ts
const submitCrop = () => {
  const crop = parseBoundedCrop(draft);
  if (crop) props.actions.edit({ kind: "photoCrop", crop });
};
```

- [x] **Step 12: Rerun the crop/suspension test GREEN**

Run the Step 10 command. Expected GREEN: PASS.

- [x] **Step 13: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run \
  test/editor/{photo-controller,photo-panel}.test.ts)
(cd apps/web && npx eslint app/editor/photoController.ts \
  app/components/editor/photo \
  test/editor/{photo-controller,photo-panel}.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add private photo controls`.
