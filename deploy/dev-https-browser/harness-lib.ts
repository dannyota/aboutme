import {
  expect,
  type BrowserContext,
  type ConsoleMessage,
  type Locator,
  type Page,
} from '@playwright/test';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
  isExpectedNegativeHTTPConsole,
} from './network-policy';

export interface DiagnosticCounters {
  certificateErrors: number;
  consoleErrors: number;
  externalRequests: number;
  pageErrors: number;
}

export function newDiagnosticCounters(): DiagnosticCounters {
  return {
    certificateErrors: 0,
    consoleErrors: 0,
    externalRequests: 0,
    pageErrors: 0,
  };
}

export interface PageDiagnosticsHooks {
  countConsoleError?: (message: ConsoleMessage) => boolean;
  onCountedConsoleError?: (message: ConsoleMessage) => void;
  onPageError?: (error: Error) => void;
}

export function pageDiagnosticsAttacher(
  counters: DiagnosticCounters,
  hooks: PageDiagnosticsHooks = {},
): (openedPage: Page) => void {
  return (openedPage: Page): void => {
    openedPage.on('console', (message) => {
      if (
        message.type() === 'error'
        && (hooks.countConsoleError?.(message) ?? true)
      ) {
        counters.consoleErrors += 1;
        hooks.onCountedConsoleError?.(message);
      }
    });
    openedPage.on('pageerror', (error) => {
      counters.pageErrors += 1;
      hooks.onPageError?.(error);
    });
    openedPage.on('requestfailed', (request) => {
      if (/CERT/i.test(request.failure()?.errorText ?? '')) counters.certificateErrors += 1;
    });
  };
}

export function isUnexpectedConsoleError(message: ConsoleMessage): boolean {
  return !isExpectedNegativeHTTPConsole(message.text(), message.location().url);
}

export async function installExternalRequestFirewall(
  context: BrowserContext,
  counters: DiagnosticCounters,
): Promise<void> {
  await context.route('**/*', async (route) => {
    if (!isAllowedHTTPURL(route.request().url())) {
      counters.externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
}

export async function installExternalWebSocketFirewall(
  context: BrowserContext,
  counters: DiagnosticCounters,
): Promise<void> {
  await context.routeWebSocket('**/*', async (webSocket) => {
    if (!isAllowedWebSocketURL(webSocket.url())) {
      counters.externalRequests += 1;
      await webSocket.close({ code: 1008, reason: 'blocked' });
      return;
    }
    webSocket.connectToServer();
  });
}

export const DEVELOPMENT_USER_LABEL = 'Development User — developer@example.invalid';

export type SignInWithGoogleStep
  = | 'login-page-loaded'
    | 'heading-visible'
    | 'authorize-url-seen'
    | 'account-checked'
    | 'callback-reached';

export interface SignInWithGoogleOptions {
  readonly accountLabel?: string;
  readonly fromLoginPage?: boolean;
  readonly keyboard?: boolean;
  readonly returnPath?: string;
  readonly activator?: Locator;
  readonly onStep?: ((step: SignInWithGoogleStep) => void) | undefined;
}

export async function pinEnglish(context: BrowserContext): Promise<void> {
  await context.addCookies([
    { name: 'aboutme-locale', value: 'en', url: ALLOWED_ORIGIN },
  ]);
}

export async function signInWithGoogle(
  page: Page,
  options: SignInWithGoogleOptions = {},
): Promise<void> {
  const activate = (target: Locator): Promise<void> => (
    options.keyboard === true ? target.press('Enter') : target.click()
  );
  if (options.fromLoginPage === true) {
    await pinEnglish(page.context());
    const response = await page.goto('/login');
    expect(response?.status()).toBe(200);
    options.onStep?.('login-page-loaded');
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
    options.onStep?.('heading-visible');
  }
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ALLOWED_ORIGIN
      && url.pathname === '/__uat/oauth/google/authorize'
    ),
    activate(
      options.activator
        ?? page.getByRole('link', { name: 'Continue with Google' }),
    ),
  ]);
  options.onStep?.('authorize-url-seen');
  const accountLabel = options.accountLabel ?? DEVELOPMENT_USER_LABEL;
  const account = page.getByLabel(accountLabel);
  if (accountLabel === DEVELOPMENT_USER_LABEL) {
    await expect(account).toBeChecked();
  } else {
    await account.check();
  }
  options.onStep?.('account-checked');
  const returnPath = options.returnPath ?? '/app/resumes';
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ALLOWED_ORIGIN && url.pathname === returnPath
    ),
    activate(page.getByRole('button', { name: 'Continue with Google' })),
  ]);
  options.onStep?.('callback-reached');
}

export const LINKEDIN_VERIFIED_LABEL = 'LinkedIn Verified (li-verified@example.invalid)';
export const LINKEDIN_NO_EMAIL_LABEL = 'LinkedIn No Email (no email)';
export const LINKEDIN_UNVERIFIED_LABEL = 'LinkedIn Unverified (li-unverified@example.invalid)';
export const LINKEDIN_COLLISION_LABEL = 'LinkedIn Collision (li-collision@example.invalid)';
export const LINKEDIN_LINK_LABEL = 'LinkedIn Link (li-link@example.invalid)';

export interface SignInWithLinkedInOptions {
  readonly accountLabel?: string;
  readonly fromLoginPage?: boolean;
  readonly returnPath?: string;
}

export async function startLinkedInAuthorize(
  page: Page,
  activator: Locator,
  accountLabel = LINKEDIN_VERIFIED_LABEL,
): Promise<void> {
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ALLOWED_ORIGIN
      && url.pathname === '/__uat/oauth/linkedin/authorize'
    ),
    activator.click(),
  ]);
  await expect(page).toHaveTitle('Local LinkedIn sign-in');
  await expect(
    page.getByRole('heading', { name: 'Choose a local LinkedIn account' }),
  ).toBeVisible();
  const account = page.getByLabel(accountLabel);
  if (accountLabel === LINKEDIN_VERIFIED_LABEL) {
    await expect(account).toBeChecked();
  } else {
    await account.check();
  }
}

export async function signInWithLinkedIn(
  page: Page,
  options: SignInWithLinkedInOptions = {},
): Promise<void> {
  if (options.fromLoginPage === true) {
    await pinEnglish(page.context());
    const response = await page.goto('/login');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  }
  await startLinkedInAuthorize(
    page,
    page.getByRole('link', { name: 'Continue with LinkedIn' }),
    options.accountLabel,
  );
  const returnPath = options.returnPath ?? '/app/resumes';
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ALLOWED_ORIGIN && url.pathname === returnPath
    ),
    page.getByRole('button', { name: 'Allow', exact: true }).click(),
  ]);
}

export async function waitForHydration(
  page: Page,
  rootId = '__nuxt',
  timeoutMs?: number,
): Promise<void> {
  await expect.poll(() =>
    page.evaluate((id) =>
      Boolean(
        (document.getElementById(id) as HTMLElement & {
          __vue_app__?: unknown;
        } | null)?.__vue_app__,
      ),
    rootId),
  timeoutMs === undefined ? undefined : { timeout: timeoutMs }).toBe(true);
}

export const WAIT_WARM_MS = 90_000;

export async function warmPage(page: Page, url: string): Promise<void> {
  await page.goto(url, { timeout: WAIT_WARM_MS });
  await waitForHydration(page, '__nuxt', WAIT_WARM_MS);
}

export async function pageStateWords(page: Page): Promise<string> {
  try {
    const state = await page.evaluate(() => {
      const header = document.querySelector('[data-testid="app-shell"]');
      const signedIn = document.querySelector('[data-testid="account-menu"]')
        !== null;
      const signedOut = header?.querySelector(
        'a[href^="/login"], a[href^="/register"]',
      ) != null;
      return {
        header: signedIn ? 'signedin' : signedOut ? 'signedout' : 'none',
        hydrated: Boolean((document.getElementById('__nuxt') as HTMLElement & {
          __vue_app__?: unknown;
        } | null)?.__vue_app__),
        lang: document.documentElement.lang,
      };
    });
    const lang = state.lang === 'vi' || state.lang === 'en'
      ? state.lang
      : 'other';
    return `lang-${lang}-header-${state.header}`
      + `-hydrated-${state.hydrated ? 'yes' : 'no'}`;
  } catch {
    return 'lang-unread-header-unread-hydrated-unread';
  }
}

const LOCATOR_STATE_PATTERNS: ReadonlyArray<readonly [RegExp, string]> = [
  [/locator resolved to /u, 'found'],
  [/element is not visible/u, 'hidden'],
  [/element is not enabled|element is disabled/u, 'disabled'],
  [/element is not editable/u, 'readonly'],
  [/intercepts pointer events/u, 'covered'],
  [/element is not stable/u, 'unstable'],
  [/element is outside of the viewport/u, 'offscreen'],
  [/element was detached from the DOM/u, 'detached'],
];

export function locatorStateWord(message: string): string {
  if (/strict mode violation/u.test(message)) return 'el-strict';
  if (!/waiting for /u.test(message)) return 'el-none';
  let word = 'notfound';
  for (const line of message.split('\n')) {
    for (const [pattern, state] of LOCATOR_STATE_PATTERNS) {
      if (pattern.test(line)) word = state;
    }
  }
  return `el-${word}`;
}
