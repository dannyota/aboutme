import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import {
  CHROME_PIXEL_TOLERANCE,
  mockSignedOutSession,
  verifyScreenshot,
  waitForImages,
} from './support';

// Pixel baselines for the community showcase (/showcase) with a stubbed
// listing: twelve tiles and a pager over three pages, and the empty state,
// in both languages and themes at phone and desktop width, signed out. Five
// more cases cover the open filter sheet, three active filters, and the
// tablet width (docs/design/ui/showcase.md). The card images are stored card
// baselines, so the tiles hold real card art.

const ORIGIN = 'http://127.0.0.1:20092';
const CARDS = [
  'card--accent-light',
  'card--no-photo-en',
  'card--short-name-en',
  'card--long-name-vi',
  'card--cjk-name',
] as const;
const TEMPLATES = [
  'engineer-compact',
  'one-page-tight',
  null,
  'nordic-muted',
  'executive-band',
  'modern-sidebar',
] as const;
const ROLES = [
  'backend',
  'frontend',
  null,
  'devops',
  'data-ai',
  'qa',
  'fresher',
  'other',
  null,
] as const;
const LANGUAGES = ['vi', 'en', 'other'] as const;

const FILLED = {
  items: Array.from({ length: 12 }, (_, index) => ({
    slug: `resume-${index}`,
    cardVersion: '0123456789abcdef',
    imageText: `Resume card ${index + 1}`,
    language: LANGUAGES[index % LANGUAGES.length],
    templateId: TEMPLATES[index % TEMPLATES.length],
    role: ROLES[index % ROLES.length],
  })),
  page: 2,
  pageCount: 3,
  total: 30,
};
const EMPTY = { items: [], page: 1, pageCount: 0, total: 0 };

const FILTERED_PATH = '/showcase?role=backend&lang=vi'
  + '&template=engineer-compact&page=2';

interface Case {
  readonly name: 'filled' | 'empty' | 'sheet' | 'filtered';
  readonly path: string;
  readonly body: typeof FILLED | typeof EMPTY;
  readonly locale: 'vi' | 'en';
  readonly theme: 'light' | 'dark';
  readonly width: number;
}

const CASES: Case[] = [];
for (const [name, path, body] of [
  ['filled', '/showcase?page=2', FILLED],
  ['empty', '/showcase', EMPTY],
] as const) {
  for (const locale of ['vi', 'en'] as const) {
    for (const theme of ['light', 'dark'] as const) {
      for (const width of [390, 1440]) {
        CASES.push({ name, path, body, locale, theme, width });
      }
    }
  }
}
CASES.push(
  {
    name: 'sheet',
    path: '/showcase?page=2',
    body: FILLED,
    locale: 'vi',
    theme: 'dark',
    width: 390,
  },
  {
    name: 'sheet',
    path: '/showcase?page=2',
    body: FILLED,
    locale: 'en',
    theme: 'light',
    width: 390,
  },
  {
    name: 'filtered',
    path: FILTERED_PATH,
    body: FILLED,
    locale: 'vi',
    theme: 'dark',
    width: 390,
  },
  {
    name: 'filtered',
    path: FILTERED_PATH,
    body: FILLED,
    locale: 'en',
    theme: 'light',
    width: 1440,
  },
  {
    name: 'filled',
    path: '/showcase?page=2',
    body: FILLED,
    locale: 'vi',
    theme: 'light',
    width: 768,
  },
);

for (const { name, path, body, locale, theme, width } of CASES) {
  const title = `showcase ${name} ${locale} ${theme} ${width}px `
    + 'matches baseline';
  test(title, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await mockSignedOutSession(page);
    await page.route(/\/api\/v1\/public\/showcase(?:\?.*)?$/u, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        headers: { 'cache-control': 'no-store, no-transform' },
        body: JSON.stringify(body),
      }));
    await page.route(
      /\/api\/v1\/public\/resumes\/resume-(\d+)\/og\/[0-9a-f]{16}\.png$/u,
      async (route) => {
        const index = Number(
          /resume-(\d+)/u.exec(route.request().url())?.[1] ?? '0',
        );
        const file = CARDS[index % CARDS.length]!;
        await route.fulfill({
          status: 200,
          contentType: 'image/png',
          body: await readFile(resolve(
            import.meta.dirname,
            'baselines',
            `${file}.png`,
          )),
        });
      },
    );
    await page.context().addCookies([
      { name: 'aboutme-locale', value: locale, url: ORIGIN },
      { name: 'aboutme-theme', value: theme, url: ORIGIN },
    ]);

    const response = await page.goto(path);
    expect(response?.status()).toBe(200);
    await page.addStyleTag({
      content: '*, *::before, *::after { transition: none !important; '
        + 'animation: none !important; }',
    });
    await expect(page.getByTestId('app-shell')).toBeVisible();
    await expect(page.locator(`[data-state="${
      name === 'empty' ? 'empty' : 'list'}"]`)).toBeVisible();
    await page.evaluate(() => document.fonts.ready);

    if (name !== 'empty') {
      await expect(page.locator('[data-showcase-slug]')).toHaveCount(12);
      // Load every lazy card, so the capture never depends on scroll or
      // decode timing.
      await page.locator('img[loading="lazy"]').evaluateAll((images) => {
        for (const image of images) {
          (image as HTMLImageElement).loading = 'eager';
        }
      });
      await waitForImages(page);
    }
    if (name === 'filled' || name === 'filtered') {
      // Grow the viewport to the page, so the capture holds every tile.
      const height = await page.evaluate(() =>
        document.documentElement.scrollHeight);
      await page.setViewportSize({ width, height });
    }
    if (name === 'sheet') {
      // The sheet is a fixed overlay, so the viewport keeps its phone height.
      await page.locator('[data-action="showcase-filters-open"]').click();
      await expect(page.getByRole('dialog')).toBeVisible();
      await expect(page.getByTestId('showcase-filter-sheet')).toBeVisible();
      // Wait for the slide to end: the sheet's box holds still over frames.
      await page.getByRole('dialog').evaluate((node) =>
        new Promise<void>((done) => {
          let last = node.getBoundingClientRect().top;
          let still = 0;
          const frame = () => {
            const top = node.getBoundingClientRect().top;
            still = top === last ? still + 1 : 0;
            last = top;
            if (still >= 3) done();
            else requestAnimationFrame(frame);
          };
          requestAnimationFrame(frame);
        }));
    }
    await page.evaluate(() => new Promise<void>((done) => {
      requestAnimationFrame(() => requestAnimationFrame(() => done()));
    }));

    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
      - document.documentElement.clientWidth);
    expect(overflow).toBe(0);

    await verifyScreenshot(
      page,
      `showcase--${name}--${locale}--${theme}--${width}.png`,
      testInfo,
      undefined,
      CHROME_PIXEL_TOLERANCE,
    );
  });
}
