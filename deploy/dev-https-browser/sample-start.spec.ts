import { expect, test, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { createBlankResume, uniqueTitle } from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pinEnglish,
  waitForHydration,
} from './harness-lib';
import { ALLOWED_ORIGIN } from './network-policy';

// Node-only capture endpoint, reached only by Playwright control code over
// loopback HTTP; the browser's page/context firewall never sees it.
const ORIGIN = ALLOWED_ORIGIN;
const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
const EVIDENCE_PATH = '/evidence/sample-start-proof.json';
const SAMPLE_PATH = '/templates/engineer-compact';
const NEXT_PATH = '/app/new?sample=engineer-compact&lng=en';
const NEXT_LOGIN_PATH = `/login?next=${encodeURIComponent(NEXT_PATH)}`;
// Mirrors RESUME_CAP in apps/web/app/composables/useResumeList.ts; the
// browser proof runs isolated from the web app source.
const RESUME_CAP = 3;
const SCHEMA_VERSION = '4';

interface CapturedMessage {
  kind: string;
  to: string;
  text_body: string;
}

interface CaptureClient {
  reset(): Promise<void>;
  messages(): Promise<CapturedMessage[]>;
}

function captureClient(token: string): CaptureClient {
  const headers = { Authorization: `Bearer ${token}` };
  return {
    async reset() {
      const response = await fetch(CAPTURE_URL, { method: 'DELETE', headers });
      if (!response.ok) throw new Error(`capture reset failed: ${response.status}`);
    },
    async messages() {
      const response = await fetch(CAPTURE_URL, { headers });
      if (!response.ok) throw new Error(`capture read failed: ${response.status}`);
      const body = (await response.json()) as { messages: CapturedMessage[] };
      return body.messages;
    },
  };
}

// secret returns a runtime-random password that satisfies the D2 policy
// (15-128 code points, not common/breached) without ever leaving test memory.
function secret(): string {
  return randomBytes(24).toString('base64url');
}

function randomEmail(): string {
  return `pa-test-${randomBytes(8).toString('hex')}@example.invalid`;
}

// waitForLink polls the Node capture store for a message of the given kind
// sent to the given address, then returns the token embedded in its fragment
// link. The message body is read only through Node; it never reaches the page.
async function waitForLink(
  capture: CaptureClient,
  kind: string,
  to: string,
): Promise<string> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    for (const message of await capture.messages()) {
      if (message.kind !== kind || message.to !== to) continue;
      const token = message.text_body.match(/#token=([A-Za-z0-9_-]+)/)?.[1];
      if (token !== undefined) return token;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`no ${kind} message for ${to} within 30s`);
}

function verifyURL(token: string): string {
  return `${ORIGIN}/verify-email#token=${token}`;
}

// gotoHydrated navigates and then waits for the Nuxt app to hydrate, so a
// Vue-bound form is interactive rather than a stale SSR shell.
async function gotoHydrated(page: Page, url: string): Promise<void> {
  await page.goto(url);
  await waitForHydration(page);
}

// resumeCount reads the account's resume list through the same authenticated
// API the web app calls, so a reload can be proven to create nothing.
async function resumeCount(page: Page): Promise<number> {
  return page.evaluate(async (schemaVersion) => {
    const response = await fetch('/api/v1/resumes', {
      cache: 'no-store',
      credentials: 'include',
      headers: { 'X-Resume-Schema-Version': schemaVersion },
    });
    if (response.status !== 200) {
      throw new Error(`resume list status ${response.status}`);
    }
    const body = (await response.json()) as { data?: unknown[] };
    if (!Array.isArray(body.data)) throw new Error('resume list malformed');
    return body.data.length;
  }, SCHEMA_VERSION);
}

function stage(name: string): void {
  console.log(`sample-start-stage:${name}`);
}

test('proves register-to-create from a gallery sample', async ({
  context,
  page,
}) => {
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const attachPageDiagnostics = pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedConsoleError,
  });
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);

  const capture = captureClient(
    (await readFile(CAPTURE_TOKEN_PATH, 'utf8')).trim(),
  );
  await capture.reset();

  const email = randomEmail();
  const password = secret();

  // 1. Signed out, the sample's "use this sample" link keeps the intended
  // destination through login.
  stage('sample-page');
  await gotoHydrated(page, SAMPLE_PATH);
  stage('sample-click');
  await page.locator('[data-action="use-sample"]').click();
  await page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/login');
  await waitForHydration(page);
  const loginNext = new URL(page.url()).searchParams.get('next');
  expect(loginNext).toBe(NEXT_PATH);

  // 2. The login page's register link keeps the same next.
  stage('register-link');
  // The site header has its own "Create account" link; the page's is in main.
  const registerLink = page.getByRole('main')
    .getByRole('link', { name: 'Create account' });
  await expect(registerLink).toHaveAttribute(
    'href',
    `/register?next=${encodeURIComponent(NEXT_PATH)}`,
  );
  await registerLink.click();
  await page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/register');
  await waitForHydration(page);

  stage('register-fill');
  await page.getByLabel('Name').fill('Sample Start Proof');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password', { exact: true }).fill(password);
  stage('register-submit');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByTestId('register-success')).toContainText(
    'Check your email',
  );

  // 3. Verify through the captured link, opened in a new tab like the real
  // email would.
  stage('verify-link');
  const verifyToken = await waitForLink(capture, 'verify', email);
  const verifyPage = await context.newPage();
  stage('verify-open');
  await gotoHydrated(verifyPage, verifyURL(verifyToken));
  await expect(verifyPage.getByTestId('verify-success')).toContainText(
    'Email verified',
  );

  // 4. The verify page's sign-in link keeps the same next; sign in and land
  // on the sample's confirm page.
  stage('verify-signin-link');
  const signInLink = verifyPage.getByTestId('verify-sign-in');
  await expect(signInLink).toHaveAttribute('href', NEXT_LOGIN_PATH);
  stage('signin');
  await signInLink.click();
  await verifyPage.waitForURL(
    (url) => url.origin === ORIGIN && url.pathname === '/login',
  );
  await waitForHydration(verifyPage);
  await verifyPage.getByLabel('Email').fill(email);
  await verifyPage.getByLabel('Password', { exact: true }).fill(password);
  await verifyPage.getByRole('button', { name: 'Sign in' }).click();
  await verifyPage.waitForURL(
    (url) => url.origin === ORIGIN
      && url.pathname === '/app/new'
      && url.search === '?sample=engineer-compact&lng=en',
  );
  await waitForHydration(verifyPage);
  await expect(verifyPage.locator('[data-new-resume="confirm"]')).toBeVisible();

  // 5. A reload of the confirm page creates nothing.
  stage('reload');
  await verifyPage.reload();
  await waitForHydration(verifyPage);
  await expect(verifyPage.locator('[data-new-resume="confirm"]')).toBeVisible();
  expect(await resumeCount(verifyPage)).toBe(0);

  // 6. Confirm creates the resume and opens the editor with the sample
  // content.
  stage('create-title');
  await verifyPage.getByLabel('Title').fill('Sample Start Resume');
  stage('create-submit');
  const createResponse = verifyPage.waitForResponse((response) =>
    response.request().method() === 'POST'
    && new URL(response.url()).origin === ORIGIN
    && new URL(response.url()).pathname === '/api/v1/resumes');
  await verifyPage.getByRole('button', { name: 'Create and open editor' }).click();
  const created = await createResponse;
  expect(created.status()).toBe(201);
  const createdBody = (await created.json()) as { data?: { id?: unknown } };
  const resumeId = createdBody.data?.id;
  if (typeof resumeId !== 'string' || resumeId === '') {
    throw new Error('resume create response did not return an id');
  }
  stage('editor-open');
  await verifyPage.waitForURL(
    (url) => url.origin === ORIGIN && url.pathname === `/app/resumes/${resumeId}`,
  );
  await waitForHydration(verifyPage);
  await expect(verifyPage.locator('[data-resume-title]')).toHaveText(
    'Sample Start Resume',
  );
  await verifyPage
    .getByRole('navigation', { name: 'Resume outline' })
    .getByRole('button', { name: 'Personal details', exact: true })
    .click();
  await expect(verifyPage.getByLabel('Headline')).toHaveValue(
    'Senior Backend Engineer · Go, Distributed Systems, Payments',
  );

  // 7. Fill the account to the resume cap, then the confirm page hides the
  // create button.
  stage('fill-cap');
  for (let count = 1; count < RESUME_CAP; count += 1) {
    await createBlankResume(verifyPage, uniqueTitle());
  }
  stage('cap-check');
  await gotoHydrated(verifyPage, NEXT_PATH);
  await expect(verifyPage.locator('[data-new-resume-cap]')).toBeVisible();
  await expect(
    verifyPage.locator('[data-action="create-from-start"]'),
  ).toHaveCount(0);

  const { certificateErrors, consoleErrors, externalRequests, pageErrors } = counters;
  expect({
    certificateErrors,
    consoleErrors,
    externalRequests,
    pageErrors,
  }).toEqual({
    certificateErrors: 0,
    consoleErrors: 0,
    externalRequests: 0,
    pageErrors: 0,
  });

  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify({
      errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
      origin: ORIGIN,
      scenario: 'sample-start',
      schemaVersion: 1,
      steps: {
        capHidesCreate: true,
        created: true,
        editorOpen: true,
        nextAfterVerify: true,
        nextKept: true,
        registered: true,
        reloadCreatedNothing: true,
        signedIn: true,
        verified: true,
      },
    }, null, 2)}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});
