import { createCanvas, loadImage } from '@napi-rs/canvas';
import { expect, test, type TestInfo } from '@playwright/test';
import {
  getDocument,
  type PDFPageProxy,
} from 'pdfjs-dist/legacy/build/pdf.mjs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { SAMPLES } from '@aboutme/schema/samples';

import { SAMPLE_PAGES } from '../app/templates/samplePages';
import { denyExternalRequests } from './support';

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
    await expect(
      page.locator('[data-render-mode="continuous"]'),
    ).toHaveCount(1);
    await expect(page.locator('[data-pagination-settled]')).toHaveCount(0);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    expect(external).toEqual([]);

    const marginYmm = await page.locator('.resume-document').evaluate(
      (article) => Number.parseFloat(
        getComputedStyle(article).getPropertyValue('--page-margin-y'),
      ),
    );
    await page.emulateMedia({ media: 'print' });

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
      const firstFill = await textFill(await document.getPage(1), marginYmm);
      const lastFill = await textFill(
        await document.getPage(document.numPages),
        marginYmm,
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
