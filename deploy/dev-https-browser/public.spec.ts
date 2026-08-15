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
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/public-proof.json';
const SCHEMA_VERSION = '2';

test('proves a published resume hydrates in a real browser', async ({
  browser,
  page,
}) => {
  const consoleErrors: string[] = [];
  const dialogs: string[] = [];
  const pageErrors: string[] = [];
  const externalRequests: string[] = [];

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

  let createdID: string | undefined;
  let publishedSlug: string | undefined;

  try {
    await loginAsDevelopmentUser(page);
    const created = await createBlankResume(page, uniqueTitle());
    createdID = created.metadata.id;
    publishedSlug = `public-${crypto.randomUUID().slice(0, 8)}`;

    // Publish: the resume must already hold a live slug before any public
    // route will serve it.
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
          'X-Resume-Schema-Version': SCHEMA_VERSION,
        },
        body: JSON.stringify({
          slug: input.slug,
          live: true,
          downloadEnabled: true,
          seoGeoEnabled: true,
        }),
      });
      return response.status;
    }, { id: createdID, revision: created.revision, csrf, slug: publishedSlug });
    expect(publishStatus).toBe(200);

    // Prove the page in a fresh context with no session cookies.
    const publicContext = await browser.newContext();
    const publicPage = await publicContext.newPage();
    attachDiagnostics(publicPage);
    await publicContext.route('**/*', async (route) => {
      const url = new URL(route.request().url());
      if (!isAllowedHTTPURL(url.href) && !isAllowedWebSocketURL(url.href)) {
        externalRequests.push(url.href);
        await route.abort('blockedbyclient');
        return;
      }
      await route.continue();
    });

    const response = await publicPage.goto(`${ORIGIN}/${publishedSlug}`);
    expect(response?.status()).toBe(200);
    expect(response?.headers()['content-security-policy']).toContain("default-src 'none'");

    // SSR markup is present before hydration runs.
    const main = publicPage.locator('#public-resume');
    await expect(main).toBeVisible();
    await expect(main).toHaveAttribute('data-revision', /^[1-9][0-9]*$/);
    await expect(publicPage).toHaveTitle(/Resume$/);

    // The client hydration mounts the Vue app on the SSR root.
    await expect.poll(() =>
      publicPage.evaluate(() =>
        Boolean(
          (document.getElementById('public-resume') as HTMLElement & {
            __vue_app__?: unknown;
          } | null)?.__vue_app__,
        ),
      ),
    ).toBe(true);

    await publicContext.close();
  } finally {
    if (createdID !== undefined) {
      await deleteRecordedResume(page, createdID);
    }
  }

  expect(consoleErrors).toEqual([]);
  expect(dialogs).toEqual([]);
  expect(pageErrors).toEqual([]);
  expect(externalRequests).toEqual([]);

  await writeFile(EVIDENCE_PATH, JSON.stringify({
    scenario: 'public-resume-hydration',
    schemaVersion: 1,
    origin: ORIGIN,
    errors: { console: consoleErrors.length, externalRequest: externalRequests.length, page: pageErrors.length },
    steps: { published: true, ssr: true, hydrated: true },
  }));
});
