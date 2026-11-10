import { expect, type Page, type TestInfo } from '@playwright/test';
import {
  getDocument,
  type PDFPageProxy,
} from 'pdfjs-dist/legacy/build/pdf.mjs';
import { writeFile } from 'node:fs/promises';

import {
  pdfBoxMatches,
  pdfColorMatches,
  pdfHasFill,
  pdfIsStroke,
  pdfPaintMatch,
  pdfPaintPage,
  pdfShapePaintMatches,
  type PdfBox,
  type PdfFill,
  type PdfPaint,
} from './pdf-paint';
import { pdfGlyphExtents, pdfTextExtents } from './pdf-fonts';

export interface PdfTextItem {
  readonly text: string;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
  readonly bounds?: PdfBox;
}

export interface PdfTextPage {
  readonly width: number;
  readonly height: number;
  readonly text: string;
  readonly items: readonly PdfTextItem[];
}

export interface PdfMembership {
  readonly pageCount: number;
  readonly pages: readonly {
    readonly main: {
      readonly first: string | null;
      readonly last: string | null;
    };
    readonly sidebar: {
      readonly first: string | null;
      readonly last: string | null;
    };
    readonly start: boolean;
    readonly end: boolean;
    readonly opposite: boolean;
  }[];
}

export function normalizePdfMarkers(text: string): string {
  return text
    .replace(/\b(MAIN|SIDEBAR)-\s+(?=(?:ENTRY|START|END|SHORT)\b)/g, '$1-')
    .replace(/\b((?:MAIN|SIDEBAR)-ENTRY)-\s+(?=\d{2}\b)/g, '$1-');
}

function orderedText(page: PdfTextPage): string {
  return normalizePdfMarkers([...page.items]
    .sort((a, b) => b.y - a.y || a.x - b.x)
    .map((item) => item.text)
    .join(' ').replace(/\s+/g, ' '));
}

export function pdfEntryIds(
  pages: readonly PdfTextPage[],
  column: 'MAIN' | 'SIDEBAR',
): string[] {
  const pattern = new RegExp(`${column}-ENTRY-\\d{2}`, 'g');
  return pages.flatMap((page) =>
    [...orderedText(page).matchAll(pattern)].map((match) => match[0]),
  );
}

export function pdfMembership(
  pages: readonly PdfTextPage[],
  markers: {
    readonly start: string;
    readonly end: string;
    readonly opposite: string;
  },
): PdfMembership {
  return {
    pageCount: pages.length,
    pages: pages.map((page) => {
      const text = orderedText(page);
      const column = (name: 'MAIN' | 'SIDEBAR') => {
        const ids = pdfEntryIds([page], name);
        return { first: ids[0] ?? null, last: ids.at(-1) ?? null };
      };
      return {
        main: column('MAIN'),
        sidebar: column('SIDEBAR'),
        start: text.includes(markers.start),
        end: text.includes(markers.end),
        opposite: text.includes(markers.opposite),
      };
    }),
  };
}

export async function pdfTextPage(page: PDFPageProxy): Promise<PdfTextPage> {
  const content = await page.getTextContent();
  const glyphs = await pdfGlyphExtents(page);
  const items = content.items.flatMap((item): PdfTextItem[] => {
    if (!('str' in item) || item.str.trim() === ''
      || (item.width === 0 && item.height === 0)) return [];
    const style = content.styles[item.fontName];
    const [a, b, c, d, x, y] = item.transform as number[];
    for (const [field, value] of Object.entries({
      a, b, c, d, x, y, width: item.width,
    })) {
      if (!Number.isFinite(value)) {
        throw new Error('Unsupported PDF glyph geometry: '
          + `non-finite ${field} (font ${item.fontName}).`);
      }
    }
    const advance = Math.hypot(a!, b!);
    if (advance === 0 || style?.vertical) {
      throw new Error('Unsupported PDF glyph geometry.');
    }
    const { ascent, descent } = pdfTextExtents(item.str, style,
      glyphs.get(item.fontName));
    const corners = [0, item.width / advance].flatMap((end) =>
      [descent, ascent].map((edge) => ({
        x: x! + a! * end + c! * edge,
        y: y! + b! * end + d! * edge,
      })),
    );
    return [{
      text: item.str.normalize('NFC'),
      x: item.transform[4]!,
      y: item.transform[5]!,
      width: item.width,
      height: Number.isFinite(item.height) ? item.height : Math.hypot(c!, d!),
      bounds: {
        left: Math.min(...corners.map((point) => point.x)),
        right: Math.max(...corners.map((point) => point.x)),
        top: page.view[3]! - Math.max(...corners.map((point) => point.y)),
        bottom: page.view[3]! - Math.min(...corners.map((point) => point.y)),
      },
    }];
  });
  const viewport = page.getViewport({ scale: 1 });
  return {
    width: viewport.width,
    height: viewport.height,
    text: normalizePdfMarkers(
      items.map((item) => item.text).join(' ').replace(/\s+/g, ' '),
    ),
    items,
  };
}

export function pdfGlyphGeometry(
  page: PdfTextPage,
  margins: { x: number; y: number },
): { contained: boolean; separated: boolean } {
  const items = [...page.items].sort((a, b) => b.y - a.y || a.x - b.x);
  return {
    contained: items.length > 0 && items.every(({ bounds }) => bounds
      && bounds.left >= margins.x - 1
      && bounds.right <= page.width - margins.x + 1
      && bounds.top >= margins.y - 1
      && bounds.bottom <= page.height - margins.y + 1),
    separated: items.every((item, index) => items.slice(index + 1)
      .every((next) => Math.min(item.bounds?.right ?? item.x + item.width,
        next.bounds?.right ?? next.x + next.width)
      - Math.max(item.bounds?.left ?? item.x, next.bounds?.left ?? next.x) <= 1
      || (item.bounds !== undefined && next.bounds !== undefined
        && Math.min(item.bounds.bottom, next.bounds.bottom)
        - Math.max(item.bounds.top, next.bounds.top) <= 1))),
  };
}

export function pdfColumnsMatch(
  page: PdfTextPage,
  layout: { sidebar: PdfBox | null; header: PdfBox | null },
  margins: { x: number; y: number },
  firstPage: boolean,
): boolean {
  const sidebar = layout.sidebar;
  if (!sidebar || !layout.header) return false;
  const columnItems = page.items.filter((item) => item.bounds
    && (!firstPage || item.bounds.top >= sidebar.top - 1));
  return columnItems.length > 0 && columnItems.every((item) => {
    const box = item.bounds!;
    if (/\bSIDEBAR\b/u.test(item.text) && box.left < sidebar.left - 1) {
      return false;
    }
    if (/\bMAIN\b/u.test(item.text) && box.left >= sidebar.left - 1) {
      return false;
    }
    return box.left >= sidebar.left - 1
      ? box.right <= page.width - margins.x + 1
      : box.right <= sidebar.left - 8 * 72 / 25.4 + 1;
  });
}

export interface PdfDecoration extends PdfBox {
  readonly color: string;
  readonly stroke: boolean;
}

function linkUnderlines(paint: PdfPaint, page: PdfTextPage): PdfFill[] {
  return paint.fills.filter((fill) => fill.alpha >= 0.99
    && !pdfColorMatches(fill.color, '#ffffff')
    && page.items.some((item) => {
      const bounds = item.bounds;
      if (!bounds) return false;
      const size = item.height;
      if (pdfIsStroke(fill)) return false;
      // A link may cover only part of an extracted text item, after a label.
      const baseline = page.height - item.y;
      return fill.bottom - fill.top <= 1
        && fill.left >= bounds.left - 1 && fill.right <= bounds.right + 1
        && fill.top >= baseline - 1
        && fill.bottom <= baseline + size * 0.3 + 1
        && paint.text.some((ink) => pdfColorMatches(ink.color, fill.color)
          && ink.x >= fill.left - 1 && ink.x < fill.right
          && Math.abs(ink.y - baseline) <= 1);
    }));
}

export function pdfSurfacePaint(
  paint: PdfPaint,
  page: PdfTextPage,
  box: PdfBox,
  color: string,
  foreground: readonly PdfFill[] = [],
  icons: readonly PdfDecoration[] = [],
): boolean {
  const surface = paint.fills.find((fill) => fill.alpha >= 0.99
    && !pdfIsStroke(fill) && pdfColorMatches(fill.color, color)
    && pdfBoxMatches(fill, box));
  if (!surface) return false;
  if (foreground.some((fill) => !paint.fills.includes(fill))) return false;
  const decorations = linkUnderlines(paint, page).filter((fill) =>
    fill.left < box.right && fill.right > box.left
    && fill.top < box.bottom && fill.bottom > box.top);
  const ink = new Map(decorations.map((fill) => [fill, fill.color]));
  for (const icon of icons.filter((icon) => icon.left < box.right
    && icon.right > box.left && icon.top < box.bottom
    && icon.bottom > box.top)) {
    const fill = paint.fills.find((fill) => !decorations.includes(fill)
      && fill.alpha >= 0.99 && pdfIsStroke(fill) === icon.stroke
      && pdfColorMatches(fill.color, icon.color) && pdfBoxMatches(fill, icon));
    if (!fill) return false;
    decorations.push(fill);
    ink.set(fill, icon.color);
  }
  if (!decorations.every((fill) => pdfShapePaintMatches(paint, fill, color,
    ink.get(fill)!, decorations))) return false;
  // Partition native bounds so DOM edge tolerance cannot move gap probes.
  const exclusions = [...page.items.flatMap(({ bounds }) =>
    bounds ? [bounds] : []), ...foreground, ...decorations];
  let exposed: PdfBox[] = [surface];
  for (const shape of exclusions) {
    exposed = exposed.flatMap((area) => {
      const left = Math.max(area.left, shape.left);
      const right = Math.min(area.right, shape.right);
      const top = Math.max(area.top, shape.top);
      const bottom = Math.min(area.bottom, shape.bottom);
      if (right <= left || bottom <= top) return [area];
      return [
        { ...area, right: left },
        { ...area, left: right },
        { left, right, top: area.top, bottom: top },
        { left, right, top: bottom, bottom: area.bottom },
      ].filter((part) => part.right > part.left && part.bottom > part.top);
    });
  }
  // Probe unknown paint bounds even when a patch misses a large area's grid.
  return exposed.length > 0 && exposed.every((area) => {
    const probes = [area, ...paint.fills.map((fill) => ({
      left: Math.max(area.left, fill.left),
      right: Math.min(area.right, fill.right),
      top: Math.max(area.top, fill.top),
      bottom: Math.min(area.bottom, fill.bottom),
    })).filter((part) => part.right > part.left && part.bottom > part.top)];
    return probes.every((probe) => pdfHasFill(paint, surface, color, probe));
  });
}

export function pdfSidebarPaint(
  paint: PdfPaint,
  page: PdfTextPage,
  sidebar: PdfBox,
  marginY: number,
  firstPage: boolean,
  color: string,
  expected: { body: string; headings: readonly (PdfBox & {
    icons?: readonly PdfDecoration[];
  })[]; },
): boolean {
  // Overflow fixtures use full-width level-five bars and heading rules.
  const fullWidth = paint.fills.filter((fill) =>
    Math.abs(fill.left - sidebar.left) <= 1
    && Math.abs(fill.right - sidebar.right) <= 1);
  const bars = fullWidth.filter((fill) =>
    Math.abs(fill.bottom - fill.top - 3) <= 1
    && pdfColorMatches(fill.color, '#0f6b7d'));
  const rules = fullWidth.filter((fill) =>
    Math.abs(fill.bottom - fill.top - 0.75) <= 0.1);
  const column = page.items.filter((item) => item.bounds
    && item.bounds.left >= sidebar.left - 1
    && (!firstPage || item.bounds.top >= sidebar.top - 1));
  const markers = pdfEntryIds([page], 'SIDEBAR');
  if (markers.length === 0 && pdfMarkerPosition(page, 'SIDEBAR-SHORT')) {
    markers.push('SIDEBAR-SHORT');
  }
  const titles = markers.map((marker) => pdfMarkerPosition(page, marker));
  const foreground: PdfFill[] = [];
  for (const [index, item] of titles.entries()) {
    const title = item?.bounds;
    if (!title) continue;
    const body = pdfMarkerPosition({ ...page,
      items: column.filter(({ bounds }) => bounds!.top >= title.bottom - 1
        && bounds!.top < (titles[index + 1]?.bounds?.top
          ?? page.height - marginY)),
    }, expected.body)?.bounds;
    const matches = bars.filter((bar) => body
      && bar.top >= title.bottom - 1 && bar.bottom <= body.top + 1);
    const fill = matches.length === 1
      ? pdfPaintMatch(paint, matches[0]!, '#0f6b7d')
      : undefined;
    if (fill) {
      foreground.push(fill);
    }
  }
  for (const heading of expected.headings) {
    const top = heading.bottom + 14 * 0.25 * 0.75;
    const box = { left: sidebar.left, right: sidebar.right,
      top, bottom: top + 0.75 };
    const matches = rules.filter((rule) => pdfBoxMatches(rule, box));
    const fill = matches.length === 1
      ? pdfPaintMatch(paint, matches[0]!, '#8eb8c2', true)
      : undefined;
    if (fill) {
      foreground.push(fill);
    }
  }
  const complete = bars.length === titles.length
    && rules.length === expected.headings.length
    && new Set(foreground).size === titles.length + expected.headings.length;
  const icons = expected.headings.flatMap((heading) => heading.icons ?? []);
  if (column.length === 0 && !firstPage) {
    // A short sidebar still paints alongside the continuing main flow.
    return paint.fills.some((fill) =>
      pdfSurfacePaint(paint, page, fill, color, foreground, icons)
      && Math.abs(fill.left - sidebar.left) <= 1
      && Math.abs(fill.right - sidebar.right) <= 1
      && Math.abs(fill.top - marginY) <= 1 && fill.bottom > marginY + 1)
    && complete;
  }
  const bottom = Math.max(...column.map((item) => item.bounds!.bottom));
  return paint.fills.some((fill) => Math.abs(fill.left - sidebar.left) <= 1
    && Math.abs(fill.right - sidebar.right) <= 1
    && Math.abs(fill.top - (firstPage ? sidebar.top : marginY)) <= 1
    && fill.bottom >= bottom - 1
    && fill.bottom <= page.height - marginY + 1
    && pdfSurfacePaint(paint, page, fill, color, foreground, icons))
  && complete;
}

export function pdfParagraphGeometry(
  page: PdfTextPage,
  paragraphs: readonly { text: string; pitch: number }[],
): boolean {
  const compact = (text: string) => text.normalize('NFC').replace(/\s+/gu, '');
  const text = page.items.map((item) => compact(item.text)).join('');
  return paragraphs.every((paragraph) => {
    const value = compact(paragraph.text);
    if (value === '') return true;
    const start = text.indexOf(value);
    if (start === -1) return false;
    let offset = 0;
    const lines: number[] = [];
    for (const item of page.items) {
      const end = offset + compact(item.text).length;
      if (end > start && offset < start + value.length
        && !lines.some((y) => Math.abs(y - item.y) <= 1)) lines.push(item.y);
      offset = end;
    }
    lines.sort((a, b) => b - a);
    return lines.every((y, index) => index === 0
      || Math.abs(lines[index - 1]! - y - paragraph.pitch) <= 1);
  });
}

export function pdfMarkerPosition(
  page: PdfTextPage,
  marker: string,
): PdfTextItem | null {
  for (let index = 0; index < page.items.length; index += 1) {
    const first = page.items[index]!;
    let text = first.text;
    for (let end = index; end < Math.min(index + 3, page.items.length);) {
      const offset = normalizePdfMarkers(text).indexOf(marker);
      if (offset >= 0 && offset < first.text.length) return first;
      end += 1;
      const next = page.items[end];
      if (next === undefined) break;
      text += ` ${next.text}`;
    }
  }
  return null;
}

export interface PrintCase {
  readonly end: string;
  readonly fixture: 'print-main-overflow' | 'print-sidebar-overflow';
  readonly opposite: string;
  readonly prefix: 'MAIN-ENTRY-' | 'SIDEBAR-ENTRY-';
  readonly start: string;
  readonly total: number;
}

export const PRINT_CASES: readonly PrintCase[] = [
  {
    end: 'SIDEBAR-END',
    fixture: 'print-sidebar-overflow',
    opposite: 'MAIN-SHORT',
    prefix: 'SIDEBAR-ENTRY-',
    start: 'SIDEBAR-START',
    total: 13,
  },
  {
    end: 'MAIN-END',
    fixture: 'print-main-overflow',
    opposite: 'SIDEBAR-SHORT',
    prefix: 'MAIN-ENTRY-',
    start: 'MAIN-START',
    total: 9,
  },
];

export const EXPECTED_PAGES = 2;

type PrintLanguage = 'en' | 'vi';
type PrintPaper = 'a4' | 'letter';
type PrintScheme = 'light' | 'dark';
type MembershipSuffix = `${PrintLanguage}-${PrintPaper}-${PrintScheme}`;
type MembershipKey = `${PrintCase['fixture']}-${MembershipSuffix}`;

const MAIN_MEMBERSHIP: PdfMembership = { pageCount: 2, pages: [
  { main: { first: 'MAIN-ENTRY-01', last: 'MAIN-ENTRY-07' },
    sidebar: { first: null, last: null },
    start: true, end: false, opposite: true },
  { main: { first: 'MAIN-ENTRY-08', last: 'MAIN-ENTRY-09' },
    sidebar: { first: null, last: null },
    start: false, end: true, opposite: false },
] };
const MAIN_LETTER_VI_MEMBERSHIP: PdfMembership = { pageCount: 2, pages: [
  { main: { first: 'MAIN-ENTRY-01', last: 'MAIN-ENTRY-06' },
    sidebar: { first: null, last: null },
    start: true, end: false, opposite: true },
  { main: { first: 'MAIN-ENTRY-07', last: 'MAIN-ENTRY-09' },
    sidebar: { first: null, last: null },
    start: false, end: true, opposite: false },
] };
const SIDEBAR_MEMBERSHIP: PdfMembership = { pageCount: 2, pages: [
  { main: { first: null, last: null },
    sidebar: { first: 'SIDEBAR-ENTRY-01', last: 'SIDEBAR-ENTRY-11' },
    start: true, end: false, opposite: true },
  { main: { first: null, last: null },
    sidebar: { first: 'SIDEBAR-ENTRY-12', last: 'SIDEBAR-ENTRY-13' },
    start: false, end: true, opposite: false },
] };
const SIDEBAR_LETTER_VI_MEMBERSHIP: PdfMembership = { pageCount: 2, pages: [
  { main: { first: null, last: null },
    sidebar: { first: 'SIDEBAR-ENTRY-01', last: 'SIDEBAR-ENTRY-10' },
    start: true, end: false, opposite: true },
  { main: { first: null, last: null },
    sidebar: { first: 'SIDEBAR-ENTRY-11', last: 'SIDEBAR-ENTRY-13' },
    start: false, end: true, opposite: false },
] };

const EXPECTED_MEMBERSHIP: Record<MembershipKey, PdfMembership> = {
  'print-main-overflow-en-a4-light': MAIN_MEMBERSHIP,
  'print-main-overflow-en-a4-dark': MAIN_MEMBERSHIP,
  'print-main-overflow-en-letter-light': MAIN_MEMBERSHIP,
  'print-main-overflow-en-letter-dark': MAIN_MEMBERSHIP,
  'print-main-overflow-vi-a4-light': MAIN_MEMBERSHIP,
  'print-main-overflow-vi-a4-dark': MAIN_MEMBERSHIP,
  'print-main-overflow-vi-letter-light': MAIN_LETTER_VI_MEMBERSHIP,
  'print-main-overflow-vi-letter-dark': MAIN_LETTER_VI_MEMBERSHIP,
  'print-sidebar-overflow-en-a4-light': SIDEBAR_MEMBERSHIP,
  'print-sidebar-overflow-en-a4-dark': SIDEBAR_MEMBERSHIP,
  'print-sidebar-overflow-en-letter-light': SIDEBAR_MEMBERSHIP,
  'print-sidebar-overflow-en-letter-dark': SIDEBAR_MEMBERSHIP,
  'print-sidebar-overflow-vi-a4-light': SIDEBAR_MEMBERSHIP,
  'print-sidebar-overflow-vi-a4-dark': SIDEBAR_MEMBERSHIP,
  'print-sidebar-overflow-vi-letter-light': SIDEBAR_LETTER_VI_MEMBERSHIP,
  'print-sidebar-overflow-vi-letter-dark': SIDEBAR_LETTER_VI_MEMBERSHIP,
};

export async function attachMembership(
  pages: readonly PdfTextPage[],
  printCase: PrintCase,
  testInfo: TestInfo,
): Promise<PdfMembership> {
  const membership = pdfMembership(pages, printCase);
  const membershipPath = testInfo.outputPath('membership.json');
  await writeFile(membershipPath, `${JSON.stringify(membership, null, 2)}\n`);
  await testInfo.attach('membership', {
    contentType: 'application/json', path: membershipPath,
  });
  return membership;
}

export function checkMembership(
  membership: PdfMembership,
  key: MembershipKey,
  approvals: Partial<typeof EXPECTED_MEMBERSHIP> = EXPECTED_MEMBERSHIP,
): void {
  const expected = approvals[key];
  if (expected === undefined) {
    throw new Error(
      `PDF membership has not been approved: ${key} `
      + JSON.stringify(membership),
    );
  }
  expect(membership, 'PDF approved page membership.').toStrictEqual(expected);
}

export function expectedEntryIds(printCase: PrintCase): string[] {
  return Array.from({ length: printCase.total }, (_, index) =>
    `${printCase.prefix}${String(index + 1).padStart(2, '0')}`,
  );
}

export async function printedTextPages(page: Page): Promise<PdfTextPage[]> {
  return withPrintedPdf(page, pdfTextPage);
}

export interface PdfDisplayPage {
  readonly text: PdfTextPage;
  readonly paint: PdfPaint;
}

export async function printedDisplayPages(
  page: Page,
): Promise<PdfDisplayPage[]> {
  return withPrintedPdf(page, async (pdfPage) => ({
    text: await pdfTextPage(pdfPage), paint: await pdfPaintPage(pdfPage),
  }));
}

export async function withPrintedPdf<T>(
  page: Page,
  read: (pdfPage: PDFPageProxy) => Promise<T>,
): Promise<T[]> {
  const pdf = await page.pdf({
    displayHeaderFooter: false,
    margin: { bottom: 0, left: 0, right: 0, top: 0 },
    preferCSSPageSize: true,
    printBackground: true,
    scale: 1,
  });
  const loadingTask = getDocument({
    data: new Uint8Array(pdf),
    isImageDecoderSupported: false,
    isOffscreenCanvasSupported: false,
    useSystemFonts: false,
  });
  try {
    const document = await loadingTask.promise;
    return await Promise.all(
      Array.from({ length: document.numPages }, async (_, index) =>
        read(await document.getPage(index + 1)),
      ),
    );
  } finally {
    await loadingTask.destroy();
  }
}

export interface PdfWidget {
  readonly entry: PdfBox;
  readonly title: PdfBox | null;
  readonly widget: PdfBox | null;
  readonly track: PdfBox | null;
  readonly fill?: PdfBox | null;
  readonly dots: readonly PdfBox[];
  readonly size: number;
  readonly paddingX: number;
  readonly paddingY: number;
  readonly border: number;
  readonly radius: number;
}

export function pdfWidgetMatches(
  page: PdfDisplayPage,
  measured: PdfWidget,
  level: number | undefined,
  mode: 'bar' | 'dots' | 'tag' | 'text',
  colors: {
    solid: string; track: string; onAccent: string; fontSize: number;
  },
): boolean {
  const { paint, text } = page;
  const inside = (inner: PdfBox, outer: PdfBox) => inner.left >= outer.left - 1
    && inner.right <= outer.right + 1 && inner.top >= outer.top - 1
    && inner.bottom <= outer.bottom + 1;
  if (level === undefined || mode === 'text') {
    return measured.widget === null && !paint.fills.some((fill) =>
      inside(fill, measured.entry) && !pdfColorMatches(fill.color, '#ffffff'));
  }
  const widget = measured.widget;
  if (!widget || !measured.title || widget.top < measured.title.bottom - 1
    || Math.abs(widget.left - measured.title.left) > 1
    || !inside(widget, measured.entry)) {
    return false;
  }
  if (mode === 'bar') {
    const track = measured.track;
    if (!track || Math.abs(track.bottom - track.top - 3) > 1
      || Math.abs(track.left - measured.title.left) > 1
      || Math.abs(track.right - measured.entry.right) > 1) return false;
    // A full solid bar hides its track; Chromium can omit the covered paint.
    const expected = { ...track,
      right: track.left + (track.right - track.left) * level / 5 };
    if (level < 5 && !pdfHasFill(paint, track, colors.track,
      { ...track, left: expected.right })) return false;
    return level === 0
      ? !paint.fills.some((fill) => inside(fill, track)
        && pdfColorMatches(fill.color, colors.solid))
      : pdfHasFill(paint, expected, colors.solid);
  }
  if (mode === 'dots') {
    const dots = measured.dots;
    const painted = paint.fills.filter((fill) => fill.curves >= 4
      && inside(fill, widget) && Math.abs(fill.right - fill.left - 5.25) <= 1
      && Math.abs(fill.bottom - fill.top - 5.25) <= 1);
    return dots.length === 5 && painted.length === 5
      && dots.every((dot, index) => {
        const previous = dots[index - 1];
        return Math.abs(dot.right - dot.left - 5.25) <= 1
          && Math.abs(dot.bottom - dot.top - 5.25) <= 1
          && (!previous || Math.abs(dot.left - previous.right - 3) <= 1)
          && paint.fills.filter((fill) => pdfBoxMatches(fill, dot)
            && fill.curves >= 4 && fill.alpha >= 0.99).length === 1
            && pdfHasFill(paint, dot,
              index < level ? colors.solid : colors.track);
      });
  }
  if (Math.abs(measured.size - colors.fontSize) > 0.1
    || Math.abs(measured.paddingX - colors.fontSize * 0.5) > 0.1
    || Math.abs(measured.paddingY - colors.fontSize * 0.15) > 0.1
    || measured.border !== 0
    || Math.abs(measured.radius - 3) > 0.1) return false;
  const items = text.items.filter((item) => item.bounds
    && inside(item.bounds, widget));
  const ink = paint.text.filter((run) => inside({
    left: run.x, right: run.x, top: run.y, bottom: run.y,
  }, widget));
  return items.map((item) => item.text).join('').trim() === `${level}/5`
    && items.length > 0 && pdfHasFill(paint, widget, colors.solid)
    && ink.map((run) => run.text).join('').trim() === `${level}/5`
    && ink.every((run) => pdfColorMatches(run.color, colors.onAccent)
      && Math.abs(run.size - colors.fontSize * 0.75) <= 0.075);
}

export function pdfWidgetDiagnostic(
  page: PdfDisplayPage, measured: PdfWidget,
): string {
  const intersects = (box: PdfBox) => box.left <= measured.entry.right + 1
    && box.right >= measured.entry.left - 1
    && box.top <= measured.entry.bottom + 1
    && box.bottom >= measured.entry.top - 1;
  return 'PDF widget measurements: ' + JSON.stringify({
    entry: measured.entry, title: measured.title, widget: measured.widget,
    track: measured.track, fill: measured.fill, dots: measured.dots,
    size: measured.size, paddingX: measured.paddingX,
    paddingY: measured.paddingY, border: measured.border,
    radius: measured.radius,
    fills: page.paint.fills.filter(intersects),
    glyphs: page.text.items.flatMap(({ bounds }) =>
      bounds && intersects(bounds) ? [bounds] : []),
    ink: page.paint.text.filter((run) => intersects({
      left: run.x, right: run.x, top: run.y, bottom: run.y,
    })).map(({ x, y, color, size }) => ({ x, y, color, size })),
  });
}
