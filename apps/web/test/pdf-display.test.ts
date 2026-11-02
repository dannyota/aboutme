// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfTextPage } from '../e2e/pdf-display';
import { pdfHasFill, pdfPaintOperators } from '../e2e/pdf-paint';

const glyph = {
  str: 'A', dir: 'ltr', fontName: 'fixture', hasEOL: false,
  transform: [10, 0, 0, 10, 10, 80], width: 20, height: 10,
};

function textPage(
  items: readonly Partial<typeof glyph>[],
  style = { ascent: 0.8, descent: -0.2, vertical: false },
): PDFPageProxy {
  return {
    view: [0, 0, 100, 100],
    getViewport: () => ({ width: 100, height: 100 }),
    getTextContent: async () => ({
      items: items.map((item) => ({ ...glyph, ...item })),
      styles: { fixture: style },
    }),
  } as unknown as PDFPageProxy;
}

describe('PDF text geometry', () => {
  it('skips empty and whitespace items before measuring their geometry',
    async () => {
      const page = await pdfTextPage(textPage([
        { str: '', hasEOL: true, transform: [], width: 0, height: 0 },
        { str: ' \t\n', transform: [NaN], width: Infinity, height: 0 },
        {},
      ]));
      expect(page.text).toBe('A');
      expect(page.items).toHaveLength(1);
    });

  it('skips zero-size items with nonempty text', async () => {
    const page = await pdfTextPage(textPage([
      { str: 'NO-GLYPH', transform: [], width: 0, height: 0 }, {},
    ]));
    expect(page.text).toBe('A');
    expect(page.items).toHaveLength(1);
  });

  it('measures visible zero-height text from its transform and font metrics',
    async () => {
      const page = await pdfTextPage(textPage([{ height: 0 }]));
      expect(page.items).toEqual([{
        text: 'A', x: 10, y: 80, width: 20, height: 0,
        bounds: { left: 10, right: 30, top: 12, bottom: 22 },
      }]);
    });

  it('rejects non-finite visible glyph geometry with a fixed diagnostic',
    async () => {
      for (const item of [
        { width: Infinity }, { height: NaN },
        { transform: [10, 0, 0, 10, NaN, 80] },
      ]) {
        await expect(pdfTextPage(textPage([item]))).rejects
          .toThrow('Unsupported PDF glyph geometry.');
      }
      await expect(pdfTextPage(textPage([{}], {
        ascent: Infinity, descent: -0.2, vertical: false,
      }))).rejects.toThrow('Unsupported PDF glyph geometry.');
    });
});

describe('PDF border paint', () => {
  it('measures a stroked rule through transforms, clipping and state restore',
    async () => {
      const page = { getViewport: () => ({
        width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
      }) } as unknown as PDFPageProxy;
      const paint = await pdfPaintOperators(page, [
        OPS.setStrokeRGBColor, OPS.setLineWidth, OPS.save, OPS.transform,
        OPS.clip, OPS.constructPath, OPS.setStrokeRGBColor, OPS.setLineWidth,
        OPS.constructPath, OPS.restore, OPS.constructPath,
      ], [
        ['#404040'], [2], [], [2, 0, 0, 2, 10, 10], [],
        [OPS.endPath, [new Float32Array([
          0, 0, 0, 1, 10, 0, 1, 10, 10, 1, 0, 10, 4,
        ])], [0, 0, 10, 10]], ['#999999'], [1],
        [OPS.stroke, [new Float32Array([0, 0, 5, 1, 20, 5])], [0, 5, 20, 5]],
        [],
        [OPS.stroke, [new Float32Array([0, 40, 40, 1, 60, 40])],
          [40, 40, 60, 40]],
      ]);
      expect(pdfHasFill(paint, {
        left: 10, right: 30, top: 79, bottom: 81,
      }, '#999999')).toBe(true);
      expect(pdfHasFill(paint, {
        left: 40, right: 60, top: 59, bottom: 61,
      }, '#404040')).toBe(true);
      expect(pdfHasFill(paint, {
        left: 10, right: 30, top: 77, bottom: 81,
      }, '#999999')).toBe(false);
    });
});
