import type { PdfInk, PdfPaint } from './pdf-paint';

interface TextStyle {
  color: string;
  size: number;
  family: string;
  bold: boolean;
}

// Static faces can expose their weight in the embedded OpenType OS/2 table.
// Variable instances need advance evidence even when this table is present.
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
    if (baseline.valid && metrics.valid
      && metrics.family === baseline.family) {
      if (regular.has(id)) bold = false;
      else if (shared >= 3) {
        if (wider >= 3 && meanDelta > 0.001) bold = true;
        else if (deltas.every((delta) => Math.abs(delta) <= 0.000001)) {
          bold = false;
        }
      }
    }
    evidence.set(id, { bold, advanceWeight: { shared, wider, meanDelta } });
  }
  return { ...paint, text: paint.text.map((run) =>
    variableFamilies.has(variableFamily(run.font))
      ? { ...run, bold: undefined, advanceWeight: undefined,
          ...evidence.get(run.fontId ?? '') }
      : run) };
}

function styleMatches(run: PdfInk, expected: TextStyle): boolean {
  return pdfColorMatches(run.color, expected.color)
    && Math.abs(run.size - expected.size) <= 0.075
    && family(run.font).includes(family(expected.family))
    && run.bold === expected.bold;
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
          weight: found.weight ?? null, advanceWeight: found.advanceWeight }
      : null,
    delta: found
      ? {
          color: channels(found.color)
            .map((value, index) => value - wanted[index]!),
          size: found.size - expected.size,
          family: family(found.font).includes(family(expected.family)),
          bold: found.bold === expected.bold,
        }
      : null,
  });
}
