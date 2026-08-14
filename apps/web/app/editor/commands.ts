import type { PhotoCrop, Section } from '@aboutme/schema';

import { projectIntent } from './projections';
import { applyIntent } from './reducer';
import type {
  EditorRuntime,
  JsonValue,
  Presence,
  Projection,
  ResumeSnapshot,
} from './types';

export type StructureEdit
  = | {
    op: 'createSection';
    key: string;
    sectionType: Section['sectionType'];
    column: 'main' | 'sidebar';
    index: number;
    displayName?: string;
    iconKey?: string;
  }
  | { op: 'deleteSection'; key: string }
  | {
    op: 'moveSection';
    key: string;
    column: 'main' | 'sidebar';
    index: number;
  }
  | {
    op: 'reorderColumn';
    column: 'main' | 'sidebar';
    keys: readonly string[];
  };

export type EntryFieldPath
  = | 'isHidden'
    | 'text'
    | 'jobTitle'
    | 'employer'
    | 'employerLink'
    | 'city'
    | 'country'
    | 'dates'
    | 'description'
    | 'degree'
    | 'school'
    | 'schoolLink'
    | 'name'
    | 'level'
    | 'infoHtml'
    | 'title'
    | 'titleLink'
    | 'issuer'
    | 'date'
    | 'link'
    | 'subtitle';

export type CustomizationSetPath
  = | 'font.family'
    | 'font.baseSizePx'
    | 'colors.primary'
    | 'colors.text'
    | 'colors.background'
    | 'colors.accent'
    | 'colors.surface'
    | 'spacing.sectionGap'
    | 'spacing.entryGap'
    | 'spacing.lineHeight'
    | 'spacing.pageMargin.x'
    | 'spacing.pageMargin.y'
    | 'heading.style'
    | 'heading.showRule'
    | 'header.align'
    | 'header.detailsLayout'
    | 'header.iconStyle'
    | 'layout.columns'
    | 'layout.surfaceTarget'
    | 'sectionDisplay.skill.style'
    | 'sectionDisplay.language.style'
    | 'pageFormat'
    | 'dateFormat';

export type CustomizationUnsetPath
  = | 'colors.accent'
    | 'colors.surface'
    | 'spacing.pageMargin'
    | 'header'
    | 'layout.surfaceTarget';

export type CustomizationDelta
  = | { op: 'set'; path: CustomizationSetPath; value: JsonValue }
    | { op: 'unset'; path: CustomizationUnsetPath };

interface CommandEnvelope {
  readonly id: string;
  readonly resumeId: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly targetKey: string;
  readonly base: Projection;
  readonly intended: Projection | null;
  readonly dependencyIds: readonly string[];
}

export type AtomicCommandIntent
  = | { kind: 'metadataField'; field: 'title'; value: string }
    | { kind: 'metadataField'; field: 'lng'; value: string | null }
    | {
      kind: 'personalField';
      path: 'fullName' | 'headline' | 'details';
      value: Presence;
    }
    | {
      kind: 'entryField';
      sectionKey: string;
      entryId: string;
      path: EntryFieldPath;
      value: Presence;
    }
    | {
      kind: 'entryUpsert';
      sectionKey: string;
      entry: Section['entries'][number];
    }
    | { kind: 'entryDelete'; sectionKey: string; entryId: string }
    | { kind: 'entryReorder'; sectionKey: string; entryIds: readonly string[] }
    | {
      kind: 'sectionMetadata';
      sectionKey: string;
      change:
        | { field: 'displayName'; value: string }
        | { field: 'iconKey'; value: string | null };
    }
    | { kind: 'structure'; commands: readonly StructureEdit[] }
    | { kind: 'customization'; deltas: readonly CustomizationDelta[] }
    | { kind: 'photoCrop'; crop: PhotoCrop | null }
    | { kind: 'photoUpload'; file: File }
    | { kind: 'photoDelete' }
    | { kind: 'resumeDelete'; confirmedTitle: string };

export type AtomicEditorCommand = CommandEnvelope & AtomicCommandIntent;

export interface CaptureCommandInput {
  readonly resumeId: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly dependencyIds: readonly string[];
  readonly intent: AtomicCommandIntent;
}

export interface CreateResumeIntent {
  readonly kind: 'resumeCreate';
  readonly id: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly title: string;
  readonly lng?: string | null;
}

export function captureCommand(
  snapshot: ResumeSnapshot,
  input: CaptureCommandInput,
  runtime: EditorRuntime,
): AtomicEditorCommand {
  const intent = cloneIntent(input.intent);
  const before = projectIntent(snapshot, intent);
  const next = applyIntent(snapshot, intent);
  const intended
    = intent.kind === 'photoUpload' ? null : projectIntent(next, intent);
  const baseProjection = withOwnerContext(before, intent, input.ownerId);
  const intendedProjection = intended === null
    ? null
    : withOwnerContext(intended, intent, input.ownerId);

  return deepFreeze({
    ...intent,
    id: runtime.uuid(),
    resumeId: input.resumeId,
    ownerId: input.ownerId,
    sequence: input.sequence,
    targetKey: targetKey(intent, input.sequence, input.resumeId),
    base: structuredClone(baseProjection),
    intended:
      intendedProjection === null ? null : structuredClone(intendedProjection),
    dependencyIds: [...input.dependencyIds],
  }) as AtomicEditorCommand;
}

export function replayCommand(
  snapshot: ResumeSnapshot,
  command: AtomicEditorCommand,
): ResumeSnapshot {
  return applyIntent(snapshot, command);
}

export { applyIntent } from './reducer';
export { coalescePending } from './coalesce';
export { equalProjection, projectCommand, projectIntent } from './projections';

export function targetKey(
  intent: AtomicCommandIntent,
  sequence: number,
  resumeId?: string,
): string {
  switch (intent.kind) {
    case 'metadataField':
      return `metadata:${intent.field}`;
    case 'personalField':
      return `personal:${intent.path}`;
    case 'entryField':
      return `entry:${intent.sectionKey}:${intent.entryId}:${intent.path}`;
    case 'entryUpsert':
      return `entry:${intent.sectionKey}:${intent.entry.id}`;
    case 'entryDelete':
      return `entry:${intent.sectionKey}:${intent.entryId}`;
    case 'entryReorder':
      return `section:${intent.sectionKey}:entryOrder`;
    case 'sectionMetadata':
      return `section:${intent.sectionKey}:${intent.change.field}`;
    case 'structure':
      return `structure:${sequence}`;
    case 'customization': {
      if (intent.deltas.length !== 1) {
        return `customization:batch:${sequence}`;
      }
      return `customization:${intent.deltas[0]!.path}`;
    }
    case 'photoCrop':
      return 'photo:crop';
    case 'photoUpload':
    case 'photoDelete':
      return 'photo:object';
    case 'resumeDelete':
      return `resume:${resumeId ?? sequence}`;
    default:
      return assertNever(intent);
  }
}

function cloneIntent(intent: AtomicCommandIntent): AtomicCommandIntent {
  switch (intent.kind) {
    case 'structure':
      return {
        ...intent,
        commands: intent.commands.map((command) => ({ ...command })),
      };
    case 'customization':
      return {
        ...intent,
        deltas: intent.deltas.map((delta) => ({ ...delta })),
      };
    case 'entryReorder':
      return { ...intent, entryIds: [...intent.entryIds] };
    case 'entryUpsert':
      return { ...intent, entry: structuredClone(intent.entry) };
    case 'personalField':
    case 'entryField':
      return {
        ...intent,
        value: intent.value.present
          ? { present: true, value: structuredClone(intent.value.value) }
          : { present: false },
      } as AtomicCommandIntent;
    default:
      return { ...intent };
  }
}

function withOwnerContext(
  projection: Projection,
  intent: AtomicCommandIntent,
  ownerId: string,
): Projection {
  if (intent.kind !== 'resumeDelete') return projection;
  return {
    ...projection,
    context: {
      ...projection.context,
      ownerId: { present: true, value: ownerId },
    },
  };
}

function deepFreeze<T>(value: T): T {
  if (Array.isArray(value)) {
    value.forEach(deepFreeze);
    return Object.freeze(value) as T;
  }
  if (value !== null && typeof value === 'object' && isPlainObject(value)) {
    Object.values(value).forEach(deepFreeze);
    return Object.freeze(value) as T;
  }
  return value;
}

function isPlainObject(value: object): boolean {
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function assertNever(value: never): never {
  throw new Error(`unhandled command: ${String(value)}`);
}
