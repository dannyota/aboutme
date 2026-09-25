import {
  type Browser,
  chromium,
  expect,
  type Page,
  test,
  webkit,
} from '@playwright/test';

import {
  CHROME_PIXEL_TOLERANCE,
  verifyScreenshot,
  waitForImages,
} from './support';

// Application chrome pixel baselines: the signed-out homepage, the login
// page, and the template gallery, at phone and desktop widths, in both
// themes (DESIGN.md; ADR 0050). Vietnamese is the default locale, so these
// baselines pin it rather than English (gallery.spec.ts uses the same
// cookie pattern).

const PAGES = [
  { name: 'home', path: '/', thumbnails: 4 },
  { name: 'login', path: '/login', thumbnails: 0 },
  { name: 'templates', path: '/templates', thumbnails: 15 },
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
        // Chrome transitions (link and chip colors, hover lifts) must not
        // be mid-flight at capture; the exact-pixel compare has no
        // tolerance for a partly blended edge.
        await browserPage.addStyleTag({
          content: '*, *::before, *::after { transition: none !important; '
            + 'animation: none !important; }',
        });
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
          // Mounted thumbnails can change the page height; match the
          // viewport to the settled height so layout does not move again.
          const settled = await browserPage.evaluate(() =>
            document.documentElement.scrollHeight);
          if (settled !== height) {
            await browserPage.setViewportSize({ width, height: settled });
          }
          await browserPage.evaluate(() => document.fonts.ready);
          await waitForImages(browserPage);
          // Thumbnails measure their width after mounting; let two frames
          // pass so every zoom has settled before the capture.
          await browserPage.evaluate(() => new Promise<void>((done) => {
            requestAnimationFrame(() => requestAnimationFrame(() => done()));
          }));
          // Each embedded sheet's name renders as a p, not an h1, so the
          // page keeps exactly one h1 once every thumbnail has mounted.
          await expect(browserPage.locator('h1')).toHaveCount(1);
        }

        await verifyScreenshot(
          browserPage,
          `chrome--${page.name}--${theme}--${width}.png`,
          testInfo,
          undefined,
          CHROME_PIXEL_TOLERANCE,
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
            CHROME_PIXEL_TOLERANCE,
          );
        }
      });
    }
  }
}

// The hero sheet and its ghost stay the same size and centered at phone
// widths in both engines. iPhone Safari once laid the zoomed sheet out
// narrower than its stage, leaving the ghost wider and the pair off center
// (DESIGN.md, landing).
const PHONE_WIDTHS = [360, 375, 390, 414, 430] as const;

async function expectCenteredSheet(page: Page, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  const boxes = await page.evaluate(() => {
    const box = (id: string) => {
      const element = document.querySelector(`[data-testid="${id}"]`);
      if (element === null) throw new Error(`missing ${id}`);
      const rect = element.getBoundingClientRect();
      return {
        left: rect.left,
        right: rect.right,
        width: rect.width,
        height: rect.height,
      };
    };
    return {
      stage: box('landing-stage'),
      sheet: box('landing-sheet'),
      overflow: document.documentElement.scrollWidth
        - document.documentElement.clientWidth,
    };
  });
  expect(boxes.overflow).toBe(0);
  expect(Math.abs(boxes.sheet.width - boxes.stage.width)).toBeLessThan(1);
  expect(Math.abs(boxes.sheet.height - boxes.stage.height)).toBeLessThan(1);
  expect(Math.abs(boxes.sheet.left - boxes.stage.left)).toBeLessThan(1);
  const leftGap = boxes.sheet.left;
  const rightGap = width - boxes.sheet.right;
  expect(Math.abs(leftGap - rightGap)).toBeLessThan(2);
}

for (const engine of ['chromium', 'webkit'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    for (const locale of ['vi', 'en'] as const) {
      const title = `hero sheet centered on phones: ${engine} ${theme} `
        + locale;
      test(title, async ({ baseURL }) => {
        const origin = baseURL ?? 'http://127.0.0.1:20092';
        const launcher = engine === 'webkit' ? webkit : chromium;
        const browser: Browser = await launcher.launch({ args: [] });
        try {
          const context = await browser.newContext({ baseURL: origin });
          await context.addCookies([
            { name: 'aboutme-locale', value: locale, url: origin },
            { name: 'aboutme-theme', value: theme, url: origin },
          ]);
          const page = await context.newPage();
          for (const width of PHONE_WIDTHS) {
            await expectCenteredSheet(page, width);
          }
        } finally {
          await browser.close();
        }
      });
    }
  }
}
