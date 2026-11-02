import {
  createCanvas,
  FillType,
  loadImage,
  Path2D,
  PathOp,
} from '@napi-rs/canvas';
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { createHash } from 'node:crypto';

import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';

export interface PdfBox {
  readonly left: number;
  readonly top: number;
  readonly right: number;
  readonly bottom: number;
}

export interface PdfFill extends PdfBox {
  readonly color: string;
  readonly alpha: number;
  readonly curves: number;
}

export interface PdfImage extends PdfBox {
  readonly width: number;
  readonly height: number;
  readonly rgbaHash: string;
}

export interface PdfInk {
  readonly text: string;
  readonly color: string;
  readonly font: string;
  readonly size: number;
  readonly bold: boolean;
}

export interface PdfPaint {
  readonly fills: readonly PdfFill[];
  readonly images: readonly PdfImage[];
  readonly text: readonly PdfInk[];
}

type Matrix = [number, number, number, number, number, number];
interface State {
  matrix: Matrix;
  clip: Path2D;
  color: string;
  alpha: number;
  font: string;
  size: number;
  textMatrix: Matrix;
  transparent: boolean;
  bold: boolean;
}

export const UNSUPPORTED_PDF_PAINT = 'Unsupported PDF paint evidence.';
function unsupported(): never {
  throw new Error(UNSUPPORTED_PDF_PAINT);
}
const identity = (): Matrix => [1, 0, 0, 1, 0, 0];
const shapes = new WeakMap<PdfFill, Path2D>();
const hitTest = createCanvas(1, 1).getContext('2d');

function numbers(value: unknown, count?: number): number[] {
  if (!Array.isArray(value) && !ArrayBuffer.isView(value)) unsupported();
  const values = Array.from(value as ArrayLike<unknown>);
  if ((count !== undefined && values.length !== count)
    || !values.every((item) => typeof item === 'number'
      && Number.isFinite(item))) unsupported();
  return values as number[];
}

function matrix(value: unknown): Matrix {
  return numbers(value, 6) as Matrix;
}

function multiply(a: Matrix, b: Matrix): Matrix {
  return [
    a[0] * b[0] + a[2] * b[1], a[1] * b[0] + a[3] * b[1],
    a[0] * b[2] + a[2] * b[3], a[1] * b[2] + a[3] * b[3],
    a[0] * b[4] + a[2] * b[5] + a[4],
    a[1] * b[4] + a[3] * b[5] + a[5],
  ];
}

function transformed(path: Path2D, m: Matrix): Path2D {
  const result = new Path2D();
  result.addPath(path, { a: m[0], b: m[1], c: m[2], d: m[3],
    e: m[4], f: m[5] });
  return result;
}

function rectangle(values: number[]): Path2D {
  const [left, top, right, bottom] = values as Matrix;
  const path = new Path2D();
  path.rect(left, top, right - left, bottom - top);
  return path;
}

function bounds(path: Path2D): PdfBox | null {
  const [left, top, right, bottom] = path.computeTightBounds();
  if (![left, top, right, bottom].every(Number.isFinite)) unsupported();
  return right > left && bottom > top ? { left, top, right, bottom } : null;
}

function clipped(path: Path2D, clip: Path2D): Path2D {
  return new Path2D(path).op(clip, PathOp.Intersect);
}

// PDF.js 6 encodes DrawOPS inside constructPath, with the consuming operator
// in args[0]. Keep this parser narrow and reject every other path encoding.
function drawing(data: unknown): { path: Path2D; curves: number } {
  const commands = numbers(data);
  const path = new Path2D();
  let curves = 0;
  for (let offset = 0; offset < commands.length;) {
    const op = commands[offset++]!;
    const count = [2, 2, 6, 4, 0][op];
    if (count === undefined || offset + count > commands.length) unsupported();
    const args = commands.slice(offset, offset + count);
    offset += count;
    if (op === 0) path.moveTo(args[0]!, args[1]!);
    else if (op === 1) path.lineTo(args[0]!, args[1]!);
    else if (op === 2) {
      path.bezierCurveTo(args[0]!, args[1]!, args[2]!, args[3]!,
        args[4]!, args[5]!);
      curves += 1;
    } else if (op === 3) {
      path.quadraticCurveTo(args[0]!, args[1]!, args[2]!, args[3]!);
      curves += 1;
    } else path.closePath();
  }
  return { path, curves };
}

function color(value: unknown): string {
  if (typeof value !== 'string' || !/^#[\da-f]{6}$/iu.test(value)) {
    unsupported();
  }
  return value.toLowerCase();
}

export function pdfColorMatches(actual: string, expected: string): boolean {
  return [1, 3, 5].every((offset) => Math.abs(
    parseInt(actual.slice(offset, offset + 2), 16)
    - parseInt(expected.slice(offset, offset + 2), 16),
  ) <= 1);
}

export function pdfBoxMatches(actual: PdfBox, expected: PdfBox): boolean {
  return (['left', 'top', 'right', 'bottom'] as const)
    .every((edge) => Math.abs(actual[edge] - expected[edge]) <= 1);
}

export function pdfHasFill(
  paint: PdfPaint,
  box: PdfBox,
  expectedColor: string,
): boolean {
  return paint.fills.some((fill) => fill.alpha >= 0.99
    && pdfColorMatches(fill.color, expectedColor) && pdfBoxMatches(fill, box)
    && [0.25, 0.5, 0.75].every((x) => [0.25, 0.5, 0.75].every((y) => {
      const path = shapes.get(fill) ?? unsupported();
      return hitTest.isPointInPath(path,
        box.left + (box.right - box.left) * x,
        box.top + (box.bottom - box.top) * y);
    })));
}

export function pdfTextStyleMatches(
  paint: PdfPaint,
  value: string,
  expected: { color: string; size: number; family: string; bold: boolean },
): boolean {
  const compact = (text: string) => text.normalize('NFC').replace(/\s+/gu, '');
  const family = (text: string) => text.replace(/[^a-z\d]/giu, '')
    .toLowerCase();
  const text = paint.text.map((run) => compact(run.text)).join('');
  const needle = compact(value);
  if (needle === '') return false;
  for (let start = text.indexOf(needle); start >= 0;
    start = text.indexOf(needle, start + 1)) {
    let offset = 0;
    const matched = paint.text.filter((run) => {
      const end = offset + compact(run.text).length;
      const overlaps = end > start && offset < start + needle.length;
      offset = end;
      return overlaps;
    });
    if (matched.length > 0 && matched.every((run) =>
      pdfColorMatches(run.color, expected.color)
      && Math.abs(run.size - expected.size) <= 0.075
      && family(run.font).includes(family(expected.family))
      && run.bold === expected.bold)) return true;
  }
  return false;
}

export async function fixturePhotoIdentity(): Promise<{
  width: number; height: number; rgbaHash: string;
}> {
  const image = await loadImage(FIXED_PHOTO_DATA_URL);
  const canvas = createCanvas(image.width, image.height);
  const context = canvas.getContext('2d');
  context.drawImage(image, 0, 0);
  return { width: image.width, height: image.height,
    rgbaHash: createHash('sha256').update(context.getImageData(
      0, 0, image.width, image.height,
    ).data).digest('hex') };
}

async function object(page: PDFPageProxy, id: unknown): Promise<unknown> {
  if (typeof id !== 'string') unsupported();
  const objects = id.startsWith('g_') ? page.commonObjs : page.objs;
  return new Promise((resolve) => objects.get(id, resolve));
}

function imageIdentity(value: unknown): Pick<PdfImage,
  'width' | 'height' | 'rgbaHash'> {
  if (value === null || typeof value !== 'object') unsupported();
  const image = value as {
    width: number; height: number; kind: number; data: Uint8Array;
  };
  const { width, height, kind, data } = image;
  if (!Number.isInteger(width) || !Number.isInteger(height)
    || width <= 0 || height <= 0 || width * height > 4_000_000
    || !(data instanceof Uint8Array
      || data instanceof Uint8ClampedArray)) unsupported();
  const channels = kind === 2 ? 3 : kind === 3 ? 4 : unsupported();
  if (data.length !== width * height * channels) unsupported();
  const rgba = new Uint8Array(width * height * 4);
  for (let pixel = 0; pixel < width * height; pixel += 1) {
    rgba.set(data.subarray(pixel * channels, pixel * channels + 3), pixel * 4);
    rgba[pixel * 4 + 3] = channels === 4 ? data[pixel * 4 + 3]! : 255;
  }
  return { width, height,
    rgbaHash: createHash('sha256').update(rgba).digest('hex') };
}

const textOperators = new Set<number>([
  OPS.dependency, OPS.setLineWidth, OPS.setLineCap, OPS.setLineJoin,
  OPS.setMiterLimit, OPS.setDash, OPS.setRenderingIntent, OPS.setFlatness,
  OPS.beginText, OPS.endText, OPS.setCharSpacing, OPS.setWordSpacing,
  OPS.setHScale, OPS.setLeading, OPS.setTextRise, OPS.moveText,
  OPS.setLeadingMoveText, OPS.nextLine, OPS.setStrokeRGBColor,
  OPS.setStrokeTransparent, OPS.markPoint, OPS.markPointProps,
  OPS.beginMarkedContent, OPS.endMarkedContent, OPS.beginCompat, OPS.endCompat,
]);

export async function pdfPaintPage(page: PDFPageProxy): Promise<PdfPaint> {
  const operators = await page.getOperatorList({ intent: 'print' });
  return pdfPaintOperators(page, operators.fnArray, operators.argsArray);
}

export async function pdfPaintOperators(
  page: PDFPageProxy,
  functions: readonly number[],
  argumentsList: readonly (readonly unknown[] | null | undefined)[],
): Promise<PdfPaint> {
  if (functions.length !== argumentsList.length) unsupported();
  const viewport = page.getViewport({ scale: 1 });
  let state: State = { matrix: matrix(viewport.transform),
    clip: rectangle([0, 0, viewport.width, viewport.height]),
    color: '#000000', alpha: 1, font: '', size: 0, textMatrix: identity(),
    transparent: false, bold: false };
  const stack: State[] = [];
  const fills: PdfFill[] = [];
  const images: PdfImage[] = [];
  const text: PdfInk[] = [];
  let pendingClip: FillType | undefined;
  const save = () => stack.push({ ...state });
  const restore = () => {
    state = stack.pop() ?? unsupported();
  };
  for (let index = 0; index < functions.length; index += 1) {
    const op = functions[index]!;
    const args = argumentsList[index] ?? [];
    if (op === OPS.save) save();
    else if (op === OPS.restore) restore();
    else if (op === OPS.transform) {
      state.matrix = multiply(state.matrix, matrix(args));
    } else if (op === OPS.setFillRGBColor) {
      state.color = color(args[0]);
      state.transparent = false;
    } else if (op === OPS.setFillTransparent) state.transparent = true;
    else if (op === OPS.setGState) {
      if (!Array.isArray(args[0])) unsupported();
      for (const [key, value] of args[0] as [string, unknown][]) {
        if (key === 'ca') {
          state.alpha = numbers([value], 1)[0]!;
          if (state.alpha < 0 || state.alpha > 1) unsupported();
        } else if (key === 'BM' && value === 'source-over') continue;
        else if ((key === 'SMask' && !value) || (key === 'TR'
          && value === null)) continue;
        else if (!['LW', 'LC', 'LJ', 'ML', 'D', 'RI', 'FL', 'CA']
          .includes(key)) unsupported();
      }
    } else if (op === OPS.clip || op === OPS.eoClip) {
      pendingClip = op === OPS.clip ? FillType.Winding : FillType.EvenOdd;
    } else if (op === OPS.constructPath) {
      const consume = args[0];
      if (!Array.isArray(args[1]) || args[1].length !== 1) unsupported();
      const { path, curves } = args[1][0] === null
        ? { path: new Path2D(), curves: 0 }
        : drawing(args[1][0]);
      if ([OPS.eoFill, OPS.eoFillStroke, OPS.closeEOFillStroke]
        .includes(consume as number)) path.setFillType(FillType.EvenOdd);
      const shape = transformed(path, state.matrix);
      const visible = clipped(shape, state.clip);
      const box = bounds(visible);
      if ([OPS.fill, OPS.eoFill, OPS.fillStroke, OPS.eoFillStroke,
        OPS.closeFillStroke, OPS.closeEOFillStroke]
        .includes(consume as number)) {
        if (box && state.alpha > 0 && !state.transparent) {
          const fill = { ...box, color: state.color,
            alpha: state.alpha, curves };
          shapes.set(fill, visible);
          fills.push(fill);
        }
      } else if (![OPS.stroke, OPS.closeStroke, OPS.endPath]
        .includes(consume as number)) unsupported();
      if (pendingClip !== undefined) {
        path.setFillType(pendingClip);
        state.clip = clipped(transformed(path, state.matrix), state.clip);
        pendingClip = undefined;
      }
    } else if (op === OPS.paintFormXObjectBegin) {
      save();
      if (args[0] !== null) {
        state.matrix = multiply(state.matrix, matrix(args[0]));
      }
      if (args[1] !== null) {
        state.clip = clipped(transformed(
          rectangle(numbers(args[1], 4)), state.matrix,
        ), state.clip);
      }
    } else if (op === OPS.paintFormXObjectEnd) restore();
    else if (op === OPS.paintImageXObject
      || op === OPS.paintInlineImageXObject) {
      if (state.alpha !== 1) unsupported();
      const image = imageIdentity(op === OPS.paintImageXObject
        ? await object(page, args[0])
        : args[0]);
      const box = bounds(clipped(transformed(
        rectangle([0, 0, 1, 1]), state.matrix,
      ), state.clip));
      if (box) images.push({ ...box, ...image });
    } else if (op === OPS.setFont) {
      const font = await object(page, args[0]) as {
        name?: string; bold?: boolean; isType3Font?: boolean;
      };
      if (typeof font.name !== 'string' || font.isType3Font) unsupported();
      state.font = font.name;
      state.bold = font.bold === true || /bold/iu.test(font.name);
      state.size = numbers([args[1]], 1)[0]!;
    } else if (op === OPS.setTextMatrix) state.textMatrix = matrix(args[0]);
    else if (op === OPS.beginMarkedContentProps) {
      if (args[0] === 'OC') unsupported();
    } else if (op === OPS.setTextRenderingMode) {
      if (args[0] !== 0) unsupported();
    } else if (op === OPS.showText) {
      if (!Array.isArray(args[0])) unsupported();
      const value = (args[0] as unknown[]).map((glyph) => {
        if (typeof glyph === 'number') return '';
        if (glyph === null || typeof glyph !== 'object'
          || !('unicode' in glyph) || typeof glyph.unicode !== 'string') {
          return unsupported();
        }
        return glyph.unicode;
      }).join('').normalize('NFC');
      const m = multiply(state.matrix, state.textMatrix);
      if (state.alpha !== 1 || state.transparent) unsupported();
      text.push({ text: value, font: state.font, color: state.color,
        bold: state.bold,
        size: Math.abs(state.size) * Math.hypot(m[2], m[3]) });
    } else if (!textOperators.has(op)) unsupported();
  }
  if (stack.length !== 0 || pendingClip !== undefined) unsupported();
  return { fills, images, text };
}
