import {
  expect,
  test,
  type Frame,
  type Page,
} from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  signInWithGoogle,
  waitForHydration,
  pinEnglish,
  warmPage,
} from './harness-lib';
import { ALLOWED_ORIGIN } from './network-policy';

// Node-only capture endpoint. The browser's page/context firewall never sees
// this: it is reached only by Playwright control code over loopback HTTP,
// authenticated with the bearer secret read from the read-only input mount.
const ORIGIN = ALLOWED_ORIGIN;
const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
const EVIDENCE_PATH = '/evidence/password-proof.json';
const DIAGNOSTIC_PATH = '/evidence/password-diagnostic.json';
let passwordStage = 'before-localization';
let passwordDetail = 'none';

function setPasswordStage(stage: string): void {
  passwordStage = stage;
  passwordDetail = 'none';
}

function callbackCategory(value: string): string {
  const url = new URL(value);
  if (url.pathname !== '/app/settings/sessions') return 'callback-other';
  const code = url.searchParams.get('error');
  if (code === null && url.search === '') return 'callback-settings-empty';
  if (code === 'auth_failed') return 'callback-settings-auth-failed';
  if (code === 'email_not_verified') return 'callback-settings-email-not-verified';
  if (code === 'cancelled') return 'callback-settings-cancelled';
  if (code === 'email_already_registered') return 'callback-settings-email-already-registered';
  if (code === 'identity_already_linked') return 'callback-settings-identity-already-linked';
  if (code === 'reauth_required') return 'callback-settings-reauth-required';
  return 'callback-settings-unrecognized-error';
}

test.afterEach(async ({}, testInfo) => {
  if (testInfo.status === testInfo.expectedStatus) return;
  await writeFile(
    DIAGNOSTIC_PATH,
    `${JSON.stringify({
      error: testInfo.error === undefined ? 'none'
        : testInfo.error.message.includes('Timeout') ? 'timeout' : 'assertion',
      detail: passwordDetail,
      stage: passwordStage,
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

// fragmentURLFor builds a local verification/reset URL from a raw token,
// re-pointing the production link origin at the trusted local origin.
function verifyURL(token: string): string {
  return `${ORIGIN}/verify-email#token=${token}`;
}

function resetURL(token: string): string {
  return `${ORIGIN}/reset-password#token=${token}`;
}

async function meStatus(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    return response.status;
  });
}

// gotoHydrated navigates and then waits for the Nuxt app to hydrate, so a
// Vue-bound form is interactive rather than a stale SSR shell.
async function gotoHydrated(page: Page, url: string): Promise<void> {
  await page.goto(url);
  await waitForHydration(page);
}

async function provePasswordVisibilityToggle(
  page: Page,
  label: string,
): Promise<void> {
  const passwordInput = page.getByLabel(label, { exact: true });
  const showToggle = page.getByRole('button', {
    name: `Show ${label.toLowerCase()}`,
    exact: true,
  });
  await expect(passwordInput).toHaveAttribute('type', 'password');
  await expect(showToggle).toBeVisible();
  await showToggle.click();
  await expect(passwordInput).toHaveAttribute('type', 'text');
  await expect(page.getByRole('button', {
    name: `Hide ${label.toLowerCase()}`,
    exact: true,
  })).toBeVisible();
  await page.getByRole('button', {
    name: `Hide ${label.toLowerCase()}`,
    exact: true,
  }).click();
  await expect(passwordInput).toHaveAttribute('type', 'password');
  await expect(showToggle).toBeVisible();
}

test('proves password authentication over native HTTPS', async ({
  browser,
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
  const newPassword = secret();

  // Warm every signed-out page this proof visits before the timed journey
  // below depends on any of them already being compiled on the harness's
  // dev server (harness-lib.ts warmPage). Each is visited again for real at
  // its own tight bound, so a real hydration regression there still fails
  // fast instead of being masked behind the warm bound.
  await warmPage(page, '/register');
  await warmPage(page, '/login');
  await warmPage(page, '/forgot-password');

  // 1. Register and prove the fixed, account-neutral accepted copy.
  await gotoHydrated(page, '/register');
  await page.getByLabel('Name').fill('Proof User');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password', { exact: true }).fill(password);
  await provePasswordVisibilityToggle(page, 'Password');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByTestId('register-success')).toContainText(
    'Check your email',
  );

  // 2. Verify through the captured link, with no session created. This page
  // reads its token from the URL fragment, so it cannot be pre-warmed with
  // no token (the second-factor journeys exclude these pages from their own
  // WARM_ROUTES for the same reason); its one real visit takes the warm
  // bound directly instead.
  const verifyToken = await waitForLink(capture, 'verify', email);
  await warmPage(page, verifyURL(verifyToken));
  await expect(page.getByTestId('verify-success')).toContainText(
    'Email verified',
  );
  await expect.poll(() => page.url()).not.toContain('#token=');
  expect(await meStatus(page)).toBe(401);

  // 3. Password login and an authenticated /me with a password credential.
  await gotoHydrated(page, '/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL('/app/resumes');
  expect(await meStatus(page)).toBe(200);

  // /app/settings/sessions needs a session, so it could not join the
  // signed-out warm pass above; warm it now that one exists.
  await warmPage(page, '/app/settings/sessions');

  // 4. Link a provider whose verified email differs from the account email.
  setPasswordStage('settings-navigation');
  await gotoHydrated(page, '/app/settings/sessions');
  setPasswordStage('locale-toggle');
  let localeMutations = 0;
  const countLocaleMutations = (request: { method(): string; url(): string }): void => {
    const url = new URL(request.url());
    if (url.origin === ORIGIN && request.method() !== 'GET' && (url.pathname === '/api/v1/auth/password/reauth' || url.pathname === '/api/v1/me/password')) localeMutations += 1;
  };
  page.on('request', countLocaleMutations);
  await page.getByTestId('landing-locale-vi').click();
  await expect(page.getByRole('heading', { name: 'Mật khẩu' })).toBeVisible();
  await page.getByTestId('password-action').click();
  setPasswordStage('password-draft');
  const currentPassword = page.getByLabel('Mật khẩu hiện tại', { exact: true });
  await currentPassword.fill(password);
  await currentPassword.focus();
  setPasswordStage('password-english-toggle');
  await page.getByTestId('landing-locale-en').click();
  setPasswordStage('password-english-render');
  const translatedCurrentPassword = page.getByLabel('Current password', { exact: true });
  await expect(translatedCurrentPassword).toHaveValue(password);
  setPasswordStage('password-focus');
  await expect(translatedCurrentPassword).toBeFocused();
  expect(localeMutations).toBe(0);
  page.off('request', countLocaleMutations);
  await page.getByTestId('password-cancel').click();
  setPasswordStage('add-provider');
  await page.getByTestId('add-provider-button').click();
  const linkGoogle = page.getByRole('button', {
    name: 'Link Google',
    exact: true,
  });
  setPasswordStage('link-provider-visible');
  await expect(linkGoogle).toBeVisible();
  setPasswordStage('link-provider-start');
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/__uat/oauth/google/authorize'
    ),
    linkGoogle.click(),
  ]);
  setPasswordStage('link-provider-select-account');
  await page.getByLabel('Password Link — pa-link@example.invalid').check();
  setPasswordStage('link-provider-callback');
  const recordCallbackNavigation = (frame: Frame): void => {
    if (frame === page.mainFrame()) passwordDetail = callbackCategory(frame.url());
  };
  page.on('framenavigated', recordCallbackNavigation);
  try {
    await Promise.all([
      page.waitForURL(
        (url) =>
          url.origin === ORIGIN
          && url.pathname === '/app/settings/sessions'
          && url.search === '',
        { timeout: 20_000 },
      ),
      page.getByRole('button', { name: 'Continue with Google' }).click(),
    ]);
  } catch (error) {
    passwordDetail = callbackCategory(page.url());
    throw error;
  } finally {
    page.off('framenavigated', recordCallbackNavigation);
  }
  setPasswordStage('link-provider-callback');
  const linkedGoogle = page.getByTestId('linked-provider-google');
  setPasswordStage('unlink-open');
  await linkedGoogle.getByTestId('unlink-button').click();
  const unlinkDialog = page.getByRole('alertdialog');
  const unlinkTitle = unlinkDialog.getByRole('heading');
  await expect(unlinkTitle).toHaveText('Unlink Google?');
  setPasswordStage('unlink-vietnamese-toggle');
  await unlinkDialog.getByRole('group', { name: 'Language' }).getByRole('button', { name: 'Tiếng Việt' }).click();
  setPasswordStage('unlink-vietnamese-title');
  await expect(unlinkTitle).toHaveText('Hủy liên kết Google?');
  setPasswordStage('unlink-english');
  await unlinkDialog.getByRole('group', { name: 'Ngôn ngữ' }).getByRole('button', { name: 'English' }).click();
  await expect(unlinkTitle).toHaveText('Unlink Google?');
  setPasswordStage('unlink-cancel');
  await unlinkDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  setPasswordStage('after-identity-localization');

  // 5. Provider-only account: sign in, then add a password.
  const providerContext = await browser.newContext();
  await pinEnglish(providerContext);
  const providerPage = await providerContext.newPage();
  attachPageDiagnostics(providerPage);
  await installExternalRequestFirewall(providerContext, counters);
  await providerPage.goto('/login');
  await signInWithGoogle(providerPage, {
    accountLabel: 'Provider Only — pa-provider-only@example.invalid',
  });
  await gotoHydrated(providerPage, '/app/settings/sessions');
  const providerPassword = secret();
  await providerPage.getByTestId('password-action').click();
  await providerPage.getByLabel('New password', { exact: true })
    .fill(providerPassword);
  await providerPage.getByLabel('Confirm password', { exact: true })
    .fill(providerPassword);
  await providerPage.getByTestId('password-set-submit').click();
  await expect(providerPage.getByTestId('password-success')).toContainText(
    'Password added.',
  );
  await providerContext.close();

  // 6. A second live session for the registered account (context C).
  const secondContext = await browser.newContext();
  await pinEnglish(secondContext);
  const secondPage = await secondContext.newPage();
  attachPageDiagnostics(secondPage);
  await installExternalRequestFirewall(secondContext, counters);
  await gotoHydrated(secondPage, '/login');
  await secondPage.getByLabel('Email').fill(email);
  await secondPage.getByLabel('Password', { exact: true }).fill(password);
  await secondPage.getByRole('button', { name: 'Sign in' }).click();
  await secondPage.waitForURL('/app/resumes');
  expect(await meStatus(secondPage)).toBe(200);

  // 7. Forgot password and reset through the captured link (no auto-login).
  await gotoHydrated(page, '/forgot-password');
  await page.getByLabel('Email').fill(email);
  await page.getByRole('button', { name: 'Send reset link' }).click();
  await expect(page.getByTestId('forgot-success')).toBeVisible();
  const resetToken = await waitForLink(capture, 'reset', email);
  // Like verify-email above, this page reads its token from the URL
  // fragment and gets its one real visit here, at the warm bound.
  await warmPage(page, resetURL(resetToken));
  await page.getByLabel('New password', { exact: true }).fill(newPassword);
  await page.getByLabel('Confirm password', { exact: true }).fill(newPassword);
  await page.getByRole('button', { name: 'Reset password' }).click();
  await expect(page.getByTestId('reset-success')).toContainText(
    'Password reset',
  );

  // 8. Every old session is revoked; the old password is rejected.
  expect(await meStatus(page)).toBe(401);
  expect(await meStatus(secondPage)).toBe(401);
  await gotoHydrated(page, '/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByTestId('login-form-error')).toContainText(
    'Invalid email or password',
  );

  // 9. The reset token is single-use: replay is rejected.
  await gotoHydrated(page, resetURL(resetToken));
  await page.getByLabel('New password', { exact: true }).fill(newPassword);
  await page.getByLabel('Confirm password', { exact: true }).fill(newPassword);
  await page.getByRole('button', { name: 'Reset password' }).click();
  await expect(page.getByTestId('reset-error')).toContainText('invalid');

  // 10. The new password signs in.
  await gotoHydrated(page, '/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(newPassword);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL('/app/resumes');
  expect(await meStatus(page)).toBe(200);

  await secondContext.close();

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
      scenario: 'password-authentication',
      schemaVersion: 1,
      steps: {
        differentEmailLink: true,
        newPasswordLogin: true,
        oldPasswordRejected: true,
        oldSessionsRevoked: true,
        passwordAdded: true,
        passwordLogin: true,
        providerOnlyLogin: true,
        registerAccepted: true,
        reset: true,
        resetReplayRejected: true,
        verifiedWithoutSession: true,
      },
    }, null, 2)}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});
