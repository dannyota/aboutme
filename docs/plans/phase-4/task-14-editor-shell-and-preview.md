# Task 14: Editor shell, preview, errors, and retained work

**Owner:** One high-judgment web integration author.

**Authorities:** all of `design.md` Resume editor through Error ownership,
`editor-contract.md` Editor layout/Validation/Conflict controls/Accessibility,
Phase 3 renderer interfaces, ADR 0005, and D11/D16/D17.

**Acceptance:** AC-EDITOR-006 and AC-EDITOR-014 through AC-EDITOR-016.

**Files:**

- Create: `apps/web/app/pages/app/resumes/[id].vue`
- Create: `apps/web/app/components/editor/EditorShell.vue`
- Create: `apps/web/app/components/editor/EditorPreview.vue`
- Create: `apps/web/app/components/editor/SaveStatus.vue`
- Create: `apps/web/app/components/editor/ErrorSummary.vue`
- Create: `apps/web/app/components/editor/ConflictPanel.vue`
- Create: `apps/web/app/editor/pageCountObserver.ts`
- Create: `apps/web/app/composables/useUnsavedNavigationGuard.ts`
- Create: `apps/web/app/assets/css/editor.css`
- Create: `apps/web/test/editor/editor-shell.test.ts`
- Create: `apps/web/test/editor/editor-preview.test.ts`
- Create: `apps/web/test/editor/page-count-observer.test.ts`
- Create: `apps/web/test/editor/navigation-guard.test.ts`
- Create: `apps/web/test/editor/accessibility.test.ts`
- Create: `apps/web/test/editor/persistence-boundary.test.ts`

**Interfaces:** `EditorShell` composes Tasks 07–13 over one Task 04 record.
`EditorPreview` alone imports `components/resume/ResumeDocument.vue` and
receives:

```ts
export interface EditorPreviewProps {
  document: Resume;
  lng: string;
  photoUrl?: string;
}
export interface PageCountObserverDeps {
  requestAnimationFrame(callback: FrameRequestCallback): number;
  cancelAnimationFrame(handle: number): void;
  createMutationObserver(
    callback: MutationCallback,
  ): Pick<MutationObserver, "observe" | "disconnect">;
  isVisible(page: HTMLElement): boolean;
}
export function observeSettledVisiblePageCount(
  root: HTMLElement,
  onSettled: (count: number) => void,
  deps?: PageCountObserverDeps,
): () => void;
```

It passes `{ document, context: { lng, mode: 'paged', photoUrl } }` and never
changes renderer code.

- [ ] **Step 1: Write the route/shell RED test**

Server-render the page and assert zero `/me` or resume request. In browser mode,
wait for resolved auth; redirect anonymous only with no local work; initialize a
valid owner response; merge missing/wrong-owner into unavailable; disable writes
on invalid response; and preserve state/focus when narrow Editor/Preview regions
switch.

```ts
it("does no owner I/O during SSR and waits for resolved auth", async () => {
  const read = vi.fn();
  await renderSuspended(EditorPage, {
    route: `/app/resumes/${resumeId}`,
    ssr: true,
  });
  expect(read).not.toHaveBeenCalled();
  authState.value = "authenticated";
  await mountSuspended(EditorPage, { route: `/app/resumes/${resumeId}` });
  expect(read).toHaveBeenCalledOnce();
});
```

- [ ] **Step 2: Run the route/shell test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/editor-shell.test.ts)
```

Expected RED: FAIL because route/shell do not exist.

- [ ] **Step 3: Implement minimal route composition**

The page calls `useResumeEditor(route.params.id)` only on client after auth. The
composable exhaustively maps Task 03 `ResumeReadResult`; only `complete`
initializes Task 04 state. Wide shell renders controls/preview side by side;
narrow shell toggles visibility without unmounting either subtree.
`EditorShell.vue` imports `editor.css`. Panels are imports only; shell performs
no transport write.

```ts
onMounted(async () => {
  await until(auth.authState).not.toBe("loading");
  if (auth.authState.value !== "authenticated") return navigateTo("/login");
  const read = await api.read(resumeId);
  if (read.kind === "complete") store.initialize(read.accepted);
  else unavailable.value = true;
});
```

- [ ] **Step 4: Rerun the route/shell test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the preview/page-count RED test**

Assert immediate optimistic render, matching-photo requirement, loading/read
failure suspension, typed renderer/pagination failures without state mutation,
and exact visible text `Estimated pages` using an exact-match assertion. Add an
import-boundary assertion that renderer files import no editor/store/API/Pinia/
Nuxt/network module. In `page-count-observer.test.ts`, include two visible pages
with `data-page-index="0"/"1"`, the hidden `.pagination-measurement` page, a
hidden indexed decoy, mutations between frames, and teardown. Assert only the
settled visible contiguous indexes report `2`.

```ts
it("reports only a settled contiguous visible page set", () => {
  const root = document.createElement("div");
  const onSettled = vi.fn();
  let mutation: MutationCallback = () => undefined;
  const callbacks = new Map<number, FrameRequestCallback>();
  let nextFrame = 0;
  const disconnect = vi.fn();
  const fakeObserverDeps: PageCountObserverDeps = {
    requestAnimationFrame: (callback) => {
      callbacks.set(++nextFrame, callback);
      return nextFrame;
    },
    cancelAnimationFrame: (handle) => void callbacks.delete(handle),
    createMutationObserver: (callback) => {
      mutation = callback;
      return { observe: vi.fn(), disconnect };
    },
    isVisible: (page) =>
      !page.hidden &&
      page.getAttribute("aria-hidden") !== "true" &&
      !page.hasAttribute("inert"),
  };
  const runNext = () => {
    const [handle, callback] = callbacks.entries().next().value!;
    callbacks.delete(handle);
    callback(0);
  };
  root.innerHTML = [
    '<article class="resume-page" data-page-index="0"></article>',
    '<article class="resume-page" data-page-index="1"></article>',
    '<article class="resume-page pagination-measurement" data-page-index="2"></article>',
    '<article class="resume-page" data-page-index="8" hidden></article>',
  ].join("");
  const stop = observeSettledVisiblePageCount(
    root,
    onSettled,
    fakeObserverDeps,
  );
  runNext();
  mutation([], {} as MutationObserver);
  expect(onSettled).not.toHaveBeenCalled();
  while (callbacks.size) runNext();
  expect(onSettled).toHaveBeenCalledExactlyOnceWith(2);
  stop();
  expect(disconnect).toHaveBeenCalledOnce();
  expect(callbacks.size).toBe(0);
});
```

- [ ] **Step 6: Run the preview/page-count test RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/editor/{editor-preview,page-count-observer}.test.ts)
```

Expected RED: FAIL because adapter is absent.

- [ ] **Step 7: Implement the preview adapter and observer**

Pass only props from the interface. Include `photoUrl` only when Task 04 ready
binding equals current photo key. Catch `ResumeRenderError` and
`PaginationError` to stable text; catch unknown exceptions to generic text.
Render `<span>Estimated pages</span>` exactly, with the observer count in a
separate labelled value. The editor-owned observer never changes renderer code:

```ts
const selector = [
  ".resume-page[data-page-index]",
  ":not(.pagination-measurement)",
  ':not([aria-hidden="true"])',
  ":not([hidden])",
  ":not([inert])",
].join("");
const readSignature = () => {
  const indexes = [...root.querySelectorAll<HTMLElement>(selector)]
    .filter((page) => deps.isVisible(page))
    .map((page) => page.dataset.pageIndex ?? "");
  if (
    indexes.length === 0 ||
    indexes.some((value, index) => value !== String(index))
  )
    return null;
  return indexes.join(",");
};
let frame = 0;
let first: string | null = null;
const cancel = () => {
  if (frame) deps.cancelAnimationFrame(frame);
  frame = 0;
  first = null;
};
const schedule = () => {
  cancel();
  frame = deps.requestAnimationFrame(() => {
    first = readSignature();
    if (first === null) return;
    frame = deps.requestAnimationFrame(() => {
      const second = readSignature();
      if (second === first) onSettled(second.split(",").length);
      frame = 0;
    });
  });
};
const observer = deps.createMutationObserver(schedule);
observer.observe(root, {
  subtree: true,
  childList: true,
  attributes: true,
  attributeFilter: [
    "class",
    "data-page-index",
    "hidden",
    "inert",
    "aria-hidden",
  ],
});
schedule();
return () => {
  observer.disconnect();
  cancel();
};
```

Default dependencies use `getComputedStyle(page)` and require both
`display !== "none"` and `visibility !== "hidden"` in `isVisible`; tests inject
the deterministic function above.

Disconnect the observer and cancel frames on unmount.

- [ ] **Step 8: Rerun the preview/page-count test GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [ ] **Step 9: Write the status/error/conflict/accessibility RED test**

Exercise every save state and template partial in polite regions. Handled errors
focus summary; mapped paths focus labelled fields; unmapped paths remain listed;
success does not move focus. Render generic Apply mine only for a Task 02 valid
replacement and dedicated controls for identity/reorder/structure/photo/
template/destructive conflicts. Cover labels, headings, dialogs, visible focus,
narrow switch, and non-color text.

```ts
it.each([
  "idle",
  "dirty",
  "saving",
  "saved",
  "offline",
  "error",
  "conflict",
  "session-lost",
] as const)("announces %s without rendering a raw message", (saveState) => {
  const wrapper = mount(EditorShell, {
    props: { record: recordWith({ saveState }) },
  });
  expect(wrapper.get('[role="status"]').text()).not.toContain(
    "sentinel raw server text",
  );
  expect(wrapper.get('[role="status"]').text()).not.toBe("");
});
```

- [ ] **Step 10: Run the status/accessibility test RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/editor/{editor-shell,accessibility}.test.ts)
```

Expected RED: FAIL on the first missing status/error/focus behavior.

- [ ] **Step 11: Implement minimal status/error/conflict UI**

Use exhaustive discriminated-union switches and stable copy keyed by error code.
Never interpolate raw server messages, object keys, filenames, tokens, request
bodies, or stacks. Focus summary after DOM update and return dialog focus to its
trigger.

```ts
const issueText = (issue: ServerValidationIssue) =>
  SAFE_ISSUE_COPY[issue.code] ?? "Check this value.";
const applyMine = (conflict: ConflictRecord) => {
  const confirmation = confirmationForVisibleControl(conflict);
  if (confirmation) actions.applyMine(conflict.id, confirmation);
};
```

- [ ] **Step 12: Rerun the status/accessibility test GREEN**

Run the Step 10 command. Expected GREEN: PASS.

- [ ] **Step 13: Write the leave/session/persistence RED test**

Warn for pending, in-flight, failed, unknown, partial, conflicted, or
session-lost work; do not warn when fully accepted; never send unload request or
beacon. Instrument both local/session `Storage` get/set/remove/clear,
`indexedDB.open/deleteDatabase`, history push/replace, `navigator.sendBeacon`,
and current full URL/query/hash. After editor load, capture the URL baseline.
Assert zero persistence/history/beacon calls and an unchanged full URL, query,
and hash after one edit and failed/session-lost state. After explicit discard,
assert only the intended login navigation and no resume data in its query/hash.
Assert no direct `fetch` outside Task 03 transport.

```ts
it("keeps failed and session-lost edits only in memory", async () => {
  const local = ["getItem", "setItem", "removeItem", "clear"].map((name) =>
    vi.spyOn(window.localStorage, name as "clear"),
  );
  const session = ["getItem", "setItem", "removeItem", "clear"].map((name) =>
    vi.spyOn(window.sessionStorage, name as "clear"),
  );
  const indexed = [
    vi.spyOn(window.indexedDB, "open"),
    vi.spyOn(window.indexedDB, "deleteDatabase"),
  ];
  const history = [
    vi.spyOn(window.history, "pushState"),
    vi.spyOn(window.history, "replaceState"),
  ];
  const sendBeacon = vi.spyOn(window.navigator, "sendBeacon");
  const before = {
    href: location.href,
    search: location.search,
    hash: location.hash,
  };
  const expectNoPersistence = () =>
    expect(
      [...local, ...session, ...indexed, ...history, sendBeacon].every(
        (probe) => probe.mock.calls.length === 0,
      ),
    ).toBe(true);
  const expectURLUnchanged = () =>
    expect({
      href: location.href,
      search: location.search,
      hash: location.hash,
    }).toEqual(before);
  const wrapper = mount(EditorShell, { props: { record, actions } });
  expectNoPersistence();
  expectURLUnchanged();
  await wrapper.get('[data-entry-field="jobTitle"]').setValue("Changed");
  expectNoPersistence();
  expectURLUnchanged();
  await setSessionLost(record);
  expectNoPersistence();
  expectURLUnchanged();
  expect(wrapper.get('[data-action="resume-after-auth"]').exists()).toBe(true);
});
```

- [ ] **Step 14: Run the persistence-boundary test RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/editor/{navigation-guard,persistence-boundary}.test.ts)
```

Expected RED: FAIL because guards/boundary proof are absent.

- [ ] **Step 15: Implement minimal guard/session UI**

Route leave returns false while unsafe work exists. `beforeunload` only sets
`event.returnValue`; it calls no async API. Session loss keeps the mounted shell
and offers Open sign-in in another tab, Resume after sign-in, and Discard and
sign in. Discard calls Task 05 before navigation.

```ts
const beforeUnload = (event: BeforeUnloadEvent) => {
  if (!hasUnsafeWork(record.value)) return;
  event.preventDefault();
  event.returnValue = "";
};
const discardAndSignIn = async () => {
  actions.discard();
  await navigateTo("/login");
};
```

- [ ] **Step 16: Rerun the persistence-boundary test GREEN**

Run the Step 14 command. Expected GREEN: PASS.

- [ ] **Step 17: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run \
  test/editor/{editor-shell,editor-preview,page-count-observer,navigation-guard,accessibility,persistence-boundary}.test.ts)
(cd apps/web && npx eslint app/pages/app/resumes/'[id].vue' \
  app/components/editor/{EditorShell,EditorPreview,SaveStatus,ErrorSummary,ConflictPanel}.vue \
  app/editor/pageCountObserver.ts app/composables/useUnsavedNavigationGuard.ts \
  test/editor/{editor-shell,editor-preview,page-count-observer,navigation-guard,accessibility,persistence-boundary}.test.ts)
make web-lint web-typecheck web-test web-build
```

Suggested commit: `feat(editor): assemble authenticated editor`.
