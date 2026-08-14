import { applyIntent, projectIntent } from './commands';
import type { AtomicEditorCommand } from './commands';
import type { Projection, ResumeSnapshot } from './types';

export type ConflictKind
  = | 'target-changed'
    | 'context-changed'
    | 'identity-missing'
    | 'identity-retyped'
    | 'membership-changed'
    | 'photo-changed'
    | 'superseded-after-success'
    | 'destructive-reconfirmation';

export interface AtomicConflictRecord {
  readonly id: string;
  readonly subject: 'atomic';
  readonly command: AtomicEditorCommand;
  readonly kind: ConflictKind;
  readonly latest: ResumeSnapshot;
  readonly latestProjection: Projection;
}

export type ConflictConfirmation
  = | { readonly kind: 'field' }
    | { readonly kind: 'recreate'; readonly newId: string }
    | { readonly kind: 'reorder'; readonly members: readonly string[] }
    | { readonly kind: 'photo'; readonly photoKey: string }
    | { readonly kind: 'destructive'; readonly latestTitle: string };

export function makeAtomicConflict(
  command: AtomicEditorCommand,
  latest: ResumeSnapshot,
  latestProjection: Projection,
  kind: ConflictKind = conflictKind(command, latestProjection),
): AtomicConflictRecord {
  return {
    id: command.id,
    subject: 'atomic',
    command,
    kind,
    latest,
    latestProjection,
  };
}

export function createAtomicReplacement(
  conflict: AtomicConflictRecord,
  latest: ResumeSnapshot,
  confirmation: ConflictConfirmation,
): AtomicEditorCommand | null {
  const command = conflict.command;
  const intent = replacementIntent(command, latest, confirmation);
  if (intent === null) return null;

  const base = projectIntent(latest, intent);
  const intended = projectIntent(applyIntent(latest, intent), intent);
  return {
    ...command,
    ...intent,
    base: withOwnerContext(base, intent.kind, command.ownerId),
    intended: withOwnerContext(intended, intent.kind, command.ownerId),
  } as AtomicEditorCommand;
}

function replacementIntent(
  command: AtomicEditorCommand,
  latest: ResumeSnapshot,
  confirmation: ConflictConfirmation,
): AtomicEditorCommand | null {
  switch (command.kind) {
    case 'metadataField':
    case 'personalField':
    case 'sectionMetadata':
    case 'customization':
      return confirmation.kind === 'field' ? command : null;
    case 'entryField':
      return confirmation.kind === 'field'
        && entryContextMatches(command, latest)
        ? command
        : null;
    case 'entryUpsert': {
      if (confirmation.kind !== 'recreate') return null;
      const section = latest.document.content[command.sectionKey];
      if (
        section === undefined
        || section.sectionType !== presentValue(command.base, 'sectionType')
      ) {
        return null;
      }
      if (section.entries.some((entry) => entry.id === confirmation.newId)) {
        return null;
      }
      return {
        ...command,
        entry: { ...command.entry, id: confirmation.newId },
      };
    }
    case 'entryDelete':
      return confirmation.kind === 'destructive'
        && entryContextMatches(command, latest)
        ? command
        : null;
    case 'entryReorder': {
      if (confirmation.kind !== 'reorder') return null;
      const section = latest.document.content[command.sectionKey];
      if (
        section === undefined
        || section.sectionType !== presentValue(command.base, 'sectionType')
      ) {
        return null;
      }
      const members = section.entries.map((entry) => entry.id);
      if (!sameMembers(members, confirmation.members)) return null;
      return { ...command, entryIds: [...confirmation.members] };
    }
    case 'structure':
      return null;
    case 'photoCrop': {
      if (confirmation.kind !== 'photo') return null;
      const photoKey = latest.document.personalDetails.photo?.key;
      return photoKey === confirmation.photoKey ? command : null;
    }
    case 'photoUpload':
    case 'photoDelete':
      return null;
    case 'resumeDelete':
      return confirmation.kind === 'destructive'
        && confirmation.latestTitle === latest.metadata.title
        ? { ...command, confirmedTitle: confirmation.latestTitle }
        : null;
    default:
      return assertNever(command);
  }
}

function conflictKind(
  command: AtomicEditorCommand,
  latest: Projection,
): ConflictKind {
  if (command.kind === 'resumeDelete') return 'destructive-reconfirmation';
  if (command.kind === 'photoCrop') return 'photo-changed';
  if (command.kind === 'photoDelete') return 'destructive-reconfirmation';
  if (command.kind === 'entryReorder') return 'membership-changed';
  if (command.kind === 'entryField' || command.kind === 'entryDelete') {
    const sectionType = latest.context.sectionType;
    if (!sectionType?.present || sectionType.value === undefined) {
      return 'identity-missing';
    }
    if (sectionType.value !== presentValue(command.base, 'sectionType')) {
      return 'identity-retyped';
    }
    if (presentValue(latest, 'membership') !== command.sectionKey) {
      return 'membership-changed';
    }
  }
  return 'target-changed';
}

function entryContextMatches(
  command: Extract<AtomicEditorCommand, { kind: 'entryField' | 'entryDelete' }>,
  latest: ResumeSnapshot,
): boolean {
  const section = latest.document.content[command.sectionKey];
  return section !== undefined
    && section.sectionType === presentValue(command.base, 'sectionType')
    && section.entries.some((entry) => entry.id === command.entryId);
}

function presentValue(
  projection: Projection,
  key: keyof Projection['context'],
) {
  const value = projection.context[key];
  return value?.present ? value.value : undefined;
}

function sameMembers(
  left: readonly string[],
  right: readonly string[],
): boolean {
  return (
    left.length === right.length
    && new Set(left).size === left.length
    && new Set(right).size === right.length
    && left.every((value) => right.includes(value))
  );
}

function withOwnerContext(
  projection: Projection,
  kind: AtomicEditorCommand['kind'],
  ownerId: string,
): Projection {
  if (kind !== 'resumeDelete') return projection;
  return {
    ...projection,
    context: {
      ...projection.context,
      ownerId: { present: true, value: ownerId },
    },
  };
}

function assertNever(value: never): never {
  throw new Error(`unhandled conflict command: ${String(value)}`);
}
