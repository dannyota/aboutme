import type { Path2D } from '@napi-rs/canvas';
import { expect, type Page } from '@playwright/test';
import { OPS, type PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';

import {
  measurePrintRules, measureTextContainment,
} from './display-assertions';
import { pdfDrawing } from './pdf-paint';
import { photoCropGeometry } from './support';

interface Extent {
  ascent: number;
  descent: number;
}

interface GlyphProgram {
  fnArray: readonly number[];
  argsArray: readonly (readonly unknown[] | null | undefined)[];
}

interface Font {
  loadedName?: string;
  isType3Font?: boolean;
  ascent?: number;
  descent?: number;
  bbox?: number[];
  fontMatrix?: number[];
  charProcOperatorList?: Map<string, GlyphProgram>;
}

interface FontExtents {
  glyphs: Map<string, Extent>;
  ascent?: number;
  descent?: number;
  bounds: Extent | undefined;
}

function unsupported(): never {
  throw new Error('Unsupported PDF glyph geometry.');
}

function finiteNumbers(value: unknown, count: number): value is number[] {
  return (Array.isArray(value) || ArrayBuffer.isView(value))
    && (value as number[]).length === count
    && Array.from(value as number[]).every((item) =>
      typeof item === 'number' && Number.isFinite(item));
}

function pathExtent(path: Path2D): Extent | undefined {
  const [left, bottom, right, top] = path.computeTightBounds();
  if (![left, bottom, right, top].every(Number.isFinite)
    || right <= left || top <= bottom) return undefined;
  return { ascent: top, descent: bottom };
}

function boxExtent(box: unknown, matrix: unknown): Extent | undefined {
  if (!finiteNumbers(box, 4) || !finiteNumbers(matrix, 6)
    || box[2]! <= box[0]! || box[3]! <= box[1]!) return undefined;
  const corners = [box[0]!, box[2]!].flatMap((x) =>
    [box[1]!, box[3]!].map((y) => [
      matrix[0]! * x + matrix[2]! * y + matrix[4]!,
      matrix[1]! * x + matrix[3]! * y + matrix[5]!,
    ]));
  if (!corners.flat().every(Number.isFinite)) return undefined;
  if (Math.max(...corners.map((point) => point[0]!))
    <= Math.min(...corners.map((point) => point[0]!))) return undefined;
  const ascent = Math.max(...corners.map((point) => point[1]!));
  const descent = Math.min(...corners.map((point) => point[1]!));
  return ascent > descent ? { ascent, descent } : undefined;
}

async function optionalObject(page: PDFPageProxy, id: string) {
  const objects = id.startsWith('g_') ? page.commonObjs : page.objs;
  if (!objects.has(id)) return undefined;
  return new Promise<unknown>((resolve) => objects.get(id, resolve));
}

async function glyphExtent(
  page: PDFPageProxy, font: Font,
  glyph: { fontChar?: string; operatorListId?: string; accent?: unknown },
): Promise<Extent | undefined> {
  // Font paths and Type3 boxes are optional evidence, not a text prerequisite.
  try {
    if (font.isType3Font) {
      const program = glyph.operatorListId === undefined
        ? undefined
        : font.charProcOperatorList?.get(glyph.operatorListId);
      const args = program?.argsArray[0];
      if (program?.fnArray[0] !== OPS.setCharWidthAndBounds
        || !finiteNumbers(args, 6)) return undefined;
      return boxExtent(args.slice(2), font.fontMatrix);
    }
    if (typeof font.loadedName !== 'string'
      || typeof glyph.fontChar !== 'string' || glyph.accent) return undefined;
    const pathId = `${font.loadedName}_path_${glyph.fontChar}`;
    const outline = await optionalObject(page, pathId) as
      { path?: unknown } | undefined;
    return outline?.path === undefined
      ? undefined
      : pathExtent(pdfDrawing(outline.path).path);
  } catch {
    return undefined;
  }
}

export async function pdfGlyphExtents(page: PDFPageProxy) {
  const operators = await page.getOperatorList({ intent: 'print' });
  const fonts = new Map<string, FontExtents>();
  const loaded = new Map<string, Font>();
  let fontId = '';
  const stack: string[] = [];
  for (const [index, op] of operators.fnArray.entries()) {
    const args = operators.argsArray[index] ?? [];
    if (op === OPS.save || op === OPS.paintFormXObjectBegin) stack.push(fontId);
    else if (op === OPS.restore || op === OPS.paintFormXObjectEnd) {
      fontId = stack.pop() ?? unsupported();
    } else if (op === OPS.setFont) {
      if (typeof args[0] !== 'string') unsupported();
      fontId = args[0];
    } else if (op === OPS.showText) {
      if (!Array.isArray(args[0]) || !fontId) unsupported();
      let font = loaded.get(fontId);
      if (!font) {
        font = (await optionalObject(page, fontId) ?? {}) as Font;
        loaded.set(fontId, font);
        fonts.set(fontId, { glyphs: new Map(), ascent: font.ascent,
          descent: font.descent,
          bounds: boxExtent(font.bbox, font.fontMatrix) });
      }
      const metrics = fonts.get(fontId)!;
      for (const glyph of args[0]) {
        if (typeof glyph === 'number') continue;
        if (!glyph || typeof glyph.unicode !== 'string') unsupported();
        const unicode = glyph.unicode.normalize('NFD');
        if (unicode.trim() === '' || metrics.glyphs.has(unicode)) continue;
        const extent = await glyphExtent(page, font, glyph);
        if (extent) metrics.glyphs.set(unicode, extent);
      }
    }
  }
  if (stack.length !== 0) unsupported();
  return fonts;
}

export function pdfTextExtents(
  text: string,
  style: { ascent?: number; descent?: number } | undefined,
  font: FontExtents | undefined,
): Extent {
  const positive = (value: number | undefined): value is number =>
    typeof value === 'number' && Number.isFinite(value) && value > 0;
  const negative = (value: number | undefined): value is number =>
    typeof value === 'number' && Number.isFinite(value) && value < 0;
  // Use declared bounds to cap fallback metrics; never cap measured glyph ink.
  // Without usable metrics, the baseline and transform give a 0.8/-0.2 em box.
  const fallback = {
    ascent: Math.min(positive(style?.ascent) ? style.ascent : 0.8,
      ...[font?.ascent, font?.bounds?.ascent].filter(positive)),
    descent: Math.max(negative(style?.descent) ? style.descent : -0.2,
      ...[font?.descent, font?.bounds?.descent].filter(negative)),
  };
  const keys = [...font?.glyphs.keys() ?? []]
    .sort((a, b) => b.length - a.length);
  let remaining = text.normalize('NFD');
  let ascent = -Infinity;
  let descent = Infinity;
  while (remaining.length > 0) {
    if (/^\s/u.test(remaining)) {
      remaining = remaining.slice(1);
      continue;
    }
    const key = keys.find((value) => remaining.startsWith(value));
    const extent = key ? font!.glyphs.get(key)! : fallback;
    ascent = Math.max(ascent, extent.ascent);
    descent = Math.min(descent, extent.descent);
    remaining = remaining.slice(key?.length ?? [...remaining][0]!.length);
  }
  return Number.isFinite(ascent) && Number.isFinite(descent)
    ? { ascent, descent }
    : fallback;
}

export function pdfFontFacesLoaded(
  faces: readonly { family: string; status: string;
    weight: string; style: string; }[],
  family: string,
): boolean {
  return [400, 700].every((weight) => faces.some((face) => {
    const range = face.weight.trim().split(/\s+/u).map(Number);
    return face.status === 'loaded' && face.style === 'normal'
      && face.family.replaceAll('"', '') === family
      && range.length >= 1 && range.length <= 2
      && range.every(Number.isFinite)
      && range[0]! <= weight && weight <= (range[1] ?? range[0]!);
  }));
}

export async function assertPrintDom(
  page: Page, family: string, columns: number,
  spacing = { entryGap: 10, sectionGap: 20 },
): Promise<void> {
  const root = page.locator('.resume-document');
  const fonts = await root.evaluate((element) => ({
    family: getComputedStyle(element).fontFamily.split(',')[0]!
      .replaceAll('"', '').trim(),
    faces: Array.from(document.fonts).map(({ family, status, weight, style }) =>
      ({ family, status, weight, style })),
  }));
  expect(fonts.family, 'Print requested font family.').toBe(family);
  expect(pdfFontFacesLoaded(fonts.faces, family),
    'Print requested fonts loaded.').toBe(true);
  const geometry = await root.evaluate((element, expected) => {
    const box = element.getBoundingClientRect();
    const sidebar = element.querySelector('.resume-sidebar')
      ?.getBoundingClientRect();
    const main = element.querySelector('.resume-main')?.getBoundingClientRect();
    const gap = 8 * 96 / 25.4;
    const flows = element.querySelectorAll(
      '.resume-main, .resume-sidebar, .layout-one-column, .resume-section',
    );
    const separated = Array.from(flows).every((flow) => {
      const boxes = Array.from(flow.children, (child) =>
        child.getBoundingClientRect());
      return boxes.every((rect, index) => rect.width > 0 && rect.height > 0
        && (!index || rect.top >= boxes[index - 1]!.bottom - 1));
    });
    const gaps = Array.from(flows).flatMap((flow) => {
      const children = Array.from(flow.children).filter((child) =>
        child.matches('.entry, .resume-section'));
      return children.slice(1).map((child, index) => {
        const previous = children[index]!.getBoundingClientRect();
        const value = child.getBoundingClientRect().top - previous.bottom;
        const target = child.matches('.entry')
          ? expected.spacing.entryGap
          : expected.spacing.sectionGap;
        return Math.abs(value - target) <= 1;
      });
    });
    const textContained = (() => {
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      while (walker.nextNode()) {
        const node = walker.currentNode;
        if (!node.textContent?.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        for (let parent = node.parentElement; parent && parent !== element;
          parent = parent.parentElement) {
          if (!parent.matches('.entry-body, .entry-body p, .entry-body li, '
            + '.entry-header, .entry-title, .entry-subtitle, .entry-meta, '
            + '.resume-name, .resume-headline, .contact-chip')) continue;
          const bounds = parent.getBoundingClientRect();
          if (parent.scrollWidth > parent.clientWidth + 1) return false;
          if (Array.from(range.getClientRects()).some((rect) =>
            rect.left < bounds.left - 1 || rect.right > bounds.right + 1
            || rect.top < bounds.top - 1 || rect.bottom > bounds.bottom + 1)) {
            return false;
          }
        }
      }
      return true;
    })();
    return {
      overflow: element.scrollWidth <= element.clientWidth + 1
        && document.documentElement.scrollWidth
        <= document.documentElement.clientWidth + 1,
      separated,
      gaps: gaps.every(Boolean),
      textContained,
      columns: expected.columns === 1
        ? !sidebar
        : Boolean(sidebar && main
          && Math.abs(sidebar.width - box.width * 0.32) <= 1
          && Math.abs(sidebar.right - box.right) <= 1
          && Math.abs(main.left - box.left) <= 1
          && Math.abs(sidebar.left - main.right - gap) <= 1
          && Math.abs(sidebar.top - main.top) <= 1),
    };
  }, { columns, spacing });
  expect(geometry.columns, 'Print column geometry.').toBe(true);
  expect(geometry.overflow, 'Print horizontal overflow.').toBe(true);
  expect(geometry.separated, 'Print sibling separation.').toBe(true);
  expect(geometry.gaps, 'Print entry and section spacing.').toBe(true);
  expect(geometry.textContained, 'Print text containment.').toBe(true);
  for (const container of await root.locator(
    '.resume-header, .section-heading, .entry',
  ).all()) {
    expect(await measureTextContainment(container), 'Print text containment.')
      .toEqual({ visible: true, contained: true });
  }
  expect(await root.locator('img').evaluateAll(async (images) => {
    return (await Promise.all(images.map(async (image) => {
      try {
        await image.decode();
      } catch {
        return false;
      }
      const style = getComputedStyle(image);
      const rect = image.getBoundingClientRect();
      return image.complete && image.naturalWidth === 192
        && image.naturalHeight === 192
        && image.currentSrc.startsWith('data:image/png;base64,')
        && style.visibility === 'visible' && Number(style.opacity) > 0
        && rect.width > 0 && rect.height > 0;
    }))).every(Boolean);
  }), 'Print images decoded and visible.').toBe(true);
}

export async function assertPhotoCrop(
  page: Page,
  crop = { x: 0, y: 0, width: 1, height: 1 },
): Promise<void> {
  const measured = await photoCropGeometry(page);
  const expected = { scaleX: 1 / crop.width, scaleY: 1 / crop.height,
    left: -crop.x / crop.width, top: -crop.y / crop.height };
  for (const key of ['scaleX', 'scaleY', 'left', 'top'] as const) {
    expect(Math.abs(measured[key] - expected[key]), 'Print photo crop.')
      .toBeLessThanOrEqual(0.001);
  }
  expect(measured.overflow, 'Print photo clipping.').toBe('hidden');
  expect(measured.objectFit, 'Print photo fit.').toBe('cover');
}

export async function assertPrintRules(page: Page): Promise<void> {
  expect(await measurePrintRules(page), 'Print fragmentation rules.').toEqual({
    bodyHeaderAfter: 'avoid', entryInside: 'auto', headerInside: 'avoid',
    orphans: '2', printColorAdjust: 'exact', sectionAfter: 'avoid',
    sectionInside: 'auto', widows: '2',
  });
}
