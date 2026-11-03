// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfColumnsMatch, pdfGlyphGeometry, pdfSidebarPaint, pdfSurfacePaint,
  type PdfTextItem, type PdfTextPage } from '../e2e/pdf-display';
import { pdfHasFill, pdfPaintMatch, pdfPaintOperators,
  type PdfBox } from '../e2e/pdf-paint';

const margins = { x: 10, y: 10 };
const layout = {
  header: { left: 10, right: 190, top: 10, bottom: 40 },
  sidebar: { left: 140, right: 190, top: 50, bottom: 190 },
};
function item(text: string, left: number, top = 20): PdfTextItem {
  return { text, x: left, y: 200 - top - 10, width: 30, height: 10,
    bounds: { left, right: left + 30, top, bottom: top + 10 } };
}
function page(items: readonly PdfTextItem[]): PdfTextPage {
  return { width: 200, height: 200,
    text: items.map(({ text }) => text).join(' '), items };
}

describe('PDF column mutations', () => {
  it('rejects the final main entry moved into the empty page-two sidebar',
    () => {
      const first = item('MAIN-ENTRY-08', 10);
      const last = item('MAIN-ENTRY-09 MAIN-END', 10, 50);
      expect(pdfColumnsMatch(page([first, last]), layout, margins, false))
        .toBe(true);
      expect(pdfColumnsMatch(page([first,
        item(last.text, 140, 50)]), layout, margins, false)).toBe(false);
    });

  it('rejects sidebar markers moved into the main column', () => {
    const texts = ['SIDEBAR-ENTRY-12', 'SIDEBAR-', 'SIDEBAR', 'SIDEBAR-END'];
    for (const text of texts) {
      expect(pdfColumnsMatch(page([item(text, 140)]), layout, margins, false))
        .toBe(true);
      expect(pdfColumnsMatch(page([item(text, 10)]), layout, margins, false))
        .toBe(false);
    }
  });

  it('checks main prefixes split before or after a hyphen', () => {
    for (const text of ['MAIN-', 'MAIN']) {
      expect(pdfColumnsMatch(page([item(text, 10)]), layout, margins, false))
        .toBe(true);
      expect(pdfColumnsMatch(page([item(text, 140)]), layout, margins, false))
        .toBe(false);
    }
    expect(pdfColumnsMatch(page([item('MAIN-END SIDEBAR-END', 10)]),
      layout, margins, false)).toBe(false);
  });
});

describe('PDF baseline mutations', () => {
  it('rejects coincident text instead of exempting its shared baseline', () => {
    const first = item('FIRST', 10);
    expect(pdfGlyphGeometry(page([first, item('SECOND', 41)]), margins)
      .separated).toBe(true);
    expect(pdfGlyphGeometry(page([first, { ...first, text: 'SECOND' }]),
      margins).separated).toBe(false);
  });

  it('rejects a cross-column collision on a shared baseline', () => {
    const main = item('MAIN-ENTRY-08', 110);
    const sidebar = item('SIDEBAR-ENTRY-12', 140);
    expect(pdfGlyphGeometry(page([main, sidebar]), margins).separated)
      .toBe(true);
    const collided = { ...main, width: 35,
      bounds: { ...main.bounds!, right: 145 } };
    expect(pdfGlyphGeometry(page([collided, sidebar]), margins).separated)
      .toBe(false);
    expect(pdfGlyphGeometry(page([{ ...main, width: 31,
      bounds: { ...main.bounds!, right: 141 } }, sidebar]), margins).separated)
      .toBe(true);
  });
});

describe('PDF overpaint mutations', () => {
  const proxy = { getViewport: () => ({
    width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
  }) } as unknown as PDFPageProxy;
  const box = { left: 10, right: 90, top: 30, bottom: 33 };
  const rectangle = new Float32Array([
    0, 10, 67, 1, 90, 67, 1, 90, 70, 1, 10, 70, 4,
  ]);

  it.each([1, 0.99])('rejects later white paint with alpha %s',
    async (alpha) => {
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath, OPS.setFillRGBColor,
        OPS.setGState, OPS.constructPath,
      ], [
        ['#0f6b7d'], [OPS.fill, [rectangle]], ['#ffffff'],
        [[['ca', alpha]]], [OPS.fill, [rectangle]],
      ]);
      expect(pdfHasFill({ ...paint, fills: [paint.fills[0]!] },
        box, '#0f6b7d')).toBe(true);
      expect(pdfHasFill(paint, box, '#0f6b7d')).toBe(false);
      expect(pdfHasFill({ ...paint, fills: [...paint.fills].reverse() },
        box, '#0f6b7d')).toBe(true);
    });

  it('checks the exposed track beside its later solid fill', async () => {
    const paint = await pdfPaintOperators(proxy, [
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath,
    ], [
      ['#cfe1e5'], [OPS.fill, [rectangle]], ['#0f6b7d'],
      [OPS.fill, [new Float32Array([
        0, 10, 67, 1, 74, 67, 1, 74, 70, 1, 10, 70, 4,
      ])]],
    ]);
    expect(pdfHasFill(paint, box, '#cfe1e5', { ...box, left: 74 }))
      .toBe(true);
    expect(pdfHasFill(paint, box, '#cfe1e5')).toBe(false);
    expect(pdfHasFill(paint, { ...box, right: 74 }, '#0f6b7d')).toBe(true);
  });
});

describe('PDF sidebar backgrounds', () => {
  const expected = { body: 'BODY', headings: [] };
  const proxy = { getViewport: () => ({
    width: 200, height: 200, transform: [1, 0, 0, -1, 0, 200],
  }) } as unknown as PDFPageProxy;
  const rectangle = (box: PdfBox) => new Float32Array([
    0, box.left, 200 - box.bottom, 1, box.right, 200 - box.bottom,
    1, box.right, 200 - box.top, 1, box.left, 200 - box.top, 4,
  ]);

  it.each([true, false])(
    'accepts tint behind a level bar on first page %s',
    async (firstPage) => {
      const background = { ...layout.sidebar, top: firstPage ? 50 : 10 };
      // A three-point full-width level bar crosses the first probe row.
      const bar = { ...background,
        top: firstPage ? 85 : 55, bottom: firstPage ? 88 : 58 };
      const text = page([item('SIDEBAR-ENTRY-12', 140,
        firstPage ? 60 : 20), item('BODY', 140, 100)]);
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath,
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
        ['#0f6b7d'], [OPS.fill, [rectangle(bar)]],
      ]);
      expect(pdfHasFill(paint, background, '#e3ecf0')).toBe(false);
      expect(pdfHasFill(paint, bar, '#0f6b7d')).toBe(true);
      expect(pdfSidebarPaint(paint, text, layout.sidebar,
        margins.y, firstPage, '#e3ecf0', expected)).toBe(true);
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0',
        [pdfPaintMatch(paint, bar, '#0f6b7d')!]))
        .toBe(true);
    },
  );

  it('rejects opaque overpaint in a blank surface region', async () => {
    const background = layout.sidebar;
    const blank = { left: 140, right: 190, top: 112, bottom: 185 };
    const text = page([item('TITLE', 140, 60),
      item('BODY', 140, 100)]);
    const paint = await pdfPaintOperators(proxy, [
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath,
    ], [
      ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
      ['#ffffff'], [OPS.fill, [rectangle(blank)]],
    ]);
    const clean = { ...paint, fills: [paint.fills[0]!] };
    expect(pdfSurfacePaint(clean, text, background, '#e3ecf0')).toBe(true);
    expect(pdfSidebarPaint(clean, text, background,
      margins.y, true, '#e3ecf0', expected)).toBe(true);
    expect(pdfSurfacePaint(paint, text, background, '#e3ecf0')).toBe(false);
    expect(pdfSidebarPaint(paint, text, background,
      margins.y, true, '#e3ecf0', expected)).toBe(false);
  });

  it('rejects overflow bar overpaint between text rows', async () => {
    const background = layout.sidebar;
    const bar = { left: 140, right: 190, top: 85, bottom: 88 };
    const text = page([item('SIDEBAR-ENTRY-12', 140, 60),
      item('BODY', 140, 100)]);
    const paint = await pdfPaintOperators(proxy, [
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath,
    ], [
      ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
      ['#0f6b7d'], [OPS.fill, [rectangle(bar)]],
      ['#ffffff'], [OPS.fill, [rectangle(bar)]],
    ]);
    const clean = { ...paint, fills: paint.fills.slice(0, 2) };
    expect(pdfSidebarPaint(clean, text, background,
      margins.y, true, '#e3ecf0', expected)).toBe(true);
    expect(pdfHasFill(paint, bar, '#0f6b7d')).toBe(false);
    expect(pdfSurfacePaint(paint, text, background, '#e3ecf0')).toBe(false);
    expect(pdfSidebarPaint(paint, text, background,
      margins.y, true, '#e3ecf0', expected)).toBe(false);
    expect(pdfSidebarPaint(clean, page([item('TITLE', 140, 60),
      item('BODY', 140, 100)]), background,
    margins.y, true, '#e3ecf0', expected)).toBe(false);
  });

  it('rejects an overflow bar moved below its entry body', async () => {
    const background = layout.sidebar;
    const text = page([item('SIDEBAR-ENTRY-12', 140, 60),
      item('BODY', 140, 100)]);
    for (const top of [85, 180]) {
      const bar = { left: 140, right: 190, top, bottom: top + 3 };
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath,
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
        ['#0f6b7d'], [OPS.fill, [rectangle(bar)]],
      ]);
      expect(pdfSidebarPaint(paint, text, background,
        margins.y, true, '#e3ecf0', expected)).toBe(top === 85);
    }
  });

  it('rejects extra overflow rules without matching headings', async () => {
    const background = layout.sidebar;
    const bar = { left: 140, right: 190, top: 85, bottom: 88 };
    const text = page([item('SIDEBAR-ENTRY-12', 140, 60),
      item('BODY', 140, 100)]);
    const paint = await pdfPaintOperators(proxy, [
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath, OPS.constructPath,
    ], [
      ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
      ['#0f6b7d'], [OPS.fill, [rectangle(bar)]], ['#8eb8c2'],
      [OPS.fill, [rectangle({ ...bar, top: 52, bottom: 52.75 })]],
      [OPS.fill, [rectangle({ ...bar, top: 55, bottom: 55.75 })]],
    ]);
    const clean = { ...paint, fills: paint.fills.slice(0, 2) };
    expect(pdfSidebarPaint(clean, text, background,
      margins.y, true, '#e3ecf0', expected)).toBe(true);
    expect(pdfSidebarPaint(paint, text, background,
      margins.y, true, '#e3ecf0', expected)).toBe(false);
  });

  it('requires one rule at the expected heading offset', async () => {
    const background = layout.sidebar;
    const heading = item('SKILLS', 140, 50);
    const text = page([heading, item('SIDEBAR-ENTRY-12', 140, 80),
      item('BODY', 140, 120)]);
    const bar = { left: 140, right: 190, top: 105, bottom: 108 };
    const rule = { left: 140, right: 190, top: 62.625, bottom: 63.375 };
    const rules = [
      [rule], [], [rule, rule],
      [{ ...rule, top: rule.top + 4, bottom: rule.bottom + 4 }],
      [rule, { ...rule, top: 52, bottom: 52.75 },
        { ...rule, top: 55, bottom: 55.75 }],
    ];
    for (const [index, shapes] of rules.entries()) {
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath, OPS.setFillRGBColor,
        ...shapes.map(() => OPS.constructPath),
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
        ['#0f6b7d'], [OPS.fill, [rectangle(bar)]], ['#8eb8c2'],
        ...shapes.map((shape) => [OPS.fill, [rectangle(shape)]]),
      ]);
      expect(pdfSidebarPaint(paint, text, background, margins.y, true,
        '#e3ecf0', { ...expected, headings: [heading.bounds!] }))
        .toBe(index === 0);
    }
  });

  it.each([1, 0.99])('rejects surface overpaint with alpha %s',
    async (alpha) => {
      const background = layout.sidebar;
      const text = page([item('SIDEBAR-ENTRY-12', 140, 60)]);
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath, OPS.setFillRGBColor,
        OPS.setGState, OPS.constructPath,
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]], ['#ffffff'],
        [[['ca', alpha]]], [OPS.fill, [rectangle(background)]],
      ]);
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0'))
        .toBe(false);
      expect(pdfSidebarPaint(paint, text, background,
        margins.y, true, '#e3ecf0', expected)).toBe(false);
    });

  it('checks surface text beside a separately asserted tag background',
    async () => {
      const background = layout.sidebar;
      const tag = { left: 140, right: 180, top: 78, bottom: 92 };
      const text = page([item('TITLE', 140, 60), item('4/5', 140, 80)]);
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath,
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
        ['#0f6b7d'], [OPS.fill, [rectangle(tag)]],
      ]);
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0'))
        .toBe(false);
      expect(pdfHasFill(paint, tag, '#0f6b7d')).toBe(true);
      const foreground = [pdfPaintMatch(paint, tag, '#0f6b7d')!];
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0', foreground))
        .toBe(true);
      expect(pdfSurfacePaint(paint, text, background, '#ffffff', foreground))
        .toBe(false);
    });

  it('keeps gaps between validated widget shapes in the surface probe',
    async () => {
      const background = layout.sidebar;
      const shapes = [
        { left: 140, right: 150, top: 80, bottom: 90 },
        { left: 160, right: 170, top: 80, bottom: 90 },
      ];
      const gap = { left: 150, right: 160, top: 80, bottom: 90 };
      const text = page([item('TITLE', 140, 60), item('BODY', 140, 100)]);
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath, OPS.setFillRGBColor,
        OPS.constructPath, OPS.constructPath,
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
        ['#0f6b7d'], [OPS.fill, [rectangle(shapes[0]!)]],
        [OPS.fill, [rectangle(shapes[1]!)]],
      ]);
      expect(shapes.every((shape) => pdfHasFill(paint, shape, '#0f6b7d')))
        .toBe(true);
      const foreground = shapes.map((shape) =>
        pdfPaintMatch(paint, shape, '#0f6b7d')!);
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0', foreground))
        .toBe(true);
      const overpaint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
      ], [['#ffffff'], [OPS.fill, [rectangle(gap)]]]);
      expect(pdfSurfacePaint({ ...paint,
        fills: [...paint.fills, ...overpaint.fills] }, text, background,
      '#e3ecf0', foreground)).toBe(false);
    });
});

describe('PDF surface decorations', () => {
  const height = 841.91998;
  const proxy = { getViewport: () => ({ width: 595.43999, height,
    transform: [1, 0, 0, 1, 0, 0],
  }) } as unknown as PDFPageProxy;
  const rectangle = (box: PdfBox) => new Float32Array([
    0, box.left, box.top, 1, box.right, box.top,
    1, box.right, box.bottom, 1, box.left, box.bottom, 4,
  ]);
  const textPage = (bounds: PdfBox, size: number, baseline: number) => ({
    width: 595.43999, height, text: 'TEXT', items: [{
      text: 'TEXT', x: bounds.left, y: height - baseline,
      width: bounds.right - bounds.left, height: size, bounds,
    }],
  });

  it.each([
    { color: '#f2ebdf', ink: '#5d5751', top: 158.25,
      bottom: 755.2499389648438, measuredTop: 158.88558070866142,
      measuredBottom: 755.5105807086615, size: 11.549999218750012,
      heading: { left: 400.7577958017578, right: 452.5770507702223,
        top: 503.25488645228575, bottom: 513.544424330513 },
      dotX: 389.3125, dotY: 502.625, width: 0.765625,
      lineX: 387.125, lineY: 503.9375, length: 5.25 },
    { color: '#f1f3f5', ink: '#63686f', top: 132.75,
      bottom: 783.7499389648438, measuredTop: 133.09261195866142,
      measuredBottom: 783.9871432086615, size: 10.724999703124993,
      heading: { left: 399.74998334375, right: 462.22207202414626,
        top: 376.1279139158936, bottom: 383.3786951559221 },
      dotX: 389.09375, dotY: 376.5625, width: 0.7109375,
      lineX: 387.0625, lineY: 377.78125, length: 4.875 },
  ])('keeps native icon gaps when surface edges differ for $color',
    async (shape) => {
      const background = { left: 386.2499694824219, right: 544.23046875,
        top: shape.top, bottom: shape.bottom };
      const measured = { left: 386.6134657972441, right: 544.5236220472441,
        top: shape.measuredTop, bottom: shape.measuredBottom };
      const text = textPage(shape.heading, shape.size,
        shape.heading.bottom);
      const paths = [
        new Float32Array([0, shape.dotX, shape.dotY,
          1, shape.dotX + shape.width * 4 / 7, shape.dotY]),
        new Float32Array([0, shape.lineX, shape.lineY,
          1, shape.lineX + shape.length, shape.lineY]),
      ];
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath, OPS.setStrokeRGBColor,
        OPS.setLineWidth, OPS.setLineCap,
        OPS.constructPath, OPS.constructPath,
      ], [
        [shape.color], [OPS.fill, [rectangle(background)]], [shape.ink],
        [shape.width], [1], ...paths.map((path) => [OPS.stroke, [path]]),
      ]);
      const icons = [
        { left: shape.dotX - shape.width / 2,
          right: shape.dotX + shape.width * 4 / 7 + shape.width / 2,
          top: shape.dotY - shape.width / 2,
          bottom: shape.dotY + shape.width / 2 },
        { left: shape.lineX - shape.width / 2,
          right: shape.lineX + shape.length + shape.width / 2,
          top: shape.lineY - shape.width / 2,
          bottom: shape.lineY + shape.width / 2 },
      ].map((box) => ({ ...box, color: shape.ink, stroke: true }));
      expect(pdfSurfacePaint(paint, text, measured, shape.color, [], icons))
        .toBe(true);
      const gap = { left: shape.dotX, right: shape.dotX + 0.3,
        top: shape.dotY + shape.width / 2 + 0.05,
        bottom: shape.lineY - shape.width / 2 - 0.05 };
      const patch = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
      ], [['#ffffff'], [OPS.fill, [rectangle(gap)]]]);
      expect(pdfSurfacePaint({ ...paint,
        fills: [...paint.fills, ...patch.fills] }, text, measured,
      shape.color, [], icons)).toBe(false);
    });

  it('uses native bounds for a separately validated heading rule',
    async () => {
      const background = { left: 386.2499694824219, right: 544.23046875,
        top: 158.25, bottom: 755.2499389648438 };
      const measured = { left: 386.6134657972441, right: 544.5236220472441,
        top: 158.88558070866142, bottom: 755.5105807086615 };
      const rule = { ...background, top: 174.74998474121094,
        bottom: 175.49998474121094 };
      const expected = { ...measured, top: 175.37386195866142,
        bottom: 176.12386195866142 };
      const text = textPage({ left: 400.7577958017578,
        right: 467.074871916146, top: 160.17772792358403,
        bottom: 168.88860494362712 }, 11.549999218750012, 168.75);
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath,
      ], [
        ['#f2ebdf'], [OPS.fill, [rectangle(background)]],
        ['#bc9d9c'], [OPS.fill, [rectangle(rule)]],
      ]);
      expect(pdfHasFill(paint, expected, '#bc9d9c')).toBe(true);
      const foreground = [pdfPaintMatch(paint, expected, '#bc9d9c')!];
      expect(pdfSurfacePaint(paint, text, measured, '#f2ebdf', foreground))
        .toBe(true);
      const stripe = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
      ], [['#0000ff'], [OPS.fill, [rectangle({ ...rule,
        top: 175.6, bottom: 177.1 })]]]);
      expect(pdfHasFill({ ...paint,
        fills: [...paint.fills, ...stripe.fills] }, expected, '#bc9d9c'))
        .toBe(true);
      expect(pdfSurfacePaint({ ...paint,
        fills: [...paint.fills, ...stripe.fills] }, text, measured,
      '#f2ebdf', foreground)).toBe(false);
    });

  it.each(['outline', 'filled'])(
    'validates final ink and holes in a captured %s contact icon',
    async (kind) => {
      const background = { left: 57, right: 538.91015625,
        top: 45, bottom: 200.24998474121094 };
      const outline = new Float32Array([
        0, 4, 4, 1, 20, 4,
        2, 20.552284, 4, 21.023689, 4.195262, 21.414213, 4.585786,
        2, 21.804737, 4.976311, 22, 5.447715, 22, 6, 1, 22, 18,
        2, 22, 18.552284, 21.804737, 19.023689, 21.414213, 19.414213,
        2, 21.023689, 19.804737, 20.552284, 20, 20, 20, 1, 4, 20,
        2, 3.447715, 20, 2.976310, 19.804737, 2.585786, 19.414213,
        2, 2.195262, 19.023689, 2, 18.552284, 2, 18, 1, 2, 6,
        2, 2, 5.447715, 2.195262, 4.976311, 2.585786, 4.585786,
        2, 2.976310, 4.195262, 3.447715, 4, 4, 4, 4,
      ]);
      const filled = new Float32Array([
        0, 416, 32, 1, 31.9, 32, 2, 14.3, 32, 0, 46.5, 0, 64.3,
        1, 0, 447.7, 2, 0, 465.5, 14.3, 480, 31.9, 480, 1, 416, 480,
        2, 433.6, 480, 448, 465.5, 448, 447.7, 1, 448, 64.3,
        2, 448, 46.5, 433.6, 32, 416, 32, 4,
        0, 135.4, 416, 1, 69, 416, 1, 69, 202.2, 1, 135.5, 202.2,
        1, 135.5, 416, 1, 135.4, 416, 4,
        0, 102.2, 96,
        2, 112.831474, 96, 121.906006, 99.758797, 129.423599, 107.276382,
        2, 136.941193, 114.793976, 140.699982, 123.868515, 140.7, 134.5,
        2, 140.699982, 145.131470, 136.941193, 154.206009, 129.423599,
        161.723602,
        2, 121.906006, 169.241196, 112.831474, 173, 102.2, 173,
        2, 91.568512, 173, 82.493973, 169.241196, 74.976379, 161.723602,
        2, 67.458786, 154.206009, 63.699989, 145.131470, 63.7, 134.5,
        2, 63.699989, 123.868515, 67.458786, 114.793976, 74.976379,
        107.276382,
        2, 82.493973, 99.758797, 91.568512, 96, 102.2, 96, 4,
        0, 384.3, 416, 1, 317.9, 416, 1, 317.9, 312,
        2, 317.9, 287.2, 317.4, 255.3, 283.4, 255.3,
        2, 248.8, 255.3, 243.5, 282.3, 243.5, 310.2,
        1, 243.5, 416, 1, 177.1, 416, 1, 177.1, 202.2,
        1, 240.8, 202.2, 1, 240.8, 231.4, 1, 241.7, 231.4,
        2, 250.6, 214.6, 272.3, 196.9, 304.6, 196.9,
        2, 371.8, 196.9, 384.3, 241.2, 384.3, 298.8, 1, 384.3, 416, 4,
      ]);
      const isOutline = kind === 'outline';
      const contact = textPage({ left: 85.6874964296875,
        right: isOutline ? 194.835736781844 : 203.5995533166849,
        top: isOutline ? 115.74096197433482 : 175.74095947433477,
        bottom: isOutline ? 126.26998375242238 : 186.26998125242233 },
      11.24999953125, isOutline ? 123.75 : 183.75);
      const headline = textPage({ left: 71.06249703906249,
        right: 395.40690084971163, top: 89.78960807517251,
        bottom: 101.89798311997322 }, 12.9374994609375, 99);
      const text = { ...contact,
        items: [...headline.items, ...contact.items] };
      const scale = isOutline ? 0.46875 : 0.02363445357523106;
      const x = isOutline ? 71.24999703125 : 71.5808754174634;
      const y = isOutline ? 113.99999525 : 173.574573568883;
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath, OPS.save, OPS.transform,
        OPS.setStrokeRGBColor, OPS.setFillRGBColor, OPS.setLineWidth,
        OPS.setLineCap, OPS.setLineJoin, OPS.constructPath, OPS.restore,
      ], [
        ['#f7e8d9'], [OPS.fill, [rectangle(background)]], [],
        [scale, 0, 0, scale, x, y], ['#615a53'], ['#615a53'], [1.75],
        [1], [1], [isOutline ? OPS.stroke : OPS.fill,
          [isOutline ? outline : filled]], [],
      ]);
      const icons = [{
        left: x + (isOutline ? 2 - 1.75 / 2 : 0) * scale,
        right: x + (isOutline ? 22 + 1.75 / 2 : 448) * scale,
        top: y + (isOutline ? 4 - 1.75 / 2 : 32) * scale,
        bottom: y + (isOutline ? 20 + 1.75 / 2 : 480) * scale,
        color: '#615a53', stroke: isOutline,
      }];
      expect(pdfSurfacePaint(paint, text, background, '#f7e8d9', [], icons))
        .toBe(true);
      for (const expected of [
        { ...icons[0]!, color: '#0000ff' },
        { ...icons[0]!, left: icons[0]!.left + 4,
          right: icons[0]!.right + 4 },
        { ...icons[0]!, right: icons[0]!.right - 4 },
      ]) {
        expect(pdfSurfacePaint(paint, text, background, '#f7e8d9', [],
          [expected])).toBe(false);
      }
      const icon = paint.fills[1]!;
      if (!isOutline) {
        expect(pdfSurfacePaint(paint, text, background, '#f7e8d9'))
          .toBe(false);
        const extra = await pdfPaintOperators(proxy, [
          OPS.setFillRGBColor, OPS.constructPath,
        ], [['#0000ff'], [OPS.fill, [rectangle({ left: 82.5, right: 85,
          top: 177, bottom: 184 })]]]);
        expect(pdfSurfacePaint({ ...paint,
          fills: [...paint.fills, ...extra.fills] }, text, background,
        '#f7e8d9', [], icons)).toBe(false);
      }
      for (const patch of [icon,
        { left: 75, right: 75.5, top: isOutline ? 119 : 179,
          bottom: isOutline ? 119.5 : 179.5 },
        { left: 100, right: 500, top: 190, bottom: 198 }]) {
        for (const color of ['#ffffff', '#0000ff']) {
          const overpaint = await pdfPaintOperators(proxy, [
            OPS.setFillRGBColor, OPS.constructPath,
          ], [[color], [OPS.fill, [rectangle(patch)]]]);
          expect(pdfSurfacePaint({ ...paint,
            fills: [...paint.fills, ...overpaint.fills] }, text, background,
          '#f7e8d9', [], icons), JSON.stringify({ kind, color, patch }))
            .toBe(false);
        }
      }
    });

  it.each(['single', 'overlapping', 'patched', 'gap-patched'])(
    'checks inline chevron paint with %s strokes',
    async (variant) => {
      const background = { left: 390.7499694824219,
        right: 555.66796875, top: 243.74998474121094, bottom: 804.94921875 };
      const text = textPage({ left: 404.8007681333006,
        right: 450.1021858805565, top: 245.58748955906253,
        bottom: 254.36548940421255 }, 11.549999218750012, 254.24998940625);
      const chevron = new Float32Array([
        0, 397.75, 247.875, 1, 400.375, 250.5, 1, 397.75, 253.125,
      ]);
      const strokes = variant === 'overlapping'
        ? [
            new Float32Array([0, 397.75, 247.875, 1, 400.375, 250.5]),
            new Float32Array([0, 400.375, 250.5, 1, 397.75, 253.125]),
          ]
        : [chevron];
      const rule = { ...background, top: 260.25, bottom: 261 };
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath, OPS.setStrokeRGBColor,
        OPS.setLineWidth, OPS.setLineCap, OPS.setLineJoin,
        ...strokes.map(() => OPS.constructPath),
      ], [
        ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
        ['#8eb8c2'], [OPS.fill, [rectangle(rule)]], ['#4e5b62'],
        [0.765625], [1], [1],
        ...strokes.map((path) => [OPS.stroke, [path]]),
      ]);
      const foreground = [pdfPaintMatch(paint, rule, '#8eb8c2')!];
      const icons = strokes.map((path) => {
        const x = Array.from(path).filter((_, index) => index % 3 === 1);
        const y = Array.from(path).filter((_, index) => index % 3 === 2);
        return { left: Math.min(...x) - 0.765625 / 2,
          right: Math.max(...x) + 0.765625 / 2,
          top: Math.min(...y) - 0.765625 / 2,
          bottom: Math.max(...y) + 0.765625 / 2,
          color: '#4e5b62', stroke: true };
      });
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0',
        foreground, icons))
        .toBe(true);
      if (variant.endsWith('patched')) {
        const box = variant === 'gap-patched'
          ? { left: 397.6, right: 398, top: 250.2, bottom: 250.8 }
          : { left: 398, right: 400, top: 249, bottom: 252 };
        const patch = await pdfPaintOperators(proxy, [
          OPS.setFillRGBColor, OPS.constructPath,
        ], [['#ffffff'], [OPS.fill, [rectangle(box)]]]);
        expect(pdfSurfacePaint({ ...paint,
          fills: [...paint.fills, ...patch.fills] }, text, background,
        '#e3ecf0', foreground, icons)).toBe(false);
      }
      const overpaint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
      ], [['#ffffff'], [OPS.fill, [rectangle({ ...background,
        top: 500, bottom: 780 })]]]);
      expect(pdfSurfacePaint({ ...paint,
        fills: [...paint.fills, ...overpaint.fills] }, text, background,
      '#e3ecf0', foreground, icons)).toBe(false);
    },
  );

  it.each([null, '#0000ff', '#ffffff'])(
    'checks a partial link underline with later paint %s',
    async (color) => {
      const background = { left: 62.25, right: 532.81640625,
        top: 51, bottom: 164.24998474121094 };
      const phone = textPage({ left: 245.41405227441405,
        right: 354.6500998479119, top: 120.99096175558475,
        bottom: 129.13500629234818 }, 11.24999953125, 128.999994625);
      const next = textPage({ left: 269.4374887734375,
        right: 315.9288769862962, top: 140.4909609430847,
        bottom: 148.6350054798482 }, 11.24999953125, 148.4999938125);
      const text = { ...phone, items: [...phone.items, ...next.items] };
      const underline = { left: 280.8046569824219, right: 354.6562194824219,
        top: 131.24998474121094, bottom: 131.99998474121094 };
      const paint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
        OPS.setFillRGBColor, OPS.constructPath,
      ], [
        ['#fff0eb'], [OPS.fill, [rectangle(background)]],
        ['#c23a14'], [OPS.fill, [rectangle(underline)]],
      ]);
      const linked = { ...paint, text: [{ text: 'LINK', color: '#c23a14',
        x: underline.left, y: 128.999994625, size: 11.24999953125,
        font: 'Test', bold: false }] };
      expect(pdfSurfacePaint(linked, text, background, '#fff0eb')).toBe(true);
      expect(pdfSurfacePaint(paint, text, background, '#fff0eb')).toBe(false);
      const detached = { ...text, items: text.items.map((item) => ({ ...item,
        y: item.y + 10 })) };
      expect(pdfSurfacePaint(linked, detached, background, '#fff0eb'))
        .toBe(false);
      if (color !== null) {
        const overpaint = await pdfPaintOperators(proxy, [
          OPS.setFillRGBColor, OPS.constructPath,
        ], [[color], [OPS.fill, [rectangle(underline)]]]);
        expect(pdfSurfacePaint({ ...linked,
          fills: [...linked.fills, ...overpaint.fills] }, text, background,
        '#fff0eb')).toBe(false);
      }
    },
  );
});
