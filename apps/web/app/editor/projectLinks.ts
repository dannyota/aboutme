import type { Resume } from '@aboutme/schema';

export interface EntryLocation {
  readonly sectionKey: string;
  readonly entryId: string;
}

export function linkedEntry(
  document: Resume,
  projectId: string,
): EntryLocation | undefined {
  for (const [sectionKey, section] of Object.entries(document.content)) {
    const entry = section.entries.find((candidate) =>
      'projectId' in candidate && candidate.projectId === projectId);
    if (entry !== undefined) return { sectionKey, entryId: entry.id };
  }
  return undefined;
}

export function linkedProjectIds(document: Resume): ReadonlySet<string> {
  const ids = new Set<string>();
  for (const section of Object.values(document.content)) {
    for (const entry of section.entries) {
      if ('projectId' in entry && entry.projectId !== undefined) {
        ids.add(entry.projectId);
      }
    }
  }
  return ids;
}

export function projectPageLanguage(
  lng: string | null | undefined,
  project: {
    readonly defaultLanguage: string;
    readonly text: Readonly<Record<string, unknown>>;
  },
): { readonly key: string; readonly fallback: boolean } {
  const primary = (lng ?? '').toLowerCase().split('-')[0] ?? '';
  const determined = primary !== '' && primary !== 'und';
  if (determined && Object.hasOwn(project.text, primary)) {
    return { key: primary, fallback: false };
  }
  return { key: project.defaultLanguage, fallback: determined };
}

export interface PublishedState {
  readonly id: string;
  readonly live: boolean;
}

export function revealsAcrossResumes(
  targetId: string,
  linkedResumeIds: Iterable<string> | null,
  resumes: readonly PublishedState[] | null,
): boolean {
  if (resumes === null) return true;
  const published = (id: string) =>
    resumes.some((item) => item.id === id && item.live);
  if (!published(targetId)) return false;
  if (linkedResumeIds === null) return true;
  for (const id of linkedResumeIds) {
    if (id !== targetId && published(id)) return true;
  }
  return false;
}

export function choosesTwoPublished(
  resumes: readonly PublishedState[],
  chosen: readonly string[],
): boolean {
  return resumes.filter((item) =>
    item.live && chosen.includes(item.id)).length >= 2;
}
