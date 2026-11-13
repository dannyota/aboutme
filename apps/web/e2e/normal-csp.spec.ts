import { createCanvas } from '@napi-rs/canvas';
import { expect, test } from '@playwright/test';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { readFileSync } from 'node:fs';
import { resolve as resolvePath } from 'node:path';

import { APP_CSP } from '../app/utils/csp';
import {
  denyExternalRequests,
  expectCspClean,
  mockSignedInSession,
  mockSignedOutSession,
  trackCsp,
} from './support';

// Chromium logs "Failed to load resource" with the status, not the URL.
const ANONYMOUS_ME_401 = 'a status of 401';

// Chromium logs "Failed to load resource" for a 404 document.
const DOCUMENT_404 = 'a status of 404';

const fixtureDocument = JSON.parse(
  readFileSync(
    resolvePath(
      import.meta.dirname,
      '../../../packages/schema/fixtures/full.json',
    ),
    'utf8',
  ),
) as { personalDetails?: { photo?: { key: string } } };

const sampleDocument = JSON.parse(JSON.stringify(fixtureDocument)) as {
  personalDetails?: { photo?: unknown };
};
delete sampleDocument.personalDetails?.photo;

const sampleMetadata = {
  id: 'resume-1',
  title: 'Fixture',
  lng: 'en',
  live: false,
  downloadEnabled: false,
  seoGeoEnabled: false,
  slug: null,
  publicTitle: null,
  faviconEmoji: null,
  schemaVersion: CURRENT_VERSION,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

function expectPlainAppCsp(headers: Record<string, string>): void {
  expect(headers['content-security-policy']).toBe(APP_CSP);
  expect(headers['x-powered-by']).toBeUndefined();
}

test('homepage sends the plain app CSP with its JSON-LD present', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  const html = await response!.text();
  expect(html).toContain('application/ld+json');
  await expect(page.getByTestId('landing')).toBeVisible();

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('the template gallery sends the plain app CSP with its JSON-LD '
  + 'present', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/templates');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  const html = await response!.text();
  expect(html).toContain('application/ld+json');
  await expect(page.getByTestId('template-gallery')).toBeVisible();

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('a template page sends the plain app CSP with its JSON-LD '
  + 'present', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  const html = await response!.text();
  expect(html).toContain('application/ld+json');
  await expect(page.locator('.template-detail__info h1')).toBeVisible();

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('the login page sends the plain app CSP', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/login');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('a same-origin /_nuxt/ script response carries the app CSP header too',
  async ({ page }) => {
    const probe = await trackCsp(page);
    const external = await denyExternalRequests(page);
    await mockSignedOutSession(page);

    const scriptUrls: string[] = [];
    page.on('response', (response) => {
      const url = response.url();
      if (/\/_nuxt\/.*\.m?js(\?|$)/.test(url)) scriptUrls.push(url);
    });

    const response = await page.goto('/login');
    expect(response?.status()).toBe(200);
    expectPlainAppCsp(response!.headers());
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    expect(scriptUrls.length).toBeGreaterThan(0);

    const scriptResponse = await page.request.get(scriptUrls[0]!);
    expect(scriptResponse.status()).toBe(200);
    expect(scriptResponse.headers()['content-security-policy']).toBe(
      APP_CSP,
    );

    await expectCspClean(probe, [ANONYMOUS_ME_401]);
    expect(external).toEqual([]);
  });

test('a 404 page sends the plain app CSP and never x-powered-by', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/templates/not-a-template');
  expect(response?.status()).toBe(404);
  expectPlainAppCsp(response!.headers());

  const html = await response!.text();
  const inlineScript
    = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g;
  const inlineScripts = [...html.matchAll(inlineScript)]
    .map((match) => match[0]);
  expect(inlineScripts).toEqual([]);

  await expectCspClean(probe, [ANONYMOUS_ME_401, DOCUMENT_404]);
  expect(probe.consoleErrors.filter((message) =>
    message.includes(DOCUMENT_404))).toHaveLength(1);
  expect(external).toEqual([]);
});

test('an unknown top-level path sends the plain app CSP', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/no-such-page');
  expect(response?.status()).toBe(404);
  expectPlainAppCsp(response!.headers());

  const html = await response!.text();
  const inlineScript
    = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g;
  const inlineScripts = [...html.matchAll(inlineScript)]
    .map((match) => match[0]);
  expect(inlineScripts).toEqual([]);

  await expectCspClean(probe, [ANONYMOUS_ME_401, DOCUMENT_404]);
  expect(probe.consoleErrors.filter((message) =>
    message.includes(DOCUMENT_404))).toHaveLength(1);
  expect(external).toEqual([]);
});

test('a client-side navigation to an unknown template raises no CSP '
  + 'violation', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await expect(page.locator('.template-detail__info h1')).toBeVisible();

  // Nuxt exposes window.useNuxtApp in production builds too.
  await page.evaluate(async () => {
    const nuxtApp = (window as unknown as {
      useNuxtApp: () => { $router: { push: (path: string) => Promise<void> } };
    }).useNuxtApp();
    await nuxtApp.$router.push('/templates/not-a-template');
  });
  await expect(page.getByTestId('error-page')).toBeVisible();

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('submitting the login form navigates cleanly under the app CSP', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedInSession(page);
  await page.route('**/api/v1/auth/password/login', async (route) => {
    await route.fulfill({ status: 204 });
  });
  await page.route('**/api/v1/resumes', async (route) => {
    await route.fulfill({
      headers: {
        'Cache-Control': 'no-store, no-transform',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: { data: [] },
    });
  });

  await page.goto('/login');
  await page.locator('#login-email').fill('csp@example.invalid');
  await page.locator('#login-password').fill('correct horse battery staple');
  await page.getByTestId('login-form').locator('button[type="submit"]')
    .click();
  await page.waitForURL((url) => url.pathname !== '/login');
  expect(new URL(page.url()).pathname).toBe('/app/resumes');
  expect(await probe.violations()).toEqual([]);
  await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();

  await expectCspClean(probe);
  expect(external).toEqual([]);
});

test('normal Nuxt output hydrates under the app CSP', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedInSession(page);
  await page.route('**/api/v1/resumes', async (route) => {
    await route.fulfill({
      headers: {
        'Cache-Control': 'no-store, no-transform',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: { data: [] },
    });
  });

  const response = await page.goto('/app/resumes');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();
  await expect.poll(() => page.evaluate(() =>
    Boolean((document.getElementById('__nuxt') as HTMLElement & {
      __vue_app__?: unknown;
    } | null)?.__vue_app__),
  )).toBe(true);
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));

  await page.getByTestId('account-menu').click();
  await expect(page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toBeHidden();

  await page.getByTestId('create-resume').click();
  await expect(
    page.getByRole('dialog', { name: 'Create resume' }),
  ).toBeVisible();

  await expectCspClean(probe);
  expect(external).toEqual([]);
});

test('the editor sends the plain app CSP and hydrates cleanly', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedInSession(page);
  // Chromium logs net::ERR_FAILED for an aborted EventSource request.
  await page.route('**/api/v1/events', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: '',
    });
  });
  await page.route('**/api/v1/resumes/resume-1', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      headers: {
        'Content-Type': 'application/json',
        'Cache-Control': 'no-store, no-transform',
        'ETag': '"r1"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: {
        data: {
          ...sampleMetadata,
          revision: '1',
          document: sampleDocument,
        },
      },
    });
  });

  const response = await page.goto('/app/resumes/resume-1');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  await expect(page.getByTestId('save-status')).toBeVisible();

  await expectCspClean(probe);
  expect(external).toEqual([]);
});

test('the editor with a photo present sends the plain app CSP and shows '
  + 'the photo under it', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedInSession(page);
  await page.route('**/api/v1/events', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: '',
    });
  });
  await page.route('**/api/v1/resumes/resume-1', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      headers: {
        'Content-Type': 'application/json',
        'Cache-Control': 'no-store, no-transform',
        'ETag': '"r1"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: {
        data: {
          ...sampleMetadata,
          revision: '1',
          document: fixtureDocument,
        },
      },
    });
  });
  await page.route('**/api/v1/resumes/resume-1/photo', async (route) => {
    const canvas = createCanvas(64, 64);
    canvas.getContext('2d').fillRect(0, 0, 64, 64);
    await route.fulfill({
      status: 200,
      contentType: 'image/png',
      headers: {
        'Cache-Control': 'no-store, no-transform',
        'ETag': '"photo-1"',
      },
      body: canvas.toBuffer('image/png'),
    });
  });

  const response = await page.goto('/app/resumes/resume-1');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  await expect(page.getByTestId('save-status')).toBeVisible();

  await page.locator('[data-action="open-photo"]').click();
  const photoImage = page.locator('[data-photo-image]');
  await expect(photoImage).toBeVisible();
  await expect(photoImage).toHaveAttribute('src', /^data:image\/png;base64,/);

  await expectCspClean(probe);
  expect(external).toEqual([]);
});

test('the publish dialog changes a published address after the second '
  + 'factor, under the plain app CSP', async ({ page }) => {
  test.setTimeout(40_000);
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedInSession(page);
  const noStore = { 'Cache-Control': 'no-store, no-transform' };
  const published = { ...sampleMetadata, live: true, slug: 'ada-lovelace' };
  const publishes: { key: string | null; ifMatch: string | null;
    body: unknown; }[] = [];
  const reauths: string[] = [];
  let factorComplete = false;
  await page.route('**/api/v1/events', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: '',
    });
  });
  await page.route('**/api/v1/resumes/resume-1', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      headers: {
        ...noStore,
        'Content-Type': 'application/json',
        'ETag': '"r1"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: { data: { ...published, revision: '1', document: sampleDocument } },
    });
  });
  await page.route('**/api/v1/auth/password/reauth', async (route) => {
    reauths.push(route.request().method());
    await route.fulfill({
      status: 202,
      headers: noStore,
      json: { data: { secondFactorRequired: true } },
    });
  });
  await page.route('**/api/v1/resumes/resume-1/publish', async (route) => {
    const request = route.request();
    publishes.push({
      key: request.headers()['idempotency-key'] ?? null,
      ifMatch: request.headers()['if-match'] ?? null,
      body: request.postDataJSON(),
    });
    if (!factorComplete) {
      await route.fulfill({
        status: 403,
        headers: noStore,
        json: { error: { code: 'reauth_required', message: 'fixed' } },
      });
      return;
    }
    await route.fulfill({
      status: 200,
      headers: {
        ...noStore,
        'Content-Type': 'application/json',
        'ETag': '"r2"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: {
        data: {
          ...published, slug: 'ada-v2', revision: '2', document: sampleDocument,
        },
      },
    });
  });

  await page.goto('/app/resumes/resume-1');
  await expect(page.getByTestId('save-status')).toBeVisible();
  await page.locator('[data-action="publish"]').click();
  const dialog = page.getByRole('dialog', { name: 'Publish resume' });
  await dialog.locator('[data-action="publish-slug"]').fill('ada-v2');
  await dialog.locator('[data-action="publish-submit"]').click();
  const password = dialog.locator('[data-action="publish-password"]');
  const reauthenticate = dialog.locator(
    '[data-action="publish-password-reauth"]',
  );
  const link = dialog.getByRole('link', { name: 'Open verification step' });
  const next = dialog.getByRole('button', { name: 'Continue', exact: true });
  await password.fill('correct horse');
  await reauthenticate.click();

  await expect(dialog).toContainText('Your account has a second verification '
    + 'step. Complete it in the new tab, then come back here.');
  await expect(link).toHaveAttribute('href', '/login/second-factor');
  await expect(link).toHaveAttribute('target', '_blank');
  await expect(link).toHaveAttribute('rel', 'noopener');
  await expect(password).toHaveCount(0);
  expect(publishes).toHaveLength(1);

  await next.click();
  await expect(dialog.getByText(
    'The second verification step is not complete yet.',
  )).toBeVisible();
  await expect(next).toHaveCount(0);
  expect(publishes).toHaveLength(2);
  await password.fill('correct horse');
  await reauthenticate.click();
  await expect(link).toBeVisible();
  expect(reauths).toHaveLength(2);
  expect(publishes).toHaveLength(2);

  factorComplete = true;
  await next.click();

  await expect(dialog.getByText('Published successfully.')).toBeVisible();
  await expect(dialog.locator('[data-action="view-public-resume"]'))
    .toHaveText('aboutme.vn/ada-v2');
  expect(publishes).toHaveLength(3);
  expect(new Set(publishes.map((entry) => entry.key)).size).toBe(1);
  expect(publishes.map((entry) => entry.ifMatch))
    .toEqual(['"r1"', '"r1"', '"r1"']);
  expect(publishes[2]?.body).toEqual(publishes[0]?.body);
  expect(publishes[0]?.body).toMatchObject({ slug: 'ada-v2', live: true });

  // Chromium logs "Failed to load resource" for each refused 403.
  await expectCspClean(probe, ['a status of 403']);
  expect(external).toEqual([]);
});

test('settings sends the plain app CSP and hydrates cleanly', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedInSession(page);
  await page.route('**/api/v1/sessions', async (route) => {
    await route.fulfill({ status: 200, json: { data: [] } });
  });
  await page.route('**/api/v1/me/second-factor', async (route) => {
    await route.fulfill({
      status: 200,
      json: {
        data: {
          enabled: false,
          passkeys: [],
          totpEnabled: false,
          recoveryCodesRemaining: 0,
        },
      },
    });
  });

  const response = await page.goto('/app/settings/sessions');
  expect(response?.status()).toBe(200);
  expectPlainAppCsp(response!.headers());
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  await expectCspClean(probe);
  expect(external).toEqual([]);
});

test('the header at 390px never overflows on /app/settings/sessions while '
  + '/me is still loading', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'vi',
    url: 'http://127.0.0.1:20092',
  }]);
  let releaseMe!: () => void;
  const meGate = new Promise<void>((resolve) => {
    releaseMe = resolve;
  });
  await page.route('**/api/v1/me', async (route) => {
    await meGate;
    await route.fulfill({
      status: 200,
      json: {
        data: {
          user: {
            id: 'csp-user',
            email: 'csp@example.invalid',
            name: 'CSP User',
            avatarKey: null,
            hasPassword: true,
          },
          csrfToken: 'csp-test-token',
          identities: [],
        },
      },
    });
  });
  await page.route('**/api/v1/sessions', async (route) => {
    await route.fulfill({ status: 200, json: { data: [] } });
  });
  await page.route('**/api/v1/me/second-factor', async (route) => {
    await route.fulfill({
      status: 200,
      json: {
        data: {
          enabled: false,
          passkeys: [],
          totpEnabled: false,
          recoveryCodesRemaining: 0,
        },
      },
    });
  });

  const response = await page.goto('/app/settings/sessions');
  expect(response?.status()).toBe(200);
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  expect(await page.getByTestId('account-menu').isVisible()).toBe(false);
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth)).toBe(0);

  releaseMe();
  await expect(page.getByTestId('account-menu')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth)).toBe(0);
});

test('the header at 390px never overflows on /app/resumes while /me settles '
  + 'to a 401 and the page redirects to /login', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'vi',
    url: 'http://127.0.0.1:20092',
  }]);
  let releaseMe!: () => void;
  const meGate = new Promise<void>((resolve) => {
    releaseMe = resolve;
  });
  await page.route('**/api/v1/me', async (route) => {
    await meGate;
    await route.fulfill({
      status: 401,
      json: { error: { code: 'session_required', message: 'Sign in.' } },
    });
  });

  const response = await page.goto('/app/resumes');
  expect(response?.status()).toBe(200);
  await expect(page.getByTestId('app-shell')).toBeVisible();

  const overflow = () => page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
  expect(await overflow()).toBe(0);

  releaseMe();
  const deadline = Date.now() + 5000;
  while (
    new URL(page.url()).pathname !== '/login' && Date.now() < deadline
  ) {
    expect(await overflow()).toBe(0);
  }
  expect(await overflow()).toBe(0);
  expect(new URL(page.url()).pathname).toBe('/login');
});

test('the header at 390px never overflows on /app/settings/sessions while '
  + '/me settles to a 401 and the page redirects to /login', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'vi',
    url: 'http://127.0.0.1:20092',
  }]);
  await page.route('**/api/v1/capabilities', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { providerLogin: false, agentAccess: false },
      }),
    });
  });
  let releaseMe!: () => void;
  const meGate = new Promise<void>((resolve) => {
    releaseMe = resolve;
  });
  await page.route('**/api/v1/me', async (route) => {
    await meGate;
    await route.fulfill({
      status: 401,
      json: { error: { code: 'session_required', message: 'Sign in.' } },
    });
  });

  const response = await page.goto('/app/settings/sessions');
  expect(response?.status()).toBe(200);
  await expect(page.getByTestId('app-shell')).toBeVisible();

  const overflow = () => page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
  expect(await overflow()).toBe(0);

  releaseMe();
  const deadline = Date.now() + 5000;
  while (
    new URL(page.url()).pathname !== '/login' && Date.now() < deadline
  ) {
    expect(await overflow()).toBe(0);
  }
  expect(await overflow()).toBe(0);
  expect(new URL(page.url()).pathname).toBe('/login');
  expect(new URL(page.url()).searchParams.get('next')).toBe(
    '/app/settings/sessions',
  );
});

test('a signed-out sample link reaches /register when /me settles before '
  + 'the page loads', async ({ page }) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedOutSession(page);
  let clicked = false;
  let meAnswered!: () => void;
  const meAfterClick = new Promise<void>((resolve) => {
    meAnswered = resolve;
  });
  await page.route('**/api/v1/me', async (route) => {
    await route.fulfill({
      status: 401,
      json: { error: { code: 'session_required', message: 'Sign in.' } },
    });
    if (clicked) meAnswered();
  });
  await page.route('**/_nuxt/**', async (route) => {
    if (clicked) {
      await meAfterClick;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
    await route.continue();
  });

  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await expect.poll(() => page.evaluate(() =>
    Boolean((document.getElementById('__nuxt') as HTMLElement & {
      __vue_app__?: unknown;
    } | null)?.__vue_app__),
  )).toBe(true);
  const link = page.locator('[data-action="use-sample"]');
  const href = await link.getAttribute('href');
  expect(href).toBe('/app/new?sample=engineer-compact&lng=en');

  clicked = true;
  await link.click();
  await page.waitForURL((url) => url.pathname === '/register');
  expect(new URL(page.url()).searchParams.get('next')).toBe(href);
});

test('the create resume dialog fits a 1280x720 viewport and scrolls', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedInSession(page);
  await page.route('**/api/v1/resumes', async (route) => {
    await route.fulfill({
      headers: {
        'Cache-Control': 'no-store, no-transform',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: { data: [] },
    });
  });

  await page.goto('/app/resumes');
  await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();
  await page.getByTestId('create-resume').click();
  const dialog = page.getByRole('dialog', { name: 'Create resume' });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('[data-create-mode="sample"]'))
    .toHaveAttribute('aria-pressed', 'true');
  await dialog.evaluate((element) => Promise.all(
    element.getAnimations().map((animation) => animation.finished),
  ));

  const box = await dialog.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.y + box!.height).toBeLessThanOrEqual(720);
  await expect(dialog.getByRole('heading', { name: 'Create resume' }))
    .toBeInViewport();

  const submit = dialog.locator('[data-action="create-submit"]');
  await submit.scrollIntoViewIfNeeded();
  await expect(submit).toBeInViewport();

  const blank = dialog.locator('[data-create-mode="blank"]');
  await blank.click();
  await expect(blank).toHaveAttribute('aria-pressed', 'true');
});
