// @vitest-environment node

import { describe, expect, it } from 'vitest';

import {
  type MeasuredBlock,
  paginate,
} from '../../app/components/resume/paginate';

const mulberry32 = (seed: number): (() => number) => {
  let state = seed | 0;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let value = Math.imul(state ^ (state >>> 15), 1 | state);
    value = (value + Math.imul(value ^ (value >>> 7), 61 | value)) ^ value;
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
};

const blockIdentity = (block: MeasuredBlock): string =>
  `${block.column}:${block.sectionKey}:${block.kind}:${block.entryIndex}`;

describe('paginate adversarial properties', () => {
  it(
    'preserves every generated block exactly once and stays deterministic',
    () => {
      const random = mulberry32(0x51a17e3d);
      let sawOversizedHeader = false;
      let sawPairOnlyOverflow = false;

      for (let caseIndex = 0; caseIndex < 256; caseIndex += 1) {
        const columns = random() < 0.5 ? 1 : 2;
        const capacity = 100 + Math.floor(random() * 101);
        const headerHeight = caseIndex % 17 === 0
          ? capacity + 1 + Math.floor(random() * 40)
          : Math.floor(random() * 41);
        const headerGap = Math.floor(random() * 13);
        const blocks: MeasuredBlock[] = [];

        const sourceColumns = ['main', 'sidebar'] as const;
        for (const sourceColumn of sourceColumns) {
          if (columns === 2 || sourceColumn === 'main') {
            const sectionCount = Math.floor(random() * 8);
            for (
              let sectionIndex = 0;
              sectionIndex < sectionCount;
              sectionIndex += 1
            ) {
              const column = columns === 1 ? 'main' : sourceColumn;
              const sectionKey = `${sourceColumn}-${sectionIndex}`;
              blocks.push({
                sectionKey,
                kind: 'heading',
                column,
                heightPx: 1 + Math.floor(random() * 80),
                gapBeforePx: Math.floor(random() * 16),
              });
              const entryCount = 1 + Math.floor(random() * 3);
              for (
                let entryIndex = 0;
                entryIndex < entryCount;
                entryIndex += 1
              ) {
                blocks.push({
                  sectionKey,
                  kind: 'entry',
                  entryIndex,
                  column,
                  heightPx: 1 + Math.floor(random() * 80),
                  gapBeforePx: Math.floor(random() * 16),
                });
              }
            }
          }
        }

        if (columns === 1) {
          const sidebarSections = 1 + Math.floor(random() * 4);
          for (
            let sectionIndex = 0;
            sectionIndex < sidebarSections;
            sectionIndex += 1
          ) {
            const sectionKey = `sidebar-${sectionIndex}`;
            blocks.push({
              sectionKey,
              kind: 'heading',
              column: 'main',
              heightPx: 1 + Math.floor(random() * 80),
              gapBeforePx: Math.floor(random() * 16),
            });
            blocks.push({
              sectionKey,
              kind: 'entry',
              entryIndex: 0,
              column: 'main',
              heightPx: 1 + Math.floor(random() * 80),
              gapBeforePx: Math.floor(random() * 16),
            });
          }
        }

        const first = paginate(
          blocks,
          columns,
          capacity,
          headerHeight,
          headerGap,
        );
        const second = paginate(
          structuredClone(blocks),
          columns,
          capacity,
          headerHeight,
          headerGap,
        );
        expect(second).toEqual(first);
        expect(first[0]?.header).toBeDefined();
        expect(first.slice(1).every((page) => page.header === undefined))
          .toBe(true);
        const firstBodyLength
          = (first[0]?.main.length ?? 0) + (first[0]?.sidebar.length ?? 0);
        const firstHasBody = firstBodyLength > 0;
        expect(first[0]?.header?.bodyGapPx).toBe(
          firstHasBody ? headerGap : 0,
        );
        if (headerHeight > capacity) {
          sawOversizedHeader = true;
          expect(first[0]).toMatchObject({
            contentHeightPx: headerHeight,
            overflow: true,
            main: [],
            sidebar: [],
          });
        }

        for (const column of ['main', 'sidebar'] as const) {
          const expected = blocks
            .filter((block) => block.column === column)
            .map(blockIdentity);
          const actual = first
            .flatMap((page) => page[column])
            .map(blockIdentity);
          expect(actual).toEqual(expected);
          expect(new Set(actual).size).toBe(actual.length);

          for (const page of first) {
            for (const [index, block] of page[column].entries()) {
              if (block.kind !== 'heading') continue;
              const following = page[column][index + 1];
              expect(following).toMatchObject({
                kind: 'entry',
                sectionKey: block.sectionKey,
              });
              const pairHeight = block.heightPx
                + (following?.heightPx ?? 0)
                + (following?.appliedGapBeforePx ?? 0);
              if (
                !block.overflow
                && following?.overflow === false
                && pairHeight > capacity
              ) {
                sawPairOnlyOverflow = true;
                expect(page.overflow).toBe(true);
              }
            }
          }
        }

        if (columns === 1) {
          expect(first.every((page) => page.sidebar.length === 0)).toBe(true);
          const keys = first.flatMap((page) => page.main)
            .filter((block) => block.kind === 'heading')
            .map((block) => block.sectionKey);
          const firstSidebar = keys.findIndex(
            (key) => key.startsWith('sidebar-'),
          );
          expect(
            firstSidebar === -1
            || keys.slice(firstSidebar).every(
              (key) => key.startsWith('sidebar-'),
            ),
          ).toBe(true);
          expect(() => paginate(
            [{
              ...blocks[0]!,
              column: 'sidebar',
            }],
            1,
            capacity,
            headerHeight,
            headerGap,
          )).toThrowError(expect.objectContaining({
            code: 'non_normalized_one_column_flow',
          }));
        }

        for (const page of first) {
          if (!page.overflow) {
            expect(page.contentHeightPx).toBeLessThanOrEqual(capacity);
          }
        }

        const larger = paginate(
          blocks,
          columns,
          capacity + 50,
          headerHeight,
          headerGap,
        );
        expect(larger.length).toBeLessThanOrEqual(first.length);
      }

      expect(sawOversizedHeader).toBe(true);
      expect(sawPairOnlyOverflow).toBe(true);
    },
  );
});
