import type { Section } from '@aboutme/schema';

import type {
  MeasuredLayout,
  MeasurePagination,
} from '../../app/components/resume/measure';

const sectionEntry = (section: Section, index: number): unknown =>
  section.entries[index];

export const syntheticMeasure: MeasurePagination = (request) => {
  const previousByColumn = new Map<'main' | 'sidebar', {
    kind: 'heading' | 'entry';
    sectionKey: string;
  }>();
  const blocks = request.blocks.map((block) => {
    const section = request.document.content[block.sectionKey];
    const value = block.kind === 'heading'
      ? section?.displayName ?? block.sectionKey
      : section === undefined || block.entryIndex === undefined
        ? ''
        : JSON.stringify(sectionEntry(section, block.entryIndex));
    const previous = previousByColumn.get(block.column);
    const gapBeforePx = block.kind === 'heading'
      ? request.document.customization.spacing.sectionGap
      : previous?.kind === 'heading'
        && previous.sectionKey === block.sectionKey
        ? request.document.customization.spacing.sectionGap * 0.4
        : request.document.customization.spacing.entryGap;
    previousByColumn.set(block.column, block);
    return {
      ...block,
      heightPx: (block.kind === 'heading' ? 24 : 36) + (value.length * 0.12),
      gapBeforePx,
    };
  });

  return {
    columns: request.columns,
    headerHeightPx:
      48 + (JSON.stringify(request.document.personalDetails).length * 0.08),
    headerBodyGapPx: request.document.customization.spacing.sectionGap,
    blocks,
  } satisfies MeasuredLayout;
};
