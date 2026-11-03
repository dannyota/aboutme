// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfGlyphGeometry, pdfTextPage } from '../e2e/pdf-display';
import { pdfFontFacesLoaded } from '../e2e/pdf-fonts';

const outline = [0, 0, -0.2, 1, 1, -0.2, 1, 1, 0.8, 1, 0, 0.8, 4];
const glyph = { str: 'A', dir: 'ltr', fontName: 'fixture', hasEOL: false,
  transform: [10, 0, 0, 10, 10, 80], width: 20, height: 10 };

function textPage(options: {
  font?: Record<string, unknown>;
  item?: Partial<typeof glyph>;
  style?: { ascent?: number; descent?: number };
  path?: unknown;
  painted?: Record<string, unknown>;
} = {}): PDFPageProxy {
  const font = { loadedName: 'fixture', ...options.font };
  const objects = new Map<string, unknown>([['fixture', font]]);
  if (options.path !== undefined) {
    objects.set('fixture_path_A', { path: options.path });
  }
  const store = { has: (id: string) => objects.has(id),
    get: (id: string, resolve: (value: unknown) => void) =>
      resolve(objects.get(id)) };
  return {
    view: [0, 0, 100, 100], commonObjs: store, objs: store,
    getViewport: () => ({ width: 100, height: 100 }),
    getTextContent: async () => ({
      items: [{ ...glyph, ...options.item }],
      styles: { fixture: options.style ?? {} },
    }),
    getOperatorList: async () => ({
      fnArray: [OPS.setFont, OPS.showText],
      argsArray: [['fixture', 10], [[{ unicode: 'A', fontChar: 'A',
        ...options.painted }]]],
    }),
  } as unknown as PDFPageProxy;
}

describe('PDF font metric fallback', () => {
  it('rejects malformed glyph records without including their operands',
    async () => {
      await expect(pdfTextPage(textPage({ painted: { unicode: 42,
        fontChar: 'PRIVATE-CONTENT' } })))
        .rejects.toThrow('Unsupported PDF glyph geometry.');
    });

  it.each([
    { name: 'missing outlines', options: {} },
    { name: 'an absent path', options: { path: null } },
    { name: 'an empty path', options: { path: [] } },
    { name: 'a malformed path', options: { path: [99] } },
    { name: 'a non-finite path', options: { path: [0, NaN, 0] } },
    { name: 'an unsupported accent', options: {
      path: outline, painted: { accent: {} },
    } },
    { name: 'a missing Type3 program', options: {
      font: { isType3Font: true, charProcOperatorList: new Map() },
    } },
    { name: 'an invalid Type3 box', options: {
      font: { isType3Font: true, fontMatrix: [0.001, 0, 0, 0.001, 0, 0],
        charProcOperatorList: new Map([['A', {
          fnArray: [OPS.setCharWidthAndBounds],
          argsArray: [[500, 0, 0, 0, 0, 0]],
        }]]) }, painted: { operatorListId: 'A' },
    } },
  ])('uses transform metrics for $name', async ({ options }) => {
    const page = await pdfTextPage(textPage({ ...options,
      item: { height: 0 }, style: { ascent: 0.7, descent: -0.1 } }));
    expect(page.items[0]!.bounds).toEqual({ left: 10, right: 30,
      top: expect.closeTo(13), bottom: expect.closeTo(21) });
  });

  it('falls back for text without a matching painted glyph', async () => {
    const page = await pdfTextPage(textPage({ path: outline,
      item: { str: 'B' }, style: { ascent: 0.7, descent: -0.1 } }));
    expect(page.items[0]!.bounds).toEqual({ left: 10, right: 30,
      top: expect.closeTo(13), bottom: expect.closeTo(21) });
  });

  it('combines an outline with fallback for the remaining glyphs', async () => {
    const page = await pdfTextPage(textPage({ path: outline,
      item: { str: 'AB' }, style: { ascent: 0.9, descent: -0.3 } }));
    expect(page.items[0]!.bounds).toEqual({ left: 10, right: 30,
      top: expect.closeTo(11), bottom: expect.closeTo(23) });
  });

  it.each([
    { name: 'absent metrics', style: {} },
    { name: 'non-finite metrics', style: { ascent: NaN, descent: Infinity } },
  ])('uses an em box for $name', async ({ style }) => {
    const page = await pdfTextPage(textPage({ style }));
    expect(page.items[0]!.bounds).toEqual({ left: 10, right: 30,
      top: expect.closeTo(12), bottom: expect.closeTo(22) });
  });

  it.each([
    { name: 'declared ascent', font: { ascent: 0.7 } },
    { name: 'declared bounding box', font: {
      bbox: [0, -100, 1000, 700], fontMatrix: [0.001, 0, 0, 0.001, 0, 0],
    } },
    { name: 'the tighter declared bound', font: {
      ascent: 0.75, bbox: [0, -100, 1000, 700],
      fontMatrix: [0.001, 0, 0, 0.001, 0, 0],
    } },
  ])('bounds fallback inflation by $name', async ({ font }) => {
    // A 0.8 em ascent puts this line 1.854 pt above the top margin.
    const page = await pdfTextPage(textPage({ font,
      item: { transform: [10, 0, 0, 10, 10, 83.854] } }));
    expect(page.items[0]!.bounds!.top).toBeCloseTo(9.146);
    expect(pdfGlyphGeometry(page, { x: 10, y: 10 }).contained).toBe(true);
    const moved = await pdfTextPage(textPage({ font,
      item: { transform: [10, 0, 0, 10, 10, 84.01] } }));
    expect(pdfGlyphGeometry(moved, { x: 10, y: 10 }).contained).toBe(false);
  });

  it('keeps available glyph ink beyond a declared ascent', async () => {
    const page = await pdfTextPage(textPage({ path: outline,
      font: { ascent: 0.6 }, item: { transform: [10, 0, 0, 10, 10, 83.854] },
    }));
    expect(page.items[0]!.bounds!.top).toBeCloseTo(8.146);
    expect(pdfGlyphGeometry(page, { x: 10, y: 10 }).contained).toBe(false);
  });

  it('caps inflated style metrics by the declared font bounds', async () => {
    const page = await pdfTextPage(textPage({
      style: { ascent: 1.2, descent: -0.5 }, font: {
        bbox: [0, -100, 1000, 700],
        fontMatrix: [0.001, 0, 0, 0.001, 0, 0],
      },
    }));
    expect(page.items[0]!.bounds).toEqual({ left: 10, right: 30,
      top: expect.closeTo(13), bottom: expect.closeTo(21) });
  });

  it('ignores invalid font bounds and transforms the fallback for rotation',
    async () => {
      const page = await pdfTextPage(textPage({
        font: { ascent: NaN, bbox: [0, 0, Infinity, 1],
          fontMatrix: [0.001, 0, 0, 0.001, 0, 0] },
        item: { transform: [0, 10, -10, 0, 30, 60] },
      }));
      expect(page.items[0]!.bounds).toEqual({ left: expect.closeTo(22),
        right: expect.closeTo(32), top: 20, bottom: 40 });
    });
});

describe('loaded PDF font weights', () => {
  const face = { family: 'Inter', weight: '100 900',
    status: 'loaded', style: 'normal' };

  it('accepts a loaded variable face covering regular and bold', () => {
    expect(pdfFontFacesLoaded([face], 'Inter')).toBe(true);
  });

  it('accepts separate regular and bold faces', () => {
    expect(pdfFontFacesLoaded([{ ...face, weight: '400' },
      { ...face, weight: '700' }], 'Inter')).toBe(true);
  });

  it.each([
    { name: 'missing bold', change: { weight: '100 600' } },
    { name: 'unloaded', change: { status: 'unloaded' } },
    { name: 'failed', change: { status: 'error' } },
    { name: 'fallback family', change: { family: 'Noto Sans' } },
    { name: 'italic only', change: { style: 'italic' } },
    { name: 'invalid range', change: { weight: '900 100' } },
  ])('rejects $name faces', ({ change }) => {
    expect(pdfFontFacesLoaded([{ ...face, ...change }], 'Inter')).toBe(false);
  });
});
