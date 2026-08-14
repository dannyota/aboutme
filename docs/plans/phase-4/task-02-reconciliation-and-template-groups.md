# Task 02: Reconciliation and template-group engine

**Owner:** One high-judgment web author.

**Authorities:** `mutation-contract.md` Unknown transport outcomes through
Conflict actions, all of `template-group-contract.md`, `editor-contract.md`
Template application and Conflict controls, ADR 0008, and D6/D9/D14.

**Acceptance:** AC-EDITOR-006 and AC-EDITOR-012.

**Files:**

- Create: `apps/web/app/editor/reconcile.ts`
- Create: `apps/web/app/editor/conflicts.ts`
- Create: `apps/web/app/editor/templateDiff.ts`
- Create: `apps/web/app/editor/templateGroup.ts`
- Create: `apps/web/test/editor/reconcile.test.ts`
- Create: `apps/web/test/editor/conflicts.test.ts`
- Create: `apps/web/test/editor/template-diff.test.ts`
- Create: `apps/web/test/editor/template-group.test.ts`

**Interfaces:** These modules are the only definition site for conflict and
template queue types:

```ts
export type ConflictKind =
  | "target-changed"
  | "context-changed"
  | "identity-missing"
  | "identity-retyped"
  | "membership-changed"
  | "photo-changed"
  | "superseded-after-success"
  | "destructive-reconfirmation";
export type ConflictRecord =
  | {
      readonly id: string;
      readonly subject: "atomic";
      readonly command: AtomicEditorCommand;
      readonly kind: ConflictKind;
      readonly latest: ResumeSnapshot;
      readonly latestProjection: Projection;
    }
  | {
      readonly id: string;
      readonly subject: "template";
      readonly group: TemplateGroupCommand;
      readonly kind: "target-changed" | "context-changed";
      readonly latest: ResumeSnapshot;
      readonly latestProjection: Projection;
    };
export type ReconcileDecision =
  | { readonly kind: "satisfied" }
  | { readonly kind: "safe-base" }
  | { readonly kind: "conflict"; readonly conflict: ConflictRecord };
export type ConflictConfirmation =
  | { readonly kind: "field" }
  | { readonly kind: "recreate"; readonly newId: string }
  | { readonly kind: "reorder"; readonly members: readonly string[] }
  | { readonly kind: "photo"; readonly photoKey: string }
  | { readonly kind: "destructive"; readonly latestTitle: string };
export function reconcileCommand(
  command: AtomicEditorCommand,
  winner: ResumeSnapshot,
): ReconcileDecision;
export function createReplacementCommand(
  conflict: Extract<ConflictRecord, { subject: "atomic" }>,
  latest: ResumeSnapshot,
  confirmation: ConflictConfirmation,
): AtomicEditorCommand | null;
export function reconcileTemplateGroup(
  group: TemplateGroupCommand,
  winner: ResumeSnapshot,
): ReconcileDecision;
export function createSupersededConflict(
  command: AtomicEditorCommand,
  successful: AcceptedResume,
  winner: AcceptedResume,
): ConflictRecord;
```

```ts
export type TemplateChildCommand = Extract<
  AtomicEditorCommand,
  { kind: "structure" | "customization" }
>;
export interface TemplateGroupCommand {
  readonly kind: "templateGroup";
  readonly id: string;
  readonly resumeId: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly base: Projection;
  readonly intended: Projection;
  readonly contentContext: Projection["context"];
  readonly dependencyIds: readonly string[];
  readonly children: readonly TemplateChildCommand[];
  readonly preApply: ResumeSnapshot;
  readonly intendedFinal: ResumeSnapshot;
}
export interface TemplateGroupInput {
  readonly resumeId: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly current: ResumeSnapshot;
  readonly preset: Readonly<TemplatePreset>;
  readonly dependencyIds: readonly string[];
  readonly runtime: EditorRuntime;
}
export interface TemplateUndo {
  readonly groupId: string;
  readonly finalRevision: Revision;
  readonly preApplyTarget: Projection;
  readonly finalTarget: Projection;
  readonly contentContext: Projection["context"];
}
export type EditorQueueItem = AtomicEditorCommand | TemplateGroupCommand;
export type TemplateGroupState =
  | { readonly kind: "queued"; readonly nextChild: 0 | 1 }
  | {
      readonly kind: "running";
      readonly nextChild: 0 | 1;
      readonly lastRevision: Revision;
    }
  | {
      readonly kind: "complete";
      readonly finalRevision: Revision;
      readonly undo: TemplateUndo;
    }
  | {
      readonly kind: "partial";
      readonly accepted: AcceptedResume;
      readonly nextChild: 0 | 1;
      readonly reason:
        | "child-failed"
        | "canonicalized"
        | "remote-change"
        | "superseded-after-success"
        | "context-change"
        | "unknown-outcome";
    };
export type TemplateRecovery =
  | { readonly kind: "enqueue"; readonly group: TemplateGroupCommand }
  | { readonly kind: "keep-partial" }
  | {
      readonly kind: "unavailable";
      readonly reason: "state-changed" | "context-changed" | "read-required";
    };
export function captureTemplateGroup(
  input: TemplateGroupInput,
): TemplateGroupCommand | null;
export function advanceTemplateGroup(
  group: TemplateGroupCommand,
  state: TemplateGroupState,
  accepted: AcceptedResume,
): TemplateGroupState;
export function nextTemplateChild(
  group: TemplateGroupCommand,
  state: Extract<TemplateGroupState, { kind: "queued" | "running" }>,
): TemplateChildCommand | null;
export function recoverTemplateGroup(
  group: TemplateGroupCommand,
  state: Extract<TemplateGroupState, { kind: "partial" }>,
  latest: AcceptedResume,
  action: "retry-remaining" | "restore-pre-apply" | "keep-partial",
  runtime: EditorRuntime,
): TemplateRecovery;
```

- [ ] **Step 1: Write the reconciliation/conflict RED test**

For every atomic command kind, prove the ordered matrix: intended target and
context is satisfied; base target and context is safe; any other target/context
combination conflicts. Include field override, entry membership, new-ID
collision, entry delete/reorder, section identity, structure untouched keys,
crop/photo binding, photo replace/delete, metadata, and resume delete.

```ts
const accepted = acceptedFixture();
const runtime: EditorRuntime = {
  nowEpochMs: () => 0,
  uuid: () => "command-1",
  delay: async () => {},
};
const command = captureCommand(
  accepted,
  {
    resumeId: accepted.metadata.id,
    ownerId: "owner-1",
    sequence: 1,
    dependencyIds: [],
    intent: { kind: "metadataField", field: "title", value: "Mine" },
  },
  runtime,
);
it("uses intended then base then conflict ordering", () => {
  expect(
    reconcileCommand(command, {
      ...accepted,
      ...replayCommand(accepted, command),
    }).kind,
  ).toBe("satisfied");
  expect(reconcileCommand(command, accepted).kind).toBe("safe-base");
  const changed = {
    ...accepted,
    metadata: { ...accepted.metadata, title: "Theirs" },
  };
  expect(reconcileCommand(command, changed).kind).toBe("conflict");
});
it("never marks opaque photo upload satisfied from a read", () => {
  const upload = captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: "owner-1",
      sequence: 2,
      dependencyIds: [],
      intent: { kind: "photoUpload", file: new File(["x"], "x.png") },
    },
    runtime,
  );
  expect(reconcileCommand(upload, accepted).kind).toBe("safe-base");
});
```

- [ ] **Step 2: Run the reconciliation/conflict test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/{reconcile,conflicts}.test.ts)
```

Expected RED: FAIL because reconciliation modules do not exist.

- [ ] **Step 3: Implement the minimal shared decision**

Use Task 01 projection/equality only:

```ts
const winnerProjection = projectCommand(winner, command);
if (
  command.intended !== null &&
  equalProjection(winnerProjection, command.intended)
)
  return { kind: "satisfied" };
if (equalProjection(winnerProjection, command.base))
  return { kind: "safe-base" };
return { kind: "conflict", conflict: makeConflict(command, winner) };
```

Replacement first projects the fresh complete read. Return `null` for stale
confirmation, missing/retyped identity, changed reorder membership, missing
structure key, changed photo key, or a template partial. Destructive replacement
compares `latestTitle` exactly. A photo upload can reach `safe-base` or
`conflict`, but never `satisfied`, because only its same-key success can bind
the opaque intended photo. `reconcileTemplateGroup` compares `group.intended`,
then `group.base`, and creates a `subject: "template"` conflict for any other
target/context pair.

- [ ] **Step 4: Rerun the reconciliation/conflict test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the template diff/capture RED test**

Visit target main left-to-right then sidebar left-to-right using server
remove-then-insert indices. Sort customization leaf paths bytewise, emit
`set`/`unset`, and exclude `layout.sections`. Applying both children must equal
the pure `applyTemplate` result without changing content. Cover no change,
zero/one/two children, named dependencies, structure-first order, and adjacent
non-coalescible children.

```ts
it("injects one group ID and deterministic child IDs", () => {
  const current = acceptedFixture();
  const preset = TEMPLATES.find(
    ({ customization }) => customization.layout.placement === "byType",
  )!;
  const resumeId = current.metadata.id;
  const ownerId = "owner-1";
  const ids = ["group-1", "structure-1", "customization-1"];
  const runtime: EditorRuntime = {
    nowEpochMs: () => 0,
    uuid: () => ids.shift()!,
    delay: async () => {},
  };
  const group = captureTemplateGroup({
    resumeId,
    ownerId,
    sequence: 4,
    current,
    preset,
    dependencyIds: ["prior-1"],
    runtime,
  });
  expect(group?.id).toBe("group-1");
  expect(group?.children.map(({ id }) => id)).toEqual([
    "structure-1",
    "customization-1",
  ]);
  expect(group?.children[1]?.dependencyIds).toEqual(["prior-1", "structure-1"]);
  expect(ids).toEqual([]);
});
```

- [ ] **Step 6: Run the template diff/capture test RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/editor/{template-diff,template-group}.test.ts)
```

Expected RED: FAIL because template capture is absent.

- [ ] **Step 7: Implement minimal deterministic capture**

Compute the helper once against optimistic input. Build the structure child,
then the customization child, dropping empty children. Capture content key/type
context before preview changes. Return `null` when both diffs are empty.

```ts
const projectContentIdentity = (
  snapshot: ResumeSnapshot,
): Projection["context"] => ({
  resumeId: { present: true, value: snapshot.metadata.id },
  schemaVersion: { present: true, value: snapshot.document.schemaVersion },
  contentIdentity: {
    present: true,
    value: Object.entries(snapshot.document.content).map(([key, section]) => ({
      key,
      sectionType: section.sectionType,
    })),
  },
});
const projectTemplateTarget = (snapshot: ResumeSnapshot): Projection => {
  const { sections, ...layout } = snapshot.document.customization.layout;
  return {
    target: {
      present: true,
      value: {
        placement: sections,
        customization: { ...snapshot.document.customization, layout },
      },
    },
    context: projectContentIdentity(snapshot),
  };
};
const groupId = input.runtime.uuid();
const intendedCustomization = applyTemplate(
  input.current.document.customization,
  input.preset,
  input.current.document.content,
);
const intendedFinal: ResumeSnapshot = {
  ...input.current,
  document: { ...input.current.document, customization: intendedCustomization },
};
const base = projectTemplateTarget(input.current);
const intended = projectTemplateTarget(intendedFinal);
const contentContext = projectContentIdentity(input.current);
const structureEdits = diffPlacement(
  input.current.document.customization.layout.sections,
  intendedCustomization.layout.sections,
);
const deltas = diffCustomization(
  input.current.document.customization,
  intendedCustomization,
);
const captureChild = (
  intent: AtomicCommandIntent,
  dependencyIds: readonly string[],
) =>
  captureCommand(
    input.current,
    {
      resumeId: input.resumeId,
      ownerId: input.ownerId,
      sequence: input.sequence,
      dependencyIds,
      intent,
    },
    input.runtime,
  ) as TemplateChildCommand;
const structure = structureEdits.length
  ? captureChild(
      { kind: "structure", commands: structureEdits },
      input.dependencyIds,
    )
  : null;
const customization = deltas.length
  ? captureChild({ kind: "customization", deltas }, [
      ...input.dependencyIds,
      ...(structure ? [structure.id] : []),
    ])
  : null;
const children = [structure, customization].filter(isTemplateChild);
if (!children.length) return null;
return deepFreeze({
  kind: "templateGroup",
  id: groupId,
  resumeId: input.resumeId,
  ownerId: input.ownerId,
  sequence: input.sequence,
  base,
  intended,
  contentContext,
  dependencyIds: input.dependencyIds,
  children,
  preApply: input.current,
  intendedFinal,
});
```

- [ ] **Step 8: Rerun the template diff/capture test GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [ ] **Step 9: Write the progression/recovery/undo RED test**

Cover expected intermediate state, monotonic child revisions, early complete
finalization, canonicalized/remote-changed partial state, one-revision final
completion, and exact recovery actions. Retry requires a complete read equal to
the expected intermediate state. Restore is a guarded reverse group. Keep
partial drops remaining intent. Undo exists only for the latest complete group
while helper target and content context remain valid.

```ts
it("completes only from one accepted intended-final revision", () => {
  const current = acceptedFixture();
  const preset = TEMPLATES.find(
    ({ customization }) => customization.layout.placement === "byType",
  )!;
  let id = 0;
  const group = captureTemplateGroup({
    resumeId: current.metadata.id,
    ownerId: "owner-1",
    sequence: 1,
    current,
    preset,
    dependencyIds: [],
    runtime: {
      nowEpochMs: () => 0,
      uuid: () => `id-${++id}`,
      delay: async () => {},
    },
  })!;
  const state: TemplateGroupState = {
    kind: "running",
    nextChild: 1,
    lastRevision: parseRevision("1"),
  };
  const final: AcceptedResume = {
    ...group.intendedFinal,
    revision: parseRevision("2"),
    metadataFreshness: "complete",
  };
  expect(advanceTemplateGroup(group, state, final)).toMatchObject({
    kind: "complete",
    finalRevision: parseRevision("2"),
  });
});
```

- [ ] **Step 10: Run the progression/recovery/undo test RED**

Run the Step 6 command. Expected RED: FAIL on the first missing progression,
recovery, or undo transition.

- [ ] **Step 11: Implement the minimal state machine**

On each complete response:

```ts
if (equalsComplete(response, group.intendedFinal)) return complete(response);
if (
  !childTargetAccepted(response) ||
  !untouchedTargetStable(response) ||
  !contentContextStable(response)
)
  return partial(response);
return nextChildExists ? running(response.revision) : complete(response);
```

Never infer full completion from a child `204`. Recovery and undo call the same
target/context comparison as normal reconciliation.

- [ ] **Step 12: Rerun the progression/recovery/undo test GREEN**

Run the Step 10 command. Expected GREEN: PASS.

- [ ] **Step 13: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run \
  test/editor/{reconcile,conflicts,template-diff,template-group}.test.ts)
(cd apps/web && npx eslint \
  app/editor/{reconcile,conflicts,templateDiff,templateGroup}.ts \
  test/editor/{reconcile,conflicts,template-diff,template-group}.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add reconciliation and template groups`.
