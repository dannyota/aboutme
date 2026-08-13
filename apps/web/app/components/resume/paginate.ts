import type { Resume } from '@aboutme/schema';

import type { RenderContext } from './resolveRenderModel';
import type { ResumeStyles } from './useResumeStyles';

export interface BlockRef {
  sectionKey: string;
  kind: 'heading' | 'entry';
  entryIndex?: number;
  column: 'main' | 'sidebar';
}

export interface PaginationRequest {
  document: Resume;
  context: RenderContext;
  columns: 1 | 2;
  blocks: BlockRef[];
  page: ResumeStyles['page'];
}

export interface MeasuredBlock extends BlockRef {
  heightPx: number;
  gapBeforePx: number;
}

export interface PlacedBlock extends BlockRef {
  heightPx: number;
  appliedGapBeforePx: number;
  overflow: boolean;
}

export interface PlacedHeader {
  heightPx: number;
  bodyGapPx: number;
}

export interface Page {
  header?: PlacedHeader;
  main: PlacedBlock[];
  sidebar: PlacedBlock[];
  contentHeightPx: number;
  overflow: boolean;
}

export type PaginationErrorCode
  = | 'pagination_measurement_required'
    | 'invalid_measurement'
    | 'invalid_page_geometry'
    | 'non_normalized_one_column_flow';

export class PaginationError extends Error {
  readonly code: PaginationErrorCode;

  constructor(code: PaginationErrorCode, message: string) {
    super(message);
    this.name = 'PaginationError';
    this.code = code;
  }
}

interface FlowPage {
  blocks: PlacedBlock[];
  heightPx: number;
  overflow: boolean;
  sealed: boolean;
}

const emptyFlowPage = (): FlowPage => ({
  blocks: [],
  heightPx: 0,
  overflow: false,
  sealed: false,
});

const isFinitePositive = (value: number): boolean =>
  Number.isFinite(value) && value > 0;

const isFiniteNonNegative = (value: number): boolean =>
  Number.isFinite(value) && value >= 0;

const validateInputs = (
  blocks: readonly MeasuredBlock[],
  columns: 1 | 2,
  pageContentHeightPx: number,
  headerHeightPx: number,
  headerBodyGapPx: number,
): void => {
  if (columns !== 1 && columns !== 2) {
    throw new PaginationError(
      'invalid_page_geometry',
      'Pagination columns must be one or two.',
    );
  }
  if (!isFinitePositive(pageContentHeightPx)) {
    throw new PaginationError(
      'invalid_page_geometry',
      'Page content height must be positive and finite.',
    );
  }
  if (
    !isFiniteNonNegative(headerHeightPx)
    || !isFiniteNonNegative(headerBodyGapPx)
  ) {
    throw new PaginationError(
      'invalid_measurement',
      'Header measurements must be non-negative and finite.',
    );
  }
  for (const block of blocks) {
    if (
      !isFinitePositive(block.heightPx)
      || !isFiniteNonNegative(block.gapBeforePx)
    ) {
      throw new PaginationError(
        'invalid_measurement',
        'Block heights must be positive and gaps non-negative and finite.',
      );
    }
    if (columns === 1 && block.column === 'sidebar') {
      throw new PaginationError(
        'non_normalized_one_column_flow',
        'One-column pagination requires every block in the main flow.',
      );
    }
  }
};

const unitAt = (
  blocks: readonly MeasuredBlock[],
  index: number,
): readonly MeasuredBlock[] => {
  const block = blocks[index]!;
  const next = blocks[index + 1];
  if (
    block.kind === 'heading'
    && next?.kind === 'entry'
    && next.sectionKey === block.sectionKey
    && next.column === block.column
  ) {
    return [block, next];
  }
  return [block];
};

const freshUnitHeight = (unit: readonly MeasuredBlock[]): number =>
  unit.reduce(
    (height, block, index) =>
      height + block.heightPx + (index === 0 ? 0 : block.gapBeforePx),
    0,
  );

const appendUnit = (
  page: FlowPage,
  unit: readonly MeasuredBlock[],
  ordinaryCapacityPx: number,
): void => {
  const unitHeight = freshUnitHeight(unit);
  for (const [index, block] of unit.entries()) {
    const appliedGapBeforePx
      = page.blocks.length === 0 && index === 0 ? 0 : block.gapBeforePx;
    page.blocks.push({
      sectionKey: block.sectionKey,
      kind: block.kind,
      ...(block.entryIndex === undefined
        ? {}
        : { entryIndex: block.entryIndex }),
      column: block.column,
      heightPx: block.heightPx,
      appliedGapBeforePx,
      overflow: block.heightPx > ordinaryCapacityPx,
    });
    page.heightPx += appliedGapBeforePx + block.heightPx;
  }
  if (unitHeight > ordinaryCapacityPx) {
    page.overflow = true;
    page.sealed = true;
  }
};

const paginateFlow = (
  blocks: readonly MeasuredBlock[],
  firstPageCapacityPx: number,
  ordinaryCapacityPx: number,
): FlowPage[] => {
  const pages: FlowPage[] = [emptyFlowPage()];

  for (let index = 0; index < blocks.length;) {
    const unit = unitAt(blocks, index);
    const unitHeight = freshUnitHeight(unit);

    while (true) {
      let page = pages.at(-1)!;
      if (page.sealed) {
        page = emptyFlowPage();
        pages.push(page);
      }
      const pageIndex = pages.length - 1;
      const capacity
        = pageIndex === 0 ? firstPageCapacityPx : ordinaryCapacityPx;
      const leadingGap
        = page.blocks.length === 0 ? 0 : unit[0]!.gapBeforePx;
      const requiredHeight = page.heightPx + leadingGap + unitHeight;

      if (requiredHeight <= capacity) {
        appendUnit(page, unit, ordinaryCapacityPx);
        break;
      }
      if (page.blocks.length > 0) {
        pages.push(emptyFlowPage());
        continue;
      }
      if (pageIndex === 0 && firstPageCapacityPx < ordinaryCapacityPx) {
        pages.push(emptyFlowPage());
        continue;
      }

      appendUnit(page, unit, ordinaryCapacityPx);
      break;
    }

    index += unit.length;
  }

  return pages;
};

export function paginate(
  blocks: MeasuredBlock[],
  columns: 1 | 2,
  pageContentHeightPx: number,
  headerHeightPx: number,
  headerBodyGapPx: number,
): Page[] {
  validateInputs(
    blocks,
    columns,
    pageContentHeightPx,
    headerHeightPx,
    headerBodyGapPx,
  );

  const firstPageCapacityPx
    = headerHeightPx > pageContentHeightPx
      ? 0
      : pageContentHeightPx - headerHeightPx - headerBodyGapPx;
  const mainPages = paginateFlow(
    blocks.filter((block) => block.column === 'main'),
    firstPageCapacityPx,
    pageContentHeightPx,
  );
  const sidebarPages = columns === 2
    ? paginateFlow(
        blocks.filter((block) => block.column === 'sidebar'),
        firstPageCapacityPx,
        pageContentHeightPx,
      )
    : [emptyFlowPage()];
  const pageCount = Math.max(mainPages.length, sidebarPages.length, 1);

  return Array.from({ length: pageCount }, (_, index): Page => {
    const main = mainPages[index] ?? emptyFlowPage();
    const sidebar = sidebarPages[index] ?? emptyFlowPage();
    const bodyHeightPx = Math.max(main.heightPx, sidebar.heightPx);
    const hasBody = main.blocks.length > 0 || sidebar.blocks.length > 0;
    const header = index === 0
      ? {
          heightPx: headerHeightPx,
          bodyGapPx: hasBody ? headerBodyGapPx : 0,
        }
      : undefined;
    const contentHeightPx
      = bodyHeightPx
        + (header?.heightPx ?? 0)
        + (header?.bodyGapPx ?? 0);

    return {
      ...(header === undefined ? {} : { header }),
      main: main.blocks,
      sidebar: sidebar.blocks,
      contentHeightPx,
      overflow:
        main.overflow
        || sidebar.overflow
        || contentHeightPx > pageContentHeightPx,
    };
  });
}
