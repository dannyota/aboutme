import type { DateRange } from '@aboutme/schema';

import {
  boundedText,
  normalizedOrigin,
  validCardImageURL,
  validFaviconHref,
  validPageTitle,
  validPreview,
  validProjectSlug,
  validSlug,
} from './envelope-fields';
import type {
  ProjectDetail,
  ProjectPageResume,
  ProjectSummary,
  PublicRenderProjectListRequest,
  PublicRenderProjectRequest,
} from './project-envelope';

// Closed decoding of the project list and project page envelopes
// (docs/design/projects-contract.md "Render envelope"). Text bounds are the
// library's own (apps/server/internal/project/document.go), so a value Go
// can store always renders and anything else fails closed. Every address is
// rebuilt from the origin and the validated slugs and must match exactly, so
// internal links hold only those characters.

type Fields = Record<string, unknown>;

const fail = (): never => {
  throw new Error('invalid project render envelope');
};

const isObject = (value: unknown): value is Fields =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

/** Requires exactly the required keys plus any of the optional ones. */
const requireKeys = (
  value: unknown,
  required: readonly string[],
  optional: readonly string[] = [],
): Fields => {
  if (!isObject(value)) return fail();
  const keys = Object.keys(value);
  if (
    required.some((key) => !keys.includes(key))
    || keys.some((key) => !required.includes(key) && !optional.includes(key))
  ) {
    fail();
  }
  return value;
};

const codePoints = (value: string): number => [...value].length;

const text = (value: unknown, maxCodePoints: number): value is string =>
  typeof value === 'string' && codePoints(value) <= maxCodePoints;

/** An optional text is absent, or present and not empty. */
const optionalText = (value: unknown, maxCodePoints: number): boolean =>
  value === undefined
  || (text(value, maxCodePoints) && value.length > 0);

const MAX_LINK_CODE_POINTS = 2048;
const MAX_DESCRIPTION_BYTES = 16_384;
const MAX_PROJECTS = 30;
const MAX_TECHNOLOGIES = 20;
const MAX_ROW_TECHNOLOGIES = 6;

// The library stores only absolute https links whose scheme it lowercased,
// and the page writes them only into href.
const externalLink = (value: unknown): value is string => {
  if (
    typeof value !== 'string'
    || !value.startsWith('https://')
    || codePoints(value) > MAX_LINK_CODE_POINTS
  ) {
    return false;
  }
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' && parsed.hostname !== '';
  } catch {
    return false;
  }
};

const optionalLink = (value: unknown): boolean =>
  value === undefined || externalLink(value);

const yearMonth = (value: unknown): boolean => {
  const month = requireKeys(value, ['y'], ['m']);
  return Number.isInteger(month.y)
    && (month.y as number) >= 1900 && (month.y as number) <= 2100
    && (month.m === undefined
      || (Number.isInteger(month.m)
        && (month.m as number) >= 1 && (month.m as number) <= 12));
};

// A present range has a null end; a completed range has an end.
const validDates = (value: unknown): value is DateRange => {
  if (value === undefined) return true;
  const range = requireKeys(value, ['start', 'end', 'present']);
  return typeof range.present === 'boolean'
    && yearMonth(range.start)
    && (range.present ? range.end === null : yearMonth(range.end));
};

const technologies = (value: unknown, max: number): value is string[] =>
  Array.isArray(value)
  && value.length <= max
  && value.every((name) => text(name, 40));

const PROJECT_LANG = /^[a-z]{2,8}$/u;

/** A text's language is absent, or a lowercase primary subtag. */
const optionalLang = (value: unknown): boolean =>
  value === undefined
  || (typeof value === 'string' && PROJECT_LANG.test(value));

// A BCP 47 tag as Go's language package writes it, private use included.
const RESUME_LNG = /^[A-Za-z]{1,8}(?:-[A-Za-z0-9]{1,8})*$/u;

const primarySubtag = (lng: string): string =>
  lng.split('-')[0]!.toLowerCase();

const resumeFields = (value: unknown, origin: string): ProjectPageResume => {
  const resume = requireKeys(value, [
    'slug', 'url', 'ownerName', 'lng', 'uiLng', 'colorScheme', 'dateFormat',
  ]);
  if (
    !validSlug(resume.slug)
    || resume.url !== `${origin}/${resume.slug}`
    || !boundedText(resume.ownerName, 2240)
    || resume.ownerName.trim() === ''
    || typeof resume.lng !== 'string'
    || resume.lng.length > 35
    || !RESUME_LNG.test(resume.lng)
    // The page copy is Vietnamese exactly when the resume is.
    || resume.uiLng !== (primarySubtag(resume.lng) === 'vi' ? 'vi' : 'en')
    || !['light', 'dark', 'system'].includes(resume.colorScheme as string)
    || !['MM/YYYY', 'Mon YYYY', 'YYYY'].includes(resume.dateFormat as string)
  ) {
    fail();
  }
  return resume as unknown as ProjectPageResume;
};

const TEXT_KEYS = ['slug', 'url', 'name', 'technologies'];

/** Checks the fields a list row and a project page share. */
const projectText = (project: Fields, listURL: string): void => {
  if (
    !validProjectSlug(project.slug)
    || project.url !== `${listURL}/${project.slug}`
    || !optionalLang(project.lang)
    || !text(project.name, 120)
    || project.name.trim() === ''
    || !optionalText(project.role, 120)
    || !optionalText(project.summary, 300)
    || !validDates(project.dates)
  ) {
    fail();
  }
};

const summaryRow = (value: unknown, listURL: string): ProjectSummary => {
  const row = requireKeys(value, TEXT_KEYS, [
    'lang', 'role', 'dates', 'summary', 'moreTechnologies',
  ]);
  projectText(row, listURL);
  const more = row.moreTechnologies;
  if (
    !technologies(row.technologies, MAX_ROW_TECHNOLOGIES)
    || (more !== undefined
      && !(Number.isInteger(more)
        && (more as number) >= 1
        && (more as number) <= MAX_TECHNOLOGIES - MAX_ROW_TECHNOLOGIES
        && row.technologies.length === MAX_ROW_TECHNOLOGIES))
  ) {
    fail();
  }
  return row as unknown as ProjectSummary;
};

const projectDetail = (value: unknown, listURL: string): ProjectDetail => {
  const project = requireKeys(value, TEXT_KEYS, [
    'lang', 'role', 'dates', 'summary', 'outcome', 'description',
    'repositoryUrl', 'demoUrl',
  ]);
  projectText(project, listURL);
  if (
    !technologies(project.technologies, MAX_TECHNOLOGIES)
    || !optionalText(project.outcome, 600)
    || !(project.description === undefined
      || boundedText(project.description, MAX_DESCRIPTION_BYTES))
    || !optionalLink(project.repositoryUrl)
    || !optionalLink(project.demoUrl)
  ) {
    fail();
  }
  return project as unknown as ProjectDetail;
};

const COMMON_KEYS = [
  'mode', 'canonicalOrigin', 'pageUrl', 'discoveryEnabled', 'pageTitle',
  'faviconHref', 'preview', 'resume',
];

/** Checks the head fields both pages share and returns the resume. */
const commonFields = (envelope: Fields): ProjectPageResume => {
  if (
    !normalizedOrigin(envelope.canonicalOrigin)
    || typeof envelope.discoveryEnabled !== 'boolean'
    || !validPageTitle(envelope.pageTitle)
    || !validFaviconHref(envelope.faviconHref)
    || !validPreview(envelope.preview, true)
  ) {
    fail();
  }
  const origin = envelope.canonicalOrigin as string;
  const resume = resumeFields(envelope.resume, origin);
  const imageUrl = (envelope.preview as Fields).imageUrl;
  if (
    imageUrl !== undefined
    && !validCardImageURL(imageUrl, origin, resume.slug)
  ) {
    fail();
  }
  return resume;
};

export function decodeProjectListEnvelope(
  envelope: Fields,
): PublicRenderProjectListRequest {
  requireKeys(envelope, [...COMMON_KEYS, 'projects']);
  const resume = commonFields(envelope);
  const listURL = `${resume.url}/projects`;
  const projects = envelope.projects;
  if (
    envelope.pageUrl !== listURL
    || !Array.isArray(projects)
    || projects.length === 0
    || projects.length > MAX_PROJECTS
  ) {
    fail();
  }
  const rows = (projects as unknown[]).map((row) => summaryRow(row, listURL));
  if (new Set(rows.map((row) => row.slug)).size !== rows.length) fail();
  return envelope as unknown as PublicRenderProjectListRequest;
}

export function decodeProjectEnvelope(
  envelope: Fields,
): PublicRenderProjectRequest {
  requireKeys(envelope, [
    ...COMMON_KEYS, 'project', 'listUrl', 'otherProjects',
  ], ['entryLink']);
  const resume = commonFields(envelope);
  const listURL = `${resume.url}/projects`;
  const project = projectDetail(envelope.project, listURL);
  const entryLink = envelope.entryLink;
  if (
    envelope.listUrl !== listURL
    || envelope.pageUrl !== project.url
    || typeof envelope.otherProjects !== 'boolean'
    || (entryLink !== undefined
      && (!externalLink(entryLink)
        || entryLink === project.repositoryUrl
        || entryLink === project.demoUrl))
  ) {
    fail();
  }
  return envelope as unknown as PublicRenderProjectRequest;
}
