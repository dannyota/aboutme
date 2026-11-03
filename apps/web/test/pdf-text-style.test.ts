// @vitest-environment node
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import { pdfPaintOperators, type PdfPaint } from '../e2e/pdf-paint';
import {
  pdfFontWeight, pdfTextStyleDiagnostic, pdfTextStyleMatches,
  pdfVariableTextWeights,
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
const paint = (text: PdfPaint['text'] = [run]): PdfPaint => ({
  fills: [], images: [], text,
});

async function variablePaint(
  family = 'Inter', widths = [550, 550, 550, 550, 550],
): Promise<PdfPaint> {
  const fonts = {
    g_regular: { name: `AAAAAA+${family}-Regular`, data: fontData(400) },
    g_bold: { name: `BBBBBB+${family}-Regular`, data: fontData(400) },
  };
  const objects = { get: (id: keyof typeof fonts,
    ready: (value: unknown) => void) => ready(fonts[id]) };
  const page = { getViewport: () => ({ width: 100, height: 100,
    transform: [1, 0, 0, -1, 0, 100] }),
  commonObjs: objects, objs: objects } as unknown as PDFPageProxy;
  return pdfPaintOperators(page, [OPS.setFont, OPS.showText,
    OPS.setFont, OPS.setHScale, OPS.setCharSpacing, OPS.showText], [
    ['g_regular', 10], [[...'abcde'].map((unicode) =>
      ({ unicode, width: 500 }))], ['g_bold', 20], [50], [2],
    [[...'decab'].map((unicode, index) => ({ unicode, width: widths[index] }))],
  ]);
}

describe('PDF variable font weight', () => {
  it.each(['Inter', 'BeVietnamPro'])(
    'checks %s advances despite Regular names and weight metadata',
    async (family) => {
      const source = await variablePaint(family);
      const result = pdfVariableTextWeights(source, family, ['abcde']);
      expect(result.text[0]).toMatchObject({ bold: false });
      expect(result.text[1]).toMatchObject({ bold: true,
        advanceWeight: { shared: 5, wider: 5 } });
      expect(source.text[1]!.bold).toBe(false);
      expect(pdfTextStyleMatches(result, 'decab', {
        color: '#000000', size: 20, family, bold: true,
      })).toBe(true);
      expect(pdfTextStyleMatches(result, 'decab', {
        color: '#000000', size: 20, family, bold: false,
      })).toBe(false);
    });

  it('rejects dropped bold with identical advances', async () => {
    const source = await variablePaint('Inter', Array(5).fill(500));
    const result = pdfVariableTextWeights(source, 'Inter', ['abcde']);
    expect(result.text[1]!.bold).toBe(false);
    expect(pdfTextStyleMatches(result, 'decab', {
      color: '#000000', size: 20, family: 'Inter', bold: true,
    })).toBe(false);
  });

  it.each([
    { name: 'absent regular anchor', anchors: [] },
    { name: 'missing text', anchors: ['absent'] },
    { name: 'different family', anchors: ['abcde'],
      family: 'Be Vietnam Pro' },
  ])('leaves $name advisory', async ({ anchors, family = 'Inter' }) => {
    const result = pdfVariableTextWeights(await variablePaint(),
      family, anchors);
    expect(result.text[1]!.bold).toBeUndefined();
  });

  it('leaves ambiguous regular text advisory', async () => {
    const source = await variablePaint();
    const duplicate = { ...source, text: [source.text[0]!,
      { ...source.text[1]!, text: 'abcde' }] };
    const result = pdfVariableTextWeights(duplicate, 'Inter', ['abcde']);
    expect(result.text.every((part) => part.bold === undefined)).toBe(true);
  });

  it.each([
    { name: 'missing advances', advances: undefined },
    { name: 'missing resource identity', fontId: undefined },
    { name: 'nonfinite width', advances: [{ unicode: 'a', width: NaN }] },
  ])('leaves $name advisory', async ({ name: _, ...changed }) => {
    const source = await variablePaint();
    const result = pdfVariableTextWeights({ ...source, text: [
      source.text[0]!, { ...source.text[1]!, ...changed },
    ] }, 'Inter', ['abcde']);
    expect(result.text[1]!.bold).toBeUndefined();
    expect(pdfTextStyleMatches(result, 'decab', {
      color: '#000000', size: 20, family: 'Inter', bold: true,
    })).toBe(true);
  });

  it.each([
    { glyphs: 'XYZ', width: 0.55, shared: 0 },
    { glyphs: 'dXYZ', width: 0.55, shared: 1 },
    { glyphs: 'deXYZ', width: 0.55, shared: 2 },
    { glyphs: 'deXYZ', width: 0.495, shared: 2 },
  ])('allows $shared shared glyphs and width $width',
    async ({ glyphs, width, shared }) => {
      const source = await variablePaint();
      const result = pdfVariableTextWeights({ ...source, text: [
        source.text[0]!, { ...source.text[1]!, text: glyphs,
          advances: [...glyphs].map((unicode) => ({ unicode, width })) },
      ] }, 'Inter', ['abcde']);
      expect(result.text[1]).toMatchObject({
        advanceWeight: { shared },
      });
      expect(result.text[1]!.bold).toBeUndefined();
      for (const bold of [true, false]) {
        const expected = { color: '#000000', size: 20,
          family: 'Inter', bold };
        expect(pdfTextStyleMatches(result, glyphs, expected)).toBe(true);
        expect(pdfTextStyleDiagnostic(result, glyphs, expected))
          .toContain('"weightEvidence":"insufficient"');
      }
    });

  it.each([
    [550, 500, 500, 500, 500],
    [550, 550, 500, 500, 500],
    [550, 550, 550, 400, 400],
  ])('allows inconclusive widening %j', async (...widths) => {
    const result = pdfVariableTextWeights(await variablePaint('Inter', widths),
      'Inter', ['abcde']);
    expect(result.text[1]!.bold).toBeUndefined();
    for (const bold of [true, false]) {
      const expected = { color: '#000000', size: 20, family: 'Inter', bold };
      expect(pdfTextStyleMatches(result, 'decab', expected)).toBe(true);
      expect(pdfTextStyleDiagnostic(result, 'decab', expected))
        .toContain('"weightEvidence":"inconclusive"');
    }
  });

  it('allows three shared glyphs with only one wider glyph', async () => {
    const source = await variablePaint();
    const result = pdfVariableTextWeights({ ...source, text: [
      source.text[0]!, { ...source.text[1]!, text: 'dea',
        advances: [
          { unicode: 'd', width: 0.55 }, { unicode: 'e', width: 0.5 },
          { unicode: 'a', width: 0.5 },
        ] },
    ] }, 'Inter', ['abcde']);
    expect(result.text[1]).toMatchObject({ advanceWeight: {
      shared: 3, wider: 1,
    } });
    const expected = { color: '#000000', size: 20,
      family: 'Inter', bold: true };
    expect(pdfTextStyleMatches(result, 'dea', expected)).toBe(true);
    expect(pdfTextStyleDiagnostic(result, 'dea', expected))
      .toContain('"weightEvidence":"inconclusive"');
  });

  it.each([
    [500.1, 500.1, 500.1, 500.1, 500.1],
    [450, 450, 450, 450, 450],
  ])('rejects expected bold when shared glyphs are not wider %j',
    async (...widths) => {
      const result = pdfVariableTextWeights(
        await variablePaint('Inter', widths), 'Inter', ['abcde']);
      expect(result.text[1]!.bold).toBe(false);
      expect(pdfTextStyleMatches(result, 'decab', {
        color: '#000000', size: 20, family: 'Inter', bold: true,
      })).toBe(false);
    });

  it('detects contradictions with exactly three shared glyphs', async () => {
    const source = await variablePaint();
    for (const [width, bold] of [[0.5, false], [0.55, true]] as const) {
      const result = pdfVariableTextWeights({ ...source, text: [
        source.text[0]!, { ...source.text[1]!, text: 'dea',
          advances: [...'dea'].map((unicode) => ({ unicode, width })) },
      ] }, 'Inter', ['abcde']);
      expect(result.text[1]).toMatchObject({ bold,
        advanceWeight: { shared: 3 } });
      expect(pdfTextStyleMatches(result, 'dea', {
        color: '#000000', size: 20, family: 'Inter', bold: !bold,
      })).toBe(false);
    }
  });

  it('keeps family, size and color strict with insufficient weight evidence',
    async () => {
      const source = await variablePaint();
      const result = pdfVariableTextWeights({ ...source, text: [
        source.text[0]!, { ...source.text[1]!, text: 'de',
          advances: source.text[1]!.advances!.slice(0, 2) },
      ] }, 'Inter', ['abcde']);
      const expected = { color: '#000000', size: 20,
        family: 'Inter', bold: true };
      expect(pdfTextStyleMatches(result, 'de', expected)).toBe(true);
      for (const changed of [
        { color: '#020000' }, { size: 20.076 }, { family: 'Alegreya' },
      ]) {
        expect(pdfTextStyleMatches(result, 'de', { ...expected, ...changed }))
          .toBe(false);
      }
    });

  it('requires weight evidence for static faces', () => {
    const source = paint([{ ...run, font: 'AAAAAA+Alegreya-Regular',
      bold: undefined }]);
    expect(pdfTextStyleMatches(source, run.text, {
      ...expected, family: 'Alegreya', bold: false,
    })).toBe(false);
  });

  it('leaves absent regular anchors advisory in style matching', async () => {
    const result = pdfVariableTextWeights(await variablePaint(),
      'Inter', []);
    expect(result.text[1]!.bold).toBeUndefined();
    expect(pdfTextStyleMatches(result, 'decab', {
      color: '#000000', size: 20, family: 'Inter', bold: true,
    })).toBe(true);
  });

  it('leaves conflicting glyph widths advisory',
    async () => {
      const source = await variablePaint();
      for (const fontId of ['g_regular', 'g_other']) {
        const conflict = { ...source, text: [...source.text,
          { ...source.text[0]!, fontId, text: 'extra',
            advances: [{ unicode: 'a', width: 0.9 }] }] };
        const result = pdfVariableTextWeights(conflict, 'Inter',
          ['abcde', 'extra']);
        expect(result.text[1]!.bold).toBeUndefined();
      }
    });

  it('collects regular and bold glyphs across tracked operators', async () => {
    const source = await variablePaint();
    const tracked = { ...source, text: source.text.flatMap((part) =>
      part.advances!.map((glyph) => ({ ...part, text: glyph.unicode,
        advances: [glyph] }))) };
    const result = pdfVariableTextWeights(tracked, 'Inter', ['abcde']);
    expect(result.text.slice(0, 5).every((part) => part.bold === false))
      .toBe(true);
    expect(result.text.slice(5).every((part) => part.bold === true)).toBe(true);
  });

  it('keeps static face classification', () => {
    const source = paint([{ ...run, font: 'AAAAAA+Alegreya-Bold' }]);
    expect(pdfVariableTextWeights(source, 'Alegreya', [])).toEqual(source);
  });

  it('ignores spacing and compares matching glyphs rather than run totals',
    async () => {
      const source = await variablePaint();
      expect(source.text[1]).toMatchObject({ fontId: 'g_bold',
        advances: [...'decab'].map((unicode) => ({ unicode, width: 0.55 })) });
      const result = pdfVariableTextWeights(source, 'Inter', ['abcde']);
      expect(result.text[1]!.bold).toBe(true);
    });

  it('reports advance evidence without including glyph or resume text',
    async () => {
      const result = pdfVariableTextWeights(await variablePaint(),
        'Inter', ['abcde']);
      const diagnostic = pdfTextStyleDiagnostic(result, 'decab', {
        color: '#000000', size: 20, family: 'Inter', bold: false,
      });
      expect(diagnostic).toContain('"advanceWeight":{"shared":5,"wider":5');
      expect(diagnostic).not.toContain('decab');
      expect(diagnostic).not.toContain('unicode');
    });
});

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
