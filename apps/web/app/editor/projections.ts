import type { Resume, Section } from '@aboutme/schema';

import type { AtomicCommandIntent, AtomicEditorCommand } from './commands';
import type {
  PlacementProjection,
  Presence,
  Projection,
  ProjectionContextKey,
  ProjectionValue,
  ResumeSnapshot,
} from './types';

export function projectIntent(
  snapshot: ResumeSnapshot,
  intent: AtomicCommandIntent,
): Projection {
  switch (intent.kind) {
    case 'metadataField':
      return commonProjection(
        presenceOf(snapshot.metadata, intent.field),
        snapshot,
      );
    case 'personalField':
      return commonProjection(
        presenceOf(snapshot.document.personalDetails, intent.path),
        snapshot,
      );
    case 'entryField':
      return entryFieldProjection(snapshot, intent);
    case 'entryUpsert':
      return entryProjection(snapshot, intent.sectionKey, intent.entry.id);
    case 'entryDelete':
      return entryProjection(snapshot, intent.sectionKey, intent.entryId);
    case 'entryReorder': {
      const section = snapshot.document.content[intent.sectionKey];
      return sectionProjection(
        presence(section?.entries.map((entry) => entry.id)),
        snapshot,
        intent.sectionKey,
      );
    }
    case 'sectionMetadata': {
      const section = snapshot.document.content[intent.sectionKey];
      return sectionProjection(
        presenceOf(section, intent.change.field),
        snapshot,
        intent.sectionKey,
      );
    }
    case 'structure':
      return structureProjection(snapshot, intent);
    case 'customization':
      return customizationProjection(snapshot, intent);
    case 'photoCrop':
      return {
        target: presenceOf(snapshot.document.personalDetails.photo, 'crop'),
        context: {
          ...commonContext(snapshot),
          photoKey: presenceOf(snapshot.document.personalDetails.photo, 'key'),
        },
      };
    case 'photoUpload':
    case 'photoDelete':
      return {
        target: presenceOf(snapshot.document.personalDetails, 'photo'),
        context: commonContext(snapshot),
      };
    case 'resumeDelete':
      return {
        target: {
          present: true,
          value: {
            document: snapshot.document,
            metadata: snapshot.metadata,
          },
        },
        context: { ownerId: { present: false } },
      };
    default:
      return assertNever(intent);
  }
}

export function projectCommand(
  snapshot: ResumeSnapshot,
  command: AtomicEditorCommand,
): Projection {
  return projectIntent(snapshot, command);
}

export function equalProjection(left: Projection, right: Projection): boolean {
  return equalValue(left, right);
}

function entryFieldProjection(
  snapshot: ResumeSnapshot,
  intent: Extract<AtomicCommandIntent, { kind: 'entryField' }>,
): Projection {
  const entry = findEntry(snapshot.document, intent.entryId);
  return sectionProjection(
    presenceOf(entry?.entry, intent.path),
    snapshot,
    intent.sectionKey,
    intent.entryId,
  );
}

function entryProjection(
  snapshot: ResumeSnapshot,
  sectionKey: string,
  entryId: string,
): Projection {
  const entry = findEntry(snapshot.document, entryId);
  return sectionProjection(
    presence(entry?.entry),
    snapshot,
    sectionKey,
    entryId,
  );
}

function sectionProjection(
  target: Presence<ProjectionValue>,
  snapshot: ResumeSnapshot,
  sectionKey: string,
  entryId?: string,
): Projection {
  const section = snapshot.document.content[sectionKey];
  return {
    target,
    context: {
      ...commonContext(snapshot),
      sectionKey: presence(sectionKey),
      sectionType: presence(section?.sectionType),
      ...(entryId === undefined
        ? {}
        : {
            entryId: presence(entryId),
            membership: presence(
              findEntry(snapshot.document, entryId)?.sectionKey,
            ),
          }),
    },
  };
}

function structureProjection(
  snapshot: ResumeSnapshot,
  intent: Extract<AtomicCommandIntent, { kind: 'structure' }>,
): Projection {
  const createdOrDeleted = new Set(
    intent.commands.flatMap((command) =>
      command.op === 'createSection' || command.op === 'deleteSection'
        ? [command.key]
        : [],
    ),
  );
  const movedOrReordered = new Set(
    intent.commands.flatMap((command) => {
      if (command.op === 'moveSection') return [command.key];
      if (command.op === 'reorderColumn') return [...command.keys];
      return [];
    }),
  );
  const placement = placementProjection(snapshot.document);
  const sections = [...createdOrDeleted].map((key) => ({
    key,
    value: presenceOf(snapshot.document.content, key),
  }));
  const context: Partial<
    Record<ProjectionContextKey, Presence<ProjectionValue>>
  > = {
    ...commonContext(snapshot),
  };
  for (const key of orderedSections(snapshot.document)) {
    if (createdOrDeleted.has(key)) continue;
    const contextKey = (movedOrReordered.has(key)
      ? `section:${key}:type`
      : `untouched:${key}:type`) as ProjectionContextKey;
    context[contextKey] = presence(snapshot.document.content[key]?.sectionType);
  }
  return {
    target: presence({ placement, sections }),
    context,
  };
}

function customizationProjection(
  snapshot: ResumeSnapshot,
  intent: Extract<AtomicCommandIntent, { kind: 'customization' }>,
): Projection {
  const target = intent.deltas.length === 1
    ? getPathPresence(snapshot.document.customization, intent.deltas[0]!.path)
    : presence(
        intent.deltas.map((delta) => ({
          path: delta.path,
          value: getPathPresence(snapshot.document.customization, delta.path),
        })),
      );
  return commonProjection(target, snapshot);
}

function commonProjection(
  target: Presence<ProjectionValue>,
  snapshot: ResumeSnapshot,
): Projection {
  return { target, context: commonContext(snapshot) };
}

function commonContext(
  snapshot: ResumeSnapshot,
): Partial<Record<ProjectionContextKey, Presence<ProjectionValue>>> {
  return {
    resumeId: presence(snapshot.metadata.id),
    schemaVersion: presence(snapshot.document.schemaVersion),
  };
}

function placementProjection(document: Resume): PlacementProjection {
  return {
    main: [...document.customization.layout.sections.main],
    sidebar: [...document.customization.layout.sections.sidebar],
  };
}

function orderedSections(document: Resume): readonly string[] {
  return [
    ...document.customization.layout.sections.main,
    ...document.customization.layout.sections.sidebar,
  ];
}

function findEntry(
  document: Resume,
  entryId: string,
):
  | {
    readonly sectionKey: string;
    readonly entry: Section['entries'][number];
  }
  | undefined {
  for (const sectionKey of orderedSections(document)) {
    const section = document.content[sectionKey];
    if (section === undefined) continue;
    const entry = section.entries.find((candidate) => candidate.id === entryId);
    if (entry !== undefined) return { sectionKey, entry };
  }
  return undefined;
}

function getPathPresence(
  value: object,
  path: string,
): Presence<ProjectionValue> {
  let current: unknown = value;
  for (const part of path.split('.')) {
    if (
      current === null
      || typeof current !== 'object'
      || !hasOwn(current, part)
    ) {
      return { present: false };
    }
    current = current[part as keyof typeof current];
  }
  return presence(current);
}

function presenceOf(
  value: object | undefined,
  key: string,
): Presence<ProjectionValue> {
  if (value === undefined || !hasOwn(value, key)) return { present: false };
  return presence(value[key as keyof typeof value]);
}

function presence(value: unknown): Presence<ProjectionValue> {
  if (value === undefined) {
    return { present: true, value: value as unknown as ProjectionValue };
  }
  return { present: true, value: value as ProjectionValue };
}

function hasOwn(value: object, key: PropertyKey): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function equalValue(left: unknown, right: unknown): boolean {
  if (Object.is(left, right)) return true;
  if (
    left === null
    || right === null
    || typeof left !== 'object'
    || typeof right !== 'object'
  ) {
    return false;
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    if (
      !Array.isArray(left)
      || !Array.isArray(right)
      || left.length !== right.length
    ) {
      return false;
    }
    return left.every((value, index) => equalValue(value, right[index]));
  }
  const leftKeys = Reflect.ownKeys(left);
  const rightKeys = Reflect.ownKeys(right);
  if (leftKeys.length !== rightKeys.length) return false;
  return leftKeys.every(
    (key) =>
      hasOwn(right, key)
      && equalValue(
        left[key as keyof typeof left],
        right[key as keyof typeof right],
      ),
  );
}

function assertNever(value: never): never {
  throw new Error(`unhandled projection: ${String(value)}`);
}
