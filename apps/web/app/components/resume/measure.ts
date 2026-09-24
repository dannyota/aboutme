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

const renderedScale = (root: HTMLElement): number => {
  const layoutWidth = root.offsetWidth;
  const renderedWidth = root.getBoundingClientRect().width;
  if (
    !Number.isFinite(layoutWidth)
    || layoutWidth <= 0
    || !Number.isFinite(renderedWidth)
    || renderedWidth <= 0
  ) {
    return 1;
  }
  return renderedWidth / layoutWidth;
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

const entryKey = (block: BlockRef): string =>
  `${block.column}:${block.sectionKey}:${String(block.entryIndex)}`;

/**
 * The body blocks of a whole rendered entry, in the order splitRichTextBlocks
 * yields them: each paragraph, each item of an unordered list, and each
 * ordered list whole.
 */
export function bodyBlockElements(body: Element): Element[] {
  return Array.from(body.children).flatMap((child) =>
    child.tagName === 'UL' ? Array.from(child.children) : [child]);
}

/**
 * The space between an entry's body blocks as the whole entry lays it out:
 * paragraph and list-item margins that a split part, rendered alone, loses.
 * Index k holds the gap before block k; index 0 is always 0. Returns
 * undefined when the rendered body does not have the expected block count,
 * so the caller falls back to no gap rather than guessing.
 */
const wholeEntryGaps = (
  whole: Element,
  parts: number,
  scale: number,
): number[] | undefined => {
  const body = whole.querySelector('.entry-body');
  if (body === null) return undefined;
  const elements = bodyBlockElements(body);
  if (elements.length !== parts) return undefined;
  const rects = elements.map((element) => element.getBoundingClientRect());
  return rects.map((rect, index) => {
    if (index === 0) return 0;
    const gap = (rect.top - rects[index - 1]!.bottom) / scale;
    return Number.isFinite(gap) && gap > 0 ? gap : 0;
  });
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
  const scale = renderedScale(root);
  const sectionGapPx = cssPixels(styles, '--gap-section');
  const headingGapPx = cssPixels(styles, '--gap-heading');
  const entryGapPx = cssPixels(styles, '--gap-entry');
  const headerGapPx = cssPixels(styles, '--gap-header');
  const header = root.querySelector<HTMLElement>('[data-pagination-header]');
  if (header === null) {
    throw new PaginationError(
      'invalid_measurement',
      'The pagination measurement tree is missing its header.',
    );
  }
  const headerHeightPx = measuredNumber(
    header.getBoundingClientRect().height / scale,
    true,
    'Header height',
  );
  const partCounts = new Map<string, number>();
  const firstPartIndex = new Map<string, number>();
  for (const [index, block] of request.blocks.entries()) {
    if (block.part === undefined) continue;
    const key = entryKey(block);
    partCounts.set(key, (partCounts.get(key) ?? 0) + 1);
    if (block.part === 0) firstPartIndex.set(key, index);
  }
  const gapsByEntry = new Map<string, number[] | undefined>();
  const partGap = (block: BlockRef): number => {
    const key = entryKey(block);
    if (!gapsByEntry.has(key)) {
      const whole = root.querySelector(
        `[data-pagination-whole-entry="${String(firstPartIndex.get(key))}"]`,
      );
      if (whole === null) {
        throw new PaginationError(
          'invalid_measurement',
          `The pagination measurement tree is missing entry ${key}.`,
        );
      }
      gapsByEntry.set(
        key,
        wholeEntryGaps(whole, partCounts.get(key) ?? 0, scale),
      );
    }
    return gapsByEntry.get(key)?.[block.part ?? 0] ?? 0;
  };
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
        element.getBoundingClientRect().height / scale,
        false,
        `Block ${index} height`,
      ),
      // A later part continues the same entry: its gap is the space the
      // whole entry leaves between the two body blocks, not an entry gap.
      gapBeforePx: (block.part ?? 0) > 0
        ? partGap(block)
        : gapFor(
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
    headerBodyGapPx: headerGapPx,
    blocks,
  };
}
