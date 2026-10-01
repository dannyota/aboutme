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
// in both languages and themes at phone and desktop width, signed out. The
// card images are stored card baselines, so the tiles hold real card art.

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

const STATES = [
  { name: 'filled', path: '/showcase?page=2', body: FILLED },
  { name: 'empty', path: '/showcase', body: EMPTY },
] as const;
const LOCALES = ['vi', 'en'] as const;
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;

for (const state of STATES) {
  for (const locale of LOCALES) {
    for (const theme of THEMES) {
      for (const width of WIDTHS) {
        const title = `showcase ${state.name} ${locale} ${theme} ${width}px `
          + 'matches baseline';
        test(title, async ({ page }, testInfo) => {
          await page.setViewportSize({ width, height: 900 });
          await mockSignedOutSession(page);
          await page.route(/\/api\/v1\/public\/showcase(?:\?.*)?$/u, (route) =>
            route.fulfill({
              status: 200,
              contentType: 'application/json',
              headers: { 'cache-control': 'no-store, no-transform' },
              body: JSON.stringify(state.body),
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

          const response = await page.goto(state.path);
          expect(response?.status()).toBe(200);
          await page.addStyleTag({
            content: '*, *::before, *::after { transition: none !important; '
              + 'animation: none !important; }',
          });
          await expect(page.getByTestId('app-shell')).toBeVisible();
          await expect(page.locator(`[data-state="${
            state.name === 'filled' ? 'list' : 'empty'}"]`)).toBeVisible();
          await page.evaluate(() => document.fonts.ready);

          if (state.name === 'filled') {
            await expect(page.locator('[data-showcase-slug]'))
              .toHaveCount(12);
            // Grow the viewport to the page and load every lazy card, so the
            // capture never depends on scroll or decode timing.
            const height = await page.evaluate(() =>
              document.documentElement.scrollHeight);
            await page.setViewportSize({ width, height });
            await page.locator('img[loading="lazy"]').evaluateAll(
              (images) => {
                for (const image of images) {
                  (image as HTMLImageElement).loading = 'eager';
                }
              },
            );
            await waitForImages(page);
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
            `showcase--${state.name}--${locale}--${theme}--${width}.png`,
            testInfo,
            undefined,
            CHROME_PIXEL_TOLERANCE,
          );
        });
      }
    }
  }
}
