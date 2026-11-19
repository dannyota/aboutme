import { expect, test, type Page } from '@playwright/test';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';

import {
  applyStyleMutation, mutateAttribute, mutationTest, proveMutation,
} from './style-mutation';
import {
  BAND, BAR, CLIPPING, darkDesk, DESK, expectContinuous, expectedScale,
  expectNoClipping, expectPageIndex, expectPager, expectPagesAria,
  expectPagesPaint, expectPagesState, expectPhoneSheets, expectPrint,
  expectSheetScale, expectSnapStops, expectStaging, expectWideSheets,
  FALLBACK, FOREGROUND, holdPaginationFonts, type Lng, openPublicPages,
  type Owner, PAGE, PAGE_INDEX, pageOneBlocks, PAGER, PAGES_ARIA, PAGES_COPY,
  PAPER, PHONE_SHEETS, PRINT, PUBLIC_PAINT, type PaintTemplate, SCROLLER,
  SHADOW, SHEET, SHEET_SCALE, SNAP, STAGING, SWAP, type Viewer, WIDE_SHEETS,
} from './public-pages';

const PAIRS = [
  ['vi', 390], ['en', 768], ['vi', 1440], ['en', 390], ['vi', 768],
  ['en', 1440],
] as const;
const CONTINUOUS = '.public-resume-page > .resume-document';
const NAMES = { vi: 'Nguyễn Thị Minh Châu', en: 'Ada Lovelace' } as const;
const PRINT_COUNT = 'Public print page count.';

async function printedPages(page: Page): Promise<number> {
  const pdf = await page.pdf({
    displayHeaderFooter: false,
    margin: { bottom: 0, left: 0, right: 0, top: 0 },
    preferCSSPageSize: true,
    printBackground: true,
    scale: 1,
  });
  const task = getDocument({
    data: new Uint8Array(pdf),
    isImageDecoderSupported: false,
    isOffscreenCanvasSupported: false,
    useSystemFonts: false,
  });
  try {
    return (await task.promise).numPages;
  } finally {
    await task.destroy();
  }
}

async function pdfPages(page: Page): Promise<number> {
  const response = await page.goto('/_harness/render?fixture=vn-full'
    + '&template=classic-serif&mode=continuous&print=1');
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  return printedPages(page);
}

async function expectPrintedPages(page: Page, expected: number) {
  expect(await printedPages(page), PRINT_COUNT).toBe(expected);
}

async function openPhone(page: Page, lng: Lng = 'vi'): Promise<number> {
  await openPublicPages(page, {
    template: 'classic-serif', lng, width: 390, height: 844,
  });
  const count = await page.locator(PAGE).count();
  expect(count, PHONE_SHEETS).toBeGreaterThan(1);
  return count;
}

async function openWide(page: Page, width = 1440): Promise<void> {
  await openPublicPages(page, {
    template: 'classic-serif', lng: 'en', width,
  });
}

async function pauseClock(page: Page): Promise<void> {
  await page.clock.install({ time: new Date('2026-01-01T00:00:00Z') });
  await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'));
}

async function openStaged(
  page: Page, width = 1440, height = 900,
): Promise<void> {
  await pauseClock(page);
  await openPublicPages(page, {
    template: 'classic-serif', lng: 'en', width, height, settle: false,
  });
  await expect(page.locator(
    '.public-pages[data-state="staging"] .paged-resume'
    + '[data-pagination-settled="true"]',
  ), STAGING).toHaveCount(1, { timeout: 15_000 });
}

async function openTimedOut(page: Page): Promise<void> {
  await holdPaginationFonts(page);
  await pauseClock(page);
  await openPublicPages(page, {
    template: 'classic-serif', lng: 'en', width: 1440, settle: false,
  });
  await expectPagesState(page, 'staging');
  await expectStaging(page);
  await page.clock.runFor(3_000);
  await expectPagesState(page, 'continuous');
  await page.evaluate(() => {
    (window as unknown as { releasePaginationFonts: () => void })
      .releasePaginationFonts();
  });
  await page.clock.runFor(1_000);
}

export function publicPagesTests(): void {
  test.describe('public pages', () => {
    for (const width of [1440, 768, 700]) {
      test(`wide sheets stack centered at ${width}`, async ({ page }) => {
        await openWide(page, width);
        await expectWideSheets(page);
        await expectSheetScale(page);
        await expectNoClipping(page);
        await expectPagesAria(page, 'en', false);
        await expect(page.locator('.public-pager'), PAGER).toBeHidden();
        if (width < 826) {
          expect(expectedScale(width), SHEET_SCALE).toBeLessThan(1);
        }
      });
    }

    for (const lng of ['vi', 'en'] as const) {
      test.describe(`phone ${lng}`, () => {
        test.use({ locale: lng === 'vi' ? 'vi-VN' : 'en-US' });
        test('one sheet per screen with snap stops and a pager', async ({
          page,
        }) => {
          const count = await openPhone(page, lng);
          await expectPhoneSheets(page);
          await expectSheetScale(page);
          await expectPagesAria(page, lng, true);
          await expectPager(page, lng, 0, count);
          await expectSnapStops(page);
          await page.getByRole('button', { name: PAGES_COPY[lng].next })
            .click();
          await expectPageIndex(page, lng, 1, count);
          await expectPager(page, lng, 1, count);
          await page.locator(SCROLLER).focus();
          await page.keyboard.press('ArrowLeft');
          await expectPageIndex(page, lng, 0, count);
          await page.keyboard.press('ArrowRight');
          await expectPageIndex(page, lng, 1, count);
          expect(await page.evaluate(() => scrollX), PHONE_SHEETS).toBe(0);
          await expectPhoneSheets(page);
        });
      });
    }

    test('one-page resume shows no pager', async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      const response = await page.goto(
        '/_harness/render?fixture=sample-one-page-tight-en'
        + '&template=one-page-tight&mode=public',
      );
      expect(response?.ok()).toBe(true);
      await expectPagesState(page, 'shown');
      await expect(page.locator(PAGE), PAGER).toHaveCount(1);
      await expect(page.locator('.public-pager'), PAGER).toHaveCount(0);
    });

    test('page 1 does not move at the swap on a wide screen', async ({
      page,
    }) => {
      await openStaged(page);
      await expectStaging(page);
      const before = await pageOneBlocks(page, CONTINUOUS);
      await page.clock.runFor(100);
      await expectPagesState(page, 'shown');
      const after = await pageOneBlocks(page,
        `${PAGE}[data-page-index="0"] .resume-page`);
      expect(Object.keys(after).length, SWAP).toBeGreaterThan(4);
      const moved = Object.entries(after).filter(([key, box]) => {
        const old = before[key];
        return old === undefined
          || box.some((value, index) => Math.abs(value - old[index]!) > 1);
      }).map(([key]) => key);
      expect(moved, SWAP).toEqual([]);
    });

    test('a reader past the first page keeps the continuous article',
      async ({ page }) => {
        await openStaged(page, 1440, 400);
        const past = await page.locator(CONTINUOUS).evaluate((article) => {
          window.scrollTo(0, document.documentElement.scrollHeight);
          return window.scrollY
            - (article.getBoundingClientRect().top + window.scrollY + 1_123);
        });
        expect(past, FALLBACK).toBeGreaterThan(0);
        await page.clock.runFor(100);
        await expectPagesState(page, 'continuous');
        await expectContinuous(page, NAMES.en);
      });

    test('a timeout keeps the continuous article with no late swap',
      async ({ page }) => {
        await openTimedOut(page);
        await expectContinuous(page, NAMES.en);
        await expectPagesState(page, 'continuous');
      });

    test('print shows the continuous article without the desk', async ({
      page,
    }) => {
      await openWide(page);
      await expectPrint(page);
    });

    test('printed page count equals the PDF', async ({ page }) => {
      test.setTimeout(60_000);
      const expected = await pdfPages(page);
      expect(expected).toBeGreaterThan(1);
      await openPublicPages(page, {
        template: 'classic-serif', lng: 'vi', width: 1440,
      });
      await expectPrintedPages(page, expected);
    });

    const ownerSchemes = ['light', 'dark', 'system'] as const;
    for (const [templateIndex, template] of
      (Object.keys(PUBLIC_PAINT) as PaintTemplate[]).entries()) {
      for (const [ownerIndex, owner] of ownerSchemes.entries()) {
        for (const [viewerIndex, viewer] of
          (['light', 'dark'] as const).entries()) {
          const [lng, width]
            = PAIRS[(templateIndex + ownerIndex * 2 + viewerIndex) % 6]!;
          test.describe(`public colors ${lng}`, () => {
            test.use({ locale: lng === 'vi' ? 'vi-VN' : 'en-US' });
            test(`${template} ${owner} ${viewer} ${width}`, async ({
              page,
            }) => {
              const external = await openPublicPages(page, {
                template, lng, width, owner, viewer,
              });
              await expectPagesPaint(page, template,
                darkDesk(owner as Owner, viewer as Viewer));
              if (width < 600) await expectPhoneSheets(page);
              else await expectWideSheets(page);
              await expectSheetScale(page);
              await expect(page.locator(`${PAGE} .resume-name`).first())
                .toHaveText(NAMES[lng]);
              expect(external).toEqual([]);
            });
          });
        }
      }
    }
  });

  mutationTest('public wide sheets reject a shifted sheet', async ({
    page,
  }) => {
    await openWide(page);
    await proveMutation(() => expectWideSheets(page),
      () => applyStyleMutation(page.locator(PAGE).nth(1),
        { 'margin-top': '4px' }), WIDE_SHEETS);
  });

  mutationTest('public wide sheets reject a bar row off the page width',
    async ({ page }) => {
      await openWide(page, 768);
      await proveMutation(() => expectWideSheets(page),
        () => applyStyleMutation(page.locator('.public-toolbar-inner'),
          { width: '600px' }), WIDE_SHEETS);
    });

  mutationTest('public sheets reject a scale above 1', async ({ page }) => {
    await openWide(page, 768);
    await proveMutation(() => expectSheetScale(page),
      () => applyStyleMutation(page.locator('.public-page-scale').first(),
        { transform: ['scale(1.2)', /matrix\(1\.2/u] }), SHEET_SCALE);
  });

  mutationTest('public sheets reject clipped overflow', async ({ page }) => {
    await openWide(page);
    await proveMutation(() => expectNoClipping(page),
      () => applyStyleMutation(page.locator(PAGE).first().locator(
        '.public-page-sheet'), { overflow: 'hidden', height: '300px' }),
      CLIPPING);
  });

  mutationTest('public phone rejects missing snap', async ({ page }) => {
    await openPhone(page);
    await proveMutation(() => expectSnapStops(page),
      () => applyStyleMutation(page.locator(SCROLLER),
        { 'scroll-snap-type': 'none' }), SNAP);
  });

  mutationTest('public phone rejects a page wider than the screen', async ({
    page,
  }) => {
    await openPhone(page);
    await proveMutation(() => expectPhoneSheets(page),
      () => applyStyleMutation(page.locator(PAGE).first(),
        { 'flex-basis': '120%' }), PHONE_SHEETS);
  });

  mutationTest('public phone rejects a broken pager', async ({ page }) => {
    const count = await openPhone(page);
    await proveMutation(() => expectPager(page, 'vi', 0, count),
      () => mutateAttribute(
        page.getByRole('button', { name: PAGES_COPY.vi.next }),
        'disabled', ''), PAGER);
  });

  mutationTest('public phone rejects a stale page index', async ({ page }) => {
    const count = await openPhone(page);
    await page.getByRole('button', { name: PAGES_COPY.vi.next }).click();
    await proveMutation(() => expectPageIndex(page, 'vi', 1, count),
      async () => {
        await page.locator(SCROLLER).evaluate((node) => {
          node.scrollTo({ left: 0, behavior: 'instant' });
        });
        await expect(page.locator('.public-pager-position'))
          .toHaveText(`1 / ${count}`);
        return async () => {
          await page.getByRole('button', { name: PAGES_COPY.vi.next })
            .click();
        };
      }, PAGE_INDEX);
  });

  mutationTest('public pages reject an unnamed page', async ({ page }) => {
    await openPhone(page);
    await proveMutation(() => expectPagesAria(page, 'vi', true),
      () => mutateAttribute(page.locator(PAGE).first(), 'aria-label',
        'Trang'), PAGES_ARIA);
  });

  mutationTest('public staging rejects exposed pages', async ({ page }) => {
    await openStaged(page);
    await proveMutation(() => expectStaging(page),
      () => mutateAttribute(page.locator('.public-pages'), 'aria-hidden',
        'false'), STAGING);
  });

  mutationTest('public swap rejects a moved first page', async ({ page }) => {
    await openStaged(page);
    const before = await pageOneBlocks(page, CONTINUOUS);
    await page.clock.runFor(100);
    await expectPagesState(page, 'shown');
    const first = `${PAGE}[data-page-index="0"] .resume-page`;
    const assertion = async () => {
      const after = await pageOneBlocks(page, first);
      const moved = Object.entries(after).filter(([key, box]) =>
        before[key] === undefined || box.some((value, index) =>
          Math.abs(value - before[key]![index]!) > 1));
      expect(moved, SWAP).toEqual([]);
    };
    await proveMutation(assertion,
      () => applyStyleMutation(page.locator(first),
        { 'margin-top': '4px' }), SWAP);
  });

  mutationTest('public fallback rejects a late swap after a timeout',
    async ({ page }) => {
      await openTimedOut(page);
      await proveMutation(() => expectContinuous(page, NAMES.en),
        () => mutateAttribute(page.locator('.public-resume-page'),
          'data-pages', 'shown'), FALLBACK);
    });

  mutationTest('public print rejects a changed page count', async ({
    page,
  }) => {
    test.setTimeout(60_000);
    const expected = await pdfPages(page);
    await openPublicPages(page, {
      template: 'classic-serif', lng: 'vi', width: 1440,
    });
    await proveMutation(() => expectPrintedPages(page, expected),
      () => applyStyleMutation(page.locator(CONTINUOUS),
        { 'padding-top': ['300mm', /px$/u] }), PRINT_COUNT);
  });

  mutationTest('public print rejects visible pages', async ({ page }) => {
    await openWide(page);
    await proveMutation(() => expectPrint(page),
      () => applyStyleMutation(page.locator(PAGE).first(),
        { display: 'flex' }), PRINT);
  });

  for (const [name, selector, styles, message] of [
    ['sheet paper', `${PAGE} .resume-page`,
      { 'background-color': 'rgb(255, 0, 0)' }, PAPER],
    ['dark pages', `${PAGE} .resume-page`,
      { 'background-color': 'rgb(20, 18, 16)' }, PAPER],
    ['header band', `${PAGE} .resume-header`,
      { 'background-color': 'rgb(255, 0, 0)' }, BAND],
    ['foreground', `${PAGE} .resume-page`, { color: 'rgb(255, 0, 0)' },
      FOREGROUND],
    ['bar paint', '.public-toolbar', { 'background-color': 'rgb(255, 0, 0)' },
      BAR],
    ['desk', 'html', { 'background-color': 'rgb(255, 0, 0)' }, DESK],
    ['sheet shadow', SHEET, { 'box-shadow': 'none' }, SHADOW],
  ] as const) {
    mutationTest(`public paint rejects changed ${name}`, async ({ page }) => {
      await openPublicPages(page, {
        template: 'executive-band', lng: 'vi', width: 1440, owner: 'dark',
      });
      await proveMutation(() => expectPagesPaint(page, 'executive-band', true),
        () => applyStyleMutation(page.locator(selector).first(), styles),
        message);
    });
  }
}
