import type { Resume } from '@aboutme/schema';

// Where a resume's library links live, and which project text a resume uses
// (docs/design/projects.md "Selection" and "Language").

export interface EntryLocation {
  readonly sectionKey: string;
  readonly entryId: string;
}

/** The entry of `document` that links `projectId`; a resume holds one. */
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

/** Every project the resume already links. */
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

/**
 * The text a project page under this resume uses: the key equal to the
 * resume language's primary subtag, otherwise the default language. It
 * falls back only when the resume has a language the project lacks; an
 * undetermined (`und`) or missing language uses the default by rule.
 */
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

/**
 * Whether linking `targetId` would show the project on a second published
 * resume, so a reader of both can tell they belong to the same person
 * (docs/design/projects.md "Privacy"). Drafts never count. When the other
 * resumes could not be read, any other linking resume counts, so the owner
 * is asked rather than not.
 */
export function revealsAcrossResumes(
  targetId: string,
  linkedResumeIds: Iterable<string>,
  resumes: readonly PublishedState[] | null,
  targetLive = resumes?.some((item) => item.id === targetId && item.live)
    ?? false,
): boolean {
  if (!targetLive) return false;
  const published = (id: string) =>
    resumes === null || resumes.some((item) => item.id === id && item.live);
  for (const id of linkedResumeIds) {
    if (id !== targetId && published(id)) return true;
  }
  return false;
}
