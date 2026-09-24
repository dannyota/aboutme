/**
 * Scripted production proof for the authenticator-app (TOTP) second factor.
 * Runs only inside the pinned browser image against the real production
 * origin, driven by the top manager, never by hand
 * (docs/runbooks/totp-keys.md "Production proofs"). Three modes share this
 * file:
 *
 * - `totp-prod-flag-off`, after the flag-off deploy and before the fence
 *   raise: health, the named account signing in unenrolled, a passkey it
 *   enrolls and proves login and recovery with, existing sessions and
 *   grants revoking on enrollment, and TOTP enrollment answering closed.
 *   The `finally` path removes the passkey again, so the account is
 *   unenrolled for the next phase.
 * - `totp-prod-enabled`, after the fence raise and the flag-on redeploy:
 *   first TOTP enrollment, pending login, replacement, one shared recovery
 *   completion, passkey coexistence, both locales, and removal. The
 *   `finally` path removes every factor again.
 * - `totp-prod-cleanup`, a standalone safety net for when an earlier run's
 *   own `finally` path could not finish: signs in, removes any active
 *   factor and recovery set, and stops.
 *
 * The account is a durable fictional identity read from a mounted file, not
 * created or deleted by this proof. Every setup secret, code, recovery
 * code, cookie, and CSRF value stays in process memory; only fixed step
 * names and outcomes reach output, matching second-factor.spec.ts's and
 * totp.spec.ts's withheld-log convention.
 *
 * Contract: docs/design/totp-second-factor-contract.md,
 * docs/design/totp-key-management.md, ADR 0049.
 */
import { expect, test, type BrowserContext, type Page, type Route } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import {
  codeForStep,
  stepAt,
  systemClock,
  TOTP_PERIOD_SECONDS,
} from './totp-fixture';

/**
 * The newest step an accepted code has used. The server accepts only a step
 * greater than the credential's last used step
 * (totp-second-factor-contract.md, "TOTP profile and code verification"),
 * so each accepted code waits for a newer step.
 */
let lastUsedStep = -1;

/** Returns a current-step code newer than every step already accepted. */
async function freshCode(page: Page, secret: string): Promise<string> {
  const waitMs = ((lastUsedStep + 1) * TOTP_PERIOD_SECONDS - Date.now() / 1000)
    * 1000;
  if (waitMs > 0) await page.waitForTimeout(Math.ceil(waitMs) + 500);
  const step = stepAt(systemClock().nowSeconds());
  lastUsedStep = step;
  return codeForStep(secret, step);
}

const MODE = process.env.ABOUTME_BROWSER_MODE ?? '';
const ORIGIN = 'https://aboutme.vn';
const ACCOUNT_PATH = '/uat-input/account.env';
const PENDING_COOKIE = '__Host-auth-pending';
const TOTP_LOGIN_INPUT = '#second-factor-totp-code';
const RECOVERY_INPUT = '#second-factor-recovery-code';
const TOTP_SETUP_INPUT = '#totp-code';
const PHONE = { height: 844, width: 390 };
const DESKTOP = { height: 900, width: 1440 };

const WAIT_RESPONSE_MS = 30_000;
const WAIT_NAVIGATION_MS = 60_000;
const WAIT_HYDRATE_MS = 30_000;

let recordedStage = 'start';
let tearingDown = false;

function stage(name: string): void {
  if (tearingDown) return;
  recordedStage = name;
  console.log(`${MODE}-stage:${name}`);
}

function beginTeardown(): void {
  tearingDown = true;
  console.log(`${MODE}-stage:cleanup-after-${recordedStage}`);
}

test.afterEach(({}, testInfo) => {
  if (testInfo.status === testInfo.expectedStatus) return;
  // Never forward the underlying error: it can hold a locator's matched
  // text, a response body, or another value this proof must keep secret.
  console.log(`${MODE}-stage:fail-at-${recordedStage}`);
});

/** Runs one step, naming it before and rethrowing a fixed message on error. */
async function step<T>(name: string, body: () => Promise<T>): Promise<T> {
  stage(name);
  try {
    return await body();
  } catch {
    throw new Error(`production step failed: ${name}`);
  }
}

// --- Account -----------------------------------------------------------

interface Account {
  readonly email: string;
  readonly password: string;
}

/** Reads the durable fictional account. Never logged, never persisted. */
async function readAccount(): Promise<Account> {
  const raw = await readFile(ACCOUNT_PATH, 'utf8');
  const fields = new Map<string, string>();
  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) continue;
    const at = trimmed.indexOf('=');
    if (at === -1) continue;
    fields.set(trimmed.slice(0, at), trimmed.slice(at + 1));
  }
  const email = fields.get('ABOUTME_TEST_EMAIL');
  const password = fields.get('ABOUTME_TEST_PASSWORD');
  if (email === undefined || email === '' || password === undefined || password === '') {
    throw new Error('production account file is missing required fields');
  }
  return { email, password };
}

// --- Page helpers, mirroring totp.spec.ts adapted for production ----------

async function hydrated(page: Page, timeout: number): Promise<void> {
  await expect.poll(
    () => page.evaluate(() => Boolean(
      (document.getElementById('__nuxt') as HTMLElement & {
        __vue_app__?: unknown;
      } | null)?.__vue_app__,
    )),
    { timeout },
  ).toBe(true);
}

async function gotoHydrated(page: Page, path: string): Promise<void> {
  await page.goto(path, { timeout: WAIT_NAVIGATION_MS });
  await hydrated(page, WAIT_HYDRATE_MS);
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

async function freshCSRF(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    const body = (await response.json()) as { data?: { csrfToken?: unknown } };
    const token = body.data?.csrfToken;
    if (response.status !== 200 || typeof token !== 'string' || token === '') {
      throw new Error('authenticated CSRF read failed');
    }
    return token;
  });
}

async function landedOnAppOrLogin(page: Page): Promise<'app' | 'login' | 'pending'> {
  await page.waitForURL(
    (url) => url.origin === ORIGIN && url.pathname !== '/login',
    { timeout: WAIT_NAVIGATION_MS },
  ).catch(() => undefined);
  const path = new URL(page.url()).pathname;
  if (path === '/login/second-factor') return 'pending';
  if (path.startsWith('/app/')) return 'app';
  return 'login';
}

async function passwordSignIn(
  page: Page,
  account: Account,
): Promise<'pending' | 'session'> {
  await gotoHydrated(page, '/login');
  await page.locator('#login-email').fill(account.email);
  await page.locator('#login-password').fill(account.password);
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/auth/password/login';
  }, { timeout: WAIT_RESPONSE_MS });
  await page.getByTestId('login-form').locator('button[type="submit"]').click();
  const status = (await response).status();
  const where = await landedOnAppOrLogin(page);
  if (status === 202) {
    if (where !== 'pending') throw new Error('pending login did not land on the pending page');
    await hydrated(page, WAIT_HYDRATE_MS);
    return 'pending';
  }
  if (status !== 204 || where !== 'app' || await meStatus(page) !== 200) {
    throw new Error('password sign-in did not establish a session');
  }
  return 'session';
}

async function signOut(page: Page): Promise<void> {
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(`${ORIGIN}/login`, { timeout: WAIT_NAVIGATION_MS });
}

async function forceReauthOnce(page: Page, pathname: string): Promise<void> {
  let spent = false;
  await page.route(`${ORIGIN}${pathname}`, async (route: Route) => {
    const url = new URL(route.request().url());
    if (spent || url.origin !== ORIGIN || url.pathname !== pathname) {
      await route.fallback();
      return;
    }
    spent = true;
    await route.fulfill({
      body: JSON.stringify({
        error: { code: 'reauth_required', message: 'reauthentication required' },
      }),
      contentType: 'application/json',
      headers: { 'Cache-Control': 'no-store' },
      status: 403,
    });
  });
}

async function reauthenticateWithPassword(page: Page, password: string): Promise<void> {
  await page.getByTestId('second-factor-reauth-password').waitFor();
  await page.getByLabel('Current password', { exact: true }).fill(password);
  await page.getByTestId('second-factor-reauth-submit').click();
  await expect(page.getByTestId('second-factor-reauth-password')).toHaveCount(0);
}

async function readRevealedCodes(page: Page): Promise<string[]> {
  const list = page.getByTestId('recovery-codes-list');
  await expect(list).toBeVisible();
  const codes = (await list.locator('li').allTextContents()).map((v) => v.trim());
  if (codes.length !== 10) throw new Error('recovery reveal did not carry ten codes');
  await page.getByTestId('recovery-reveal-close').click();
  await expect(page.getByTestId('recovery-codes-list')).toHaveCount(0);
  return codes;
}

async function submitRecoveryCode(page: Page, code: string): Promise<number> {
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/auth/second-factor/recovery/verify';
  }, { timeout: WAIT_RESPONSE_MS });
  const input = page.locator(RECOVERY_INPUT);
  await input.click();
  await input.fill(code);
  await input.press('Enter');
  return (await response).status();
}

async function completeWithRecovery(page: Page, code: string): Promise<void> {
  if (await submitRecoveryCode(page, code) !== 204) {
    throw new Error('recovery completion was rejected');
  }
  await page.waitForURL(
    (url) => url.origin === ORIGIN && url.pathname !== '/login/second-factor',
    { timeout: WAIT_NAVIGATION_MS },
  );
}

async function submitPendingTotpCode(page: Page, code: string): Promise<number> {
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/auth/second-factor/totp/verify';
  }, { timeout: WAIT_RESPONSE_MS });
  const input = page.locator(TOTP_LOGIN_INPUT);
  await input.click();
  await input.fill(code);
  // The recovery form's button has the same name, so scope to the TOTP form.
  await page.getByTestId('second-factor-totp-form')
    .locator('button[type="submit"]').click();
  return (await response).status();
}

/**
 * Answers the settings reauthentication on an enrolled account. The password
 * opens a pending reauthentication, and a fresh code from the current secret
 * completes it (second-factor-authentication.md, "Enrollment and
 * management": primary and an active factor must both be recent). The page
 * then returns to settings.
 */
async function reauthenticateEnrolled(
  page: Page,
  password: string,
  secret: string,
): Promise<void> {
  await page.getByTestId('second-factor-reauth-password').waitFor();
  await page.getByLabel('Current password', { exact: true }).fill(password);
  await Promise.all([
    page.waitForURL(`${ORIGIN}/login/second-factor`,
      { timeout: WAIT_NAVIGATION_MS }),
    page.getByTestId('second-factor-reauth-submit').click(),
  ]);
  await hydrated(page, WAIT_HYDRATE_MS);
  await completeWithTotp(page, await freshCode(page, secret));
  await page.waitForURL(`${ORIGIN}/app/settings/sessions`,
    { timeout: WAIT_NAVIGATION_MS });
  await hydrated(page, WAIT_HYDRATE_MS);
}

async function completeWithTotp(page: Page, code: string): Promise<void> {
  if (await submitPendingTotpCode(page, code) !== 204) {
    throw new Error('TOTP completion was rejected');
  }
  await page.waitForURL(
    (url) => url.origin === ORIGIN && url.pathname !== '/login/second-factor',
    { timeout: WAIT_NAVIGATION_MS },
  );
}

async function readSetupSecret(page: Page): Promise<string> {
  await expect(page.getByTestId('totp-setup-dialog')).toBeVisible();
  await expect(page.getByTestId('totp-qr')).toBeVisible();
  const grouped = (await page.getByTestId('totp-secret').textContent()) ?? '';
  const secret = grouped.replace(/\s+/gu, '');
  if (!/^[A-Z2-7]{32}$/u.test(secret)) throw new Error('setup secret is malformed');
  return secret;
}

async function startTotpSetup(page: Page): Promise<string> {
  const created = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/me/second-factor/totp/enrollment'
      && candidate.request().method() === 'POST';
  }, { timeout: WAIT_RESPONSE_MS });
  const startButton = (await page.getByTestId('totp-setup-start').count()) > 0
    ? page.getByTestId('totp-setup-start')
    : page.getByTestId('totp-setup-replace');
  await startButton.click();
  if ((await created).status() !== 200) throw new Error('TOTP setup did not start');
  return readSetupSecret(page);
}

async function submitSetupCode(page: Page, code: string): Promise<number> {
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/me/second-factor/totp/enrollment'
      && candidate.request().method() === 'PUT';
  }, { timeout: WAIT_RESPONSE_MS });
  const input = page.locator(TOTP_SETUP_INPUT);
  await input.click();
  await input.fill(code);
  await page.getByTestId('totp-code-submit').click();
  return (await response).status();
}

// --- Virtual authenticator, one credential at a time -----------------------

type CDPSend = (
  method: string,
  params?: Record<string, unknown>,
) => Promise<Record<string, unknown>>;

async function attachAuthenticator(
  context: BrowserContext,
  page: Page,
): Promise<string> {
  const session = await context.newCDPSession(page);
  const send = session.send.bind(session) as unknown as CDPSend;
  await send('WebAuthn.enable', { enableUI: false });
  const result = await send('WebAuthn.addVirtualAuthenticator', {
    options: {
      automaticPresenceSimulation: true,
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      protocol: 'ctap2',
      transport: 'internal',
    },
  });
  const id = result.authenticatorId;
  if (typeof id !== 'string' || id === '') {
    throw new Error('virtual authenticator was not created');
  }
  return id;
}

// --- Teardown ---------------------------------------------------------------

/**
 * Removes every active factor and the recovery set for the account, leaving
 * it unenrolled. Never deletes the account itself: it is a durable identity
 * reused across releases (docs/runbooks/totp-keys.md "Production proofs").
 * A recovery code, when supplied, completes a pending row this cleanup pass
 * itself cannot otherwise close.
 */
async function removeEveryFactor(
  page: Page,
  account: Account,
  recoveryCode: string,
): Promise<boolean> {
  try {
    // Leave the app page first. The previous step may have just landed on a
    // signed-in page whose startup reads (`/me`, then the resume list) are
    // still running; clearing cookies under it turns the next read into a
    // 401 that the page logs as a console error.
    await page.goto('about:blank');
    await page.context().clearCookies();
    const outcome = await passwordSignIn(page, account);
    if (outcome === 'pending') {
      if (recoveryCode === '') return false;
      await completeWithRecovery(page, recoveryCode);
    }
    await gotoHydrated(page, '/app/settings/sessions');
    const csrf = await freshCSRF(page);
    // Best-effort: remove TOTP, then every passkey, then sign every other
    // session out. A route this account never used answers 404 and is
    // ignored.
    await page.evaluate(async (token) => {
      await fetch('/api/v1/me/second-factor/totp', {
        credentials: 'include',
        headers: { 'X-CSRF-Token': token },
        method: 'DELETE',
      });
    }, csrf);
    let removedPasskey = true;
    while (removedPasskey) {
      removedPasskey = await page.evaluate(async (token) => {
        const state = await fetch('/api/v1/me/second-factor', {
          cache: 'no-store',
          credentials: 'include',
        }).then((r) => r.json() as Promise<{
          data?: { passkeys?: ReadonlyArray<{ id?: unknown }> };
        }>);
        const first = state.data?.passkeys?.[0];
        const id = typeof first?.id === 'string' ? first.id : null;
        if (id === null) return false;
        const response = await fetch(`/api/v1/me/second-factor/passkeys/${id}`, {
          credentials: 'include',
          headers: { 'X-CSRF-Token': token },
          method: 'DELETE',
        });
        return response.status === 204;
      }, csrf);
    }
    return true;
  } catch {
    return false;
  }
}

// --- totp-prod-flag-off -----------------------------------------------------

test('proves the pre-activation production state', async ({ context, page }) => {
  test.skip(MODE !== 'totp-prod-flag-off', 'flag-off mode only');

  const account = await readAccount();
  let recoveryCleanupCode = '';

  try {
    await step('health', async () => {
      const response = await page.request.get(`${ORIGIN}/readyz`);
      if (!response.ok()) throw new Error('health check failed');
    });

    await step('existing-unenrolled', async () => {
      const outcome = await passwordSignIn(page, account);
      if (outcome !== 'session') throw new Error('account was not unenrolled');
    });

    let recoveryCodes: string[] = [];
    await step('passkey-enroll', async () => {
      await attachAuthenticator(context, page);
    });
    await step('passkey-enroll-complete', async () => {
      await gotoHydrated(page, '/app/settings/sessions');
      await forceReauthOnce(page, '/api/v1/me/second-factor/passkeys/options');
      await page.getByTestId('passkey-add').click();
      await expect(page.getByTestId('second-factor-reauth-password')).toBeVisible();
      await reauthenticateWithPassword(page, account.password);
      const created = page.waitForResponse((candidate) => {
        const url = new URL(candidate.url());
        return url.origin === ORIGIN
          && url.pathname === '/api/v1/me/second-factor/passkeys'
          && candidate.request().method() === 'POST';
      }, { timeout: WAIT_RESPONSE_MS });
      await page.getByTestId('passkey-add').click();
      if ((await created).status() !== 201) throw new Error('passkey enrollment failed');
      recoveryCodes = await readRevealedCodes(page);
    });

    await step('passkey-login', async () => {
      await signOut(page);
      if (await passwordSignIn(page, account) !== 'pending') {
        throw new Error('enrolled account did not require a second factor');
      }
      await page.getByTestId('second-factor-passkey-button').click();
      await page.waitForURL(
        (url) => url.origin === ORIGIN && url.pathname !== '/login/second-factor',
        { timeout: WAIT_NAVIGATION_MS },
      );
      if (await meStatus(page) !== 200) throw new Error('passkey login did not establish a session');
    });

    await step('recovery-login', async () => {
      await signOut(page);
      if (await passwordSignIn(page, account) !== 'pending') {
        throw new Error('enrolled account did not require a second factor');
      }
      await completeWithRecovery(page, recoveryCodes[0] as string);
      if (await meStatus(page) !== 200) throw new Error('recovery login did not establish a session');
    });
    recoveryCleanupCode = recoveryCodes[1] as string;

    await step('totp-enrollment-closed', async () => {
      const csrf = await freshCSRF(page);
      const status = await page.evaluate(async (token) => {
        const response = await fetch('/api/v1/me/second-factor/totp/enrollment', {
          body: '{}',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
          method: 'POST',
        });
        return response.status;
      }, csrf);
      if (status !== 404) throw new Error('TOTP enrollment did not answer closed');
    });
  } finally {
    beginTeardown();
    await removeEveryFactor(page, account, recoveryCleanupCode);
  }
});

// --- totp-prod-enabled -------------------------------------------------

test('proves the activated production authenticator-app journey', async ({
  context,
  page,
}) => {
  test.skip(MODE !== 'totp-prod-enabled', 'enabled mode only');

  const account = await readAccount();
  let recoveryCleanupCode = '';

  try {
    await step('sign-in-unenrolled', async () => {
      const outcome = await passwordSignIn(page, account);
      if (outcome !== 'session') throw new Error('account was not unenrolled');
    });

    let secret = '';
    let recoveryCodes: string[] = [];
    await step('totp-enroll', async () => {
      await gotoHydrated(page, '/app/settings/sessions');
      await forceReauthOnce(page, '/api/v1/me/second-factor/totp/enrollment');
      await page.getByTestId('totp-setup-start').click();
      await expect(page.getByTestId('second-factor-reauth-password')).toBeVisible();
      await reauthenticateWithPassword(page, account.password);
      secret = await startTotpSetup(page);
      if (await submitSetupCode(page, await freshCode(page, secret)) !== 200) {
        throw new Error('TOTP enrollment completion was rejected');
      }
      recoveryCodes = await readRevealedCodes(page);
    });

    await step('totp-pending-login', async () => {
      await signOut(page);
      if (await passwordSignIn(page, account) !== 'pending') {
        throw new Error('enrolled account did not require a second factor');
      }
      await completeWithTotp(page, await freshCode(page, secret));
      if (await meStatus(page) !== 200) throw new Error('TOTP login did not establish a session');
    });

    // The old-secret rejection after replacement is dropped here: the hosted
    // proof (totp.spec.ts, "replace-old-secret-rejected") already covers it,
    // and production has only one account to spend its pending budget on
    // (docs/runbooks/totp-keys.md "Production proofs").
    await step('totp-replace', async () => {
      await gotoHydrated(page, '/app/settings/sessions');
      await forceReauthOnce(page, '/api/v1/me/second-factor/totp/enrollment');
      await page.getByTestId('totp-setup-replace').click();
      await expect(page.getByTestId('second-factor-reauth-password')).toBeVisible();
      await reauthenticateEnrolled(page, account.password, secret);
      secret = await startTotpSetup(page);
      if (await submitSetupCode(page, await freshCode(page, secret)) !== 200) {
        throw new Error('TOTP replacement completion was rejected');
      }
      await signOut(page);
      if (await passwordSignIn(page, account) !== 'pending') {
        throw new Error('replaced account did not require a second factor');
      }
      await completeWithTotp(page, await freshCode(page, secret));
    });

    await step('recovery-completion', async () => {
      await signOut(page);
      if (await passwordSignIn(page, account) !== 'pending') {
        throw new Error('enrolled account did not require a second factor');
      }
      await completeWithRecovery(page, recoveryCodes[0] as string);
      if (await meStatus(page) !== 200) throw new Error('recovery login did not establish a session');
    });
    recoveryCleanupCode = recoveryCodes[1] as string;

    // Passkey coexistence and both locales share one pending row and one
    // completion: the account has only 8 admitted attempts to spend here
    // (docs/runbooks/totp-keys.md "Production proofs"), so this proves
    // passkey and authenticator-app both list on the pending page, in both
    // locales, and completes once with TOTP rather than the passkey.
    await step('passkey-coexistence-and-locales', async () => {
      await attachAuthenticator(context, page);
      await gotoHydrated(page, '/app/settings/sessions');
      const created = page.waitForResponse((candidate) => {
        const url = new URL(candidate.url());
        return url.origin === ORIGIN
          && url.pathname === '/api/v1/me/second-factor/passkeys'
          && candidate.request().method() === 'POST';
      }, { timeout: WAIT_RESPONSE_MS });
      await page.getByTestId('passkey-add').click();
      if ((await created).status() !== 201) throw new Error('passkey enrollment failed');
      await expect(page.getByTestId('recovery-codes-list')).toHaveCount(0);

      await signOut(page);
      await context.addCookies([{ name: 'aboutme-locale', url: ORIGIN, value: 'vi' }]);
      await page.setViewportSize(PHONE);
      if (await passwordSignIn(page, account) !== 'pending') {
        throw new Error('account did not require a second factor');
      }
      await expect(page.getByTestId('second-factor-passkey')).toBeVisible();
      await expect(page.getByTestId('second-factor-totp')).toBeVisible();
      const viOverflow = await page.evaluate(() =>
        document.documentElement.scrollWidth <= window.innerWidth);
      if (!viOverflow) throw new Error('pending page overflowed its viewport');

      // Same pending row, reloaded under the other locale and viewport.
      await context.addCookies([{ name: 'aboutme-locale', url: ORIGIN, value: 'en' }]);
      await page.setViewportSize(DESKTOP);
      await gotoHydrated(page, '/login/second-factor');
      await expect(page.getByTestId('second-factor-passkey')).toBeVisible();
      await expect(page.getByTestId('second-factor-totp')).toBeVisible();
      const enOverflow = await page.evaluate(() =>
        document.documentElement.scrollWidth <= window.innerWidth);
      if (!enOverflow) throw new Error('pending page overflowed its viewport');

      await completeWithTotp(page, await freshCode(page, secret));
      if (await meStatus(page) !== 200) throw new Error('TOTP login did not establish a session');
    });

    await step('removal', async () => {
      await gotoHydrated(page, '/app/settings/sessions');
      await forceReauthOnce(page, '/api/v1/me/second-factor/totp');
      await page.getByTestId('totp-remove').click();
      await expect(page.getByRole('alertdialog')).toBeVisible();
      await page.locator('[data-action="totp-remove-confirm"]').click();
      await expect(page.getByTestId('second-factor-reauth-password')).toBeVisible();
      await reauthenticateEnrolled(page, account.password, secret);
      await page.getByTestId('totp-remove').click();
      await expect(page.getByRole('alertdialog')).toBeVisible();
      await page.locator('[data-action="totp-remove-confirm"]').click();
      await expect(page.getByTestId('totp-removed-success')).toBeVisible();
    });
  } finally {
    beginTeardown();
    await removeEveryFactor(page, account, recoveryCleanupCode);
  }
});

// --- totp-prod-cleanup --------------------------------------------------

test('removes every production TOTP proof factor', async ({ page }) => {
  test.skip(MODE !== 'totp-prod-cleanup', 'cleanup mode only');

  const account = await readAccount();
  try {
    stage('cleanup-only');
    const removed = await removeEveryFactor(page, account, '');
    if (!removed) throw new Error('production step failed: cleanup-only');
  } finally {
    beginTeardown();
  }
});
