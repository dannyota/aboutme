import {
  expect,
  type BrowserContext,
  type Page,
  type Route,
} from '@playwright/test';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { constants } from 'node:fs';
import { open, readFile } from 'node:fs/promises';
import { freshCSRF } from './editor-fixtures';
import { type DiagnosticCounters } from './harness-lib';
import {
  APP_LANDINGS,
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

export async function setLocale(
  context: BrowserContext,
  locale: 'en' | 'vi',
): Promise<void> {
  await context.addCookies([
    { name: LOCALE_COOKIE, url: ORIGIN, value: locale },
  ]);
}

export const WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/login', 'login'],
  ['/login/second-factor', 'login-second-factor'],
  ['/forgot-password', 'forgot-password'],
];

export const SIGNED_IN_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/app/resumes', 'app-resumes'],
  ['/app/settings/sessions', 'app-settings-sessions'],
];

export const DISABLED_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/login', 'login'],
];

export type CounterClass = 'certificate' | 'console' | 'external' | 'page';

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

interface FactorLogin {
  readonly email: string;
  readonly password: string;
}

export async function readSecondFactorAccounts(
  proof: 'passkey' | 'totp',
): Promise<(role: string) => FactorLogin> {
  stage('fixture-accounts');
  const invalid = (): Error => new Error('invalid private second-factor accounts');
  const file = await open('/uat-input/second-factor-accounts.json',
    constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK)
    .catch(() => { throw invalid(); });
  let body: string;
  try {
    const info = await file.stat();
    if (!info.isFile() || (info.mode & 0o7777) !== 0o600
      || info.uid !== process.getuid?.() || info.nlink !== 1 || info.size > 8192) {
      throw invalid();
    }
    const bytes = Buffer.alloc(8193);
    let size = 0;
    while (size < bytes.length) {
      const { bytesRead } = await file.read(bytes, size, bytes.length - size, size);
      if (bytesRead === 0) break;
      size += bytesRead;
    }
    if (size > 8192) throw invalid();
    body = bytes.subarray(0, size).toString('utf8');
  } finally {
    await file.close();
  }
  let doc: unknown;
  try { doc = JSON.parse(body); } catch { throw invalid(); }
  if (JSON.stringify(doc) + '\n' !== body || doc === null || typeof doc !== 'object') {
    throw invalid();
  }
  const document = doc as Record<string, unknown>;
  const roles = proof === 'passkey' ? ['primary', 'recovery', 'attempts']
    : ['primary', 'skew', 'replay', 'concurrent', 'replace', 'epoch', 'locale', 'recovery', 'attempts'];
  if (Object.keys(document).sort().join(',') !== 'accounts,proof,schemaVersion'
    || document.schemaVersion !== 1 || document.proof !== proof
    || !Array.isArray(document.accounts) || document.accounts.length !== roles.length) {
    throw invalid();
  }
  const accounts = new Map<string, FactorLogin>();
  const identities = new Set<string>();
  for (const [index, entry] of document.accounts.entries()) {
    if (entry === null || typeof entry !== 'object') throw invalid();
    const account = entry as Record<string, unknown>;
    const role = roles[index] as string;
    if (Object.keys(account).sort().join(',') !== 'email,id,password,role'
      || account.role !== role || typeof account.id !== 'string'
      || !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u.test(account.id)
      || identities.has(account.id) || typeof account.email !== 'string'
      || !new RegExp(`^pa-test-${proof}-${role}-[0-9a-f]{16}@example\\.invalid$`, 'u').test(account.email)
      || typeof account.password !== 'string' || !/^[A-Za-z0-9_-]{32}$/u.test(account.password)) {
      throw invalid();
    }
    identities.add(account.id);
    accounts.set(role, { email: account.email, password: account.password });
  }
  return (role) => {
    const account = accounts.get(role);
    if (account === undefined) throw invalid();
    return account;
  };
}

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

export async function signOut(page: Page): Promise<void> {
  await setLocale(page.context(), 'en');
  if (isSharded()
    && await cookieValue(page.context(), SESSION_COOKIE) === null) return;
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(`${ORIGIN}/login`, { timeout: WAIT_NAVIGATION_MS });
  await expect.poll(
    () => cookieValue(page.context(), LOCALE_COOKIE),
    { timeout: WAIT_RESPONSE_MS },
  ).toBe('en');
}

export async function expectSignedInApp(page: Page): Promise<void> {
  expect(APP_LANDINGS).toContain(landingCategory(page.url()));
  expect(await meStatus(page)).toBe(200);
}

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

export async function completeWithRecovery(page: Page, code: string): Promise<void> {
  expect(await submitRecoveryCode(page, code)).toBe(204);
  await landedAfter(page, '/login/second-factor');
}

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

export interface PendingLocaleCase {
  readonly code: string;
  readonly email: string;
  readonly locale: 'en' | 'vi';
  readonly password: string;
  readonly viewport: { readonly height: number; readonly width: number };
}

export interface AccountTeardown {
  readonly email: string;
  readonly password: string;
  readonly recoveryCode: string;
}

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
