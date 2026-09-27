/**
 * Turns a parsed LinkedIn profile into the review the import page shows, and
 * the reviewed choices into the resume document and the create request. See
 * docs/design/linkedin-import.md, "Mapping" (including "Limits") and "What
 * is dropped", and docs/design/linkedin-import-ui.md, "Review state"; ADR
 * 0023.
 */
import type {
  Content,
  Customization,
  PersonalDetail,
  PersonalDetails,
  Resume,
  Section,
} from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { validateDocument } from '@aboutme/schema/validation';

import { applyTemplate } from '../../components/resume/applyTemplate';
// A build-time Ajv standalone compile, like the editor's own document check
// (apps/web/app/editor/documentValidation.ts): a runtime `ajv.compile()`
// would emit a `new Function`, which the app-page CSP's script-src blocks.
import validateSchema from '../../editor/documentValidator.generated.mjs';
import { editorSectionsCopy } from '../../i18n/editor-sections';
import { GALLERY, galleryTemplate } from '../../templates/catalog';
import { blankTemplateDocument } from '../../templates/startDocument';
import { analyzeLayout } from './layout';
import { buildPageLines, columnBoundary } from './lines';
import { clipText, RICH_TEXT_MAX_BYTES } from './richText';
import type { ReadFailure, ReadOptions } from './read';
import { readLinkedInPdf } from './read';
import type {
  ContactDraft,
  EducationDraft,
  LanguageDraft,
  ParsedProfile,
  WorkDraft,
} from './sections';
import { parseProfile } from './sections';

export type ImportSectionKey
  = | 'profile'
    | 'work'
    | 'education'
    | 'skill'
    | 'language'
    | 'certificate';

export const IMPORT_SECTION_ORDER: readonly ImportSectionKey[] = [
  'profile',
  'work',
  'education',
  'skill',
  'language',
  'certificate',
];

export type CutField
  = | 'fullName'
    | 'headline'
    | 'jobTitle'
    | 'employer'
    | 'school'
    | 'degree'
    | 'certificate'
    | 'city'
    | 'country'
    | 'skill'
    | 'language'
    | 'description'
    | 'summary';

export type EntryMark
  = | { kind: 'noDates' }
    | { kind: 'startOnly' }
    | { kind: 'cut'; field: CutField; max: number };

export interface ReviewDetail {
  id: string;
  type: 'email' | 'phone' | 'location' | 'linkedin' | 'website';
  value: string;
  label?: string;
  selectedByDefault: boolean;
}

export interface ReviewEntry {
  id: string;
  section: ImportSectionKey;
  /** The schema entry object with its new UUID id, ready for content. */
  entry: Record<string, unknown>;
  marks: EntryMark[];
  selectedByDefault: boolean;
}

export interface ReviewSection {
  key: ImportSectionKey;
  entries: ReviewEntry[];
  overLimit: boolean;
}

export interface ImportReview {
  fullName: string;
  headline: string;
  /** Cut marks for fullName and headline. */
  nameMarks: EntryMark[];
  details: ReviewDetail[];
  /** Only found, non-empty sections, in IMPORT_SECTION_ORDER. */
  sections: ReviewSection[];
  dropped: { heading: string; count: number }[];
  /** The gallery's first template (docs/design/linkedin-import.md, I4). */
  template: { id: string; name: string };
}

export interface ReviewChoices {
  title: string;
  fullName: string;
  headline: string;
  detailIds: ReadonlySet<string>;
  entryIds: ReadonlySet<string>;
}

/** Bytes over this fail the size meter (docs/design/linkedin-import.md). */
export const REQUEST_MAX_BYTES = 262144;

export type SchemaCheck
  = | { ok: true }
    | {
      ok: false;
      /** Selected entries whose own subtree fails. */
      entryIds: string[];
      /** A failure outside any entry, such as a layout or version problem. */
      general: boolean;
    };

export type ImportFailure = ReadFailure | 'notLinkedIn' | 'notEnglish';

const NAME_MAX = 160;
const LONG_FIELD_MAX = 160;
const SHORT_FIELD_MAX = 120;
const LABEL_MAX = 40;
const MAX_SECTION_ENTRIES = 64;

/** Clips to `max` and records a cut mark on `marks` when it was too long. */
function clipField(
  value: string,
  max: number,
  field: CutField,
  marks: EntryMark[],
): string {
  const clipped = clipText(value, max);
  if (clipped.cut) marks.push({ kind: 'cut', field, max });
  return clipped.text;
}

function omitEmpty(
  entry: Record<string, unknown>,
  key: string,
  value: string | undefined,
): void {
  if (value !== undefined && value !== '') entry[key] = value;
}

interface Built {
  entry: Record<string, unknown>;
  marks: EntryMark[];
}

function buildProfileEntry(
  draft: { html: string; cut: boolean },
  id: string,
): Built {
  const marks: EntryMark[] = [];
  if (draft.cut) {
    marks.push({ kind: 'cut', field: 'summary', max: RICH_TEXT_MAX_BYTES });
  }
  return { entry: { id, text: draft.html }, marks };
}

function buildWorkEntry(draft: WorkDraft, id: string): Built {
  const marks: EntryMark[] = [];
  const employer = clipField(draft.employer, LONG_FIELD_MAX, 'employer', marks);
  const jobTitle = clipField(draft.jobTitle, LONG_FIELD_MAX, 'jobTitle', marks);
  const city = draft.city === undefined
    ? undefined
    : clipField(draft.city, SHORT_FIELD_MAX, 'city', marks);
  const country = draft.country === undefined
    ? undefined
    : clipField(draft.country, SHORT_FIELD_MAX, 'country', marks);
  if (draft.descriptionCut) {
    marks.push({ kind: 'cut', field: 'description', max: RICH_TEXT_MAX_BYTES });
  }
  if (draft.dateNotice === 'startOnly') marks.push({ kind: 'startOnly' });
  if (draft.dateNotice === 'unreadable') marks.push({ kind: 'noDates' });

  const entry: Record<string, unknown> = { id };
  omitEmpty(entry, 'employer', employer);
  omitEmpty(entry, 'jobTitle', jobTitle);
  if (draft.dates !== undefined) entry.dates = draft.dates;
  omitEmpty(entry, 'city', city);
  omitEmpty(entry, 'country', country);
  omitEmpty(entry, 'description', draft.description);
  return { entry, marks };
}

function buildEducationEntry(draft: EducationDraft, id: string): Built {
  const marks: EntryMark[] = [];
  const school = clipField(draft.school, LONG_FIELD_MAX, 'school', marks);
  const degree = draft.degree === undefined
    ? undefined
    : clipField(draft.degree, LONG_FIELD_MAX, 'degree', marks);
  if (draft.dateNotice === 'unreadable') marks.push({ kind: 'noDates' });

  const entry: Record<string, unknown> = { id };
  omitEmpty(entry, 'school', school);
  omitEmpty(entry, 'degree', degree);
  if (draft.dates !== undefined) entry.dates = draft.dates;
  return { entry, marks };
}

function buildSkillEntry(name: string, id: string): Built {
  const marks: EntryMark[] = [];
  const clipped = clipField(name, SHORT_FIELD_MAX, 'skill', marks);
  return { entry: { id, name: clipped }, marks };
}

function buildLanguageEntry(draft: LanguageDraft, id: string): Built {
  const marks: EntryMark[] = [];
  const name = clipField(draft.name, SHORT_FIELD_MAX, 'language', marks);
  const entry: Record<string, unknown> = { id, name };
  if (draft.level !== undefined) entry.level = draft.level;
  return { entry, marks };
}

function buildCertificateEntry(title: string, id: string): Built {
  const marks: EntryMark[] = [];
  const clipped = clipField(title, LONG_FIELD_MAX, 'certificate', marks);
  return { entry: { id, title: clipped }, marks };
}

/** Builds one section from its drafts, or undefined when there are none. */
function buildSection<T>(
  key: ImportSectionKey,
  drafts: readonly T[],
  uuid: () => string,
  build: (draft: T, id: string) => Built,
): ReviewSection | undefined {
  if (drafts.length === 0) return undefined;
  const entries: ReviewEntry[] = drafts.map((draft, index) => {
    const id = uuid();
    const { entry, marks } = build(draft, id);
    return {
      id,
      section: key,
      entry,
      marks,
      selectedByDefault: index < MAX_SECTION_ENTRIES,
    };
  });
  return { key, entries, overLimit: entries.length > MAX_SECTION_ENTRIES };
}

function contactsOf(
  contacts: readonly ContactDraft[],
  type: ContactDraft['type'],
): ContactDraft[] {
  return contacts.filter((contact) => contact.type === type);
}

/**
 * Personal details in review order: email, phone, location, linkedin,
 * websites. Email and phone start unselected because a published resume
 * shows them (docs/design/linkedin-import.md, "Review screen").
 */
function buildDetails(
  parsed: ParsedProfile,
  uuid: () => string,
): ReviewDetail[] {
  const details: ReviewDetail[] = [];
  for (const contact of contactsOf(parsed.contacts, 'email')) {
    details.push({
      id: uuid(), type: 'email', value: contact.value, selectedByDefault: false,
    });
  }
  for (const contact of contactsOf(parsed.contacts, 'phone')) {
    details.push({
      id: uuid(), type: 'phone', value: contact.value, selectedByDefault: false,
    });
  }
  if (parsed.profile.location !== undefined) {
    details.push({
      id: uuid(),
      type: 'location',
      value: parsed.profile.location,
      selectedByDefault: true,
    });
  }
  for (const contact of contactsOf(parsed.contacts, 'linkedin')) {
    details.push({
      id: uuid(),
      type: 'linkedin',
      value: contact.value,
      selectedByDefault: true,
    });
  }
  for (const contact of contactsOf(parsed.contacts, 'website')) {
    const label = contact.label === undefined
      ? undefined
      : clipText(contact.label, LABEL_MAX).text;
    details.push({
      id: uuid(),
      type: 'website',
      value: contact.value,
      selectedByDefault: true,
      ...(label === undefined ? {} : { label }),
    });
  }
  return details;
}

/** The parsed profile as the review the person checks before Create. */
export function buildReview(
  parsed: ParsedProfile,
  uuid: () => string,
): ImportReview {
  const nameMarks: EntryMark[] = [];
  const fullName = clipField(
    parsed.profile.fullName, NAME_MAX, 'fullName', nameMarks,
  );
  const headline = clipField(
    parsed.profile.headline, NAME_MAX, 'headline', nameMarks,
  );

  const sections: ReviewSection[] = [];
  const profileDrafts = parsed.summary === undefined ? [] : [parsed.summary];
  const built: (ReviewSection | undefined)[] = [
    buildSection('profile', profileDrafts, uuid, buildProfileEntry),
    buildSection('work', parsed.work, uuid, buildWorkEntry),
    buildSection('education', parsed.education, uuid, buildEducationEntry),
    buildSection('skill', parsed.skills, uuid, buildSkillEntry),
    buildSection('language', parsed.languages, uuid, buildLanguageEntry),
    buildSection(
      'certificate', parsed.certificates, uuid, buildCertificateEntry,
    ),
  ];
  for (const section of built) {
    if (section !== undefined) sections.push(section);
  }

  const template = GALLERY[0];
  if (template === undefined) throw new Error('gallery has no templates');
  return {
    fullName,
    headline,
    nameMarks,
    details: buildDetails(parsed, uuid),
    sections,
    dropped: parsed.dropped,
    template: { id: template.id, name: template.name },
  };
}

/** The chosen entries of one section, capped at 64, in their review order. */
function selectedEntries(
  section: ReviewSection,
  choices: ReviewChoices,
): ReviewEntry[] {
  return section.entries
    .filter((entry) => choices.entryIds.has(entry.id))
    .slice(0, MAX_SECTION_ENTRIES);
}

function selectedDetails(
  review: ImportReview,
  choices: ReviewChoices,
): ReviewDetail[] {
  return review.details.filter((detail) => choices.detailIds.has(detail.id));
}

/** Every section's chosen entries, keyed by section, in IMPORT_SECTION_ORDER.
 */
function selectedContent(
  review: ImportReview,
  choices: ReviewChoices,
): Map<ImportSectionKey, ReviewEntry[]> {
  const result = new Map<ImportSectionKey, ReviewEntry[]>();
  for (const key of IMPORT_SECTION_ORDER) {
    const section = review.sections.find((candidate) => candidate.key === key);
    if (section === undefined) continue;
    result.set(key, selectedEntries(section, choices));
  }
  return result;
}

/**
 * The reviewed choices as a resume document: `blankTemplateDocument` of the
 * gallery's first template, filled with the chosen personal details and
 * content, then `applyTemplate` so placement covers the new section keys, as
 * a blank resume would (docs/design/linkedin-import.md, "Mapping").
 */
export function buildDocument(
  review: ImportReview,
  choices: ReviewChoices,
): Resume {
  const template = galleryTemplate(review.template.id);
  if (template === undefined) {
    throw new Error(`unknown gallery template "${review.template.id}"`);
  }
  const base = blankTemplateDocument(template.preset);

  const content: Content = {};
  for (const [key, entries] of selectedContent(review, choices)) {
    if (entries.length === 0) continue;
    content[key] = {
      sectionType: key,
      displayName: editorSectionsCopy.en.sectionTypes[key],
      entries: entries.map((entry) => entry.entry),
    } as unknown as Section;
  }

  const current: Customization = {
    ...base.customization,
    layout: {
      ...base.customization.layout,
      sections: { main: Object.keys(content), sidebar: [] },
    },
  };
  const customization = applyTemplate(current, template.preset, content);

  const details: PersonalDetail[] = selectedDetails(review, choices).map(
    (detail): PersonalDetail => ({
      id: detail.id,
      type: detail.type,
      value: detail.value,
      isHidden: false,
      ...(detail.label === undefined ? {} : { label: detail.label }),
    }),
  );
  const personalDetails: PersonalDetails = {
    fullName: choices.fullName,
    headline: choices.headline,
    details,
  };

  return {
    schemaVersion: CURRENT_VERSION, personalDetails, content, customization,
  };
}

/** UTF-8 bytes of the JSON body the create request sends. */
export function requestBytes(title: string, document: Resume): number {
  return new TextEncoder().encode(
    JSON.stringify({ title, lng: 'en', document }),
  ).length;
}

const AJV_ENTRY_RE = /^\/content\/([^/]+)\/entries\/(\d+)(?:\/|$)/;
const AJV_DETAIL_RE = /^\/personalDetails\/details\/(\d+)(?:\/|$)/;
const DOT_ENTRY_RE = /^content\.([^.]+)\.entries\[(\d+)]/;
const DOT_DETAIL_RE = /^personalDetails\.details\[(\d+)]/;

interface AjvLikeError {
  instancePath: string;
}

/**
 * Maps one path (an Ajv `instancePath` or a `validateDocument` dotted path)
 * to the entry or detail id it names in `entryIds`, when it names one at
 * all. Returns whether it mapped to a chosen entry or detail.
 */
function mapPath(
  entryPattern: RegExp,
  detailPattern: RegExp,
  path: string,
  content: ReadonlyMap<ImportSectionKey, ReviewEntry[]>,
  details: readonly ReviewDetail[],
  entryIds: Set<string>,
): boolean {
  const entryMatch = entryPattern.exec(path);
  if (entryMatch !== null) {
    const entries = content.get(entryMatch[1] as ImportSectionKey);
    const entry = entries?.[Number(entryMatch[2])];
    if (entry !== undefined) {
      entryIds.add(entry.id);
      return true;
    }
  }
  const detailMatch = detailPattern.exec(path);
  if (detailMatch !== null) {
    const detail = details[Number(detailMatch[1])];
    if (detail !== undefined) {
      entryIds.add(detail.id);
      return true;
    }
  }
  return false;
}

/**
 * Runs the generated schema validator and the aggregate store checks the
 * create route also applies (packages/schema/validation/store.ts, for
 * example date ordering), and maps each failure to the chosen entry or
 * detail it belongs to.
 */
export function checkDocument(
  document: Resume,
  review: ImportReview,
  choices: ReviewChoices,
): SchemaCheck {
  const content = selectedContent(review, choices);
  const details = selectedDetails(review, choices);
  const entryIds = new Set<string>();
  let general = false;

  const schemaOk = validateSchema(document);
  const schemaErrors = (
    validateSchema as unknown as { errors?: readonly AjvLikeError[] | null }
  ).errors ?? [];
  for (const error of schemaErrors) {
    const mapped = mapPath(
      AJV_ENTRY_RE,
      AJV_DETAIL_RE,
      error.instancePath,
      content,
      details,
      entryIds,
    );
    if (!mapped) general = true;
  }

  const storeIssues = validateDocument(document as never);
  for (const issue of storeIssues) {
    const mapped = mapPath(
      DOT_ENTRY_RE, DOT_DETAIL_RE, issue.path, content, details, entryIds,
    );
    if (!mapped) general = true;
  }

  if (schemaOk && storeIssues.length === 0) return { ok: true };
  return { ok: false, entryIds: [...entryIds], general };
}

/**
 * The whole pipeline: reads the PDF, builds each page's lines against the
 * page-1 column boundary, splits them into a layout, maps the layout to a
 * profile, and builds the review (docs/design/linkedin-import.md, "Reading
 * the file").
 */
export async function importLinkedInPdf(
  file: Blob,
  options: ReadOptions,
  uuid: () => string,
): Promise<
  { ok: true; review: ImportReview } | { ok: false; reason: ImportFailure }
> {
  const read = await readLinkedInPdf(file, options);
  if (!read.ok) return { ok: false, reason: read.reason };

  const boundary = columnBoundary(read.pages[0] ?? []);
  const pageLines = read.pages.map(
    (items, index) => buildPageLines(index + 1, items, boundary),
  );
  const layoutResult = analyzeLayout(pageLines);
  if (layoutResult.kind !== 'ok') {
    return { ok: false, reason: layoutResult.kind };
  }

  const parsed = parseProfile(layoutResult.layout);
  return { ok: true, review: buildReview(parsed, uuid) };
}
