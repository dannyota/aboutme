# Task 04: Pinia resume store

**Owner:** One high-judgment web author.

**Authorities:** `design.md` State model, `mutation-contract.md` Command
capture/Accepted-state adoption, `template-group-contract.md` state terms, D4,
D6, D9, and D10.

**Acceptance:** AC-EDITOR-003, AC-EDITOR-007, and AC-EDITOR-016.

**Files:**

- Create: `apps/web/app/stores/resumes.ts`
- Create: `apps/web/test/editor/resume-store.test.ts`

**Interfaces:** This file is the only definition site for store record and photo
read state:

```ts
export type PhotoReadState =
  | { readonly kind: "none" }
  | {
      readonly kind: "loading";
      readonly binding: string;
      readonly generation: number;
    }
  | {
      readonly kind: "ready";
      readonly binding: string;
      readonly generation: number;
      readonly etag: ObjectETag;
      readonly dataUrl: string;
    }
  | {
      readonly kind: "suspended";
      readonly binding: string;
      readonly generation: number;
      readonly reason: "read-failed" | "binding-mismatch" | "session-lost";
    };
export type AttemptState =
  | {
      readonly kind: "dispatching";
      readonly queueItem: EditorQueueItem;
      readonly command: AtomicEditorCommand;
      readonly attempt: FrozenAttempt;
    }
  | {
      readonly kind: "unknown";
      readonly queueItem: EditorQueueItem;
      readonly command: AtomicEditorCommand;
      readonly attempt: FrozenAttempt;
      readonly reason: "transport" | "server" | "cutoff";
    }
  | {
      readonly kind: "retry-later";
      readonly queueItem: EditorQueueItem;
      readonly command: AtomicEditorCommand;
      readonly attempt: FrozenAttempt;
      readonly reason: "rate-limited" | "media-busy";
      readonly retryAfterMs: number | null;
    }
  | {
      readonly kind: "failed";
      readonly queueItem: EditorQueueItem;
      readonly command: AtomicEditorCommand;
      readonly attempt: FrozenAttempt;
      readonly reason:
        | AttemptFailureCode
        | "csrf-rejected"
        | "idempotency-reuse"
        | "second-stale";
    };
export type OpaquePhotoOutcome = {
  readonly kind: "photo-cutoff";
  readonly command: Extract<AtomicEditorCommand, { kind: "photoUpload" }>;
  readonly attempt: FrozenAttempt;
  readonly observed: "unchanged" | "changed" | "unavailable";
};
export type CompletionAdoption =
  | { readonly kind: "adopted"; readonly accepted: AcceptedResume }
  | { readonly kind: "older"; readonly winner: AcceptedResume };
export interface ResumeRecord {
  accepted: AcceptedResume;
  current: ResumeSnapshot;
  pending: readonly EditorQueueItem[];
  attempt: AttemptState | null;
  conflicts: readonly ConflictRecord[];
  issues: Readonly<Record<string, readonly ServerValidationIssue[]>>;
  templateState: TemplateGroupState | null;
  photoRead: PhotoReadState;
  completeReadRequired: boolean;
  sessionLost: boolean;
  opaquePhotoOutcome: OpaquePhotoOutcome | null;
}
export interface ResumeStoreState {
  records: Map<string, ResumeRecord>;
}
export interface ResumeStoreActions {
  initialize(accepted: AcceptedResume): void;
  enqueue(resumeId: string, item: EditorQueueItem): void;
  startAttempt(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
    attempt: FrozenAttempt,
  ): void;
  holdAttempt(
    resumeId: string,
    state: Exclude<AttemptState, { kind: "dispatching" }>,
  ): void;
  adoptComplete(resumeId: string, accepted: AcceptedResume): CompletionAdoption;
  adoptStaleWinner(resumeId: string, accepted: AcceptedResume): void;
  acknowledgeChild(resumeId: string, itemId: string, etag: ParentETag): void;
  acknowledgeResumeDelete(resumeId: string, itemId: string): void;
  replaceHead(resumeId: string, item: EditorQueueItem): void;
  dropHead(resumeId: string, itemId: string): void;
  markConflict(resumeId: string, conflict: ConflictRecord): void;
  setIssues(
    resumeId: string,
    itemId: string,
    issues: readonly ServerValidationIssue[],
  ): void;
  setTemplateState(resumeId: string, state: TemplateGroupState | null): void;
  setPhotoRead(resumeId: string, state: PhotoReadState): void;
  markSessionLost(resumeId: string): void;
  clearSessionLost(resumeId: string): void;
  setOpaquePhotoOutcome(
    resumeId: string,
    state: OpaquePhotoOutcome | null,
  ): void;
  discardLocal(resumeId: string): void;
  removeResume(resumeId: string): void;
}
export function nextSequence(record: ResumeRecord): number;
export function dependencyIdsForNewCommand(
  record: ResumeRecord,
): readonly string[];
```

`useResumeStore` is the inferred result of
`defineStore("resumes", { state, getters, actions })`. Its getters are
`recordFor(resumeId): ResumeRecord | undefined` and
`saveStateFor(resumeId): SaveState`. Only the actions above mutate records.

- [ ] **Step 1: Write the state/replay RED test**

Use a fresh Pinia per case. Prove accepted/current separation, sequence replay,
adjacent coalescing, dependency retention, metadata freshness, issue indexing,
conflict/template/session flags, independent resume records, and derived save
labels. A complete read reconciles retained commands instead of clearing them.

```ts
const titleCommand = (
  accepted: AcceptedResume,
  value: string,
): AtomicEditorCommand =>
  captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: "owner-1",
      sequence: 1,
      dependencyIds: [],
      intent: { kind: "metadataField", field: "title", value },
    },
    { nowEpochMs: () => 0, uuid: () => "command-1", delay: async () => {} },
  );
it("keeps accepted separate and derives current by queue replay", () => {
  setActivePinia(createPinia());
  const store = useResumeStore();
  const accepted = acceptedFixture();
  const command = titleCommand(accepted, "Optimistic");
  store.initialize(accepted);
  store.enqueue(accepted.metadata.id, command);
  const record = store.recordFor(accepted.metadata.id)!;
  expect(record.accepted.metadata.title).toBe(accepted.metadata.title);
  expect(record.current.metadata.title).toBe("Optimistic");
  expect(record.pending).toEqual([command]);
});
```

- [ ] **Step 2: Run the state/replay test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/resume-store.test.ts)
```

Expected RED: FAIL because `useResumeStore` does not exist.

- [ ] **Step 3: Implement minimal initialization/replay**

Keep one `Map<string, ResumeRecord>`. After every accepted or queue transition,
derive current rather than mutating it in place:

```ts
const active = record.attempt ? [record.attempt.queueItem] : [];
record.current = [...active, ...record.pending].reduce(
  (snapshot, item) => replayQueueItem(snapshot, item),
  record.accepted,
);
```

`replayQueueItem` applies atomic commands and a template group's captured final
snapshot. `saved` is impossible while any queue, in-flight, conflict, partial,
unknown, or session-loss state remains.

- [ ] **Step 4: Rerun the state/replay test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the adoption/teardown RED test**

Prove complete response replacement without implicit queue removal, stale-winner
document/revision adoption, monotonic revision, and older idempotent replay
closure. An older complete response returns
`{ kind: "older", winner: currentAccepted }` without mutating accepted/current
or removing the command. Cover entry/photo child `204`, stale metadata,
drain-time read, whole-resume `204`, photo cleanup, session retention, discard,
and removal. Reject a child acknowledgement for the wrong head or non-increasing
tag.

```ts
const acceptedAt = (revision: string) =>
  acceptedFixture({ revision: parseRevision(revision) });
const initializedStore = (accepted: AcceptedResume) => {
  setActivePinia(createPinia());
  const store = useResumeStore();
  store.initialize(accepted);
  return store;
};
it("returns older without mutating or removing the successful command", () => {
  const store = initializedStore(acceptedAt("5"));
  const command = titleCommand(
    store.recordFor(resumeId)!.accepted,
    "Saved earlier",
  );
  store.enqueue(resumeId, command);
  const before = structuredClone(store.recordFor(resumeId));
  expect(store.adoptComplete(resumeId, acceptedAt("4"))).toEqual({
    kind: "older",
    winner: before!.accepted,
  });
  expect(store.recordFor(resumeId)).toEqual(before);
  expect(store.recordFor(resumeId)!.pending).toContainEqual(command);
});
it("adopts newer state but leaves dropHead explicit", () => {
  const store = initializedStore(acceptedAt("5"));
  const command = titleCommand(store.recordFor(resumeId)!.accepted, "Saved");
  store.enqueue(resumeId, command);
  expect(store.adoptComplete(resumeId, acceptedAt("6")).kind).toBe("adopted");
  expect(store.recordFor(resumeId)!.pending).toContainEqual(command);
});
```

- [ ] **Step 6: Run the adoption/teardown test RED**

Run the Step 2 command. Expected RED: FAIL on the first missing adoption
transition.

- [ ] **Step 7: Implement minimal adoption/teardown**

For child acknowledgement, apply only the acknowledged reducer to accepted
document, adopt the tag revision, preserve summary fields, set metadata stale,
and set `completeReadRequired`. Whole-resume acknowledgement removes the record
only after the coordinator declares the outcome definitive. Revoke photo data on
key change, deletion, or removal. `adoptStaleWinner` preserves complete
metadata, marks it stale, and adopts the validated document/revision only when
`compareRevision` increases. Queue removal is always a separate `dropHead`
transition so a template group can adopt a child response and remain queued. Run
the comparison before any mutation:

```ts
if (compareRevision(accepted.revision, record.accepted.revision) <= 0)
  return { kind: "older", winner: record.accepted };
record.accepted = accepted;
replay(record);
return { kind: "adopted", accepted };
```

- [ ] **Step 8: Rerun the adoption/teardown test GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [ ] **Step 9: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/resume-store.test.ts)
(cd apps/web && npx eslint app/stores/resumes.ts \
  test/editor/resume-store.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add optimistic resume store`.
