import { createCanvas, loadImage } from '@napi-rs/canvas';
import { expect, test, type TestInfo } from '@playwright/test';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { measurePrintRules } from './display-assertions';
import {
  attachMembership,
  checkMembership,
  EXPECTED_PAGES,
  pdfMarkerPosition,
  pdfTextPage,
  PRINT_CASES as CASES,
} from './pdf-display';
import {
  denyExternalRequests,
  FIXTURE_PHOTO_CROP_GEOMETRY,
  photoCropGeometry,
  waitForImages,
} from './support';

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

async function verifyRaster(
  bytes: Buffer,
  filename: string,
  testInfo: TestInfo,
): Promise<void> {
  const actualPath = testInfo.outputPath(filename);
  await writeFile(actualPath, bytes);
  const update = testInfo.config.updateSnapshots !== 'none';
  if (update) {
    const root = process.env.PLAYWRIGHT_RESULTS_DIR;
    if (root === undefined) {
      throw new Error('PLAYWRIGHT_RESULTS_DIR is required.');
    }
    const candidate = resolve(
      root,
      'candidate-baselines/print-baselines',
      filename,
    );
    await mkdir(resolve(candidate, '..'), { recursive: true });
    await writeFile(candidate, bytes);
    return;
  }
  const expectedPath = resolve(
    import.meta.dirname,
    'print-baselines',
    filename,
  );
  const expected = await readFile(expectedPath);
  await compareRaster(
    bytes,
    expected,
    testInfo.outputPath(filename.replace(/\.png$/, '-diff.png')),
  );
}

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
        const viewport = pdfPage.getViewport({ scale: 96 / 72 });
        const canvas = createCanvas(
          Math.ceil(viewport.width),
          Math.ceil(viewport.height),
        );
        await pdfPage.render({
          canvas: canvas as unknown as HTMLCanvasElement,
          canvasContext: (
            canvas.getContext('2d') as unknown as CanvasRenderingContext2D
          ),
          intent: 'print',
          viewport,
        }).promise;
        await verifyRaster(
          canvas.toBuffer('image/png'),
          `${printCase.fixture}-p${pageNumber}.png`,
          testInfo,
        );
      }

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
