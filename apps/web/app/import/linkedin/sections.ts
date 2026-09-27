// Maps a LinkedIn Save to PDF layout's sections into plain drafts for the
// resume's mapped parts. See docs/design/linkedin-import.md, "Mapping",
// "Dates", and "What is dropped". build.ts turns these drafts into the
// resume document and applies the schema's field limits; this module keeps
// text as extracted.
import type { ColumnLine, Layout } from './layout';
import type { DateRange } from './dates';
import {
  matchDateLine,
  isDurationLine,
  parseDateRange,
  workDates,
  educationDates,
} from './dates';
import { linesToRichText } from './richText';

export interface ContactDraft {
  type: 'email' | 'phone' | 'linkedin' | 'website';
  value: string;
  label?: string;
}

export interface WorkDraft {
  employer: string;
  jobTitle: string;
  dates?: DateRange;
  dateNotice?: 'startOnly' | 'unreadable';
  city?: string;
  country?: string;
  description: string;
  descriptionCut: boolean;
}

export interface EducationDraft {
  school: string;
  degree?: string;
  dates?: DateRange;
  dateNotice?: 'unreadable';
}

export interface LanguageDraft {
  name: string;
  level?: number;
}

export interface ProfileDraft {
  fullName: string;
  headline: string;
  location?: string;
}

export interface ParsedProfile {
  profile: ProfileDraft;
  contacts: ContactDraft[];
  summary?: { html: string; cut: boolean };
  work: WorkDraft[];
  education: EducationDraft[];
  skills: string[];
  languages: LanguageDraft[];
  certificates: string[];
  dropped: { heading: string; count: number }[];
}

/**
 * Intro: headline is every intro line up to the location, joined with
 * spaces; the last of two or more intro lines is the location. One intro
 * line is headline only; no intro lines gives an empty headline.
 */
export function parseIntro(
  name: ColumnLine,
  intro: readonly ColumnLine[],
): ProfileDraft {
  const fullName = name.text;
  if (intro.length === 0) return { fullName, headline: '' };
  if (intro.length === 1) return { fullName, headline: intro[0]!.text };
  // intro.length is at least 2 here, so the last index is in range.
  const location = intro[intro.length - 1]!.text;
  const headline = intro
    .slice(0, -1)
    .map((line) => line.text)
    .join(' ');
  return { fullName, headline, location };
}

/**
 * Splits at the last comma: the right part is the country, the left is the
 * city.
 */
export function splitLocation(
  text: string,
): { city?: string; country?: string } {
  const commaIndex = text.lastIndexOf(',');
  if (commaIndex === -1) {
    const city = text.trim();
    return city === '' ? {} : { city };
  }
  const city = text.slice(0, commaIndex).trim();
  const country = text.slice(commaIndex + 1).trim();
  const result: { city?: string; country?: string } = {};
  if (city !== '') result.city = city;
  if (country !== '') result.country = country;
  return result;
}

/** A line ending with, or consisting only of, a trailing "(Label)". */
function trailingLabel(
  text: string,
): { prefix: string; label: string } | undefined {
  if (!text.endsWith(')')) return undefined;
  const openIndex = text.lastIndexOf('(');
  if (openIndex === -1) return undefined;
  return {
    prefix: text.slice(0, openIndex).trim(),
    label: text.slice(openIndex + 1, -1),
  };
}

const PHONE_RE = /^[\d\s+()-]+$/;

/**
 * Prepends https:// (upgrading http://), lowercases the host through URL
 * parsing, and applies the link rule: an https URL with a dotted host, no
 * user name or password, and no whitespace. Returns undefined when the
 * value fails the rule.
 */
function buildContactUrl(value: string): string | undefined {
  if (/\s/.test(value)) return undefined;
  const normalized = /^https:\/\//i.test(value)
    ? value
    : /^http:\/\//i.test(value)
      ? `https://${value.slice(7)}`
      : `https://${value}`;
  let parsed: URL;
  try {
    parsed = new URL(normalized);
  } catch {
    return undefined;
  }
  if (
    parsed.protocol !== 'https:'
    || !parsed.hostname.includes('.')
    || parsed.username !== ''
    || parsed.password !== ''
  ) {
    return undefined;
  }
  return parsed.href;
}

function classifyContact(
  value: string,
  label: string,
): ContactDraft | undefined {
  if (value !== '' && PHONE_RE.test(value)) {
    return { type: 'phone', value, label };
  }
  // A labeled email is still an email, never a link with a user name.
  if (value.includes('@')) return { type: 'email', value };
  const url = buildContactUrl(value);
  if (url === undefined) return undefined;
  return label.toLowerCase() === 'linkedin'
    ? { type: 'linkedin', value: url }
    : { type: 'website', value: url, label };
}

/**
 * Contact entries: an @ line with no label is an email on its own; other
 * lines join without spaces until a line ends with (Label) or is only
 * (Label). Gaps never separate entries; leftover text with no label at the
 * end of the section is dropped.
 */
export function parseContact(lines: readonly ColumnLine[]): ContactDraft[] {
  const contacts: ContactDraft[] = [];
  let buffer: string[] = [];
  for (const rawLine of lines) {
    const text = rawLine.text.trim();
    const label = trailingLabel(text);
    if (label === undefined && text.includes('@')) {
      buffer = [];
      contacts.push({ type: 'email', value: text });
      continue;
    }
    if (label !== undefined) {
      const value = buffer.join('') + label.prefix;
      buffer = [];
      const contact = classifyContact(value, label.label);
      if (contact !== undefined) contacts.push(contact);
      continue;
    }
    buffer.push(text);
  }
  return contacts;
}

const SIDEBAR_WRAP_GAP_FACTOR = 1.4;

/**
 * Other sidebar lists (Top Skills, Certifications, and dropped lists): a gap
 * over 1.4 times the line's size, or a null gap, starts a new entry.
 */
export function sidebarEntries(lines: readonly ColumnLine[]): string[] {
  const entries: string[] = [];
  let current: string[] = [];
  for (const line of lines) {
    const startsNewEntry
      = line.gap === null || line.gap > SIDEBAR_WRAP_GAP_FACTOR * line.size;
    if (startsNewEntry && current.length > 0) {
      entries.push(current.join(' '));
      current = [];
    }
    current.push(line.text);
  }
  if (current.length > 0) entries.push(current.join(' '));
  return entries;
}

const PROFICIENCY_LEVELS: Readonly<Record<string, number>> = Object.freeze({
  'elementary': 1,
  'limited working': 2,
  'professional working': 3,
  'full professional': 4,
  'native or bilingual': 5,
});

function proficiencyLevel(text: string): number | undefined {
  const normalized = text.trim().toLowerCase().replace(/\s+proficiency$/, '');
  return PROFICIENCY_LEVELS[normalized];
}

/** "Name (Proficiency)"; the last parenthesized group is the proficiency. */
function parseLanguageEntry(text: string): LanguageDraft {
  const match = /^(.*)\(([^()]*)\)\s*$/.exec(text.trim());
  if (match === null) return { name: text.trim(), level: undefined };
  // Both groups above are mandatory, so they exist on a match.
  return { name: match[1]!.trim(), level: proficiencyLevel(match[2]!) };
}

/** Languages: lines join with spaces until the parentheses close. */
export function parseLanguages(lines: readonly ColumnLine[]): LanguageDraft[] {
  const languages: LanguageDraft[] = [];
  let buffer: string[] = [];
  for (const line of lines) {
    buffer.push(line.text);
    if (buffer.join(' ').trim().endsWith(')')) {
      languages.push(parseLanguageEntry(buffer.join(' ')));
      buffer = [];
    }
  }
  if (buffer.length > 0) languages.push(parseLanguageEntry(buffer.join(' ')));
  return languages;
}

const LOCATION_GAP_FACTOR = 1.6;

interface ExperienceEntry {
  entryStart: number;
  jobTitle: string;
  employer: string;
  location?: string;
  descStart: number;
  dateIndex: number;
}

/**
 * Experience entries, per docs/design/linkedin-import.md "Experience": the
 * job title is the run of lines before a date line sharing the size of the
 * line right before it; the employer line (largest size in the section) or
 * a group's duration line and employer precede it; a role without its own
 * employer takes the group's; the location follows the date when its size
 * matches and its gap allows it; the description runs to the next entry.
 */
export function parseExperience(lines: readonly ColumnLine[]): WorkDraft[] {
  if (lines.length === 0) return [];
  const maxSize = Math.max(...lines.map((line) => line.size));
  const dateIndices: number[] = [];
  for (let i = 0; i < lines.length; i++) {
    if (matchDateLine(lines[i]!.text) !== undefined) dateIndices.push(i);
  }

  const entries: ExperienceEntry[] = [];
  let groupEmployer = '';

  for (const dateIndex of dateIndices) {
    const titleSize = lines[dateIndex - 1]?.size;
    let titleStart = dateIndex - 1;
    // titleStart > 0 was just checked, so titleStart - 1 is in range.
    while (titleStart > 0 && lines[titleStart - 1]!.size === titleSize) {
      titleStart--;
    }
    const jobTitle = lines
      .slice(titleStart, dateIndex)
      .map((line) => line.text)
      .join(' ');

    const beforeTitleIndex = titleStart - 1;
    let employer = groupEmployer;
    let entryStart = titleStart;
    if (beforeTitleIndex >= 0) {
      // beforeTitleIndex >= 0 and < titleStart <= lines.length here.
      const candidate = lines[beforeTitleIndex]!;
      if (candidate.size === maxSize) {
        employer = candidate.text;
        entryStart = beforeTitleIndex;
        groupEmployer = employer;
      } else if (isDurationLine(candidate.text)) {
        const employerIndex = beforeTitleIndex - 1;
        if (employerIndex >= 0) {
          const employerLine = lines[employerIndex]!;
          if (employerLine.size === maxSize) {
            employer = employerLine.text;
            entryStart = employerIndex;
            groupEmployer = employer;
          }
        }
      }
    }

    const afterDateIndex = dateIndex + 1;
    let location: string | undefined;
    let descStart = afterDateIndex;
    if (afterDateIndex < lines.length) {
      // afterDateIndex is in range by the check above; dateIndex came from
      // dateIndices, always a valid index into lines.
      const candidate = lines[afterDateIndex]!;
      const isLocation
        = candidate.size === lines[dateIndex]!.size
          && matchDateLine(candidate.text) === undefined
          && candidate.gap !== null
          && candidate.gap <= LOCATION_GAP_FACTOR * candidate.size;
      if (isLocation) {
        location = candidate.text;
        descStart = afterDateIndex + 1;
      }
    }

    entries.push({
      entryStart,
      jobTitle,
      employer,
      location,
      descStart,
      dateIndex,
    });
  }

  const work: WorkDraft[] = [];
  for (let i = 0; i < entries.length; i++) {
    const entry = entries[i]!;
    // i + 1 < entries.length was just checked, so it is in range too.
    const descEnd
      = i + 1 < entries.length ? entries[i + 1]!.entryStart : lines.length;
    const descriptionLines = lines
      .slice(entry.descStart, descEnd)
      .map((line) => ({ text: line.text, gap: line.gap }));
    const richText = linesToRichText(descriptionLines);

    // entry.dateIndex came from dateIndices, always a valid index.
    const dateLine = matchDateLine(lines[entry.dateIndex]!.text);
    const parsedDate = parseDateRange(dateLine?.datesText ?? '');
    const { dates, notice } = workDates(parsedDate);
    const splitLoc
      = entry.location === undefined ? {} : splitLocation(entry.location);

    work.push({
      employer: entry.employer,
      jobTitle: entry.jobTitle,
      dates,
      dateNotice: notice,
      city: splitLoc.city,
      country: splitLoc.country,
      description: richText.html,
      descriptionCut: richText.cut,
    });
  }
  return work;
}

/**
 * Education entries: a school line has the section's largest size;
 * consecutive school-size lines join as one school. The following lines
 * join with spaces; text after the last · is the date, stripped of its
 * parentheses; text before it, trimmed, is the degree as written.
 */
export function parseEducation(lines: readonly ColumnLine[]): EducationDraft[] {
  if (lines.length === 0) return [];
  const maxSize = Math.max(...lines.map((line) => line.size));
  const educations: EducationDraft[] = [];
  let i = 0;
  while (i < lines.length) {
    // i is in range by the enclosing while condition in each loop below.
    if (lines[i]!.size !== maxSize) {
      i++;
      continue;
    }
    const schoolLines: string[] = [];
    while (i < lines.length && lines[i]!.size === maxSize) {
      schoolLines.push(lines[i]!.text);
      i++;
    }
    const school = schoolLines.join(' ');
    const bodyLines: string[] = [];
    while (i < lines.length && lines[i]!.size !== maxSize) {
      bodyLines.push(lines[i]!.text);
      i++;
    }
    educations.push(parseEducationBody(school, bodyLines.join(' ')));
  }
  return educations;
}

function parseEducationBody(school: string, body: string): EducationDraft {
  const dotIndex = body.lastIndexOf('·');
  if (dotIndex === -1) return { school, degree: body.trim() };

  const degree = body.slice(0, dotIndex).trim();
  const datePart = body
    .slice(dotIndex + 1)
    .trim()
    .replace(/^\(|\)$/g, '');
  const { dates, notice } = educationDates(parseDateRange(datePart));
  return { school, degree, dates, dateNotice: notice };
}

function mainEntryCount(lines: readonly ColumnLine[]): number {
  if (lines.length === 0) return 0;
  const dateLineCount = lines.filter(
    (line) => matchDateLine(line.text) !== undefined,
  ).length;
  return dateLineCount > 0 ? dateLineCount : 1;
}

/**
 * Maps a whole layout to plain drafts. Sections not mapped to a resume part
 * (known or unknown) are listed in `dropped` with their entry count, per
 * "What is dropped". Repeated sections with the same heading append.
 */
export function parseProfile(layout: Layout): ParsedProfile {
  const contacts: ContactDraft[] = [];
  const skills: string[] = [];
  const languages: LanguageDraft[] = [];
  const certificates: string[] = [];
  const work: WorkDraft[] = [];
  const education: EducationDraft[] = [];
  const dropped: { heading: string; count: number }[] = [];
  const summaryLines: ColumnLine[] = [];
  let hadSummary = false;

  for (const section of layout.sidebar) {
    switch (section.heading) {
      case 'Contact':
        contacts.push(...parseContact(section.lines));
        break;
      case 'Top Skills':
        skills.push(...sidebarEntries(section.lines));
        break;
      case 'Languages':
        languages.push(...parseLanguages(section.lines));
        break;
      case 'Certifications':
        certificates.push(...sidebarEntries(section.lines));
        break;
      default:
        dropped.push({
          heading: section.heading,
          count: sidebarEntries(section.lines).length,
        });
    }
  }

  for (const section of layout.main) {
    switch (section.heading) {
      case 'Summary':
        hadSummary = true;
        summaryLines.push(...section.lines);
        break;
      case 'Experience':
        work.push(...parseExperience(section.lines));
        break;
      case 'Education':
        education.push(...parseEducation(section.lines));
        break;
      default:
        dropped.push({
          heading: section.heading,
          count: mainEntryCount(section.lines),
        });
    }
  }

  const profile = parseIntro(layout.name, layout.intro);
  const summary = hadSummary
    ? linesToRichText(
        summaryLines.map((line) => ({ text: line.text, gap: line.gap })),
      )
    : undefined;

  return {
    profile,
    contacts,
    summary,
    work,
    education,
    skills,
    languages,
    certificates,
    dropped,
  };
}
