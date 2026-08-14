import { equalProjection, projectCommand } from './commands';
import { createAtomicReplacement, makeAtomicConflict } from './conflicts';
import type { AtomicConflictRecord, ConflictConfirmation } from './conflicts';
import type { AtomicEditorCommand } from './commands';
import type { AcceptedResume, Projection, ResumeSnapshot } from './types';
import type { TemplateGroupCommand } from './templateGroup';

export type { ConflictConfirmation, ConflictKind } from './conflicts';

export type ConflictRecord
  = | AtomicConflictRecord
    | {
      readonly id: string;
      readonly subject: 'template';
      readonly group: TemplateGroupCommand;
      readonly kind: 'target-changed' | 'context-changed';
      readonly latest: ResumeSnapshot;
      readonly latestProjection: Projection;
    };

export type ReconcileDecision
  = | { readonly kind: 'satisfied' }
    | { readonly kind: 'safe-base' }
    | { readonly kind: 'conflict'; readonly conflict: ConflictRecord };

export function reconcileCommand(
  command: AtomicEditorCommand,
  winner: ResumeSnapshot,
): ReconcileDecision {
  const winnerProjection = withOwnerContext(
    projectCommand(winner, command),
    command,
  );
  if (
    command.intended !== null
    && equalProjection(winnerProjection, command.intended)
  ) {
    return { kind: 'satisfied' };
  }
  if (equalProjection(winnerProjection, command.base)) {
    return { kind: 'safe-base' };
  }
  return {
    kind: 'conflict',
    conflict: makeAtomicConflict(command, winner, winnerProjection),
  };
}

export function createReplacementCommand(
  conflict: Extract<ConflictRecord, { subject: 'atomic' }>,
  latest: ResumeSnapshot,
  confirmation: ConflictConfirmation,
): AtomicEditorCommand | null {
  return createAtomicReplacement(conflict, latest, confirmation);
}

export function reconcileTemplateGroup(
  group: TemplateGroupCommand,
  winner: ResumeSnapshot,
): ReconcileDecision {
  const latestProjection = projectTemplateTarget(winner);
  if (equalProjection(latestProjection, group.intended)) {
    return { kind: 'satisfied' };
  }
  if (equalProjection(latestProjection, group.base)) {
    return { kind: 'safe-base' };
  }
  return {
    kind: 'conflict',
    conflict: {
      id: group.id,
      subject: 'template',
      group,
      kind: equalContext(latestProjection.context, group.base.context)
        ? 'target-changed'
        : 'context-changed',
      latest: winner,
      latestProjection,
    },
  };
}

export function createSupersededConflict(
  command: AtomicEditorCommand,
  _successful: AcceptedResume,
  winner: AcceptedResume,
): ConflictRecord {
  return makeAtomicConflict(
    command,
    winner,
    projectCommand(winner, command),
    'superseded-after-success',
  );
}

function equalContext(
  left: Projection['context'],
  right: Projection['context'],
): boolean {
  return equalProjection(
    { target: { present: true, value: null }, context: left },
    { target: { present: true, value: null }, context: right },
  );
}

function withOwnerContext(
  projection: Projection,
  command: AtomicEditorCommand,
): Projection {
  if (command.kind !== 'resumeDelete') return projection;
  return {
    ...projection,
    context: {
      ...projection.context,
      ownerId: { present: true, value: command.ownerId },
    },
  };
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
    context: {
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
    },
  };
}
