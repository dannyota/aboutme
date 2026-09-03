import { expect, test } from '@playwright/test';

import { HTML_CSP } from '../app/utils/csp';
import { denyExternalRequests } from './support';

test('normal Nuxt output hydrates under the renderer CSP', async ({ page }) => {
  const dialogs: string[] = [];
  const errors: string[] = [];
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
  const external = await denyExternalRequests(page);
  await page.route('**/api/v1/me', async (route) => {
    await route.fulfill({ status: 204 });
  });
  await page.route('**/api/v1/capabilities', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { providerLogin: false, agentAccess: false },
      }),
    });
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
  await expect.poll(() => page.evaluate(() =>
    Boolean((document.getElementById('__nuxt') as HTMLElement & {
      __vue_app__?: unknown;
    } | null)?.__vue_app__),
  )).toBe(true);
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
