// @vitest-environment node

import { describe, expect, it } from 'vitest';

import { pageContentHeightPx } from '../../app/components/resume/pageMetrics';
import {
  type MeasuredBlock,
  PaginationError,
  paginate,
} from '../../app/components/resume/paginate';

const entry = (
  sectionKey: string,
  heightPx: number,
  gapBeforePx = 0,
  column: 'main' | 'sidebar' = 'main',
  entryIndex = 0,
): MeasuredBlock => ({
  sectionKey,
  kind: 'entry',
  entryIndex,
  column,
  heightPx,
  gapBeforePx,
});

const heading = (
  sectionKey: string,
  heightPx: number,
  gapBeforePx = 0,
  column: 'main' | 'sidebar' = 'main',
): MeasuredBlock => ({
  sectionKey,
  kind: 'heading',
  column,
  heightPx,
  gapBeforePx,
});

describe('paginate', () => {
  it('returns one empty page with the page-one header', () => {
    expect(paginate([], 2, 100, 20, 10)).toEqual([
      {
        header: { heightPx: 20, bodyGapPx: 0 },
        main: [],
        sidebar: [],
        contentHeightPx: 20,
        overflow: false,
      },
    ]);
  });

  it(
    'reduces both first-page columns by the header and never repeats it',
    () => {
      const pages = paginate(
        [
          entry('main-a', 50),
          entry('main-b', 1),
          entry('side-a', 50, 0, 'sidebar'),
        ],
        2,
        100,
        40,
        10,
      );

      expect(pages).toHaveLength(2);
      expect(pages[0]).toMatchObject({
        header: { heightPx: 40, bodyGapPx: 10 },
        contentHeightPx: 100,
        overflow: false,
      });
      expect(pages[0]?.main.map((block) => block.sectionKey)).toEqual([
        'main-a',
      ]);
      expect(pages[0]?.sidebar.map((block) => block.sectionKey)).toEqual([
        'side-a',
      ]);
      expect(pages[1]).not.toHaveProperty('header');
      expect(pages[1]?.main.map((block) => block.sectionKey)).toEqual([
        'main-b',
      ]);
    },
  );

  it(
    'expands an oversized header and starts both body flows on page two',
    () => {
      const pages = paginate(
        [entry('main', 20), entry('side', 30, 0, 'sidebar')],
        2,
        100,
        120,
        12,
      );

      expect(pages).toHaveLength(2);
      expect(pages[0]).toEqual({
        header: { heightPx: 120, bodyGapPx: 0 },
        main: [],
        sidebar: [],
        contentHeightPx: 120,
        overflow: true,
      });
      expect(pages[1]?.main.map((block) => block.sectionKey)).toEqual(['main']);
      expect(pages[1]?.sidebar.map((block) => block.sectionKey)).toEqual([
        'side',
      ]);
    },
  );

  it('keeps exact fits and suppresses the leading gap after a break', () => {
    const pages = paginate(
      [
        entry('a', 40, 99, 'main', 0),
        entry('b', 50, 10, 'main', 1),
        entry('c', 1, 8, 'main', 2),
      ],
      1,
      100,
      0,
      0,
    );

    expect(pages).toHaveLength(2);
    expect(pages[0]?.contentHeightPx).toBe(100);
    expect(
      pages[0]?.main.map((block) => block.appliedGapBeforePx),
    ).toEqual([0, 10]);
    expect(pages[1]?.main[0]).toMatchObject({
      sectionKey: 'c',
      appliedGapBeforePx: 0,
    });
  });

  it(
    'moves a heading and first entry together instead of stranding the heading',
    () => {
      const pages = paginate(
        [entry('previous', 60), heading('next', 20, 5), entry('next', 15, 4)],
        1,
        100,
        0,
        0,
      );

      expect(pages).toHaveLength(2);
      expect(pages[0]?.main.map((block) => block.sectionKey)).toEqual([
        'previous',
      ]);
      expect(pages[1]?.main).toMatchObject([
        { sectionKey: 'next', kind: 'heading', appliedGapBeforePx: 0 },
        { sectionKey: 'next', kind: 'entry', appliedGapBeforePx: 4 },
      ]);
    },
  );

  it.each([
    {
      name: 'exact-fit pair',
      blocks: [heading('pair', 20), entry('pair', 75, 5)],
      expectedHeight: 100,
      expectedOverflow: false,
    },
    {
      name: 'pair-only overflow',
      blocks: [heading('pair', 60), entry('pair', 50, 10)],
      expectedHeight: 120,
      expectedOverflow: true,
    },
  ])(
    'places a $name atomically',
    ({ blocks, expectedHeight, expectedOverflow }) => {
      const [page] = paginate(blocks, 1, 100, 0, 0);
      expect(page?.contentHeightPx).toBe(expectedHeight);
      expect(page?.overflow).toBe(expectedOverflow);
      expect(page?.main.map((block) => block.overflow)).toEqual([
        false,
        false,
      ]);
    },
  );

  it(
    'gives an oversized block one dedicated expanded page and marks it',
    () => {
      const pages = paginate(
        [
          entry('before', 20),
          entry('oversized', 120, 5, 'main', 1),
          entry('after', 10, 5, 'main', 2),
        ],
        1,
        100,
        0,
        0,
      );

      expect(pages).toHaveLength(3);
      expect(pages[1]).toMatchObject({ contentHeightPx: 120, overflow: true });
      expect(pages[1]?.main).toMatchObject([
        {
          sectionKey: 'oversized',
          appliedGapBeforePx: 0,
          overflow: true,
        },
      ]);
      expect(pages[2]?.main[0]?.sectionKey).toBe('after');
    },
  );

  it(
    'paginates columns independently and pads the shorter flow',
    () => {
      const pages = paginate(
        [
          entry('main-a', 60),
          entry('main-b', 60, 0, 'main', 1),
          entry('side-a', 30, 0, 'sidebar'),
        ],
        2,
        100,
        0,
        0,
      );

      expect(pages).toHaveLength(2);
      expect(pages[0]?.main.map((block) => block.sectionKey)).toEqual([
        'main-a',
      ]);
      expect(pages[1]?.main.map((block) => block.sectionKey)).toEqual([
        'main-b',
      ]);
      expect(pages[0]?.sidebar.map((block) => block.sectionKey)).toEqual([
        'side-a',
      ]);
      expect(pages[1]?.sidebar).toEqual([]);
    },
  );

  it('rejects a sidebar flow in one-column mode', () => {
    expect(() => paginate([entry('side', 10, 0, 'sidebar')], 1, 100, 0, 0))
      .toThrowError(expect.objectContaining({
        code: 'non_normalized_one_column_flow',
      }));
  });

  it.each([
    ['zero block height', [entry('bad', 0)], 100, 0, 0],
    [
      'infinite block height',
      [entry('bad', Number.POSITIVE_INFINITY)],
      100,
      0,
      0,
    ],
    ['negative gap', [entry('bad', 10, -1)], 100, 0, 0],
    ['negative header', [], 100, -1, 0],
    ['infinite header gap', [], 100, 0, Number.POSITIVE_INFINITY],
  ])(
    'rejects invalid measurement: %s',
    (_name, blocks, capacity, headerHeight, headerGap) => {
      expect(() => paginate(blocks, 1, capacity, headerHeight, headerGap))
        .toThrowError(expect.objectContaining({ code: 'invalid_measurement' }));
    },
  );

  it.each([
    ['zero capacity', 0],
    ['negative capacity', -1],
    ['infinite capacity', Number.POSITIVE_INFINITY],
  ])('rejects invalid page geometry: %s', (_name, capacity) => {
    expect(() => paginate([], 1, capacity, 0, 0))
      .toThrowError(expect.objectContaining({
        code: 'invalid_page_geometry',
      }));
  });

  it('rejects a runtime column count outside the public union', () => {
    expect(() => paginate([], 3 as never, 100, 0, 0))
      .toThrowError(expect.objectContaining({
        code: 'invalid_page_geometry',
      }));
  });

  it('uses the typed pagination error', () => {
    expect(() => paginate([], 1, 0, 0, 0)).toThrow(PaginationError);
  });
});

describe('pageContentHeightPx', () => {
  const pxPerMm = 96 / 25.4;

  it.each([
    [
      'A4 default margin',
      {
        format: 'a4',
        widthPx: 794,
        heightPx: 1123,
        marginXmm: 15,
        marginYmm: 15,
      },
      1123 - (30 * pxPerMm),
    ],
    [
      'A4 explicit margin',
      {
        format: 'a4',
        widthPx: 794,
        heightPx: 1123,
        marginXmm: 9,
        marginYmm: 12,
      },
      1123 - (24 * pxPerMm),
    ],
    [
      'Letter default margin',
      {
        format: 'letter',
        widthPx: 816,
        heightPx: 1056,
        marginXmm: 15,
        marginYmm: 15,
      },
      1056 - (30 * pxPerMm),
    ],
    [
      'Letter zero margin',
      {
        format: 'letter',
        widthPx: 816,
        heightPx: 1056,
        marginXmm: 0,
        marginYmm: 0,
      },
      1056,
    ],
  ] as const)('computes %s', (_name, page, expected) => {
    expect(pageContentHeightPx(page)).toBeCloseTo(expected, 10);
  });

  it.each([
    [
      'non-positive width',
      {
        format: 'a4',
        widthPx: 0,
        heightPx: 1123,
        marginXmm: 15,
        marginYmm: 15,
      },
    ],
    [
      'non-positive height',
      {
        format: 'a4',
        widthPx: 794,
        heightPx: -1,
        marginXmm: 15,
        marginYmm: 15,
      },
    ],
    [
      'negative margin',
      {
        format: 'a4',
        widthPx: 794,
        heightPx: 1123,
        marginXmm: -1,
        marginYmm: 15,
      },
    ],
    [
      'non-finite margin',
      {
        format: 'a4',
        widthPx: 794,
        heightPx: 1123,
        marginXmm: 15,
        marginYmm: Number.NaN,
      },
    ],
    [
      'no content height',
      {
        format: 'a4',
        widthPx: 794,
        heightPx: 100,
        marginXmm: 15,
        marginYmm: 20,
      },
    ],
  ])('rejects invalid geometry: %s', (_name, page) => {
    expect(() => pageContentHeightPx(page as never))
      .toThrowError(expect.objectContaining({ code: 'invalid_page_geometry' }));
  });
});
