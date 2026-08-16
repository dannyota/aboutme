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

    // Publish requires at least a full name; fill it and wait for the autosave.
    await page
      .getByRole('navigation', { name: 'Resume outline' })
      .getByRole('button', { name: 'Personal details', exact: true })
      .press('Enter');
    await page.getByLabel('Full name').fill('Public proof resume');
    await page.getByLabel('Full name').press('Tab');
    await expect(page.locator('[data-state="saved"]')).toBeVisible();

    // Add one work section and entry, so the resume meets the publish
    // completeness minimum (a full name plus at least one visible entry).
    await page.getByRole('button', { name: 'Structure' }).press('Enter');
    await page.getByLabel('Section type').selectOption('work');
    await page.locator('[data-action="create"]').press('Enter');
    await expect(page.locator('[data-state="saved"]')).toBeVisible();
    await page.getByRole('button', { name: 'Document' }).press('Enter');
    await page
      .getByRole('navigation', { name: 'Resume outline' })
      .getByRole('button', { name: 'Experience' })
      .press('Enter');
    await page.getByRole('button', { name: 'Add entry' }).press('Enter');
    await page.locator('[data-entry-id]').first().getByLabel('Job title').fill('Engineer');
    await page.locator('[data-entry-id]').first().getByLabel('Job title').press('Tab');
    await page.locator('[data-entry-id]').first().getByLabel('Employer', { exact: true }).fill('Example Corp');
    await page.locator('[data-entry-id]').first().getByLabel('Employer', { exact: true }).press('Tab');
    await expect(page.locator('[data-state="saved"]')).toBeVisible();

    // The autosave advanced the revision; read the current one for If-Match.
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
          'X-Resume-Schema-Version': input.schemaVersion,
        },
        body: JSON.stringify({
          slug: input.slug,
          live: true,
          downloadEnabled: true,
          seoGeoEnabled: true,
        }),
      });
      const body = await response.json().catch(() => null);
      return { status: response.status, body };
    }, { id: createdID, revision: currentRevision, csrf, slug: publishedSlug, schemaVersion: SCHEMA_VERSION });
    expect(publishStatus.status, JSON.stringify(publishStatus.body)).toBe(200);

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

  await writeFile(EVIDENCE_PATH, `${JSON.stringify({
    schemaVersion: 1,
    scenario: 'public-resume-hydration',
    origin: ORIGIN,
    errors: { console: consoleErrors.length, externalRequest: externalRequests.length, page: pageErrors.length },
    steps: { published: true, ssr: true, hydrated: true },
  })}\n`, { flag: 'wx', mode: 0o600 });
});
