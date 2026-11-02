import {
  createCanvas,
  FillType,
  loadImage,
  Path2D,
  PathOp,
  StrokeCap,
  StrokeJoin,
} from '@napi-rs/canvas';
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';

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
  readonly signature: readonly number[];
}

export interface PdfInk {
  readonly text: string;
  readonly color: string;
  readonly font: string;
  readonly size: number;
  readonly bold: boolean;
  readonly x: number;
  readonly y: number;
}

export interface PdfPaint {
  readonly fills: readonly PdfFill[];
  readonly images: readonly PdfImage[];
  readonly text: readonly PdfInk[];
}

type Matrix = [number, number, number, number, number, number];
interface GlyphProgram {
  fnArray: readonly number[];
  argsArray: readonly (readonly unknown[] | null | undefined)[];
}
interface Stroke {
  color: string;
  alpha: number;
  transparent: boolean;
  width: number;
  cap: StrokeCap;
  join: StrokeJoin;
  miterLimit: number;
  dashed: boolean;
}
interface State {
  matrix: Matrix;
  clip: Path2D;
  color: string;
  alpha: number;
  font: string;
  size: number;
  textMatrix: Matrix;
  textPosition: { x: number; y: number; lineX: number; lineY: number;
    leading: number; rise: number; scale: number; charGap: number;
    wordGap: number; unit: number; };
  transparent: boolean;
  bold: boolean;
  type3: Map<string, GlyphProgram> | undefined;
  stroke: Stroke;
}

export const UNSUPPORTED_PDF_PAINT = 'Unsupported PDF paint evidence.';
const operatorNames = new Map(Object.entries(OPS).map(([name, op]) =>
  [op, name]));
class UnsupportedPaint extends Error {
  readonly operator: number | undefined;

  constructor(operator?: number) {
    super(`${UNSUPPORTED_PDF_PAINT} Operator: `
      + `${operatorNames.get(operator ?? -1) ?? 'unknown'}.`);
    this.operator = operator;
  }
}
function unsupported(op?: number): never {
  throw new UnsupportedPaint(op);
}
const identity = (): Matrix => [1, 0, 0, 1, 0, 0];
const shapes = new WeakMap<PdfFill, {
  path: Path2D; stroke: boolean;
}>();
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
  result.setFillType(path.getFillType());
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

function hasPaint(
  paint: PdfPaint,
  box: PdfBox,
  expectedColor: string,
  allowStroke: boolean,
): boolean {
  return paint.fills.some((fill) => {
    const { path, stroke } = shapes.get(fill) ?? unsupported();
    return (allowStroke || !stroke) && fill.alpha >= 0.99
      && pdfColorMatches(fill.color, expectedColor) && pdfBoxMatches(fill, box)
      && [0.25, 0.5, 0.75].every((x) => [0.25, 0.5, 0.75].every((y) => {
        // Edge matching applies the 1 pt tolerance, even for thin rules.
        return hitTest.isPointInPath(path,
          fill.left + (fill.right - fill.left) * x,
          fill.top + (fill.bottom - fill.top) * y,
          path.getFillType() === FillType.EvenOdd ? 'evenodd' : 'nonzero');
      }));
  });
}

export function pdfHasFill(
  paint: PdfPaint,
  box: PdfBox,
  expectedColor: string,
): boolean {
  return hasPaint(paint, box, expectedColor, false);
}

export function pdfHasRule(
  paint: PdfPaint,
  box: PdfBox,
  expectedColor: string,
): boolean {
  return hasPaint(paint, box, expectedColor, true);
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

function imageSignature(
  rgba: ArrayLike<number>, width: number, height: number,
): number[] {
  const signature: number[] = [];
  for (let row = 0; row < 4; row += 1) {
    for (let column = 0; column < 4; column += 1) {
      const startX = Math.floor(column * width / 4);
      const startY = Math.floor(row * height / 4);
      const endX = Math.max(startX + 1, Math.floor((column + 1) * width / 4));
      const endY = Math.max(startY + 1, Math.floor((row + 1) * height / 4));
      const means = [0, 0, 0, 0];
      for (let y = startY; y < endY; y += 1) {
        for (let x = startX; x < endX; x += 1) {
          for (let channel = 0; channel < 4; channel += 1) {
            means[channel]! += rgba[(y * width + x) * 4 + channel]!;
          }
        }
      }
      signature.push(...means.map((sum) => sum / ((endX - startX)
        * (endY - startY))));
    }
  }
  return signature;
}

export function pdfImageMatches(
  image: PdfImage | undefined,
  expected: Pick<PdfImage, 'width' | 'height' | 'signature'>,
  box: PdfBox,
): boolean {
  // Each 4 by 4 cell allows 8/255 channel drift from PDF image encoding.
  return image !== undefined && image.width === expected.width
    && image.height === expected.height && pdfBoxMatches(image, box)
    && image.signature.length === 64 && expected.signature.length === 64
    && image.signature.every((value, index) =>
      Math.abs(value - expected.signature[index]!) <= 8);
}

export function pdfImageDiagnostic(
  image: PdfImage | undefined,
  expected: Pick<PdfImage, 'width' | 'height' | 'signature'>,
  box: PdfBox,
): string {
  return 'PDF photo measurements: ' + JSON.stringify({
    actual: image && { width: image.width, height: image.height,
      left: image.left, top: image.top,
      right: image.right, bottom: image.bottom },
    expected: { width: expected.width, height: expected.height, ...box },
    maximumChannelDelta: image && Math.max(...image.signature.map(
      (value, index) => Math.abs(value - expected.signature[index]!),
    )),
  });
}

export async function fixturePhotoIdentity(): Promise<Pick<PdfImage,
  'width' | 'height' | 'signature'>> {
  const image = await loadImage(FIXED_PHOTO_DATA_URL);
  const canvas = createCanvas(image.width, image.height);
  const context = canvas.getContext('2d');
  context.drawImage(image, 0, 0);
  return { width: image.width, height: image.height,
    signature: imageSignature(context.getImageData(
      0, 0, image.width, image.height,
    ).data, image.width, image.height) };
}

async function object(page: PDFPageProxy, id: unknown): Promise<unknown> {
  if (typeof id !== 'string') unsupported();
  const objects = id.startsWith('g_') ? page.commonObjs : page.objs;
  return new Promise((resolve) => objects.get(id, resolve));
}

function imageIdentity(value: unknown): Pick<PdfImage,
  'width' | 'height' | 'signature'> {
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
    signature: imageSignature(rgba, width, height) };
}

export async function pdfGlyphExtents(page: PDFPageProxy) {
  const operators = await page.getOperatorList({ intent: 'print' });
  type Extent = { ascent: number; descent: number };
  const fonts = new Map<string, Map<string, Extent>>();
  let fontId = '';
  const stack: string[] = [];
  for (const [index, op] of operators.fnArray.entries()) {
    const args = operators.argsArray[index] ?? [];
    if (op === OPS.save || op === OPS.paintFormXObjectBegin) stack.push(fontId);
    else if (op === OPS.restore || op === OPS.paintFormXObjectEnd) {
      fontId = stack.pop() ?? unsupported(op);
    } else if (op === OPS.setFont) {
      if (typeof args[0] !== 'string') unsupported(op);
      fontId = args[0];
    } else if (op === OPS.showText) {
      if (!Array.isArray(args[0])) unsupported(op);
      const font = await object(page, fontId) as {
        loadedName: string; isType3Font?: boolean; fontMatrix: number[];
        charProcOperatorList?: Map<string, GlyphProgram>;
      };
      const metrics = fonts.get(fontId) ?? new Map();
      fonts.set(fontId, metrics);
      for (const glyph of args[0]) {
        if (typeof glyph === 'number') continue;
        if (!glyph || typeof glyph.unicode !== 'string') unsupported(op);
        const unicode = glyph.unicode.normalize('NFD');
        if (unicode.trim() === '' || metrics.has(unicode)) continue;
        let path: Path2D;
        if (font.isType3Font) {
          const program = font.charProcOperatorList?.get(glyph.operatorListId);
          if (program?.fnArray[0] !== OPS.setCharWidthAndBounds) {
            unsupported(op);
          }
          path = transformed(rectangle(numbers(program.argsArray[0], 6)
            .slice(2)), matrix(font.fontMatrix));
        } else {
          if (typeof glyph.fontChar !== 'string' || glyph.accent) {
            unsupported(op);
          }
          const pathId = `${font.loadedName}_path_${glyph.fontChar}`;
          if (!page.commonObjs.has(pathId)) unsupported(op);
          const outline = await object(page, pathId) as { path: unknown };
          path = drawing(outline?.path).path;
        }
        const box = bounds(path);
        if (!box) unsupported(op);
        metrics.set(unicode, { ascent: box.bottom, descent: box.top });
      }
    }
  }
  if (stack.length !== 0) unsupported();
  return fonts;
}

const textOperators = new Set<number>([
  OPS.dependency, OPS.setRenderingIntent, OPS.setFlatness,
  OPS.beginText, OPS.endText, OPS.setCharSpacing, OPS.setWordSpacing,
  OPS.setHScale, OPS.setLeading, OPS.setTextRise, OPS.moveText,
  OPS.setLeadingMoveText, OPS.nextLine, OPS.markPoint, OPS.markPointProps,
  OPS.beginMarkedContent, OPS.endMarkedContent, OPS.beginCompat, OPS.endCompat,
]);
const strokeOperators = new Map<number, string>([
  [OPS.setLineWidth, 'LW'], [OPS.setLineCap, 'LC'],
  [OPS.setLineJoin, 'LJ'], [OPS.setMiterLimit, 'ML'], [OPS.setDash, 'D'],
]);

function strokeSetting(stroke: Stroke, key: string, value: unknown): void {
  if (key === 'D') {
    if (!Array.isArray(value) || value.length !== 2) unsupported();
    stroke.dashed = numbers(value[0]).length > 0;
    numbers([value[1]], 1);
    return;
  }
  const number = numbers([value], 1)[0]!;
  if (key === 'LW') {
    if (number < 0) unsupported();
    stroke.width = number;
  } else if (key === 'LC' || key === 'LJ') {
    if (![0, 1, 2].includes(number)) unsupported();
    if (key === 'LC') stroke.cap = number;
    else stroke.join = number;
  } else if (key === 'ML') {
    if (number < 1) unsupported();
    stroke.miterLimit = number;
  } else if (key === 'CA') {
    if (number < 0 || number > 1) unsupported();
    stroke.alpha = number;
  } else unsupported();
}

export async function pdfPaintPage(page: PDFPageProxy): Promise<PdfPaint> {
  const operators = await page.getOperatorList({ intent: 'print' });
  return pdfPaintOperators(page, operators.fnArray, operators.argsArray);
}

export async function pdfPaintOperators(
  page: PDFPageProxy,
  functions: readonly number[],
  argumentsList: readonly (readonly unknown[] | null | undefined)[],
  type3Glyph = false,
): Promise<PdfPaint> {
  if (functions.length !== argumentsList.length) unsupported();
  const viewport = page.getViewport({ scale: 1 });
  let state: State = { matrix: matrix(viewport.transform),
    clip: rectangle([0, 0, viewport.width, viewport.height]),
    color: '#000000', alpha: 1, font: '', size: 0, textMatrix: identity(),
    textPosition: { x: 0, y: 0, lineX: 0, lineY: 0, leading: 0,
      rise: 0, scale: 1, charGap: 0, wordGap: 0, unit: 0.001 },
    transparent: false, bold: false, type3: undefined,
    stroke: { color: '#000000', alpha: 1, transparent: false,
      width: 1, cap: StrokeCap.Butt, join: StrokeJoin.Miter,
      miterLimit: 10, dashed: false } };
  const stack: State[] = [];
  const fills: PdfFill[] = [];
  const images: PdfImage[] = [];
  const text: PdfInk[] = [];
  const checkedGlyphs = new WeakSet<GlyphProgram>();
  let pendingClip: FillType | undefined;
  const save = () => stack.push({ ...state, stroke: { ...state.stroke },
    textPosition: { ...state.textPosition } });
  const restore = () => {
    state = stack.pop() ?? unsupported();
  };
  const record = (
    path: Path2D, color: string, alpha: number, curves: number,
    stroke = false,
  ) => {
    const visible = clipped(transformed(path, state.matrix), state.clip);
    const box = bounds(visible);
    if (!box || alpha <= 0) return;
    const fill = { ...box, color, alpha, curves };
    shapes.set(fill, { path: visible, stroke });
    fills.push(fill);
  };
  const visit = async (op: number, args: readonly unknown[]) => {
    // Uncolored Type3 glyphs inherit the text ink. Their paths must never
    // become candidates for page regions, rules or level widgets.
    if (type3Glyph && [OPS.setFillRGBColor, OPS.setStrokeRGBColor,
      OPS.setFillTransparent, OPS.setStrokeTransparent, OPS.setFont,
      OPS.showText, OPS.paintImageXObject, OPS.paintInlineImageXObject]
      .includes(op)) unsupported();
    if (op === OPS.save) save();
    else if (op === OPS.restore) restore();
    else if (op === OPS.transform) {
      state.matrix = multiply(state.matrix, matrix(args));
    } else if (op === OPS.setFillRGBColor) {
      state.color = color(args[0]);
      state.transparent = false;
    } else if (op === OPS.setFillTransparent) state.transparent = true;
    else if (op === OPS.setStrokeRGBColor) {
      state.stroke.color = color(args[0]);
      state.stroke.transparent = false;
    } else if (op === OPS.setStrokeTransparent) {
      state.stroke.transparent = true;
    } else if (strokeOperators.has(op)) {
      const key = strokeOperators.get(op)!;
      strokeSetting(state.stroke, key, key === 'D' ? args : args[0]);
    } else if (op === OPS.setGState) {
      if (!Array.isArray(args[0])) unsupported();
      for (const [key, value] of args[0] as [string, unknown][]) {
        if (key === 'ca') {
          state.alpha = numbers([value], 1)[0]!;
          if (state.alpha < 0 || state.alpha > 1
            || (type3Glyph && state.alpha !== 1)) unsupported();
        } else if (['LW', 'LC', 'LJ', 'ML', 'D', 'CA'].includes(key)) {
          strokeSetting(state.stroke, key, value);
          if (type3Glyph && state.stroke.alpha !== 1) unsupported();
        } else if (key === 'BM' && value === 'source-over') continue;
        else if ((key === 'SMask' && !value) || (key === 'TR'
          && value === null)) continue;
        else if (!['RI', 'FL'].includes(key)) unsupported();
      }
    } else if (op === OPS.clip || op === OPS.eoClip) {
      pendingClip = op === OPS.clip ? FillType.Winding : FillType.EvenOdd;
    } else if (op === OPS.setCharWidthAndBounds && type3Glyph) {
      const [, , x1, y1, x2, y2] = numbers(args, 6) as Matrix;
      state.clip = clipped(transformed(rectangle([
        Math.min(x1, x2), Math.min(y1, y2),
        Math.max(x1, x2), Math.max(y1, y2),
      ]), state.matrix), state.clip);
    } else if (op === OPS.constructPath) {
      const consume = args[0];
      if (!Array.isArray(args[1]) || args[1].length !== 1) unsupported();
      const { path, curves } = args[1][0] === null
        ? { path: new Path2D(), curves: 0 }
        : drawing(args[1][0]);
      if ([OPS.closeStroke, OPS.closeFillStroke, OPS.closeEOFillStroke]
        .includes(consume as number)) path.closePath();
      if ([OPS.eoFill, OPS.eoFillStroke, OPS.closeEOFillStroke]
        .includes(consume as number)) path.setFillType(FillType.EvenOdd);
      if ([OPS.fill, OPS.eoFill, OPS.fillStroke, OPS.eoFillStroke,
        OPS.closeFillStroke, OPS.closeEOFillStroke]
        .includes(consume as number)) {
        if (!state.transparent) record(path, state.color, state.alpha, curves);
      } else if (![OPS.stroke, OPS.closeStroke, OPS.endPath]
        .includes(consume as number)) unsupported();
      if ([OPS.stroke, OPS.closeStroke, OPS.fillStroke, OPS.eoFillStroke,
        OPS.closeFillStroke, OPS.closeEOFillStroke].includes(consume as number)
        && !state.stroke.transparent && state.stroke.alpha > 0) {
        const stroke = state.stroke;
        if (stroke.width === 0 || stroke.dashed) unsupported();
        record(new Path2D(path).stroke(stroke), stroke.color,
          stroke.alpha, curves, true);
      }
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
        fontMatrix?: number[]; vertical?: boolean;
        charProcOperatorList?: Map<string, GlyphProgram>;
      };
      if (typeof font.name !== 'string' || font.vertical) unsupported();
      state.type3 = font.isType3Font ? font.charProcOperatorList : undefined;
      if (font.isType3Font && !(state.type3 instanceof Map)) unsupported();
      state.font = font.name;
      state.bold = font.bold === true || /bold/iu.test(font.name);
      state.size = numbers([args[1]], 1)[0]!;
      state.textPosition.unit = numbers([font.fontMatrix?.[0] ?? 0.001], 1)[0]!;
    } else if (op === OPS.setTextMatrix || op === OPS.beginText) {
      state.textMatrix = op === OPS.beginText ? identity() : matrix(args[0]);
      Object.assign(state.textPosition, { x: 0, y: 0, lineX: 0, lineY: 0 });
    } else if ([OPS.moveText, OPS.setLeadingMoveText, OPS.nextLine]
      .includes(op)) {
      const position = state.textPosition;
      const [x, y] = op === OPS.nextLine
        ? [0, -position.leading]
        : numbers(args, 2);
      if (op === OPS.setLeadingMoveText) position.leading = -y!;
      position.lineX += x!;
      position.lineY += y!;
      position.x = position.lineX;
      position.y = position.lineY;
    } else if ([OPS.setLeading, OPS.setTextRise, OPS.setHScale,
      OPS.setCharSpacing, OPS.setWordSpacing].includes(op)) {
      const value = numbers(args, 1)[0]!;
      const key = op === OPS.setLeading
        ? 'leading'
        : op === OPS.setTextRise
          ? 'rise'
          : op === OPS.setHScale
            ? 'scale'
            : op === OPS.setCharSpacing ? 'charGap' : 'wordGap';
      state.textPosition[key] = op === OPS.setHScale ? value / 100 : value;
    } else if (op === OPS.beginMarkedContentProps) {
      if (args[0] === 'OC') unsupported();
    } else if (op === OPS.setTextRenderingMode) {
      if (args[0] !== 0) unsupported();
    } else if (op === OPS.showText) {
      if (!Array.isArray(args[0])) unsupported();
      if (state.type3) {
        for (const glyph of args[0] as unknown[]) {
          if (typeof glyph === 'number') continue;
          if (!glyph || typeof glyph !== 'object'
            || !('operatorListId' in glyph)
            || typeof glyph.operatorListId !== 'string') unsupported();
          const program = state.type3.get(glyph.operatorListId);
          if (!program) unsupported();
          if (!checkedGlyphs.has(program)) {
            if (program.fnArray.length > 0
              && program.fnArray[0] !== OPS.setCharWidthAndBounds) {
              unsupported(program.fnArray[0]);
            }
            const ink = await pdfPaintOperators(page,
              program.fnArray, program.argsArray, true);
            if (ink.fills.some((fill) => fill.alpha !== 1)) unsupported();
            checkedGlyphs.add(program);
          }
          if (program.fnArray.length === 0 && 'unicode' in glyph
            && typeof glyph.unicode === 'string'
            && glyph.unicode.trim() !== '') unsupported();
        }
      }
      const value = (args[0] as unknown[]).map((glyph) => {
        if (typeof glyph === 'number') return '';
        if (glyph === null || typeof glyph !== 'object'
          || !('unicode' in glyph) || typeof glyph.unicode !== 'string') {
          return unsupported();
        }
        return glyph.unicode;
      }).join('').normalize('NFC');
      const m = multiply(state.matrix, state.textMatrix);
      const position = state.textPosition;
      let x = position.x;
      for (const glyph of args[0]) {
        if (typeof glyph !== 'number') break;
        x -= glyph * state.size / 1000 * position.scale;
      }
      const y = position.y + position.rise;
      if (state.alpha !== 1 || state.transparent) unsupported();
      text.push({ text: value, font: state.font, color: state.color,
        bold: state.bold, x: m[4] + m[0] * x + m[2] * y,
        y: m[5] + m[1] * x + m[3] * y,
        size: Math.abs(state.size) * Math.hypot(m[2], m[3]) });
      for (const glyph of args[0]) {
        const direction = Math.sign(state.size);
        const advance = typeof glyph === 'number'
          ? -glyph * Math.abs(state.size) / 1000
          : numbers([glyph.width], 1)[0]! * Math.abs(state.size) * position.unit
            + direction * (position.charGap
              + (glyph.isSpace ? position.wordGap : 0));
        position.x += advance * position.scale * direction;
      }
    } else if (!textOperators.has(op)) unsupported();
  };
  for (let index = 0; index < functions.length; index += 1) {
    const op = functions[index]!;
    try {
      await visit(op, argumentsList[index] ?? []);
    } catch (error) {
      if (error instanceof UnsupportedPaint && error.operator !== undefined) {
        throw error;
      }
      unsupported(op);
    }
  }
  if (stack.length !== 0 || pendingClip !== undefined) unsupported();
  return { fills, images, text };
}
