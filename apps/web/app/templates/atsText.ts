import type { Resume, Section } from '@aboutme/schema';

import { formatDateRange } from '../components/resume/formatDate';

/**
 * A resume as plain text, in the order an applicant tracking system reads the
 * document: the header, then the main column, then the sidebar. Hidden
 * entries and details are left out, as the renderer leaves them out.
 */
export function atsText(document: Resume, lng: string): string {
  const lines: string[] = [];
  const { personalDetails, customization, content } = document;
  const format = customization.dateFormat;
  const range = (dates: Parameters<typeof formatDateRange>[0] | undefined) =>
    dates === undefined ? '' : formatDateRange(dates, format, lng);
  push(lines, personalDetails.fullName);
  push(lines, personalDetails.headline);
  push(lines, (personalDetails.details ?? [])
    .filter((detail) => !detail.isHidden)
    .map((detail) => detail.value)
    .join(' | '));
  const keys = [
    ...customization.layout.sections.main,
    ...customization.layout.sections.sidebar,
  ];
  for (const key of keys) {
    const section = content[key];
    if (section === undefined) continue;
    const entries = section.entries.filter((entry) => !entry.isHidden);
    if (entries.length === 0) continue;
    lines.push('');
    push(lines, section.displayName?.toUpperCase());
    for (const entry of entries) sectionLines(lines, section, entry, range);
  }
  return lines.join('\n').trim();
}

type Entry = Section['entries'][number];
type Range = (dates: Parameters<typeof formatDateRange>[0] | undefined) =>
string;

function sectionLines(
  lines: string[],
  section: Section,
  entry: Entry,
  range: Range,
): void {
  const value = entry as unknown as Record<string, unknown>;
  const text = (key: string) =>
    typeof value[key] === 'string' ? value[key] as string : undefined;
  const dates = value.dates as Parameters<typeof formatDateRange>[0]
    | undefined;
  switch (section.sectionType) {
    case 'profile':
      push(lines, richText(text('text')));
      return;
    case 'work':
      push(lines, joined([text('jobTitle'), text('employer')], ', '));
      push(lines, joined([range(dates), text('city'), text('country')], ' · '));
      push(lines, richText(text('description')));
      return;
    case 'education':
      push(lines, joined([text('degree'), text('school')], ', '));
      push(lines, joined([range(dates), text('city'), text('country')], ' · '));
      push(lines, richText(text('description')));
      return;
    case 'skill':
    case 'language':
      push(lines, text('name'));
      push(lines, richText(text('infoHtml')));
      return;
    case 'certificate': {
      const date = value.date as { y: number; m?: number } | undefined;
      push(lines, joined([text('title'), text('issuer')], ', '));
      push(lines, date === undefined
        ? undefined
        : range({ start: date, end: null, present: false }));
      push(lines, richText(text('description')));
      return;
    }
    default:
      push(lines, text('title'));
      push(lines, text('subtitle'));
      push(lines, joined([range(dates), text('city')], ' · '));
      push(lines, richText(text('description')));
  }
}

function push(lines: string[], line: string | undefined): void {
  if (line !== undefined && line.trim() !== '') lines.push(line.trim());
}

function joined(parts: readonly (string | undefined)[], by: string): string {
  return parts.filter((part) => part !== undefined && part !== '').join(by);
}

const ENTITIES: Readonly<Record<string, string>> = {
  '&amp;': '&',
  '&lt;': '<',
  '&gt;': '>',
  '&quot;': '"',
  '&#39;': '\'',
  '&nbsp;': ' ',
};

/** Sanitized rich text as plain lines: list items become "- " lines. */
export function richText(html: string | undefined): string | undefined {
  if (html === undefined) return undefined;
  return html
    .replace(/<li[^>]*>/gu, '\n- ')
    .replace(/<\/(?:p|li|ul|ol)>|<br\s*\/?>/gu, '\n')
    .replace(/<[^>]+>/gu, '')
    .replace(/&(?:amp|lt|gt|quot|#39|nbsp);/gu, (entity) => ENTITIES[entity]!)
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
    .join('\n');
}
