import type { Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// Fixture and route helpers for verify.spec.ts
// (docs/design/deployment-transparency/page.md,
// docs/design/deployment-transparency/document.md). The verify page fetches
// `/.well-known/deployment.json` same-origin and reads its freshness from the
// response's `Date` and `Age` headers, never the visitor's clock, so every
// helper here sets `Date` from the fixture's own `observed_at` instead of the
// wall clock.

export const VERIFY_BASE_URL = 'http://127.0.0.1:20092';
const FIXTURES_DIR = resolve(import.meta.dirname, 'fixtures/deployment');

export async function readFixtureText(name: string): Promise<string> {
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
