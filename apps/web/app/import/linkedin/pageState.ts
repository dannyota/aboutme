/**
 * Pure state and derived values for /app/import/linkedin's review screen
 * (docs/design/linkedin-import-ui.md, "Review state" and "Action panel").
 * Kept free of Vue and Nuxt so it runs under a plain Node test, like the
 * rest of apps/web/app/import/linkedin.
 */
import type { Customization, DateRange } from '@aboutme/schema';

import { formatDateRange } from '../../components/resume/formatDate';
import { editorFieldsCopy } from '../../i18n/editor-fields';
import { editorSectionsCopy } from '../../i18n/editor-sections';
import type { Locale } from '../../i18n/locale';
import type {
  CutField,
  ImportReview,
  ImportSectionKey,
  ReviewEntry,
  ReviewSection,
} from './build';

/** The resume the import creates is always English (ADR 0064, decision 7). */
const DOCUMENT_LNG = 'en';

export type DateFormat = Customization['dateFormat'];

// --- Selection state -------------------------------------------------------

export interface ReviewChoiceSets {
  readonly detailIds: Set<string>;
  readonly entryIds: Set<string>;
}

/** The selection a fresh review starts from (`selectedByDefault` on each
 * detail and entry). */
export function initialChoiceSets(review: ImportReview): ReviewChoiceSets {
  const detailIds = new Set(
    review.details.filter((detail) => detail.selectedByDefault)
      .map((detail) => detail.id),
  );
  const entryIds = new Set<string>();
  for (const section of review.sections) {
    for (const entry of section.entries) {
      if (entry.selectedByDefault) entryIds.add(entry.id);
    }
  }
  return { detailIds, entryIds };
}

export type GroupState = 'checked' | 'unchecked' | 'indeterminate';

/** A section's group checkbox state: checked when every entry is, unchecked
 * when none is, indeterminate otherwise (docs/design/linkedin-import-ui.md,
 * "Review state"). */
export function groupCheckState(
  section: ReviewSection,
  entryIds: ReadonlySet<string>,
): GroupState {
  if (section.entries.length === 0) return 'unchecked';
  const selected = section.entries
    .filter((entry) => entryIds.has(entry.id)).length;
  if (selected === 0) return 'unchecked';
  if (selected === section.entries.length) return 'checked';
  return 'indeterminate';
}

/** Toggling the group checkbox sets every entry in the section. */
export function toggleGroup(
  section: ReviewSection,
  entryIds: ReadonlySet<string>,
  checked: boolean,
): Set<string> {
  const next = new Set(entryIds);
  for (const entry of section.entries) {
    if (checked) next.add(entry.id);
    else next.delete(entry.id);
  }
  return next;
}

/** `{n} of {m} selected` for one section's group row. */
export function sectionCount(
  section: ReviewSection,
  entryIds: ReadonlySet<string>,
): { n: number; m: number } {
  const n = section.entries.filter((entry) => entryIds.has(entry.id)).length;
  return { n, m: section.entries.length };
}

/** `{n} items from {s} sections` for the action panel. */
export function selectedSummary(
  review: ImportReview,
  entryIds: ReadonlySet<string>,
): { n: number; s: number } {
  let n = 0;
  let s = 0;
  for (const section of review.sections) {
    const count = section.entries
      .filter((entry) => entryIds.has(entry.id)).length;
    n += count;
    if (count > 0) s += 1;
  }
  return { n, s };
}

// --- Size meter --------------------------------------------------------

/** Request bytes divided by 1,024, rounded up (docs/design/
 * linkedin-import-ui.md, "Action panel"). */
export function requestKb(bytes: number): number {
  return Math.ceil(bytes / 1024);
}

/** The size meter's maximum, in KB (`REQUEST_MAX_BYTES` / 1,024). */
export const MAX_REQUEST_KB = 256;

// --- Catalog lookups -----------------------------------------------------

/** A cut field's localized name, from the editor's field catalogs. */
export function cutFieldName(locale: Locale, field: CutField): string {
  const fields = editorFieldsCopy[locale];
  switch (field) {
    case 'fullName': return fields.personal.fullName;
    case 'headline': return fields.personal.headline;
    case 'jobTitle': return fields.entry.work.jobTitle;
    case 'employer': return fields.entry.work.employer;
    case 'school': return fields.entry.education.school;
    case 'degree': return fields.entry.education.degree;
    case 'certificate': return fields.entry.certificate.title;
    case 'city': return fields.entry.work.city;
    case 'country': return fields.entry.work.country;
    case 'skill': return fields.entry.skill.name;
    case 'language': return fields.entry.language.name;
    case 'description': return fields.entry.work.description;
    case 'summary': return fields.entry.profile.text;
  }
}

/** A found section's editor name (the manager's decision on this brief: the
 * Summary group uses the editor's section name, "Profile" / "Hồ sơ"). */
export function importSectionName(
  locale: Locale,
  key: ImportSectionKey,
): string {
  return editorSectionsCopy[locale].sectionTypes[key];
}

/** A language level's name in the interface language, or undefined when the
 * level was not read. Levels are 1 to 5; index 0 of the catalog is "None". */
export function languageLevelName(
  locale: Locale,
  level: number | undefined,
): string | undefined {
  if (level === undefined) return undefined;
  return editorFieldsCopy[locale].levels.language[level];
}

// --- Entry text ------------------------------------------------------------

function stringField(
  entry: Readonly<Record<string, unknown>>,
  key: string,
): string | undefined {
  const value = entry[key];
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function numberField(
  entry: Readonly<Record<string, unknown>>,
  key: string,
): number | undefined {
  const value = entry[key];
  return typeof value === 'number' ? value : undefined;
}

function dateRangeField(
  entry: Readonly<Record<string, unknown>>,
): DateRange | undefined {
  const value = entry.dates;
  return typeof value === 'object' && value !== null
    ? value as DateRange
    : undefined;
}

function joinParts(parts: readonly (string | undefined)[]): string {
  return parts.filter((part): part is string => part !== undefined)
    .join(' · ');
}

/**
 * An entry row's label, per docs/design/linkedin-import-ui.md's entry table.
 * The Profile row's label is the fixed `summaryEntry` copy key, not derived
 * from the entry, so callers handle that section on their own.
 */
export function entryLabel(
  key: ImportSectionKey,
  entry: Readonly<Record<string, unknown>>,
): string {
  switch (key) {
    case 'work':
      return joinParts([
        stringField(entry, 'jobTitle'), stringField(entry, 'employer'),
      ]);
    case 'education': return stringField(entry, 'school') ?? '';
    case 'skill': return stringField(entry, 'name') ?? '';
    case 'language': return stringField(entry, 'name') ?? '';
    case 'certificate': return stringField(entry, 'title') ?? '';
    case 'profile': return '';
  }
}

/**
 * An entry row's description, per the same table. The document's language
 * is always English (ADR 0064, decision 7), so dates print in English
 * regardless of the interface language; the language level name is the
 * interface language's own word.
 */
export function entryDescription(
  key: ImportSectionKey,
  entry: Readonly<Record<string, unknown>>,
  locale: Locale,
  dateFormat: DateFormat,
): string | undefined {
  switch (key) {
    case 'work': {
      const dates = dateRangeField(entry);
      const datesText = dates === undefined
        ? undefined
        : formatDateRange(dates, dateFormat, DOCUMENT_LNG);
      const city = stringField(entry, 'city');
      const country = stringField(entry, 'country');
      const location = [city, country]
        .filter((part): part is string => part !== undefined)
        .join(', ') || undefined;
      const line = joinParts([datesText, location]);
      return line === '' ? undefined : line;
    }
    case 'education': {
      const degree = stringField(entry, 'degree');
      const dates = dateRangeField(entry);
      const datesText = dates === undefined
        ? undefined
        : formatDateRange(dates, dateFormat, DOCUMENT_LNG);
      const line = joinParts([degree, datesText]);
      return line === '' ? undefined : line;
    }
    case 'language':
      return languageLevelName(locale, numberField(entry, 'level'));
    case 'skill':
    case 'certificate':
    case 'profile':
      return undefined;
  }
}

export type EntryIndicator = 'invalid' | 'noDates' | 'cut' | undefined;

/**
 * The third line an entry row adds, per "Review state": an entry that fails
 * the schema check shows `entryInvalid` instead of any notice mark. A date
 * notice (no dates, or a start date only) takes priority over a cut mark,
 * since a missing date matters more than a shortened field; only one line
 * shows.
 */
export function entryIndicator(
  entry: ReviewEntry,
  invalidEntryIds: ReadonlySet<string>,
): EntryIndicator {
  if (invalidEntryIds.has(entry.id)) return 'invalid';
  if (entry.marks.some(
    (mark) => mark.kind === 'noDates' || mark.kind === 'startOnly',
  )) {
    return 'noDates';
  }
  if (entry.marks.some((mark) => mark.kind === 'cut')) return 'cut';
  return undefined;
}

// --- Notices -----------------------------------------------------------

export type NoticeLine
  = | { kind: 'dates'; entry: string }
    | { kind: 'startOnly'; entry: string }
    | { kind: 'cut'; field: string; max: number }
    | { kind: 'overLimit'; section: string };

/**
 * Every notice line for the review's "Notices" banner: cut name/headline
 * fields, then each section's date and cut marks in entry order, then any
 * section over the 64-entry limit.
 */
export function buildNoticeLines(
  locale: Locale,
  review: ImportReview,
): NoticeLine[] {
  const lines: NoticeLine[] = [];
  for (const mark of review.nameMarks) {
    if (mark.kind === 'cut') {
      lines.push({
        kind: 'cut', field: cutFieldName(locale, mark.field), max: mark.max,
      });
    }
  }
  for (const section of review.sections) {
    for (const entry of section.entries) {
      const label = entryLabel(section.key, entry.entry);
      for (const mark of entry.marks) {
        if (mark.kind === 'noDates') {
          lines.push({ kind: 'dates', entry: label });
        } else if (mark.kind === 'startOnly') {
          lines.push({ kind: 'startOnly', entry: label });
        } else if (mark.kind === 'cut') {
          lines.push({
            kind: 'cut', field: cutFieldName(locale, mark.field), max: mark.max,
          });
        }
      }
    }
    if (section.overLimit) {
      lines.push({
        kind: 'overLimit', section: importSectionName(locale, section.key),
      });
    }
  }
  return lines;
}

// --- Rich text as plain text -----------------------------------------------

const ENTITIES: ReadonlyArray<readonly [RegExp, string]> = [
  [/&lt;/g, '<'],
  [/&gt;/g, '>'],
  [/&quot;/g, '"'],
  [/&#39;/g, '\''],
  // &amp; decodes last, so an already-decoded "&lt;" is never re-decoded.
  [/&amp;/g, '&'],
];

function decodeEntities(text: string): string {
  let result = text;
  for (const [pattern, replacement] of ENTITIES) {
    result = result.replace(pattern, replacement);
  }
  return result;
}

/**
 * The plain-text view of the escaped `p`/`ul`/`li` HTML that build.ts
 * produces for rich text (docs/design/linkedin-import-ui.md, "Entry rows
 * show plain text only"): list items start "• ", paragraphs and items each
 * end a line, and HTML entities decode back to their characters. The result
 * is shown as a text node, never parsed as HTML, so a decoded "<script>" or
 * "<img>" string stays inert text.
 */
export function richTextToPlainText(html: string): string {
  const withBreaks = html
    .replace(/<ul>/g, '')
    .replace(/<\/ul>/g, '')
    .replace(/<li>/g, '• ')
    .replace(/<\/li>/g, '\n')
    .replace(/<p>/g, '')
    .replace(/<\/p>/g, '\n');
  return decodeEntities(withBreaks).replace(/\n+$/, '');
}
