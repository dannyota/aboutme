/**
 * Page journeys shared by the two second-factor browser proofs: warming and
 * hydration, registration, password sign-in and sign-out, recovery codes,
 * reauthentication, agent grants, and account teardown. They record stages
 * through the harness in `second-factor-lib.ts`.
 */
import {
  expect,
  type BrowserContext,
  type Page,
  type Route,
} from '@playwright/test';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { freshCSRF } from './editor-fixtures';
import { type DiagnosticCounters } from './harness-lib';
import {
  APP_LANDINGS,
  type CaptureClient,
  CLIENT_NAME_PATH,
  cookieValue,
  isSharded,
  landedAfter,
  landingCategory,
  LOCALE_COOKIE,
  ORIGIN,
  RECOVERY_INPUT,
  REDIRECT_URI,
  SESSION_COOKIE,
  stage,
  WAIT_HYDRATE_MS,
  WAIT_LANDING_MS,
  WAIT_NAVIGATION_MS,
  WAIT_RESPONSE_MS,
  WAIT_WARM_MS,
} from './second-factor-lib';

// --- Page helpers ----------------------------------------------------------

export async function setLocale(
  context: BrowserContext,
  locale: 'en' | 'vi',
): Promise<void> {
  await context.addCookies([
    { name: LOCALE_COOKIE, url: ORIGIN, value: locale },
  ]);
}

/**
 * Pages the journey opens that are safe to warm with no session, each with
 * the closed word that names it. A first visit is compiled on demand by the
 * harness's dev server, so paying them once, up front and in a stage of their
 * own, keeps a later failure about the behaviour under test rather than about
 * a cold route. None of these issues a request while signed out beyond the
 * reads each proof's console filter already accepts.
 *
 * The two pages that take their token from the URL fragment are deliberately
 * absent. Each reads the fragment during setup, on the client only, because
 * the token must never reach the server, and with no fragment each sets its
 * own failure state before hydration. The server render and the first client
 * render then disagree and the development build says so on the console.
 * Every other page here starts in the same state on both sides: the ones
 * that fetch do it after mount or with server rendering turned off. The
 * journey only opens the fragment pages with a real token, where they are
 * clean, so each takes its first visit there instead.
 */
export const WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/register', 'register'],
  ['/login', 'login'],
  ['/login/second-factor', 'login-second-factor'],
  ['/forgot-password', 'forgot-password'],
];

/**
 * Pages that require a session. Warming these signed out would make the
 * account reads behind them answer 401, which is correct behaviour but noise
 * each proof would then have to accept everywhere, so they are warmed once
 * the journey has a session instead. The one landing that reaches an app
 * page before that runs under the landing bound.
 */
export const SIGNED_IN_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/app/resumes', 'app-resumes'],
  ['/app/settings/sessions', 'app-settings-sessions'],
];

/** The signed-out page the disabled-enrollment journey opens first. */
export const DISABLED_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/login', 'login'],
];

/** The counter classes a warm visit may move, as closed words. */
export type CounterClass = 'certificate' | 'console' | 'external' | 'page';

/** The first counter class that moved, or null when the visit was clean. */
export function dirtiedCounter(
  before: DiagnosticCounters,
  after: DiagnosticCounters,
): CounterClass | null {
  if (after.certificateErrors !== before.certificateErrors) {
    return 'certificate';
  }
  if (after.consoleErrors !== before.consoleErrors) return 'console';
  if (after.externalRequests !== before.externalRequests) return 'external';
  if (after.pageErrors !== before.pageErrors) return 'page';
  return null;
}

/**
 * Opens each route once, so its first compile happens in a stage that names
 * it, and checks the diagnostic counters after every visit rather than once
 * at the end. A page that dirties a counter names itself and the counter
 * class before the assertion fails.
 */
export async function warmRoutes(
  page: Page,
  routes: ReadonlyArray<readonly [string, string]>,
  counters: DiagnosticCounters,
): Promise<void> {
  for (const [path, token] of routes) {
    stage(`warm-${token}`);
    const before = { ...counters };
    await page.goto(path, { timeout: WAIT_WARM_MS });
    await hydrated(page, WAIT_WARM_MS);
    const dirty = dirtiedCounter(before, counters);
    if (dirty !== null) stage(`warm-dirty-${token}-${dirty}`);
    expect(dirty).toBeNull();
  }
}

export async function hydrated(page: Page, timeout: number): Promise<void> {
  await expect.poll(
    () => page.evaluate(() => Boolean(
      (document.getElementById('__nuxt') as HTMLElement & {
        __vue_app__?: unknown;
      } | null)?.__vue_app__,
    )),
    { timeout },
  ).toBe(true);
}

export async function gotoHydrated(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await hydrated(page, WAIT_HYDRATE_MS);
}

/** Opens a page the warm pass skips, at first-visit cost. */
export async function gotoFirstVisit(page: Page, url: string): Promise<void> {
  await page.goto(url, { timeout: WAIT_WARM_MS });
  await hydrated(page, WAIT_WARM_MS);
}

export async function meStatus(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    return response.status;
  });
}

// --- Account journeys ------------------------------------------------------

/** Registers a fictional account and verifies it through the captured mail. */
export async function registerVerified(
  page: Page,
  capture: CaptureClient,
  email: string,
  password: string,
  name: string,
): Promise<void> {
  stage('register-open');
  await gotoHydrated(page, '/register');
  // The form is inert and invisible until the capabilities read resolves, so
  // this is the first step that depends on the page's own data.
  stage('register-form-ready');
  await expect(page.getByTestId('register-form')).toBeVisible();
  stage('register-fill');
  await page.getByLabel('Name').fill(name);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password', { exact: true }).fill(password);
  stage('register-submit');
  await page.getByRole('button', { name: 'Create account' }).click();
  stage('register-accepted');
  await expect(page.getByTestId('register-success')).toBeVisible();
  stage('register-await-mail');
  const token = await capture.waitForToken('verify', email);
  stage('register-verify-open');
  await gotoFirstVisit(page, `${ORIGIN}/verify-email#token=${token}`);
  stage('register-verified');
  await expect(page.getByTestId('verify-success')).toBeVisible();
}

/**
 * Submits the password sign-in form and reports whether the account was sent
 * to the pending second-factor page instead of receiving a session. The
 * fields are found by id and the button by its form, not by label, because
 * the locale journey signs in with the page in Vietnamese.
 */
export async function passwordSignIn(
  page: Page,
  email: string,
  password: string,
  landingTimeoutMs: number = WAIT_LANDING_MS,
): Promise<'pending' | 'session'> {
  stage('sign-in-open');
  await gotoHydrated(page, '/login');
  stage('sign-in-fill');
  await page.locator('#login-email').fill(email);
  await page.locator('#login-password').fill(password);
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/auth/password/login';
  }, { timeout: WAIT_RESPONSE_MS });
  stage('sign-in-submit');
  await page.getByTestId('login-form').locator('button[type="submit"]')
    .click();
  const status = (await response).status();
  stage('sign-in-landing');
  const where = await landedAfter(page, '/login', landingTimeoutMs);
  if (status === 202) {
    expect(where).toBe('landing-second-factor');
    await hydrated(page, WAIT_HYDRATE_MS);
    return 'pending';
  }
  expect(status).toBe(204);
  expect(APP_LANDINGS).toContain(where);
  expect(await meStatus(page)).toBe(200);
  return 'session';
}

/**
 * Logs out from the settings page in English, whatever locale the previous
 * step left. In a sharded run, no-ops without navigating when no session
 * exists yet: a shard's first role has no session yet, and visiting
 * /app/settings/sessions while signed out logs unexpected 401s from the API
 * calls the settings page makes on the way to redirecting to /login. The
 * unsharded default always expects a session and still fails if there is
 * none, so a broken logout keeps failing an unsharded run. A caller that
 * wants another locale sets it after this returns.
 */
export async function signOut(page: Page): Promise<void> {
  await setLocale(page.context(), 'en');
  if (isSharded()
    && await cookieValue(page.context(), SESSION_COOKIE) === null) return;
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(`${ORIGIN}/login`, { timeout: WAIT_NAVIGATION_MS });
  // The logout response's Clear-Site-Data drops every cookie. The app writes
  // the language choice back before it leaves for /login, so English
  // survives without the proof pinning it again.
  await expect.poll(
    () => cookieValue(page.context(), LOCALE_COOKIE),
    { timeout: WAIT_RESPONSE_MS },
  ).toBe('en');
}

/** Asserts the browser holds a signed-in app session, wherever it landed. */
export async function expectSignedInApp(page: Page): Promise<void> {
  expect(APP_LANDINGS).toContain(landingCategory(page.url()));
  expect(await meStatus(page)).toBe(200);
}

/** Submits one recovery code by keyboard and returns the response status. */
export async function submitRecoveryCode(page: Page, code: string): Promise<number> {
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

/** Completes the open pending authentication with one recovery code. */
export async function completeWithRecovery(page: Page, code: string): Promise<void> {
  expect(await submitRecoveryCode(page, code)).toBe(204);
  await landedAfter(page, '/login/second-factor');
}

/** Answers the settings reauthentication prompt with the account password. */
export async function reauthenticateWithPassword(
  page: Page,
  password: string,
): Promise<void> {
  await page.getByTestId('second-factor-reauth-password').waitFor();
  await page.getByLabel('Current password', { exact: true }).fill(password);
  await page.getByTestId('second-factor-reauth-submit').click();
  await expect(page.getByTestId('second-factor-reauth-password'))
    .toHaveCount(0);
}

/**
 * Makes the next request to `pathname` answer `403 reauth_required` inside the
 * browser, which drives the settings block into its reauthentication branch
 * without waiting out the fifteen-minute window. The request never leaves the
 * page, so the real route and its budgets are untouched.
 */
export async function forceReauthOnce(page: Page, pathname: string): Promise<void> {
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
        error: {
          code: 'reauth_required',
          message: 'recent reauthentication is required',
        },
      }),
      contentType: 'application/json',
      headers: { 'Cache-Control': 'no-store' },
      status: 403,
    });
  });
}

/** Reads the one-time recovery codes from the open reveal dialog. */
export async function readRevealedCodes(page: Page): Promise<string[]> {
  const reveal = page.getByTestId('recovery-reveal');
  const list = page.getByTestId('recovery-codes-list');
  await expect(list).toBeVisible();
  const codes = (await list.locator('li').allTextContents())
    .map((value) => value.trim());
  expect(codes).toHaveLength(10);
  for (const code of codes) expect(code).toMatch(/^amr_[0-9A-Z-]+$/u);
  await expect(reveal.getByRole('button', { name: 'Copy' })).toBeVisible();
  await expect(reveal.getByRole('button', { name: 'Download' })).toBeVisible();
  return codes;
}

/** Closes the reveal and proves the plaintext left every page-held store. */
export async function closeRevealAndProveCleared(
  page: Page,
  codes: readonly string[],
): Promise<void> {
  await page.getByTestId('recovery-reveal-close').click();
  await expect(page.getByTestId('recovery-codes-list')).toHaveCount(0);
  const leaked = await page.evaluate((values) => {
    const blobs = [document.documentElement.outerHTML];
    for (const store of [localStorage, sessionStorage]) {
      for (let index = 0; index < store.length; index += 1) {
        const key = store.key(index);
        if (key !== null) blobs.push(key, store.getItem(key) ?? '');
      }
    }
    return values.some((value) => blobs.some((blob) => blob.includes(value)));
  }, [...codes]);
  expect(leaked).toBe(false);
}

// --- Agent grants ----------------------------------------------------------

/** Registers a connected agent and completes one consent round trip. */
export async function createAgentGrant(
  context: BrowserContext,
  page: Page,
): Promise<string> {
  const clientName = (await readFile(CLIENT_NAME_PATH, 'utf8')).trim();
  const clientID = await page.evaluate(
    async ({ name, redirectURI }) => {
      const response = await fetch('/oauth/register', {
        body: JSON.stringify({
          client_name: name,
          redirect_uris: [redirectURI],
          token_endpoint_auth_method: 'none',
        }),
        headers: { 'Content-Type': 'application/json' },
        method: 'POST',
      });
      const body = (await response.json()) as { client_id?: unknown };
      if (response.status !== 201 || typeof body.client_id !== 'string') {
        throw new Error('OAuth registration failed');
      }
      return body.client_id;
    },
    { name: clientName, redirectURI: REDIRECT_URI },
  );
  const verifier = randomBytes(48).toString('base64url');
  const challenge = createHash('sha256').update(verifier).digest('base64url');
  const state = randomUUID();
  let callback: URL | undefined;
  await context.route(`${REDIRECT_URI}**`, async (route: Route) => {
    callback = new URL(route.request().url());
    await route.fulfill({
      body: 'complete',
      contentType: 'text/plain',
      status: 200,
    });
  });
  const query = new URLSearchParams({
    client_id: clientID,
    code_challenge: challenge,
    code_challenge_method: 'S256',
    redirect_uri: REDIRECT_URI,
    response_type: 'code',
    scope: 'resumes:read resumes:write',
    state,
  });
  await gotoHydrated(page, `/oauth/authorize?${query.toString()}`);
  await Promise.all([
    page.waitForURL(`${REDIRECT_URI}**`, { timeout: WAIT_NAVIGATION_MS }),
    page.getByRole('button', { name: 'Approve' }).click(),
  ]);
  expect(callback?.searchParams.get('state')).toBe(state);
  const code = callback?.searchParams.get('code');
  expect(code).toMatch(/^[A-Za-z0-9_-]+$/u);
  await gotoHydrated(page, '/app/resumes');
  const accessToken = await page.evaluate(
    async ({ authorizationCode, codeVerifier, id, redirectURI }) => {
      const response = await fetch('/oauth/token', {
        body: new URLSearchParams({
          client_id: id,
          code: authorizationCode,
          code_verifier: codeVerifier,
          grant_type: 'authorization_code',
          redirect_uri: redirectURI,
        }),
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        method: 'POST',
      });
      const body = (await response.json()) as { access_token?: unknown };
      if (response.status !== 200 || typeof body.access_token !== 'string') {
        throw new Error('OAuth token exchange failed');
      }
      return body.access_token;
    },
    {
      authorizationCode: code as string,
      codeVerifier: verifier,
      id: clientID,
      redirectURI: REDIRECT_URI,
    },
  );
  await context.unroute(`${REDIRECT_URI}**`);
  return accessToken;
}

/** The status a connected agent gets when it lists tools with its token. */
export async function agentToolsStatus(page: Page, token: string): Promise<number> {
  return page.evaluate(async (bearer) => {
    const response = await fetch('/mcp', {
      body: JSON.stringify({
        id: 1,
        jsonrpc: '2.0',
        method: 'tools/list',
        params: {},
      }),
      cache: 'no-store',
      credentials: 'omit',
      headers: {
        'Accept': 'application/json, text/event-stream',
        'Authorization': `Bearer ${bearer}`,
        'Content-Type': 'application/json',
      },
      method: 'POST',
    });
    return response.status;
  }, token);
}

/** One locale and width a pending sign-in is completed at by keyboard. */
export interface PendingLocaleCase {
  readonly code: string;
  readonly email: string;
  readonly locale: 'en' | 'vi';
  readonly password: string;
  readonly viewport: { readonly height: number; readonly width: number };
}

// --- Teardown --------------------------------------------------------------

export interface AccountTeardown {
  readonly email: string;
  readonly password: string;
  /** An unused recovery code, when the account is still enrolled. */
  readonly recoveryCode: string;
}

/**
 * Removes one fictional account. An enrolled account finishes its pending
 * sign-in with a spare recovery code, which sets both verification times and
 * therefore satisfies the deletion boundary without a further round trip.
 * `landingTimeoutMs` bounds the sign-in landing; a proof whose accounts land
 * fast in teardown passes a tighter bound than the first-visit one.
 */
export async function deleteAccount(
  page: Page,
  account: AccountTeardown,
  landingTimeoutMs: number = WAIT_LANDING_MS,
): Promise<boolean> {
  if (account.email === '') return true;
  try {
    // Leave the app page first. The previous step may have just landed on a
    // signed-in page whose startup reads (`/me`, then the resume list) are
    // still running; clearing cookies under it turns the next read into a
    // 401 that the page logs as a console error.
    await page.goto('about:blank');
    await page.context().clearCookies();
    await setLocale(page.context(), 'en');
    const outcome = await passwordSignIn(
      page, account.email, account.password, landingTimeoutMs,
    );
    if (outcome === 'pending') {
      if (account.recoveryCode === '') return false;
      await completeWithRecovery(page, account.recoveryCode);
    } else {
      await gotoHydrated(page, '/app/settings/sessions');
      const reauth = await page.evaluate(
        async ({ secret, token }) => {
          const response = await fetch('/api/v1/auth/password/reauth', {
            body: JSON.stringify({ password: secret }),
            cache: 'no-store',
            credentials: 'include',
            headers: {
              'Content-Type': 'application/json',
              'X-CSRF-Token': token,
            },
            method: 'POST',
          });
          return response.status;
        },
        { secret: account.password, token: await freshCSRF(page) },
      );
      if (reauth !== 204) return false;
    }
    return await deleteSignedInAccount(page);
  } catch {
    return false;
  }
}

/** Deletes the account the current page is signed into. */
export async function deleteSignedInAccount(page: Page): Promise<boolean> {
  await gotoHydrated(page, '/app/settings/sessions');
  if (await meStatus(page) !== 200) return false;
  const csrf = await freshCSRF(page);
  const status = await page.evaluate(async (token) => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
      headers: { 'X-CSRF-Token': token },
      method: 'DELETE',
    });
    return response.status;
  }, csrf);
  return status === 204;
}
