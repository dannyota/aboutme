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
  /** Decide whether an error console message counts (default: every one). */
  countConsoleError?: (message: ConsoleMessage) => boolean;
  /** Observe a console message immediately after it was counted. */
  onCountedConsoleError?: (message: ConsoleMessage) => void;
  /** Observe a page error immediately after it was counted. */
  onPageError?: (error: Error) => void;
}

// pageDiagnosticsAttacher returns the shared per-page listener installer: it
// counts console errors, page errors, and certificate-related request
// failures into the given counters.
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

// isUnexpectedConsoleError is the countConsoleError hook for specs that
// intentionally provoke the fixed set of negative HTTP responses.
export function isUnexpectedConsoleError(message: ConsoleMessage): boolean {
  return !isExpectedNegativeHTTPConsole(message.text(), message.location().url);
}

// installExternalRequestFirewall blocks and counts every HTTP request that
// leaves the trusted origin; allowed requests continue unmodified.
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

// installExternalWebSocketFirewall closes and counts every WebSocket that
// targets anything but the trusted origin; allowed sockets connect through.
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

export interface SignInWithGoogleOptions {
  /** Account radio label on the authorize page (default: development user). */
  readonly accountLabel?: string;
  /** Navigate to /login and assert it rendered before starting. */
  readonly fromLoginPage?: boolean;
  /** Activate the login link and authorize button by keyboard, not click. */
  readonly keyboard?: boolean;
  /** Expected same-origin callback path (default: /app/resumes). */
  readonly returnPath?: string;
}

// pinEnglish sets the site-language cookie so the homepage and account pages
// render English, the copy these proofs assert. Vietnamese is the default.
export async function pinEnglish(context: BrowserContext): Promise<void> {
  await context.addCookies([
    { name: 'aboutme-locale', value: 'en', url: ALLOWED_ORIGIN },
  ]);
}

// signInWithGoogle completes a provider login: it follows the login anchor to
// the same-origin authorize page, resolves the named local account, and
// returns after the callback lands on the expected path. The local provider
// pre-selects only the development user, so that label is proven pre-checked
// while any other account is selected explicitly.
export async function signInWithGoogle(
  page: Page,
  options: SignInWithGoogleOptions = {},
): Promise<void> {
  const activate = (target: Locator): Promise<void> => (
    options.keyboard === true ? target.press('Enter') : target.click()
  );
  if (options.fromLoginPage === true) {
    // This path asserts the English sign-in copy. Pin it here too, because an
    // account deletion clears site cookies, the language choice included.
    await pinEnglish(page.context());
    const response = await page.goto('/login');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  }
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ALLOWED_ORIGIN
      && url.pathname === '/__uat/oauth/google/authorize'
    ),
    activate(page.getByRole('link', { name: 'Continue with Google' })),
  ]);
  const accountLabel = options.accountLabel ?? DEVELOPMENT_USER_LABEL;
  const account = page.getByLabel(accountLabel);
  if (accountLabel === DEVELOPMENT_USER_LABEL) {
    await expect(account).toBeChecked();
  } else {
    await account.check();
  }
  const returnPath = options.returnPath ?? '/app/resumes';
  await Promise.all([
    page.waitForURL((url) =>
      url.origin === ALLOWED_ORIGIN && url.pathname === returnPath
    ),
    activate(page.getByRole('button', { name: 'Continue with Google' })),
  ]);
}

// The mock LinkedIn accounts (docs/design/linkedin-sign-in.md "Design"), one
// constant per fixed radio label. The verified account is first and
// pre-selected, matching the authorize page's default.
export const LINKEDIN_VERIFIED_LABEL = 'LinkedIn Verified (li-verified@example.invalid)';
export const LINKEDIN_NO_EMAIL_LABEL = 'LinkedIn No Email (no email)';
export const LINKEDIN_UNVERIFIED_LABEL = 'LinkedIn Unverified (li-unverified@example.invalid)';
export const LINKEDIN_COLLISION_LABEL = 'LinkedIn Collision (li-collision@example.invalid)';
export const LINKEDIN_LINK_LABEL = 'LinkedIn Link (li-link@example.invalid)';

export interface SignInWithLinkedInOptions {
  /** Account radio label on the authorize page (default: the verified account). */
  readonly accountLabel?: string;
  /** Navigate to /login and assert it rendered before starting. */
  readonly fromLoginPage?: boolean;
  /** Expected same-origin callback path (default: /app/resumes). */
  readonly returnPath?: string;
}

// startLinkedInAuthorize activates the login anchor or link button and
// returns once the same-origin LinkedIn authorize page has rendered, with
// the named account selected (the verified account by default, already
// checked, matching the authorize page's default selection). It leaves the
// choice of submit button ("Allow", "Cancel sign-in", or "Cancel
// authorization") to the caller, so it fits every LinkedIn proof case
// (docs/design/linkedin-sign-in.md "Tests").
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

// signInWithLinkedIn completes the happy-path LinkedIn login: it follows the
// login anchor, selects the named account (the verified account by
// default), and returns after "Allow" reaches the expected same-origin
// callback path.
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

// waitForHydration polls until the client Vue app has mounted on the given
// SSR root, the deterministic signal that Vue-bound controls are interactive.
// `timeoutMs` bounds the poll; omitted, it keeps Playwright's own default.
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

// A first visit to a page compiles its route on the harness's dev server, so
// it is far slower than any later visit to the same route in this browser
// process (second-factor.spec.ts and totp.spec.ts pay the same cost with
// their own WARM_ROUTES pass, `second-factor-pages.ts`). This bound is a
// ceiling for that one-time cost, not a typical duration, so widening it
// touches no ordinary check.
export const WAIT_WARM_MS = 90_000;

// warmPage opens `url` once at the warm bound, so its first compile happens
// here rather than during a later, tightly bounded hydration wait. It also
// fits a page whose state comes from the URL fragment or another one-time
// value, opened for the only time this call makes: there the warm bound
// applies directly to that real, singular visit instead of to a throwaway
// one before it, since replaying such a page a second time would not be
// idempotent.
export async function warmPage(page: Page, url: string): Promise<void> {
  await page.goto(url, { timeout: WAIT_WARM_MS });
  await waitForHydration(page, '__nuxt', WAIT_WARM_MS);
}

// --- Failure diagnosis in closed words ---------------------------------------
//
// The browser runner prints only a proof's last stage line, so a failed
// locator step has to name its own cause there. These helpers reduce the page
// and Playwright's call log to fixed words that match the runner's
// [a-z0-9-] filter. They never return page text, a URL, a selector, or an
// account value.

/**
 * The page's language, whether the header shows the signed-in account menu or
 * the signed-out account links, and whether the app has hydrated, as
 * `lang-<vi|en|other>-header-<signedin|signedout|none>-hydrated-<yes|no>`.
 * Read while the failed page is still open, before any teardown navigates.
 */
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

// Call-log lines Playwright writes while an action retries, mapped to one
// word each. The last match wins: it is the element's state when the action
// gave up.
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

/**
 * What the failed action's target was doing when it gave up, as
 * `el-<word>`: `notfound` when no element ever matched, `strict` when more
 * than one did, `none` when the failure was not a locator wait.
 */
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
