import { expect, test } from '@playwright/test';

import { HTML_CSP } from '../app/utils/csp';

test('normal Nuxt output hydrates under the renderer CSP', async ({ page }) => {
  const dialogs: string[] = [];
  const errors: string[] = [];
  const external: string[] = [];
  const pageErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text());
  });
  page.on('dialog', async (dialog) => {
    dialogs.push(`${dialog.type()}:${dialog.message()}`);
    await dialog.dismiss();
  });
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.addInitScript(() => {
    const violations: object[] = [];
    Object.defineProperty(window, '__cspViolations', { value: violations });
    document.addEventListener('securitypolicyviolation', (event) => {
      violations.push({
        blockedURI: event.blockedURI,
        effectiveDirective: event.effectiveDirective,
      });
    });
  });
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url());
    if (
      (url.protocol === 'http:' || url.protocol === 'https:')
      && url.hostname !== '127.0.0.1'
      && url.hostname !== 'localhost'
      && url.hostname !== '[::1]'
    ) {
      external.push(url.href);
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
  await page.route('**/login', async (route) => {
    const upstream = await route.fetch({ maxRedirects: 0, maxRetries: 0 });
    await route.fulfill({
      response: upstream,
      headers: {
        ...upstream.headers(),
        'content-security-policy': HTML_CSP,
      },
    });
  });

  const response = await page.goto('/login');
  expect(response?.headers()['content-security-policy']).toBe(HTML_CSP);
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await expect(page.locator('#__nuxt')).toHaveAttribute('data-v-app', '');
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
  const violations = await page.evaluate(() =>
    (window as Window & { __cspViolations?: unknown[] }).__cspViolations ?? [],
  );
  expect(violations).toEqual([]);
  expect(dialogs).toEqual([]);
  expect(errors).toEqual([]);
  expect(pageErrors).toEqual([]);
  expect(external).toEqual([]);
});
