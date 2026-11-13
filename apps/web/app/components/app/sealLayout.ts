import type { Locale } from '@/i18n/locale';

export const STAMP_HEIGHT = 72;
export const STAMP_MIN_WIDTH = 156;
export const STAMP_MAX_WIDTH = 260;
export const LINK_INSET = 20;

export const SEAL_WORD: Record<Locale, string> = {
  en: 'PUBLIC',
  vi: 'CÔNG KHAI',
};

const WORD_WIDTH: Record<Locale, number> = { en: 94.8, vi: 146.2 };
const TRAILING_TRACKING = 2.8;
const MARK_WIDTH = 26;
const MARK_GAP = 8;

export const LINK_ADVANCE: Readonly<Record<string, number>> = {
  'a': 7, 'b': 7, 'c': 7, 'd': 7, 'e': 7, 'f': 4.5, 'g': 7, 'h': 7, 'i': 3,
  'j': 3, 'k': 6, 'l': 3, 'm': 10, 'n': 6, 'o': 7, 'p': 7, 'q': 7, 'r': 5,
  's': 6, 't': 5, 'u': 6, 'v': 6, 'w': 10, 'x': 7, 'y': 7, 'z': 6,
  '0': 8, '1': 5, '2': 7, '3': 7, '4': 8, '5': 7, '6': 8, '7': 7, '8': 7,
  '9': 8, '-': 7, '.': 4, '/': 4.1,
};
const WIDEST_ADVANCE = 10;
const LINK_SAFETY = 1.04;

export function linkWidth(text: string): number {
  let width = 0;
  for (const char of text) width += LINK_ADVANCE[char] ?? WIDEST_ADVANCE;
  return width * LINK_SAFETY;
}

export interface SealLayout {
  readonly width: number;
  readonly headX: number;
  readonly wordX: number;
  readonly linkLength: number | undefined;
  readonly viewBox: string;
  readonly svgWidth: number;
  readonly svgHeight: number;
}

export function sealLayout(
  link: string,
  locale: Locale,
  rotate: number,
): SealLayout {
  const wordWidth = WORD_WIDTH[locale] - TRAILING_TRACKING;
  const headWidth = MARK_WIDTH + MARK_GAP + wordWidth;
  const natural = linkWidth(`aboutme.vn${link}`);
  const width = Math.min(
    STAMP_MAX_WIDTH,
    Math.max(
      STAMP_MIN_WIDTH,
      Math.ceil(natural + 2 * LINK_INSET),
      Math.ceil(headWidth + 48),
    ),
  );
  const linkRoom = width - 2 * LINK_INSET;
  const headX = (width - headWidth) / 2;

  const radians = (Math.abs(rotate) % 180) * (Math.PI / 180);
  const halfX = (width / 2) * Math.abs(Math.cos(radians))
    + (STAMP_HEIGHT / 2) * Math.abs(Math.sin(radians)) + 2;
  const halfY = (width / 2) * Math.abs(Math.sin(radians))
    + (STAMP_HEIGHT / 2) * Math.abs(Math.cos(radians)) + 2;
  const svgWidth = Math.ceil(halfX * 2);
  const svgHeight = Math.ceil(halfY * 2);
  const minX = width / 2 - svgWidth / 2;
  const minY = STAMP_HEIGHT / 2 - svgHeight / 2;

  return {
    width,
    headX,
    wordX: headX + MARK_WIDTH + MARK_GAP,
    linkLength: natural > linkRoom ? linkRoom : undefined,
    viewBox: `${minX} ${minY} ${svgWidth} ${svgHeight}`,
    svgWidth,
    svgHeight,
  };
}
