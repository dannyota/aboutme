import { expect } from '@playwright/test';
import type { PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';
import type { DateRange, PersonalDetail, Resume, Section, YearMonth }
  from '@aboutme/schema';

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

type Region = 'header' | 'main' | 'sidebar';
export interface SamplePdfChunk {
  readonly text: string;
  readonly value: string;
  readonly role: string;
  readonly region: Region;
  readonly linked: boolean;
  readonly strong: boolean;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const LABELS = {
  en: { email: 'Email', phone: 'Phone', location: 'Location',
    website: 'Website', linkedin: 'LinkedIn', github: 'GitHub',
    twitter: 'X', custom: 'Detail' },
  vi: { email: 'Email', phone: 'Điện thoại', location: 'Địa chỉ',
    website: 'Website', linkedin: 'LinkedIn', github: 'GitHub',
    twitter: 'X', custom: 'Thông tin' },
};
const compact = (value: string) => value.normalize('NFC').replace(/\s+/gu, '');

// Expectations use sample fields and the template contract, never render
// output.
export function samplePdfChunks(
  sample: Resume, lng: 'en' | 'vi',
): SamplePdfChunk[] {
  const chunks: SamplePdfChunk[] = [];
  let region: Region = 'header';
  const add = (text: string | undefined, role: string,
    linked = false, strong = false) => {
    if (!text?.trim()) return;
    text = text.normalize('NFC').trim();
    let value = text;
    if (role === 'heading') {
      if (sample.customization.heading.style === 'uppercase') {
        value = text.toLocaleUpperCase(lng);
      } else if (lng === 'en'
        && sample.customization.heading.style === 'titlecase') {
        value = text.replace(/(^|\s)\p{L}/gu, (letter) => letter.toUpperCase());
      }
    }
    chunks.push({ text, value, role, region, linked, strong });
  };
  const date = (value: YearMonth): string => {
    const format = sample.customization.dateFormat;
    if (value.m === undefined || format === 'YYYY') return String(value.y);
    return format === 'MM/YYYY'
      ? `${String(value.m).padStart(2, '0')}/${value.y}`
      : `${lng === 'vi' ? `thg ${value.m}` : MONTHS[value.m - 1]} ${value.y}`;
  };
  const range = (value: DateRange): string => {
    const start = date(value.start);
    const end = value.present
      ? (lng === 'vi' ? 'Hiện tại' : 'Present')
      : value.end ? date(value.end) : '';
    return end === '' || start === end ? start : `${start} – ${end}`;
  };
  const html = (value: string | undefined) => {
    const stack: string[] = [];
    for (const token of value?.match(/<[^>]*>|[^<]+/gu) ?? []) {
      if (token.startsWith('<')) {
        const tag = /^<(\/?)([a-z]+)(?:\s[^>]*)?>$/iu.exec(token);
        if (!tag || !['p', 'ul', 'ol', 'li', 'strong', 'b', 'a', 'em', 'i',
          'u', 's', 'br'].includes(tag[2]!.toLowerCase())) {
          throw new Error('Unsupported sample rich text.');
        }
        const name = tag[2]!.toLowerCase();
        if (name === 'br') continue;
        if (tag[1] === '/') {
          if (stack.pop() !== name) {
            throw new Error('Invalid sample rich text.');
          }
        } else stack.push(name);
      } else {
        const text = token.replace(/&(#x[\da-f]+|#\d+|amp|lt|gt|quot|apos);/giu,
          (_, entity: string) => {
            if (entity.startsWith('#')) {
              return String.fromCodePoint(
                entity[1]?.toLowerCase() === 'x'
                  ? parseInt(entity.slice(2), 16)
                  : Number(entity.slice(1)));
            }
            const named: Record<string, string> = {
              amp: '&', lt: '<', gt: '>', quot: '"', apos: '\'',
            };
            return named[entity.toLowerCase()]!;
          });
        add(text, 'body', stack.includes('a'),
          stack.includes('b') || stack.includes('strong'));
      }
    }
    if (stack.length > 0) throw new Error('Invalid sample rich text.');
  };
  add(sample.personalDetails.fullName, 'name');
  add(sample.personalDetails.headline, 'headline');
  for (const detail of sample.personalDetails.details ?? []) {
    if (detail.isHidden) continue;
    const label = detail.label || LABELS[lng][detail.type];
    const web = detail.type !== 'email' && detail.type !== 'phone'
      && detail.type !== 'location' && detail.value.startsWith('https://');
    const display = web ? detail.display ?? 'short' : 'short';
    const iconless = sample.customization.header?.iconStyle === 'none';
    if (display !== 'label'
      && (iconless || detail.type === 'custom' || detail.label)) {
      add(`${label}:`, 'body');
    }
    const shown = display === 'label'
      ? label
      : web && display === 'short'
        ? detail.value.slice(8).replace(/\/$/u, '') || detail.value
        : detail.value;
    add(shown, 'body', web || contactLinked(detail));
  }
  for (const column of ['main', 'sidebar'] as const) {
    region = column === 'sidebar' && sample.customization.layout.columns === 1
      ? 'main'
      : column;
    for (const key of sample.customization.layout.sections[column]) {
      const section = sample.content[key];
      if (!section?.entries.some((entry) => !entry.isHidden)) continue;
      add(section.displayName, 'heading');
      for (const entry of section.entries) {
        if (entry.isHidden) continue;
        const fields = entry as Section['entries'][number] & {
          jobTitle?: string; degree?: string; name?: string; title?: string;
          employer?: string; school?: string; subtitle?: string;
          issuer?: string;
          employerLink?: string; schoolLink?: string; titleLink?: string;
          link?: string; dates?: DateRange; date?: YearMonth;
          city?: string; country?: string; text?: string;
          description?: string; infoHtml?: string;
        };
        const title = fields.jobTitle ?? fields.degree ?? fields.name
          ?? fields.title;
        const titleLink = fields.titleLink ?? fields.link;
        add(title, 'title', Boolean(titleLink), !titleLink);
        const subtitle = fields.employer ?? fields.school ?? fields.subtitle
          ?? fields.issuer;
        add(subtitle, 'subtitle',
          Boolean(fields.employerLink || fields.schoolLink));
        const meta = [
          fields.dates ? range(fields.dates) : undefined,
          fields.date ? date(fields.date) : undefined,
          fields.city, fields.country,
        ].filter((text): text is string => Boolean(text));
        meta.forEach((text, index) => {
          if (index > 0) add('·', 'meta');
          add(text, 'meta');
        });
        html(fields.text ?? fields.description ?? fields.infoHtml);
      }
    }
  }
  return chunks;
}

function contactLinked(detail: PersonalDetail): boolean {
  if (detail.type === 'phone') {
    return /^\+?\d{3,20}$/u.test(detail.value.replace(/[ .()-]/gu, ''));
  }
  return detail.type === 'email' && detail.value.length <= 254
    && /^[^\s@<>"'`()\\,;:?#%]+@[^\s@.]+(?:\.[^\s@.]+)+$/u.test(detail.value);
}

export function samplePdfContentMatches(
  page: PdfTextPage,
  chunks: readonly SamplePdfChunk[],
  layout: { sidebar: PdfBox | null; header: PdfBox | null },
): boolean {
  if (!layout.header || page.items.some((item) => !item.bounds)) return false;
  const regions = { header: '', main: '', sidebar: '' };
  const ordered = [...page.items].sort((a, b) => b.y - a.y || a.x - b.x);
  for (const item of ordered) {
    const box = item.bounds!;
    const region = box.top < layout.header.bottom - 1
      ? 'header'
      : layout.sidebar && box.left >= layout.sidebar.left - 1
        ? 'sidebar'
        : 'main';
    regions[region] += compact(item.text);
  }
  return (['header', 'main', 'sidebar'] as const).every((region) => {
    const values = chunks.filter((chunk) => chunk.region === region)
      .map((chunk) => compact(chunk.value));
    const expected = values.join('');
    const actual = regions[region];
    const count = (text: string, value: string) => text.split(value).length - 1;
    const same = (value: string) =>
      count(actual, value) === count(expected, value);
    if (!values.every(same)) {
      return false;
    }
    let cursor = 0;
    return values.every((value) => {
      const index = actual.indexOf(value, cursor);
      if (index === -1) return false;
      cursor = index + value.length;
      return true;
    });
  });
}

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
): Promise<void> {
  const { font, spacing, colors, heading, sectionDisplay }
    = sample.customization;
  const base = font.baseSizePx;
  const [family, fallback] = FAMILIES[font.family]!;
  expect(layout.family.replaceAll('"', '')).toBe(`${family}, ${fallback}`);
  expect(layout.synthesis).toBe('none');
  expect(Math.abs(layout.size - base), 'Print DOM font size.')
    .toBeLessThanOrEqual(0.1);
  expect(Math.abs(layout.lineHeight - base * spacing.lineHeight))
    .toBeLessThanOrEqual(0.1);
  expect(layout.background).toBe('rgb(255, 255, 255)');
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
    expect(pdfColorMatches(icon.color, color), 'Print icon color.').toBe(true);
  }
  expect(layout.headerAlign).toBe(sample.customization.header?.align ?? 'left');
  for (const gap of layout.entryGaps) {
    expect(Math.abs(gap - spacing.entryGap)).toBeLessThanOrEqual(1);
  }
  for (const gap of layout.sectionGaps) {
    expect(Math.abs(gap - spacing.sectionGap)).toBeLessThanOrEqual(1);
  }
  for (const decoration of layout.headings) {
    const surface = target === 'sidebar' && decoration.sidebar
      ? colors.surface!
      : '#ffffff';
    expect(decoration.rule, 'Print heading rule.')
      .toBe(heading.showRule ? 1 : 0);
    expect(Math.abs(decoration.gap - spacing.sectionGap * 0.4))
      .toBeLessThanOrEqual(1);
    if (heading.showRule) {
      const color = mixedColor(accent, surface, 0.6);
      expect(decoration.ruleStyle).toBe('solid');
      expect(pdfColorMatches(cssHex(decoration.color), color)).toBe(true);
      const fill = pdfPaintMatch(display.paint, decoration.ruleBox,
        color, true);
      expect(fill).toBeDefined();
      foreground.push(fill!);
    }
  }
  let transform = 'none';
  if (heading.style === 'uppercase') transform = 'uppercase';
  if (heading.style === 'titlecase' && lng === 'en') transform = 'capitalize';
  const chunks = samplePdfChunks(sample, lng);
  const content = ({ text, role, region, linked, strong }:
    typeof chunks[number]) => ({ text, role, region, linked, strong });
  expect(layout.chunks.map((chunk) => ({
    text: chunk.text, role: chunk.role, region: chunk.region,
    linked: chunk.linked, strong: chunk.strong,
  })), 'Print DOM text and occurrences from sample input.')
    .toEqual(chunks.map(content));
  expect(samplePdfContentMatches(display.text, chunks, layout),
    'PDF sample input content, occurrences and column order.').toBe(true);
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
    if (isHeading) color = darkHeader ? '#ffffff' : colors.primary;
    if (chunk.linked) color = darkHeader ? '#b6834a' : accent;
    const sizes: Readonly<Record<string, number>> = {
      name: 2, headline: 1.15, heading: 1.1, meta: 0.9,
    };
    const size = base * (sizes[chunk.role!] ?? 1);
    const bold = isHeading || chunk.strong;
    expect(Math.abs(measured.size - size)).toBeLessThanOrEqual(0.1);
    let weight = bold ? '700' : '400';
    if (chunk.role === 'title' && chunk.strong) weight = '900';
    expect(measured.weight, 'Print DOM text weight.').toBe(weight);
    expect(pdfColorMatches(cssHex(measured.color), color),
      'Print DOM text color.').toBe(true);
    const expected = { color, size: size * 0.75, family, bold };
    expectedChunks.push({ value: chunk.value, expected });
  }
  const paint = pdfVariableTextWeights(display.paint, family,
    expectedChunks.filter(({ expected }) => !expected.bold)
      .map(({ value }) => value));
  for (const { value, expected } of expectedChunks) {
    const matches = pdfTextStyleMatches(paint, value, expected);
    expect(matches, matches
      ? undefined
      : pdfTextStyleDiagnostic(paint, value, expected)).toBe(true);
  }
  for (const text of layout.text) {
    if (text.role === 'heading') {
      expect(text.transform).toBe(transform);
      expect(Math.abs(text.spacing - (transform === 'uppercase'
        ? base * 1.1 * 0.06
        : 0))).toBeLessThanOrEqual(0.1);
      expect(Math.abs(text.lineHeight - base * 1.1 * 1.2))
        .toBeLessThanOrEqual(0.1);
    }
    if (text.role === 'body') expect(text.align).toBe(font.textAlign ?? 'left');
  }
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
  expect(pdfGlyphGeometry(display.text, margins),
    `PDF glyph overflow in points: ${JSON.stringify(overflow)}`)
    .toEqual({ contained: true, separated: true });
  expect(pdfParagraphGeometry(display.text, layout.paragraphs.map((part) => ({
    ...part, pitch: base * spacing.lineHeight * 0.75,
  }))), 'PDF paragraph line pitch.').toBe(true);
  if (sample.customization.layout.columns === 2) {
    expect(layout.sidebar).not.toBeNull();
    expect(Math.abs(layout.sidebar!.right - layout.sidebar!.left
      - layout.width * 0.32)).toBeLessThanOrEqual(1);
    expect(Math.abs(layout.sidebar!.right - display.text.width + margins.x))
      .toBeLessThanOrEqual(1);
  } else expect(layout.sidebar).toBeNull();
  expect(display.paint.images, 'PDF sample photo count.')
    .toHaveLength(sample.personalDetails.photo ? 1 : 0);
  if (sample.personalDetails.photo) {
    const identity = await fixturePhotoIdentity();
    expect(pdfImageMatches(display.paint.images[0], identity, layout.photo!),
      pdfImageDiagnostic(display.paint.images[0], identity, layout.photo!))
      .toBe(true);
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
      expect(widget).toBeDefined();
      const mode = sectionDisplay[section.sectionType].style;
      expect(pdfWidgetMatches(display, widget!, entry.level,
        mode, {
          solid: accent,
          track: mixedColor(accent, surface, 0.8),
          onAccent: '#ffffff',
          fontSize: base * 0.9,
        }), pdfWidgetDiagnostic(display, widget!)).toBe(true);
      if (entry.level !== undefined && mode !== 'text') {
        const match = (box: PdfBox, color: string, exposed?: PdfBox) => {
          const fill = pdfPaintMatch(display.paint, box, color,
            false, exposed);
          expect(fill).toBeDefined();
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
          if (entry.level > 0) match(fill, accent);
        } else match(widget!.widget!, accent);
      }
    }
  }
  if (target === 'sidebar' || target === 'header') {
    const box = target === 'sidebar' ? layout.sidebar : layout.header;
    expect(box).not.toBeNull();
    expect(pdfSurfacePaint(display.paint, display.text, box!, colors.surface!,
      foreground, layout.icons), 'PDF sample surface paint.').toBe(true);
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
    if (!('str' in item) || item.str.trim() === '') continue;
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
