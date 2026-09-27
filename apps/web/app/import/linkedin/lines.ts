// Turns pdf.js text items into per-page lines, and finds and removes the
// page footer. See docs/design/linkedin-import.md, "Reading the file" >
// "Lines".

/**
 * The fields of a pdf.js TextItem the parser reads; y is relative to the
 * page's bottom edge.
 */
export interface TextItemLike {
  str: string;
  transform: readonly number[];
  width: number;
  height: number;
}

export interface Line {
  page: number;
  x: number;
  y: number;
  size: number;
  text: string;
}

const FOOTER_PATTERN = /^Page (\d+) of (\d+)$/;
const FOOTER_MAX_Y = 72;
/** The column boundary lies this far left of the name's left edge. */
export const COLUMN_BOUNDARY_MARGIN = 12;

function roundHalf(value: number): number {
  return Math.round(value * 2) / 2;
}

interface PositionedItem {
  str: string;
  x: number;
  y: number;
  size: number;
  width: number;
}

function toPositionedItems(items: readonly TextItemLike[]): PositionedItem[] {
  const positioned: PositionedItem[] = [];
  for (const item of items) {
    if (item.str.trim() === '') continue;
    const t = item.transform;
    positioned.push({
      str: item.str,
      x: t[4],
      y: t[5],
      size: Math.hypot(t[2], t[3]),
      width: item.width,
    });
  }
  return positioned;
}

interface Group {
  baseline: number;
  items: PositionedItem[];
}

/** Groups items whose baselines lie within 40% of the larger font size. */
function groupIntoLines(items: readonly PositionedItem[]): Group[] {
  const groups: Group[] = [];
  for (const item of items) {
    const group = groups.find((g) => {
      const tolerance = 0.4 * Math.max(g.items[0].size, item.size);
      return Math.abs(g.baseline - item.y) <= tolerance;
    });
    if (group) {
      group.items.push(item);
    } else {
      groups.push({ baseline: item.y, items: [item] });
    }
  }
  return groups;
}

/** Joins a line's items left to right, inserting a space over a wide gap. */
function joinText(items: readonly PositionedItem[]): string {
  const ordered = [...items].sort((a, b) => a.x - b.x);
  let text = '';
  for (let i = 0; i < ordered.length; i++) {
    const current = ordered[i];
    if (i === 0) {
      text = current.str;
      continue;
    }
    const previous = ordered[i - 1];
    const gap = current.x - (previous.x + previous.width);
    const threshold = 0.25 * Math.max(previous.size, current.size);
    const previousEndsWithSpace = /\s$/.test(text);
    const currentStartsWithSpace = /^\s/.test(current.str);
    if (gap > threshold && !previousEndsWithSpace && !currentStartsWithSpace) {
      text += ' ';
    }
    text += current.str;
  }
  return text.replace(/\s+/g, ' ').trim().normalize('NFC');
}

/**
 * The column boundary from page 1's items: the left edge of the largest
 * text (the name) minus 12 pt. Undefined when the page has no text.
 */
export function columnBoundary(
  items: readonly TextItemLike[],
): number | undefined {
  let name: PositionedItem | undefined;
  for (const item of toPositionedItems(items)) {
    if (
      name === undefined
      || item.size > name.size
      || (item.size === name.size && item.y > name.y)
    ) {
      name = item;
    }
  }
  return name === undefined ? undefined : name.x - COLUMN_BOUNDARY_MARGIN;
}

/**
 * One page's items to lines, top to bottom. With a column boundary, items on
 * either side never join one line, even when a sidebar line and a main line
 * share a baseline.
 */
export function buildPageLines(
  page: number,
  items: readonly TextItemLike[],
  boundary?: number,
): Line[] {
  const positioned = toPositionedItems(items);
  const groups = boundary === undefined
    ? groupIntoLines(positioned)
    : [
        ...groupIntoLines(positioned.filter((item) => item.x < boundary)),
        ...groupIntoLines(positioned.filter((item) => item.x >= boundary)),
      ];
  const lines: Line[] = groups.map((group) => {
    const text = joinText(group.items);
    const minX = Math.min(...group.items.map((i) => i.x));
    const maxSize = Math.max(...group.items.map((i) => i.size));
    return {
      page,
      x: roundHalf(minX),
      y: roundHalf(group.items[0].y),
      size: roundHalf(maxSize),
      text,
    };
  });
  lines.sort((a, b) => (b.y !== a.y ? b.y - a.y : a.x - b.x));
  return lines;
}

/**
 * Removes the page footer; undefined when it is missing or its numbers are
 * wrong.
 */
export function stripFooter(
  lines: readonly Line[],
  page: number,
  numPages: number,
): Line[] | undefined {
  const footers = lines.filter(
    (line) => line.y <= FOOTER_MAX_Y && FOOTER_PATTERN.test(line.text),
  );
  if (footers.length !== 1) return undefined;
  const match = FOOTER_PATTERN.exec(footers[0].text);
  if (!match) return undefined;
  const n = Number(match[1]);
  const m = Number(match[2]);
  if (n !== page || m !== numPages) return undefined;
  return lines.filter((line) => line !== footers[0]);
}
