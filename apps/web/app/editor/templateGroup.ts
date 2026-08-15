import type { TemplatePreset } from '@aboutme/schema/templates';

import { applyTemplate } from '../components/resume/applyTemplate';
import {
  captureCommand,
  equalProjection,
  replayCommand,
} from './commands';
import { compareRevision } from './revision';
import { diffCustomization, diffPlacement } from './templateDiff';
import type { AtomicCommandIntent, AtomicEditorCommand } from './commands';
import type {
  AcceptedResume,
  EditorRuntime,
  PlacementProjection,
  Projection,
  Revision,
  ResumeSnapshot,
  TemplateCustomizationProjection,
} from './types';

export type { EditorRuntime } from './types';

export type TemplateChildCommand = Extract<
  AtomicEditorCommand,
  { kind: 'structure' | 'customization' }
>;

export interface TemplateGroupCommand {
  readonly kind: 'templateGroup';
  readonly id: string;
  readonly resumeId: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly base: Projection;
  readonly intended: Projection;
  readonly contentContext: Projection['context'];
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
  readonly contentContext: Projection['context'];
}

export interface TemplateUndoInput {
  readonly undo: TemplateUndo;
  readonly current: AcceptedResume;
  readonly ownerId: string;
  readonly sequence: number;
  readonly dependencyIds: readonly string[];
  readonly runtime: EditorRuntime;
}

export type EditorQueueItem = AtomicEditorCommand | TemplateGroupCommand;

export type TemplateGroupState
  = | { readonly kind: 'queued'; readonly nextChild: 0 | 1 }
    | {
      readonly kind: 'running';
      readonly nextChild: 0 | 1;
      readonly lastRevision: Revision;
    }
    | {
      readonly kind: 'complete';
      readonly finalRevision: Revision;
      readonly undo: TemplateUndo;
    }
    | {
      readonly kind: 'partial';
      readonly accepted: AcceptedResume;
      readonly nextChild: 0 | 1;
      readonly reason:
        | 'child-failed'
        | 'canonicalized'
        | 'remote-change'
        | 'superseded-after-success'
        | 'context-change'
        | 'unknown-outcome';
    };

export type TemplateRecovery
  = | { readonly kind: 'enqueue'; readonly group: TemplateGroupCommand }
    | { readonly kind: 'keep-partial' }
    | {
      readonly kind: 'unavailable';
      readonly reason: 'state-changed' | 'context-changed' | 'read-required';
    };

export function captureTemplateGroup(
  input: TemplateGroupInput,
): TemplateGroupCommand | null {
  const intendedCustomization = applyTemplate(
    input.current.document.customization,
    input.preset,
    input.current.document.content,
  );
  const intendedFinal: ResumeSnapshot = {
    ...input.current,
    document: {
      ...input.current.document,
      customization: intendedCustomization,
    },
  };
  return createGroup(
    input.current,
    intendedFinal,
    input.resumeId,
    input.ownerId,
    input.sequence,
    input.dependencyIds,
    input.runtime,
  );
}

export function captureTemplateUndo(
  input: TemplateUndoInput,
): Extract<TemplateRecovery, { kind: 'enqueue' | 'unavailable' }> {
  if (
    !equalProjection(
      projectTemplateTarget(input.current),
      input.undo.finalTarget,
    )
    || !contextMatchesUndo(input.undo, input.current)
  ) {
    return { kind: 'unavailable', reason: 'state-changed' };
  }
  const intendedFinal = snapshotForTarget(
    input.current,
    input.undo.preApplyTarget,
  );
  if (intendedFinal === null) {
    return { kind: 'unavailable', reason: 'state-changed' };
  }
  const group = createGroup(
    input.current,
    intendedFinal,
    input.current.metadata.id,
    input.ownerId,
    input.sequence,
    input.dependencyIds,
    input.runtime,
  );
  return group === null
    ? { kind: 'unavailable', reason: 'state-changed' }
    : { kind: 'enqueue', group };
}

function createGroup(
  preApply: ResumeSnapshot,
  intendedFinal: ResumeSnapshot,
  resumeId: string,
  ownerId: string,
  sequence: number,
  dependencyIds: readonly string[],
  runtime: EditorRuntime,
): TemplateGroupCommand | null {
  const groupId = runtime.uuid();
  const base = projectTemplateTarget(preApply);
  const intended = projectTemplateTarget(intendedFinal);
  const structureEdits = diffPlacement(
    preApply.document.customization.layout.sections,
    intendedFinal.document.customization.layout.sections,
  );
  const deltas = diffCustomization(
    preApply.document.customization,
    intendedFinal.document.customization,
  );
  const captureChild = (
    intent: AtomicCommandIntent,
    dependencyIds: readonly string[],
  ): TemplateChildCommand =>
    captureCommand(
      preApply,
      {
        resumeId,
        ownerId,
        sequence,
        dependencyIds,
        intent,
      },
      runtime,
    ) as TemplateChildCommand;
  const capturedStructure
    = structureEdits.length === 0
      ? null
      : captureChild(
          { kind: 'structure', commands: structureEdits },
          dependencyIds,
        );
  const structure = capturedStructure === null
    ? null
    : withProjections(
        capturedStructure,
        structureChildProjection(
          preApply,
          preApply.document.customization.layout.sections,
          preApply.document.customization,
        ),
        structureChildProjection(
          preApply,
          intendedFinal.document.customization.layout.sections,
          preApply.document.customization,
        ),
      );
  const capturedCustomization
    = deltas.length === 0
      ? null
      : captureChild({ kind: 'customization', deltas }, [
          ...dependencyIds,
          ...(structure === null ? [] : [structure.id]),
        ]);
  const customization = capturedCustomization === null
    ? null
    : withProjections(
        capturedCustomization,
        customizationChildProjection(
          preApply,
          preApply.document.customization,
          intendedFinal.document.customization.layout.sections,
        ),
        customizationChildProjection(
          preApply,
          intendedFinal.document.customization,
          intendedFinal.document.customization.layout.sections,
        ),
      );
  const children = [structure, customization].filter(isTemplateChild);
  if (children.length === 0) return null;
  return freezeCopy({
    kind: 'templateGroup',
    id: groupId,
    resumeId,
    ownerId,
    sequence,
    base,
    intended,
    contentContext: base.context,
    dependencyIds: [...dependencyIds],
    children,
    preApply,
    intendedFinal,
  });
}

export function advanceTemplateGroup(
  group: TemplateGroupCommand,
  state: TemplateGroupState,
  accepted: AcceptedResume,
): TemplateGroupState {
  if (state.kind === 'complete' || state.kind === 'partial') return state;
  if (!contextMatches(group, accepted)) {
    return partial(state, accepted, 'context-change');
  }
  if (
    state.kind === 'running'
    && compareRevision(accepted.revision, state.lastRevision) <= 0
  ) {
    return partial(state, accepted, 'remote-change');
  }
  if (equalProjection(projectTemplateTarget(accepted), group.intended)) {
    return complete(group, accepted.revision);
  }
  const child = nextTemplateChild(group, state);
  if (
    child === null
    || !equalProjection(projectChild(accepted, child), child.intended!)
  ) {
    return partial(state, accepted, 'canonicalized');
  }
  const nextChild = childIndex(group, child) + 1;
  if (nextChild >= group.children.length) {
    return partial(state, accepted, 'remote-change');
  }
  return {
    kind: 'running',
    nextChild: nextChild as 0 | 1,
    lastRevision: accepted.revision,
  };
}

export function nextTemplateChild(
  group: TemplateGroupCommand,
  state: Extract<TemplateGroupState, { kind: 'queued' | 'running' }>,
): TemplateChildCommand | null {
  return group.children[state.nextChild] ?? null;
}

export function recoverTemplateGroup(
  group: TemplateGroupCommand,
  state: Extract<TemplateGroupState, { kind: 'partial' }>,
  latest: AcceptedResume,
  action: 'retry-remaining' | 'restore-pre-apply' | 'keep-partial',
  runtime: EditorRuntime,
): TemplateRecovery {
  if (action === 'keep-partial') return { kind: 'keep-partial' };
  if (!contextMatches(group, latest)) {
    return { kind: 'unavailable', reason: 'context-changed' };
  }
  if (action === 'retry-remaining') {
    const expected = expectedIntermediate(group, state.nextChild);
    return equalProjection(
      projectTemplateTarget(latest),
      projectTemplateTarget(expected),
    )
      ? { kind: 'enqueue', group }
      : { kind: 'unavailable', reason: 'state-changed' };
  }
  if (!equalProjection(
    projectTemplateTarget(latest),
    projectTemplateTarget(expectedIntermediate(group, state.nextChild)),
  )) {
    return { kind: 'unavailable', reason: 'state-changed' };
  }
  const reverse = createGroup(
    latest,
    group.preApply,
    group.resumeId,
    group.ownerId,
    group.sequence,
    [],
    runtime,
  );
  return reverse === null
    ? { kind: 'unavailable', reason: 'state-changed' }
    : { kind: 'enqueue', group: reverse };
}

function projectTemplateTarget(snapshot: ResumeSnapshot): Projection {
  const { sections, ...layout } = snapshot.document.customization.layout;
  return {
    target: {
      present: true,
      value: {
        placement: sections,
        customization: { ...snapshot.document.customization, layout },
      },
    },
    context: contentContext(snapshot),
  };
}

function contentContext(snapshot: ResumeSnapshot): Projection['context'] {
  return {
    resumeId: { present: true, value: snapshot.metadata.id },
    schemaVersion: { present: true, value: snapshot.document.schemaVersion },
    contentIdentity: {
      present: true,
      value: Object.entries(snapshot.document.content).map(
        ([key, section]) => ({
          key,
          sectionType: section.sectionType,
        }),
      ),
    },
  };
}

function contextMatches(
  group: TemplateGroupCommand,
  snapshot: ResumeSnapshot,
): boolean {
  return equalProjection(
    {
      target: { present: true, value: null },
      context: contentContext(snapshot),
    },
    { target: { present: true, value: null }, context: group.contentContext },
  );
}

export function templateUndoAvailable(
  undo: TemplateUndo,
  current: ResumeSnapshot,
): boolean {
  return equalProjection(projectTemplateTarget(current), undo.finalTarget)
    && contextMatchesUndo(undo, current);
}

function contextMatchesUndo(
  undo: TemplateUndo,
  current: ResumeSnapshot,
): boolean {
  return equalProjection(
    {
      target: { present: true, value: null },
      context: contentContext(current),
    },
    {
      target: { present: true, value: null },
      context: undo.contentContext,
    },
  );
}

function snapshotForTarget(
  current: ResumeSnapshot,
  target: Projection,
): ResumeSnapshot | null {
  if (!target.target.present || target.target.value === null) return null;
  const value = target.target.value;
  if (
    typeof value !== 'object'
    || !('placement' in value)
    || !('customization' in value)
  ) return null;
  const { placement, customization } = value as {
    placement: PlacementProjection;
    customization: TemplateCustomizationProjection;
  };
  return {
    ...current,
    document: {
      ...current.document,
      customization: {
        ...customization,
        layout: {
          ...customization.layout,
          sections: {
            main: [...placement.main],
            sidebar: [...placement.sidebar],
          },
        },
      },
    },
  };
}

function freezeCopy<T>(value: T): T {
  const copied = structuredClone(value);
  return freeze(copied);
}

function freeze<T>(value: T): T {
  if (value !== null && typeof value === 'object') {
    for (const nested of Object.values(value as object)) freeze(nested);
    Object.freeze(value);
  }
  return value;
}

function projectChild(
  snapshot: ResumeSnapshot,
  child: TemplateChildCommand,
): Projection {
  if (child.kind === 'structure') {
    return structureChildProjection(
      snapshot,
      snapshot.document.customization.layout.sections,
      snapshot.document.customization,
    );
  }
  return customizationChildProjection(
    snapshot,
    snapshot.document.customization,
    snapshot.document.customization.layout.sections,
  );
}

function structureChildProjection(
  snapshot: ResumeSnapshot,
  placement: PlacementProjection,
  customization: ResumeSnapshot['document']['customization'],
): Projection {
  return {
    target: { present: true, value: structuredClone(placement) },
    context: {
      ...contentContext(snapshot),
      customization: {
        present: true,
        value: templateCustomization(customization),
      },
    },
  };
}

function customizationChildProjection(
  snapshot: ResumeSnapshot,
  customization: ResumeSnapshot['document']['customization'],
  placement: PlacementProjection,
): Projection {
  return {
    target: { present: true, value: templateCustomization(customization) },
    context: {
      ...contentContext(snapshot),
      placement: { present: true, value: structuredClone(placement) },
    },
  };
}

function templateCustomization(
  customization: ResumeSnapshot['document']['customization'],
): TemplateCustomizationProjection {
  const { sections: _sections, ...layout } = customization.layout;
  return { ...customization, layout };
}

function withProjections(
  command: TemplateChildCommand,
  base: Projection,
  intended: Projection,
): TemplateChildCommand {
  return { ...command, base, intended } as TemplateChildCommand;
}

function expectedIntermediate(
  group: TemplateGroupCommand,
  nextChild: number,
): ResumeSnapshot {
  return group.children
    .slice(0, nextChild)
    .reduce(replayCommand, group.preApply);
}

function complete(
  group: TemplateGroupCommand,
  finalRevision: Revision,
): TemplateGroupState {
  return {
    kind: 'complete',
    finalRevision,
    undo: {
      groupId: group.id,
      finalRevision,
      preApplyTarget: projectTemplateTarget(group.preApply),
      finalTarget: group.intended,
      contentContext: group.contentContext,
    },
  };
}

function partial(
  state: TemplateGroupState,
  accepted: AcceptedResume,
  reason: Extract<TemplateGroupState, { kind: 'partial' }>['reason'],
): TemplateGroupState {
  return {
    kind: 'partial',
    accepted,
    nextChild: state.kind === 'complete' ? 1 : state.nextChild,
    reason,
  };
}

function childIndex(
  group: TemplateGroupCommand,
  child: TemplateChildCommand,
): number {
  return group.children.findIndex((candidate) => candidate.id === child.id);
}

function isTemplateChild(
  value: TemplateChildCommand | null,
): value is TemplateChildCommand {
  return value !== null;
}
