import { expect, test } from '@playwright/test';

import { verifyScreenshot, waitForImages } from './support';

// Application chrome pixel baselines: the signed-out homepage and the login
// page, at phone and desktop widths, in both themes (DESIGN.md; ADR 0050).
// Vietnamese is the default locale, so these baselines pin it rather than
// English (gallery.spec.ts uses the same cookie pattern).

const PAGES = [
  { name: 'home', path: '/' },
  { name: 'login', path: '/login' },
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

        await verifyScreenshot(
          browserPage,
          `chrome--${page.name}--${theme}--${width}.png`,
          testInfo,
        );
      });
    }
  }
}
