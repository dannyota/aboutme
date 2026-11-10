import { expect, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import {
  expectScreen, measureTextContainment, measureVerticalFlow,
} from './display-assertions';

import { chromeScreenContract } from './support';

// Fixture and route helpers for verify.spec.ts
// (docs/design/deployment-transparency/page.md,
// docs/design/deployment-transparency/document.md). The verify page fetches
// `/.well-known/deployment.json` same-origin and reads its freshness from the
// response's `Date` and `Age` headers, never the visitor's clock, so every
// helper here sets `Date` from the fixture's own `observed_at` instead of the
// wall clock.

export const VERIFY_BASE_URL = 'http://127.0.0.1:20092';
const FIXTURES_DIR = resolve(import.meta.dirname, 'fixtures/deployment');

async function readFixtureText(name: string): Promise<string> {
  return readFile(resolve(FIXTURES_DIR, `${name}.json`), 'utf8');
}

export interface ServeOptions {
  /** The fixture file base name, without `.json`. Omit for a raw `body`. */
  readonly fixture?: string;
  readonly body?: string;
  readonly status?: number;
  /**
   * Milliseconds added to the fixture's `observed_at` for the `Date` header.
   * A fresh fixture uses a small margin; the stale fixture uses one past its
   * own `stale_after` (docs/design/deployment-transparency/README.md,
   * run freshness and staleness).
   */
  readonly dateOffsetMs?: number;
  readonly age?: string;
}

/** Serves `/.well-known/deployment.json` from a fixture or a raw body. */
export async function serveDeployment(
  page: Page,
  options: ServeOptions,
): Promise<void> {
  const { fixture, status = 200, dateOffsetMs = 5000, age = '0' } = options;
  let body = options.body ?? '';
  let date = new Date().toUTCString();
  if (fixture !== undefined) {
    body = await readFixtureText(fixture);
    const { observed_at: observedAt } = JSON.parse(body) as {
      observed_at: string;
    };
    date = new Date(Date.parse(observedAt) + dateOffsetMs).toUTCString();
  }
  await page.route('**/.well-known/deployment.json', async (route) => {
    await route.fulfill({
      status,
      contentType: 'application/json',
      headers: { date, age, 'cache-control': 'no-cache' },
      body,
    });
  });
}

/** Sets the locale and theme cookies and opens `/verify`. */
export async function gotoVerify(
  page: Page,
  locale: 'vi' | 'en' = 'vi',
  theme: 'light' | 'dark' = 'light',
): Promise<void> {
  await page.context().addCookies([
    { name: 'aboutme-locale', value: locale, url: VERIFY_BASE_URL },
    { name: 'aboutme-theme', value: theme, url: VERIFY_BASE_URL },
  ]);
  await page.goto('/verify');
}

/**
 * Replaces `navigator.clipboard.writeText` with a recorder so headless
 * Chromium never needs the unreliable clipboard permission. The written text
 * lands on `window.__verifyClipboardText`; a rejecting stub exercises the
 * copy-failed announcement.
 */
export async function stubClipboard(
  page: Page,
  { reject = false }: { reject?: boolean } = {},
): Promise<void> {
  await page.addInitScript((shouldReject: boolean) => {
    (window as unknown as { __verifyClipboardText: string | null })
      .__verifyClipboardText = null;
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (text: string) => {
          (window as unknown as { __verifyClipboardText: string | null })
            .__verifyClipboardText = text;
          return shouldReject
            ? Promise.reject(new Error('clipboard denied'))
            : Promise.resolve();
        },
      },
    });
  }, reject);
}

export async function readClipboard(page: Page): Promise<string | null> {
  return page.evaluate(() => (window as unknown as {
    __verifyClipboardText: string | null;
  }).__verifyClipboardText);
}

/** Disables CSS transitions and animations, as chrome.spec.ts does. */
export async function disableTransitions(page: Page): Promise<void> {
  await page.addStyleTag({
    content: '*, *::before, *::after { transition: none !important; '
      + 'animation: none !important; }',
  });
}

export async function pageOverflow(page: Page): Promise<number> {
  return page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
}

export async function expectVerifyDisplay(
  page: Page,
  state: 'verified' | 'unavailable',
  locale: 'vi' | 'en',
  theme: 'light' | 'dark',
): Promise<void> {
  await expectScreen(page, verifyContract(locale, theme, state));
  const title = locale === 'vi'
    ? 'Kiểm chứng phiên bản đang chạy'
    : 'Verify what\'s running';
  const heading = page.getByRole('heading', { level: 1, name: title });
  await expect(heading).toBeVisible();
  const wide = page.viewportSize()!.width >= 672;
  await expect(heading).toHaveCSS('font-size', wide ? '32px' : '24px');
  await expect(heading).toHaveCSS('line-height', wide ? '41.6px' : '31.2px');
  await expect(heading).toHaveCSS('font-weight', '700');
  await expect(page.locator('body')).toHaveCSS('font-size', '15px');
  await expect(page.locator('body')).toHaveCSS('line-height', '22.5px');
  await expect(heading).toMatchAriaSnapshot(
    `- heading "${title}" [level=1]`);
  const status = page.getByTestId('verify-status');
  await expect(status).toHaveAttribute('data-state', state);
  const titleText = state === 'verified'
    ? (locale === 'vi'
        ? 'Đang chạy đúng mã nguồn trên GitHub'
        : 'Running exactly what\'s on GitHub')
    : (locale === 'vi'
        ? 'Máy chủ này không công bố thông tin triển khai'
        : 'This server publishes no deployment record');
  await expect(status.getByText(titleText, { exact: true })).toBeVisible();
  const colors = {
    dark: { unavailable: 'rgb(26, 33, 56)', verified: 'rgb(21, 29, 54)' },
    light: {
      unavailable: 'rgb(239, 237, 230)', verified: 'rgb(236, 238, 247)',
    },
  };
  await expect(status).toHaveCSS('background-color', colors[theme][state]);
  await expect(status).toHaveCSS('color', theme === 'light'
    ? 'rgb(16, 27, 63)'
    : 'rgb(243, 241, 236)');
  expect(await page.evaluate(() => Array.from(document.fonts).some((face) =>
    face.family === 'Be Vietnam Pro' && face.status === 'loaded')))
    .toBe(true);
  const mark = status.locator('.verify-mark');
  await expect(mark.locator('svg')).toHaveCount(1);
  await expect(mark.locator(state === 'verified'
    ? '.lucide-shield-check'
    : '.lucide-info')).toBeVisible();
  const pageWidth = page.viewportSize()!.width;
  await expect(mark).toHaveCSS('width', pageWidth < 672 ? '40px' : '56px');
  expect(await pageOverflow(page)).toBeLessThanOrEqual(1);
  expect(await measureVerticalFlow(page.locator(
    '[data-testid="verify-page"] > :not(.sr-only)',
  )), 'Verify card separation.').toBe(true);
  expect(await measureTextContainment(status.locator(
    '.verify-status-text [role="status"] > p',
  ).first())).toEqual({ contained: true, visible: true });
  await expect(page.getByTestId('verify-limits').getByRole('heading', {
    level: 2, name: locale === 'vi'
      ? 'Trang này chứng minh gì'
      : 'What this proves',
  })).toBeVisible();
  await expect(page.getByTestId('verify-limits').locator('li')).toHaveCount(5);
  if (state === 'unavailable') {
    await expect(page.getByTestId('verify-chain')).toHaveCount(0);
    await expect(page.getByTestId('verify-components')).toHaveCount(0);
    await expect(page.getByTestId('verify-selector')).toHaveCount(0);
    await expect(page.getByTestId('verify-command-2'))
      .toContainText('<digest>');
    return;
  }
  const chain = page.getByTestId('verify-chain');
  await expect(chain.getByRole('heading', {
    level: 2,
    name: locale === 'vi'
      ? 'Từ mã nguồn đến bản đang chạy'
      : 'From source to running',
  })).toBeVisible();
  const steps = chain.locator('[data-step]');
  await expect(steps).toHaveCount(4);
  expect(await steps.evaluateAll((els) => els.map((el) =>
    el.getAttribute('data-step')))).toEqual([
    'source', 'build', 'image', 'running',
  ]);
  const labels = locale === 'vi'
    ? ['Mã nguồn', 'Bản dựng', 'Image', 'Đang chạy']
    : ['Source', 'Build', 'Image', 'Running'];
  for (let index = 0; index < labels.length; index += 1) {
    await expect(steps.nth(index).getByText(labels[index]!, { exact: true }))
      .toBeVisible();
  }
  if (pageWidth < 672) {
    expect(await measureVerticalFlow(steps)).toBe(true);
  } else {
    const separate = await steps.evaluateAll((elements) => {
      const boxes = elements.map((el) => el.getBoundingClientRect());
      return boxes.every((box, index) => box.width > 0 && box.height > 0
        && (index === 0 || box.left >= boxes[index - 1]!.right - 1));
    });
    expect(separate).toBe(true);
  }
  await expect(chain.locator('[data-chip="match"]')).toHaveCount(4);
  await expect(chain.locator('[data-connector="agree"]')).toHaveCount(3);
  const rows = page.getByTestId('verify-components')
    .locator('[data-component]');
  await expect(rows).toHaveCount(3);
  expect(await rows.evaluateAll((els) => els.map((el) =>
    el.getAttribute('data-component')))).toEqual(['server', 'web', 'caddy']);
  expect(await measureVerticalFlow(rows)).toBe(true);
  for (const component of ['server', 'web', 'caddy']) {
    const row = rows.filter({
      has: page.locator(
        `[aria-label="${locale === 'vi' ? 'Sao chép mã băm của' : 'Copy the'} `
        + `${component}${locale === 'vi' ? '' : ' digest'}"]`,
      ),
    });
    await expect(row).toBeVisible();
    await expect(row.locator('[title^="sha256:"]')).toHaveCount(1);
    const chip = row.locator('[title^="sha256:"]');
    await expect(chip).toHaveCSS('white-space', 'nowrap');
    expect(await chip.evaluate((el) => el.scrollWidth <= el.clientWidth + 1))
      .toBe(true);
  }
}

export function verifyContract(
  locale: 'vi' | 'en', theme: 'light' | 'dark',
  state: 'verified' | 'unavailable' = 'verified',
) {
  return {
    ...chromeScreenContract('[data-testid="verify-page"]',
      '[data-testid="verify-page"] :is(h1,h3,p,li)'
      + ':not(.sr-only):not(:has(p,pre,ol,ul,.sr-only))',
      '[data-testid="verify-page"] > :not(.sr-only)',
      '[data-testid="verify-page"] h1', locale === 'vi'
        ? 'Kiểm chứng phiên bản đang chạy'
        : 'Verify what\'s running', theme),
    paint: ['[data-testid="verify-status"]', 'background-color',
      state === 'verified'
        ? theme === 'light' ? 'rgb(236, 238, 247)' : 'rgb(21, 29, 54)'
        : theme === 'light' ? 'rgb(239, 237, 230)' : 'rgb(26, 33, 56)',
    ] as const,
  };
}
