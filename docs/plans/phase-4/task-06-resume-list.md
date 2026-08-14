# Task 06: Authenticated resume list and lifecycle actions

**Owner:** One web author.

**Authorities:** `design.md` Resume list and Initial load, current OpenAPI,
`mutation-contract.md` opaque create/resume delete rules, `editor-contract.md`
Validation/Accessibility, and D11/D12/D17.

**Acceptance:** AC-EDITOR-002 and AC-EDITOR-016.

**Files:**

- Create: `apps/web/app/pages/app/resumes/index.vue`
- Create: `apps/web/app/composables/useResumeList.ts`
- Create: `apps/web/app/components/editor/list/ResumeList.vue`
- Create: `apps/web/app/components/editor/list/CreateResumeDialog.vue`
- Create: `apps/web/app/components/editor/list/RenameResumeDialog.vue`
- Create: `apps/web/app/components/editor/list/DeleteResumeDialog.vue`
- Create: `apps/web/test/editor/resume-list.test.ts`

**Interfaces:** `useResumeList` reads through Task 03 `ResumeApi`. Create
captures Task 01 `CreateResumeIntent`, calls Task 05
`createResume`/`retryCreate`, and navigates only from same-key validated `201`.
Post-cutoff create UI calls `refreshOpaqueCreate` or `abandonOpaqueCreate`; a
new submit after abandon captures a new intent and key. Rename/delete first call
`ResumeApi.read`, initialize Task 04 store state, then enqueue through Task 05.

```ts
export type ResumeListView =
  | { readonly kind: "waiting-auth" }
  | { readonly kind: "loading" }
  | { readonly kind: "ready"; readonly items: readonly ResumeSummary[] }
  | { readonly kind: "unavailable" };
export interface ResumeListController {
  readonly view: Ref<ResumeListView>;
  readonly items: ComputedRef<readonly ResumeSummary[]>;
  settled(): Promise<void>;
  create(
    title: string,
    lng: string | null | undefined,
  ): Promise<CreateResumeResult>;
  refreshCreate(intentId: string): Promise<OpaqueCreateOutcome>;
  abandonCreate(intentId: string): void;
  rename(id: string, title: string): Promise<void>;
  remove(id: string, confirmedTitle: string): Promise<void>;
}
export function useResumeList(deps?: {
  api?: ResumeApi;
  authState?: Ref<AuthState>;
  ownerId?: Ref<string | null>;
  runtime?: EditorRuntime;
  coordinator?: ResumeMutationCoordinator;
  store?: ReturnType<typeof useResumeStore>;
  actionsFor?: (resumeId: string) => ResumeEditorActions;
}): ResumeListController;
```

- [ ] **Step 1: Write the list/load RED test**

Prove no list call while auth loads, redirect after resolved anonymous state,
one browser read after authentication, oldest-first rendering, empty state, safe
error, and open route.

```ts
it("waits for auth and preserves server order", async () => {
  const accepted = acceptedFixture();
  const older: ResumeSummary = {
    ...accepted.metadata,
    revision: parseRevision("1"),
  };
  const newer: ResumeSummary = {
    ...accepted.metadata,
    id: "resume-2",
    revision: parseRevision("2"),
  };
  const api = {
    list: vi.fn().mockResolvedValue({ kind: "ready", items: [older, newer] }),
  };
  const authState = ref<AuthState>("loading");
  const list = useResumeList({ api: api as ResumeApi, authState });
  expect(api.list).not.toHaveBeenCalled();
  authState.value = "authenticated";
  await nextTick();
  await list.settled();
  expect(list.items.value).toEqual([older, newer]);
});
```

- [ ] **Step 2: Run the list/load test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/resume-list.test.ts)
```

Expected RED: FAIL because the route and composable do not exist.

- [ ] **Step 3: Implement the minimal list transition**

Model list state as `waiting-auth | loading | ready | unavailable`; only the
authenticated transition calls `api.list()` and exhaustively switches on
`ResumeListResult`. Preserve API order and route open to `/app/resumes/${id}`.

```ts
watch(
  authState,
  async (state) => {
    if (state !== "authenticated") return;
    view.value = { kind: "loading" };
    const result = await api.list();
    view.value =
      result.kind === "ready"
        ? { kind: "ready", items: result.items }
        : { kind: "unavailable" };
  },
  { immediate: true },
);
```

- [ ] **Step 4: Rerun the list/load test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the create/cutoff RED test**

Assert labelled title, optional language absence/null/empty, omitted `document`,
one injected intent UUID, duplicate-submit prevention, resume-cap copy, retained
unknown state, exact retry, and navigation only to validated returned ID. At
cutoff, assert refreshed summaries never imply which create succeeded; expose
only Refresh list and Abandon outcome. A later Create is disabled until abandon
and then uses a new intent ID/key.

```ts
it("requires abandonment before a new create after cutoff", async () => {
  const intent: CreateResumeIntent = {
    kind: "resumeCreate",
    id: "intent-1",
    ownerId: "owner-1",
    sequence: 0,
    title: "First",
  };
  const outcome: OpaqueCreateOutcome = {
    kind: "create-cutoff",
    intent,
    attempt: {} as FrozenAttempt,
    refreshedItems: [],
  };
  const coordinator = {
    createResume: vi.fn(),
    abandonOpaqueCreate: vi.fn(),
  } as unknown as ResumeMutationCoordinator & {
    createResume: ReturnType<typeof vi.fn>;
    abandonOpaqueCreate: ReturnType<typeof vi.fn>;
  };
  coordinator.createResume
    .mockResolvedValueOnce({ kind: "opaque-create", outcome })
    .mockResolvedValueOnce({ kind: "created", resume: acceptedFixture() });
  const ids = ["intent-1", "intent-2"];
  const uuid = vi.fn(() => ids.shift()!);
  const runtime: EditorRuntime & { uuid: ReturnType<typeof vi.fn> } = {
    nowEpochMs: () => 0,
    uuid,
    delay: async () => {},
  };
  const list = useResumeList({ coordinator, runtime, ownerId: ref("owner-1") });
  await expect(list.create("First", undefined)).resolves.toEqual({
    kind: "opaque-create",
    outcome,
  });
  list.abandonCreate(outcome.intent.id);
  await list.create("Second", undefined);
  expect(coordinator.abandonOpaqueCreate).toHaveBeenCalledWith(
    outcome.intent.id,
  );
  expect(runtime.uuid).toHaveBeenCalledTimes(2);
});
```

- [ ] **Step 6: Run the create/cutoff test RED**

Run the Step 2 command. Expected RED: FAIL at the first missing create case.

- [ ] **Step 7: Implement the minimal create transition**

Build
`{ kind: 'resumeCreate', id, ownerId, sequence, title, ...(lngWasTouched && {lng}) }`.
Keep the frozen intent in dialog memory while blocked. Never infer success from
list order.

```ts
const submit = async () => {
  if (retained.value) return;
  const intent: CreateResumeIntent = {
    kind: "resumeCreate",
    id: runtime.uuid(),
    ownerId,
    sequence: 0,
    title,
    ...(lngWasTouched.value ? { lng: lng.value } : {}),
  };
  const result = await coordinator.createResume(intent);
  if (result.kind === "created")
    return navigateTo(`/app/resumes/${result.resume.metadata.id}`);
  if (result.kind === "opaque-create") retained.value = result.outcome;
};
```

- [ ] **Step 8: Rerun the create/cutoff test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 9: Write the rename/delete RED test**

Prove both actions require `ResumeReadResult.kind === 'complete'` first and
never use summary revision. Rename enqueues `metadataField`. Delete requires the
current visible title, enqueues a bodyless destructive command, reconfirms after
conflict, removes the row only after definitive `204`, and merges
missing/wrong-owner into one result. Cover dialog Escape, focus return, busy
isolation, and keyboard submit.

```ts
it("reads a complete owner snapshot before rename or delete", async () => {
  const accepted = acceptedFixture();
  const read = vi.fn().mockResolvedValue({ kind: "complete", accepted });
  const api = { read } as unknown as ResumeApi & {
    read: ReturnType<typeof vi.fn>;
  };
  setActivePinia(createPinia());
  const store = useResumeStore();
  const edit = vi.fn();
  const actions = { edit } as unknown as ResumeEditorActions;
  const list = useResumeList({ api, store, actionsFor: () => actions });
  await list.rename(accepted.metadata.id, "New title");
  await list.remove(accepted.metadata.id, accepted.metadata.title);
  expect(read).toHaveBeenCalledTimes(2);
  expect(edit.mock.calls.map(([intent]) => intent.kind)).toEqual([
    "metadataField",
    "resumeDelete",
  ]);
});
```

- [ ] **Step 10: Run the rename/delete test RED**

Run the Step 2 command. Expected RED: FAIL at the first missing lifecycle case.

- [ ] **Step 11: Implement minimal lifecycle actions**

Disable only the active row. Dialog close drops unsent dialog intent but never
discards queued store work. On `resume-deleted`, remove store/list record and
return focus to the next row or Create button.

```ts
const rename = async (id: string, title: string) => {
  const read = await api.read(id);
  if (read.kind !== "complete") return showUnavailable();
  store.initialize(read.accepted);
  actionsFor(id).edit({ kind: "metadataField", field: "title", value: title });
};
```

- [ ] **Step 12: Rerun the rename/delete test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 13: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/resume-list.test.ts)
(cd apps/web && npx eslint app/pages/app/resumes/index.vue \
  app/composables/useResumeList.ts app/components/editor/list \
  test/editor/resume-list.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add resume list lifecycle`.
