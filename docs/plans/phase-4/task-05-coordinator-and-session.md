# Task 05: Mutation coordinator and session recovery

**Owner:** One high-judgment web author.

**Authorities:** `design.md` Edit and save/Session loss/Leave and recover, all
of `mutation-contract.md`, all of `template-group-contract.md`, ADRs 0016/0024,
and D7–D10/D14.

**Acceptance:** AC-EDITOR-004 through AC-EDITOR-007, AC-EDITOR-012, and
AC-EDITOR-016.

**Files:**

- Create: `apps/web/app/editor/coordinator.ts`
- Create: `apps/web/app/composables/useResumeEditor.ts`
- Modify: `apps/web/app/composables/useAuth.ts`
- Create: `apps/web/test/editor/coordinator.test.ts`
- Modify: `apps/web/test/useAuth.test.ts`
- Modify: `apps/web/test/useAuth-csrf-rotation.test.ts`

**Interfaces:** This task is the only definition site for create and coordinator
results:

```ts
export type AuthState = "loading" | "authenticated" | "anonymous" | "error";
export interface UseAuthReturn {
  user: ComputedRef<AuthUser | null>;
  csrfToken: ComputedRef<string | null>;
  identities: ComputedRef<AuthIdentity[]>;
  authState: ComputedRef<AuthState>;
  refresh(): Promise<void>;
  logout(): Promise<void>;
  mutate<T = void>(url: string, options: MutateOptions): Promise<T>;
}
export type CreateResumeResult =
  | { readonly kind: "created"; readonly resume: AcceptedResume }
  | {
      readonly kind: "blocked";
      readonly intentId: string;
      readonly reason: "unknown" | "session-lost";
    }
  | {
      readonly kind: "retry-later";
      readonly intentId: string;
      readonly retryAfterMs: number | null;
    }
  | { readonly kind: "opaque-create"; readonly outcome: OpaqueCreateOutcome }
  | { readonly kind: "rejected"; readonly code: AttemptFailureCode };
export interface OpaqueCreateOutcome {
  readonly kind: "create-cutoff";
  readonly intent: CreateResumeIntent;
  readonly attempt: FrozenAttempt;
  readonly refreshedItems: readonly ResumeSummary[] | null;
}
export type OpaquePhotoDecision =
  | { readonly kind: "keep-observed" }
  | { readonly kind: "replace"; readonly file: File };
export type EditorActionResult =
  | { readonly kind: "enqueued"; readonly command: AtomicEditorCommand }
  | {
      readonly kind: "blocked";
      readonly reason: "not-loaded" | "session-lost" | "owner-mismatch";
    };
export type TemplateActionResult =
  | { readonly kind: "enqueued"; readonly group: TemplateGroupCommand }
  | { readonly kind: "no-change" }
  | Extract<EditorActionResult, { kind: "blocked" }>;
export interface ResumeEditorActions {
  readonly record: ComputedRef<ResumeRecord | undefined>;
  edit(intent: AtomicCommandIntent): EditorActionResult;
  applyTemplate(preset: Readonly<TemplatePreset>): TemplateActionResult;
  resolveOpaquePhoto(
    commandId: string,
    decision: OpaquePhotoDecision,
  ): Promise<void>;
  retry(commandId: string): Promise<void>;
  acceptLatest(conflictId: string): Promise<void>;
  applyMine(
    conflictId: string,
    confirmation: ConflictConfirmation,
  ): Promise<void>;
  resumeAfterAuth(): Promise<void>;
  discard(): void;
}
export interface ResumeMutationCoordinator {
  createResume(intent: CreateResumeIntent): Promise<CreateResumeResult>;
  retryCreate(intentId: string): Promise<CreateResumeResult>;
  refreshOpaqueCreate(intentId: string): Promise<OpaqueCreateOutcome>;
  abandonOpaqueCreate(intentId: string): void;
  schedule(resumeId: string): void;
  flush(resumeId: string): Promise<void>;
  retry(resumeId: string, commandId: string): Promise<void>;
  refreshAndReconcile(resumeId: string): Promise<void>;
  acceptLatest(resumeId: string, conflictId: string): Promise<void>;
  applyMine(
    resumeId: string,
    conflictId: string,
    confirmation: ConflictConfirmation,
  ): Promise<void>;
  resumeAfterAuth(resumeId: string): Promise<void>;
  resolveOpaquePhoto(
    resumeId: string,
    commandId: string,
    decision: OpaquePhotoDecision,
  ): Promise<void>;
  discard(resumeId: string): void;
}
export function createMutationCoordinator(deps: {
  api: ResumeApi;
  store: ReturnType<typeof useResumeStore>;
  auth: ReturnType<typeof useAuth>;
  runtime: EditorRuntime;
}): ResumeMutationCoordinator;
export const browserEditorRuntime: EditorRuntime;
export interface ResumeEditorActionDeps {
  resumeId: string;
  store: ReturnType<typeof useResumeStore>;
  coordinator: ResumeMutationCoordinator;
  auth: ReturnType<typeof useAuth>;
  runtime: EditorRuntime;
}
export function createResumeEditorActions(
  deps: ResumeEditorActionDeps,
): ResumeEditorActions;
export function useResumeEditor(
  resumeId: string,
  runtime?: EditorRuntime,
): ResumeEditorActions;
```

Only this coordinator calls `ResumeApi.dispatch`. It retains a frozen create
attempt by intent ID outside resume records until same-key `201`, definitive
rejection, explicit abandonment, or cutoff. `useResumeEditor` defaults to
`browserEditorRuntime`; tests inject a fake runtime. `edit` reads the current
owner, sequence, and dependency IDs, calls Task 01 `captureCommand`, calls the
store's `enqueue`, then schedules the coordinator. `applyTemplate` calls Task 02
with the same runtime so group and child IDs are injected once.

- [ ] **Step 1: Write the auth-state RED test**

Cover loading, authenticated, anonymous `401`, transient error, refreshed token,
and another-tab session recovery. Preserve existing settings `mutate()`
behavior. `refresh()` must resolve only after state and CSRF reflect the
response.

```ts
it("resolves refresh after identity, state, and CSRF update", async () => {
  meHandler.reply({ data: authenticatedMe("owner-1", "csrf-new") });
  const auth = useAuth();
  await auth.refresh();
  expect(auth.authState.value).toBe("authenticated");
  expect(auth.user.value?.id).toBe("owner-1");
  expect(auth.csrfToken.value).toBe("csrf-new");
});
```

- [ ] **Step 2: Run the auth-state test RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/useAuth.test.ts test/useAuth-csrf-rotation.test.ts)
```

Expected RED: FAIL because resolved `authState` is absent.

- [ ] **Step 3: Implement minimal resolved auth state**

Use a single in-flight refresh promise. Map `/me` `200` to authenticated, `401`
to anonymous, and transport/server failure to error without erasing the last
token before resolution.

```ts
const authState = computed<AuthState>(() => {
  if (status.value === "idle" || status.value === "pending") return "loading";
  if (error.value?.statusCode === 401) return "anonymous";
  if (error.value) return "error";
  return data.value?.data?.user ? "authenticated" : "error";
});
```

- [ ] **Step 4: Rerun the auth-state test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the action-boundary RED test**

```ts
it("captures atomic and template IDs from the injected runtime", () => {
  const actions = createResumeEditorActions({ resumeId, store, coordinator, auth, runtime });
  expect(actions.edit({ kind: "metadataField", field: "title", value: "Ada" })).toMatchObject({
    kind: "enqueued", command: { id: "command-1", ownerId: "owner-1", sequence: 1 },
  });
  expect(actions.applyTemplate(preset)).toMatchObject({
    kind: "enqueued",
    group: { id: "group-1", children: [{ id: "structure-1" }, { id: "customization-1" }] },
  });
  expect(store.recordFor(resumeId)!.pending.map(({ kind }) => kind).toEqual([
    "metadataField", "templateGroup",
  ]);
  expect(coordinator.schedule).toHaveBeenCalledTimes(2);
});
```

- [ ] **Step 6: Run the action-boundary test RED**

```sh
(cd apps/web && npx vitest run test/editor/coordinator.test.ts)
```

Expected RED: FAIL because `createResumeEditorActions` is absent.

- [ ] **Step 7: Implement the exact action boundary**

```ts
export const browserEditorRuntime: EditorRuntime = {
  nowEpochMs: () => Date.now(),
  uuid: () => crypto.randomUUID(),
  delay: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
};
export function createResumeEditorActions(
  deps: ResumeEditorActionDeps,
): ResumeEditorActions {
  const record = computed(() => deps.store.recordFor(deps.resumeId));
  const blocked = (
    reason: Extract<EditorActionResult, { kind: "blocked" }>["reason"],
  ): Extract<EditorActionResult, { kind: "blocked" }> => ({
    kind: "blocked",
    reason,
  });
  const edit = (intent: AtomicCommandIntent): EditorActionResult => {
    const state = record.value;
    const ownerId = deps.auth.user.value?.id;
    if (!state || !ownerId) return blocked("not-loaded");
    if (state.sessionLost) return blocked("session-lost");
    const pendingOwner = state.pending[0]?.ownerId;
    if (pendingOwner && pendingOwner !== ownerId)
      return blocked("owner-mismatch");
    const command = captureCommand(
      state.current,
      {
        resumeId: deps.resumeId,
        ownerId,
        sequence: nextSequence(state),
        dependencyIds: dependencyIdsForNewCommand(state),
        intent,
      },
      deps.runtime,
    );
    deps.store.enqueue(deps.resumeId, command);
    deps.coordinator.schedule(deps.resumeId);
    return { kind: "enqueued", command };
  };
  const applyTemplate = (
    preset: Readonly<TemplatePreset>,
  ): TemplateActionResult => {
    const state = record.value;
    const ownerId = deps.auth.user.value?.id;
    if (!state || !ownerId) return blocked("not-loaded");
    if (state.sessionLost) return blocked("session-lost");
    const pendingOwner = state.pending[0]?.ownerId;
    if (pendingOwner && pendingOwner !== ownerId)
      return blocked("owner-mismatch");
    const group = captureTemplateGroup({
      resumeId: deps.resumeId,
      ownerId,
      sequence: nextSequence(state),
      current: state.current,
      preset,
      dependencyIds: dependencyIdsForNewCommand(state),
      runtime: deps.runtime,
    });
    if (!group) return { kind: "no-change" };
    deps.store.enqueue(deps.resumeId, group);
    deps.coordinator.schedule(deps.resumeId);
    return { kind: "enqueued", group };
  };
  return {
    record,
    edit,
    applyTemplate,
    resolveOpaquePhoto: (commandId, decision) =>
      deps.coordinator.resolveOpaquePhoto(deps.resumeId, commandId, decision),
    retry: (commandId) => deps.coordinator.retry(deps.resumeId, commandId),
    acceptLatest: (conflictId) =>
      deps.coordinator.acceptLatest(deps.resumeId, conflictId),
    applyMine: (conflictId, confirmation) =>
      deps.coordinator.applyMine(deps.resumeId, conflictId, confirmation),
    resumeAfterAuth: () => deps.coordinator.resumeAfterAuth(deps.resumeId),
    discard: () => deps.coordinator.discard(deps.resumeId),
  };
}
export function useResumeEditor(
  resumeId: string,
  runtime: EditorRuntime = browserEditorRuntime,
): ResumeEditorActions {
  const store = useResumeStore();
  const auth = useAuth();
  const coordinator = createMutationCoordinator({
    api: createResumeApi(),
    store,
    auth,
    runtime,
  });
  return createResumeEditorActions({
    resumeId,
    store,
    coordinator,
    auth,
    runtime,
  });
}
```

- [ ] **Step 8: Rerun the action-boundary test GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [ ] **Step 9: Write the debounce/serialization RED test**

With fake timers, prove edits at 0 and 700 ms dispatch once at 1700 ms, one
resume never has two writes, two resumes may each have one, unmet dependency or
missing auth/CSRF causes no request, and an already-satisfied head drops without
I/O.

```ts
it("dispatches once one second after the last local edit", async () => {
  vi.useFakeTimers();
  coordinator.schedule(resumeId);
  await vi.advanceTimersByTimeAsync(700);
  coordinator.schedule(resumeId);
  await vi.advanceTimersByTimeAsync(999);
  expect(api.dispatch).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1);
  expect(api.dispatch).toHaveBeenCalledOnce();
  expect(api.dispatch.mock.calls[0]![0].firstDispatchAt).toBe(1700);
});
```

- [ ] **Step 10: Run the debounce/serialization test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/coordinator.test.ts)
```

Expected RED: FAIL because coordinator scheduling is absent.

- [ ] **Step 11: Implement the minimal drain loop**

Each resume owns one timer and one drain promise:

```ts
for (;;) {
  const record = store.recordFor(id);
  const head = record ? admissibleHead(record) : null;
  if (!record || !head) break;
  if (
    head.kind === "templateGroup" &&
    record.templateState?.kind === "queued"
  ) {
    const groupDecision = reconcileTemplateGroup(head, record.accepted);
    if (groupDecision.kind === "satisfied") {
      store.dropHead(id, head.id);
      continue;
    }
    if (groupDecision.kind === "conflict") {
      store.markConflict(id, groupDecision.conflict);
      break;
    }
  }
  const command =
    head.kind === "templateGroup"
      ? nextTemplateChild(head, requireRunnableTemplateState(record))
      : head;
  if (!command) break;
  const decision = reconcileCommand(command, record.accepted);
  if (decision.kind === "satisfied") continueAfterSatisfied(id, head, command);
  else if (decision.kind === "conflict") {
    store.markConflict(id, decision.conflict);
    break;
  } else {
    const csrfToken = auth.csrfToken.value;
    if (!csrfToken) break;
    const attempt = freezeAttempt(command, record.accepted, runtime);
    store.startAttempt(id, head, command, attempt);
    await settle(
      id,
      head,
      command,
      attempt,
      await api.dispatch(attempt, csrfToken),
    );
  }
}
```

The one-second timer resets only for local edits. Complete-read drains happen
after the queue empties.

- [ ] **Step 12: Rerun the debounce/serialization test GREEN**

Run the Step 10 command. Expected GREEN: PASS.

- [ ] **Step 13: Write the retry/cutoff/create/photo RED test**

Prove one same-key CSRF retry with only token changed; one automatic exact
replay for transport/`5xx`; explicit exact retry before cutoff; no loop or
cutoff crossing; read-based resolution; create and opaque upload close only on
same-key success; and idempotency reuse stops. After cutoff, create becomes
`OpaqueCreateOutcome` with refreshed list plus Refresh and Abandon; upload
becomes `OpaquePhotoOutcome` with Keep observed and Replace. Later commands
remain blocked until an explicit decision.

```ts
it("returns an explicit opaque create after cutoff", async () => {
  runtime.nowEpochMs
    .mockReturnValueOnce(0)
    .mockReturnValue(23 * 60 * 60 * 1000);
  api.dispatch.mockResolvedValue({ kind: "unknown", reason: "transport" });
  api.list.mockResolvedValue({ kind: "ready", items: summaries });
  await expect(coordinator.createResume(createIntent)).resolves.toMatchObject({
    kind: "opaque-create",
    outcome: {
      kind: "create-cutoff",
      intent: createIntent,
      refreshedItems: summaries,
    },
  });
  expect(api.dispatch).toHaveBeenCalledOnce();
});
it("keeps later work blocked until opaque upload replacement", async () => {
  store.setOpaquePhotoOutcome(resumeId, {
    kind: "photo-cutoff",
    command: oldUpload,
    attempt: expiredAttempt,
    observed: "changed",
  });
  await coordinator.resolveOpaquePhoto(resumeId, oldUpload.id, {
    kind: "replace",
    file: newFile,
  });
  const replacement = store.recordFor(resumeId)!
    .pending[0] as AtomicEditorCommand;
  expect(replacement.id).not.toBe(oldUpload.id);
  await coordinator.flush(resumeId);
  expect(store.recordFor(resumeId)!.attempt!.attempt.idempotencyKey).not.toBe(
    expiredAttempt.idempotencyKey,
  );
});
it("keeps the observed winner and drops only the opaque upload", async () => {
  const winnerBefore = store.recordFor(resumeId)!.accepted;
  store.setOpaquePhotoOutcome(resumeId, {
    kind: "photo-cutoff",
    command: oldUpload,
    attempt: expiredAttempt,
    observed: "unchanged",
  });
  await coordinator.resolveOpaquePhoto(resumeId, oldUpload.id, {
    kind: "keep-observed",
  });
  expect(store.recordFor(resumeId)!.accepted).toEqual(winnerBefore);
  expect(store.recordFor(resumeId)!.opaquePhotoOutcome).toBeNull();
  expect(store.recordFor(resumeId)!.pending).not.toContainEqual(oldUpload);
});
```

- [ ] **Step 14: Run the retry/cutoff/create/photo test RED**

Run the Step 10 command. Expected RED: FAIL on the first missing retry
transition.

- [ ] **Step 15: Implement minimal frozen retry transitions**

`settle` switches exhaustively on Task 03 `AttemptResult`. Same-attempt retry
copies only the local `automaticReplays` counter and calls `api.dispatch` again;
Task 03 rebuilds the request from unchanged frozen wire fields. Use this closed
transition:

```ts
switch (result.kind) {
  case "complete":
    return queueItem.kind === "templateGroup"
      ? settleTemplateComplete(id, queueItem, result.accepted)
      : settleAtomicComplete(id, command, result.accepted);
  case "child-ack":
    return store.acknowledgeChild(id, queueItem.id, result.etag);
  case "resume-deleted":
    return store.acknowledgeResumeDelete(id, queueItem.id);
  case "stale":
    return settleStale(id, queueItem, command, attempt, result.winner);
  case "csrf-rejected":
    return refreshThenRetrySameAttemptOnce(id, attempt);
  case "session-lost":
    return stopForSessionLoss(id, attempt);
  case "validation-rejected":
    return stopWithIssues(id, attempt, result.issues);
  case "rate-limited":
    return holdForExplicitRetry(id, attempt, result);
  case "media-busy":
    return holdForExplicitRetry(id, attempt, result);
  case "idempotency-reuse":
    return stopAndReadWinner(id, attempt);
  case "rejected":
    return stopWithCode(id, attempt, result.code);
  case "unknown":
    return resolveUnknownWithSameKey(id, queueItem, command, attempt, result);
  default:
    return assertNever(result);
}
```

`resolveUnknownWithSameKey` performs at most one bounded automatic replay while
`runtime.nowEpochMs() < retryCutoff`. Before cutoff an explicit Retry calls the
same descriptor. At/after cutoff it never reuses the old key. It transitions by
operation:

```ts
if (attempt.operation === "createResume") {
  const refreshedItems = await listOrNull(api);
  const outcome: OpaqueCreateOutcome = {
    kind: "create-cutoff",
    intent: retainedIntent,
    attempt,
    refreshedItems,
  };
  opaqueCreates.set(retainedIntent.id, outcome);
  return { kind: "opaque-create", outcome };
}
if (command.kind === "photoUpload") {
  const observed = await classifyObservedPhoto(api, store, id, command);
  store.setOpaquePhotoOutcome(id, {
    kind: "photo-cutoff",
    command,
    attempt,
    observed,
  });
  return;
}
return resolveNonOpaqueByCompleteRead(id, command, attempt);
```

`refreshOpaqueCreate` refreshes only the list; `abandonOpaqueCreate` discards
the frozen attempt so a later Create captures a new intent/key. `keep-observed`
drops the old upload intent and retains the complete-read winner. `replace`
drops the old intent, clears the opaque state, captures the chosen `File` as a
new `photoUpload`, and therefore gets a new command ID and idempotency key. A
stale safe rebase alone freezes new bytes/precondition with a new key and sets
`staleRebases: 1`.

```ts
const opaqueCreates = new Map<string, OpaqueCreateOutcome>();
async function refreshOpaqueCreate(
  intentId: string,
): Promise<OpaqueCreateOutcome> {
  const prior = opaqueCreates.get(intentId);
  if (!prior) throw new Error("opaque create not found");
  const listed = await api.list();
  const refreshed = {
    ...prior,
    refreshedItems: listed.kind === "ready" ? listed.items : null,
  };
  opaqueCreates.set(intentId, refreshed);
  return refreshed;
}
function abandonOpaqueCreate(intentId: string): void {
  opaqueCreates.delete(intentId);
}
async function resolveOpaquePhoto(
  resumeId: string,
  commandId: string,
  decision: OpaquePhotoDecision,
): Promise<void> {
  const record = store.recordFor(resumeId);
  if (!record) return;
  const opaque = record.opaquePhotoOutcome;
  if (!opaque || opaque.command.id !== commandId) return;
  store.dropHead(resumeId, commandId);
  store.setOpaquePhotoOutcome(resumeId, null);
  if (decision.kind === "keep-observed") return;
  const replacement = captureCommand(
    record.current,
    {
      resumeId,
      ownerId: opaque.command.ownerId,
      sequence: nextSequence(record),
      dependencyIds: dependencyIdsForNewCommand(record),
      intent: { kind: "photoUpload", file: decision.file },
    },
    runtime,
  );
  store.enqueue(resumeId, replacement);
  schedule(resumeId);
}
```

- [ ] **Step 16: Rerun the retry/cutoff/create/photo test GREEN**

Run the Step 14 command. Expected GREEN: PASS.

- [ ] **Step 17: Write the stale/older-success/session RED test**

Cover winner adoption, metadata reread, intended/base/conflict order, one safe
rebase, second `412` stop, older-details reread, Accept latest, guarded Apply
mine, dedicated actions, template child progression/partial/undo, session loss
with retained work, reauthentication, resume, and explicit discard.

```ts
it("reconciles an older stored success without another write", async () => {
  store.adoptComplete.mockReturnValue({ kind: "older", winner });
  reconcileCommand.mockReturnValue({
    kind: "conflict",
    conflict: baseConflict,
  });
  await settleAtomicComplete(resumeId, command, successful);
  expect(createSupersededConflict).toHaveBeenCalledWith(
    command,
    successful,
    winner,
  );
  expect(store.markConflict).toHaveBeenCalledWith(
    resumeId,
    expect.objectContaining({ kind: "superseded-after-success" }),
  );
  expect(store.dropHead).toHaveBeenCalledWith(resumeId, command.id);
  expect(api.dispatch).not.toHaveBeenCalled();
});
it("retains queue and attempt while the session is lost", async () => {
  await settleResult({ kind: "session-lost" });
  expect(store.recordFor(resumeId)).toMatchObject({
    sessionLost: true,
    pending: [command],
  });
  expect(store.recordFor(resumeId)!.attempt).not.toBeNull();
});
```

- [ ] **Step 18: Run the stale/older-success/session test RED**

Run the Step 10 command. Expected RED: FAIL on the first missing state.

- [ ] **Step 19: Implement minimal reconciliation/session transitions**

On session loss, mark every affected record and cancel timers but keep queue and
attempt descriptor. Reconcile stale details as follows:

```ts
if (compareRevision(winner.revision, record.accepted.revision) <= 0) {
  return readCompleteWinner();
}
if (command.kind === "metadataField" || command.kind === "resumeDelete") {
  return readCompleteWinner();
}
const snapshot: AcceptedResume = {
  document: winner.document,
  revision: winner.revision,
  metadata: record.accepted.metadata,
  metadataFreshness: "stale",
};
store.adoptStaleWinner(id, snapshot);
const decision = reconcileCommand(command, snapshot);
if (decision.kind === "satisfied") {
  return continueAfterSatisfied(id, queueItem, command);
}
if (decision.kind === "conflict")
  return store.markConflict(id, decision.conflict);
if (attempt.staleRebases === 1) return stopWithSecondStale(id, attempt);
return dispatchRebasedAttemptWithNewKey(id, queueItem, command, snapshot);
```

`resumeAfterAuth` first awaits `/me`, then exhaustively switches on
`ResumeReadResult`: only `complete` reconciles/restarts; unavailable,
rate-limited, failed, or renewed session loss keeps the head blocked. Template
children call Task 02 state transitions; the coordinator does not duplicate
them. If refreshed `user.id` differs from the `ownerId` captured by a command,
group, or retained create, no read or write resumes and retained work requires
explicit discard. `settleAtomicComplete` handles the store result exactly:

```ts
const adoption = store.adoptComplete(id, successful);
if (adoption.kind === "adopted") return store.dropHead(id, command.id);
const decision = reconcileCommand(command, adoption.winner);
if (decision.kind === "satisfied") return store.dropHead(id, command.id);
store.markConflict(
  id,
  createSupersededConflict(command, successful, adoption.winner),
);
return store.dropHead(id, command.id);
```

An older result never receives a new attempt: its stored success proves the
attempt completed, and a winner no longer equal to intent is visibly superseded.
`settleTemplateComplete` also checks `CompletionAdoption`; an older result
reconciles against the current winner and records template partial reason
`superseded-after-success` unless the full group is already satisfied. An
adopted template response calls `advanceTemplateGroup`, drops the group only
when complete, and keeps running/partial visible. A template child satisfied
from `412` details schedules a complete owner read; only that read may create
saved/undo state.

- [ ] **Step 20: Rerun the stale/older-success/session test GREEN**

Run the Step 18 command. Expected GREEN: PASS.

- [ ] **Step 21: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/coordinator.test.ts \
  test/useAuth.test.ts test/useAuth-csrf-rotation.test.ts)
(cd apps/web && npx eslint app/editor/coordinator.ts \
  app/composables/{useResumeEditor,useAuth}.ts \
  test/editor/coordinator.test.ts test/useAuth.test.ts \
  test/useAuth-csrf-rotation.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): coordinate safe autosave and sessions`.
