// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfColumnsMatch, pdfGlyphGeometry,
  type PdfTextItem, type PdfTextPage } from '../e2e/pdf-display';
import { pdfHasFill, pdfPaintOperators } from '../e2e/pdf-paint';

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
