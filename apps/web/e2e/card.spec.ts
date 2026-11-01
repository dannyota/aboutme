import { createCanvas } from '@napi-rs/canvas';
import { expect, test, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { resolve } from 'node:path';
import { Worker } from 'node:worker_threads';

import {
  CARD_LINK_PREFIX,
  CARD_SITE,
  fitHeadline,
  fitName,
} from '../app/components/preview/cardFit';
import {
  CARD_LAYOUT_VERSION,
  type PreviewCardContent,
} from '../app/components/preview/cardLayout';
import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';
import type { PrintCardEnvelope } from '../server/utils/print/envelope';
import { PRINT_CONTENT_SECURITY_POLICY } from '../server/utils/print/protocol';
import { measureCardCrop } from './display-assertions';
import { denyExternalRequests, verifyScreenshot } from './support';

// The harness Nitro output directory (apps/web/nuxt.config.ts sets
// nitro.output.dir to '.output/harness' when NUXT_HARNESS=1) holds the print
// worker at server/utils/print/worker-build.ts's
// printWorkerOutput('.output/harness'), which joins 'server/workers/print.mjs'
// onto it. This spec runs only in the harness Playwright surface, so that
// build always exists by the time it starts.
const PRINT_WORKER_PATH = resolve(
  import.meta.dirname,
  '../.output/harness/server/workers/print.mjs',
);

const CARD_WIDTH = 1200;
const CARD_HEIGHT = 630;
const CARD_MAX_BYTES = 524_288;
const RESUME_ID = 'c0000000-0000-4000-8000-000000000003';
// PRINT_PHOTO_MAX_BYTES in server/utils/print/envelope.ts. That module
// imports the build-time document validator alias, which only the Nuxt and
// Vite builds resolve, so this spec imports its types alone.
const PRINT_PHOTO_MAX_BYTES = 2_097_152;

// The safe square every identifying box must stay inside
// (docs/design/link-previews.md, "Preview card").
const SAFE_SQUARE = { xMax: 885, xMin: 315, yMax: 615, yMin: 15 };

// Copied verbatim from apps/server/internal/printrender/browser.go's
// pageReadinessExpression, so this spec waits exactly as Go's capture step
// does before it screenshots the card.
/* eslint-disable max-len -- verbatim copy of Go's pageReadinessExpression. */
const PAGE_READINESS_EXPRESSION = `(async () => {
  const roots = document.querySelectorAll('[data-print-document="true"]');
  if (roots.length !== 1 || document.scripts.length !== 0) throw new Error('invalid document');
  void document.documentElement.offsetHeight;
  await document.fonts.ready;
  const fonts = Array.from(document.fonts);
  if (document.fonts.status !== 'loaded' || fonts.some((font) => font.status === 'error')) throw new Error('fonts failed');
  const images = Array.from(document.images);
  await Promise.all(images.map((image) => image.decode()));
  if (images.some((image) => !image.complete || image.naturalWidth <= 0 || image.naturalHeight <= 0)) throw new Error('image failed');
  return true;
})()`;
/* eslint-enable max-len -- end of the verbatim copy above. */

// Contact-shaped strings that must never reach the card page, since the card
// envelope carries no contact field (docs/design/link-previews.md, "Preview
// card").
const CONTACT_SENTINELS = [
  'sentinel-contact@example.test',
  '+84 987 654 321',
  'https://sentinel-contact.example.test',
] as const;

// The envelope Go sends; the worker renders it as the print route would
// after decoding (test/print/card-envelope.test.ts covers the decoder).
function buildEnvelope(card: PreviewCardContent): PrintCardEnvelope {
  return { card, kind: 'card', resumeId: RESUME_ID, version: 1 };
}

async function renderCardHTML(envelope: PrintCardEnvelope): Promise<string> {
  return new Promise<string>((resolvePromise, reject) => {
    const worker = new Worker(PRINT_WORKER_PATH, { workerData: envelope });
    worker.once('message', (message: unknown) => {
      void worker.terminate();
      const typed = message as { html?: unknown; type?: unknown };
      if (typed.type !== 'result' || typeof typed.html !== 'string') {
        reject(new Error('print worker sent an unexpected message'));
        return;
      }
      resolvePromise(typed.html);
    });
    worker.once('error', (error) => {
      void worker.terminate();
      reject(error);
    });
  });
}

// Serves html at a same-origin harness path with the print CSP and
// content-type, so the card's own stylesheets and fonts still load from the
// real harness server while every other request stays denied
// (apps/web/e2e/support.ts's denyExternalRequests).
async function openCard(page: Page, html: string): Promise<void> {
  await page.route('**/_e2e-card-fixture.html', async (route) => {
    await route.fulfill({
      body: html,
      contentType: 'text/html; charset=utf-8',
      headers: { 'content-security-policy': PRINT_CONTENT_SECURITY_POLICY },
      status: 200,
    });
  });
  await page.setViewportSize({ height: CARD_HEIGHT, width: CARD_WIDTH });
  const response = await page.goto('/_e2e-card-fixture.html');
  expect(response?.ok()).toBe(true);
  const ready = await page.evaluate(PAGE_READINESS_EXPRESSION);
  expect(ready).toBe(true);
}

interface Rect {
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

function expectRectInSafeSquare(rect: Rect): void {
  expect(rect.x).toBeGreaterThanOrEqual(SAFE_SQUARE.xMin);
  expect(rect.x + rect.width).toBeLessThanOrEqual(SAFE_SQUARE.xMax);
  expect(rect.y).toBeGreaterThanOrEqual(SAFE_SQUARE.yMin);
  expect(rect.y + rect.height).toBeLessThanOrEqual(SAFE_SQUARE.yMax);
}

// Checks one identifying box, when present, and every line rect of its text
// against the safe square (docs/design/link-previews.md, "Preview card").
// The photo has no text, so it skips the line-rect pass; the name and
// headline are also capped at two lines each
// (docs/design/link-preview-card.md, "Fitting the text").
async function expectBoxInSafeSquare(
  page: Page,
  selector: string,
  hasText: boolean,
): Promise<void> {
  const locator = page.locator(selector);
  if ((await locator.count()) === 0) return;
  const box = await locator.boundingBox();
  expect(box).not.toBeNull();
  expectRectInSafeSquare(box!);
  if (!hasText) return;
  // The rects of the text itself, one or more per line box; element boxes
  // are left out so a two-line link counts as two lines, not four rects.
  const lines = await locator.evaluate((element) => {
    const rects: { height: number; width: number; x: number; y: number }[]
      = [];
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    for (
      let node = walker.nextNode();
      node !== null;
      node = walker.nextNode()
    ) {
      const range = document.createRange();
      range.selectNodeContents(node);
      for (const rect of range.getClientRects()) {
        if (rect.width === 0) continue;
        rects.push({
          height: rect.height, width: rect.width, x: rect.x, y: rect.y,
        });
      }
    }
    return rects;
  });
  expect(lines.length).toBeGreaterThan(0);
  for (const line of lines) {
    expectRectInSafeSquare(line);
    expect(line.x).toBeGreaterThanOrEqual(box!.x - 1);
    expect(line.y).toBeGreaterThanOrEqual(box!.y - 1);
    expect(line.x + line.width).toBeLessThanOrEqual(box!.x + box!.width + 1);
    expect(line.y + line.height).toBeLessThanOrEqual(box!.y + box!.height + 1);
  }
  if (
    selector === '.preview-card-name' || selector === '.preview-card-headline'
  ) {
    const tops = new Set(lines.map((line) => Math.round(line.y)));
    expect(tops.size).toBeLessThanOrEqual(2);
  }
}

async function expectGeometry(page: Page): Promise<void> {
  await expectBoxInSafeSquare(page, '.preview-card-photo', false);
  await expectBoxInSafeSquare(page, '.preview-card-name', true);
  await expectBoxInSafeSquare(page, '.preview-card-headline', true);
  await expectBoxInSafeSquare(page, '.preview-card-footer', true);
}

// The page's visible text, normalized to single spaces between the runs a
// block boundary would separate: block-level lines collapse to one space in
// innerText the same way here as they do in the browser, so this stays an
// exact comparison of the meaningful text rather than of incidental
// whitespace.
function normalizeVisibleText(text: string): string {
  return text.replace(/\s+/gu, ' ').trim();
}

function expectedVisibleText(card: PreviewCardContent): string {
  const parts: string[] = [];
  if (card.name === null) {
    parts.push(CARD_LINK_PREFIX, card.slug);
  } else {
    parts.push(fitName(card.name).text);
    if (card.headline !== null) parts.push(fitHeadline(card.headline));
  }
  parts.push(card.name === null ? CARD_SITE : CARD_LINK_PREFIX + card.slug);
  return normalizeVisibleText(parts.join(' '));
}

async function expectPageContent(
  page: Page,
  card: PreviewCardContent,
): Promise<void> {
  const visibleText = await page.locator('body').innerText();
  expect(normalizeVisibleText(visibleText)).toBe(expectedVisibleText(card));
  const html = await page.content();
  for (const sentinel of CONTACT_SENTINELS) {
    expect(html).not.toContain(sentinel);
  }
  await expect(page.locator('[data-print-document="true"]')).toHaveCount(1);
  await expect(page.locator('script')).toHaveCount(0);
}

// Builds a Vietnamese name-shaped string of exactly `length` grapheme
// clusters, with stacked diacritics on capitals (Ẩ, Ỗ) and never ending in a
// space, so validCardText (server/utils/print/envelope.ts) accepts it.
function longVietnameseText(length: number): string {
  const unit = 'Nguyễn Ẩn Ỗn Thị Bích Ngọc Vũ Phương ';
  const repeated = unit.repeat(Math.ceil(length / [...unit].length) + 2);
  const clusters = [...repeated].slice(0, length);
  if (clusters.at(-1) === ' ') clusters[clusters.length - 1] = 'x';
  return clusters.join('');
}

// A public slug of exactly `length` characters (publicroots.ValidSlug):
// lowercase letters and digits in hyphen-separated runs.
function slugOfLength(length: number): string {
  const sizes: number[] = [];
  let remaining = length;
  const letters = 'abcd';
  while (remaining > 0) {
    const size = Math.min(remaining, 7);
    sizes.push(size);
    remaining -= size + (remaining > size ? 1 : 0);
  }
  return sizes
    .map((size, index) => letters[index % letters.length]!.repeat(size))
    .join('-');
}

const CROPPED_PHOTO = {
  crop: { height: 0.8, width: 0.8, x: 0.1, y: 0.05 },
  url: FIXED_PHOTO_DATA_URL,
};
const UNCROPPED_PHOTO = { crop: null, url: FIXED_PHOTO_DATA_URL };
const FIXTURE_ACCENT = '#1a5ceb';

interface CardCase {
  readonly card: PreviewCardContent;
  readonly name: string;
}

const CASES: readonly CardCase[] = [
  {
    card: {
      accent: FIXTURE_ACCENT,
      headline: longVietnameseText(80),
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'vi',
      name: longVietnameseText(150),
      photo: CROPPED_PHOTO,
      slug: 'nguyen-an-long',
    },
    name: 'long-name-vi',
  },
  {
    card: {
      accent: FIXTURE_ACCENT,
      headline: 'Analyst',
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'en',
      name: 'Ada Lovelace',
      photo: UNCROPPED_PHOTO,
      slug: 'ada-lovelace',
    },
    name: 'short-name-en',
  },
  {
    card: {
      accent: FIXTURE_ACCENT,
      headline: 'Computer Scientist',
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'en',
      name: 'Grace Hopper',
      photo: null,
      slug: 'grace-hopper',
    },
    name: 'no-photo-en',
  },
  {
    card: {
      accent: FIXTURE_ACCENT,
      headline: null,
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'en',
      name: null,
      photo: null,
      slug: slugOfLength(30),
    },
    name: 'no-name',
  },
  {
    card: {
      // Already at least 3:1 against white (docs/design/link-previews.md,
      // "Preview card").
      accent: '#2f855a',
      headline: 'Physicist',
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'en',
      name: 'Marie Curie',
      photo: UNCROPPED_PHOTO,
      slug: 'marie-curie-light',
    },
    name: 'accent-light',
  },
  {
    card: {
      accent: '#000000',
      headline: 'Physicist',
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'en',
      name: 'Marie Curie',
      photo: UNCROPPED_PHOTO,
      slug: 'marie-curie-dark',
    },
    name: 'accent-dark',
  },
  {
    card: {
      accent: FIXTURE_ACCENT,
      headline: '軟件工程師',
      layoutVersion: CARD_LAYOUT_VERSION,
      lng: 'zh',
      name: '王小明',
      photo: UNCROPPED_PHOTO,
      slug: 'wang-xiao-ming',
    },
    name: 'cjk-name',
  },
];

async function expectCardDisplay(
  page: Page,
  card: PreviewCardContent,
  name: string,
): Promise<void> {
  const root = page.locator('.preview-card');
  await expect(root).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(root).toHaveCSS('color', 'rgb(23, 26, 24)');
  await expect(root).toHaveCSS('font-family', '"Be Vietnam Pro", sans-serif');
  await expect(root).toHaveCSS('font-synthesis', 'none');
  await expect(root).toHaveAttribute('lang', card.lng);
  expect(await page.evaluate(() => Array.from(document.fonts).some((face) =>
    face.family === 'Be Vietnam Pro' && face.status === 'loaded')))
    .toBe(true);
  const rootBox = await root.boundingBox();
  expect(rootBox).toMatchObject({ height: 630, width: 1200, x: 0, y: 0 });
  const sizes: Record<string, string> = {
    'long-name-vi': '52px',
    'no-name': '24px',
  };
  await expect(page.locator('.preview-card-name')).toHaveCSS(
    'font-size', sizes[name] ?? '72px');
  await expect(page.locator('.preview-card-name')).toHaveCSS(
    'font-weight', '700');
  if (card.headline !== null && card.name !== null) {
    await expect(page.locator('.preview-card-headline')).toHaveCSS(
      'font-size', '32px');
  } else {
    await expect(page.locator('.preview-card-headline')).toHaveCount(0);
  }
  const logo = page.getByRole('img', { name: 'aboutme.vn' });
  await expect(logo).toHaveCount(1);
  await expect(logo).toHaveAttribute('viewBox', '0 0 30 32');
  await expect(logo.locator('[data-logo-ring]')).toHaveCount(2);
  await expect(logo.locator('[data-logo-part="mark"]')).toHaveCSS(
    'color', 'rgb(204, 38, 73)');
  for (const [selector, x] of [
    ['.preview-card-band-left', 0],
    ['.preview-card-band-right', 915],
  ] as const) {
    const band = page.locator(selector);
    expect(await band.boundingBox()).toMatchObject({
      height: 630, width: 285, x, y: 0,
    });
    expect(await band.evaluate((el) => getComputedStyle(el).backgroundImage))
      .toContain('linear-gradient');
  }
  const photo = page.locator('.preview-card-photo');
  if (card.photo === null) {
    await expect(photo).toHaveCount(0);
    return;
  }
  await expect(photo).toBeVisible();
  const photoBox = await photo.boundingBox();
  expect(photoBox).toMatchObject({ height: 208, width: 208 });
  await expect(photo.locator('.preview-card-photo-clip')).toHaveCSS(
    'overflow', 'hidden');
  await expect(photo).toHaveCSS('border-color',
    card.accent === '#000000' ? 'rgb(0, 0, 0)'
      : card.accent === '#2f855a' ? 'rgb(47, 133, 90)'
        : 'rgb(26, 92, 235)');
  const image = photo.locator('img');
  await expect(image).toHaveCSS('object-fit', 'cover');
  expect(await image.evaluate((el) => {
    const img = el as HTMLImageElement;
    return img.complete && img.naturalWidth > 0 && img.naturalHeight > 0;
  })).toBe(true);
  const crop = await measureCardCrop(image);
  const expected = card.photo.crop === null
    ? { height: 1, left: 0, top: 0, width: 1 }
    : { height: 1.25, left: -0.125, top: -0.0625, width: 1.25 };
  for (const key of ['height', 'left', 'top', 'width'] as const) {
    expect(Math.abs(crop[key] - expected[key])).toBeLessThanOrEqual(0.001);
  }
}

for (const { card, name } of CASES) {
  test(`card--${name}`, async ({ page }, testInfo) => {
    const external = await denyExternalRequests(page);
    const html = await renderCardHTML(buildEnvelope(card));
    await openCard(page, html);
    await expectGeometry(page);
    await expectPageContent(page, card);
    await expectCardDisplay(page, card, name);
    await verifyScreenshot(
      page,
      `card--${name}.png`,
      testInfo,
      page.locator('.preview-card'),
    );
    expect(external).toEqual([]);
  });
}

for (const scheme of ['light', 'dark'] as const) {
  for (const { card, name } of CASES) {
    test(`card display ${name} with ${scheme} preference`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      const html = await renderCardHTML(buildEnvelope(card));
      await openCard(page, html);
      await expectGeometry(page);
      await expectPageContent(page, card);
      await expectCardDisplay(page, card, name);
    });
  }
  for (const variant of ['no-photo-en', 'no-name'] as const) {
    test(`Vietnamese ${variant} card with ${scheme} preference`, async ({
      page,
    }) => {
      const original = CASES.find((item) => item.name === variant)!;
      const card = { ...original.card, lng: 'vi' };
      await page.emulateMedia({ colorScheme: scheme });
      await openCard(page, await renderCardHTML(buildEnvelope(card)));
      await expectGeometry(page);
      await expectPageContent(page, card);
      await expectCardDisplay(page, card, variant);
    });
  }
}

test('card crop assertion detects ignored offsets', async ({ page }) => {
  const card = CASES[0]!.card;
  await openCard(page, await renderCardHTML(buildEnvelope(card)));
  const image = page.locator('.preview-card-photo-image');
  await expectCardDisplay(page, card, 'long-name-vi');
  await image.evaluate((el) => {
    (el as HTMLElement).style.left = '0';
  });
  await expect(expectCardDisplay(page, card, 'long-name-vi')).rejects.toThrow();
});

// Generates a JPEG data URL of random pixels, backing off the encoder
// quality until it fits PRINT_PHOTO_MAX_BYTES: random pixels are the
// hardest case for JPEG's DCT coder, so this always finds a quality that
// fits rather than assuming one.
function noisyPhotoDataURL(): string {
  const size = 512;
  const canvas = createCanvas(size, size);
  const context = canvas.getContext('2d');
  const image = context.createImageData(size, size);
  image.data.set(randomBytes(image.data.length));
  for (let offset = 3; offset < image.data.length; offset += 4) {
    image.data[offset] = 255;
  }
  context.putImageData(image, 0, 0);
  let quality = 90;
  let buffer = canvas.toBuffer('image/jpeg', quality);
  while (buffer.length > PRINT_PHOTO_MAX_BYTES && quality > 10) {
    quality -= 10;
    buffer = canvas.toBuffer('image/jpeg', quality);
  }
  expect(buffer.length).toBeLessThanOrEqual(PRINT_PHOTO_MAX_BYTES);
  return `data:image/jpeg;base64,${buffer.toString('base64')}`;
}

test('a noisy photo keeps the rendered card under its byte cap', async ({
  page,
}) => {
  const external = await denyExternalRequests(page);
  const card: PreviewCardContent = {
    accent: FIXTURE_ACCENT,
    headline: 'Stress case',
    layoutVersion: CARD_LAYOUT_VERSION,
    lng: 'en',
    name: 'Noise Test',
    photo: { crop: null, url: noisyPhotoDataURL() },
    slug: 'noisy-photo-size',
  };
  const html = await renderCardHTML(buildEnvelope(card));
  await openCard(page, html);
  const bytes = await page.screenshot({
    clip: { height: CARD_HEIGHT, width: CARD_WIDTH, x: 0, y: 0 },
    type: 'png',
  });
  expect(bytes.byteLength).toBeLessThanOrEqual(CARD_MAX_BYTES);
  expect(external).toEqual([]);
});
