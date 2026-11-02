import type { PdfInk, PdfPaint } from './pdf-paint';

interface TextStyle {
  color: string;
  size: number;
  family: string;
  bold: boolean;
}

// Variable font instances can retain a Regular PostScript name. Read the
// embedded OpenType OS/2 usWeightClass instead of inferring their weight.
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
          family: found.font, bold: found.bold, weight: found.weight ?? null }
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
