import { createCanvas, loadImage } from '@napi-rs/canvas';
import { expect, test, type TestInfo } from '@playwright/test';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { SAMPLES } from '@aboutme/schema/samples';
import type { Resume } from '@aboutme/schema';

import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';
import { SAMPLE_PAGES } from '../app/templates/samplePages';
import { measurePdfLayout } from './display-assertions';
import { applyStyleMutation } from './style-mutation';
import { assertPrintDom, assertPhotoCrop, assertPrintRules } from './pdf-fonts';
import {
  pdfParagraphGeometry, pdfTextPage, printedDisplayPages, withPrintedPdf,
  type PdfDisplayPage,
} from './pdf-display';
import { pdfPaintPage, pdfTextStyleMatches, pdfTextStyleDiagnostic }
  from './pdf-paint';
import { pdfVariableTextWeights } from './pdf-text-style';
import { measurePrintTextContainment } from './print-text-containment';
import { samplePdfParagraphs } from './pdf-input-content';
import {
  assertPdfPageGeometry, assertSampleFill, assertSampleStyle, FAMILIES,
  samplePdfChunks, textFill,
} from './sample-pdf-content';
import { denyExternalRequests, waitForImages } from './support';

// Samples use the production print flags and retain their product-image
// comparisons until display cutover (docs/design/display-assertions.md).
const PAGES_DIR = resolve(import.meta.dirname, '../public/templates/pages');
const CANDIDATE_SUBDIR = 'template-pages';
// About 1240px wide for an A4 page (docs/design/templates/print.md §2):
// large enough for a library card and the template page, small enough to
// ship ten-plus documents.
const RASTER_DPI = 150;
const POINTS_PER_MM = 72 / 25.4;
interface ManifestEntry {
  readonly templateId: string;
  readonly lng: 'en' | 'vi';
  readonly pages: number;
  readonly width: number;
  readonly height: number;
  readonly files: readonly string[];
}

// Accepted one-page A4 rasters at 150 DPI. Each cell checks its own output;
// manifest validation never depends on another test's process or result.
const manifestEntries: readonly ManifestEntry[] = SAMPLES.map((sample) => ({
  ...sample, pages: 1, width: 1240, height: 1754,
  files: [`${sample.templateId}-${sample.lng}-p1.png`],
}));

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

test.describe.configure({ mode: 'default' });

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
    await assertPrintDom(page, family, sample.customization.layout.columns,
      sample.customization.spacing);
    await assertPrintRules(page);
    if (sample.personalDetails.photo) {
      await assertPhotoCrop(page, sample.personalDetails.photo.crop);
    }
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
      expect.soft(document.numPages).toBe(1);
      expect.soft(SAMPLE_PAGES[templateId]).toBe(1);
      assertSampleFill(firstFill);
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
        assertPdfPageGeometry([textPage], 1, 'a4');
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
      expect({
        templateId,
        lng,
        pages: document.numPages,
        width,
        height,
        files,
      }).toEqual(manifestEntries.find((entry) =>
        entry.templateId === templateId && entry.lng === lng));
    } finally {
      await loadingTask.destroy();
    }
  });
}

test('sample paragraphs preserve input text across inline markup', async () => {
  const sample: Resume = JSON.parse(await readFile(resolve(
    import.meta.dirname,
    '../../../packages/schema/samples/modern-sidebar.vi.json',
  ), 'utf8'));
  const profile = sample.content.profile!;
  expect(profile.sectionType).toBe('profile');
  if (profile.sectionType !== 'profile') {
    throw new Error('Expected profile input fixture.');
  }
  sample.customization.layout.sections = { main: ['profile'], sidebar: [] };
  profile.entries[0]!.text = '<p>Nội dung <strong>đầu tiên</strong>.</p>'
    + '<ul><li>Nội dung trong danh sách.</li></ul><p>Đoạn thứ hai.</p>';
  expect(samplePdfParagraphs(sample, 'vi')).toEqual([
    { text: 'Nội dung đầu tiên.' }, { text: 'Đoạn thứ hai.' },
  ]);
});

for (const templateId of ['ats-plain', 'elegant-serif-two']) {
  test(`${templateId} print ink fits tall metrics and rejects clipping`,
    async ({ page }) => {
      const external = await denyExternalRequests(page);
      await page.emulateMedia({ media: 'print' });
      const sample: Resume = JSON.parse(await readFile(resolve(
        import.meta.dirname, '../../../packages/schema/samples',
        `${templateId}.vi.json`,
      ), 'utf8'));
      const margins = sample.customization.spacing.pageMargin!;
      await page.setViewportSize({
        width: Math.round((210 - 2 * margins.x) * 96 / 25.4), height: 1123,
      });
      expect((await page.goto(`/_harness/render?fixture=sample-${templateId}-vi`
        + `&template=${templateId}&mode=continuous&print=1`))?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      const nominalOverflow = await page.locator(
        '.resume-name, .section-heading h2',
      ).evaluateAll((elements) => elements.some((element) => {
        const range = document.createRange();
        range.selectNodeContents(element);
        const box = element.getBoundingClientRect();
        return Array.from(range.getClientRects()).some((rect) =>
          rect.top < box.top - 1 || rect.bottom > box.bottom + 1);
      }));
      expect(nominalOverflow, 'Font metrics exceed nominal heading boxes.')
        .toBe(true);
      const [family] = FAMILIES[sample.customization.font.family]!;
      const check = () => assertPrintDom(page, family,
        sample.customization.layout.columns, sample.customization.spacing,
        'Print text containment.',
      );
      await check();
      const name = page.locator('.resume-name');
      await applyStyleMutation(name, { height: '1px', overflow: 'hidden' });
      expect(await name.evaluate((element) =>
        element.scrollHeight > element.clientHeight + 1)).toBe(true);
      await expect(check()).rejects.toThrow('Print text containment.');
      expect(external).toEqual([]);
    });
}

test('print heading accents reject clipping without scroll overflow',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({ width: 688, height: 1123 });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const root = page.locator('.resume-document');
    const heading = page.locator('.section-heading')
      .filter({ hasText: 'Học vấn' });
    const check = () => assertPrintDom(page, 'Be Vietnam Pro', 2, undefined,
      'Print text containment.');
    await check();
    const clean = await measurePrintTextContainment(root);
    const accent = clean.lines.find((line) => line.text === 'HỌC VẤN')!;
    expect(accent, 'Measured uppercase heading ink.').toBeDefined();
    const box = await heading.boundingBox();
    expect(accent.ink.top, 'Accents paint above the nominal heading box.')
      .toBeLessThan(box!.y - 1);
    const restore = await applyStyleMutation(heading, { overflow: 'hidden' });
    await expect.poll(() => heading.evaluate((element) => ({
      x: element.scrollWidth <= element.clientWidth + 1,
      y: element.scrollHeight <= element.clientHeight + 1,
      overflow: getComputedStyle(element).overflow,
    })), 'Clipping changes paint while scroll bounds still pass.').toEqual({
      x: true, y: true, overflow: 'hidden',
    });
    const clipped = await measurePrintTextContainment(root);
    expect(clipped.failures.length).toBeGreaterThan(0);
    expect(clipped.failures.every((failure) => failure.kind === 'ink'
      && failure.axis === 'y' && failure.text === 'HỌC VẤN'
      && failure.container.includes('.section-heading')),
    'Only the accent ink rejects the clipping mutation.').toBe(true);
    await expect(check()).rejects.toThrow('Print text containment.');
    await restore();
    await check();
    expect(external).toEqual([]);
  });

test('visible print heading ink rejects a collision with body text',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({ width: 688, height: 1123 });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const root = page.locator('.resume-document');
    const check = () => assertPrintDom(page, 'Be Vietnam Pro', 2, undefined,
      'Print text containment.');
    await check();
    const clean = await measurePrintTextContainment(root);
    const heading = clean.lines.find((line) => line.text === 'HỌC VẤN')!;
    const title = clean.lines.find((line) =>
      line.text === 'Cử nhân Marketing')!;
    const shift = title.ink.top - heading.ink.top;
    const target = page.locator('.section-heading')
      .filter({ hasText: 'Học vấn' });
    const restore = await applyStyleMutation(target, {
      transform: [`translateY(${shift}px)`, /^matrix\(/u],
    });
    const altered = await measurePrintTextContainment(root);
    expect(altered.failures.length).toBeGreaterThan(0);
    expect(altered.failures.every((failure) => failure.kind === 'overlap'),
      'Only painted heading/body collision rejects this mutation.').toBe(true);
    await expect(check()).rejects.toThrow('Print text containment.');
    await restore();
    await check();
    expect(external).toEqual([]);
  });

test('print ink follows each line and its complete shaping style',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const paragraph = page.locator('.entry-body p').first();
    await paragraph.evaluate((element) => {
      element.textContent = 'xxxx 123\nẤgj 456';
    });
    await expect(paragraph).toHaveText('xxxx 123\nẤgj 456');
    await applyStyleMutation(paragraph, {
      'font-size': '32px', 'line-height': '48px', 'white-space': 'pre',
      'font-variant-caps': 'small-caps', 'font-variant-numeric': 'tabular-nums',
      'font-feature-settings': '"kern" 0',
      'font-variation-settings': '"wght" 550',
      'letter-spacing': '2px', 'word-spacing': '3px',
      'padding': '16px', 'width': '400px',
    });
    const measured = await measurePrintTextContainment(paragraph);
    expect({ visible: measured.visible, contained: measured.contained,
      failures: measured.failures }).toEqual({
      visible: true, contained: true, failures: [],
    });
    expect(measured.lines.map((line) => line.text))
      .toEqual(['xxxx 123', 'Ấgj 456']);
    expect(measured.lines[0]!.metrics.ascent)
      .toBeLessThan(measured.lines[1]!.metrics.ascent);
    for (const line of measured.lines) {
      expect(Math.abs(line.advance - line.referenceWidth),
        'Canvas and DOM use the same complete shaping state.')
        .toBeLessThanOrEqual(1);
    }
    await paragraph.evaluate((element) => {
      element.textContent = element.textContent!.replace('\n', ' ');
    });
    const width = Math.ceil(Math.max(...measured.lines.map((line) =>
      line.referenceWidth))) + 32;
    await applyStyleMutation(paragraph, {
      'white-space': 'normal', 'width': `${width}px`,
    });
    const wrapped = await measurePrintTextContainment(paragraph);
    expect(wrapped.failures).toEqual([]);
    expect(wrapped.lines.map((line) => line.text.trim()))
      .toEqual(['xxxx 123', 'Ấgj 456']);
    expect(wrapped.lines[0]!.metrics.ascent)
      .toBeLessThan(wrapped.lines[1]!.metrics.ascent);
    expect(external).toEqual([]);
  });

test('sample print DOM and PDF detect dropped bold with shared glyphs',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto('/_harness/render?fixture=sample-minimal-air-en'
      + '&template=minimal-air&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const samplePath = resolve(import.meta.dirname,
      '../../../packages/schema/samples/minimal-air.en.json');
    const sample = JSON.parse(await readFile(samplePath, 'utf8')) as Resume;
    const margins = sample.customization.spacing.pageMargin!;
    const inset = { x: margins.x * POINTS_PER_MM,
      y: margins.y * POINTS_PER_MM };
    await page.setViewportSize({
      width: Math.round((210 - 2 * margins.x) * 96 / 25.4), height: 1123,
    });
    const heading = 'Shared heading glyphs';
    const regular = 'Shared regular glyphs';
    const name = page.locator('.resume-name');
    await name.evaluate((element, text) => {
      element.textContent = text;
    }, heading);
    await page.locator('.resume-headline').evaluate((element, text) => {
      element.textContent = text;
    }, regular);
    sample.personalDetails.fullName = heading;
    sample.personalDetails.headline = regular;
    const layout = await measurePdfLayout(page, inset);
    const baseline = await printedDisplayPages(page);
    expect(baseline).toHaveLength(1);
    await assertSampleStyle(sample, 'minimal-air', 'en', layout, baseline[0]!);
    const expected = { color: sample.customization.colors.primary,
      size: sample.customization.font.baseSizePx * 2 * 0.75,
      family: 'Inter', bold: true };
    const correct = pdfVariableTextWeights(baseline[0]!.paint, 'Inter',
      [regular]);
    const checkPdf = (paint: typeof correct) => {
      expect(pdfTextStyleMatches(paint, heading, expected),
        'PDF heading weight. '
        + pdfTextStyleDiagnostic(paint, heading, expected)).toBe(true);
    };
    checkPdf(correct);
    const restore = await applyStyleMutation(name, { 'font-weight': '400' });
    const alteredLayout = await measurePdfLayout(page, inset);
    expect(alteredLayout.chunks.find(({ role }) => role === 'name')?.weight)
      .toBe('400');
    await expect(assertSampleStyle(sample, 'minimal-air', 'en',
      alteredLayout, baseline[0]!, 'text weight'))
      .rejects.toThrow('Print DOM text weight.');
    const altered = await printedDisplayPages(page);
    expect(altered).not.toEqual(baseline);
    const dropped = pdfVariableTextWeights(altered[0]!.paint, 'Inter',
      [regular]);
    const diagnostic = pdfTextStyleDiagnostic(dropped, heading, expected);
    const measurements = JSON.parse(diagnostic.slice(diagnostic.indexOf('{')));
    expect(measurements.found).toMatchObject({ bold: false,
      weightEvidence: 'not-wider' });
    expect(measurements.found.advanceWeight.shared).toBeGreaterThanOrEqual(3);
    expect(() => checkPdf(dropped)).toThrow('PDF heading weight.');
    expect(pdfTextStyleMatches(dropped, heading, expected), diagnostic)
      .toBe(false);
    expect(altered).toHaveLength(1);
    await restore();
    await assertSampleStyle(sample, 'minimal-air', 'en',
      await measurePdfLayout(page, inset), baseline[0]!);
    expect(external).toEqual([]);
  });

test('gallery sample page manifest stays current', async () => {
  const testInfo = test.info();
  const json = `${JSON.stringify(manifestEntries, null, 2)}\n`;
  if (testInfo.config.updateSnapshots !== 'none') {
    const candidate = resolve(candidateRoot(), 'manifest.json');
    await mkdir(resolve(candidate, '..'), { recursive: true });
    await writeFile(candidate, json);
    return;
  }
  const expected = await readFile(resolve(PAGES_DIR, 'manifest.json'), 'utf8');
  const stored: Record<string, unknown>[] = JSON.parse(expected);
  const withHashes = manifestEntries.map((entry, index) => ({
    ...entry,
    ...Object.fromEntries(Object.entries(stored[index] ?? {}).filter(
      ([key]) => key === 'sourceSha256' || key === 'outputSha256',
    )),
  }));
  expect(`${JSON.stringify(withHashes, null, 2)}\n`).toBe(expected);
});

for (const defect of ['font size', 'text color', 'icon color',
  'sidebar tint', 'level bar',
  'photo', 'margin', 'heading rule', 'line spacing'] as const) {
  test(`sample PDF contract detects changed ${defect}`, async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print', colorScheme: 'dark' });
    const sample: Resume = JSON.parse(await readFile(resolve(
      import.meta.dirname,
      '../../../packages/schema/samples/modern-sidebar.vi.json',
    ), 'utf8'));
    const margin = sample.customization.spacing.pageMargin!;
    const margins = { x: margin.x * POINTS_PER_MM,
      y: margin.y * POINTS_PER_MM };
    await page.setViewportSize({
      width: Math.round((210 - 2 * margin.x) * 96 / 25.4), height: 1123,
    });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const baseline = await printedDisplayPages(page);
    assertPdfPageGeometry(baseline.map(({ text }) => text), 1, 'a4');
    const check = async (display: PdfDisplayPage[], focused = false) => {
      await assertSampleStyle(sample, 'modern-sidebar', 'vi',
        await measurePdfLayout(page, margins), display[0]!,
        focused ? defect : undefined);
    };
    await check(baseline);
    await check(baseline, true);
    let message: string;
    if (defect === 'font size') {
      await applyStyleMutation(page.locator('.resume-document'), {
        'font-size': '16px',
      });
      message = 'Print DOM font size.';
    } else if (defect === 'text color') {
      await applyStyleMutation(page.locator('.entry-body'), {
        color: 'rgb(0, 0, 0)',
      });
      message = 'Print DOM text color.';
    } else if (defect === 'icon color') {
      await applyStyleMutation(page.locator('.resume-icon'), {
        color: 'rgb(0, 0, 0)',
      });
      message = 'Print icon color.';
    } else if (defect === 'sidebar tint') {
      await applyStyleMutation(page.locator('.resume-sidebar'), {
        'background-color': 'rgb(0, 0, 0)',
      });
      message = 'PDF sample surface paint.';
    } else if (defect === 'level bar') {
      await applyStyleMutation(page.locator('.level-fill'), {
        width: ['20%', /^\d+(?:\.\d+)?px$/u],
      });
      message = 'PDF widget measurements:';
    } else if (defect === 'photo') {
      await page.locator('.resume-header').evaluate(async (header, src) => {
        const image = document.createElement('img');
        image.src = src;
        image.style.cssText = 'position: absolute; width: 96px; height: 96px';
        header.append(image);
        await image.decode();
      }, FIXED_PHOTO_DATA_URL);
      await expect(page.locator('.resume-header img')).toHaveCount(1);
      message = 'PDF sample photo count.';
    } else if (defect === 'margin') {
      await applyStyleMutation(page.locator('.resume-name'), {
        transform: ['translateX(-8px)', 'matrix(1, 0, 0, 1, -8, 0)'],
      });
      message = 'PDF glyph overflow in points:';
    } else if (defect === 'heading rule') {
      await applyStyleMutation(page.locator('.section-heading'), {
        'border-bottom-width': '0px',
      });
      message = 'Print heading rule.';
    } else {
      await applyStyleMutation(page.locator('.entry-body p').first(), {
        'line-height': '23px',
      });
      message = 'PDF paragraph line pitch.';
    }
    const altered = await printedDisplayPages(page);
    expect(altered).not.toEqual(baseline);
    if (defect === 'line spacing') {
      const paragraph = samplePdfChunks(sample, 'vi').find((chunk) =>
        chunk.region === 'main' && chunk.role === 'body')!.value;
      expect(pdfParagraphGeometry(altered[0]!.text, [{
        text: paragraph, pitch: 23 * 0.75,
      }]), 'Mutated paragraph remains complete at changed pitch.').toBe(true);
    }
    await expect(check(altered, true)).rejects.toThrow(message);
    expect(external).toEqual([]);
  });
}

for (const boundary of ['minimum', 'maximum'] as const) {
  test(`sample PDF fill detects a broken ${boundary}`, async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({ width: 688, height: 1123 });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const baseline = await withPrintedPdf(page, (part) => textFill(part, 13));
    expect(baseline).toHaveLength(1);
    assertSampleFill(baseline[0]!);
    if (boundary === 'minimum') {
      await applyStyleMutation(page.locator('.resume-document'), {
        'transform-origin': '0px 0px',
        'transform': ['scaleY(0.8)', /^matrix\(/u],
      });
    } else {
      // Scaling the whole root can add a page containing only empty overflow.
      // Move only the sample's final text, keeping it inside the paper margins.
      const shift = (0.99 - baseline[0]!) * (297 - 2 * 13) * 96 / 25.4;
      await applyStyleMutation(
        page.locator('.resume-main .entry-meta').last(), {
          transform: [`translateY(${shift}px)`, /^matrix\(/u],
        },
      );
    }
    const altered = await withPrintedPdf(page, (part) => textFill(part, 13));
    expect(altered, 'Fill mutation keeps the one-page sample.')
      .toHaveLength(1);
    expect(altered[0]).not.toBe(baseline[0]);
    expect(() => assertSampleFill(altered[0]!))
      .toThrow(`PDF sample ${boundary} fill.`);
    if (boundary === 'maximum') {
      expect(Math.abs(altered[0]! - 0.99)).toBeLessThanOrEqual(0.002);
    }
    expect(external).toEqual([]);
  });
}
