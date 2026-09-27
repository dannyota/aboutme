// Splits a LinkedIn Save to PDF export's lines into a sidebar/main layout,
// and checks the file is a LinkedIn PDF in English. See
// docs/design/linkedin-import.md, "Reading the file" > "Lines" and "Is it
// an English LinkedIn PDF?".
import type { Line } from './lines';
import { COLUMN_BOUNDARY_MARGIN, stripFooter } from './lines';

export const SIDEBAR_HEADINGS: readonly string[] = [
  'Contact',
  'Top Skills',
  'Languages',
  'Certifications',
  'Honors-Awards',
  'Publications',
  'Patents',
];

export const MAIN_HEADINGS: readonly string[] = [
  'Summary',
  'Experience',
  'Education',
  'Volunteer Experience',
  'Projects',
];

const ENGLISH_MARKER_HEADINGS = [
  'Contact',
  'Top Skills',
  'Summary',
  'Experience',
  'Education',
];

export interface ColumnLine extends Line {
  gap: number | null;
}

export interface Section {
  heading: string;
  known: boolean;
  lines: ColumnLine[];
}

export interface Layout {
  name: ColumnLine;
  intro: ColumnLine[];
  sidebar: Section[];
  main: Section[];
}

export type LayoutResult
  = | { kind: 'notLinkedIn' }
    | { kind: 'notEnglish' }
    | { kind: 'ok'; layout: Layout };

function median(values: readonly number[]): number {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0
    ? (sorted[mid - 1] + sorted[mid]) / 2
    : sorted[mid];
}

/**
 * Column lines in page order, each with its gap to the previous line of
 * that column on the same page.
 */
function buildColumnStream(
  pages: readonly (readonly Line[])[],
  inColumn: (line: Line) => boolean,
): ColumnLine[] {
  const stream: ColumnLine[] = [];
  for (const page of pages) {
    let previousY: number | null = null;
    for (const line of page) {
      if (!inColumn(line)) continue;
      const gap = previousY === null ? null : previousY - line.y;
      stream.push({ ...line, gap });
      previousY = line.y;
    }
  }
  return stream;
}

/**
 * The size at which a column's headings print, or undefined when no known
 * heading was found.
 */
function headingSizeOf(
  stream: readonly ColumnLine[],
  knownHeadings: readonly string[],
  medianSize: number,
): number | undefined {
  const candidateSizes = stream
    .filter(
      (line) => knownHeadings.includes(line.text) && line.size > medianSize,
    )
    .map((line) => line.size);
  return candidateSizes.length === 0 ? undefined : Math.max(...candidateSizes);
}

/**
 * Splits a column's lines (after any lines to drop, such as the name and
 * intro) into sections at each heading.
 */
function buildSections(
  lines: readonly ColumnLine[],
  headingSize: number,
  knownHeadings: readonly string[],
): Section[] {
  const sections: Section[] = [];
  let current: Section | undefined;
  for (const line of lines) {
    if (line.size === headingSize) {
      current = {
        heading: line.text,
        known: knownHeadings.includes(line.text),
        lines: [],
      };
      sections.push(current);
      continue;
    }
    current?.lines.push(line);
  }
  return sections;
}

/** pages[i] holds the lines of page i + 1, footers included. */
export function analyzeLayout(
  pages: readonly (readonly Line[])[],
): LayoutResult {
  if (pages.length === 0) return { kind: 'notLinkedIn' };

  const stripped: Line[][] = [];
  for (let i = 0; i < pages.length; i++) {
    const page = stripFooter(pages[i], i + 1, pages.length);
    if (page === undefined) return { kind: 'notLinkedIn' };
    stripped.push(page);
  }

  const page1 = stripped[0];
  if (page1.length === 0) return { kind: 'notLinkedIn' };

  const name = page1.reduce((best, line) =>
    line.size > best.size || (line.size === best.size && line.y > best.y)
      ? line
      : best,
  );
  const boundary = name.x - COLUMN_BOUNDARY_MARGIN;

  const allSizes = stripped.flat().map((line) => line.size);
  const med = median(allSizes);

  const sidebarStream = buildColumnStream(
    stripped,
    (line) => line.x < boundary,
  );
  const mainStream = buildColumnStream(stripped, (line) => line.x >= boundary);

  const sidebarHeadingSize = headingSizeOf(
    sidebarStream,
    SIDEBAR_HEADINGS,
    med,
  );
  const mainHeadingSize = headingSizeOf(mainStream, MAIN_HEADINGS, med);

  const sidebar
    = sidebarHeadingSize === undefined
      ? []
      : buildSections(sidebarStream, sidebarHeadingSize, SIDEBAR_HEADINGS);

  const nameIndex = mainStream.findIndex(
    (line) =>
      line.page === name.page && line.x === name.x && line.y === name.y,
  );
  const afterName
    = nameIndex === -1 ? mainStream : mainStream.slice(nameIndex + 1);
  const firstMainHeadingIndex
    = mainHeadingSize === undefined
      ? -1
      : afterName.findIndex((line) => line.size === mainHeadingSize);
  const intro
    = firstMainHeadingIndex === -1
      ? afterName
      : afterName.slice(0, firstMainHeadingIndex);
  const mainBody
    = firstMainHeadingIndex === -1
      ? []
      : afterName.slice(firstMainHeadingIndex);
  const main
    = mainHeadingSize === undefined
      ? []
      : buildSections(mainBody, mainHeadingSize, MAIN_HEADINGS);

  const foundMarkers = new Set(
    [...sidebar, ...main].filter((s) => s.known).map((s) => s.heading),
  );
  const markerCount = ENGLISH_MARKER_HEADINGS.filter((h) =>
    foundMarkers.has(h),
  ).length;
  if (markerCount < 2) return { kind: 'notEnglish' };

  const nameColumnLine: ColumnLine
    = nameIndex === -1 ? { ...name, gap: null } : mainStream[nameIndex];
  return { kind: 'ok', layout: { name: nameColumnLine, intro, sidebar, main } };
}
