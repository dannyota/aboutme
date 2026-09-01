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
}

// signInWithGoogle completes a provider login: it follows the login anchor to
// the same-origin authorize page, resolves the named local account, and
// returns after the callback lands on the home page. The local provider
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
  await Promise.all([
    page.waitForURL((url) => url.origin === ALLOWED_ORIGIN && url.pathname === '/'),
    activate(page.getByRole('button', { name: 'Continue with Google' })),
  ]);
}

// waitForHydration polls until the client Vue app has mounted on the given
// SSR root, the deterministic signal that Vue-bound controls are interactive.
export async function waitForHydration(
  page: Page,
  rootId = '__nuxt',
): Promise<void> {
  await expect.poll(() =>
    page.evaluate((id) =>
      Boolean(
        (document.getElementById(id) as HTMLElement & {
          __vue_app__?: unknown;
        } | null)?.__vue_app__,
      ),
    rootId),
  ).toBe(true);
}
