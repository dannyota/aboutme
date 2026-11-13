import type { components } from '@/api/generated/openapi';

export type Project = components['schemas']['Project'];
export type ProjectRequest = components['schemas']['ReplaceProjectRequest'];
export type CreateProjectRequest
  = components['schemas']['CreateProjectRequest'];
export type ProjectDates = components['schemas']['PublicDateRange'];

export const PROJECT_LIMIT = 30;
export const LANGUAGE_LIMIT = 4;
export const TECHNOLOGY_LIMIT = 20;
export const TECHNOLOGY_LENGTH = 40;
export const DESCRIPTION_BYTES = 16384;
export const TEXT_LIMITS = {
  name: 120,
  role: 120,
  summary: 300,
  outcome: 600,
} as const;
export const COUNTER_WINDOW = 20;

export type PlainTextField = keyof typeof TEXT_LIMITS;
export const PLAIN_TEXT_FIELDS: readonly PlainTextField[] = [
  'name', 'role', 'summary', 'outcome',
];

export interface ProjectTextDraft {
  name: string;
  role: string;
  summary: string;
  outcome: string;
  description: string;
}

export interface ProjectDraft {
  slug: string;
  dates: ProjectDates | null;
  repositoryUrl: string;
  demoUrl: string;
  technologies: string[];
  defaultLanguage: string;
  text: Record<string, ProjectTextDraft>;
}

export type ProjectIssueCode
  = 'required' | 'maxLength' | 'maxItems' | 'format' | 'date-range-order';

export interface ProjectIssue {
  readonly path: string;
  readonly code: ProjectIssueCode;
}

const LANGUAGE_KEY = /^[a-z]{2,8}$/u;
const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/u;

export function isLanguageKey(value: string): boolean {
  return LANGUAGE_KEY.test(value);
}

export function isProjectSlug(value: string): boolean {
  return value.length >= 2 && value.length <= 40 && SLUG.test(value);
}

export function characterCount(value: string): number {
  return [...value].length;
}

export function emptyText(): ProjectTextDraft {
  return { name: '', role: '', summary: '', outcome: '', description: '' };
}

export function draftFromProject(project: Project): ProjectDraft {
  const text: Record<string, ProjectTextDraft> = {};
  for (const [key, value] of Object.entries(project.text)) {
    text[key] = {
      name: value.name ?? '',
      role: value.role ?? '',
      summary: value.summary ?? '',
      outcome: value.outcome ?? '',
      description: value.description ?? '',
    };
  }
  return {
    slug: project.slug,
    dates: datesOf(project.dates),
    repositoryUrl: linkOf(project.repositoryUrl),
    demoUrl: linkOf(project.demoUrl),
    technologies: [...(project.technologies ?? [])],
    defaultLanguage: project.defaultLanguage,
    text,
  };
}

type YearMonth = ProjectDates['start'];

// structuredClone throws DataCloneError on a Vue reactive proxy.
function yearMonthOf(value: YearMonth): YearMonth {
  return value.m === undefined ? { y: value.y } : { y: value.y, m: value.m };
}

function datesOf(dates: ProjectDates | null | undefined): ProjectDates | null {
  if (dates === undefined || dates === null) return null;
  return {
    start: yearMonthOf(dates.start),
    end: dates.end === null ? null : yearMonthOf(dates.end),
    present: dates.present,
  };
}

function linkOf(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

export function requestFromDraft(draft: ProjectDraft): ProjectRequest {
  const text: ProjectRequest['text'] = {};
  for (const [key, value] of Object.entries(draft.text)) {
    const entry: ProjectRequest['text'][string] = {};
    for (const field of [...PLAIN_TEXT_FIELDS, 'description'] as const) {
      if (value[field] !== '') entry[field] = value[field];
    }
    text[key] = entry;
  }
  return {
    slug: draft.slug,
    ...(draft.dates === null ? {} : { dates: draft.dates }),
    ...(draft.repositoryUrl === ''
      ? {}
      : { repositoryUrl: draft.repositoryUrl }),
    ...(draft.demoUrl === '' ? {} : { demoUrl: draft.demoUrl }),
    ...(draft.technologies.length === 0
      ? {}
      : { technologies: [...draft.technologies] }),
    defaultLanguage: draft.defaultLanguage,
    text,
  };
}

export function requestFromProject(
  project: Project,
  slug = project.slug,
): ProjectRequest {
  return requestFromDraft({ ...draftFromProject(project), slug });
}

export function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJSON).join(',')}]`;
  }
  if (typeof value === 'object' && value !== null) {
    const entries = Object.entries(value as Record<string, unknown>)
      .filter(([, item]) => item !== undefined)
      .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
    return `{${entries.map(([key, item]) =>
      `${JSON.stringify(key)}:${canonicalJSON(item)}`).join(',')}}`;
  }
  return JSON.stringify(value);
}

export function sameDocument(left: ProjectDraft, right: ProjectDraft): boolean {
  return canonicalJSON(requestFromDraft(left))
    === canonicalJSON(requestFromDraft(right));
}

function validLink(value: string): boolean {
  if (value === '') return true;
  if (!value.startsWith('https://')) return false;
  try {
    return new URL(value).hostname !== '';
  } catch {
    return false;
  }
}

function utf8Bytes(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

export function validateDraft(draft: ProjectDraft): ProjectIssue[] {
  const issues: ProjectIssue[] = [];
  for (const [key, value] of Object.entries(draft.text)) {
    for (const field of PLAIN_TEXT_FIELDS) {
      const path = `text.${key}.${field}`;
      if (
        field === 'name'
        && key === draft.defaultLanguage
        && value.name.trim() === ''
      ) {
        issues.push({ path, code: 'required' });
      } else if (characterCount(value[field]) > TEXT_LIMITS[field]) {
        issues.push({ path, code: 'maxLength' });
      }
    }
    if (utf8Bytes(value.description) > DESCRIPTION_BYTES) {
      issues.push({ path: `text.${key}.description`, code: 'maxLength' });
    }
  }
  if (!validLink(draft.repositoryUrl)) {
    issues.push({ path: 'repositoryUrl', code: 'format' });
  }
  if (!validLink(draft.demoUrl)) {
    issues.push({ path: 'demoUrl', code: 'format' });
  }
  if (draft.technologies.length > TECHNOLOGY_LIMIT) {
    issues.push({ path: 'technologies', code: 'maxItems' });
  } else if (draft.technologies.some((item) =>
    characterCount(item) > TECHNOLOGY_LENGTH)) {
    issues.push({ path: 'technologies', code: 'maxLength' });
  }
  return issues;
}

export function projectName(project: Project): string {
  return project.text[project.defaultLanguage]?.name ?? '';
}

export function projectRole(project: Project): string {
  return project.text[project.defaultLanguage]?.role ?? '';
}
