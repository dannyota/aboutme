import { expect, test } from '@playwright/test';
import { writeFile } from 'node:fs/promises';

import {
  createBlankResume,
  deleteRecordedResume,
  freshCSRF,
  loginAsDevelopmentUser,
  uniqueTitle,
} from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pinEnglish,
  waitForHydration,
} from './harness-lib';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/public-proof.json';
const SCHEMA_VERSION = '4';
const CUSTOM_LINK = 'https://orcid.example/0000-0001';
const PAGE_TITLE = 'Danny from aboutme.vn';
const PAGE_EMOJI = '\u{1F680}';
const PDF_NAME = 'Public-proof-resume-Resume.pdf';
let createdID: string | undefined;

function stage(name: string): void {
  console.log('public-stage:' + name);
}

test.afterEach(async ({ browser }, testInfo) => {
  if (createdID === undefined) return;
  testInfo.setTimeout(testInfo.timeout + 30_000);
  const cleanupID = createdID;
  const cleanupCounters = newDiagnosticCounters();
  try {
    const cleanupContext = await browser.newContext();
    await installExternalRequestFirewall(cleanupContext, cleanupCounters);
    await installExternalWebSocketFirewall(cleanupContext, cleanupCounters);
    await pinEnglish(cleanupContext);
    try {
      const cleanupPage = await cleanupContext.newPage();
      await loginAsDevelopmentUser(cleanupPage);
      await deleteRecordedResume(cleanupPage, cleanupID);
      expect(cleanupCounters.externalRequests).toBe(0);
      createdID = undefined;
    } finally {
      await cleanupContext.close();
    }
  } catch (error) {
    stage('cleanup-failed');
    throw error;
  }
});

test('proves a published resume hydrates in a real browser', async ({
  browser,
  page,
}) => {
  const consoleErrors: string[] = [];
  const dialogs: string[] = [];
  const pageErrors: string[] = [];
  const externalRequests: string[] = [];
  const privateRequests: string[] = [];

  const attachDiagnostics = (openedPage: typeof page): void => {
    openedPage.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text());
    });
    openedPage.on('dialog', async (dialog) => {
      dialogs.push(`${dialog.type()}:${dialog.message()}`);
      await dialog.dismiss();
    });
    openedPage.on('pageerror', (error) => pageErrors.push(error.message));
  };

  let publishedSlug: string | undefined;

  {
    createdID = undefined;
    stage('locale');
    await pinEnglish(page.context());
    stage('sign-in');
    await loginAsDevelopmentUser(page);
    stage('create-resume');
    const created = await createBlankResume(
      page,
      uniqueTitle(),
      (createStage) => {
        stage('create-' + createStage);
      },
      (accepted) => {
        createdID = accepted.metadata.id;
      },
    );
    createdID = created.metadata.id;
    publishedSlug = `public-${crypto.randomUUID().slice(0, 8)}`;

    // Publish requires at least a full name; fill it and wait for the autosave.
    stage('personal-details');
    await page.locator('[data-action="open-document"]').press('Enter');
    await page.locator('[data-field="fullName"] [data-field-input]')
      .fill('Public proof resume');
    await page.locator('[data-field="fullName"] [data-field-input]').press('Tab');
    await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');

    // Add one work section and entry, so the resume meets the publish
    // completeness minimum (a full name plus at least one visible entry).
    stage('work-entry');
    await page.locator('[data-action="open-structure"]').press('Enter');
    await page.locator('[data-action="section-type"]').selectOption('work');
    await page
      .getByTestId('section-create-form')
      .locator('[data-action="create"]')
      .press('Enter');
    await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');
    await page.locator('[data-outline-key="work"]').press('Enter');
    await page.locator('[data-action="add-entry"]').press('Enter');
    const entry = page.locator('[data-entry-id]').first();
    await entry.locator('[data-entry-field="jobTitle"] [data-field-input]')
      .fill('Engineer');
    await entry.locator('[data-entry-field="jobTitle"] [data-field-input]').press('Tab');
    await entry.locator('[data-entry-field="employer"] [data-field-input]')
      .fill('Example Corp');
    await entry.locator('[data-entry-field="employer"] [data-field-input]').press('Tab');
    await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');

    // The autosave advanced the revision; read the current one for If-Match.
    stage('read-revision');
    const currentRevision = await page.evaluate(async (id) => {
      const response = await fetch(`/api/v1/resumes/${id}`, {
        credentials: 'include',
        cache: 'no-store',
      });
      const body = await response.json() as { data?: { revision?: unknown } };
      const revision = body.data?.revision;
      if (response.status !== 200 || typeof revision !== 'string') {
        throw new Error('resume read failed');
      }
      return revision;
    }, createdID);

    // A custom https contact with discovery on: the Go validator requires the
    // renderer's JSON-LD sameAs to match its own, custom links included.
    stage('write-contacts');
    const detailCSRF = await freshCSRF(page);
    const detailWrite = await page.evaluate(async (input) => {
      const response = await fetch(`/api/v1/resumes/${input.id}/personal-details`, {
        method: 'PATCH',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': `"r${input.revision}"`,
          'X-CSRF-Token': input.csrf,
          'X-Resume-Schema-Version': input.schemaVersion,
        },
        body: JSON.stringify({
          fullName: 'Public proof resume',
          details: [{
            id: crypto.randomUUID(),
            type: 'custom',
            label: 'ORCID',
            value: input.customLink,
            isHidden: false,
          }, {
            id: crypto.randomUUID(),
            type: 'email',
            value: 'proof@example.com',
            isHidden: false,
          }, {
            id: crypto.randomUUID(),
            type: 'phone',
            value: '(+84) 374837720',
            isHidden: false,
          }],
        }),
      });
      const body = await response.json() as { data?: { revision?: unknown } };
      return { status: response.status, revision: body.data?.revision };
    }, {
      id: createdID,
      revision: currentRevision,
      csrf: detailCSRF,
      schemaVersion: SCHEMA_VERSION,
      customLink: CUSTOM_LINK,
    });
    expect(detailWrite.status).toBe(200);
    expect(typeof detailWrite.revision).toBe('string');

    // Publish: the resume must already hold a live slug before any public
    // route will serve it.
    stage('publish');
    const csrf = await freshCSRF(page);
    const publishStatus = await page.evaluate(async (input) => {
      const response = await fetch(`/api/v1/resumes/${input.id}/publish`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': `"r${input.revision}"`,
          'X-CSRF-Token': input.csrf,
          'X-Resume-Schema-Version': input.schemaVersion,
        },
        body: JSON.stringify({
          slug: input.slug,
          live: true,
          downloadEnabled: true,
          seoGeoEnabled: true,
          publicTitle: input.publicTitle,
          faviconEmoji: input.faviconEmoji,
        }),
      });
      const body = await response.json().catch(() => null);
      return { status: response.status, body };
    }, {
      id: createdID,
      revision: detailWrite.revision as string,
      csrf,
      slug: publishedSlug,
      schemaVersion: SCHEMA_VERSION,
      publicTitle: PAGE_TITLE,
      faviconEmoji: PAGE_EMOJI,
    });
    expect(publishStatus.status, JSON.stringify(publishStatus.body)).toBe(200);

    // Prove the page in a fresh context with no session cookies.
    stage('public-context');
    const publicContext = await browser.newContext();
    const publicPage = await publicContext.newPage();
    attachDiagnostics(publicPage);
    // A signed-out public page may call only public endpoints, and none may
    // answer as if a session were expected.
    publicPage.on('response', (publicResponse) => {
      const url = new URL(publicResponse.url());
      if (url.origin !== ORIGIN) return;
      if (url.pathname.startsWith('/api/')
        && !url.pathname.startsWith('/api/v1/public/')
        && !url.pathname.startsWith('/api/v1/live/')) {
        privateRequests.push(url.pathname);
      }
      if (publicResponse.status() === 401 || publicResponse.status() === 403) {
        privateRequests.push(`${publicResponse.status()} ${url.pathname}`);
      }
    });
    await publicContext.route('**/*', async (route) => {
      const url = new URL(route.request().url());
      if (!isAllowedHTTPURL(url.href) && !isAllowedWebSocketURL(url.href)) {
        externalRequests.push(url.href);
        await route.abort('blockedbyclient');
        return;
      }
      await route.continue();
    });

    stage('public-navigation');
    const response = await publicPage.goto(`${ORIGIN}/${publishedSlug}`);
    expect(response?.status()).toBe(200);
    const structured = await publicPage
      .locator('script[type="application/ld+json"]')
      .textContent();
    expect(JSON.parse(structured ?? '{}').mainEntity?.sameAs).toEqual([CUSTOM_LINK]);
    await expect(publicPage.locator(`a[href="${CUSTOM_LINK}"]`)).toHaveText('orcid.example/0000-0001');
    // Email and phone link after the server-checked rules (ADR 0043).
    await expect(publicPage.locator('a[href="mailto:proof@example.com"]')).toHaveText('proof@example.com');
    await expect(publicPage.locator('a[href="tel:+84374837720"]')).toHaveText('(+84) 374837720');

    stage('public-head');
    // The owner's title and emoji favicon pass the server's exact-head check.
    await expect(publicPage).toHaveTitle(PAGE_TITLE);
    const icon = publicPage.locator('link[rel="icon"]');
    await expect(icon).toHaveCount(1);
    const iconHref = await icon.getAttribute('href');
    expect(iconHref?.startsWith('data:image/svg+xml,')).toBe(true);
    expect(decodeURIComponent(iconHref!.slice('data:image/svg+xml,'.length)))
      .toContain(`>${PAGE_EMOJI}</text>`);

    // Every public page credits the site once, linking the canonical home.
    const credit = publicPage.locator('a.public-credit');
    await expect(credit).toHaveCount(1);
    await expect(credit).toHaveAttribute('href', `${new URL(publicPage.url()).origin}/`);
    await expect(credit).toHaveText('Built with aboutme.vn');

    // Download is enabled, so the page links its own PDF.
    stage('public-download');
    const download = publicPage.locator('a.public-download');
    await expect(download).toHaveCount(1);
    await expect(download).toHaveAttribute('href', `/api/v1/public/resumes/${publishedSlug}/pdf`);
    await expect(download).toHaveText('Download PDF');
    const pdf = await publicPage.evaluate(async (href) => {
      const response = await fetch(href, { cache: 'no-store' });
      return {
        status: response.status,
        type: response.headers.get('content-type'),
        disposition: response.headers.get('content-disposition'),
      };
    }, `/api/v1/public/resumes/${publishedSlug}/pdf`);
    expect(pdf).toEqual({
      status: 200,
      type: 'application/pdf',
      disposition: `attachment; filename="${PDF_NAME}"; filename*=UTF-8''${PDF_NAME}`,
    });
    await publicPage.emulateMedia({ media: 'print' });
    await expect(download).toBeHidden();
    await publicPage.emulateMedia({ media: 'screen' });
    expect(response?.headers()['content-security-policy']).toContain("default-src 'none'");

    // SSR markup is present before hydration runs.
    stage('public-ssr');
    const main = publicPage.locator('#public-resume');
    await expect(main).toBeVisible();
    await expect(main).toHaveAttribute('data-revision', /^[1-9][0-9]*$/);
    await expect(publicPage).toHaveTitle(PAGE_TITLE);

    // The client hydration mounts the Vue app on the SSR root.
    stage('public-hydration');
    await waitForHydration(publicPage, 'public-resume');

    // The template stylesheets load under the page CSP and style the resume;
    // DOM presence alone would pass on an unstyled page.
    stage('public-styling');
    const styling = await publicPage.evaluate(() => {
      const sheets = [...document.styleSheets].map((sheet) => ({
        href: sheet.href === null ? '' : new URL(sheet.href).pathname,
        search: sheet.href === null ? '' : new URL(sheet.href).search,
        rules: sheet.cssRules.length,
      }));
      const resume = document.querySelector('.resume-document');
      if (resume === null) return { sheets, resume: null };
      const computed = getComputedStyle(resume);
      return {
        sheets,
        resume: {
          boxSizing: computed.boxSizing,
          paddingLeft: computed.paddingLeft,
          fontFamily: computed.fontFamily,
          fontVariable: computed.getPropertyValue('--font-family').trim(),
        },
      };
    });
    for (const path of [
      '/_nuxt/assets/print-fonts.css',
      '/_nuxt/assets/print.css',
    ]) {
      const sheet = styling.sheets.find((candidate) => candidate.href === path);
      expect(sheet?.rules ?? 0, `${path} loaded with rules`).toBeGreaterThan(0);
      // A per-release version defeats the year-long immutable cache.
      expect(sheet?.search, `${path} version`).toMatch(/^\?v=[0-9a-f]{16}$/u);
    }
    // The hydration script is versioned too, so a returning browser never
    // runs a previous release's cached script against new HTML.
    const scriptURL = await publicPage
      .locator('script[type="module"]')
      .getAttribute('src');
    expect(scriptURL).toMatch(
      /^\/_nuxt\/assets\/public-resume\.mjs\?v=[0-9a-f]{16}$/u,
    );
    expect(styling.resume).not.toBeNull();
    expect(styling.resume?.boxSizing).toBe('border-box');
    expect(styling.resume?.paddingLeft).not.toBe('0px');
    expect(styling.resume?.fontVariable).not.toBe('');
    // Computed font-family drops quotes around single-word family names.
    const unquoted = (value?: string): string => (value ?? '').replaceAll('"', '');
    expect(unquoted(styling.resume?.fontFamily)).toBe(unquoted(styling.resume?.fontVariable));

    // The skip link stays out of view until keyboard focus reaches it.
    stage('public-skip-link');
    const skip = publicPage.getByRole('link', { name: 'Skip to content' });
    const hidden = await skip.boundingBox();
    expect(hidden === null || (hidden.width <= 1 && hidden.height <= 1)).toBe(true);
    await publicPage.keyboard.press('Tab');
    await expect(skip).toBeFocused();
    const shown = await skip.boundingBox();
    expect(shown?.width ?? 0).toBeGreaterThan(1);
    expect(shown?.height ?? 0).toBeGreaterThan(1);

    await publicContext.close();
  }

  expect(consoleErrors).toEqual([]);
  expect(dialogs).toEqual([]);
  expect(pageErrors).toEqual([]);
  expect(externalRequests).toEqual([]);
  expect(privateRequests).toEqual([]);

  await writeFile(EVIDENCE_PATH, `${JSON.stringify({
    schemaVersion: 1,
    scenario: 'public-resume-hydration',
    origin: ORIGIN,
    errors: { console: consoleErrors.length, externalRequest: externalRequests.length, page: pageErrors.length },
    steps: { published: true, ssr: true, hydrated: true },
  })}\n`, { flag: 'wx', mode: 0o600 });
});
