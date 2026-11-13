import type { DateRange } from '@aboutme/schema';

import {
  boundedText,
  normalizedOrigin,
  type PreviewBounds,
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

// The library stores no NUL and only valid UTF-8, which has no lone
// surrogate.
const storable = (value: string): boolean =>
  !value.includes('\u0000') && value.isWellFormed();

const text = (value: unknown, maxCodePoints: number): value is string =>
  typeof value === 'string'
  && codePoints(value) <= maxCodePoints
  && storable(value);

/** An optional text is absent, or present and not empty. */
const optionalText = (value: unknown, maxCodePoints: number): boolean =>
  value === undefined
  || (text(value, maxCodePoints) && value.length > 0);

const MAX_LINK_CODE_POINTS = 2048;
const MAX_DESCRIPTION_BYTES = 16_384;
const MAX_PROJECTS = 30;
const MAX_TECHNOLOGIES = 20;
const MAX_ROW_TECHNOLOGIES = 6;

interface LinkParts {
  readonly userinfo: string | undefined;
  readonly host: string;
  readonly port: string;
  /** The path, query and fragment, as written. */
  readonly rest: string;
}

// Splits an https link the way Go's net/url does: the authority ends at the
// first /, ? or #, the userinfo at its last @, and the port follows the
// host's last colon outside an IPv6 literal.
const linkParts = (value: string): LinkParts => {
  const afterScheme = value.slice('https://'.length);
  const end = afterScheme.search(/[/?#]/u);
  const authority = end === -1 ? afterScheme : afterScheme.slice(0, end);
  const at = authority.lastIndexOf('@');
  const hostPort = authority.slice(at + 1);
  const colon = hostPort.lastIndexOf(':');
  const hasPort = colon > hostPort.lastIndexOf(']');
  return {
    userinfo: at === -1 ? undefined : authority.slice(0, at),
    host: hasPort ? hostPort.slice(0, colon) : hostPort,
    port: hasPort ? hostPort.slice(colon + 1) : '',
    rest: end === -1 ? '' : afterScheme.slice(end),
  };
};

// What Go's url.Parse accepts and URL parsing alone would let through: no
// control byte anywhere (URL parsing drops tabs and newlines), userinfo of
// RFC 3986 characters, ASCII host characters Go's host parser allows, and a
// numeric port.
const USERINFO = /^[A-Za-z0-9\-._:~!$&'()*+,;=%@]*$/u;
const HOST_ASCII = /^[A-Za-z0-9\-._~!$&'()*+,;=:[\]<>"%]$/u;

const goParsable = (value: string): boolean => {
  if ([...value].some((character) => {
    const code = character.codePointAt(0)!;
    return code < 0x20 || code === 0x7f;
  })) {
    return false;
  }
  const { userinfo, host, port } = linkParts(value);
  return (userinfo === undefined || USERINFO.test(userinfo))
    && /^[0-9]*$/u.test(port)
    && [...host].every((character) =>
      character.codePointAt(0)! >= 0x80 || HOST_ASCII.test(character));
};

// The library stores only absolute https links whose scheme it lowercased,
// and the page writes them only into href.
const externalLink = (value: unknown): value is string => {
  if (
    typeof value !== 'string'
    || !value.startsWith('https://')
    || codePoints(value) > MAX_LINK_CODE_POINTS
    || !storable(value)
    || !goParsable(value)
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

// The contract's link comparison: the scheme and host lowercased, the
// default port dropped, and an empty path read as /; the rest byte for byte
// (docs/design/projects-contract.md "Render envelope").
const comparableLink = (value: string): string => {
  const { userinfo, host, port, rest } = linkParts(value);
  return [
    'https://',
    userinfo === undefined ? '' : `${userinfo}@`,
    host.toLowerCase(),
    port === '' || port === '443' ? '' : `:${port}`,
    rest.startsWith('/') ? rest : `/${rest}`,
  ].join('');
};

const sameLink = (link: string, other: string | undefined): boolean =>
  other !== undefined && comparableLink(link) === comparableLink(other);

/** The description is absent, or stored rich text within its bound. */
const optionalDescription = (value: unknown): boolean =>
  value === undefined
  || (boundedText(value, MAX_DESCRIPTION_BYTES) && storable(value));

const optionalLink = (value: unknown): boolean =>
  value === undefined || externalLink(value);

// Go orders a range by year and month, an omitted month as January
// (packages/schema/gen/go/store_validate.go, ValidateDateRange).
const monthOrdinal = (value: unknown): number => {
  const month = value as { y: number; m?: number };
  return month.y * 12 + (month.m ?? 1);
};

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
    && (range.present
      ? range.end === null
      : yearMonth(range.end)
        && monthOrdinal(range.start) <= monthOrdinal(range.end));
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
    || !storable(resume.ownerName)
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
    || !optionalDescription(project.description)
    || !optionalLink(project.repositoryUrl)
    || !optionalLink(project.demoUrl)
  ) {
    fail();
  }
  return project as unknown as ProjectDetail;
};

// Go sends both titles uncut: "{name} · {ownerName}" on a project page,
// at most 683 code points and 2,727 bytes. A project page's description is
// its summary uncut, at most 300 code points and 1,200 bytes, else the
// first 160 code points of its outcome; the list page has none.
const PROJECT_PREVIEW: PreviewBounds = {
  titleBytes: 2727,
  titleCodePoints: 683,
  descriptionBytes: 1200,
  emptyDescription: true,
};
const OUTCOME_DESCRIPTION_CODE_POINTS = 160;

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
    || !validPreview(envelope.preview, PROJECT_PREVIEW)
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
    || (envelope.preview as Fields).description !== ''
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
  const description = project.summary ?? (project.outcome === undefined
    ? ''
    : [...project.outcome].slice(0, OUTCOME_DESCRIPTION_CODE_POINTS)
        .join(''));
  if (
    envelope.listUrl !== listURL
    || envelope.pageUrl !== project.url
    || (envelope.preview as Fields).description !== description
    || typeof envelope.otherProjects !== 'boolean'
    || (entryLink !== undefined
      && (!externalLink(entryLink)
        || sameLink(entryLink, project.repositoryUrl)
        || sameLink(entryLink, project.demoUrl)))
  ) {
    fail();
  }
  return envelope as unknown as PublicRenderProjectRequest;
}
