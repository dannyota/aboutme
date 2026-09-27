// Builders for synthetic pdf.js-like text items and parser Line values, used
// across the LinkedIn import parser tests. Sizes and gaps here reuse the
// measured values in docs/design/linkedin-import.md so fixtures stay
// representative of a real Save to PDF export.
import type { TextItemLike, Line } from '../../../../app/import/linkedin/lines';
import type { ColumnLine } from '../../../../app/import/linkedin/layout';

/**
 * One text item at (x, y) in a font of the given size, y from the page
 * bottom.
 */
export function item(
  text: string,
  x: number,
  y: number,
  size: number,
  widthOverride?: number,
): TextItemLike {
  const width = widthOverride ?? text.length * size * 0.5;
  return {
    str: text,
    transform: [size, 0, 0, size, x, y],
    width,
    height: size,
  };
}

/** A synthetic footer item, "Page N of M", at the standard 9 pt footer size. */
export function footerItem(
  page: number,
  numPages: number,
  y = 40,
): TextItemLike {
  return item(`Page ${page} of ${numPages}`, 264, y, 9);
}

/** A Line value, for layout tests that build Line arrays directly. */
export function line(
  page: number,
  x: number,
  y: number,
  size: number,
  text: string,
): Line {
  return { page, x, y, size, text };
}

/** A ColumnLine value with an explicit gap. */
export function columnLine(
  page: number,
  x: number,
  y: number,
  size: number,
  text: string,
  gap: number | null,
): ColumnLine {
  return { page, x, y, size, text, gap };
}
