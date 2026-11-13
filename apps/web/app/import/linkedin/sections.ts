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

export function parseIntro(
  name: ColumnLine,
  intro: readonly ColumnLine[],
): ProfileDraft {
  const fullName = name.text;
  if (intro.length === 0) return { fullName, headline: '' };
  if (intro.length === 1) return { fullName, headline: intro[0]!.text };
  const location = intro[intro.length - 1]!.text;
  const headline = intro
    .slice(0, -1)
    .map((line) => line.text)
    .join(' ');
  return { fullName, headline, location };
}

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
  if (value.includes('@')) return { type: 'email', value };
  const url = buildContactUrl(value);
  if (url === undefined) return undefined;
  return label.toLowerCase() === 'linkedin'
    ? { type: 'linkedin', value: url }
    : { type: 'website', value: url, label };
}

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

function parseLanguageEntry(text: string): LanguageDraft {
  const match = /^(.*)\(([^()]*)\)\s*$/.exec(text.trim());
  if (match === null) return { name: text.trim(), level: undefined };
  return { name: match[1]!.trim(), level: proficiencyLevel(match[2]!) };
}

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
    const descEnd
      = i + 1 < entries.length ? entries[i + 1]!.entryStart : lines.length;
    const descriptionLines = lines
      .slice(entry.descStart, descEnd)
      .map((line) => ({ text: line.text, gap: line.gap }));
    const richText = linesToRichText(descriptionLines);

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

export function parseEducation(lines: readonly ColumnLine[]): EducationDraft[] {
  if (lines.length === 0) return [];
  const maxSize = Math.max(...lines.map((line) => line.size));
  const educations: EducationDraft[] = [];
  let i = 0;
  while (i < lines.length) {
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
