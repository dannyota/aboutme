// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { createCanvas, loadImage } from '@napi-rs/canvas';
import { describe, expect, it } from 'vitest';

import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';
import {
  checkMembership, pdfGlyphGeometry, pdfTextPage, pdfWidgetMatches,
  type PdfWidget,
} from '../e2e/pdf-display';
import {
  fixturePhotoIdentity, pdfGlyphExtents, pdfHasFill, pdfHasRule,
  pdfImageMatches,
  pdfPaintOperators, pdfTextStyleMatches,
  UNSUPPORTED_PDF_PAINT,
} from '../e2e/pdf-paint';

const glyph = {
  str: 'A', dir: 'ltr', fontName: 'fixture', hasEOL: false,
  transform: [10, 0, 0, 10, 10, 80], width: 20, height: 10,
};

function textPage(
  items: readonly Partial<typeof glyph>[],
  style: Partial<{ ascent: number; descent: number; vertical: boolean }>
    | null = { ascent: 0.8, descent: -0.2, vertical: false },
  outline: number[] | null = [
    0, 0, -0.2, 1, 1, -0.2, 1, 1, 0.8, 1, 0, 0.8, 4,
  ],
): PDFPageProxy {
  const objects = {
    has: (id: string) => id === 'fixture' || outline !== null,
    get: (id: string, resolve: (value: unknown) => void) => resolve(
      id === 'fixture' ? { loadedName: 'fixture' } : { path: outline },
    ),
  };
  return {
    view: [0, 0, 100, 100],
    getViewport: () => ({ width: 100, height: 100 }),
    getOperatorList: async () => ({
      fnArray: [OPS.setFont, OPS.showText],
      argsArray: [['fixture', 10], [[{ unicode: 'A', fontChar: 'A' }]]],
    }),
    commonObjs: objects,
    objs: objects,
    getTextContent: async () => ({
      items: items.map((item) => ({ ...glyph, ...item })),
      styles: style === null ? {} : { fixture: style },
    }),
  } as unknown as PDFPageProxy;
}

describe('PDF text geometry', () => {
  it('rejects absent outlines and text absent from the painted glyphs',
    async () => {
      await expect(pdfTextPage(textPage([{}], undefined, null)))
        .rejects.toThrow(UNSUPPORTED_PDF_PAINT);
      await expect(pdfTextPage(textPage([{ str: 'B' }])))
        .rejects.toThrow('Unsupported PDF glyph outline.');
    });

  it('transforms each Type3 glyph box instead of using the whole font box',
    async () => {
      const font = { loadedName: 'g_font', isType3Font: true,
        fontMatrix: [0.001, 0, 0, 0.001, 0, 0],
        charProcOperatorList: new Map([['A', {
          fnArray: [OPS.setCharWidthAndBounds],
          argsArray: [[500, 0, 0, -100, 500, 700]],
        }]]) };
      const page = Object.assign(paintPage({ g_font: font }), {
        getOperatorList: async () => ({
          fnArray: [OPS.save, OPS.setFont, OPS.showText, OPS.restore],
          argsArray: [[], ['g_font', 10],
            [[{ unicode: 'A', operatorListId: 'A' }]], []],
        }),
      });
      const metrics = (await pdfGlyphExtents(page)).get('g_font')!.get('A')!;
      expect(metrics.ascent).toBeCloseTo(0.7);
      expect(metrics.descent).toBeCloseTo(-0.1);
    });

  it('uses the embedded glyph outline instead of a font-wide ascent',
    async () => {
      const page = await pdfTextPage(textPage([
        { transform: [10, 0, 0, 10, 10, 82] },
      ], { ascent: 1.2, descent: -0.5 }));
      expect(page.items[0]!.bounds)
        .toEqual({ left: 10, right: 30,
          top: expect.closeTo(10), bottom: expect.closeTo(20) });
      expect(pdfGlyphGeometry(page, { x: 10, y: 10 }).contained).toBe(true);
      expect(pdfGlyphGeometry(page, { x: 10, y: 11.01 }).contained)
        .toBe(false);
    });

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

  it('measures visible zero-height text from its transform and glyph outline',
    async () => {
      const page = await pdfTextPage(textPage([{ height: 0 }]));
      expect(page.items).toEqual([{
        text: 'A', x: 10, y: 80, width: 20, height: 0,
        bounds: { left: 10, right: 30,
          top: expect.closeTo(12), bottom: expect.closeTo(22) },
      }]);
    });

  it('measures text without a font style from the outline', async () => {
    const page = await pdfTextPage(textPage([{}], null));
    expect(page.items[0]!.bounds)
      .toEqual({ left: 10, right: 30,
        top: expect.closeTo(12), bottom: expect.closeTo(22) });
  });

  it.each([
    { name: 'NaN ascent', style: { ascent: NaN, descent: -0.2 } },
    { name: 'infinite ascent', style: { ascent: Infinity, descent: -0.2 } },
    { name: 'NaN descent', style: { ascent: 0.8, descent: NaN } },
    { name: 'infinite descent', style: { ascent: 0.8, descent: -Infinity } },
    { name: 'missing metrics', style: {} },
  ])('uses glyph outlines for $name', async ({ style }) => {
    const page = await pdfTextPage(textPage([{ height: 0 }], style));
    expect(page.items[0]!.bounds)
      .toEqual({ left: 10, right: 30,
        top: expect.closeTo(12), bottom: expect.closeTo(22) });
  });

  it('measures rotated text with unavailable ascent from its transform',
    async () => {
      const page = await pdfTextPage(textPage([
        { transform: [0, 10, -10, 0, 30, 60] },
      ], { ascent: NaN, descent: -0.2 }));
      expect(page.items[0]!.bounds)
        .toEqual({ left: expect.closeTo(22), right: expect.closeTo(32),
          top: 20, bottom: 40 });
    });

  it('uses the transform font size when item height is non-finite',
    async () => {
      const page = await pdfTextPage(textPage([{ height: NaN }]));
      expect(page.items[0]!.height).toBe(10);
      expect(page.items[0]!.bounds)
        .toEqual({ left: 10, right: 30,
          top: expect.closeTo(12), bottom: expect.closeTo(22) });
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

describe('approved PDF membership', () => {
  for (const column of ['main', 'sidebar'] as const) {
    for (const language of ['en', 'vi'] as const) {
      for (const paper of ['a4', 'letter'] as const) {
        for (const scheme of ['light', 'dark'] as const) {
          const base = `${column}-overflow-${language}` as const;
          const key = `print-${base}-${paper}-${scheme}` as const;
          it(`requires the approved boundaries for ${key}`, () => {
            const shorter = language === 'vi' && paper === 'letter';
            const last = column === 'main'
              ? (shorter ? 6 : 7)
              : (shorter ? 10 : 11);
            const total = column === 'main' ? 9 : 13;
            const id = (index: number) => `${column.toUpperCase()}-ENTRY-`
              + String(index).padStart(2, '0');
            const membership = { pageCount: 2, pages: [
              { main: { first: null, last: null },
                sidebar: { first: null, last: null },
                [column]: { first: id(1), last: id(last) },
                start: true, end: false, opposite: true },
              { main: { first: null, last: null },
                sidebar: { first: null, last: null },
                [column]: { first: id(last + 1), last: id(total) },
                start: false, end: true, opposite: false },
            ] };
            expect(() => checkMembership(membership, key)).not.toThrow();
            expect(() => checkMembership({ ...membership,
              pageCount: 3,
            }, key)).toThrow();
            expect(() => checkMembership({ ...membership,
              pages: [...membership.pages].reverse(),
            }, key)).toThrow();
            expect(() => checkMembership({ ...membership,
              unapproved: undefined,
            } as typeof membership, key)).toThrow();
          });
        }
      }
    }
  }
});

describe('PDF photo content', () => {
  it('accepts bounded channel drift and rejects missing or substituted photos',
    async () => {
      const expected = await fixturePhotoIdentity();
      const source = await loadImage(FIXED_PHOTO_DATA_URL);
      const context = createCanvas(source.width, source.height)
        .getContext('2d');
      context.drawImage(source, 0, 0);
      const original = context.getImageData(0, 0, source.width, source.height);
      const box = { left: 0, top: 0, right: 1, bottom: 1 };
      const read = async (data: Uint8Array) => (await pdfPaintOperators(
        paintPage(), [OPS.paintInlineImageXObject], [[{
          width: source.width, height: source.height, kind: 3, data,
        }]],
      )).images[0]!;
      const shifted = Uint8Array.from(original.data, (value, index) =>
        index % 4 === 3 ? value : Math.min(255, value + 2));
      expect(pdfImageMatches(await read(shifted), expected,
        { ...box, top: 99, bottom: 100 })).toBe(true);
      expect(pdfImageMatches(undefined, expected, box)).toBe(false);
      const blank = await read(new Uint8Array(original.data.length).fill(255));
      expect(pdfImageMatches(blank, expected, blank)).toBe(false);
      const patterned = Uint8Array.from(original.data, (_, index) =>
        index % 4 === 3 ? 255 : index < original.data.length / 2 ? 20 : 220);
      const reference = await read(patterned);
      const swapped = new Uint8Array(original.data.length);
      for (let pixel = 0; pixel < source.width * source.height; pixel += 1) {
        swapped.set(patterned.subarray(pixel * 4, pixel * 4 + 4),
          swapped.length - pixel * 4 - 4);
      }
      const reversed = await read(swapped);
      expect(pdfImageMatches(reversed, reference, reversed)).toBe(false);
      const baseline = await read(new Uint8Array(original.data));
      expect(pdfImageMatches({ ...baseline, width: 191 }, expected, baseline))
        .toBe(false);
      expect(pdfImageMatches(baseline, expected,
        { ...baseline, left: baseline.left + 1.01 })).toBe(false);
      const changed = [...baseline.signature];
      changed[0] = changed[0]! + 8;
      expect(pdfImageMatches({ ...baseline, signature: changed }, expected,
        baseline)).toBe(true);
      changed[0] = changed[0]! + 0.01;
      expect(pdfImageMatches({ ...baseline, signature: changed }, expected,
        baseline)).toBe(false);
    });
});

function paintPage(objects: Readonly<Record<string, unknown>> = {}) {
  return {
    getViewport: () => ({
      width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
    }),
    commonObjs: { get: (id: string, resolve: (value: unknown) => void) =>
      resolve(objects[id]) },
    objs: { get: (id: string, resolve: (value: unknown) => void) =>
      resolve(objects[id]) },
  } as unknown as PDFPageProxy;
}

const rectCommands = new Float32Array([
  0, 0, 0, 1, 10, 0, 1, 10, 10, 1, 0, 10, 4,
]);

describe('PDF widget evidence', () => {
  it('locates text after advances, line moves, rise and graphics restoration',
    async () => {
      const paint = await pdfPaintOperators(paintPage({
        g_font: { name: 'Fixture' },
      }), [OPS.setFont, OPS.beginText, OPS.setTextMatrix,
        OPS.showText, OPS.showText, OPS.setLeadingMoveText, OPS.showText,
        OPS.save, OPS.setTextRise, OPS.setHScale, OPS.setCharSpacing,
        OPS.showText, OPS.showText, OPS.restore, OPS.nextLine, OPS.showText], [
        ['g_font', 10], [], [[1, 0, 0, 1, 10, 80]],
        [[{ unicode: 'A', width: 500 }]],
        [[100, { unicode: 'B', width: 500 }]],
        [0, -12], [[{ unicode: 'C', width: 500 }]],
        [], [2], [50], [1], [[{ unicode: 'D', width: 500 }]],
        [[{ unicode: 'E', width: 500 }]], [], [],
        [[{ unicode: 'F', width: 500 }]],
      ]);
      expect(paint.text.map(({ text, x, y }) => ({ text, x, y })))
        .toEqual([
          { text: 'A', x: 10, y: 20 }, { text: 'B', x: 14, y: 20 },
          { text: 'C', x: 10, y: 32 }, { text: 'D', x: 15, y: 30 },
          { text: 'E', x: 18, y: 30 }, { text: 'F', x: 10, y: 44 },
        ]);
    });

  it('matches a tag split across text operators and rejects a changed label',
    async () => {
      const widget = { left: 10, right: 40, top: 25, bottom: 45 };
      const measured: PdfWidget = {
        entry: { left: 10, right: 90, top: 10, bottom: 50 },
        title: { left: 10, right: 90, top: 10, bottom: 20 },
        widget, track: null, dots: [], size: 12.6,
        paddingX: 6.3, paddingY: 1.89, border: 0, radius: 3,
      };
      const paint = await pdfPaintOperators(paintPage({
        g_font: { name: 'Fixture', bold: false },
      }), [OPS.setFillRGBColor, OPS.constructPath, OPS.setFillRGBColor,
        OPS.setFont, OPS.setTextMatrix, OPS.showText, OPS.showText], [
        ['#0f6b7d'], [OPS.fill, [new Float32Array([
          0, 10, 55, 1, 40, 55, 1, 40, 75, 1, 10, 75, 4,
        ])]], ['#ffffff'], ['g_font', 9.45], [[1, 0, 0, 1, 15, 65]],
        [[{ unicode: '4', width: 500 }]], [[{ unicode: '/5', width: 1000 }]],
      ]);
      const page = { paint, text: { width: 100, height: 100, text: '4/5',
        items: [{ text: '4/5', x: 15, y: 65, width: 20, height: 10,
          bounds: { left: 15, right: 35, top: 28, bottom: 38 } }] } };
      const colors = { solid: '#0f6b7d', track: '#cfe1e5',
        onAccent: '#ffffff', fontSize: 12.6 };
      expect(pdfWidgetMatches(page, measured, 4, 'tag', colors)).toBe(true);
      expect(pdfWidgetMatches(page, measured, 3, 'tag', colors)).toBe(false);
      expect(pdfWidgetMatches({ ...page, paint: { ...paint,
        text: paint.text.map((run) => ({ ...run, color: '#000000' })),
      } }, measured, 4, 'tag', colors)).toBe(false);
      expect(pdfWidgetMatches({ ...page, paint: { ...paint,
        text: paint.text.map((run) => ({ ...run, x: 80 })),
      } }, measured, 4, 'tag', colors)).toBe(false);
    });
});

describe('PDF operator forms', () => {
  it('reads uncolored Type3 glyph programs without treating them as regions',
    async () => {
      const font = { name: 'Fixture-Bold', isType3Font: true,
        charProcOperatorList: new Map<string, {
          fnArray: number[]; argsArray: unknown[][];
        }>([['A', {
          fnArray: [OPS.setCharWidthAndBounds, OPS.constructPath],
          argsArray: [[10, 0, 0, 0, 10, 10],
            [OPS.fill, [rectCommands], [0, 0, 10, 10]]],
        }]]) };
      const paint = await pdfPaintOperators(paintPage({ g_font: font }), [
        OPS.setFont, OPS.setTextMatrix, OPS.showText,
      ], [
        ['g_font', 10], [new Float32Array([1, 0, 0, 1, 0, 0])],
        [[{ unicode: 'A', operatorListId: 'A', width: 500 }]],
      ]);
      expect(paint.fills).toEqual([]);
      expect(pdfTextStyleMatches(paint, 'A', {
        color: '#000000', size: 10, family: 'Fixture', bold: true,
      })).toBe(true);
      font.charProcOperatorList.clear();
      await expect(pdfPaintOperators(paintPage({ g_font: font }), [
        OPS.setFont, OPS.showText,
      ], [['g_font', 10], [[{ unicode: 'PRIVATE', operatorListId: 'A' }]]]))
        .rejects.toThrow(`${UNSUPPORTED_PDF_PAINT} Operator: showText.`);
      font.charProcOperatorList.set('A', {
        fnArray: [OPS.setCharWidthAndBounds, OPS.shadingFill],
        argsArray: [[10, 0, 0, 0, 10, 10], ['PRIVATE-SHADING']],
      });
      await expect(pdfPaintOperators(paintPage({ g_font: font }), [
        OPS.setFont, OPS.showText,
      ], [['g_font', 10], [[{ unicode: 'PRIVATE', operatorListId: 'A' }]]]))
        .rejects.toThrow(`${UNSUPPORTED_PDF_PAINT} Operator: shadingFill.`);
      font.charProcOperatorList.set('A', {
        fnArray: [OPS.setCharWidthAndBounds, OPS.setGState, OPS.constructPath],
        argsArray: [[10, 0, 0, 0, 10, 10], [[['ca', 0]]],
          [OPS.fill, [rectCommands], [0, 0, 10, 10]]],
      });
      await expect(pdfPaintOperators(paintPage({ g_font: font }), [
        OPS.setFont, OPS.showText,
      ], [['g_font', 10], [[{ unicode: 'PRIVATE', operatorListId: 'A' }]]]))
        .rejects.toThrow(`${UNSUPPORTED_PDF_PAINT} Operator: setGState.`);
    });

  it.each([
    { form: 'paintImageXObject', kind: 2 },
    { form: 'paintImageXObject', kind: 3 },
    { form: 'paintInlineImageXObject', kind: 2 },
    { form: 'paintInlineImageXObject', kind: 3 },
  ] as const)(
    'measures $form kind $kind in a transformed, clipped Form XObject',
    async ({ form, kind }) => {
      const data = new Uint8Array(kind === 2
        ? [255, 0, 0, 0, 255, 0]
        : [255, 0, 0, 255, 0, 255, 0, 255]);
      const image = { width: 2, height: 1, kind, data };
      const paint = await pdfPaintOperators(paintPage({ image }), [
        OPS.paintFormXObjectBegin, OPS.transform, OPS[form],
        OPS.paintFormXObjectEnd, OPS.constructPath,
      ], [
        [[2, 0, 0, 2, 10, 10], [0, 0, 10, 10]],
        [20, 0, 0, 10, 0, 0], [form === 'paintImageXObject' ? 'image' : image],
        [], [OPS.fill, [rectCommands], [0, 0, 10, 10]],
      ]);
      expect(paint.images).toMatchObject([{
        left: 10, right: 30, top: 70, bottom: 90, width: 2, height: 1,
      }]);
      const row = [255, 0, 0, 255, 255, 0, 0, 255,
        0, 255, 0, 255, 0, 255, 0, 255];
      expect(paint.images[0]!.signature)
        .toEqual([...row, ...row, ...row, ...row]);
      expect(pdfHasFill(paint, {
        left: 0, right: 10, top: 90, bottom: 100,
      }, '#000000')).toBe(true);
    },
  );

  it('retains gs opacity and restores opaque region evidence', async () => {
    const paint = await pdfPaintOperators(paintPage(), [
      OPS.save, OPS.setGState, OPS.constructPath, OPS.restore,
      OPS.constructPath,
    ], [
      [], [[['ca', 0.5], ['CA', 0.25], ['BM', 'source-over']]],
      [OPS.fill, [rectCommands], [0, 0, 10, 10]], [],
      [OPS.fill, [rectCommands], [0, 0, 10, 10]],
    ]);
    const box = { left: 0, right: 10, top: 90, bottom: 100 };
    expect(paint.fills.map(({ alpha }) => alpha)).toEqual([0.5, 1]);
    expect(pdfHasFill({ ...paint, fills: [paint.fills[0]!] }, box, '#000000'))
      .toBe(false);
    expect(pdfHasFill(paint, box, '#000000')).toBe(true);
  });

  it.each([
    { name: 'shadingFill', args: ['PRIVATE-CONTENT'] },
    { name: 'paintImageMaskXObject', args: ['PRIVATE-CONTENT'] },
    { name: 'constructPath', args: [OPS.fill, [[99]], [0, 0, 1, 1]] },
    { name: 'setGState', args: [[['PRIVATE-KEY', 'PRIVATE-VALUE']]] },
  ] as const)('names unsupported $name without operand content',
    async ({ name, args }) => {
      await expect(pdfPaintOperators(paintPage(), [OPS[name]], [args]))
        .rejects.toMatchObject({
          message: `${UNSUPPORTED_PDF_PAINT} Operator: ${name}.`,
        });
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
      expect(pdfHasFill(paint, expected, '#999999')).toBe(form === 'fill');
      expect(pdfHasRule(paint, expected, '#999999')).toBe(true);
      for (const delta of [-1, -0.5, 0.5, 1]) {
        expect(pdfHasRule(paint, { ...expected,
          top: expected.top + delta, bottom: expected.bottom + delta,
        }, '#999999')).toBe(true);
      }
      for (const delta of [-1.01, 1.01]) {
        expect(pdfHasRule(paint, { ...expected,
          top: expected.top + delta, bottom: expected.bottom + delta,
        }, '#999999')).toBe(false);
      }
      expect(pdfHasRule(paint, expected, '#000000')).toBe(false);
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
      expect(pdfHasRule(paint, {
        left: 10, right: 90, top: 10, bottom: 90,
      }, '#999999')).toBe(false);
    });

  it('matches a region with nested nonzero fill contours', async () => {
    const page = { getViewport: () => ({
      width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
    }) } as unknown as PDFPageProxy;
    const paint = await pdfPaintOperators(page, [
      OPS.setFillRGBColor, OPS.constructPath,
    ], [
      ['#999999'],
      [OPS.fill, [new Float32Array([
        0, 10, 10, 1, 90, 10, 1, 90, 90, 1, 10, 90, 4,
        0, 11, 11, 1, 89, 11, 1, 89, 89, 1, 11, 89, 4,
      ])], [10, 10, 90, 90]],
    ]);
    expect(pdfHasFill(paint, {
      left: 10, right: 90, top: 10, bottom: 90,
    }, '#999999')).toBe(true);
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
      expect(pdfHasRule(paint, {
        left: 10, right: 30, top: 79, bottom: 81,
      }, '#999999')).toBe(true);
      expect(pdfHasRule(paint, {
        left: 40, right: 60, top: 59, bottom: 61,
      }, '#404040')).toBe(true);
      expect(pdfHasRule(paint, {
        left: 10, right: 30, top: 77, bottom: 81,
      }, '#999999')).toBe(false);
    });
});
