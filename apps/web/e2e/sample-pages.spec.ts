import { createCanvas, loadImage } from '@napi-rs/canvas';
import { expect, test, type TestInfo } from '@playwright/test';
import {
  getDocument,
  type PDFPageProxy,
} from 'pdfjs-dist/legacy/build/pdf.mjs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { SAMPLES } from '@aboutme/schema/samples';
import type { Resume } from '@aboutme/schema';

import { SAMPLE_PAGES } from '../app/templates/samplePages';
import { measurePdfLayout } from './display-assertions';
import {
  pdfGlyphGeometry,
  pdfParagraphGeometry,
  pdfTextPage,
  pdfWidgetMatches,
  pdfWidgetDiagnostic,
  type PdfDisplayPage,
} from './pdf-display';
import {
  fixturePhotoIdentity,
  pdfImageMatches,
  pdfImageDiagnostic,
  pdfColorMatches,
  pdfHasFill,
  pdfHasRule,
  pdfPaintPage,
  pdfTextStyleMatches,
} from './pdf-paint';
import { denyExternalRequests, waitForImages } from './support';

// Every gallery sample's PDF pages, rasterized through the same print path
// that produces the real PDF: the harness in continuous mode, print media,
// and page.pdf() with the production print flags (docs/design/templates/
// print.md). The template gallery shows these images on the cards of the
// templates with a sample (DESIGN.md, Library); this spec only produces and
// checks them. Compare mode fails on the first stale file; update mode
// writes review-only candidates plus the manifest that lists them, for the
// manager to commit exactly like a pixel baseline.
//
// A sample is a model resume, so it prints on one page and fills it: its
// text must reach between MIN_FILL and MAX_FILL of the usable page height.
// A short sample makes its template look empty; a sample at the very bottom
// leaves no room for the reader's own longer entries.

const PAGES_DIR = resolve(import.meta.dirname, '../public/templates/pages');
const CANDIDATE_SUBDIR = 'template-pages';
// About 1240px wide for an A4 page (docs/design/templates/print.md §2):
// large enough for a library card and the template page, small enough to
// ship ten-plus documents.
const RASTER_DPI = 150;
const POINTS_PER_MM = 72 / 25.4;
const MIN_FILL = 0.9;
const MAX_FILL = 0.98;
// pdf.js reports no descent for Type 3 fonts; a quarter em is close to the
// descent of every catalog face.
const FALLBACK_DESCENT = -0.25;

const FAMILIES: Readonly<Record<string, readonly [string, string]>> = {
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

async function assertSampleStyle(
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
  expect(Math.abs(layout.size - base)).toBeLessThanOrEqual(0.1);
  expect(Math.abs(layout.lineHeight - base * spacing.lineHeight))
    .toBeLessThanOrEqual(0.1);
  expect(layout.background).toBe('rgb(255, 255, 255)');
  const target = sample.customization.layout.surfaceTarget ?? 'none';
  const accent = colors.accent ?? colors.primary;
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
    expect(decoration.rule).toBe(heading.showRule ? 1 : 0);
    expect(Math.abs(decoration.gap - spacing.sectionGap * 0.4))
      .toBeLessThanOrEqual(1);
    if (heading.showRule) {
      const color = mixedColor(accent, surface, 0.6);
      expect(decoration.ruleStyle).toBe('solid');
      expect(pdfColorMatches(cssHex(decoration.color), color)).toBe(true);
      expect(pdfHasRule(display.paint, decoration.ruleBox, color)).toBe(true);
    }
  }
  let transform = 'none';
  if (heading.style === 'uppercase') transform = 'uppercase';
  if (heading.style === 'titlecase' && lng === 'en') transform = 'capitalize';
  for (const chunk of layout.chunks) {
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
    expect(Math.abs(chunk.size - size)).toBeLessThanOrEqual(0.1);
    let weight = bold ? '700' : '400';
    if (chunk.role === 'title' && chunk.strong) weight = '900';
    expect(chunk.weight).toBe(weight);
    expect(pdfColorMatches(cssHex(chunk.color), color)).toBe(true);
    let value = chunk.text;
    if (chunk.role === 'heading' && transform === 'uppercase') {
      value = value.toLocaleUpperCase(lng);
    }
    if (chunk.role === 'heading' && transform === 'capitalize') {
      value = value.replace(/(^|\s)\p{L}/gu, (letter) => letter.toUpperCase());
    }
    expect(pdfTextStyleMatches(display.paint, value, {
      color, size: size * 0.75, family, bold,
    })).toBe(true);
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
  })))).toBe(true);
  if (sample.customization.layout.columns === 2) {
    expect(layout.sidebar).not.toBeNull();
    expect(Math.abs(layout.sidebar!.right - layout.sidebar!.left
      - layout.width * 0.32)).toBeLessThanOrEqual(1);
    expect(Math.abs(layout.sidebar!.right - display.text.width + margins.x))
      .toBeLessThanOrEqual(1);
  } else expect(layout.sidebar).toBeNull();
  if (target === 'sidebar' || target === 'header') {
    const box = target === 'sidebar' ? layout.sidebar : layout.header;
    expect(box).not.toBeNull();
    expect(pdfHasFill(display.paint, box!, colors.surface!)).toBe(true);
  }
  expect(display.paint.images)
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
      expect(pdfWidgetMatches(display, widget!, entry.level,
        sectionDisplay[section.sectionType].style, {
          solid: accent,
          track: mixedColor(accent, surface, 0.8),
          onAccent: '#ffffff',
          fontSize: base * 0.9,
        }), pdfWidgetDiagnostic(display, widget!)).toBe(true);
    }
  }
}

/**
 * How far down a PDF page its text reaches: the lowest glyph bottom below
 * the top margin, as a share of the usable height between the margins.
 * Glyph bottoms use each font's descent, so a descender counts.
 */
async function textFill(
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

interface ManifestEntry {
  readonly templateId: string;
  readonly lng: 'en' | 'vi';
  readonly pages: number;
  readonly width: number;
  readonly height: number;
  readonly files: readonly string[];
}

interface SampleFill {
  readonly sample: string;
  readonly pages: number;
  readonly pinnedPages: number;
  readonly fill: number;
}

// Populated in file order under the serial mode below, then written or
// checked once by the final tests. Page counts and fills are checked there,
// all at once, so one run reports every sample that misses and an update
// run still writes every candidate.
const manifestEntries: ManifestEntry[] = [];
const fills: SampleFill[] = [];

function candidateRoot(): string {
  const root = process.env.PLAYWRIGHT_RESULTS_DIR;
  if (root === undefined) {
    throw new Error('PLAYWRIGHT_RESULTS_DIR is required.');
  }
  return resolve(root, 'candidate-baselines', CANDIDATE_SUBDIR);
}

async function compareRaster(
  actual: Buffer,
  expected: Buffer,
  diffPath: string,
): Promise<void> {
  const [actualImage, expectedImage] = await Promise.all([
    loadImage(actual),
    loadImage(expected),
  ]);
  expect(actualImage.width).toBe(expectedImage.width);
  expect(actualImage.height).toBe(expectedImage.height);
  const width = actualImage.width;
  const height = actualImage.height;
  const actualCanvas = createCanvas(width, height);
  const expectedCanvas = createCanvas(width, height);
  actualCanvas.getContext('2d').drawImage(actualImage, 0, 0);
  expectedCanvas.getContext('2d').drawImage(expectedImage, 0, 0);
  const actualPixels = actualCanvas
    .getContext('2d')
    .getImageData(0, 0, width, height).data;
  const expectedPixels = expectedCanvas
    .getContext('2d')
    .getImageData(0, 0, width, height).data;
  let changed = 0;
  const diffCanvas = createCanvas(width, height);
  const diffContext = diffCanvas.getContext('2d');
  const diff = diffContext.createImageData(width, height);
  for (let offset = 0; offset < actualPixels.length; offset += 4) {
    const differs = actualPixels[offset] !== expectedPixels[offset]
      || actualPixels[offset + 1] !== expectedPixels[offset + 1]
      || actualPixels[offset + 2] !== expectedPixels[offset + 2]
      || actualPixels[offset + 3] !== expectedPixels[offset + 3];
    if (differs) {
      changed += 1;
      diff.data.set([255, 0, 0, 255], offset);
    } else {
      diff.data.set([255, 255, 255, 255], offset);
    }
  }
  if (changed > 0) {
    diffContext.putImageData(diff, 0, 0);
    await writeFile(diffPath, diffCanvas.toBuffer('image/png'));
  }
  expect(changed).toBe(0);
}

async function verifyPageImage(
  bytes: Buffer,
  filename: string,
  testInfo: TestInfo,
): Promise<void> {
  await writeFile(testInfo.outputPath(filename), bytes);
  if (testInfo.config.updateSnapshots !== 'none') {
    const candidate = resolve(candidateRoot(), filename);
    await mkdir(resolve(candidate, '..'), { recursive: true });
    await writeFile(candidate, bytes);
    return;
  }
  const expected = await readFile(resolve(PAGES_DIR, filename));
  await compareRaster(
    bytes,
    expected,
    testInfo.outputPath(filename.replace(/\.png$/, '-diff.png')),
  );
}

test.describe.configure({ mode: 'serial' });

for (const { templateId, lng } of SAMPLES) {
  test(`${templateId} (${lng}) rasterizes its PDF pages`, async (
    { page },
    testInfo,
  ) => {
    const external = await denyExternalRequests(page);
    const response = await page.goto(
      `/_harness/render?fixture=sample-${templateId}-${lng}`
      + `&template=${templateId}&mode=continuous&print=1`,
    );
    expect(response?.ok()).toBe(true);
    const samplePath = resolve(import.meta.dirname,
      '../../../packages/schema/samples', `${templateId}.${lng}.json`);
    const sample = JSON.parse(await readFile(samplePath, 'utf8')) as Resume;
    const fullName = sample.personalDetails.fullName;
    expect(fullName).toBeTruthy();
    await expect(
      page.locator('[data-render-mode="continuous"]'),
    ).toHaveCount(1);
    await expect(page.locator('[data-pagination-settled]')).toHaveCount(0);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    expect(external).toEqual([]);

    const margins = sample.customization.spacing.pageMargin ?? { x: 15, y: 15 };
    await page.emulateMedia({ media: 'print', colorScheme: 'dark' });
    await page.setViewportSize({
      width: Math.round((210 - 2 * margins.x) * 96 / 25.4), height: 1123,
    });
    const layout = await measurePdfLayout(page, {
      x: margins.x * POINTS_PER_MM, y: margins.y * POINTS_PER_MM,
    });
    const [family] = FAMILIES[sample.customization.font.family]!;
    expect(await page.evaluate((expected) => [400, 700].every((weight) =>
      Array.from(document.fonts).some((face) => face.status === 'loaded'
        && face.family.replaceAll('"', '') === expected
        && face.weight === String(weight))), family)).toBe(true);
    const sections = sample.customization.layout.sections;
    const visible = [...sections.main, ...sections.sidebar]
      .map((key) => sample.content[key]!)
      .filter((section) => section.entries.some((entry) => !entry.isHidden));
    await expect(page.locator('.resume-section')).toHaveCount(visible.length);
    await expect(page.locator('.section-heading h2')).toHaveText(
      visible.map((section) => section.displayName ?? ''),
    );
    await expect(page.locator('.entry')).toHaveCount(visible.reduce(
      (total, section) => total + section.entries
        .filter((entry) => !entry.isHidden).length, 0,
    ));

    const pdf = await page.pdf({
      displayHeaderFooter: false,
      margin: { bottom: 0, left: 0, right: 0, top: 0 },
      preferCSSPageSize: true,
      printBackground: true,
      scale: 1,
    });
    await writeFile(
      testInfo.outputPath(`${templateId}-${lng}.pdf`),
      pdf,
    );

    const loadingTask = getDocument({
      data: new Uint8Array(pdf),
      isImageDecoderSupported: false,
      isOffscreenCanvasSupported: false,
      useSystemFonts: false,
    });
    try {
      const document = await loadingTask.promise;
      const firstFill = await textFill(await document.getPage(1), margins.y);
      const lastFill = await textFill(
        await document.getPage(document.numPages),
        margins.y,
      );
      console.log(
        `sample fill ${templateId} ${lng}: pages=${document.numPages}`
        + ` first=${firstFill.toFixed(3)} last=${lastFill.toFixed(3)}`,
      );
      const files: string[] = [];
      let width = 0;
      let height = 0;
      for (
        let pageNumber = 1;
        pageNumber <= document.numPages;
        pageNumber += 1
      ) {
        const pdfPage = await document.getPage(pageNumber);
        const textPage = await pdfTextPage(pdfPage);
        await assertSampleStyle(sample, templateId, lng, layout, {
          text: textPage, paint: await pdfPaintPage(pdfPage),
        });
        const viewport = pdfPage.getViewport({ scale: RASTER_DPI / 72 });
        width = Math.ceil(viewport.width);
        height = Math.ceil(viewport.height);
        const canvas = createCanvas(width, height);
        await pdfPage.render({
          canvas: canvas as unknown as HTMLCanvasElement,
          canvasContext: (
            canvas.getContext('2d') as unknown as CanvasRenderingContext2D
          ),
          intent: 'print',
          viewport,
        }).promise;
        const filename = `${templateId}-${lng}-p${pageNumber}.png`;
        await verifyPageImage(canvas.toBuffer('image/png'), filename, testInfo);
        expect(Math.abs(textPage.width - 595.28)).toBeLessThanOrEqual(1);
        expect(Math.abs(textPage.height - 841.89)).toBeLessThanOrEqual(1);
        expect(textPage.items.length).toBeGreaterThan(0);
        expect(textPage.text.toLocaleLowerCase('en-US'))
          .toContain(fullName!.normalize('NFC').toLocaleLowerCase('en-US'));
        for (const item of textPage.items) {
          const marginX = margins.x * POINTS_PER_MM;
          const marginY = margins.y * POINTS_PER_MM;
          expect(item.x).toBeGreaterThanOrEqual(marginX - 1);
          expect(item.x + item.width)
            .toBeLessThanOrEqual(textPage.width - marginX + 1);
          expect(item.y).toBeGreaterThanOrEqual(marginY - 1);
          expect(item.y).toBeLessThanOrEqual(textPage.height - marginY + 1);
        }
        files.push(filename);
      }
      manifestEntries.push({
        templateId,
        lng,
        pages: document.numPages,
        width,
        height,
        files,
      });
      fills.push({
        sample: `${templateId} (${lng})`,
        pages: document.numPages,
        pinnedPages: SAMPLE_PAGES[templateId]!,
        fill: firstFill,
      });
    } finally {
      await loadingTask.destroy();
    }
  });
}

test('every gallery sample prints one full page', () => {
  expect(fills).toHaveLength(SAMPLES.length);
  const misses = fills
    .filter(({ pages, pinnedPages, fill }) => pages !== 1
      || pinnedPages !== 1
      || fill < MIN_FILL
      || fill > MAX_FILL)
    .map(({ sample, pages, pinnedPages, fill }) => `${sample}: ${pages}`
      + ` page(s), pinned ${pinnedPages}, fill ${fill.toFixed(3)}`);
  expect(misses).toEqual([]);
});

test('gallery sample page manifest stays current', async () => {
  const testInfo = test.info();
  expect(manifestEntries).toHaveLength(SAMPLES.length);
  const ordered = SAMPLES.map(({ templateId, lng }) => manifestEntries.find(
    (entry) => entry.templateId === templateId && entry.lng === lng,
  )!);
  const json = `${JSON.stringify(ordered, null, 2)}\n`;
  if (testInfo.config.updateSnapshots !== 'none') {
    const candidate = resolve(candidateRoot(), 'manifest.json');
    await mkdir(resolve(candidate, '..'), { recursive: true });
    await writeFile(candidate, json);
    return;
  }
  const expected = await readFile(resolve(PAGES_DIR, 'manifest.json'), 'utf8');
  expect(json).toBe(expected);
});
