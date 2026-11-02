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
  pdfPaintPage,
  type PdfBox,
  type PdfPaint,
} from './pdf-paint';

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
    // PDF.js uses a 0.8 ascent ratio when font metrics are unavailable.
    const ascent = Number.isFinite(style?.ascent) ? style!.ascent : 0.8;
    const descent = Number.isFinite(style?.descent) ? style!.descent : -0.2;
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
      .every((next) => Math.abs(item.y - next.y) <= 1
        || Math.min(item.x + item.width, next.x + next.width)
        - Math.max(item.x, next.x) <= 1
        || (item.bounds !== undefined && next.bounds !== undefined
          && next.bounds.top >= item.bounds.bottom - 1))),
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
    const sidebarEntry = /SIDEBAR/u.test(item.text);
    if (sidebarEntry && box.left < sidebar.left - 1) return false;
    return box.left >= sidebar.left - 1
      ? box.right <= page.width - margins.x + 1
      : box.right <= sidebar.left - 8 * 72 / 25.4 + 1;
  });
}

export function pdfSidebarPaint(
  paint: PdfPaint,
  page: PdfTextPage,
  sidebar: PdfBox,
  marginY: number,
  firstPage: boolean,
  color: string,
): boolean {
  const column = page.items.filter((item) => item.bounds
    && item.bounds.left >= sidebar.left - 1
    && (!firstPage || item.bounds.top >= sidebar.top - 1));
  if (column.length === 0 && !firstPage) {
    // A short sidebar still paints alongside the continuing main flow.
    return paint.fills.some((fill) => pdfHasFill(paint, fill, color)
      && Math.abs(fill.left - sidebar.left) <= 1
      && Math.abs(fill.right - sidebar.right) <= 1
      && Math.abs(fill.top - marginY) <= 1 && fill.bottom > marginY + 1);
  }
  const bottom = Math.max(...column.map((item) => item.bounds!.bottom));
  return paint.fills.some((fill) => pdfHasFill(paint, fill, color)
    && Math.abs(fill.left - sidebar.left) <= 1
    && Math.abs(fill.right - sidebar.right) <= 1
    && Math.abs(fill.top - (firstPage ? sidebar.top : marginY)) <= 1
    && fill.bottom >= bottom - 1
    && fill.bottom <= page.height - marginY + 1);
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

const EXPECTED_MEMBERSHIP: Partial<
  Record<MembershipKey, PdfMembership | undefined>
> = {
  // approved after review
  'print-main-overflow-en-a4-light': undefined,
  'print-main-overflow-en-a4-dark': undefined,
  'print-main-overflow-en-letter-light': undefined,
  'print-main-overflow-en-letter-dark': undefined,
  'print-main-overflow-vi-a4-light': undefined,
  'print-main-overflow-vi-a4-dark': undefined,
  'print-main-overflow-vi-letter-light': undefined,
  'print-main-overflow-vi-letter-dark': undefined,
  'print-sidebar-overflow-en-a4-light': undefined,
  'print-sidebar-overflow-en-a4-dark': undefined,
  'print-sidebar-overflow-en-letter-light': undefined,
  'print-sidebar-overflow-en-letter-dark': undefined,
  'print-sidebar-overflow-vi-a4-light': undefined,
  'print-sidebar-overflow-vi-a4-dark': undefined,
  'print-sidebar-overflow-vi-letter-light': undefined,
  'print-sidebar-overflow-vi-letter-dark': undefined,
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
  approvals: typeof EXPECTED_MEMBERSHIP = EXPECTED_MEMBERSHIP,
): void {
  const expected = approvals[key];
  if (expected === undefined) {
    throw new Error(
      `PDF membership has not been approved: ${key} `
      + JSON.stringify(membership),
    );
  }
  expect(membership).toEqual(expected);
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

async function withPrintedPdf<T>(
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
      || Math.abs(track.right - measured.entry.right) > 1
      || !pdfHasFill(paint, track, colors.track)) return false;
    const expected = { ...track,
      right: track.left + (track.right - track.left) * level / 5 };
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
  return items.map((item) => item.text).join('').trim() === `${level}/5`
    && items.length > 0 && pdfHasFill(paint, widget, colors.solid)
    && paint.text.some((run) => run.text === `${level}/5`
      && pdfColorMatches(run.color, colors.onAccent));
}
