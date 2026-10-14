import { expect, test } from '@playwright/test';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';

import {
  CHROME_PIXEL_TOLERANCE,
  verifyScreenshot,
  waitForImages,
} from './support';

const CASES = [
  { name: 'sheet', locale: 'vi', theme: 'dark', width: 390 },
  { name: 'sheet', locale: 'en', theme: 'light', width: 390 },
  { name: 'filtered', locale: 'vi', theme: 'dark', width: 390 },
  { name: 'filtered', locale: 'en', theme: 'light', width: 1440 },
  { name: 'rows', locale: 'vi', theme: 'light', width: 1024 },
] as const;
const LIVE_THUMBNAILS
  = TEMPLATES.length - new Set(SAMPLES.map(({ templateId }) => templateId)).size;

for (const { name, locale, theme, width } of CASES) {
  test(`Library ${name} ${locale} ${theme} ${width} matches baseline`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([
      { name: 'aboutme-locale', value: locale, url: 'http://127.0.0.1:20092' },
      { name: 'aboutme-theme', value: theme, url: 'http://127.0.0.1:20092' },
    ]);
    const path
      = name === 'filtered'
        ? '/templates?role=backend&filter=technical'
        : '/templates';
    const response = await page.goto(path);
    expect(response?.status()).toBe(200);
    await page.addStyleTag({
      content:
        '*, *::before, *::after { transition: none !important; '
        + 'animation: none !important; }',
    });
    await page.evaluate(() => document.fonts.ready);
    await page.locator('img[loading="lazy"]').evaluateAll((images) => {
      for (const image of images) (image as HTMLImageElement).loading = 'eager';
    });
    await waitForImages(page);
    if (name === 'sheet') {
      await page.locator('[data-action="gallery-filters-open"]').click();
      await expect(page.getByTestId('gallery-filter-sheet')).toBeVisible();
      await expect(
        page.locator('[data-action="gallery-filters-close"]'),
      ).toBeFocused();
    } else {
      const height = await page.evaluate(
        () => document.documentElement.scrollHeight,
      );
      await page.setViewportSize({ width, height });
      if (name === 'rows') {
        await expect(page.locator('[data-sheet-thumbnail-render]')).toHaveCount(
          LIVE_THUMBNAILS,
        );
      }
      const settled = await page.evaluate(
        () => document.documentElement.scrollHeight,
      );
      if (settled !== height) {
        await page.setViewportSize({ width, height: settled });
      }
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);
    }
    await page.evaluate(
      () =>
        new Promise<void>((done) => {
          requestAnimationFrame(() => requestAnimationFrame(() => done()));
        }),
    );
    expect(
      await page.evaluate(
        () =>
          document.documentElement.scrollWidth
          - document.documentElement.clientWidth,
      ),
    ).toBe(0);
    await verifyScreenshot(
      page,
      `gallery--${name}--${locale}--${theme}--${width}.png`,
      testInfo,
      undefined,
      CHROME_PIXEL_TOLERANCE,
      name !== 'sheet',
    );
  });
}
