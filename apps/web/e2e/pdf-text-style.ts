import { expect } from '@playwright/test';
import type { measurePdfLayout } from './display-assertions';
import type { PdfInk, PdfPaint } from './pdf-paint';

interface TextStyle {
  color: string;
  size: number;
  family: string;
  bold: boolean;
}

// Static faces can expose their weight in the embedded OpenType OS/2 table.
// Variable advances only detect contradictions, even with this table present.
export function pdfFontWeight(data: unknown): number | undefined {
  if (!(data instanceof Uint8Array) || data.length < 12) return undefined;
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  if (![0x00010000, 0x4f54544f].includes(view.getUint32(0))) return undefined;
  const count = view.getUint16(4);
  const directoryEnd = 12 + count * 16;
  if (directoryEnd > data.length) return undefined;
  for (let record = 12; record < directoryEnd; record += 16) {
    if (view.getUint32(record) !== 0x4f532f32) continue;
    const offset = view.getUint32(record + 8);
    const length = view.getUint32(record + 12);
    if (length < 8 || offset < directoryEnd
      || offset > data.length - length) return undefined;
    const weight = view.getUint16(offset + 4);
    return weight >= 1 && weight <= 1000 ? weight : undefined;
  }
  return undefined;
}

const channels = (color: string) => [1, 3, 5].map((offset) =>
  parseInt(color.slice(offset, offset + 2), 16));

export function pdfColorMatches(actual: string, expected: string): boolean {
  const wanted = channels(expected);
  return channels(actual).every((value, index) =>
    Math.abs(value - wanted[index]!) <= 1);
}

const compact = (text: string) => text.normalize('NFD').replace(/\s+/gu, '');
const family = (text: string) => text.replace(/[^a-z\d]/giu, '').toLowerCase();

function textRuns(paint: PdfPaint, value: string): PdfInk[][] {
  const runs = paint.text.filter((run) => compact(run.text) !== '');
  const text = runs.map((run) => compact(run.text)).join('');
  const needle = compact(value);
  const matches: PdfInk[][] = [];
  if (needle === '') return matches;
  for (let start = text.indexOf(needle); start >= 0;
    start = text.indexOf(needle, start + 1)) {
    let offset = 0;
    matches.push(runs.filter((run) => {
      const end = offset + compact(run.text).length;
      const overlaps = end > start && offset < start + needle.length;
      offset = end;
      return overlaps;
    }));
  }
  return matches;
}

const variableFamily = (font: string) => family(font
  .replace(/^[A-Z]{6}\+/u, '')
  .replace(/-(?:Regular|Bold|Italic|BoldItalic)$/iu, ''));
const variableFamilies = new Set(['inter', 'bevietnampro']);

interface Advances {
  family: string;
  widths: Map<string, number>;
  valid: boolean;
}

function mergeAdvance(target: Advances, unicode: string, width: number) {
  if (!Number.isFinite(width) || width < 0) target.valid = false;
  const key = unicode.normalize('NFC');
  if (!/^[\p{L}\p{N}]$/u.test(key) || width <= 0) return;
  const previous = target.widths.get(key);
  if (previous !== undefined && Math.abs(previous - width) > 0.000001) {
    target.valid = false;
  }
  target.widths.set(key, width);
}

// Anchor regular resources to independently checked print-DOM weight 400.
// Compare the same Unicode glyphs in em units, never total text-run widths.
export function pdfVariableTextWeights(
  paint: PdfPaint, expectedFamily: string, regularText: readonly string[],
): PdfPaint {
  const fonts = new Map<string, Advances>();
  for (const run of paint.text) {
    const name = variableFamily(run.font);
    if (!variableFamilies.has(name) || !run.fontId) continue;
    const metrics = fonts.get(run.fontId)
      ?? { family: name, widths: new Map(), valid: true };
    if (metrics.family !== name || !run.advances) metrics.valid = false;
    for (const glyph of run.advances ?? []) {
      mergeAdvance(metrics, glyph.unicode, glyph.width);
    }
    fonts.set(run.fontId, metrics);
  }
  const regular = new Set<string>();
  for (const value of regularText) {
    const matches = textRuns(paint, value);
    if (matches.length !== 1) continue;
    for (const run of matches[0]!) {
      if (run.fontId && variableFamily(run.font) === family(expectedFamily)) {
        regular.add(run.fontId);
      }
    }
  }
  const baseline: Advances = { family: family(expectedFamily),
    widths: new Map(), valid: regular.size > 0 };
  for (const id of regular) {
    const metrics = fonts.get(id);
    if (!metrics?.valid) baseline.valid = false;
    for (const [unicode, width] of metrics?.widths ?? []) {
      mergeAdvance(baseline, unicode, width);
    }
  }
  const evidence = new Map<string, Pick<PdfInk, 'bold' | 'advanceWeight'>>();
  for (const [id, metrics] of fonts) {
    let bold: boolean | undefined;
    const deltas = [...metrics.widths].flatMap(([unicode, width]) => {
      const other = baseline.widths.get(unicode);
      return other === undefined ? [] : [width - other];
    });
    const shared = deltas.length;
    const wider = deltas.filter((delta) => delta > 0.001).length;
    const meanDelta = shared ? deltas.reduce((a, b) => a + b, 0) / shared : 0;
    if (shared >= 3 && baseline.valid && metrics.valid
      && metrics.family === baseline.family) {
      if (wider >= 3 && meanDelta > 0.001) bold = true;
      else if (wider === 0) bold = false;
    }
    evidence.set(id, { bold, advanceWeight: { shared, wider, meanDelta } });
  }
  return { ...paint, text: paint.text.map((run) =>
    variableFamilies.has(variableFamily(run.font))
      ? { ...run, bold: undefined, advanceWeight: undefined,
          ...evidence.get(run.fontId ?? '') }
      : run) };
}

function weightMatches(run: PdfInk, bold: boolean): boolean {
  return run.bold === bold || (run.bold === undefined
    && variableFamilies.has(variableFamily(run.font)));
}

function weightEvidence(run: PdfInk): string | undefined {
  if (!variableFamilies.has(variableFamily(run.font))) return undefined;
  if (!run.advanceWeight) return 'unavailable';
  if (run.advanceWeight.shared < 3) return 'insufficient';
  if (run.bold === undefined) return 'inconclusive';
  return run.bold ? 'wider' : 'not-wider';
}

function styleMatches(run: PdfInk, expected: TextStyle): boolean {
  return pdfColorMatches(run.color, expected.color)
    && Math.abs(run.size - expected.size) <= 0.075
    && family(run.font).includes(family(expected.family))
    && weightMatches(run, expected.bold);
}

export function pdfTextStyleMatches(
  paint: PdfPaint, value: string, expected: TextStyle,
): boolean {
  return textRuns(paint, value).some((runs) =>
    runs.every((run) => styleMatches(run, expected)));
}

function nearestTextRun(paint: PdfPaint, value: string): PdfInk | undefined {
  // Case folding helps diagnose transforms, never approve text or style.
  const needle = compact(value).toUpperCase();
  if (needle === '') return undefined;
  return paint.text.map((run) => ({ run,
    text: compact(run.text).toUpperCase() }))
    .filter(({ text }) => text !== ''
      && (needle.includes(text) || text.includes(needle)))
    .sort((a, b) => Math.min(b.text.length, needle.length)
      - Math.min(a.text.length, needle.length))[0]?.run;
}

export function pdfTextStyleDiagnostic(
  paint: PdfPaint, value: string, expected: TextStyle,
): string {
  const misses = (runs: PdfInk[]) => runs.filter((run) =>
    !styleMatches(run, expected)).length;
  const nearest = textRuns(paint, value)
    .sort((a, b) => misses(a) - misses(b))[0];
  const found = nearest?.find((run) => !styleMatches(run, expected))
    ?? nearest?.[0] ?? nearestTextRun(paint, value);
  const wanted = channels(expected.color);
  return 'PDF text style measurements: ' + JSON.stringify({
    matched: nearest !== undefined,
    expected: { ...expected, color: wanted },
    found: found
      ? { color: channels(found.color), size: found.size,
          family: found.font, bold: found.bold ?? null,
          weight: found.weight ?? null, advanceWeight: found.advanceWeight,
          weightEvidence: weightEvidence(found) }
      : null,
    delta: found
      ? {
          color: channels(found.color)
            .map((value, index) => value - wanted[index]!),
          size: found.size - expected.size,
          family: family(found.font).includes(family(expected.family)),
          bold: weightMatches(found, expected.bold),
        }
      : null,
  });
}

export function assertOverflowTextStyle(
  layout: Awaited<ReturnType<typeof measurePdfLayout>>, paint: PdfPaint,
): void {
  expect(layout.family.replaceAll('"', ''), 'Print font family.')
    .toBe('Be Vietnam Pro, Noto Sans');
  expect(layout.synthesis, 'Print font synthesis.').toBe('none');
  expect(layout.size, 'Print font size.').toBe(14);
  expect(layout.lineHeight, 'Print line height.').toBe(21);
  expect(layout.background, 'Print paper color.').toBe('rgb(255, 255, 255)');
  expect(layout.headerAlign, 'Print header alignment.').toBe('left');
  for (const icon of layout.icons) {
    const sidebar = layout.sidebar && icon.left >= layout.sidebar.left - 1
      && icon.top >= layout.sidebar.top - 1;
    expect(pdfColorMatches(icon.color, sidebar ? '#4e5b62' : '#556066'),
      'Print icon color.').toBe(true);
  }
  const expected = layout.chunks.map((chunk) => {
    const heading = ['name', 'heading', 'title'].includes(chunk.role);
    const scales: Record<string, number> = {
      name: 2, headline: 1.15, heading: 1.1, meta: 0.9,
    };
    const size = 14 * (scales[chunk.role] ?? 1);
    const color = chunk.linked
      ? '#0f6b7d'
      : heading
        ? '#0e3f52'
        : chunk.role === 'meta'
          ? (chunk.region === 'sidebar' ? '#4e5b62' : '#556066')
          : '#1c2b33';
    expect(Math.abs(chunk.size - size), 'Print text size.')
      .toBeLessThanOrEqual(0.1);
    expect(chunk.weight, 'Print text weight.')
      .toBe(chunk.role === 'title' && chunk.strong
        ? '900'
        : heading || chunk.strong ? '700' : '400');
    const channels = chunk.color.match(/[\d.]+/gu) ?? [];
    const hex = '#' + channels.map((channel) => Number(channel)
      .toString(16).padStart(2, '0')).join('');
    expect(pdfColorMatches(hex, color), 'Print text color.').toBe(true);
    return {
      value: chunk.role === 'heading'
        ? chunk.text.toLocaleUpperCase('en-US')
        : chunk.text,
      expected: { color, size: size * 0.75, family: 'Be Vietnam Pro',
        bold: heading || chunk.strong },
    };
  });
  const weighted = pdfVariableTextWeights(paint, 'Be Vietnam Pro',
    expected.filter((chunk) => !chunk.expected.bold).map(({ value }) => value));
  for (const chunk of expected) {
    expect(pdfTextStyleMatches(weighted, chunk.value, chunk.expected),
      'PDF print text style. '
      + pdfTextStyleDiagnostic(weighted, chunk.value, chunk.expected))
      .toBe(true);
  }
  for (const text of layout.text) {
    if (text.role === 'body') {
      expect(text.align, 'Print body alignment.').toBe('left');
    }
    if (text.role === 'heading') {
      expect(text.transform, 'Print heading transform.').toBe('uppercase');
      expect(Math.abs(text.spacing - 14 * 1.1 * 0.06), 'Print heading spacing.')
        .toBeLessThanOrEqual(0.1);
    }
  }
}
