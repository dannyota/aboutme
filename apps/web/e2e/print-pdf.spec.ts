import { expect, test } from '@playwright/test';
import {
  getDocument,
  OPS,
  type PDFPageProxy,
} from 'pdfjs-dist/legacy/build/pdf.mjs';
import { writeFile } from 'node:fs/promises';

import { measurePdfLayout } from './display-assertions';
import {
  attachMembership,
  checkMembership,
  expectedEntryIds,
  EXPECTED_PAGES,
  normalizePdfMarkers,
  pdfEntryIds,
  pdfColumnsMatch,
  pdfGlyphGeometry,
  pdfMarkerPosition,
  pdfMembership,
  pdfParagraphGeometry,
  pdfSidebarPaint,
  pdfTextPage,
  pdfWidgetMatches,
  pdfWidgetDiagnostic,
  printedDisplayPages,
  printedTextPages,
  PRINT_CASES as CASES,
  type PdfMembership,
  type PdfTextPage,
} from './pdf-display';
import {
  fixturePhotoIdentity,
  pdfImageMatches,
  pdfImageDiagnostic,
  pdfPaintOperators,
  pdfPaintPage,
  UNSUPPORTED_PDF_PAINT,
} from './pdf-paint';
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
const PROBE_ROUTE = '/_harness/render?template=modern-sidebar&paper=a4'
  + '&mode=continuous'
  + '&fixture=print-probe-';

test('PDF paint applies transforms, clips and graphics-state restoration',
  async () => {
    const page = { getViewport: () => ({
      width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
    }) } as unknown as PDFPageProxy;
    const rect = (size: number) => new Float32Array([
      0, 0, 0, 1, size, 0, 1, size, size, 1, 0, size, 4,
    ]);
    const paint = await pdfPaintOperators(page, [
      OPS.save, OPS.transform, OPS.clip, OPS.constructPath,
      OPS.setFillRGBColor, OPS.constructPath, OPS.restore,
      OPS.setFillRGBColor, OPS.constructPath,
    ], [
      [], [2, 0, 0, 2, 10, 10], [],
      [OPS.endPath, [rect(10)], [0, 0, 10, 10]], ['#e3ecf0'],
      [OPS.fill, [rect(20)], [0, 0, 20, 20]], [], ['#ffffff'],
      [OPS.fill, [rect(5)], [0, 0, 5, 5]],
    ]);
    expect(paint.fills).toEqual([
      { left: 10, right: 30, top: 70, bottom: 90,
        color: '#e3ecf0', alpha: 1, curves: 0 },
      { left: 0, right: 5, top: 95, bottom: 100,
        color: '#ffffff', alpha: 1, curves: 0 },
    ]);
  });

test('unsupported PDF paint fails with a fixed diagnostic', async () => {
  const page = { getViewport: () => ({
    width: 100, height: 100, transform: [1, 0, 0, -1, 0, 100],
  }) } as unknown as PDFPageProxy;
  for (const [op, args] of [
    [OPS.shadingFill, []],
    [OPS.paintImageMaskXObject, []],
    [OPS.constructPath, [OPS.fill, [[99]], [0, 0, 1, 1]]],
    [OPS.setGState, [[['BM', 'multiply']]]],
  ] as const) {
    await expect(pdfPaintOperators(page, [op], [args]))
      .rejects.toThrow(UNSUPPORTED_PDF_PAINT);
  }
});

test('PDF glyph measurements detect margin and line violations', () => {
  const textPage: PdfTextPage = { width: 100, height: 100, text: 'A B',
    items: [
      { text: 'A', x: 10, y: 80, width: 20, height: 10,
        bounds: { left: 10, right: 30, top: 10, bottom: 20 } },
      { text: 'B', x: 10, y: 68, width: 20, height: 10,
        bounds: { left: 10, right: 30, top: 22, bottom: 32 } },
    ] };
  expect(pdfGlyphGeometry(textPage, { x: 10, y: 10 }))
    .toEqual({ contained: true, separated: true });
  expect(pdfGlyphGeometry(textPage, { x: 12, y: 10 }).contained).toBe(false);
  const overlap = { ...textPage, items: [textPage.items[0]!, {
    ...textPage.items[1]!,
    bounds: { left: 10, right: 30, top: 18, bottom: 28 },
  }] };
  expect(pdfGlyphGeometry(overlap, { x: 10, y: 10 }).separated).toBe(false);
  expect(pdfParagraphGeometry(textPage, [{ text: 'A B', pitch: 12 }]))
    .toBe(true);
  expect(pdfParagraphGeometry(textPage, [{ text: 'A B', pitch: 14 }]))
    .toBe(false);
});

test('PDF membership recognizes markers split after hyphens', () => {
  const item = (text: string, y: number) => ({
    text, x: 10, y, width: 40, height: 10,
  });
  const pages: PdfTextPage[] = [{
    width: 100,
    height: 100,
    text: 'SIDEBAR- START SIDEBAR- ENTRY- 01 SIDEBAR- END MAIN- SHORT',
    items: [
      item('SIDEBAR-', 90), item('START', 89),
      item('SIDEBAR-', 80), item('ENTRY-', 79), item('01', 78),
      item('SIDEBAR-', 70), item('END', 69),
      item('MAIN-', 60), item('SHORT', 59),
    ],
  }];
  expect(pdfEntryIds(pages, 'SIDEBAR')).toEqual(['SIDEBAR-ENTRY-01']);
  expect(pdfMarkerPosition(pages[0]!, 'SIDEBAR-ENTRY-01'))
    .toEqual(pages[0]!.items[2]);
  expect(normalizePdfMarkers(pages[0]!.text)).toBe(
    'SIDEBAR-START SIDEBAR-ENTRY-01 SIDEBAR-END MAIN-SHORT',
  );
  expect(normalizePdfMarkers(
    'MAIN- START MAIN- ENTRY- 09 MAIN- END SIDEBAR- SHORT',
  )).toBe('MAIN-START MAIN-ENTRY-09 MAIN-END SIDEBAR-SHORT');
  expect(pdfMembership(pages, CASES[0]!).pages[0]).toEqual({
    main: { first: null, last: null },
    sidebar: { first: 'SIDEBAR-ENTRY-01', last: 'SIDEBAR-ENTRY-01' },
    start: true,
    end: true,
    opposite: true,
  });
});

test('PDF membership requires approval when CI is unset', () => {
  const membership: PdfMembership = { pageCount: 1, pages: [{
    main: { first: 'MAIN-ENTRY-01', last: 'MAIN-ENTRY-01' },
    sidebar: { first: null, last: null },
    start: true, end: true, opposite: true,
  }] };
  const key = 'print-main-overflow-en-a4-light';
  const ci = process.env.CI;
  delete process.env.CI;
  try {
    expect(() => checkMembership(membership, key, {})).toThrow(new Error(
      `PDF membership has not been approved: ${key} `
      + JSON.stringify(membership),
    ));
  } finally {
    if (ci === undefined) delete process.env.CI;
    else process.env.CI = ci;
  }
});

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
            const width = paper === 'a4' ? 210 * 96 / 25.4 : 816;
            await page.setViewportSize({
              width: Math.round(width - 28 * 96 / 25.4), height: 1123,
            });
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
              const key
                = `${printCase.fixture}-${lng}-${paper}-${scheme}` as const;
              const membership = await attachMembership(
                pages, printCase, testInfo,
              );
              expect.soft(() => checkMembership(membership, key)).not.toThrow();
              await expect(page.locator('.resume-document')).toHaveCSS(
                'background-color', 'rgb(255, 255, 255)',
              );
              await expect(page.locator('.resume-sidebar')).toHaveCSS(
                'background-color', 'rgb(227, 236, 240)',
              );
              expect(await photoCropGeometry(page))
                .toEqual(FIXTURE_PHOTO_CROP_GEOMETRY);
              const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
              const layout = await measurePdfLayout(page, margins);
              expect(Math.abs(layout.width - (width * 0.75 - 2 * margins.x)))
                .toBeLessThanOrEqual(1);
              const photo = await fixturePhotoIdentity();
              expect(document.numPages).toBe(EXPECTED_PAGES);
              const dimensions = paper === 'a4' ? [595.28, 841.89] : [612, 792];
              for (const pdfPage of pages) {
                expect(Math.abs(pdfPage.width - dimensions[0]!))
                  .toBeLessThanOrEqual(1);
                expect(Math.abs(pdfPage.height - dimensions[1]!))
                  .toBeLessThanOrEqual(1);
                expect(pdfPage.items.length).toBeGreaterThan(0);
                expect(pdfGlyphGeometry(pdfPage, margins))
                  .toEqual({ contained: true, separated: true });
              }
              for (let index = 0; index < pages.length; index += 1) {
                const textPage = pages[index]!;
                const paint = await pdfPaintPage(await document.getPage(
                  index + 1,
                ));
                expect(pdfColumnsMatch(textPage, layout, margins, index === 0))
                  .toBe(true);
                expect(pdfSidebarPaint(paint, textPage, layout.sidebar!,
                  margins.y, index === 0, '#e3ecf0')).toBe(true);
                expect(paint.images).toHaveLength(index === 0 ? 1 : 0);
                if (index === 0) {
                  expect(pdfImageMatches(paint.images[0], photo, layout.photo!),
                    pdfImageDiagnostic(paint.images[0], photo, layout.photo!))
                    .toBe(true);
                }
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
              const text = pages.map((pdfPage) => pdfPage.text).join(' ');
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

function normalizeHeading(text: string | null): string {
  return (text ?? '').normalize('NFKC').replace(/\s+/g, '').toLowerCase();
}

for (const template of ['modern-sidebar', 'engineer-compact'] as const) {
  for (const lng of ['en', 'vi'] as const) {
    for (const mode of ['bar', 'dots', 'tag', 'text'] as const) {
      test(`PDF ${template} ${lng} ${mode} maps levels zero to five`, async ({
        page,
      }) => {
        const external = await denyExternalRequests(page);
        await page.emulateMedia({ media: 'print', colorScheme: 'dark' });
        const margin = template === 'modern-sidebar' ? [14, 13] : [11, 12];
        const margins = { x: margin[0]! * 72 / 25.4,
          y: margin[1]! * 72 / 25.4 };
        await page.setViewportSize({
          width: Math.round((210 - 2 * margin[0]!) * 96 / 25.4), height: 1123,
        });
        const url = `/_harness/render?fixture=print-probe-widgets-${mode}`
          + `&mode=continuous&lng=${lng}&template=${template}&paper=a4`;
        expect((await page.goto(url))?.ok()).toBe(true);
        await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
        const layout = await measurePdfLayout(page, margins);
        const pages = await printedDisplayPages(page);
        expect(pages).toHaveLength(1);
        expect(layout.widgets).toHaveLength(14);
        for (const widget of layout.widgets) {
          const level = widget.name.endsWith('NONE')
            ? undefined
            : Number(widget.name.at(-1));
          const modern = template === 'modern-sidebar';
          const sidebar = widget.name.startsWith('LANGUAGE');
          expect(pdfWidgetMatches(pages[0]!, widget, level, mode, {
            solid: modern ? '#0f6b7d' : '#0d5a73',
            track: modern
              ? (sidebar ? '#b9d2d9' : '#cfe1e5')
              : '#cfdee3',
            onAccent: '#ffffff',
            fontSize: (modern ? 14 : 13) * 0.9,
          }), pdfWidgetDiagnostic(pages[0]!, widget)).toBe(true);
        }
        expect(pdfGlyphGeometry(pages[0]!.text, margins))
          .toEqual({ contained: true, separated: true });
        expect(external).toEqual([]);
      });
    }
  }
}

test('a section heading stays with the entry and its first body fragment',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto(`${PROBE_ROUTE}heading`))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await expect(page.locator('.section-heading')).toHaveCSS(
      'break-after', 'avoid',
    );
    const pages = await printedTextPages(page);
    expect(pages).toHaveLength(2);
    expect(pages[0]!.text).toContain('BOUNDARY-ANCHOR');
    expect(pages[0]!.text).not.toContain('KEEP-');
    for (const marker of ['KEEP-HEADING', 'KEEP-TITLE', 'KEEP-FIRST']) {
      expect(pages[1]!.text).toContain(marker);
    }
    expect(pdfParagraphGeometry(pages[1]!, [{
      text: 'KEEP-FIRST KEEP-SECOND', pitch: 18,
    }])).toBe(true);
    expect(external).toEqual([]);
  });

test('header-only entries break within both columns without blank pages',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto(`${PROBE_ROUTE}header-only`))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await expect(page.locator('.entry-body')).toHaveCount(0);
    expect(await page.locator('.entry-header').evaluateAll((headers) =>
      headers.length === 112 && headers.every((header) =>
        header === header.parentElement?.lastElementChild
        && getComputedStyle(header).breakAfter === 'auto'),
    ),
    ).toBe(true);
    const pages = await printedTextPages(page);
    expect(pages).toHaveLength(2);
    for (const column of ['SKILL', 'LANGUAGE']) {
      const ids = pages.flatMap((part) => [...part.text.matchAll(
        new RegExp(`${column}-\\d{2}`, 'gu'),
      )].map((match) => match[0]));
      expect(ids).toEqual(Array.from({ length: 56 }, (_, index) =>
        `${column}-${String(index + 1).padStart(2, '0')}`));
      const positions = pages.map((part) => part.items.find((item) =>
        item.text.includes(`${column}-`)));
      expect(positions.every(Boolean)).toBe(true);
      expect(Math.abs(positions[0]!.x - positions[1]!.x))
        .toBeLessThanOrEqual(1);
    }
    expect(external).toEqual([]);
  });

for (const rule of ['orphan', 'widow'] as const) {
  test(`the ${rule} rule moves a two-line paragraph`, async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    expect((await page.goto(`${PROBE_ROUTE}${rule}`))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const paragraph = page.locator('.entry-body p');
    await expect(paragraph).toHaveCSS('orphans', '2');
    await expect(paragraph).toHaveCSS('widows', '2');
    // Natural wrapping exercises line constraints rather than a forced break.
    await paragraph.evaluate((element, selected) => {
      element.textContent = `${selected.toUpperCase()}-FIRST `
        + `${selected.toUpperCase()}-SECOND`;
      (element as HTMLElement).style.width = '12em';
    }, rule);
    const lines = await paragraph.evaluate((element) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      return [...range.getClientRects()].map((rect) => rect.top);
    });
    expect(new Set(lines).size).toBe(2);
    // Relax the other constraint to prove each rule independently.
    await paragraph.evaluate((element, selected) => {
      const other = selected === 'orphan' ? 'widows' : 'orphans';
      (element as HTMLElement).style[other] = '1';
    }, rule);
    const pages = await printedTextPages(page);
    expect(pages).toHaveLength(2);
    expect(pages[0]!.text).not.toContain(`${rule.toUpperCase()}-FIRST`);
    expect(pages[1]!.text).toContain(`${rule.toUpperCase()}-FIRST`);
    expect(pages[1]!.text).toContain(`${rule.toUpperCase()}-SECOND`);
    expect(pdfParagraphGeometry(pages[1]!, [{
      text: `${rule.toUpperCase()}-FIRST ${rule.toUpperCase()}-SECOND`,
      pitch: 18,
    }])).toBe(true);
    await paragraph.evaluate((element) => {
      Object.assign((element as HTMLElement).style, {
        orphans: '1', widows: '1',
      });
    });
    const split = await printedTextPages(page);
    expect(split).toHaveLength(2);
    expect(split[0]!.text).toContain(`${rule.toUpperCase()}-FIRST`);
    expect(split[1]!.text).toContain(`${rule.toUpperCase()}-SECOND`);
    expect(external).toEqual([]);
  });
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
