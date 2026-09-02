import { expect, test, type Page } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  waitForHydration,
} from './harness-lib';
import { ALLOWED_ORIGIN } from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/entry-proof.json';

// The seed identities are frozen by docs/plans/phase-pf/design.md D7 and
// pinned by apps/server/cmd/dev-seed/seed_test.go.
const SEED_EMAIL = 'dev@aboutme.invalid';
const SEED_PASSWORD = 'aboutme-dev-password-1';

const steps = {
  landing: false,
  providerLinks: false,
  resumeList: false,
  signIn: false,
  signedInShell: false,
};

async function expectSignedOutShell(page: Page): Promise<void> {
  const header = page.getByRole('banner');
  await expect(header.getByRole('link', { name: 'Sign in' })).toBeVisible();
  await expect(
    header.getByRole('link', { name: 'Create account' }),
  ).toBeVisible();
  await expect(header.getByRole('link', { name: 'Resumes' })).toHaveCount(0);
  await expect(header.getByRole('link', { name: 'Settings' })).toHaveCount(0);
}

test('landing, sign-in, and the signed-in shell', async ({ browser }) => {
  const counters = newDiagnosticCounters();
  const context = await browser.newContext();
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  const page = await context.newPage();
  pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedConsoleError,
  })(page);

  try {
    await page.goto(`${ORIGIN}/`);
    await waitForHydration(page);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(
      'Build your resume. Publish it at its own link.',
    );
    const main = page.getByRole('main');
    await expect(main.getByRole('link', { name: 'Sign in' })).toHaveAttribute(
      'href',
      '/login',
    );
    await expect(
      main.getByRole('link', { name: 'Create account' }),
    ).toHaveAttribute('href', '/register');
    await expectSignedOutShell(page);
    steps.landing = true;

    await main.getByRole('link', { name: 'Sign in' }).click();
    await expect(page).toHaveURL(`${ORIGIN}/login`);
    await waitForHydration(page);
    // The harness runs with PROVIDER_LOGIN_ENABLED=true, so the capabilities
    // read must surface all three provider links.
    await expect(page.locator('a[href^="/api/v1/auth/"]')).toHaveCount(3);
    steps.providerLinks = true;

    await page.getByRole('textbox', { name: 'Email' }).fill(SEED_EMAIL);
    await page
      .getByRole('textbox', { name: 'Password', exact: true })
      .fill(SEED_PASSWORD);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page).toHaveURL(`${ORIGIN}/app/resumes`);
    steps.signIn = true;

    await expect(
      page.getByRole('heading', { level: 1, name: 'Resumes' }),
    ).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'Create resume' }),
    ).toBeVisible();
    steps.resumeList = true;

    const header = page.getByRole('banner');
    await expect(header.getByRole('link', { name: 'Resumes' })).toBeVisible();
    await expect(
      header.getByRole('link', { name: 'Settings', exact: true }),
    ).toBeVisible();
    await expect(
      header.getByRole('link', { name: /Account settings for Dev User/ }),
    ).toBeVisible();
    await expect(header.getByRole('link', { name: 'Sign in' })).toHaveCount(0);
    steps.signedInShell = true;

    // Sign out through settings so the proof leaves no session.
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    await waitForHydration(page);
    await page
      .getByRole('button', { name: 'Log out', exact: true })
      .first()
      .click();
    await expect(page).toHaveURL(`${ORIGIN}/login`);
  } finally {
    const evidence = {
      errors: {
        certificate: counters.certificateErrors,
        console: counters.consoleErrors,
        externalRequest: counters.externalRequests,
        page: counters.pageErrors,
      },
      origin: ORIGIN,
      scenario: 'entry-flow',
      schemaVersion: 1,
      steps,
    };
    await writeFile(EVIDENCE_PATH, JSON.stringify(evidence), {
      flag: 'wx',
      mode: 0o600,
    });
    await context.close();
  }

  expect(counters.certificateErrors).toBe(0);
  expect(counters.consoleErrors).toBe(0);
  expect(counters.externalRequests).toBe(0);
  expect(counters.pageErrors).toBe(0);
});
