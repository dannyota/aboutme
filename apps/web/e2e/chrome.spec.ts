import { expect, test } from '@playwright/test';

import { verifyScreenshot, waitForImages } from './support';

// Application chrome pixel baselines: the signed-out homepage, the login
// page, and the template gallery, at phone and desktop widths, in both
// themes (DESIGN.md; ADR 0050). Vietnamese is the default locale, so these
// baselines pin it rather than English (gallery.spec.ts uses the same
// cookie pattern).

const PAGES = [
  { name: 'home', path: '/', thumbnails: 4 },
  { name: 'login', path: '/login', thumbnails: 0 },
  { name: 'templates', path: '/templates', thumbnails: 20 },
] as const;
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;

for (const page of PAGES) {
  for (const theme of THEMES) {
    for (const width of WIDTHS) {
      const title = `chrome ${page.name} ${theme} ${width}px matches baseline`;
      test(title, async ({ page: browserPage }, testInfo) => {
        await browserPage.setViewportSize({ width, height: 900 });
        await browserPage.context().addCookies([
          {
            name: 'aboutme-locale',
            value: 'vi',
            url: 'http://127.0.0.1:20092',
          },
          {
            name: 'aboutme-theme',
            value: theme,
            url: 'http://127.0.0.1:20092',
          },
        ]);

        const response = await browserPage.goto(page.path);
        expect(response?.status()).toBe(200);
        await expect(browserPage.locator('[data-testid="app-shell"]'))
          .toBeVisible();
        await browserPage.evaluate(() => document.fonts.ready);
        await waitForImages(browserPage);

        const overflow = await browserPage.evaluate(() =>
          document.documentElement.scrollWidth
          - document.documentElement.clientWidth);
        expect(overflow).toBe(0);

        if (page.thumbnails > 0) {
          // Template thumbnails mount only near the viewport, so a full-page
          // capture of a short viewport shows empty sheets. Grow the viewport
          // to the whole page and wait for every thumbnail render.
          const height = await browserPage.evaluate(() =>
            document.documentElement.scrollHeight);
          await browserPage.setViewportSize({ width, height });
          await expect(
            browserPage.locator('[data-sheet-thumbnail-render]'),
          ).toHaveCount(page.thumbnails);
          await waitForImages(browserPage);
        }

        await verifyScreenshot(
          browserPage,
          `chrome--${page.name}--${theme}--${width}.png`,
          testInfo,
        );

        if (page.name === 'home') {
          const templates = browserPage.locator(
            '[data-testid="landing-templates"]',
          );
          await templates.scrollIntoViewIfNeeded();
          await expect(
            browserPage.locator('[data-sheet-thumbnail-render]'),
          ).toHaveCount(4);
          await waitForImages(browserPage);
          await verifyScreenshot(
            browserPage,
            `chrome--home-templates--${theme}--${width}.png`,
            testInfo,
            templates,
          );
        }
      });
    }
  }
}
