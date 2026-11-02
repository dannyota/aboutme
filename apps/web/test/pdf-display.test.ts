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
  style: Partial<{ ascent: number; descent: number; vertical: boolean }>
    | null = { ascent: 0.8, descent: -0.2, vertical: false },
): PDFPageProxy {
  return {
    view: [0, 0, 100, 100],
    getViewport: () => ({ width: 100, height: 100 }),
    getTextContent: async () => ({
      items: items.map((item) => ({ ...glyph, ...item })),
      styles: style === null ? {} : { fixture: style },
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

  it('measures text without a font style from the transform', async () => {
    const page = await pdfTextPage(textPage([{}], null));
    expect(page.items[0]!.bounds)
      .toEqual({ left: 10, right: 30, top: 12, bottom: 22 });
  });

  it.each([
    { name: 'NaN ascent', style: { ascent: NaN, descent: -0.2 } },
    { name: 'infinite ascent', style: { ascent: Infinity, descent: -0.2 } },
    { name: 'NaN descent', style: { ascent: 0.8, descent: NaN } },
    { name: 'infinite descent', style: { ascent: 0.8, descent: -Infinity } },
    { name: 'missing metrics', style: {} },
  ])('uses transform bounds for $name', async ({ style }) => {
    const page = await pdfTextPage(textPage([{ height: 0 }], style));
    expect(page.items[0]!.bounds)
      .toEqual({ left: 10, right: 30, top: 12, bottom: 22 });
  });

  it('measures rotated text with unavailable ascent from its transform',
    async () => {
      const page = await pdfTextPage(textPage([
        { transform: [0, 10, -10, 0, 30, 60] },
      ], { ascent: NaN, descent: -0.2 }));
      expect(page.items[0]!.bounds)
        .toEqual({ left: 22, right: 32, top: 20, bottom: 40 });
    });

  it('uses the transform font size when item height is non-finite',
    async () => {
      const page = await pdfTextPage(textPage([{ height: NaN }]));
      expect(page.items[0]!.height).toBe(10);
      expect(page.items[0]!.bounds)
        .toEqual({ left: 10, right: 30, top: 12, bottom: 22 });
    });

  it('names the non-finite field and font without including glyph text',
    async () => {
      for (const [index, field] of ['a', 'b', 'c', 'd', 'x', 'y'].entries()) {
        const transform = [...glyph.transform];
        transform[index] = NaN;
        await expect(pdfTextPage(textPage([
          { str: 'PRIVATE-GLYPH', transform },
        ]))).rejects.toThrow(new Error(
          `Unsupported PDF glyph geometry: non-finite ${field} (font fixture).`,
        ));
      }
      await expect(pdfTextPage(textPage([
        { str: 'PRIVATE-GLYPH', width: Infinity },
      ]))).rejects.toThrow(new Error(
        'Unsupported PDF glyph geometry: non-finite width (font fixture).',
      ));
    });
});

describe('PDF border paint', () => {
  it.each(['fill', 'stroke'] as const)(
    'matches a one-pixel %s border after CSS scale and page origin flips',
    async (form) => {
      const page = { getViewport: () => ({
        width: 300, height: 300, transform: [1, 0, 0, -1, 0, 300],
      }) } as unknown as PDFPageProxy;
      // PDF.js buildPath expands `re` to these DrawOPS; `m`/`l` keep a line.
      const commands = form === 'fill'
        ? [0, 20, 39, 1, 180, 39, 1, 180, 40, 1, 20, 40, 4]
        : [0, 20, 39.5, 1, 180, 39.5];
      const paint = await pdfPaintOperators(page, [
        OPS.save, OPS.transform, OPS.transform,
        OPS.setFillRGBColor, OPS.setStrokeRGBColor, OPS.setLineWidth,
        OPS.constructPath, OPS.restore,
      ], [
        [], [0.24, 0, 0, -0.24, 0, 300], [3.125, 0, 0, 3.125, 0, 0],
        ['#999999'], ['#999999'], [1],
        [form === 'fill' ? OPS.fill : OPS.stroke,
          [new Float32Array(commands)], [20, 39, 180, 40]], [],
      ]);
      expect(paint.fills).toHaveLength(1);
      expect(paint.fills[0]).toMatchObject({
        left: 15, right: 135, top: 29.25, bottom: 30,
      });
      const expected = { left: 15, right: 135, top: 29.25, bottom: 30 };
      expect(pdfHasFill(paint, expected, '#999999')).toBe(true);
      for (const delta of [-1, -0.5, 0.5, 1]) {
        expect(pdfHasFill(paint, { ...expected,
          top: expected.top + delta, bottom: expected.bottom + delta,
        }, '#999999')).toBe(true);
      }
      for (const delta of [-1.01, 1.01]) {
        expect(pdfHasFill(paint, { ...expected,
          top: expected.top + delta, bottom: expected.bottom + delta,
        }, '#999999')).toBe(false);
      }
      expect(pdfHasFill(paint, expected, '#000000')).toBe(false);
    },
  );

  it('rejects an outline with matching bounds and an unpainted interior',
    async () => {
      const page = { getViewport: () => ({
        width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
      }) } as unknown as PDFPageProxy;
      const paint = await pdfPaintOperators(page, [
        OPS.setFillRGBColor, OPS.constructPath,
      ], [
        ['#999999'],
        [OPS.eoFill, [new Float32Array([
          0, 10, 10, 1, 90, 10, 1, 90, 90, 1, 10, 90, 4,
          0, 11, 11, 1, 89, 11, 1, 89, 89, 1, 11, 89, 4,
        ])], [10, 10, 90, 90]],
      ]);
      expect(paint.fills).toHaveLength(1);
      expect(pdfHasFill(paint, {
        left: 10, right: 90, top: 10, bottom: 90,
      }, '#999999')).toBe(false);
    });

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
