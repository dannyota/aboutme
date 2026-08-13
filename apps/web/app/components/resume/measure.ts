import type { InjectionKey } from 'vue';

import { fontsReady } from '../../utils/fontsReady';
import {
  type BlockRef,
  type MeasuredBlock,
  PaginationError,
  type PaginationRequest,
} from './paginate';

export interface MeasuredLayout {
  columns: 1 | 2;
  headerHeightPx: number;
  headerBodyGapPx: number;
  blocks: MeasuredBlock[];
}

export type MeasurePagination = (
  request: PaginationRequest,
) => MeasuredLayout | Promise<MeasuredLayout>;

export const PaginationMeasureKey: InjectionKey<MeasurePagination>
  = Symbol('aboutme-pagination-measure');

const measuredNumber = (
  value: number,
  allowZero: boolean,
  label: string,
): number => {
  const valid = Number.isFinite(value) && (allowZero ? value >= 0 : value > 0);
  if (!valid) {
    throw new PaginationError(
      'invalid_measurement',
      `${label} must be ${allowZero ? 'non-negative' : 'positive'} and finite.`,
    );
  }
  return value;
};

const cssPixels = (styles: CSSStyleDeclaration, name: string): number => {
  const raw = styles.getPropertyValue(name).trim();
  if (!raw.endsWith('px')) {
    throw new PaginationError(
      'invalid_measurement',
      `${name} must resolve to CSS pixels.`,
    );
  }
  return measuredNumber(Number(raw.slice(0, -2)), true, name);
};

const gapFor = (
  block: BlockRef,
  previous: BlockRef | undefined,
  sectionGapPx: number,
  headingGapPx: number,
  entryGapPx: number,
): number => {
  if (block.kind === 'heading') return sectionGapPx;
  return previous?.kind === 'heading'
    && previous.sectionKey === block.sectionKey
    ? headingGapPx
    : entryGapPx;
};

export async function measurePagination(
  root: HTMLElement,
  request: PaginationRequest,
): Promise<MeasuredLayout> {
  await fontsReady(
    request.document.customization.font.family,
    root.ownerDocument.fonts,
  );

  const styles = getComputedStyle(root);
  const sectionGapPx = cssPixels(styles, '--gap-section');
  const headingGapPx = cssPixels(styles, '--gap-heading');
  const entryGapPx = cssPixels(styles, '--gap-entry');
  const header = root.querySelector<HTMLElement>('[data-pagination-header]');
  if (header === null) {
    throw new PaginationError(
      'invalid_measurement',
      'The pagination measurement tree is missing its header.',
    );
  }
  const headerHeightPx = measuredNumber(
    header.getBoundingClientRect().height,
    true,
    'Header height',
  );
  const previousByColumn: Partial<Record<BlockRef['column'], BlockRef>> = {};
  const blocks = request.blocks.map((block, index): MeasuredBlock => {
    const element = root.querySelector<HTMLElement>(
      `[data-pagination-block-index="${index}"]`,
    );
    if (element === null) {
      throw new PaginationError(
        'invalid_measurement',
        `The pagination measurement tree is missing block ${index}.`,
      );
    }
    const measured: MeasuredBlock = {
      ...block,
      heightPx: measuredNumber(
        element.getBoundingClientRect().height,
        false,
        `Block ${index} height`,
      ),
      gapBeforePx: gapFor(
        block,
        previousByColumn[block.column],
        sectionGapPx,
        headingGapPx,
        entryGapPx,
      ),
    };
    previousByColumn[block.column] = block;
    return measured;
  });

  return {
    columns: request.columns,
    headerHeightPx,
    headerBodyGapPx: sectionGapPx,
    blocks,
  };
}
