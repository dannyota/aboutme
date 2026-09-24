import { expect, test } from '@playwright/test';

import {
  CHROME_PIXEL_TOLERANCE,
  verifyScreenshot,
  waitForImages,
} from './support';

// The template gallery and template pages in the production build: no
// horizontal page scroll at phone or desktop width, the filter row scrolls on
// phones, a filter narrows the grid, the template page tabs switch views, and
// an unknown template is a 404 (DESIGN.md, template gallery).

for (const width of [390, 1440]) {
  test(`gallery at ${width} px fits the viewport`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([{
      name: 'aboutme-locale',
      value: 'vi',
      url: 'http://127.0.0.1:20092',
    }]);
    const response = await page.goto('/templates');
    expect(response?.status()).toBe(200);
    await expect(page.locator('[data-template]')).toHaveCount(20);
    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
      - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
    const filters = page.locator('[data-testid="template-gallery"] nav');
    const scrolls = await filters.evaluate((element) =>
      element.scrollWidth > element.clientWidth);
    expect(scrolls).toBe(width === 390);

    await page.goto('/templates?filter=ats');
    await expect(page.locator('[data-template]')).toHaveCount(7);
    await expect(page.locator('[data-filter="ats"]'))
      .toHaveAttribute('aria-current', 'page');
  });

  test(`gallery at ${width} px shows the 5 stored sample pages`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const response = await page.goto('/templates');
    expect(response?.status()).toBe(200);

    const images = page.locator('img[data-page-image]');
    await expect(images).toHaveCount(5);
    const count = await images.count();
    for (let index = 0; index < count; index += 1) {
      const image = images.nth(index);
      await expect(image).toHaveAttribute('alt', /.+/);
      await expect(image).toHaveAttribute(
        'loading',
        index < 2 ? 'eager' : 'lazy',
      );
      await expect(image).toHaveAttribute('width', /^\d+$/);
      await expect(image).toHaveAttribute('height', /^\d+$/);
      await image.scrollIntoViewIfNeeded();
      await expect(async () => {
        const naturalWidth = await image.evaluate((element) =>
          (element as HTMLImageElement).naturalWidth);
        expect(naturalWidth).toBeGreaterThan(0);
      }).toPass();
    }

    await expect(page.locator('[data-sheet-thumbnail]')).toHaveCount(15);
    await expect(page.locator('h1')).toHaveCount(1);
  });
}

test('gallery header at 360 px fits the viewport in English', async ({
  page,
}) => {
  // The narrowest supported phone width; the English header labels are the
  // longer ones, so English is the tighter fit.
  await page.setViewportSize({ width: 360, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates');
  expect(response?.status()).toBe(200);
  const overflow = await page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
  const header = await page.locator('[data-testid="app-shell"]').boundingBox();
  expect(header).not.toBeNull();
  if (header !== null) expect(header.x + header.width).toBeLessThanOrEqual(360);
});

test('template page switches between the page and the ATS text', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await expect(page.locator('.template-detail__info h1'))
    .toHaveText('Engineer Compact');
  await expect(page.locator('.resume-document')).toHaveCount(1);

  const tabs = page.getByRole('tab');
  await expect(tabs).toHaveText(['Page', 'PDF', 'What an ATS reads']);

  await page.getByRole('tab', { name: 'What an ATS reads' }).click();
  await expect(page.locator('[data-ats-text]')).toContainText('Khoa Vu');
  await expect(page.locator('[data-action="use-sample"]'))
    .toHaveAttribute('href', '/app/new?sample=engineer-compact&lng=en');
  await expect(page.locator('h1')).toHaveCount(1);
});

test('the executive band PDF tab shows both stored pages', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates/executive-band');
  expect(response?.status()).toBe(200);

  await page.getByRole('tab', { name: 'PDF' }).click();
  const images = page.locator('img[data-pdf-page]');
  await expect(images).toHaveCount(2);
  const count = await images.count();
  for (let index = 0; index < count; index += 1) {
    const image = images.nth(index);
    await expect(image).toHaveAttribute('alt', /.+/);
    await image.scrollIntoViewIfNeeded();
    await expect(async () => {
      const naturalWidth = await image.evaluate((element) =>
        (element as HTMLImageElement).naturalWidth);
      expect(naturalWidth).toBeGreaterThan(0);
    }).toPass();
  }
  await expect(page.locator('figcaption')).toHaveText([
    'Page 1 of 2',
    'Page 2 of 2',
  ]);
  await expect(page.locator('h1')).toHaveCount(1);
});

test('the engineer compact page renders server-side with one h1', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.request.get('/templates/engineer-compact');
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(html).toContain('Khoa Vu');
  expect(html.match(/<h1/g)).toHaveLength(1);
});

test('the ATS tab keeps dark ink on a white sheet in dark theme', async ({
  page,
}) => {
  await page.context().addCookies([
    { name: 'aboutme-locale', value: 'en', url: 'http://127.0.0.1:20092' },
    { name: 'aboutme-theme', value: 'dark', url: 'http://127.0.0.1:20092' },
  ]);
  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await page.getByRole('tab', { name: 'What an ATS reads' }).click();

  const panel = page.locator('[data-ats-text]');
  await expect(panel).toBeVisible();
  const colors = await panel.evaluate((element) => {
    const style = getComputedStyle(element);
    return { color: style.color, background: style.backgroundColor };
  });
  expect(colors.color).toBe('rgb(23, 26, 24)');
  expect(colors.background).toBe('rgb(255, 255, 255)');
});

test('an unknown template is not found', async ({ page }) => {
  const response = await page.goto('/templates/not-a-template');
  expect(response?.status()).toBe(404);
});

// Pixel baselines for the executive band PDF tab (two stored pages) and the
// ATS tab in dark theme (DESIGN.md, Library), at phone and desktop widths.
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`template PDF tab ${theme} ${width}px matches baseline`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      await page.context().addCookies([
        { name: 'aboutme-locale', value: 'vi', url: 'http://127.0.0.1:20092' },
        { name: 'aboutme-theme', value: theme, url: 'http://127.0.0.1:20092' },
      ]);

      const response = await page.goto('/templates/executive-band');
      expect(response?.status()).toBe(200);
      await page.addStyleTag({
        content: '*, *::before, *::after { transition: none !important; '
          + 'animation: none !important; }',
      });
      await page.getByRole('tab', { name: 'PDF' }).click();
      await expect(page.locator('img[data-pdf-page]')).toHaveCount(2);
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);

      const overflow = await page.evaluate(() =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth);
      expect(overflow).toBe(0);

      await verifyScreenshot(
        page,
        `template-pdf--${theme}--${width}.png`,
        testInfo,
        undefined,
        CHROME_PIXEL_TOLERANCE,
      );
    });
  }
}

for (const width of WIDTHS) {
  test(`template ATS tab dark ${width}px matches baseline`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([
      { name: 'aboutme-locale', value: 'vi', url: 'http://127.0.0.1:20092' },
      { name: 'aboutme-theme', value: 'dark', url: 'http://127.0.0.1:20092' },
    ]);

    const response = await page.goto('/templates/executive-band');
    expect(response?.status()).toBe(200);
    await page.addStyleTag({
      content: '*, *::before, *::after { transition: none !important; '
        + 'animation: none !important; }',
    });
    await page.getByRole('tab', { name: 'ATS đọc được gì' }).click();
    await expect(page.locator('[data-ats-text]')).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    await waitForImages(page);

    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
      - document.documentElement.clientWidth);
    expect(overflow).toBe(0);

    await verifyScreenshot(
      page,
      `template-ats--dark--${width}.png`,
      testInfo,
      undefined,
      CHROME_PIXEL_TOLERANCE,
    );
  });
}
