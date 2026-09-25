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

// A real anonymous visit already logs the browser's own "Failed to load
// resource" message for GET /api/v1/me's expected 401 (useAuth.ts); it is
// unrelated to CSP, so the signed-out page tests below allow it. Matched by
// status code, not URL: the browser's message text embeds the mocked
// response's status line, not always the request path.
const ANONYMOUS_ME_401 = 'a status of 401';

// The browser logs its own "Failed to load resource" message for a document
// served with status 404; the not-found tests below expect that status.
const DOCUMENT_404 = 'a status of 404';

// The schema-current golden fixture (part of the reviewed e2e source set;
// packages/schema/fixtures/full.json), used as the editor tests' mocked
// resume body. The client validates the rest itself on read
// (app/editor/resumeApi.ts's parseAcceptedResponse), so this file need not
// duplicate that validation.
const fixtureDocument = JSON.parse(
  readFileSync(
    resolvePath(
      import.meta.dirname,
      '../../../packages/schema/fixtures/full.json',
    ),
    'utf8',
  ),
) as { personalDetails?: { photo?: { key: string } } };

// A clone with the photo stripped: the editor watches
// document.personalDetails.photo?.key and fetches the owner photo when it is
// present, which the plain editor test below does not also mock.
const sampleDocument = JSON.parse(JSON.stringify(fixtureDocument)) as {
  personalDetails?: { photo?: unknown };
};
delete sampleDocument.personalDetails?.photo;

// Matches app/editor/types.ts's ResumeMetadata; the wire shape
// app/editor/resumeApi.ts's read() expects nested under `document` and
// `revision` (app/editor/resumeApiParsing.ts's parseSummary).
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

// Every Nuxt-rendered page in the production build sends the app-page CSP
// (nuxt.config.ts routeRules) and never x-powered-by
// (server/plugins/security-headers.ts), and no real interaction on any of
// them trips a CSP violation. docs/design/security.md and the CSP itself
// (app/utils/csp.ts) own the policy this proves. The homepage and the
// template pages each render their JSON-LD as a
// `<script type="application/ld+json">` data block (app/landing/
// structuredData.ts, app/templates/structuredData.ts); per the HTML spec, the
// browser never executes a data block
// (https://html.spec.whatwg.org/multipage/scripting.html#data-block), so
// those pages send this same, unmodified policy too.

/** The CSP header every app page sends: APP_CSP unchanged. */
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

test('a 404 page sends the plain app CSP and never x-powered-by', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/templates/not-a-template');
  expect(response?.status()).toBe(404);
  expectPlainAppCsp(response!.headers());

  // A failure here should name the exact inline script CSP blocked, not
  // just that one was blocked: server/utils/cspExternalize.ts externalizes
  // Nuxt's own hydration payload for every response, so any inline
  // `<script>` a 404 response still carries is worth seeing verbatim.
  const html = await response!.text();
  const inlineScript
    = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g;
  const inlineScripts = [...html.matchAll(inlineScript)]
    .map((match) => match[0]);
  expect(inlineScripts).toEqual([]);

  await expectCspClean(probe, [ANONYMOUS_ME_401, DOCUMENT_404]);
  // Only the document itself may be a 404; a missing chunk or asset would
  // log a second one.
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
  // Only the document itself may be a 404; a missing chunk or asset would
  // log a second one.
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

  // A client-side route change (no full navigation, so no fresh
  // Content-Security-Policy header): Nuxt exposes its running app instance
  // as window.useNuxtApp on every build (not just dev), and the router it
  // resolves is the one already handling this page, so pushing an unknown
  // template path here reruns app/pages/templates/[id].vue's setup() and
  // its createError({ statusCode: 404 }) client-side, which Nuxt then shows
  // through app/error.vue without a page reload.
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
  // A real successful login sets a session cookie and lands on /app/resumes
  // through a full reload (crossing from the SSR /login to the client-only
  // /app/resumes route rule), so /api/v1/me reads as signed in throughout,
  // exactly as the real destination page would see it.
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
  // Check for a real CSP violation before the visibility assertion below,
  // which throws first (and would otherwise hide it) when the navigation
  // landed but the destination page failed to render.
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
  // A successful, immediately closed stream: EventSource retries silently
  // when its connection ends, but an aborted request logs the browser's own
  // "Failed to load resource: net::ERR_FAILED", which is unrelated to CSP.
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
  // The owner photo the editor fetches for the fixture's
  // personalDetails.photo.key and converts to a `data:` URL
  // (pages/app/resumes/[id].vue's bytesToDataURL) for the preview and photo
  // panel: this exercises img-src's `data:` source, which the plain editor
  // test above never does because it strips the photo.
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

// AppShell.vue keeps the signed-out "Tạo tài khoản" / "Đăng nhập" links
// unhidden on phones for this route's header (there is no other in-page
// account affordance), so while its own /api/v1/me read is still in flight,
// those wider signed-out links must not appear in place of the eventual
// account menu: at 390px they push the header past the viewport.
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
