import { createCanvas } from '@napi-rs/canvas';
import { expect, test } from '@playwright/test';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { writeFile } from 'node:fs/promises';

import { captureReviewImage } from './review-images';
import { measurePdfLayout, measurePrintRules } from './display-assertions';
import { assertPrintDom, assertPrintRules } from './pdf-fonts';
import { applyStyleMutation } from './style-mutation';
import { pdfPaintPage } from './pdf-paint';
import { assertOverflowTextStyle } from './pdf-text-style';
import { overflowPdfChunks } from './pdf-input-content';
import { assertOverflowPdf } from './sample-pdf-content';
import {
  attachMembership,
  checkMembership,
  EXPECTED_PAGES,
  pdfMarkerPosition,
  pdfTextPage,
  printedTextPages,
  type PdfTextPage,
  PRINT_CASES as CASES,
} from './pdf-display';
import {
  denyExternalRequests,
  FIXTURE_PHOTO_CROP_GEOMETRY,
  photoCropGeometry,
  waitForImages,
} from './support';

test.describe.configure({ mode: 'default' });

test('wraps an unbroken entry token within its column', async ({ page }) => {
  test.setTimeout(20_000);
  const external = await denyExternalRequests(page);
  const response = await page.goto(
    '/_harness/render?fixture=print-main-overflow'
    + '&template=modern-sidebar&mode=continuous',
  );
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);

  const entryBody = page.locator('.entry-body p').first();
  await entryBody.evaluate((element) => {
    element.textContent = 'x'.repeat(16 * 1024);
  });

  for (const media of ['screen', 'print'] as const) {
    await page.emulateMedia({ media });
    const lines = await entryBody.evaluate((element) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      return range.getClientRects().length;
    });
    expect(lines).toBeGreaterThan(1);
    await expect.poll(async () => entryBody.evaluate((element) =>
      element.clientWidth > 0 && element.scrollWidth <= element.clientWidth,
    )).toBe(true);
  }
  expect(external).toEqual([]);
});

for (const printCase of CASES) {
  test(printCase.fixture, async ({ page }, testInfo) => {
    test.setTimeout(20_000);
    const external = await denyExternalRequests(page);
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    const response = await page.goto(
      `/_harness/render?fixture=${printCase.fixture}`
      + '&template=modern-sidebar&mode=continuous',
    );
    expect(response?.ok()).toBe(true);
    await expect(
      page.locator('[data-render-mode="continuous"]'),
    ).toHaveCount(1);
    await expect(page.locator('[data-pagination-settled]')).toHaveCount(0);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    expect(external).toEqual([]);
    await page.emulateMedia({ media: 'print' });
    await assertPrintDom(page, 'Be Vietnam Pro', 2);
    expect(await photoCropGeometry(page)).toEqual(FIXTURE_PHOTO_CROP_GEOMETRY);
    expect(await measurePrintRules(page)).toEqual({
      bodyHeaderAfter: 'avoid',
      entryInside: 'auto',
      headerInside: 'avoid',
      orphans: '2',
      printColorAdjust: 'exact',
      sectionAfter: 'avoid',
      sectionInside: 'auto',
      widows: '2',
    });

    const pdf = await page.pdf({
      displayHeaderFooter: false,
      margin: { bottom: 0, left: 0, right: 0, top: 0 },
      preferCSSPageSize: true,
      printBackground: true,
      scale: 1,
    });
    await writeFile(testInfo.outputPath(`${printCase.fixture}.pdf`), pdf);

    const loadingTask = getDocument({
      data: new Uint8Array(pdf),
      isImageDecoderSupported: false,
      isOffscreenCanvasSupported: false,
      useSystemFonts: false,
    });
    try {
      const document = await loadingTask.promise;
      const membershipPages = await Promise.all(
        Array.from({ length: document.numPages }, async (_, index) =>
          pdfTextPage(await document.getPage(index + 1)),
        ),
      );
      const membership = await attachMembership(
        membershipPages, printCase, testInfo,
      );
      checkMembership(membership, `${printCase.fixture}-vi-a4-light`);
      const layout = await measurePdfLayout(page, {
        x: 14 * 72 / 25.4, y: 13 * 72 / 25.4,
      });
      const display = await Promise.all(membershipPages.map(async (text, i) =>
        ({ text, paint: await pdfPaintPage(await document.getPage(i + 1)) })));
      await assertOverflowPdf(display, layout, printCase, 'vi', 'a4', 'light');
      assertOverflowTextStyle(layout, {
        fills: [], images: [], text: display.flatMap(({ paint }) => paint.text),
      }, await overflowPdfChunks(printCase, 'vi'));
      expect(document.numPages).toBeGreaterThanOrEqual(2);
      expect(document.numPages).toBe(EXPECTED_PAGES);
      const textPages = [];
      for (
        let pageNumber = 1;
        pageNumber <= document.numPages;
        pageNumber += 1
      ) {
        const pdfPage = await document.getPage(pageNumber);
        textPages.push(await pdfTextPage(pdfPage));
      }

      const review = printCase.fixture === 'print-main-overflow'
        ? 'print-main-overflow-p2'
        : 'print-sidebar-overflow-p2';
      await captureReviewImage(testInfo, review, async () => {
        const pdfPage = await document.getPage(2);
        const viewport = pdfPage.getViewport({ scale: 96 / 72 });
        const canvas = createCanvas(
          Math.ceil(viewport.width), Math.ceil(viewport.height),
        );
        await pdfPage.render({
          canvas: canvas as unknown as HTMLCanvasElement,
          canvasContext: (
            canvas.getContext('2d') as unknown as CanvasRenderingContext2D
          ),
          intent: 'print', viewport,
        }).promise;
        return canvas.toBuffer('image/png');
      });

      const lastOnFirst = printCase.fixture === 'print-main-overflow' ? 7 : 11;
      const pageMarkers = textPages.map((textPage) => textPage.text);
      for (let number = 1; number <= printCase.total; number += 1) {
        const marker = `${printCase.prefix}${String(number).padStart(2, '0')}`;
        const expectedPage = number <= lastOnFirst ? 0 : 1;
        expect(pageMarkers[expectedPage]).toContain(marker);
        expect(pageMarkers[1 - expectedPage]).not.toContain(marker);
      }
      const firstPosition = pdfMarkerPosition(
        textPages[0]!, `${printCase.prefix}02`,
      );
      const secondPosition = pdfMarkerPosition(
        textPages[1]!,
        `${printCase.prefix}${String(lastOnFirst + 1).padStart(2, '0')}`,
      );
      expect(firstPosition).not.toBeNull();
      expect(secondPosition).not.toBeNull();
      expect(Math.abs(firstPosition!.x - secondPosition!.x))
        .toBeLessThanOrEqual(1);
      for (const textPage of textPages) {
        expect(Math.abs(textPage.width - 595.28)).toBeLessThanOrEqual(1);
        expect(Math.abs(textPage.height - 841.89)).toBeLessThanOrEqual(1);
        expect(textPage.items.length).toBeGreaterThan(0);
      }

      expect(pageMarkers[0]).toContain(printCase.start);
      expect(pageMarkers[1]).toContain(printCase.end);
      expect(pageMarkers[0]).toContain(printCase.opposite);
      expect(pageMarkers[1]).not.toContain(printCase.opposite);
      const allText = pageMarkers.join(' ');
      let previous = -1;
      for (let number = 1; number <= printCase.total; number += 1) {
        const marker = `${printCase.prefix}${String(number).padStart(2, '0')}`;
        expect(allText.split(marker)).toHaveLength(2);
        const offset = allText.indexOf(marker);
        expect(offset).toBeGreaterThan(previous);
        previous = offset;
      }
    } finally {
      await loadingTask.destroy();
    }
  });
}

for (const fixture of ['print-main-overflow', 'sample-modern-sidebar-vi']) {
  test(`${fixture} print rules detect relaxed widows`, async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto(`/_harness/render?fixture=${fixture}`
      + '&template=modern-sidebar&mode=continuous&print=1'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await assertPrintRules(page);
    await applyStyleMutation(page.locator('.entry-body p'), { widows: '1' });
    await expect(assertPrintRules(page))
      .rejects.toThrow('Print fragmentation rules.');
    expect(external).toEqual([]);
  });
}

test('PDF membership detects an orphan section heading', async ({ page }) => {
  const external = await denyExternalRequests(page);
  await page.emulateMedia({ media: 'print' });
  expect((await page.goto('/_harness/render?fixture=print-probe-heading'
    + '&template=modern-sidebar&mode=continuous&paper=a4'))?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const check = (pages: readonly PdfTextPage[]) => {
    for (const marker of ['KEEP-HEADING', 'KEEP-TITLE', 'KEEP-FIRST']) {
      expect(pages[1]!.text, 'PDF heading with first body fragment.')
        .toContain(marker);
      expect(pages[0]!.text, 'PDF heading with first body fragment.')
        .not.toContain(marker);
    }
  };
  const baseline = await printedTextPages(page);
  expect(baseline).toHaveLength(2);
  check(baseline);
  await applyStyleMutation(page.locator('.section-heading'), {
    'break-after': 'auto',
  });
  const altered = await printedTextPages(page);
  expect(altered).not.toEqual(baseline);
  expect(() => check(altered)).toThrow('PDF heading with first body fragment.');
  expect(altered).toHaveLength(2);
  expect(external).toEqual([]);
});
