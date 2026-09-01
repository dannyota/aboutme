import {
  expect,
  test,
  type BrowserContext,
  type Cookie,
} from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import {
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
} from './harness-lib';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/auth-proof.json';

interface CookieAttributes {
  hostOnly: boolean;
  httpOnly: boolean;
  path: string;
  sameSite: string;
  secure: boolean;
}

interface BrowserSecretState extends Window {
  __aboutmeUATCSRF?: string;
}

async function cookieAttributes(
  context: BrowserContext,
  name: string,
): Promise<CookieAttributes | null> {
  const cookies = await context.cookies(ORIGIN);
  const cookie: Cookie | undefined = cookies.find((item) => item.name === name);
  if (!cookie) return null;
  return {
    hostOnly: cookie.domain === new URL(ORIGIN).hostname,
    httpOnly: cookie.httpOnly,
    path: cookie.path,
    sameSite: cookie.sameSite,
    secure: cookie.secure,
  };
}

function requiredCookieAttributes(): CookieAttributes {
  return {
    hostOnly: true,
    httpOnly: true,
    path: '/',
    sameSite: 'Lax',
    secure: true,
  };
}

test('proves trusted local Google authentication and CSRF boundaries', async ({
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  const attachPageDiagnostics = pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedConsoleError,
  });
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  await context.route('**/*', async (route) => {
    if (!isAllowedHTTPURL(route.request().url())) {
      counters.externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
  await context.routeWebSocket('**/*', async (webSocket) => {
    if (!isAllowedWebSocketURL(webSocket.url())) {
      counters.externalRequests += 1;
      await webSocket.close({ code: 1008, reason: 'blocked' });
      return;
    }
    webSocket.connectToServer();
  });

  // 1. Begin with an anonymous browser at the public login page.
  const loginResponse = await page.goto('/login');
  expect(loginResponse?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  expect((await context.cookies(ORIGIN)).map((cookie) => cookie.name)).not.toContain(
    '__Host-session',
  );

  // 2. Start login and inspect only the transaction cookie's attributes.
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/__uat/oauth/google/authorize'
    ),
    page.getByRole('link', { name: 'Continue with Google' }).click(),
  ]);
  expect(await cookieAttributes(context, '__Host-oauth-tx')).toEqual(
    requiredCookieAttributes(),
  );

  // 3. Select the fixed local account at the same-origin provider page.
  const developmentAccount = page.getByLabel(
    'Development User — developer@example.invalid',
  );
  await expect(developmentAccount).toBeChecked();

  // 4. Complete callback and prove the transaction handle is gone.
  await Promise.all([
    page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/'),
    page.getByRole('button', { name: 'Continue with Google' }).click(),
  ]);
  expect((await context.cookies(ORIGIN)).map((cookie) => cookie.name)).not.toContain(
    '__Host-oauth-tx',
  );

  // 5. Inspect only the session cookie's required attributes.
  expect(await cookieAttributes(context, '__Host-session')).toEqual(
    requiredCookieAttributes(),
  );

  // 6. Validate the fixed account and retain CSRF only in page memory.
  const accountProof = await page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    const body: unknown = await response.json();
    const data = (
      body as {
        data?: {
          csrfToken?: unknown;
          identities?: Array<{ provider?: unknown }>;
          user?: {
            avatarKey?: unknown;
            email?: unknown;
            id?: unknown;
            name?: unknown;
          };
        };
      }
    ).data;
    const csrfToken = data?.csrfToken;
    if (typeof csrfToken === 'string' && csrfToken.length > 0) {
      (window as BrowserSecretState).__aboutmeUATCSRF = csrfToken;
    }
    return {
      accountMatched: data?.user?.email === 'developer@example.invalid'
        && data.user.name === 'Development User'
        && data.user.avatarKey === null
        && typeof data.user.id === 'string'
        && /^[0-9a-f-]{36}$/i.test(data.user.id),
      csrfPresent: typeof csrfToken === 'string' && csrfToken.length > 0,
      providerMatched: data?.identities?.length === 1
        && data.identities[0]?.provider === 'google',
      status: response.status,
    };
  });
  expect(accountProof).toEqual({
    accountMatched: true,
    csrfPresent: true,
    providerMatched: true,
    status: 200,
  });

  // 7. Start and complete reauthentication using that in-memory token.
  const reauthStart = await page.evaluate(async () => {
    const state = window as BrowserSecretState;
    const token = state.__aboutmeUATCSRF;
    delete state.__aboutmeUATCSRF;
    if (typeof token !== 'string') {
      return { authorizeURLAccepted: false, status: 0 };
    }
    const response = await fetch('/api/v1/auth/google/start?purpose=reauth', {
      credentials: 'include',
      headers: { 'X-CSRF-Token': token },
      method: 'POST',
    });
    const body: unknown = await response.json();
    const value = (
      body as { data?: { authorizeUrl?: unknown } }
    ).data?.authorizeUrl;
    let authorizeURLAccepted = false;
    if (typeof value === 'string') {
      const authorizeURL = new URL(value);
      authorizeURLAccepted = authorizeURL.origin === location.origin
        && authorizeURL.pathname === '/__uat/oauth/google/authorize'
        && !authorizeURL.username
        && !authorizeURL.password
        && !authorizeURL.hash;
      if (authorizeURLAccepted) {
        setTimeout(() => location.assign(authorizeURL.href), 0);
      }
    }
    return { authorizeURLAccepted, status: response.status };
  });
  expect(reauthStart).toEqual({ authorizeURLAccepted: true, status: 200 });
  await page.waitForURL((url) =>
    url.origin === ORIGIN
    && url.pathname === '/__uat/oauth/google/authorize'
  );
  expect(await cookieAttributes(context, '__Host-oauth-tx')).toEqual(
    requiredCookieAttributes(),
  );
  await expect(developmentAccount).toBeChecked();
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ORIGIN
      && url.pathname === '/app/settings/sessions'
      && url.search === ''
    ),
    page.getByRole('button', { name: 'Continue with Google' }).click(),
  ]);
  expect((await context.cookies(ORIGIN)).map((cookie) => cookie.name)).not.toContain(
    '__Host-oauth-tx',
  );

  // 8. The same privileged start without CSRF must fail closed.
  const missingToken = await page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/google/start?purpose=reauth', {
      credentials: 'include',
      method: 'POST',
    });
    const body: unknown = await response.json();
    return {
      code: (body as { error?: { code?: unknown } }).error?.code,
      status: response.status,
    };
  });
  expect(missingToken).toEqual({ code: 'csrf_rejected', status: 403 });
  expect((await context.cookies(ORIGIN)).map((cookie) => cookie.name)).not.toContain(
    '__Host-oauth-tx',
  );

  // 9. Obtain a fresh in-memory token, log out, and prove the session is dead.
  const logoutProof = await page.evaluate(async () => {
    const me = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    const meBody: unknown = await me.json();
    const token = (
      meBody as { data?: { csrfToken?: unknown } }
    ).data?.csrfToken;
    if (typeof token !== 'string' || token.length === 0) {
      return { logoutStatus: 0, meStatus: me.status };
    }
    const logout = await fetch('/api/v1/auth/logout', {
      credentials: 'include',
      headers: { 'X-CSRF-Token': token },
      method: 'POST',
    });
    const after = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    return { logoutStatus: logout.status, meStatus: after.status };
  });
  expect(logoutProof).toEqual({ logoutStatus: 204, meStatus: 401 });
  expect((await context.cookies(ORIGIN)).map((cookie) => cookie.name)).not.toContain(
    '__Host-session',
  );

  // 10. Persist only bounded verdicts after all leak and isolation checks pass.
  const { certificateErrors, consoleErrors, externalRequests, pageErrors } = counters;
  expect({ certificateErrors, consoleErrors, externalRequests, pageErrors }).toEqual({
    certificateErrors: 0,
    consoleErrors: 0,
    externalRequests: 0,
    pageErrors: 0,
  });
  const steps = Object.fromEntries(
    Array.from({ length: 10 }, (_, index) => [String(index + 1), true]),
  );
  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify({
      errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
      origin: ORIGIN,
      scenario: 'google-authentication',
      schemaVersion: 1,
      steps,
    }, null, 2)}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});
