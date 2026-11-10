import { expect } from '@playwright/test';
import type { PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import type { Resume } from '@aboutme/schema';

import type { measurePdfLayout } from './display-assertions';
import {
  checkMembership, expectedEntryIds, pdfColumnsMatch, pdfEntryIds,
  pdfGlyphGeometry, pdfMembership, pdfParagraphGeometry, pdfSidebarPaint,
  pdfSurfacePaint, pdfWidgetMatches, pdfWidgetDiagnostic,
  type PrintCase, type PdfDisplayPage, type PdfTextPage,
} from './pdf-display';
import {
  fixturePhotoIdentity, pdfImageMatches, pdfImageDiagnostic, pdfColorMatches,
  pdfPaintMatch, pdfTextStyleMatches, pdfTextStyleDiagnostic,
  type PdfBox, type PdfFill,
} from './pdf-paint';
import { pdfVariableTextWeights } from './pdf-text-style';

import {
  assertDomInputContent, assertPdfInputOccurrences, overflowPdfChunks,
  samplePdfChunks, samplePdfContentMatches,
} from './pdf-input-content';

export { samplePdfChunks, samplePdfContentMatches, type SamplePdfChunk }
  from './pdf-input-content';

// Type 3 text may omit descent; every catalog face fits within a quarter em.
const FALLBACK_DESCENT = -0.25;
const POINTS_PER_MM = 72 / 25.4;

export const FAMILIES: Readonly<Record<string, readonly [string, string]>> = {
  'be-vietnam-pro': ['Be Vietnam Pro', 'Noto Sans'],
  'source-sans-3': ['Source Sans 3', 'Noto Sans'],
  'inter': ['Inter', 'Noto Sans'],
  'alegreya': ['Alegreya', 'Noto Serif'],
};
const META_COLORS: Readonly<Record<string, readonly [string, string]>> = {
  'ats-plain': ['#404040', '#404040'],
  'consulting-formal': ['#666a6e', '#666a6e'],
  'creative-accent': ['#575351', '#57504c'],
  'elegant-serif-two': ['#605c59', '#5d5751'],
  'engineer-compact': ['#53575b', '#53575b'],
  'executive-band': ['#575f69', '#808ea2'],
  'graduate-friendly': ['#635f5c', '#615a53'],
  'international-lang': ['#5f5d5b', '#5c5955'],
  'minimal-air': ['#5b5d61', '#5b5d61'],
  'modern-sidebar': ['#556066', '#4e5b62'],
  'mono-print': ['#404040', '#404040'],
  'nordic-muted': ['#666b71', '#63686f'],
  'one-page-tight': ['#55595c', '#55595c'],
};

function cssHex(color: string): string {
  const channels = color.match(/[\d.]+/gu);
  if (!channels || channels.length !== 3) {
    throw new Error('Unsupported sample color.');
  }
  return '#' + channels.map((channel) => Number(channel)
    .toString(16).padStart(2, '0')).join('');
}

function mixedColor(accent: string, surface: string, share: number): string {
  return '#' + [1, 3, 5].map((offset) => Math.round(
    parseInt(accent.slice(offset, offset + 2), 16) * (1 - share)
    + parseInt(surface.slice(offset, offset + 2), 16) * share,
  ).toString(16).padStart(2, '0')).join('');
}

export async function assertSampleStyle(
  sample: Resume,
  templateId: string,
  lng: 'en' | 'vi',
  layout: Awaited<ReturnType<typeof measurePdfLayout>>,
  display: PdfDisplayPage,
  assertion?: 'font size' | 'text color' | 'icon color' | 'sidebar tint'
    | 'level bar' | 'photo' | 'margin' | 'heading rule' | 'line spacing'
    | 'text weight',
): Promise<void> {
  const check = (name: string, run: () => void) => {
    if (assertion === undefined || assertion === name) {
      run();
    }
  };
  const { font, spacing, colors, heading, sectionDisplay }
    = sample.customization;
  const base = font.baseSizePx;
  const [family, fallback] = FAMILIES[font.family]!;
  check('typography', () => {
    expect(layout.family.replaceAll('"', '')).toBe(`${family}, ${fallback}`);
    expect(layout.synthesis).toBe('none');
  });
  check('font size', () => {
    expect(Math.abs(layout.size - base), 'Print DOM font size.')
      .toBeLessThanOrEqual(0.1);
  });
  check('typography', () => {
    expect(Math.abs(layout.lineHeight - base * spacing.lineHeight))
      .toBeLessThanOrEqual(0.1);
    expect(layout.background).toBe('rgb(255, 255, 255)');
  });
  const target = sample.customization.layout.surfaceTarget ?? 'none';
  const accent = colors.accent ?? colors.primary;
  const foreground: PdfFill[] = [];
  const surfaceBox = target === 'sidebar'
    ? layout.sidebar
    : target === 'header' ? layout.header : null;
  for (const icon of layout.icons) {
    const tinted = surfaceBox && icon.left >= surfaceBox.left - 1
      && icon.right <= surfaceBox.right + 1
      && icon.top >= surfaceBox.top - 1
      && icon.bottom <= surfaceBox.bottom + 1;
    const color = META_COLORS[templateId]![tinted ? 1 : 0];
    check('icon color', () => {
      expect(pdfColorMatches(icon.color, color), 'Print icon color.')
        .toBe(true);
    });
  }
  check('geometry', () => {
    expect(layout.headerAlign)
      .toBe(sample.customization.header?.align ?? 'left');
    for (const gap of layout.entryGaps) {
      expect(Math.abs(gap - spacing.entryGap)).toBeLessThanOrEqual(1);
    }
    for (const gap of layout.sectionGaps) {
      expect(Math.abs(gap - spacing.sectionGap)).toBeLessThanOrEqual(1);
    }
  });
  for (const decoration of layout.headings) {
    const surface = target === 'sidebar' && decoration.sidebar
      ? colors.surface!
      : '#ffffff';
    check('heading rule', () => {
      expect(decoration.rule, 'Print heading rule.')
        .toBe(heading.showRule ? 1 : 0);
      expect(Math.abs(decoration.gap - spacing.sectionGap * 0.4))
        .toBeLessThanOrEqual(1);
    });
    if (heading.showRule) {
      const color = mixedColor(accent, surface, 0.6);
      check('heading rule', () => {
        expect(decoration.ruleStyle).toBe('solid');
        expect(pdfColorMatches(cssHex(decoration.color), color)).toBe(true);
      });
      const fill = pdfPaintMatch(display.paint, decoration.ruleBox,
        color, true);
      check('heading rule', () => {
        expect(fill).toBeDefined();
      });
      foreground.push(fill!);
    }
  }
  let transform = 'none';
  if (heading.style === 'uppercase') {
    transform = 'uppercase';
  }
  if (heading.style === 'titlecase' && lng === 'en') {
    transform = 'capitalize';
  }
  const chunks = samplePdfChunks(sample, lng);
  const content = ({ text, role, region, linked, strong }:
    typeof chunks[number]) => ({ text, role, region, linked, strong });
  check('content', () => {
    expect(layout.chunks.map((chunk) => ({
      text: chunk.text, role: chunk.role, region: chunk.region,
      linked: chunk.linked, strong: chunk.strong,
    })), 'Print DOM text and occurrences from sample input.')
      .toEqual(chunks.map(content));
    expect(samplePdfContentMatches(display.text, chunks, layout),
      'PDF sample input content, occurrences and column order.').toBe(true);
  });
  const expectedChunks = [];
  for (const [index, chunk] of chunks.entries()) {
    const measured = layout.chunks[index]!;
    const region = target !== 'none' && chunk.region === target;
    const darkHeader = templateId === 'executive-band' && region;
    const isHeading = ['name', 'heading', 'title'].includes(chunk.role!);
    let color = darkHeader ? '#e3e5e8' : colors.text;
    if (chunk.role === 'meta') {
      color = META_COLORS[templateId]![region ? 1 : 0];
    }
    if (isHeading) {
      color = darkHeader ? '#ffffff' : colors.primary;
    }
    if (chunk.linked) {
      color = darkHeader ? '#b6834a' : accent;
    }
    const sizes: Readonly<Record<string, number>> = {
      name: 2, headline: 1.15, heading: 1.1, meta: 0.9,
    };
    const size = base * (sizes[chunk.role!] ?? 1);
    const bold = isHeading || chunk.strong;
    check('font size', () => {
      expect(Math.abs(measured.size - size)).toBeLessThanOrEqual(0.1);
    });
    let weight = bold ? '700' : '400';
    if (chunk.role === 'title' && chunk.strong) {
      weight = '900';
    }
    check('text weight', () => {
      expect(measured.weight, 'Print DOM text weight.').toBe(weight);
    });
    check('text color', () => {
      expect(pdfColorMatches(cssHex(measured.color), color),
        'Print DOM text color.').toBe(true);
    });
    const expected = { color, size: size * 0.75, family, bold };
    expectedChunks.push({ value: chunk.value, expected });
  }
  const paint = pdfVariableTextWeights(display.paint, family,
    expectedChunks.filter(({ expected }) => !expected.bold)
      .map(({ value }) => value));
  for (const { value, expected } of expectedChunks) {
    const matches = pdfTextStyleMatches(paint, value, expected);
    check('pdf typography', () => {
      expect(matches, matches
        ? undefined
        : pdfTextStyleDiagnostic(paint, value, expected)).toBe(true);
    });
  }
  check('typography', () => {
    for (const text of layout.text) {
      if (text.role === 'heading') {
        expect(text.transform).toBe(transform);
        expect(Math.abs(text.spacing - (transform === 'uppercase'
          ? base * 1.1 * 0.06
          : 0))).toBeLessThanOrEqual(0.1);
        expect(Math.abs(text.lineHeight - base * 1.1 * 1.2))
          .toBeLessThanOrEqual(0.1);
      }
      if (text.role === 'body') {
        expect(text.align).toBe(font.textAlign ?? 'left');
      }
    }
  });
  const inset = spacing.pageMargin ?? { x: 15, y: 15 };
  const margins = { x: inset.x * POINTS_PER_MM, y: inset.y * POINTS_PER_MM };
  const boxes = display.text.items.flatMap(({ bounds }) => bounds
    ? [bounds]
    : []);
  const overflow = {
    left: Math.max(0, ...boxes.map((box) => margins.x - box.left)),
    right: Math.max(0, ...boxes.map((box) => box.right
      - display.text.width + margins.x)),
    top: Math.max(0, ...boxes.map((box) => margins.y - box.top)),
    bottom: Math.max(0, ...boxes.map((box) => box.bottom
      - display.text.height + margins.y)),
  };
  check('margin', () => {
    expect(pdfGlyphGeometry(display.text, margins),
      `PDF glyph overflow in points: ${JSON.stringify(overflow)}`)
      .toEqual({ contained: true, separated: true });
  });
  check('line spacing', () => {
    const paragraphs = assertion === 'line spacing'
      ? layout.paragraphs.slice(0, 1)
      : layout.paragraphs;
    expect(pdfParagraphGeometry(display.text, paragraphs.map((part) => ({
      ...part, pitch: base * spacing.lineHeight * 0.75,
    }))), 'PDF paragraph line pitch.').toBe(true);
  });
  check('geometry', () => {
    if (sample.customization.layout.columns === 2) {
      expect(layout.sidebar).not.toBeNull();
      expect(Math.abs(layout.sidebar!.right - layout.sidebar!.left
        - layout.width * 0.32)).toBeLessThanOrEqual(1);
      expect(Math.abs(layout.sidebar!.right - display.text.width + margins.x))
        .toBeLessThanOrEqual(1);
    } else {
      expect(layout.sidebar).toBeNull();
    }
  });
  check('photo', () => {
    expect(display.paint.images, 'PDF sample photo count.')
      .toHaveLength(sample.personalDetails.photo ? 1 : 0);
  });
  if (sample.personalDetails.photo) {
    const identity = await fixturePhotoIdentity();
    check('photo', () => {
      expect(pdfImageMatches(display.paint.images[0], identity, layout.photo!),
        pdfImageDiagnostic(display.paint.images[0], identity, layout.photo!))
        .toBe(true);
    });
  }
  const sections = Object.values(sample.content);
  for (const section of sections) {
    if (section.sectionType !== 'skill' && section.sectionType !== 'language') {
      continue;
    }
    const sidebar = sample.customization.layout.sections.sidebar
      .some((key) => sample.content[key] === section);
    const surface = target === 'sidebar' && sidebar
      ? colors.surface!
      : '#ffffff';
    for (const entry of section.entries.filter((entry) => !entry.isHidden)) {
      const widget = layout.widgets.find(({ name }) => name === entry.name);
      check('level bar', () => {
        expect(widget).toBeDefined();
      });
      const mode = sectionDisplay[section.sectionType].style;
      check('level bar', () => {
        expect(pdfWidgetMatches(display, widget!, entry.level,
          mode, {
            solid: accent,
            track: mixedColor(accent, surface, 0.8),
            onAccent: '#ffffff',
            fontSize: base * 0.9,
          }), pdfWidgetDiagnostic(display, widget!)).toBe(true);
      });
      if (entry.level !== undefined && mode !== 'text') {
        const match = (box: PdfBox, color: string, exposed?: PdfBox) => {
          const fill = pdfPaintMatch(display.paint, box, color,
            false, exposed);
          check('level bar', () => {
            expect(fill).toBeDefined();
          });
          foreground.push(fill!);
        };
        if (mode === 'dots') {
          widget!.dots.forEach((dot, index) => match(dot,
            index < entry.level! ? accent : mixedColor(accent, surface, 0.8)));
        } else if (mode === 'bar') {
          const track = widget!.track!;
          const fill = { ...track,
            right: track.left + (track.right - track.left) * entry.level / 5 };
          if (entry.level < 5) {
            match(track, mixedColor(accent, surface, 0.8),
              { ...track, left: fill.right });
          }
          if (entry.level > 0) {
            match(fill, accent);
          }
        } else {
          match(widget!.widget!, accent);
        }
      }
    }
  }
  if (target === 'sidebar' || target === 'header') {
    const box = target === 'sidebar' ? layout.sidebar : layout.header;
    check('sidebar tint', () => {
      expect(box).not.toBeNull();
      expect(pdfSurfacePaint(display.paint, display.text, box!, colors.surface!,
        foreground, layout.icons), 'PDF sample surface paint.').toBe(true);
    });
  }
}

/**
 * How far down a PDF page its text reaches: the lowest glyph bottom below
 * the top margin, as a share of the usable height between the margins.
 * Glyph bottoms use each font's descent, so a descender counts.
 */
export async function textFill(
  pdfPage: PDFPageProxy,
  marginYmm: number,
): Promise<number> {
  const [, bottom, , top] = pdfPage.view;
  const pageHeight = top! - bottom!;
  const margin = marginYmm * POINTS_PER_MM;
  const content = await pdfPage.getTextContent();
  let lowest = 0;
  for (const item of content.items) {
    if (!('str' in item) || item.str.trim() === '') {
      continue;
    }
    const reported = content.styles[item.fontName]?.descent;
    const descent = Number.isFinite(reported) ? reported! : FALLBACK_DESCENT;
    const size = Number.isFinite(item.height) && item.height > 0
      ? item.height
      : Math.abs(item.transform[3]!);
    const glyphBottom = item.transform[5]! + (descent * size);
    lowest = Math.max(lowest, top! - glyphBottom);
  }
  return (lowest - margin) / (pageHeight - (2 * margin));
}

export function assertPdfPageGeometry(
  pages: readonly PdfTextPage[], count: number, paper: 'a4' | 'letter',
): void {
  expect(pages, 'PDF page count.').toHaveLength(count);
  const [width, height] = paper === 'a4' ? [595.28, 841.89] : [612, 792];
  for (const page of pages) {
    expect(Math.abs(page.width - width!), 'PDF page size.')
      .toBeLessThanOrEqual(1);
    expect(Math.abs(page.height - height!), 'PDF page size.')
      .toBeLessThanOrEqual(1);
    expect(page.items.length, 'PDF nonempty page.').toBeGreaterThan(0);
  }
}

export function assertSampleFill(fill: number): void {
  expect(fill, 'PDF sample minimum fill.').toBeGreaterThanOrEqual(0.90);
  expect(fill, 'PDF sample maximum fill.').toBeLessThanOrEqual(0.98);
}

export async function assertOverflowPdf(
  pages: readonly PdfDisplayPage[],
  layout: Awaited<ReturnType<typeof measurePdfLayout>>,
  printCase: PrintCase, lng: 'en' | 'vi', paper: 'a4' | 'letter',
  scheme: 'light' | 'dark',
): Promise<void> {
  const text = pages.map((page) => page.text);
  const chunks = await overflowPdfChunks(printCase, lng);
  assertDomInputContent(layout, chunks);
  assertPdfInputOccurrences(text, chunks, layout);
  expect(samplePdfContentMatches(text, chunks, layout),
    'PDF overflow input content and column order.').toBe(true);
  assertPdfPageGeometry(text, 2, paper);
  checkMembership(pdfMembership(text, printCase),
    `${printCase.fixture}-${lng}-${paper}-${scheme}`);
  const column = printCase.prefix === 'MAIN-ENTRY-' ? 'MAIN' : 'SIDEBAR';
  expect(pdfEntryIds(text, column), 'PDF entry occurrences and order.')
    .toEqual(expectedEntryIds(printCase));
  const body = column === 'MAIN'
    ? lng === 'vi'
      ? 'Nội dung kiểm tra phân trang bản in'
      : 'Print pagination test content'
    : lng === 'vi'
      ? 'Nội dung kiểm tra phân trang'
      : 'Pagination test content';
  const allText = text.map((page) => page.text).join(' ');
  for (let number = 1; number <= printCase.total; number += 1) {
    expect(allText.split(`${body} ${number}.`), 'PDF numbered body text.')
      .toHaveLength(2);
  }
  const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
  const headings = layout.text.filter((item) => item.role === 'heading'
    && item.left >= layout.sidebar!.left - 1);
  const photo = await fixturePhotoIdentity();
  for (const [index, page] of pages.entries()) {
    expect(pdfGlyphGeometry(page.text, margins), 'PDF glyph bounds.')
      .toEqual({ contained: true, separated: true });
    expect(pdfColumnsMatch(page.text, layout, margins, index === 0),
      'PDF column positions.').toBe(true);
    expect(pdfSidebarPaint(page.paint, page.text, layout.sidebar!, margins.y,
      index === 0, '#e3ecf0', {
        body: column === 'MAIN' ? 'SIDEBAR-SHORT' : body,
        headings: index === 0 ? headings : [],
      }), 'PDF sidebar paint and level bars.').toBe(true);
    expect(page.paint.images, 'PDF photo count.')
      .toHaveLength(index === 0 ? 1 : 0);
    if (index === 0) {
      expect(pdfImageMatches(page.paint.images[0], photo, layout.photo!),
        pdfImageDiagnostic(page.paint.images[0], photo, layout.photo!))
        .toBe(true);
    }
  }
}
