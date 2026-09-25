import type { Customization } from '@aboutme/schema';

import { PaginationError } from './paginate';

export type ResolvedPageGeometry = {
  marginXmm: number;
  marginYmm: number;
} & (
  | { format: 'a4'; widthPx: 794; heightPx: 1123 }
  | { format: 'letter'; widthPx: 816; heightPx: 1056 }
);

export function resolvePageGeometry(
  customization: Customization,
): ResolvedPageGeometry {
  const marginXmm = customization.spacing.pageMargin?.x ?? 15;
  const marginYmm = customization.spacing.pageMargin?.y ?? 15;
  return customization.pageFormat === 'a4'
    ? { format: 'a4', widthPx: 794, heightPx: 1123, marginXmm, marginYmm }
    : {
        format: 'letter',
        widthPx: 816,
        heightPx: 1056,
        marginXmm,
        marginYmm,
      };
}

export function pageContentHeightPx(page: ResolvedPageGeometry): number {
  if (
    !Number.isFinite(page.widthPx)
    || page.widthPx <= 0
    || !Number.isFinite(page.heightPx)
    || page.heightPx <= 0
    || !Number.isFinite(page.marginXmm)
    || page.marginXmm < 0
    || !Number.isFinite(page.marginYmm)
    || page.marginYmm < 0
  ) {
    throw new PaginationError(
      'invalid_page_geometry',
      'Page dimensions and margins must be finite with positive dimensions.',
    );
  }
  // Print's page content box is the exact paper height minus the margins
  // (docs/design/templates/print.md §2: 297mm A4, 11in Letter), not the
  // rounded 1123/1056 px editor box: the rounding made the preview's
  // capacity 0.48 px taller than what the PDF actually fits per page.
  const paperHeightPx = page.format === 'a4'
    ? (297 * 96) / 25.4
    : 11 * 96;
  const heightPx = paperHeightPx - ((2 * page.marginYmm * 96) / 25.4);
  if (!Number.isFinite(heightPx) || heightPx <= 0) {
    throw new PaginationError(
      'invalid_page_geometry',
      'Page margins must leave a positive finite content height.',
    );
  }
  return heightPx;
}
