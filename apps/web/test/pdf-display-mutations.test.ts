// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfColumnsMatch, pdfGlyphGeometry, pdfSidebarPaint, pdfSurfacePaint,
  type PdfTextItem, type PdfTextPage } from '../e2e/pdf-display';
import { pdfHasFill, pdfPaintOperators,
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
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0', [bar]))
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
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0', [tag]))
        .toBe(true);
      expect(pdfSurfacePaint(paint, text, background, '#ffffff', [tag]))
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
      expect(pdfSurfacePaint(paint, text, background, '#e3ecf0', shapes))
        .toBe(true);
      const overpaint = await pdfPaintOperators(proxy, [
        OPS.setFillRGBColor, OPS.constructPath,
      ], [['#ffffff'], [OPS.fill, [rectangle(gap)]]]);
      expect(pdfSurfacePaint({ ...paint,
        fills: [...paint.fills, ...overpaint.fills] }, text, background,
      '#e3ecf0', shapes)).toBe(false);
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

  it('accepts inline chevron strokes beside a sidebar heading', async () => {
    const background = { left: 390.7499694824219,
      right: 555.66796875, top: 243.74998474121094, bottom: 804.94921875 };
    const text = textPage({ left: 404.8007681333006,
      right: 450.1021858805565, top: 245.58748955906253,
      bottom: 254.36548940421255 }, 11.549999218750012, 254.24998940625);
    const chevron = new Float32Array([
      0, 397.75, 247.875, 1, 400.375, 250.5, 1, 397.75, 253.125,
    ]);
    const rule = { ...background, top: 260.25, bottom: 261 };
    const paint = await pdfPaintOperators(proxy, [
      OPS.setFillRGBColor, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath, OPS.setStrokeRGBColor,
      OPS.setLineWidth, OPS.setLineCap, OPS.setLineJoin, OPS.constructPath,
    ], [
      ['#e3ecf0'], [OPS.fill, [rectangle(background)]],
      ['#8eb8c2'], [OPS.fill, [rectangle(rule)]], ['#4e5b62'],
      [0.765625], [1], [1], [OPS.stroke, [chevron]],
    ]);
    expect(pdfSurfacePaint(paint, text, background, '#e3ecf0', [rule]))
      .toBe(true);
    const overpaint = await pdfPaintOperators(proxy, [
      OPS.setFillRGBColor, OPS.constructPath,
    ], [['#ffffff'], [OPS.fill, [rectangle({ ...background,
      top: 500, bottom: 780 })]]]);
    expect(pdfSurfacePaint({ ...paint,
      fills: [...paint.fills, ...overpaint.fills] }, text, background,
    '#e3ecf0', [rule])).toBe(false);
  });

  it('accepts a partial link underline below its extracted text', async () => {
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
  });
});
