/**
 * Geometry of the aboutme.vn seal mark (docs/design/ui/identity-and-seal.md,
 * "Logo"; ADR 0020): a thick outer ring, a hairline inner ring, and a
 * single-story "a" tilted -8 degrees. `AppLogo` and the public page bar's
 * mark both draw this shape, so a logo change reaches both from one source.
 *
 * The path data sits in a 30 x 32 box so either component can scale it to
 * any height by width/height alone, without touching the coordinates.
 */

export const MARK_VIEW_BOX = '0 0 30 32';
export const MARK_WIDTH = 30;
export const MARK_HEIGHT = 32;

/** Tilts the ring and the letter together around the mark's own center. */
export const MARK_TILT = 'rotate(-8 15 16)';

export const MARK_OUTER_RING = { cx: 15, cy: 16, r: 13.4, strokeWidth: 2.6 };

/** Drops out at 24 px and below; a hairline this thin disappears under a
 *  pixel at that size (identity-and-seal.md, "Logo"). */
export const MARK_INNER_RING = { cx: 15, cy: 16, r: 9.6, strokeWidth: 1 };

/** The single-story "a": a bowl and a stem, stroked without a font. */
export const MARK_BOWL = { cx: 13.4, cy: 17.2, r: 3.7 };
export const MARK_STEM = { d: 'M17.1 13.2v7.8', strokeWidth: 3.2 };
