import { createCanvas, loadImage } from '@napi-rs/canvas';
import { expect, test, type Page, type TestInfo } from '@playwright/test';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { measurePrintRules } from './display-assertions';
import {
  pdfEntryIds,
  pdfMarkerPosition,
  pdfMembership,
  pdfTextPage,
  type PdfMembership,
  type PdfTextPage,
} from './pdf-display';
import {
  denyExternalRequests,
  FIXTURE_PHOTO_CROP_GEOMETRY,
  photoCropGeometry,
  waitForImages,
} from './support';

const LONG_TEMPLATES = [
  'elegant-serif-two',
  'engineer-compact',
  'international-lang',
  'modern-sidebar',
  'nordic-muted',
  'one-page-tight',
  'executive-band',
] as const;

interface PrintCase {
  readonly end: string;
  readonly fixture: 'print-main-overflow' | 'print-sidebar-overflow';
  readonly opposite: string;
  readonly prefix: 'MAIN-ENTRY-' | 'SIDEBAR-ENTRY-';
  readonly start: string;
  readonly total: number;
}

const CASES: readonly PrintCase[] = [
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

const EXPECTED_PAGES = 2;

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

async function attachMembership(
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

function checkMembership(membership: PdfMembership, key: MembershipKey): void {
  const expected = EXPECTED_MEMBERSHIP[key];
  if (expected !== undefined) expect(membership).toEqual(expected);
  else if (process.env.CI) {
    expect.soft(
      expected,
      `PDF membership has not been approved: ${key} `
      + JSON.stringify(membership),
    ).toBeDefined();
  }
}

function expectedEntryIds(printCase: PrintCase): string[] {
  return Array.from({ length: printCase.total }, (_, index) =>
    `${printCase.prefix}${String(index + 1).padStart(2, '0')}`,
  );
}

async function printedTextPages(page: Page): Promise<PdfTextPage[]> {
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
        pdfTextPage(await document.getPage(index + 1)),
      ),
    );
  } finally {
    await loadingTask.destroy();
  }
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
      const pageTexts: string[] = [];
      const textPages = [];
      for (
        let pageNumber = 1;
        pageNumber <= document.numPages;
        pageNumber += 1
      ) {
        const pdfPage = await document.getPage(pageNumber);
        textPages.push(await pdfTextPage(pdfPage));
        const text = await pdfPage.getTextContent();
        pageTexts.push(text.items
          .map((item) => ('str' in item ? item.str : ''))
          .join(' '));
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
      const pageMarkers = textPages.map((textPage) => textPage.text
        .replace(/-\s+(?=ENTRY-\d{2})/g, '-'));
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

      expect(pageTexts[0]).toContain(printCase.start);
      expect(pageTexts[1]).toContain(printCase.end);
      expect(pageTexts[0]).toContain(printCase.opposite);
      expect(pageTexts[1]).not.toContain(printCase.opposite);
      const allText = pageTexts.join(' ')
        .replace(/-\s+(?=ENTRY-\d{2})/g, '-');
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

for (const printCase of CASES) {
  for (const lng of ['en', 'vi'] as const) {
    for (const paper of ['a4', 'letter'] as const) {
      for (const scheme of ['light', 'dark'] as const) {
        test(
          `${printCase.fixture} ${lng} ${paper} ${scheme}`,
          async ({ page }, testInfo) => {
            test.setTimeout(20_000);
            const external = await denyExternalRequests(page);
            await page.emulateMedia({ colorScheme: scheme, media: 'print' });
            const response = await page.goto(
              `/_harness/render?fixture=${printCase.fixture}`
              + `&template=modern-sidebar&mode=continuous&lng=${lng}`
              + `&paper=${paper}`,
            );
            expect(response?.ok()).toBe(true);
            await expect(
              page.locator('[data-fonts-ready="true"]'),
            ).toHaveCount(1);
            await waitForImages(page);
            await expect(page.locator('.resume-document')).toHaveCSS(
              'background-color', 'rgb(255, 255, 255)',
            );
            await expect(page.locator('.resume-sidebar')).toHaveCSS(
              'background-color', 'rgb(227, 236, 240)',
            );
            expect(await photoCropGeometry(page))
              .toEqual(FIXTURE_PHOTO_CROP_GEOMETRY);
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
              const pages = await Promise.all(
                Array.from({ length: document.numPages }, async (_, index) =>
                  pdfTextPage(await document.getPage(index + 1)),
                ),
              );
              const key: MembershipKey
                = `${printCase.fixture}-${lng}-${paper}-${scheme}`;
              const membership = await attachMembership(
                pages, printCase, testInfo,
              );
              checkMembership(membership, key);
              expect(document.numPages).toBe(EXPECTED_PAGES);
              const dimensions = paper === 'a4' ? [595.28, 841.89] : [612, 792];
              for (const pdfPage of pages) {
                expect(Math.abs(pdfPage.width - dimensions[0]!))
                  .toBeLessThanOrEqual(1);
                expect(Math.abs(pdfPage.height - dimensions[1]!))
                  .toBeLessThanOrEqual(1);
                expect(pdfPage.items.length).toBeGreaterThan(0);
              }
              expect(pdfEntryIds(
                pages,
                printCase.prefix === 'MAIN-ENTRY-' ? 'MAIN' : 'SIDEBAR',
              )).toEqual(expectedEntryIds(printCase));
              const column = printCase.prefix === 'MAIN-ENTRY-'
                ? 'main'
                : 'sidebar';
              const firstId = membership.pages[0]?.[column].first;
              const continuationId = membership.pages[1]?.[column].first;
              expect(firstId).toBeTruthy();
              expect(continuationId).toBeTruthy();
              const first = pdfMarkerPosition(pages[0]!, firstId!);
              const continuation = pdfMarkerPosition(
                pages[1]!, continuationId!,
              );
              expect(first).not.toBeNull();
              expect(continuation).not.toBeNull();
              expect(Math.abs(first!.x - continuation!.x))
                .toBeLessThanOrEqual(1);
              const bodyText = printCase.fixture === 'print-main-overflow'
                ? (lng === 'vi'
                    ? 'Nội dung kiểm tra phân trang bản in'
                    : 'Print pagination test content')
                : (lng === 'vi'
                    ? 'Nội dung kiểm tra phân trang'
                    : 'Pagination test content');
              expect(pages.map((part) => part.text).join(' '))
                .toContain(bodyText);
              expect(pages[0]!.text).toContain(printCase.start);
              expect(pages[0]!.text).toContain(printCase.opposite);
              expect(pages[1]!.text).toContain(printCase.end);
              expect(pages[1]!.text).not.toContain(printCase.opposite);
              const text = pages.map((pdfPage) => pdfPage.text).join(' ')
                .replace(/-\s+(?=ENTRY-\d{2})/g, '-');
              let previous = -1;
              for (let number = 1; number <= printCase.total; number += 1) {
                const marker = `${printCase.prefix}`
                  + String(number).padStart(2, '0');
                expect(text.split(marker)).toHaveLength(2);
                const position = text.indexOf(marker);
                expect(position).toBeGreaterThan(previous);
                previous = position;
              }
            } finally {
              await loadingTask.destroy();
            }
            expect(external).toEqual([]);
          },
        );
      }
    }
  }
}

test('PDF content measurements detect entry and accent mutations', async ({
  page,
}) => {
  test.setTimeout(60_000);
  const external = await denyExternalRequests(page);
  const printCase = CASES[1]!;
  const expected = expectedEntryIds(printCase);
  for (const mutation of ['hide', 'duplicate', 'reorder', 'accents'] as const) {
    const response = await page.goto(
      '/_harness/render?fixture=print-main-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4',
    );
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    await page.emulateMedia({ media: 'print' });
    const baseline = await printedTextPages(page);
    expect(pdfEntryIds(baseline, 'MAIN')).toEqual(expected);
    expect(baseline.map((part) => part.text).join(' '))
      .toContain('Nội dung kiểm tra phân trang bản in');
    const entries = page.locator('.resume-main .entry');
    await expect(entries).toHaveCount(printCase.total);
    await entries.first().evaluate((first, change) => {
      const next = first.nextElementSibling;
      if (change === 'hide') (first as HTMLElement).style.display = 'none';
      if (change === 'duplicate') first.after(first.cloneNode(true));
      if (change === 'reorder' && next !== null) first.before(next);
      if (change === 'accents') {
        first.closest('.resume-main')!.querySelectorAll('.entry-body')
          .forEach((body) => {
            body.textContent = (body.textContent ?? '')
              .normalize('NFD').replace(/[\u0300-\u036f]/g, '')
              .replace(/đ/g, 'd').replace(/Đ/g, 'D');
          });
      }
    }, mutation);
    const mutated = await printedTextPages(page);
    if (mutation === 'accents') {
      expect(mutated.map((part) => part.text).join(' '))
        .not.toContain('Nội dung kiểm tra phân trang bản in');
    } else {
      expect(pdfEntryIds(mutated, 'MAIN')).not.toEqual(expected);
    }
  }
  expect(external).toEqual([]);
});

for (const mutation of ['forced break', 'unbreakable entries'] as const) {
  test(`PDF membership detects ${mutation}`, async ({ page }) => {
    test.setTimeout(30_000);
    const external = await denyExternalRequests(page);
    const printCase = CASES[1]!;
    const response = await page.goto(
      '/_harness/render?fixture=print-main-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4',
    );
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    await page.emulateMedia({ media: 'print' });
    const baseline = pdfMembership(await printedTextPages(page), printCase);
    expect(baseline.pageCount).toBe(EXPECTED_PAGES);
    const entries = page.locator('.resume-main .entry');
    await expect(entries).toHaveCount(printCase.total);
    if (mutation === 'forced break') {
      await entries.nth(3).evaluate((entry) => {
        (entry as HTMLElement).style.breakBefore = 'page';
      });
    } else {
      await page.addStyleTag({
        content: '.entry { break-inside: avoid !important; }',
      });
    }
    const altered = pdfMembership(await printedTextPages(page), printCase);
    expect(altered).not.toEqual(baseline);
    expect(external).toEqual([]);
  });
}

function normalizeHeading(text: string | null): string {
  return (text ?? '').normalize('NFKC').replace(/\s+/g, '').toLowerCase();
}

// A long two-column resume must fragment each column in place across a page
// break rather than lose a column's content to a later page
// (docs/design/templates/print.md §5). No page may be blank, and the paged
// preview must estimate the PDF's page count (§1).
for (const templateId of LONG_TEMPLATES) {
  for (const lng of ['en', 'vi'] as const) {
    test(
      `long ${templateId} (${lng}) fills page one and prints no blank page`,
      async ({ page }, testInfo) => {
        test.setTimeout(30_000);
        const external = await denyExternalRequests(page);
        const pagedUrl = `/_harness/render?fixture=sample-${templateId}`
          + `-${lng}&template=${templateId}&mode=paged&repeat=2`;
        const pagedResponse = await page.goto(pagedUrl);
        expect(pagedResponse?.ok()).toBe(true);
        await expect(
          page.locator('[data-pagination-settled="true"]'),
        ).toHaveCount(1);
        const previewPageCount = await page
          .locator('.resume-page:not(.pagination-measurement)')
          .count();

        const continuousUrl = `/_harness/render?fixture=sample-${templateId}`
          + `-${lng}&template=${templateId}&mode=continuous&print=1`
          + '&repeat=2';
        const response = await page.goto(continuousUrl);
        expect(response?.ok()).toBe(true);
        await expect(
          page.locator('[data-fonts-ready="true"]'),
        ).toHaveCount(1);
        await waitForImages(page);

        const expectedHeadings = templateId === 'executive-band'
          ? [
              await page
                .locator('.layout-one-column .section-heading')
                .first()
                .textContent(),
            ]
          : await Promise.all([
              page
                .locator('.resume-main .section-heading')
                .first()
                .textContent(),
              page
                .locator('.resume-sidebar .section-heading')
                .first()
                .textContent(),
            ]);

        await page.emulateMedia({ media: 'print' });
        const pdf = await page.pdf({
          displayHeaderFooter: false,
          margin: { bottom: 0, left: 0, right: 0, top: 0 },
          preferCSSPageSize: true,
          printBackground: true,
          scale: 1,
        });
        await writeFile(
          testInfo.outputPath(`long-${templateId}-${lng}.pdf`),
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
          expect(document.numPages).toBeGreaterThanOrEqual(2);
          expect(document.numPages).toBe(previewPageCount);
          const pageTexts: string[] = [];
          for (
            let pageNumber = 1;
            pageNumber <= document.numPages;
            pageNumber += 1
          ) {
            const pdfPage = await document.getPage(pageNumber);
            const text = await pdfPage.getTextContent();
            pageTexts.push(text.items
              .map((item) => ('str' in item ? item.str : ''))
              .join(' '));
          }
          for (const pageText of pageTexts) {
            expect(normalizeHeading(pageText).length).toBeGreaterThan(0);
          }
          const firstPage = normalizeHeading(pageTexts[0]);
          for (const heading of expectedHeadings) {
            expect(firstPage).toContain(normalizeHeading(heading));
          }
        } finally {
          await loadingTask.destroy();
        }

        expect(external).toEqual([]);
      },
    );
  }
}
