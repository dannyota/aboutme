// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfPaintOperators, type PdfPaint } from '../e2e/pdf-paint';
import {
  pdfFontWeight, pdfTextStyleDiagnostic, pdfTextStyleMatches,
} from '../e2e/pdf-text-style';

function fontData(weight: number): Uint8Array {
  const data = new Uint8Array(36);
  const view = new DataView(data.buffer);
  view.setUint32(0, 0x00010000);
  view.setUint16(4, 1);
  view.setUint32(12, 0x4f532f32); // OS/2
  view.setUint32(20, 28);
  view.setUint32(24, 8);
  view.setUint16(32, weight);
  return data;
}

const expected = {
  color: '#14304f', size: 19.5, family: 'Inter', bold: true,
};
const run = { text: 'Đặng An', font: 'AAAAAA+Inter-Regular',
  color: '#14304f', size: 19.5, bold: true, weight: 700, x: 10, y: 20 };
const paint = (text = [run]): PdfPaint => ({ fills: [], images: [], text });

describe('PDF font weight', () => {
  it.each([400, 700, 900])('reads embedded weight %i under a regular name',
    async (weight) => {
      const font = { name: 'Inter-Regular', data: fontData(weight) };
      const objects = { get: (_: string, ready: (value: unknown) => void) =>
        ready(font) };
      const page = { getViewport: () => ({ width: 100, height: 100,
        transform: [1, 0, 0, -1, 0, 100] }),
      commonObjs: objects, objs: objects } as unknown as PDFPageProxy;
      const result = await pdfPaintOperators(page, [OPS.setFont, OPS.showText],
        [['g_font', 19.5], [[{ unicode: 'Đặng An', width: 500 }]]]);
      expect(pdfFontWeight(font.data)).toBe(weight);
      expect(result.text[0]).toMatchObject({ weight, bold: weight >= 700 });
      expect(pdfTextStyleMatches({ ...result, text: result.text.map((part) =>
        ({ ...part, color: expected.color })) }, 'Đặng An', expected))
        .toBe(weight >= 700);
    });

  it('reads a subarray and an OpenType header', () => {
    const bytes = new Uint8Array(40);
    bytes.set(fontData(700), 4);
    const data = bytes.subarray(4);
    new DataView(data.buffer, data.byteOffset).setUint32(0, 0x4f54544f);
    expect(pdfFontWeight(data)).toBe(700);
  });

  it('rejects a regular embedded face despite a bold name', async () => {
    const objects = { get: (_: string, ready: (value: unknown) => void) =>
      ready({ name: 'Inter-Bold', bold: true, data: fontData(400) }) };
    const page = { getViewport: () => ({ width: 100, height: 100,
      transform: [1, 0, 0, -1, 0, 100] }),
    commonObjs: objects, objs: objects } as unknown as PDFPageProxy;
    const result = await pdfPaintOperators(page, [OPS.setFont, OPS.showText],
      [['g_font', 10], [[{ unicode: 'A', width: 500 }]]]);
    expect(result.text[0]).toMatchObject({ weight: 400, bold: false });
  });

  it.each([
    undefined, new Uint8Array(11), fontData(0), fontData(1001),
    ...[4, 20, 24].map((offset) => {
      const data = fontData(700);
      new DataView(data.buffer).setUint32(offset, 0xffffffff);
      return data;
    }),
  ])('rejects absent, truncated or invalid weight evidence %#', (data) => {
    expect(pdfFontWeight(data)).toBeUndefined();
  });
});

describe('PDF text style', () => {
  it('matches tracked characters across operators and canonical accents',
    () => {
      const text = [...run.text.normalize('NFD')]
        .map((text) => ({ ...run, text }));
      expect(pdfTextStyleMatches(paint(text), run.text, expected)).toBe(true);
    });

  it.each([
    { color: '#16304f' }, { size: 19.576 },
    { font: 'AAAAAA+NotoSans-Bold' }, { bold: false, weight: 400 },
  ])('rejects a changed style field %j', (changed) => {
    expect(pdfTextStyleMatches(paint([{ ...run, ...changed }]),
      run.text, expected)).toBe(false);
  });

  it('keeps accents and case significant', () => {
    for (const text of ['Dang An', 'ĐẶNG AN', '']) {
      expect(pdfTextStyleMatches(paint(), text, expected)).toBe(false);
    }
  });

  it('ignores unpainted spacing and checks every visible tracked run', () => {
    const runs = [{ ...run, text: 'Đặng' },
      { ...run, text: ' ', font: 'Other', size: 0 },
      { ...run, text: 'An' }];
    expect(pdfTextStyleMatches(paint(runs), run.text, expected)).toBe(true);
    runs[2]!.bold = false;
    expect(pdfTextStyleMatches(paint(runs), run.text, expected)).toBe(false);
  });

  it('reports a partial case match without accepting transformed text', () => {
    const value = run.text.toUpperCase();
    expect(pdfTextStyleMatches(paint(), value, expected)).toBe(false);
    const diagnostic = pdfTextStyleDiagnostic(paint(), value, expected);
    const details = JSON.parse(diagnostic.slice(diagnostic.indexOf('{')));
    expect(details).toMatchObject({ matched: false,
      found: { size: 19.5, weight: 700, bold: true } });
    expect(diagnostic).not.toContain(run.text);
  });

  it('reports nearest run measurements without including resume text', () => {
    const result = paint([{ ...run, color: '#16304f', size: 20 }]);
    const diagnostic = pdfTextStyleDiagnostic(result, run.text, expected);
    const details = JSON.parse(diagnostic.slice(diagnostic.indexOf('{')));
    expect(details).toMatchObject({
      expected: { color: [20, 48, 79], size: 19.5,
        family: 'Inter', bold: true },
      found: { color: [22, 48, 79], size: 20,
        family: 'AAAAAA+Inter-Regular', bold: true, weight: 700 },
      delta: { color: [2, 0, 0], size: 0.5, family: true, bold: true },
    });
    expect(diagnostic).not.toContain(run.text);
    expect(pdfTextStyleDiagnostic(paint(), 'Absent', expected))
      .toContain('"matched":false');
  });
});
