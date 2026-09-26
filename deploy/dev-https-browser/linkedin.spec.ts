import { expect, test, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  LINKEDIN_COLLISION_LABEL,
  LINKEDIN_LINK_LABEL,
  LINKEDIN_NO_EMAIL_LABEL,
  LINKEDIN_UNVERIFIED_LABEL,
  LINKEDIN_VERIFIED_LABEL,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pinEnglish,
  signInWithLinkedIn,
  startLinkedInAuthorize,
  waitForHydration,
} from './harness-lib';
import { ALLOWED_ORIGIN } from './network-policy';

// Node-only capture endpoint, reached only by Playwright control code over
// loopback HTTP (password-auth.spec.ts "captureClient"), never by the page.
const ORIGIN = ALLOWED_ORIGIN;
const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
const EVIDENCE_PATH = '/evidence/linkedin-proof.json';
const DIAGNOSTIC_PATH = '/evidence/linkedin-diagnostic.json';
const COLLISION_EMAIL = 'li-collision@example.invalid';
const VERIFIED_EMAIL = 'li-verified@example.invalid';

let linkedinStage = 'before-setup';

function stage(name: string): void {
  linkedinStage = name;
  console.log(`linkedin-stage:${name}`);
}

test.afterEach(async ({}, testInfo) => {
  if (testInfo.status === testInfo.expectedStatus) return;
  await writeFile(
    DIAGNOSTIC_PATH,
    `${JSON.stringify({
      error: testInfo.error === undefined ? 'none'
        : testInfo.error.message.includes('Timeout') ? 'timeout' : 'assertion',
      stage: linkedinStage,
    })}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});

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
      // nosemgrep: typescript.react.security.react-insecure-request.react-insecure-request -- loopback HTTP to the local mail capture harness
      const response = await fetch(CAPTURE_URL, { method: 'DELETE', headers });
      if (!response.ok) throw new Error(`capture reset failed: ${response.status}`);
    },
    async messages() {
      // nosemgrep: typescript.react.security.react-insecure-request.react-insecure-request -- loopback HTTP to the local mail capture harness
      const response = await fetch(CAPTURE_URL, { headers });
      if (!response.ok) throw new Error(`capture read failed: ${response.status}`);
      const body = (await response.json()) as { messages: CapturedMessage[] };
      return body.messages;
    },
  };
}

// waitForVerifyToken polls the capture store for a verify message to the
// given address and returns its token. dev-https-check.sh removes every row
// an earlier run left before this proof starts, so each registration here is
// new and must send one.
async function waitForVerifyToken(
  capture: CaptureClient,
  to: string,
): Promise<string> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    for (const message of await capture.messages()) {
      if (message.kind !== 'verify' || message.to !== to) continue;
      const token = message.text_body.match(/#token=([A-Za-z0-9_-]+)/)?.[1];
      if (token !== undefined) return token;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error('verify message did not arrive');
}

function randomEmail(): string {
  return `lnkd-test-${randomBytes(8).toString('hex')}@example.invalid`;
}

// secret returns a runtime-random password that satisfies the D2 policy
// without ever leaving test memory.
function secret(): string {
  return randomBytes(24).toString('base64url');
}

async function gotoHydrated(page: Page, url: string): Promise<void> {
  await page.goto(url);
  await waitForHydration(page);
}

async function meEmail(page: Page): Promise<string | null> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    if (response.status !== 200) return null;
    const body = (await response.json()) as { data?: { user?: { email?: unknown } } };
    const email = body.data?.user?.email;
    return typeof email === 'string' ? email : null;
  });
}

// createVerifiedPasswordAccount registers the given account through the UI
// and verifies it through the captured link.
async function createVerifiedPasswordAccount(
  page: Page,
  capture: CaptureClient,
  email: string,
  password: string,
): Promise<void> {
  await gotoHydrated(page, '/register');
  await page.getByLabel('Name').fill('LinkedIn Proof User');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByTestId('register-success')).toContainText(
    'Check your email',
  );
  const token = await waitForVerifyToken(capture, email);
  await gotoHydrated(page, `${ORIGIN}/verify-email#token=${token}`);
  await expect(page.getByTestId('verify-success')).toContainText(
    'Email verified',
  );
}

async function signOut(page: Page): Promise<void> {
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await expect(page).toHaveURL(`${ORIGIN}/login`);
}

// attemptLinkedInSignIn starts a LinkedIn login for the named account from
// an anonymous /login, allows it, and returns the resulting callback error
// code (null on success, which never happens for this helper's callers).
async function attemptLinkedInSignIn(
  page: Page,
  accountLabel: string,
): Promise<{ error: string | null; provider: string | null }> {
  await gotoHydrated(page, '/login');
  await startLinkedInAuthorize(
    page,
    page.getByRole('link', { name: 'Continue with LinkedIn' }),
    accountLabel,
  );
  await Promise.all([
    page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/login'),
    page.getByRole('button', { name: 'Allow', exact: true }).click(),
  ]);
  const url = new URL(page.url());
  return { error: url.searchParams.get('error'), provider: url.searchParams.get('provider') };
}

test('proves LinkedIn sign-in, linking, and cancellation over native HTTPS', async ({
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

  const steps = {
    cancelAuthorize: false,
    cancelLogin: false,
    collisionBlocked: false,
    lastMethodGuard: false,
    linked: false,
    linkedSignIn: false,
    noEmailBlocked: false,
    signOutSignIn: false,
    signUp: false,
    unlinked: false,
    unverifiedBlocked: false,
  };

  // 1. Sign-up with the verified account lands on the resume list
  // (docs/design/linkedin-sign-in.md "Tests").
  stage('sign-up');
  await gotoHydrated(page, '/login');
  await signInWithLinkedIn(page, { accountLabel: LINKEDIN_VERIFIED_LABEL });
  await expect(page).toHaveURL(`${ORIGIN}/app/resumes`);
  expect(await meEmail(page)).toBe(VERIFIED_EMAIL);
  steps.signUp = true;

  // 2. Sign out, then sign in again with LinkedIn reaches the same account.
  stage('sign-out-sign-in');
  await signOut(page);
  await signInWithLinkedIn(page, { accountLabel: LINKEDIN_VERIFIED_LABEL });
  await expect(page).toHaveURL(`${ORIGIN}/app/resumes`);
  expect(await meEmail(page)).toBe(VERIFIED_EMAIL);
  steps.signOutSignIn = true;
  await signOut(page);

  // 3. The no-email account shows the verify-with-provider message and
  // creates nothing: signing in again fails the exact same way.
  stage('no-email-blocked');
  const noEmailFirst = await attemptLinkedInSignIn(page, LINKEDIN_NO_EMAIL_LABEL);
  expect(noEmailFirst.error).toBe('email_not_verified');
  await expect(page.getByTestId('login-error')).toContainText(
    'must be verified with your provider',
  );
  const noEmailSecond = await attemptLinkedInSignIn(page, LINKEDIN_NO_EMAIL_LABEL);
  expect(noEmailSecond.error).toBe('email_not_verified');
  steps.noEmailBlocked = true;

  // 4. The unverified account behaves the same way.
  stage('unverified-blocked');
  const unverifiedFirst = await attemptLinkedInSignIn(page, LINKEDIN_UNVERIFIED_LABEL);
  expect(unverifiedFirst.error).toBe('email_not_verified');
  const unverifiedSecond = await attemptLinkedInSignIn(page, LINKEDIN_UNVERIFIED_LABEL);
  expect(unverifiedSecond.error).toBe('email_not_verified');
  steps.unverifiedBlocked = true;

  // 5. The collision account, whose email a verified password account
  // already holds, shows the generic collision message and creates
  // nothing.
  stage('collision-account');
  await createVerifiedPasswordAccount(page, capture, COLLISION_EMAIL, secret());
  stage('collision-blocked');
  const collision = await attemptLinkedInSignIn(page, LINKEDIN_COLLISION_LABEL);
  expect(collision.error).toBe('email_already_registered');
  expect(collision.provider).toBe('linkedin');
  await expect(page.getByTestId('login-error')).toContainText(
    'An account with this email already exists.',
  );
  steps.collisionBlocked = true;

  // 6. A password account links LinkedIn (the Link account) from Settings
  // after reauthentication (a freshly logged-in session), signs out, then
  // signs in with LinkedIn to the same account.
  stage('link-account-register');
  const linkEmail = randomEmail();
  const linkPassword = secret();
  await createVerifiedPasswordAccount(page, capture, linkEmail, linkPassword);
  stage('link-account-login');
  await gotoHydrated(page, '/login');
  await page.getByLabel('Email').fill(linkEmail);
  await page.getByLabel('Password', { exact: true }).fill(linkPassword);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(`${ORIGIN}/app/resumes`);
  stage('link-account-settings');
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByTestId('add-provider-button').click();
  const linkLinkedIn = page.getByRole('button', {
    name: 'Link LinkedIn',
    exact: true,
  });
  await expect(linkLinkedIn).toBeVisible();
  stage('link-account-authorize');
  await startLinkedInAuthorize(page, linkLinkedIn, LINKEDIN_LINK_LABEL);
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/app/settings/sessions'
      && url.search === ''
    ),
    page.getByRole('button', { name: 'Allow', exact: true }).click(),
  ]);
  await expect(page.getByTestId('linked-provider-linkedin')).toBeVisible();
  steps.linked = true;

  stage('link-account-sign-out-sign-in');
  await signOut(page);
  await signInWithLinkedIn(page, { accountLabel: LINKEDIN_LINK_LABEL });
  await expect(page).toHaveURL(`${ORIGIN}/app/resumes`);
  expect(await meEmail(page)).toBe(linkEmail);
  steps.linkedSignIn = true;

  // 7. Unlink works: the password account keeps its other sign-in method.
  stage('unlink');
  await gotoHydrated(page, '/app/settings/sessions');
  const linkedRow = page.getByTestId('linked-provider-linkedin');
  await linkedRow.getByTestId('unlink-button').click();
  await page.getByRole('alertdialog').getByRole('button', {
    name: 'Unlink LinkedIn',
    exact: true,
  }).click();
  await expect(page.getByTestId('linked-provider-linkedin')).toHaveCount(0);
  steps.unlinked = true;
  await signOut(page);

  // 8. The last-method guard holds for a LinkedIn-only account (the
  // verified account has no password): Unlink stays disabled.
  stage('last-method-guard');
  await gotoHydrated(page, '/login');
  await signInWithLinkedIn(page, { accountLabel: LINKEDIN_VERIFIED_LABEL });
  await gotoHydrated(page, '/app/settings/sessions');
  const guardedRow = page.getByTestId('linked-provider-linkedin');
  await expect(guardedRow.getByTestId('unlink-button')).toBeDisabled();
  await expect(guardedRow.getByTestId('unlink-blocked')).toBeVisible();
  steps.lastMethodGuard = true;
  await signOut(page);

  // 9. Both cancel buttons show the same cancelled message.
  stage('cancel-login');
  await gotoHydrated(page, '/login');
  await startLinkedInAuthorize(
    page,
    page.getByRole('link', { name: 'Continue with LinkedIn' }),
  );
  await Promise.all([
    page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/login'),
    page.getByRole('button', { name: 'Cancel sign-in', exact: true }).click(),
  ]);
  expect(new URL(page.url()).searchParams.get('error')).toBe('cancelled');
  await expect(page.getByTestId('login-error')).toHaveText(
    'Sign-in was cancelled.',
  );
  steps.cancelLogin = true;

  stage('cancel-authorize');
  await gotoHydrated(page, '/login');
  await startLinkedInAuthorize(
    page,
    page.getByRole('link', { name: 'Continue with LinkedIn' }),
  );
  await Promise.all([
    page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/login'),
    page.getByRole('button', { name: 'Cancel authorization', exact: true }).click(),
  ]);
  expect(new URL(page.url()).searchParams.get('error')).toBe('cancelled');
  await expect(page.getByTestId('login-error')).toHaveText(
    'Sign-in was cancelled.',
  );
  steps.cancelAuthorize = true;

  stage('teardown');
  const { certificateErrors, consoleErrors, externalRequests, pageErrors } = counters;
  expect({ certificateErrors, consoleErrors, externalRequests, pageErrors }).toEqual({
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
      scenario: 'linkedin-sign-in',
      schemaVersion: 1,
      steps,
    }, null, 2)}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});
