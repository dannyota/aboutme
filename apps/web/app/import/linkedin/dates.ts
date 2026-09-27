/**
 * LinkedIn "Save to PDF" date parsing, per docs/design/linkedin-import.md
 * "Dates" and "Experience".
 */

export interface YearMonth {
  y: number;
  m?: number;
}

export interface DateRange {
  start: YearMonth;
  end: YearMonth | null;
  present: boolean;
}

/** The parse of a dates text with any duration already removed. */
export type LinkedInDates
  = | { kind: 'range'; dates: DateRange }
    | { kind: 'startOnly'; start: YearMonth }
    | { kind: 'unreadable' };

const MONTHS: Readonly<Record<string, number>> = Object.freeze({
  jan: 1,
  january: 1,
  feb: 2,
  february: 2,
  mar: 3,
  march: 3,
  apr: 4,
  april: 4,
  may: 5,
  jun: 6,
  june: 6,
  jul: 7,
  july: 7,
  aug: 8,
  august: 8,
  sep: 9,
  sept: 9,
  september: 9,
  oct: 10,
  october: 10,
  nov: 11,
  november: 11,
  dec: 12,
  december: 12,
});

const MIN_YEAR = 1900;
const MAX_YEAR = 2100;

const SINGLE_DATE_RE = /^(?:([A-Za-z]+)\s+)?(\d{4})$/;
const RANGE_SPLIT_RE = /^(.+?)\s[-–]\s(.+)$/;
const PRESENT_RE = /^present$/i;

/**
 * `2020`, `Jan 2020`, `January 2020` (English, any case; `Sept` too). Year
 * 1900 to 2100.
 */
export function parseLinkedInDate(text: string): YearMonth | undefined {
  const trimmed = text.trim();
  const match = SINGLE_DATE_RE.exec(trimmed);
  if (match === null) return undefined;

  const [, monthWord, yearText] = match;
  const year = Number(yearText);
  if (year < MIN_YEAR || year > MAX_YEAR) return undefined;

  if (monthWord === undefined) return { y: year };

  const month = MONTHS[monthWord.toLowerCase()];
  if (month === undefined) return undefined;
  return { y: year, m: month };
}

/** Compares two year-months. A missing month counts as equal to any month. */
function compareYearMonth(a: YearMonth, b: YearMonth): number {
  if (a.y !== b.y) return a.y < b.y ? -1 : 1;
  if (a.m === undefined || b.m === undefined) return 0;
  if (a.m === b.m) return 0;
  return a.m < b.m ? -1 : 1;
}

/** The parse of a dates text with any duration already removed. */
export function parseDateRange(text: string): LinkedInDates {
  const trimmed = text.trim();
  const rangeMatch = RANGE_SPLIT_RE.exec(trimmed);

  if (rangeMatch === null) {
    const start = parseLinkedInDate(trimmed);
    if (start === undefined) return { kind: 'unreadable' };
    return { kind: 'startOnly', start };
  }

  // Both groups in RANGE_SPLIT_RE are mandatory, so they exist on any match.
  const [, startText, endText] = rangeMatch;
  const start = parseLinkedInDate(startText!);
  if (start === undefined) return { kind: 'unreadable' };

  if (PRESENT_RE.test(endText!.trim())) {
    return { kind: 'range', dates: { start, end: null, present: true } };
  }

  const end = parseLinkedInDate(endText!);
  if (end === undefined) return { kind: 'unreadable' };
  if (compareYearMonth(start, end) > 0) return { kind: 'unreadable' };

  return { kind: 'range', dates: { start, end, present: false } };
}

const DATE_PART = '(?:[A-Za-z]+\\s+)?\\d{4}';
const RANGE_CORE_RE = new RegExp(
  `^${DATE_PART}\\s[-–]\\s(?:${DATE_PART}|Present)$`,
  'i',
);
const SINGLE_DATE_SHAPE_RE = new RegExp(`^${DATE_PART}$`);
const TRAILING_PARENS_RE = /\s*\(([^)]*)\)\s*$/;

/**
 * A work date line: a date-shaped range (or single date) optionally followed
 * by a duration in parentheses. Returns the dates text without the duration,
 * or undefined when the line is not date-shaped.
 */
export function matchDateLine(text: string): { datesText: string } | undefined {
  const trimmed = text.trim();
  // Only a duration counts: `Summer 2020 (internship)` is not a date line.
  const parens = TRAILING_PARENS_RE.exec(trimmed);
  const hadDuration = parens !== null && isDurationLine(parens[1]!);
  const withoutDuration = hadDuration
    ? trimmed.slice(0, parens.index)
    : trimmed;

  if (RANGE_CORE_RE.test(withoutDuration)) {
    return { datesText: withoutDuration };
  }

  if (hadDuration && SINGLE_DATE_SHAPE_RE.test(withoutDuration)) {
    return { datesText: withoutDuration };
  }

  return undefined;
}

const LESS_THAN_A_YEAR_RE = /^less than a year$/i;
const DURATION_UNITS_RE
  = /^\d+\s+years?(?:\s+\d+\s+months?)?$|^\d+\s+months?$/i;

/**
 * A group duration line: only a duration without parentheses, e.g.
 * `2 years 1 month`, `9 months`, `1 year`, `less than a year`.
 */
export function isDurationLine(text: string): boolean {
  const trimmed = text.trim();
  return LESS_THAN_A_YEAR_RE.test(trimmed) || DURATION_UNITS_RE.test(trimmed);
}

/**
 * Work: range as is; startOnly -> no dates + notice 'startOnly'; unreadable
 * -> no dates + notice 'unreadable'.
 */
export function workDates(
  parsed: LinkedInDates,
): { dates?: DateRange; notice?: 'startOnly' | 'unreadable' } {
  switch (parsed.kind) {
    case 'range':
      return { dates: parsed.dates };
    case 'startOnly':
      return { notice: 'startOnly' };
    case 'unreadable':
      return { notice: 'unreadable' };
  }
}

/**
 * Education: startOnly -> {start, end: start, present: false} (a graduation
 * year); unreadable -> no dates + notice.
 */
export function educationDates(
  parsed: LinkedInDates,
): { dates?: DateRange; notice?: 'unreadable' } {
  switch (parsed.kind) {
    case 'range':
      return { dates: parsed.dates };
    case 'startOnly':
      return {
        dates: { start: parsed.start, end: parsed.start, present: false },
      };
    case 'unreadable':
      return { notice: 'unreadable' };
  }
}
