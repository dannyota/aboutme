import { AxeBuilder } from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  waitForHydration,
} from './harness-lib';
import { ALLOWED_ORIGIN, isExpectedAnonymousMeConsole } from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/entry-proof.json';

// The seed identities are frozen by docs/plans/phase-pf/design.md D7 and
// pinned by apps/server/cmd/dev-seed/seed_test.go.
const SEED_EMAIL = 'dev@aboutme.invalid';
const SEED_PASSWORD = 'aboutme-dev-password-1';
const THEMES = ['light', 'dark'] as const;
type Theme = (typeof THEMES)[number];

const steps = {
  landing: false,
  providerLinks: false,
  resumeList: false,
  signIn: false,
  signOut: false,
  signedInShell: false,
};

function stage(name: string): void {
  console.log(`entry-stage:${name}`);
}

async function expectSignedOutShell(page: Page): Promise<void> {
  const header = page.getByRole('banner');
  await expect(header.getByRole('link', { name: 'Sign in' })).toBeVisible();
  await expect(
    header.getByRole('link', { name: 'Create account' }),
  ).toBeVisible();
  await expect(header.getByRole('link', { name: 'Resumes' })).toHaveCount(0);
  await expect(header.getByRole('link', { name: 'Settings' })).toHaveCount(0);
}

async function setTheme(page: Page, theme: Theme): Promise<void> {
  await page
    .context()
    .addCookies([{ name: 'aboutme-theme', value: theme, url: ORIGIN }]);
}

async function auditAccessibility(
  page: Page,
  route: string,
  theme: Theme,
): Promise<void> {
  const routeName
    = route === '/'
      ? 'landing'
      : route.replaceAll(/[^a-z0-9]+/g, '-').replaceAll(/^-+|-+$/g, '');
  stage(`${routeName}-${theme}`);
  const findings = await new AxeBuilder({ page }).analyze();
  const violations = findings.violations.filter(
    ({ impact }) => impact === 'serious' || impact === 'critical',
  );
  if (violations.length > 0) {
    const first = violations[0];
    const target = JSON.stringify(first?.nodes[0]?.target ?? [])
      .toLowerCase()
      .replaceAll(/[^a-z0-9-]+/g, '-')
      .replaceAll(/^-+|-+$/g, '')
      .slice(0, 100);
    stage(`${routeName}-${theme}-${first?.id ?? 'unknown'}-${target}`);
  }
  expect(violations, `${route} ${theme} axe violations`).toEqual([]);
}

async function auditRouteInBothThemes(
  page: Page,
  route: string,
  ready: () => Promise<void>,
): Promise<void> {
  for (const theme of THEMES) {
    await setTheme(page, theme);
    await page.goto(`${ORIGIN}${route}`);
    await waitForHydration(page);
    await ready();
    await auditAccessibility(page, route, theme);
  }
}

test('landing, sign-in, and the signed-in shell', async ({ browser }) => {
  const counters = newDiagnosticCounters();
  const context = await browser.newContext();
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  const page = await context.newPage();
  pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url),
  })(page);

  try {
    await auditRouteInBothThemes(page, '/', async () => {
      await expect(page.getByTestId('landing-title')).toHaveText(
        'The resume is public. You are not.',
      );
    });
    await setTheme(page, 'light');
    await page.goto(`${ORIGIN}/`);
    await waitForHydration(page);
    await expect(page.getByTestId('landing-title')).toHaveText(
      'The resume is public. You are not.',
    );
    const main = page.getByRole('main');
    await expect(
      main.getByRole('link', { name: /^(Create account|Sign in)$/ }),
    ).toHaveText(['Create account', 'Sign in']);
    await expect(main.getByRole('link', { name: 'Sign in' })).toHaveAttribute(
      'href',
      '/login',
    );
    await expect(
      main.getByRole('link', { name: 'Create account' }),
    ).toHaveAttribute('href', '/register');
    await expectSignedOutShell(page);
    steps.landing = true;

    stage('login-open');
    await main.getByRole('link', { name: 'Sign in' }).click();
    await expect(page).toHaveURL(`${ORIGIN}/login`);
    await waitForHydration(page);
    await auditAccessibility(page, '/login', 'light');
    await setTheme(page, 'dark');
    await page.goto(`${ORIGIN}/login`);
    await waitForHydration(page);
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
    await auditAccessibility(page, '/login', 'dark');
    stage('login-light-reset');
    await setTheme(page, 'light');
    await page.goto(`${ORIGIN}/login`);
    await waitForHydration(page);
    await expect(
      page.getByRole('button', { name: 'Show password', exact: true }),
    ).toBeVisible();
    // The harness runs with PROVIDER_LOGIN_ENABLED=true, so the capabilities
    // read must surface all three provider links.
    await expect(
      page.getByRole('link', {
        name: /^Continue with (Google|GitHub|LinkedIn)$/,
      }),
    ).toHaveCount(3);
    steps.providerLinks = true;

    stage('password-login');
    await page.getByLabel('Email', { exact: true }).fill(SEED_EMAIL);
    stage('password-login-secret');
    await page.getByLabel('Password', { exact: true }).fill(SEED_PASSWORD);
    stage('password-login-submit');
    await page.getByRole('button', { name: 'Sign in' }).click();
    stage('password-login-redirect');
    await expect(page).toHaveURL(`${ORIGIN}/app/resumes`);
    steps.signIn = true;

    await expect(
      page.getByRole('heading', { level: 1, name: 'Resumes' }),
    ).toBeVisible();
    await expect(page.getByTestId('create-resume')).toBeVisible();
    await auditAccessibility(page, '/app/resumes', 'light');
    await setTheme(page, 'dark');
    await page.goto(`${ORIGIN}/app/resumes`);
    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { level: 1, name: 'Resumes' }),
    ).toBeVisible();
    await auditAccessibility(page, '/app/resumes', 'dark');
    await setTheme(page, 'light');
    await page.goto(`${ORIGIN}/app/resumes`);
    await waitForHydration(page);
    steps.resumeList = true;

    const header = page.getByRole('banner');
    await expect(header.getByRole('link', { name: 'Resumes' })).toBeVisible();
    await expect(
      header.getByRole('link', { name: 'Settings', exact: true }),
    ).toBeVisible();
    await expect(
      header.getByRole('button', { name: 'Account menu', exact: true }),
    ).toBeVisible();
    await expect(header.getByRole('link', { name: 'Sign in' })).toHaveCount(0);
    steps.signedInShell = true;

    // Sign out through settings so the proof leaves no session.
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Signed-in devices' }),
    ).toBeVisible();
    await auditAccessibility(page, '/app/settings/sessions', 'light');
    await setTheme(page, 'dark');
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Signed-in devices' }),
    ).toBeVisible();
    await auditAccessibility(page, '/app/settings/sessions', 'dark');
    await setTheme(page, 'light');
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    await waitForHydration(page);
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await expect(page).toHaveURL(`${ORIGIN}/login`);
    await expectSignedOutShell(page);
    steps.signOut = true;
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
