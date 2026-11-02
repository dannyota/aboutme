import { expect, test } from '@playwright/test';

import { measurePdfLayout } from './display-assertions';
import {
  expectedEntryIds,
  EXPECTED_PAGES,
  pdfEntryIds,
  pdfColumnsMatch,
  pdfMembership,
  pdfSidebarPaint,
  pdfWidgetMatches,
  printedDisplayPages,
  printedTextPages,
  PRINT_CASES as CASES,
} from './pdf-display';
import { fixturePhotoIdentity, pdfBoxMatches } from './pdf-paint';
import { denyExternalRequests, waitForImages } from './support';

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

test('PDF membership detects forced break', async ({ page }) => {
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
  await entries.nth(3).evaluate((entry) => {
    (entry as HTMLElement).style.breakBefore = 'page';
  });
  const altered = pdfMembership(await printedTextPages(page), printCase);
  expect(altered).not.toEqual(baseline);
  expect(external).toEqual([]);
});

test('PDF membership detects unbreakable entries', async ({ page }) => {
  const external = await denyExternalRequests(page);
  await page.emulateMedia({ media: 'print' });
  expect((await page.goto('/_harness/render?fixture=print-probe-breakable-entry'
    + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4'))?.ok())
    .toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const entries = page.locator('.layout-one-column .entry');
  await expect(entries).toHaveCount(2);
  await expect(entries.first()).toHaveCSS('break-inside', 'auto');
  const baselinePages = await printedTextPages(page);
  expect(baselinePages).toHaveLength(2);
  expect(baselinePages[0]!.text).toContain('MAIN-ENTRY-01');
  expect(baselinePages[0]!.text).not.toContain('BREAK-LAST');
  expect(baselinePages[1]!.text).toContain('BREAK-LAST');
  expect(baselinePages[1]!.text).toContain('MAIN-ENTRY-02');
  expect(pdfEntryIds(baselinePages, 'MAIN'))
    .toEqual(['MAIN-ENTRY-01', 'MAIN-ENTRY-02']);
  const baseline = pdfMembership(baselinePages, CASES[1]!);
  await page.addStyleTag({
    content: '.entry { break-inside: avoid !important; }',
  });
  await expect(entries.first()).toHaveCSS('break-inside', 'avoid');
  const alteredPages = await printedTextPages(page);
  expect(alteredPages).toHaveLength(2);
  expect(alteredPages[0]!.text).toContain('BOUNDARY-ANCHOR');
  expect(alteredPages[0]!.text).not.toContain('MAIN-ENTRY-01');
  expect(alteredPages[1]!.text).toContain('MAIN-ENTRY-01');
  expect(alteredPages[1]!.text).toContain('BREAK-LAST');
  expect(pdfEntryIds(alteredPages, 'MAIN'))
    .toEqual(['MAIN-ENTRY-01', 'MAIN-ENTRY-02']);
  const altered = pdfMembership(alteredPages, CASES[1]!);
  expect(altered).not.toEqual(baseline);
  expect(altered.pages[0]!.main.first).toBeNull();
  expect(altered.pages[1]!.main.first).toBe('MAIN-ENTRY-01');
  expect(external).toEqual([]);
});

const PAINT_MUTATIONS = [
  'sidebar position', 'continuation tint', 'photo',
] as const;
for (const mutation of PAINT_MUTATIONS) {
  test(`PDF measurements detect a changed ${mutation}`, async ({ page }) => {
    test.setTimeout(30_000);
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    expect((await page.goto('/_harness/render?fixture=print-sidebar-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4'))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const layout = await measurePdfLayout(page, margins);
    const baseline = await printedDisplayPages(page);
    expect(baseline).toHaveLength(2);
    const columns = (pages: typeof baseline) => pdfColumnsMatch(
      pages[1]!.text, layout, margins, false,
    );
    const tint = (pages: typeof baseline, index: number) => pdfSidebarPaint(
      pages[index]!.paint, pages[index]!.text, layout.sidebar!,
      margins.y, index === 0, '#e3ecf0',
    );
    const identity = await fixturePhotoIdentity();
    const photo = (pages: typeof baseline) => {
      const images = pages[0]!.paint.images;
      return images.length === 1 && images[0]!.rgbaHash === identity.rgbaHash
        && pdfBoxMatches(images[0]!, layout.photo!);
    };
    expect(columns(baseline)).toBe(true);
    expect(tint(baseline, 0)).toBe(true);
    expect(tint(baseline, 1)).toBe(true);
    expect(photo(baseline)).toBe(true);
    if (mutation === 'sidebar position') {
      await page.addStyleTag({ content: '.resume-sidebar '
        + '.entry:nth-of-type(n+12)'
        + ' { transform: translateX(-300px) !important; }' });
    } else if (mutation === 'continuation tint') {
      const firstHeight = (baseline[0]!.text.height - margins.y
        - layout.sidebar!.top) / 0.75;
      await page.addStyleTag({ content: '.resume-sidebar {'
        + ' background: transparent !important; position: relative; }'
        + '.resume-sidebar::before { content: ""; position: absolute;'
        + ` top: 0; left: 0; width: 100%; height: ${firstHeight}px;`
        + ' background: #e3ecf0; }'
        + '.resume-sidebar .resume-section { position: relative; }' });
    } else {
      await page.locator('.resume-photo img').evaluate((image) => {
        (image as HTMLElement).style.visibility = 'hidden';
      });
    }
    const altered = await printedDisplayPages(page);
    expect(altered).toHaveLength(2);
    if (mutation === 'sidebar position') expect(columns(altered)).toBe(false);
    else if (mutation === 'continuation tint') {
      expect(tint(altered, 0)).toBe(true);
      expect(tint(altered, 1)).toBe(false);
    } else expect(photo(altered)).toBe(false);
    expect(external).toEqual([]);
  });
}

for (const mode of ['bar', 'dots', 'tag'] as const) {
  test(`PDF ${mode} detects a level-four mutation`, async ({ page }) => {
    test.setTimeout(30_000);
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    expect((await page.goto('/_harness/render?template=modern-sidebar'
      + `&mode=continuous&fixture=print-probe-widgets-${mode}`))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const layout = await measurePdfLayout(page, margins);
    const widget = layout.widgets.find(({ name }) => name === 'SKILL-4');
    expect(widget).toBeDefined();
    const baseline = await printedDisplayPages(page);
    expect(baseline).toHaveLength(1);
    const correct = (pages: typeof baseline) => pdfWidgetMatches(
      pages[0]!, widget!, 4, mode, {
        solid: '#0f6b7d', track: '#cfe1e5', onAccent: '#ffffff',
        fontSize: 14 * 0.9,
      },
    );
    expect(correct(baseline)).toBe(true);
    if (mode === 'bar') {
      await page.addStyleTag({
        content: '.level-fill { width: 20% !important; }',
      });
    } else if (mode === 'dots') {
      await page.addStyleTag({ content: '.level-dot {'
        + ' background: #0f6b7d !important; }' });
    } else {
      await page.getByRole('img', { name: 'SKILL-4: 4 of 5', exact: true })
        .locator('span').evaluate((label) => { label.textContent = '3/5'; });
    }
    const altered = await printedDisplayPages(page);
    expect(altered).toHaveLength(1);
    expect(correct(altered)).toBe(false);
    expect(external).toEqual([]);
  });
}
