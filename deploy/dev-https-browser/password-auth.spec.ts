import {
  expect,
  test,
  type Page,
} from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
  isExpectedNegativeHTTPConsole,
} from './network-policy';

// Node-only capture endpoint. The browser's page/context firewall never sees
// this: it is reached only by Playwright control code over loopback HTTP,
// authenticated with the bearer secret read from the read-only input mount.
const ORIGIN = ALLOWED_ORIGIN;
const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
const EVIDENCE_PATH = '/evidence/password-proof.json';

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
  await page.waitForLoadState('networkidle');
}

// signInWithGoogle completes a provider login: it follows the login anchor to
// the same-origin authorize page, selects the named local account, and returns
// after the callback lands on the home page.
async function signInWithGoogle(page: Page, accountLabel: string): Promise<void> {
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/__uat/oauth/google/authorize'
    ),
    page.getByRole('link', { name: 'Continue with Google' }).click(),
  ]);
  await page.getByLabel(accountLabel).check();
  await Promise.all([
    page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/'),
    page.getByRole('button', { name: 'Continue with Google' }).click(),
  ]);
}

test('proves password authentication over native HTTPS', async ({
  browser,
  context,
  page,
}) => {
  let certificateErrors = 0;
  let consoleErrors = 0;
  let externalRequests = 0;
  let pageErrors = 0;

  const attachPageDiagnostics = (openedPage: Page): void => {
    openedPage.on('console', (message) => {
      if (
        message.type() === 'error'
        && !isExpectedNegativeHTTPConsole(
          message.text(),
          message.location().url,
        )
      ) {
        consoleErrors += 1;
      }
    });
    openedPage.on('pageerror', () => {
      pageErrors += 1;
    });
    openedPage.on('requestfailed', (request) => {
      if (/CERT/i.test(request.failure()?.errorText ?? '')) certificateErrors += 1;
    });
  };
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  await context.route('**/*', async (route) => {
    if (!isAllowedHTTPURL(route.request().url())) {
      externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
  await context.routeWebSocket('**/*', async (webSocket) => {
    if (!isAllowedWebSocketURL(webSocket.url())) {
      externalRequests += 1;
      await webSocket.close({ code: 1008, reason: 'blocked' });
      return;
    }
    webSocket.connectToServer();
  });

  const capture = captureClient(
    (await readFile(CAPTURE_TOKEN_PATH, 'utf8')).trim(),
  );
  await capture.reset();

  const email = randomEmail();
  const password = secret();
  const newPassword = secret();

  // 1. Register and prove the fixed, account-neutral accepted copy.
  await gotoHydrated(page, '/register');
  await page.getByLabel('Name').fill('Proof User');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByTestId('register-success')).toContainText(
    'Check your email',
  );

  // 2. Verify through the captured link, with no session created.
  const verifyToken = await waitForLink(capture, 'verify', email);
  await gotoHydrated(page, verifyURL(verifyToken));
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

  // 4. Link a provider whose verified email differs from the account email.
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByTestId('add-provider-button').click();
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/__uat/oauth/google/authorize'
    ),
    page.getByRole('button', { name: 'Link google' }).click(),
  ]);
  await page.getByLabel('Bob Local — bob@example.invalid').check();
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/app/settings/sessions'
      && url.search === ''
    ),
    page.getByRole('button', { name: 'Continue with Google' }).click(),
  ]);

  // 5. Provider-only account: sign in, then add a password.
  const providerContext = await browser.newContext();
  const providerPage = await providerContext.newPage();
  attachPageDiagnostics(providerPage);
  await providerContext.route('**/*', async (route) => {
    if (!isAllowedHTTPURL(route.request().url())) {
      externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
  await providerPage.goto('/login');
  await signInWithGoogle(
    providerPage,
    'Provider Only — pa-provider-only@example.invalid',
  );
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
  const secondPage = await secondContext.newPage();
  attachPageDiagnostics(secondPage);
  await secondContext.route('**/*', async (route) => {
    if (!isAllowedHTTPURL(route.request().url())) {
      externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
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
  await gotoHydrated(page, resetURL(resetToken));
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
