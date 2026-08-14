import type { Customization, Resume, Section } from '@aboutme/schema';

import type {
  AtomicCommandIntent,
  CustomizationDelta,
  StructureEdit,
} from './commands';
import type { ResumeSnapshot } from './types';

type UnknownRecord = Record<string, unknown>;

export function applyIntent(
  snapshot: ResumeSnapshot,
  intent: AtomicCommandIntent,
): ResumeSnapshot {
  switch (intent.kind) {
    case 'metadataField':
      return {
        ...snapshot,
        metadata: {
          ...snapshot.metadata,
          [intent.field]: intent.value ?? '',
        },
      };
    case 'personalField':
      return updatePersonal(snapshot, intent.path, intent.value);
    case 'entryField':
      return updateEntryField(snapshot, intent);
    case 'entryUpsert':
      return upsertEntry(snapshot, intent.sectionKey, intent.entry);
    case 'entryDelete':
      return deleteEntry(snapshot, intent.sectionKey, intent.entryId);
    case 'entryReorder':
      return reorderEntries(snapshot, intent.sectionKey, intent.entryIds);
    case 'sectionMetadata':
      return updateSectionMetadata(snapshot, intent);
    case 'structure':
      return intent.commands.reduce(applyStructureEdit, snapshot);
    case 'customization':
      return intent.deltas.reduce(applyCustomizationDelta, snapshot);
    case 'photoCrop':
      return updatePhotoCrop(snapshot, intent.crop);
    case 'photoUpload':
      return snapshot;
    case 'photoDelete':
      return deletePhoto(snapshot);
    case 'resumeDelete':
      return snapshot;
    default:
      return assertNever(intent);
  }
}

function updatePersonal(
  snapshot: ResumeSnapshot,
  path: 'fullName' | 'headline' | 'details',
  value: { readonly present: boolean; readonly value?: unknown },
): ResumeSnapshot {
  const personalDetails = { ...snapshot.document.personalDetails } as Record<
    string,
    unknown
  >;
  const nextPersonalDetails = value.present
    ? { ...personalDetails, [path]: value.value }
    : withoutKey(personalDetails, path);
  return withDocument(snapshot, {
    ...snapshot.document,
    personalDetails: nextPersonalDetails,
  });
}

function updateEntryField(
  snapshot: ResumeSnapshot,
  intent: Extract<AtomicCommandIntent, { kind: 'entryField' }>,
): ResumeSnapshot {
  return updateSection(snapshot, intent.sectionKey, (section) => ({
    ...section,
    entries: section.entries.map((entry) => {
      if (entry.id !== intent.entryId) return entry;
      const next = intent.value.present
        ? { ...entry, [intent.path]: intent.value.value }
        : withoutKey(entry, intent.path);
      return next as unknown as Section['entries'][number];
    }),
  }));
}

function upsertEntry(
  snapshot: ResumeSnapshot,
  sectionKey: string,
  entry: Section['entries'][number],
): ResumeSnapshot {
  return updateSection(snapshot, sectionKey, (section) => {
    const index = section.entries.findIndex(
      (candidate) => candidate.id === entry.id,
    );
    const entries
      = index === -1
        ? [...section.entries, structuredClone(entry)]
        : section.entries.map((candidate, candidateIndex) =>
            candidateIndex === index ? structuredClone(entry) : candidate,
          );
    return { ...section, entries } as Section;
  });
}

function deleteEntry(
  snapshot: ResumeSnapshot,
  sectionKey: string,
  entryId: string,
): ResumeSnapshot {
  return updateSection(snapshot, sectionKey, (section) => ({
    ...section,
    entries: section.entries.filter((entry) => entry.id !== entryId),
  }));
}

function reorderEntries(
  snapshot: ResumeSnapshot,
  sectionKey: string,
  entryIds: readonly string[],
): ResumeSnapshot {
  return updateSection(snapshot, sectionKey, (section) => {
    const entries = new Map(section.entries.map((entry) => [entry.id, entry]));
    return {
      ...section,
      entries: entryIds.flatMap((entryId) => {
        const entry = entries.get(entryId);
        return entry === undefined ? [] : [entry];
      }),
    };
  });
}

function updateSectionMetadata(
  snapshot: ResumeSnapshot,
  intent: Extract<AtomicCommandIntent, { kind: 'sectionMetadata' }>,
): ResumeSnapshot {
  return updateSection(snapshot, intent.sectionKey, (section) => {
    const next = intent.change.value === null
      ? withoutKey(section, intent.change.field)
      : { ...section, [intent.change.field]: intent.change.value };
    return next as Section;
  });
}

function applyStructureEdit(
  snapshot: ResumeSnapshot,
  edit: StructureEdit,
): ResumeSnapshot {
  switch (edit.op) {
    case 'createSection': {
      const section: Section = {
        sectionType: edit.sectionType,
        entries: [],
        ...(edit.displayName === undefined
          ? {}
          : { displayName: edit.displayName }),
        ...(edit.iconKey === undefined ? {} : { iconKey: edit.iconKey }),
      } as Section;
      const placement = insertPlacement(
        snapshot.document.customization.layout.sections,
        edit.key,
        edit.column,
        edit.index,
      );
      return withDocument(snapshot, {
        ...snapshot.document,
        content: { ...snapshot.document.content, [edit.key]: section },
        customization: withPlacement(
          snapshot.document.customization,
          placement,
        ),
      });
    }
    case 'deleteSection': {
      const { [edit.key]: _, ...content } = snapshot.document.content;
      return withDocument(snapshot, {
        ...snapshot.document,
        content,
        customization: withPlacement(
          snapshot.document.customization,
          removePlacement(
            snapshot.document.customization.layout.sections,
            edit.key,
          ),
        ),
      });
    }
    case 'moveSection':
      return withDocument(snapshot, {
        ...snapshot.document,
        customization: withPlacement(
          snapshot.document.customization,
          insertPlacement(
            removePlacement(
              snapshot.document.customization.layout.sections,
              edit.key,
            ),
            edit.key,
            edit.column,
            edit.index,
          ),
        ),
      });
    case 'reorderColumn':
      return withDocument(snapshot, {
        ...snapshot.document,
        customization: withPlacement(snapshot.document.customization, {
          ...snapshot.document.customization.layout.sections,
          [edit.column]: [...edit.keys],
        }),
      });
    default:
      return assertNever(edit);
  }
}

function applyCustomizationDelta(
  snapshot: ResumeSnapshot,
  delta: CustomizationDelta,
): ResumeSnapshot {
  const parts = delta.path.split('.');
  if (delta.op === 'unset') {
    const nextCustomization = removePath(
      snapshot.document.customization as unknown as UnknownRecord,
      parts,
    ) as unknown as Customization;
    return withDocument(snapshot, {
      ...snapshot.document,
      customization: nextCustomization,
    });
  }
  const customization = setPath(
    snapshot.document.customization as unknown as UnknownRecord,
    parts,
    delta.value,
  );
  return withDocument(snapshot, {
    ...snapshot.document,
    customization: customization as unknown as Customization,
  });
}

function updatePhotoCrop(
  snapshot: ResumeSnapshot,
  crop: Extract<AtomicCommandIntent, { kind: 'photoCrop' }>['crop'],
): ResumeSnapshot {
  const photo = snapshot.document.personalDetails.photo;
  if (photo === undefined) return snapshot;
  const nextPhoto = { ...photo } as Record<string, unknown>;
  if (crop === null) delete nextPhoto.crop;
  else nextPhoto.crop = structuredClone(crop);
  return withDocument(snapshot, {
    ...snapshot.document,
    personalDetails: {
      ...snapshot.document.personalDetails,
      photo: nextPhoto as unknown as Resume['personalDetails']['photo'],
    },
  });
}

function deletePhoto(snapshot: ResumeSnapshot): ResumeSnapshot {
  const { photo: _, ...personalDetails } = snapshot.document.personalDetails;
  return withDocument(snapshot, { ...snapshot.document, personalDetails });
}

function updateSection(
  snapshot: ResumeSnapshot,
  sectionKey: string,
  update: (section: Section) => Section,
): ResumeSnapshot {
  const section = snapshot.document.content[sectionKey];
  if (section === undefined) return snapshot;
  return withDocument(snapshot, {
    ...snapshot.document,
    content: { ...snapshot.document.content, [sectionKey]: update(section) },
  });
}

function withDocument(
  snapshot: ResumeSnapshot,
  document: Resume,
): ResumeSnapshot {
  return { ...snapshot, document };
}

function withoutKey(value: object, key: string): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(value).filter(([candidate]) => candidate !== key),
  );
}

function removePath(
  value: Record<string, unknown>,
  parts: readonly string[],
): Record<string, unknown> {
  const [part, ...rest] = parts;
  if (part === undefined) return value;
  if (rest.length === 0) return withoutKey(value, part);
  const child = value[part];
  if (child === null || typeof child !== 'object') return value;
  return {
    ...value,
    [part]: removePath(child as Record<string, unknown>, rest),
  };
}

function setPath(
  value: UnknownRecord,
  parts: readonly string[],
  nextValue: unknown,
): UnknownRecord {
  const [part, ...rest] = parts;
  if (part === undefined) return value;
  if (rest.length === 0) {
    return { ...value, [part]: structuredClone(nextValue) };
  }
  const child = value[part];
  const childValue = child !== null && typeof child === 'object'
    ? (child as UnknownRecord)
    : {};
  return { ...value, [part]: setPath(childValue, rest, nextValue) };
}

function withPlacement(
  customization: Customization,
  sections: Customization['layout']['sections'],
): Customization {
  return {
    ...customization,
    layout: { ...customization.layout, sections },
  };
}

function removePlacement(
  placement: Customization['layout']['sections'],
  key: string,
): Customization['layout']['sections'] {
  return {
    main: placement.main.filter((candidate) => candidate !== key),
    sidebar: placement.sidebar.filter((candidate) => candidate !== key),
  };
}

function insertPlacement(
  placement: Customization['layout']['sections'],
  key: string,
  column: 'main' | 'sidebar',
  index: number,
): Customization['layout']['sections'] {
  const existing = removePlacement(placement, key);
  const entries = [...existing[column]];
  entries.splice(Math.max(0, Math.min(index, entries.length)), 0, key);
  return { ...existing, [column]: entries };
}

function assertNever(value: never): never {
  throw new Error(`unhandled intent: ${String(value)}`);
}
