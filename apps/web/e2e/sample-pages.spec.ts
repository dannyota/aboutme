import { expect, test } from '@playwright/test';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { SAMPLES } from '@aboutme/schema/samples';
import type { Resume } from '@aboutme/schema';

import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';
import { SAMPLE_PAGES } from '../app/templates/samplePages';
import { measurePdfLayout } from './display-assertions';
import { applyStyleMutation, mutationTest } from './style-mutation';
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

// Samples use production print flags and assert PDF content and layout.
const PAGES_DIR = resolve(import.meta.dirname, '../public/templates/pages');
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

test.describe.configure({ mode: 'default' });

for (const { templateId, lng } of SAMPLES) {
  test(`${templateId} (${lng}) keeps PDF content and geometry`, async (
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
        const filename = `${templateId}-${lng}-p${pageNumber}.png`;
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
  mutationTest(
    `${templateId} print ranges allow visible overhang and reject clipping`,
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

mutationTest('print heading displacement outside an ancestor clip fails',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({ width: 688, height: 1123 });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const heading = page.locator('.section-heading').first();
    const title = heading.locator('h2');
    await applyStyleMutation(heading, { overflow: 'hidden' });
    const check = async () => {
      const measured = await measurePrintTextContainment(title);
      expect({ visible: measured.visible, contained: measured.contained,
        failures: measured.failures }, 'Print heading containment. '
        + JSON.stringify(measured.failures)).toEqual({
        visible: true, contained: true, failures: [],
      });
    };
    await check();
    const restore = await applyStyleMutation(title, {
      transform: ['translateY(-64px)', 'matrix(1, 0, 0, 1, 0, -64)'],
    });
    const altered = await measurePrintTextContainment(title);
    expect(altered.failures.some((failure) => failure.kind === 'clip'
      && failure.axis === 'y' && failure.delta > 1
      && failure.container.includes('.section-heading'))).toBe(true);
    await expect(check()).rejects.toThrow('Print heading containment.');
    await restore();
    await check();
    expect(external).toEqual([]);
  });

test('print text ranges support transformed text and font features',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const paragraph = page.locator('.entry-body p').first();
    await applyStyleMutation(paragraph, {
      'text-transform': 'capitalize',
      'font-variant-caps': 'small-caps',
      'font-variant-numeric': 'tabular-nums',
      'font-feature-settings': '"kern" 0',
      'font-variation-settings': '"wght" 550',
    });
    const measured = await measurePrintTextContainment(paragraph);
    expect({ visible: measured.visible, contained: measured.contained,
      failures: measured.failures }).toEqual({
      visible: true, contained: true, failures: [],
    });
    expect(external).toEqual([]);
  });

for (const defect of ['ancestor clip', 'line overlap', 'horizontal overflow']) {
  mutationTest(`print text ranges detect ${defect}`, async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto('/_harness/render?fixture=sample-modern-sidebar-vi'
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const paragraph = page.locator('.entry-body p').first();
    await paragraph.evaluate((element) => {
      const word = element.textContent!.trim().split(/\s+/u)[0]!;
      element.textContent = `${word}\n${word}`;
    });
    await applyStyleMutation(paragraph, {
      'white-space': 'pre', 'line-height': '32px',
    });
    const check = async () => {
      const measured = await measurePrintTextContainment(paragraph);
      expect({ visible: measured.visible, contained: measured.contained,
        failures: measured.failures }, 'Print range containment. '
        + JSON.stringify(measured.failures)).toEqual({
        visible: true, contained: true, failures: [],
      });
    };
    await check();
    const clean = await measurePrintTextContainment(paragraph);
    expect(clean.lines).toHaveLength(2);
    let restore: () => Promise<void>;
    let kind: string;
    if (defect === 'ancestor clip') {
      restore = await applyStyleMutation(paragraph.locator('..'), {
        height: '1px', overflow: 'hidden',
      });
      kind = 'clip';
    } else if (defect === 'line overlap') {
      restore = await applyStyleMutation(paragraph, { 'line-height': '1px' });
      kind = 'overlap';
    } else {
      restore = await applyStyleMutation(paragraph, { width: '1px' });
      kind = 'range';
    }
    const altered = await measurePrintTextContainment(paragraph);
    expect(altered.failures.some((failure) => failure.kind === kind))
      .toBe(true);
    await expect(check()).rejects.toThrow('Print range containment.');
    await restore();
    await check();
    expect(external).toEqual([]);
  });
}

mutationTest('sample print DOM and PDF detect dropped bold with shared glyphs',
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
  mutationTest(`sample PDF contract detects changed ${defect}`, async ({
    page,
  }) => {
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
  mutationTest(`sample PDF fill detects a broken ${boundary}`, async ({
    page,
  }) => {
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
