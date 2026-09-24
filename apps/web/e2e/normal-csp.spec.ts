import { expect, test } from '@playwright/test';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { readFileSync } from 'node:fs';
import { resolve as resolvePath } from 'node:path';

import { APP_CSP } from '../app/utils/csp';
import {
  jsonLdScriptContent,
  scriptHashSource,
  withScriptSource,
} from '../server/utils/cspHash';
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

// The schema-current golden fixture (part of the reviewed e2e source set;
// packages/schema/fixtures/full.json), used as the editor test's mocked
// resume body with its photo stripped: the editor watches
// document.personalDetails.photo?.key and fetches the owner photo when it is
// present, which this test does not also mock. The client validates the rest
// itself on read (app/editor/resumeApi.ts's parseAcceptedResponse), so this
// file need not duplicate that validation.
const sampleDocument = JSON.parse(
  readFileSync(
    resolvePath(
      import.meta.dirname,
      '../../../packages/schema/fixtures/full.json',
    ),
    'utf8',
  ),
) as { personalDetails?: { photo?: unknown } };
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
// (app/utils/csp.ts) own the policy this proves.

/** The CSP header a page with no inline script sends: APP_CSP unchanged. */
function expectPlainAppCsp(headers: Record<string, string>): void {
  expect(headers['content-security-policy']).toBe(APP_CSP);
  expect(headers['x-powered-by']).toBeUndefined();
}

/**
 * The CSP header a page with one inline JSON-LD script sends: APP_CSP with
 * that exact script's own hash added to script-src, computed the same way
 * server/plugins/security-headers.ts computes it.
 */
async function expectHashedAppCsp(
  headers: Record<string, string>,
  html: string,
): Promise<void> {
  const content = jsonLdScriptContent(html);
  expect(content).not.toBeNull();
  const expected = withScriptSource(
    APP_CSP,
    scriptHashSource(content as string),
  );
  expect(headers['content-security-policy']).toBe(expected);
  expect(headers['x-powered-by']).toBeUndefined();
}

test('homepage sends the app CSP with its JSON-LD script hashed', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  await expectHashedAppCsp(response!.headers(), await response!.text());
  await expect(page.getByTestId('landing')).toBeVisible();

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('a template page sends the CSP with its JSON-LD script hashed', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);

  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await expectHashedAppCsp(response!.headers(), await response!.text());
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
  await page.route('**/api/v1/events', async (route) => {
    await route.abort();
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
