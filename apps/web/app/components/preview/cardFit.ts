/**
 * Text fitting and accent tints for the link-preview card
 * (docs/design/link-preview-card.md, "Fitting the text" and "Accent
 * decoration"). The card page runs no script, so every size is decided while
 * the component renders, from estimated widths that are upper bounds of the
 * real Be Vietnam Pro advances.
 */

/** Width of the body box and of each text line, in px. */
export const CARD_LINE_WIDTH = 530;
/** Width left for the footer link after the mark and the gap, in px. */
export const CARD_FOOTER_LINK_WIDTH = 489;
export const CARD_NAME_SIZES = [72, 64, 58, 52] as const;
export const CARD_HEADLINE_SIZE = 32;
export const CARD_SITE = 'aboutme.vn';
export const CARD_LINK_PREFIX = `${CARD_SITE}/`;

const SPACE_WIDTH = 0.23;
const ELLIPSIS = '…';
const ELLIPSIS_WIDTH = 0.87;

const WIDTHS: readonly (readonly [number, string])[] = [
  [0.35, 'ijI.,:;\'’!`'],
  [0.55, 'flrtJ()[]{}"*/\\|~<>'],
  [0.8, 'dđĐABCDEFGHKLNPRSTUVXYZ0123456789-+=?$&^_'],
  [0.9, 'mwMOQ#%'],
  [1.06, 'W@'],
];

const CJK = new RegExp(
  '[\\p{Script=Han}\\p{Script=Hiragana}\\p{Script=Katakana}'
  + '\\p{Script=Hangul}\\u3000-\\u303f\\uff01-\\uff60]',
  'u',
);

const graphemes = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

const clustersOf = (text: string): string[] =>
  [...graphemes.segment(text)].map(({ segment }) => segment);

const isCJK = (cluster: string): boolean => CJK.test(cluster);

/** The estimated width of one grapheme cluster, in em. */
export function clusterWidth(cluster: string): number {
  if (cluster === ' ') return SPACE_WIDTH;
  if (cluster === ELLIPSIS) return ELLIPSIS_WIDTH;
  const first = String.fromCodePoint(
    cluster.normalize('NFD').codePointAt(0) ?? 0,
  );
  for (const [width, characters] of WIDTHS) {
    if (characters.includes(first)) return width;
  }
  if (/^[a-z]$/u.test(first)) return 0.67;
  if (isCJK(first)) return 1;
  return 1.26;
}

/** The estimated width of text on one line, in em. */
export const textWidth = (text: string): number =>
  clustersOf(text).reduce((sum, cluster) => sum + clusterWidth(cluster), 0);

interface Unit {
  clusters: string[];
  width: number;
  spaceBefore: boolean;
  /** Part of a unit that was wider than a line on its own. */
  split: boolean;
}

// Runs between spaces, with each CJK cluster a unit of its own.
function unitsOf(text: string): Unit[] {
  const units: Unit[] = [];
  let current: Unit | undefined;
  let space = false;
  for (const cluster of clustersOf(text)) {
    if (cluster === ' ') {
      current = undefined;
      space = true;
      continue;
    }
    const cjk = isCJK(cluster);
    if (current === undefined || cjk || isCJK(current.clusters.at(-1)!)) {
      current = { clusters: [], width: 0, spaceBefore: space, split: false };
      units.push(current);
      space = false;
    }
    current.clusters.push(cluster);
    current.width += clusterWidth(cluster);
    if (cjk) current = undefined;
  }
  return units;
}

// A unit wider than a line splits into cluster runs that each fit, as
// overflow-wrap: anywhere does.
function splitWide(units: Unit[], capacity: number): Unit[] {
  return units.flatMap((unit) => {
    if (unit.width <= capacity) return [unit];
    const runs: Unit[] = [];
    let run: Unit = { ...unit, clusters: [], width: 0, split: true };
    for (const cluster of unit.clusters) {
      const width = clusterWidth(cluster);
      if (run.clusters.length > 0 && run.width + width > capacity) {
        runs.push(run);
        run = { clusters: [], width: 0, spaceBefore: false, split: true };
      }
      run.clusters.push(cluster);
      run.width += width;
    }
    runs.push(run);
    return runs;
  });
}

// Greedy line breaking; each line is a list of units.
function layout(text: string, size: number): Unit[][] {
  const capacity = CARD_LINE_WIDTH / size;
  const lines: Unit[][] = [];
  let line: Unit[] = [];
  let width = 0;
  for (const unit of splitWide(unitsOf(text), capacity)) {
    const added = unit.width + (unit.spaceBefore ? SPACE_WIDTH : 0);
    if (line.length > 0 && width + added <= capacity) {
      line.push(unit);
      width += added;
      continue;
    }
    if (line.length > 0) lines.push(line);
    line = [unit];
    width = unit.width;
  }
  if (line.length > 0) lines.push(line);
  return lines;
}

/** The number of lines text takes at size px in a 530 px line. */
export const lineCount = (text: string, size: number): number =>
  layout(text, size).length;

const joinUnits = (units: readonly Unit[]): string =>
  units.map((unit, index) =>
    (index > 0 && unit.spaceBefore ? ' ' : '') + unit.clusters.join(''))
    .join('');

/**
 * Text cut to two lines at size px: line 1 whole, then as much of line 2 as
 * leaves room for the ellipsis. Text that fits returns unchanged.
 */
export function cutToTwoLines(text: string, size: number): string {
  const lines = layout(text, size);
  if (lines.length <= 2) return text;
  const capacity = CARD_LINE_WIDTH / size;
  const rest = lines.slice(1).flat();
  const kept: Unit[] = [];
  let width = ELLIPSIS_WIDTH;
  for (const unit of rest) {
    const space = kept.length > 0 && unit.spaceBefore ? SPACE_WIDTH : 0;
    if (width + space + unit.width <= capacity) {
      kept.push(unit);
      width += space + unit.width;
      continue;
    }
    if (kept.length === 0 || unit.split) {
      const part: Unit = { ...unit, clusters: [], width: 0 };
      for (const cluster of unit.clusters) {
        const clusterEm = clusterWidth(cluster);
        if (width + space + part.width + clusterEm > capacity) break;
        part.clusters.push(cluster);
        part.width += clusterEm;
      }
      if (part.clusters.length > 0) kept.push(part);
    }
    break;
  }
  const first = joinUnits(lines[0]!);
  const second = joinUnits(kept);
  const joined = kept[0]?.spaceBefore ? `${first} ${second}` : first + second;
  return joined.replace(/[,;:–\- ]+$/u, '') + ELLIPSIS;
}

export interface FittedName {
  size: number;
  text: string;
}

/** The first name size at which the name takes at most two lines. */
export function fitName(name: string): FittedName {
  for (const size of CARD_NAME_SIZES) {
    if (lineCount(name, size) <= 2) return { size, text: name };
  }
  const size = CARD_NAME_SIZES.at(-1)!;
  return { size, text: cutToTwoLines(name, size) };
}

export const fitHeadline = (headline: string): string =>
  cutToTwoLines(headline, CARD_HEADLINE_SIZE);

const clampSize = (value: number, low: number, high: number): number =>
  Math.min(high, Math.max(low, Math.floor(value)));

/** Size of the two-line link that stands in for a missing name. */
export const linkSize = (slug: string): number => clampSize(
  CARD_LINE_WIDTH
  / Math.max(textWidth(CARD_LINK_PREFIX), textWidth(slug)),
  19,
  72,
);

/** Size of the footer link text, aboutme.vn/<slug> or aboutme.vn. */
export const footerSize = (text: string): number => clampSize(
  CARD_FOOTER_LINK_WIDTH / textWidth(text),
  14,
  24,
);

const linearChannel = (value: number): number => {
  const channel = value / 255;
  return channel <= 0.04045
    ? channel / 12.92
    : ((channel + 0.055) / 1.055) ** 2.4;
};

/** WCAG relative luminance of a #rrggbb color. */
export function relativeLuminance(color: string): number {
  const [r, g, b] = [1, 3, 5].map((start) =>
    linearChannel(Number.parseInt(color.slice(start, start + 2), 16)));
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}

export interface AccentTints {
  bandOuter: string;
  bandInner: string;
  dot: string;
}

/**
 * The accent's share, as a CSS percentage, in each tint mixed with white in
 * linear light, so every tint lands on a fixed luminance whatever the accent.
 */
export function accentTints(accent: string): AccentTints {
  const luminance = Math.min(relativeLuminance(accent), 0.99);
  const share = (target: number): string => {
    const percent = Math.min(100, ((1 - target) / (1 - luminance)) * 100);
    return `${Math.round(percent * 10) / 10}%`;
  };
  return { bandOuter: share(0.82), bandInner: share(0.985), dot: share(0.6) };
}
