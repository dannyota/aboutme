/**
 * Passkey second-factor journey over the trusted HTTPS harness.
 *
 * One spec serves two browser modes. `second-factor` runs against a server
 * whose passkey enrollment flag is on and walks enrollment, pending password
 * and provider sign-in, recovery, revocation, and removal.
 * `second-factor-disabled` runs against the same stack restarted with the flag
 * off and proves enrollment answers exactly like an unregistered route while
 * verification, removal, state, and recovery routes keep working.
 *
 * Ceremonies use Chrome DevTools virtual authenticators. Exactly one simulates
 * presence at a time, so a registration whose `excludeCredentials` names an
 * existing credential still has a device that can answer, and every assertion
 * resolves against a known credential. Chrome allows one internal (platform)
 * authenticator per page, so the first is internal and every later one is a
 * USB security key.
 *
 * The proof spends three fictional accounts because every pending route shares
 * one bounded budget of ten attempts per account and client address in fifteen
 * minutes (docs/design/budgets.md). Attempt exhaustion alone costs five, so it
 * gets an account of its own.
 *
 * Recovery plaintext, credential material, cookies and CSRF values stay in
 * process memory. The evidence file holds only booleans, the fixed scenario
 * name, and the origin. Accounts carry a per-run marker and are deleted in a
 * `finally` path on success and failure alike.
 *
 * Contract: docs/design/passkey-second-factor-contract.md and
 * docs/design/second-factor-authentication.md.
 */
import {
  expect,
  test,
  type BrowserContext,
  type ConsoleMessage,
  type Page,
  type Route,
} from '@playwright/test';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { request as httpsRequest } from 'node:https';
import { freshCSRF } from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  type DiagnosticCounters,
  signInWithGoogle,
} from './harness-lib';
import {
  ALLOWED_ORIGIN,
  httpFailureStatus,
  isExpectedNegativeHTTPConsole,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const MODE = process.env.ABOUTME_BROWSER_MODE ?? 'second-factor';
const CA_PATH = '/uat-input/caddy-root.crt';
const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
const CLIENT_NAME_PATH = '/uat-input/mcp-client-name';
const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
const ENABLED_EVIDENCE_PATH = '/evidence/passkey-second-factor-proof.json';
const DISABLED_EVIDENCE_PATH
  = '/evidence/passkey-enrollment-disabled-proof.json';
const REDIRECT_URI = 'http://127.0.0.1:20090/callback';
const LINK_ACCOUNT_LABEL = 'Bob Local — bob@example.invalid';
const DISABLED_ACCOUNT_LABEL = 'Development User — developer@example.invalid';
const PENDING_COOKIE = '__Host-auth-pending';
const RECOVERY_INPUT = '#second-factor-recovery-code';
const PHONE = { height: 844, width: 390 };
const DESKTOP = { height: 900, width: 1440 };
const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

// Explicit bounds for the waits Playwright's action and navigation options do
// not cover: response and event waits, DevTools commands, loopback requests
// from Node, and this spec's own polling. Every one is far above the slowest
// healthy hosted step, so only a genuinely stuck wait trips it, and a trip
// fails at its own stage instead of consuming the whole test budget.
const WAIT_RESPONSE_MS = 30_000;
const WAIT_NAVIGATION_MS = 60_000;
// The first visit to an app route pulls that route's whole module graph from
// the harness's Vite dev server, transformed on demand and replayed through
// this proof's request interception, so it is far slower than any later
// navigation. Three minutes is a ceiling, not a cost: only the first landing
// approaches it, and it stays well inside the 900-second test budget.
const WAIT_LANDING_MS = 180_000;
// A first visit to a page compiles its route on the harness's dev server and
// pulls its module graph through this proof's request interception, so it is
// far slower than any later visit. The journey pays that once up front, under
// the warm bound; every visit after it uses the tight hydration bound, which
// is what makes a hydration failure inside the journey mean something.
const WAIT_WARM_MS = 90_000;
const WAIT_HYDRATE_MS = 30_000;
const WAIT_CDP_MS = 30_000;
const WAIT_LOOPBACK_MS = 20_000;
const WAIT_MAIL_MS = 45_000;

// Every negative status this proof deliberately provokes inside the page,
// keyed by the exact path that may answer with it. Anything else counts as a
// console error and fails the run.
const EXPECTED_PAGE_FAILURES: ReadonlyMap<string, readonly number[]> = new Map([
  ['/api/v1/me', [401]],
  ['/api/v1/me/second-factor', [401]],
  ['/api/v1/me/second-factor/passkeys', [404]],
  ['/api/v1/me/second-factor/passkeys/options', [403, 404]],
  ['/api/v1/me/second-factor/recovery-codes', [403]],
  ['/api/v1/me/second-factor/totp/enrollment', [404]],
  ['/api/v1/auth/second-factor', [401]],
  ['/api/v1/auth/second-factor/passkey/options', [400, 401]],
  ['/api/v1/auth/second-factor/passkey/verify', [400, 401]],
  ['/api/v1/auth/second-factor/recovery/verify', [401]],
  ['/api/v1/auth/password/login', [401]],
  ['/api/v1/auth/password/reauth', [401]],
]);
const REMOVAL_PATH = /^\/api\/v1\/me\/second-factor\/passkeys\/[^/]+$/u;

// --- Failure reporting ------------------------------------------------------
//
// The runner prints only the last stage line it finds and withholds every
// other byte of browser output, so that one line has to carry the whole
// diagnosis. It is assembled from three closed vocabularies and never from an
// exception message, a URL, a response body, or an account value. Teardown
// stops updating the recorded stage, so a failure keeps the stage it happened
// in instead of being relabelled as cleanup.

type FailureOutcome
  = | 'timeout'
    | 'navigation'
    | 'response'
    | 'locator'
    | 'ceremony'
    | 'capture'
    | 'assertion'
    | 'unknown';

type AccountRole = 'none' | 'primary' | 'recovery' | 'attempts' | 'disabled';

let recordedStage = 'start';
let recordedRole: AccountRole = 'none';
let tearingDown = false;

function stage(name: string): void {
  if (tearingDown) return;
  recordedStage = name;
  console.log(`${MODE}-stage:${name}`);
}

function role(next: AccountRole): void {
  if (!tearingDown) recordedRole = next;
}

/**
 * Marks the point after which stage and role stop being recorded, and names
 * the stage the body stopped at. The runner forwards only the last stage line,
 * so a bare teardown line would hide exactly what a reader needs.
 */
function beginTeardown(): void {
  tearingDown = true;
  console.log(`${MODE}-stage:cleanup-after-${recordedStage}`);
}

// Ordered classifiers. Each maps a Playwright or helper failure to one fixed
// word; the matched text itself is never printed.
const OUTCOME_PATTERNS: ReadonlyArray<readonly [RegExp, FailureOutcome]> = [
  [/^Test timeout of \d+ms exceeded/u, 'timeout'],
  [/waitForURL|waiting for navigation/u, 'navigation'],
  [/waitForResponse|waitForEvent/u, 'response'],
  [
    /credentials\.(get|create)|WebAuthn|NotAllowedError|InvalidStateError|virtual authenticator/u,
    'ceremony',
  ],
  [/capture (read|reset) failed|within its capture bound/u, 'capture'],
  // An action timeout arrives as `TimeoutError: locator.fill: ...`.
  [/^(?:TimeoutError: )?(locator|page|frame|elementHandle)\./u, 'locator'],
  [/expect|Timed out \d+ms waiting for/u, 'assertion'],
];

function outcomeOf(status: string, message: string): FailureOutcome {
  if (status === 'timedOut') return 'timeout';
  for (const [pattern, outcome] of OUTCOME_PATTERNS) {
    if (pattern.test(message)) return outcome;
  }
  return 'unknown';
}

test.afterEach(({}, testInfo) => {
  // One spec serves both modes, so the other mode's test is always skipped.
  // Only a real failure may add a line.
  if (testInfo.status === 'skipped') return;
  if (testInfo.status === testInfo.expectedStatus) return;
  const outcome = outcomeOf(
    testInfo.status ?? 'unknown',
    testInfo.error?.message ?? '',
  );
  console.log(
    `${MODE}-stage:fail-${outcome}-at-${recordedStage}-for-${recordedRole}`,
  );
});

/**
 * Names where a provider round trip landed, from a closed set. The harness
 * redirects a failed link or login back to the settings or login page with an
 * `error` code, and waiting for the clean URL alone would hang on a page that
 * has already finished loading. Recording the category turns that into a
 * named stage.
 */
function callbackCategory(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return 'callback-unparsed';
  }
  if (url.origin !== ORIGIN) return 'callback-foreign-origin';
  if (url.pathname === '/login/second-factor') return 'callback-second-factor';
  if (url.pathname === '/login') return 'callback-login';
  if (url.pathname !== '/app/settings/sessions') return 'callback-other-path';
  const code = url.searchParams.get('error');
  if (code === null) return 'callback-settings-clean';
  switch (code) {
    case 'auth_failed': return 'callback-settings-auth-failed';
    case 'authentication_required':
      return 'callback-settings-authentication-required';
    case 'cancelled': return 'callback-settings-cancelled';
    case 'email_already_registered':
      return 'callback-settings-email-already-registered';
    case 'email_not_verified':
      return 'callback-settings-email-not-verified';
    case 'identity_already_linked':
      return 'callback-settings-identity-already-linked';
    case 'reauth_required': return 'callback-settings-reauth-required';
    default: return 'callback-settings-unrecognized';
  }
}

/** Every landing that means the browser holds a signed-in app session. */
const APP_LANDINGS: readonly string[] = [
  'landing-app-new',
  'landing-app-other',
  'landing-app-resume',
  'landing-app-resumes',
  'landing-app-settings',
];

/**
 * Names where a sign-in or a pending completion landed, from a closed set.
 * The proof asserts the class of destination it depends on, not one exact
 * path, so an app route this spec did not predict is named rather than
 * waited on until the budget runs out.
 */
function landingCategory(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return 'landing-unparsed';
  }
  if (url.origin !== ORIGIN) return 'landing-foreign-origin';
  const path = url.pathname;
  if (path === '/login') return 'landing-login';
  if (path === '/login/second-factor') return 'landing-second-factor';
  if (path === '/app/resumes') return 'landing-app-resumes';
  if (path.startsWith('/app/resumes/')) return 'landing-app-resume';
  if (path === '/app/new') return 'landing-app-new';
  if (path === '/app/settings/sessions') return 'landing-app-settings';
  if (path.startsWith('/app/')) return 'landing-app-other';
  if (path === '/') return 'landing-home';
  return 'landing-other-path';
}

/**
 * Whether the sign-in form is still submitting, as one closed word. A form
 * that is still busy means its navigation started and the destination has not
 * settled; an idle form means the submit finished without one. Read from the
 * control's disabled state, never from its text.
 */
async function submitState(page: Page): Promise<string> {
  const submit = page
    .getByTestId('login-form')
    .locator('button[type="submit"]');
  if (await submit.count() === 0) return 'submit-absent';
  return await submit.isDisabled() ? 'submit-busy' : 'submit-idle';
}

/**
 * Waits for the page to leave `from`, then records and returns the closed
 * name for where it stopped. A page that never leaves is named too, a login
 * page is split by whether it is showing an error, and a wait that gave up
 * also records whether the form is still submitting. That one forwarded line
 * then says which side is wrong without carrying any page text.
 */
async function landedAfter(page: Page, from: string): Promise<string> {
  let settled = true;
  try {
    await page.waitForURL(
      (url) => url.origin === ORIGIN && url.pathname !== from,
      { timeout: WAIT_LANDING_MS },
    );
  } catch {
    settled = false;
  }
  let where = landingCategory(page.url());
  if (where === 'landing-login') {
    where = await page.getByTestId('login-form-error').count() > 0
      ? 'landing-login-error'
      : 'landing-login-idle';
  }
  stage(settled ? where : `${where}-${await submitState(page)}`);
  return where;
}

/** Asserts exactly where the page landed, naming it as a stage first. */
function expectLanding(page: Page, want: string): void {
  // landedAfter already recorded this landing, with its discriminator when
  // its wait gave up; recording the plain name again would drop that.
  expect(landingCategory(page.url())).toBe(want);
}

/** Asserts the browser holds a signed-in app session, wherever it landed. */
async function expectSignedInApp(page: Page): Promise<void> {
  expect(APP_LANDINGS).toContain(landingCategory(page.url()));
  expect(await meStatus(page)).toBe(200);
}

/**
 * Runs a provider round trip while recording where each main-frame navigation
 * landed. A failure is re-stamped with that closed category, so the withheld
 * log still names the exact callback outcome.
 */
async function watchingCallback(
  page: Page,
  body: () => Promise<void>,
): Promise<void> {
  let landed = 'callback-none';
  const record = (frame: { url(): string; parentFrame(): unknown }): void => {
    if (frame.parentFrame() === null) landed = callbackCategory(frame.url());
  };
  page.on('framenavigated', record);
  try {
    await body();
  } catch (error) {
    if (!tearingDown) recordedStage = landed;
    throw error;
  } finally {
    page.off('framenavigated', record);
  }
}

function isUnexpectedSecondFactorConsole(message: ConsoleMessage): boolean {
  const text = message.text();
  const location = message.location().url;
  if (isExpectedNegativeHTTPConsole(text, location)) return false;
  const status = httpFailureStatus(text);
  if (status === null) return true;
  let url: URL;
  try {
    url = new URL(location);
  } catch {
    return true;
  }
  if (url.origin !== ORIGIN) return true;
  const allowed = EXPECTED_PAGE_FAILURES.get(url.pathname)
    ?? (REMOVAL_PATH.test(url.pathname) ? [404] : undefined);
  return allowed === undefined || !allowed.includes(status);
}

// --- Fictional run identity -----------------------------------------------

const RUN_MARKER = randomBytes(6).toString('hex');
let accountSequence = 0;

/** A fictional, per-run address in the reserved invalid top-level domain. */
function runEmail(role: string): string {
  accountSequence += 1;
  return `pk-${role}-${RUN_MARKER}-${accountSequence}@example.invalid`;
}

/** A runtime-random password inside the accepted length policy. */
function runPassword(): string {
  return randomBytes(24).toString('base64url');
}

function canonicalRecovery(value: string): string {
  return value.replace(/[- ]/gu, '').toUpperCase();
}

/** A canonical recovery-code shape that is none of the issued codes. */
function fabricatedRecoveryCode(issued: readonly string[]): string {
  for (;;) {
    let body = '';
    for (const byte of randomBytes(26)) body += CROCKFORD[byte % 32];
    const code = `amr_${body}`;
    const wanted = canonicalRecovery(code);
    if (!issued.some((value) => canonicalRecovery(value) === wanted)) {
      return code;
    }
  }
}

// --- Trusted loopback requests --------------------------------------------

interface TrustedResult {
  readonly status: number;
  readonly code: string | null;
}

/**
 * One bounded HTTPS request to the harness origin, made from Node with the
 * exported Caddy root. Used only for states the page cannot produce: a
 * foreign Origin, a missing bound session cookie, a replayed ceremony, and
 * two genuinely concurrent completions.
 */
function trustedPost(
  ca: Buffer,
  path: string,
  headers: Readonly<Record<string, string>>,
  body: string,
): Promise<TrustedResult> {
  if (!path.startsWith('/') || path.startsWith('//')) {
    return Promise.reject(new Error('trusted request path rejected'));
  }
  return new Promise((resolve, reject) => {
    const request = httpsRequest(
      {
        ca,
        headers,
        hostname: 'localhost',
        method: 'POST',
        path,
        port: 20443,
        protocol: 'https:',
        timeout: WAIT_LOOPBACK_MS,
      },
      (response) => {
        const chunks: Buffer[] = [];
        let size = 0;
        response.on('data', (chunk: Buffer) => {
          size += chunk.length;
          if (size > 64 * 1024) {
            request.destroy(new Error('trusted response exceeded bound'));
            return;
          }
          chunks.push(chunk);
        });
        response.on('end', () => {
          resolve({
            code: errorCode(Buffer.concat(chunks).toString('utf8')),
            status: response.statusCode ?? 0,
          });
        });
      },
    );
    request.on('timeout', () => {
      request.destroy(new Error('trusted request exceeded its loopback bound'));
    });
    request.on('error', reject);
    if (body !== '') request.write(body);
    request.end();
  });
}

/** The `error.code` of a JSON envelope, or null for any other body. */
function errorCode(body: string): string | null {
  try {
    const parsed = JSON.parse(body) as { error?: { code?: unknown } };
    return typeof parsed.error?.code === 'string' ? parsed.error.code : null;
  } catch {
    return null;
  }
}

async function cookieValue(
  context: BrowserContext,
  name: string,
): Promise<string | null> {
  const jar = await context.cookies(ORIGIN);
  return jar.find((entry) => entry.name === name)?.value ?? null;
}

/** The pending cookie header for a Node request, or a fixed failure. */
async function pendingCookieHeader(context: BrowserContext): Promise<string> {
  const token = await cookieValue(context, PENDING_COOKIE);
  if (token === null) throw new Error('no pending cookie to carry');
  return `${PENDING_COOKIE}=${token}`;
}

// --- Captured security mail ------------------------------------------------

interface CapturedMessage {
  kind: string;
  to: string;
  text_body: string;
}

interface CaptureClient {
  reset(): Promise<void>;
  waitForToken(kind: string, to: string): Promise<string>;
}

function captureClient(token: string): CaptureClient {
  const headers = { Authorization: `Bearer ${token}` };
  const read = async (): Promise<CapturedMessage[]> => {
    const response = await fetch(CAPTURE_URL, {
      headers,
      signal: AbortSignal.timeout(WAIT_LOOPBACK_MS),
    });
    if (!response.ok) throw new Error(`capture read failed: ${response.status}`);
    const body = (await response.json()) as { messages: CapturedMessage[] };
    return body.messages;
  };
  return {
    async reset() {
      const response = await fetch(CAPTURE_URL, {
        headers,
        method: 'DELETE',
        signal: AbortSignal.timeout(WAIT_LOOPBACK_MS),
      });
      if (!response.ok) {
        throw new Error(`capture reset failed: ${response.status}`);
      }
    },
    async waitForToken(kind, to) {
      const deadline = Date.now() + WAIT_MAIL_MS;
      while (Date.now() < deadline) {
        for (const message of await read()) {
          if (message.kind !== kind || message.to !== to) continue;
          const found = message.text_body.match(/#token=([A-Za-z0-9_-]+)/u);
          if (found?.[1] !== undefined) return found[1];
        }
        await new Promise((resolve) => setTimeout(resolve, 250));
      }
      throw new Error('no security message within its capture bound');
    },
  };
}

// --- Virtual authenticators -------------------------------------------------

type CDPSend = (
  method: string,
  params?: Record<string, unknown>,
) => Promise<Record<string, unknown>>;

/**
 * Bounds one DevTools round trip. Nothing in Playwright's action or
 * navigation options covers DevTools, so an unanswered command would stall the
 * whole test. The rejection carries fixed words only.
 */
async function boundedCDPCall<T>(work: Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      work,
      new Promise<never>((_, reject) => {
        timer = setTimeout(
          () => reject(new Error(
            'virtual authenticator command exceeded its bound',
          )),
          WAIT_CDP_MS,
        );
      }),
    ]);
  } finally {
    if (timer !== undefined) clearTimeout(timer);
  }
}

function boundedCDP(send: CDPSend): CDPSend {
  return (method, params) => boundedCDPCall(send(method, params));
}

/** A pool of virtual authenticators on one page, one present at a time. */
class AuthenticatorPool {
  private readonly ids: string[] = [];

  private constructor(private readonly send: CDPSend) {}

  /** The transport for the authenticator added after `existing` others. */
  static transportFor(existing: number): 'internal' | 'usb' {
    return existing === 0 ? 'internal' : 'usb';
  }

  static async attach(
    context: BrowserContext,
    page: Page,
  ): Promise<AuthenticatorPool> {
    const session = await boundedCDPCall(context.newCDPSession(page));
    const send = boundedCDP(
      session.send.bind(session) as unknown as CDPSend,
    );
    await send('WebAuthn.enable', { enableUI: false });
    return new AuthenticatorPool(send);
  }

  /**
   * Adds an authenticator and makes it the only one that can answer. Chrome
   * rejects a second internal authenticator in one environment, so only the
   * first is internal and the rest are resident-key USB security keys.
   */
  async add(): Promise<string> {
    const result = await this.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        automaticPresenceSimulation: true,
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        protocol: 'ctap2',
        transport: AuthenticatorPool.transportFor(this.ids.length),
      },
    });
    const id = result.authenticatorId;
    if (typeof id !== 'string' || id === '') {
      throw new Error('virtual authenticator was not created');
    }
    this.ids.push(id);
    await this.present(id);
    return id;
  }

  async present(id: string): Promise<void> {
    for (const other of this.ids) {
      await this.send('WebAuthn.setAutomaticPresenceSimulation', {
        authenticatorId: other,
        enabled: other === id,
      });
    }
  }

  async setUserVerified(id: string, verified: boolean): Promise<void> {
    await this.send('WebAuthn.setUserVerified', {
      authenticatorId: id,
      isUserVerified: verified,
    });
  }

  async credentialCount(id: string): Promise<number> {
    const result = await this.send('WebAuthn.getCredentials', {
      authenticatorId: id,
    });
    return Array.isArray(result.credentials) ? result.credentials.length : 0;
  }
}

// --- Page helpers -----------------------------------------------------------

async function setLocale(
  context: BrowserContext,
  locale: 'en' | 'vi',
): Promise<void> {
  await context.addCookies([
    { name: 'aboutme-locale', url: ORIGIN, value: locale },
  ]);
}

/**
 * Pages the journey opens that are safe to warm with no session, each with
 * the closed word that names it. A first visit is compiled on demand by the
 * harness's dev server, so paying them once, up front and in a stage of their
 * own, keeps a later failure about the behaviour under test rather than about
 * a cold route. None of these issues a request while signed out beyond the
 * reads this proof's console filter already accepts.
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
const WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/register', 'register'],
  ['/login', 'login'],
  ['/login/second-factor', 'login-second-factor'],
  ['/forgot-password', 'forgot-password'],
];

/**
 * Pages that require a session. Warming these signed out would make the
 * account reads behind them answer 401, which is correct behaviour but noise
 * this proof would then have to accept everywhere, so they are warmed once
 * the journey has a session instead. The one landing that reaches an app
 * page before that runs under the landing bound.
 */
const SIGNED_IN_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/app/resumes', 'app-resumes'],
  ['/app/settings/sessions', 'app-settings-sessions'],
];

/** The signed-out page the disabled-enrollment journey opens first. */
const DISABLED_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/login', 'login'],
];

/** The counter classes a warm visit may move, as closed words. */
type CounterClass = 'certificate' | 'console' | 'external' | 'page';

/** The first counter class that moved, or null when the visit was clean. */
function dirtiedCounter(
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
async function warmRoutes(
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
  await page.goto(path);
  await hydrated(page, WAIT_HYDRATE_MS);
}

/** Opens a page the warm pass skips, at first-visit cost. */
async function gotoFirstVisit(page: Page, url: string): Promise<void> {
  await page.goto(url, { timeout: WAIT_WARM_MS });
  await hydrated(page, WAIT_WARM_MS);
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

interface FactorState {
  readonly status: number;
  readonly enabled: boolean;
  readonly passkeyCount: number;
  readonly recoveryRemaining: number;
}

async function factorState(page: Page): Promise<FactorState> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/me/second-factor', {
      cache: 'no-store',
      credentials: 'include',
    });
    if (response.status !== 200) {
      return {
        enabled: false,
        passkeyCount: -1,
        recoveryRemaining: -1,
        status: response.status,
      };
    }
    const body = (await response.json()) as {
      data?: {
        enabled?: unknown;
        passkeys?: unknown;
        recoveryCodesRemaining?: unknown;
      };
    };
    return {
      enabled: body.data?.enabled === true,
      passkeyCount: Array.isArray(body.data?.passkeys)
        ? body.data.passkeys.length
        : -1,
      recoveryRemaining: typeof body.data?.recoveryCodesRemaining === 'number'
        ? body.data.recoveryCodesRemaining
        : -1,
      status: response.status,
    };
  });
}

/** Registers a fictional account and verifies it through the captured mail. */
async function registerVerified(
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
async function passwordSignIn(
  page: Page,
  email: string,
  password: string,
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
  const where = await landedAfter(page, '/login');
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
 * step left. The logout response sends `Clear-Site-Data: "cookies"`, which
 * also drops the locale cookie, so the next page would render in the
 * Vietnamese default. English is pinned again afterwards; a caller that wants
 * another locale sets it after this returns.
 */
async function signOut(page: Page): Promise<void> {
  await setLocale(page.context(), 'en');
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(`${ORIGIN}/login`, { timeout: WAIT_NAVIGATION_MS });
  await setLocale(page.context(), 'en');
}

/** Runs the pending passkey ceremony with the present virtual authenticator. */
async function clickPasskey(page: Page): Promise<void> {
  await expect(page.getByTestId('second-factor-passkey')).toBeVisible();
  await page.getByTestId('second-factor-passkey-button').click();
}

/** Completes the open pending authentication with a passkey. */
async function completeWithPasskey(page: Page): Promise<void> {
  await clickPasskey(page);
  await landedAfter(page, '/login/second-factor');
}

/** Submits one recovery code by keyboard and returns the response status. */
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

/** Completes the open pending authentication with one recovery code. */
async function completeWithRecovery(page: Page, code: string): Promise<void> {
  expect(await submitRecoveryCode(page, code)).toBe(204);
  await landedAfter(page, '/login/second-factor');
}

/** Answers the settings reauthentication prompt with the account password. */
async function reauthenticateWithPassword(
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
async function readRevealedCodes(page: Page): Promise<string[]> {
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
async function closeRevealAndProveCleared(
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

/**
 * Adds one passkey from the settings page. `expectCodes` states whether this
 * enrollment creates the factor policy and therefore the one-time recovery
 * set; the returned codes are read from the reveal dialog itself.
 */
async function addPasskey(
  page: Page,
  expectCodes: boolean,
): Promise<string[]> {
  const created = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/me/second-factor/passkeys'
      && candidate.request().method() === 'POST';
  }, { timeout: WAIT_RESPONSE_MS });
  await page.getByTestId('passkey-add').click();
  expect((await created).status()).toBe(201);
  if (!expectCodes) {
    await expect(page.getByTestId('passkey-added-success')).toBeVisible();
    await expect(page.getByTestId('recovery-codes-list')).toHaveCount(0);
    return [];
  }
  const codes = await readRevealedCodes(page);
  await closeRevealAndProveCleared(page, codes);
  return codes;
}

/** Enrolls a first passkey on a freshly signed-in account. */
async function enrollFirstPasskey(
  page: Page,
  password: string,
): Promise<string[]> {
  await gotoHydrated(page, '/app/settings/sessions');
  await expect(page.getByTestId('second-factor-empty')).toBeVisible();
  await forceReauthOnce(page, '/api/v1/me/second-factor/passkeys/options');
  await page.getByTestId('passkey-add').click();
  await expect(page.getByTestId('second-factor-reauth-password')).toBeVisible();
  await reauthenticateWithPassword(page, password);
  return addPasskey(page, true);
}

// --- Enabled-enrollment journey --------------------------------------------

test('proves the passkey second factor over native HTTPS', async ({
  browser,
  context,
  page,
}) => {
  test.skip(MODE !== 'second-factor', 'enabled enrollment mode only');

  const steps = {
    agentGranted: false,
    agentRevoked: false,
    attemptsExhausted: false,
    ceremonyReplayRejected: false,
    cleanup: false,
    concurrentCompletion: false,
    enrolled: false,
    finalRemoved: false,
    locales: false,
    oneRemoved: false,
    otherSessionRevoked: false,
    otherSessionStarted: false,
    passkeyCompletion: false,
    passwordPending: false,
    providerAccount: false,
    providerPending: false,
    reauthPending: false,
    reauthRequired: false,
    recoveryCompletion: false,
    recoveryRegenerated: false,
    recoveryRevealedOnce: false,
    recoveryReuseRejected: false,
    resetPreservesEnforcement: false,
    secondPasskeyAdded: false,
    staleEpochRejected: false,
    userVerificationRequired: false,
    viewports: false,
    wrongBindingRejected: false,
    wrongOriginRejected: false,
  };

  const counters = newDiagnosticCounters();
  const attach = pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedSecondFactorConsole,
  });
  attach(page);
  context.on('page', attach);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  await setLocale(context, 'en');
  await page.setViewportSize(DESKTOP);

  await warmRoutes(page, WARM_ROUTES, counters);

  stage('capture-reset');
  const ca = await readFile(CA_PATH);
  const capture = captureClient(
    (await readFile(CAPTURE_TOKEN_PATH, 'utf8')).trim(),
  );
  await capture.reset();

  stage('virtual-authenticator');
  const pool = await AuthenticatorPool.attach(context, page);
  const authPrimary = await pool.add();

  const primaryEmail = runEmail('primary');
  const primaryPassword = runPassword();
  const recoveryEmail = runEmail('recovery');
  const recoveryPassword = runPassword();
  const attemptsEmail = runEmail('attempts');
  const attemptsPassword = runPassword();

  let authRecovery = '';
  let authAttempts = '';
  let primaryFinalPassword = primaryPassword;
  let primaryCleanupCode = '';
  let recoveryCleanupCode = '';
  let attemptsCleanupCode = '';
  const extraContexts: BrowserContext[] = [];

  try {
    // 1. A fictional primary account with a password and a linked provider.
    role('primary');
    stage('primary-register');
    await registerVerified(
      page,
      capture,
      primaryEmail,
      primaryPassword,
      'Passkey Proof',
    );
    stage('primary-first-sign-in');
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('session');
    await warmRoutes(page, SIGNED_IN_WARM_ROUTES, counters);

    stage('primary-open-settings');
    await gotoHydrated(page, '/app/settings/sessions');
    stage('primary-open-provider-list');
    await page.getByTestId('add-provider-button').click();
    const linkGoogle = page.getByRole('button', {
      exact: true,
      name: 'Link Google',
    });
    await expect(linkGoogle).toBeVisible();
    stage('primary-link-authorize');
    await Promise.all([
      page.waitForURL(
        (url) => url.origin === ORIGIN
          && url.pathname === '/__uat/oauth/google/authorize',
        { timeout: WAIT_NAVIGATION_MS },
      ),
      linkGoogle.click(),
    ]);
    stage('primary-link-select-account');
    await page.getByLabel(LINK_ACCOUNT_LABEL).check();
    stage('primary-link-callback');
    // Wait for the settings page itself, then judge the outcome from the
    // closed callback vocabulary. Waiting for the clean URL alone would hang
    // on an error redirect that has already finished loading.
    await watchingCallback(page, async () => {
      await Promise.all([
        page.waitForURL(
          (url) => url.origin === ORIGIN
            && url.pathname === '/app/settings/sessions',
          { timeout: WAIT_NAVIGATION_MS },
        ),
        page.getByRole('button', { name: 'Continue with Google' }).click(),
      ]);
      expect(callbackCategory(page.url())).toBe('callback-settings-clean');
    });
    await hydrated(page, WAIT_HYDRATE_MS);
    await expect(page.getByTestId('linked-provider-google')).toBeVisible();
    steps.providerAccount = true;

    // 2. A connected agent and a second browser session, both created while
    //    the account is still unenrolled.
    stage('agent-grant');
    const agentToken = await createAgentGrant(context, page);
    expect(await agentToolsStatus(page, agentToken)).toBe(200);
    steps.agentGranted = true;

    stage('other-session');
    const otherContext = await browser.newContext();
    extraContexts.push(otherContext);
    await setLocale(otherContext, 'en');
    await installExternalRequestFirewall(otherContext, counters);
    const otherPage = await otherContext.newPage();
    attach(otherPage);
    expect(await passwordSignIn(otherPage, primaryEmail, primaryPassword))
      .toBe('session');
    expect(await meStatus(otherPage)).toBe(200);
    steps.otherSessionStarted = true;

    // 3. Enrollment demands recent reauthentication, then issues the one-time
    //    recovery set exactly once.
    stage('enroll-first-passkey');
    await gotoHydrated(page, '/app/settings/sessions');
    await expect(page.getByTestId('second-factor-empty')).toBeVisible();
    await forceReauthOnce(page, '/api/v1/me/second-factor/passkeys/options');
    await page.getByTestId('passkey-add').click();
    await expect(page.getByTestId('second-factor-reauth-password'))
      .toBeVisible();
    steps.reauthRequired = true;
    await reauthenticateWithPassword(page, primaryPassword);
    const firstCodes = await addPasskey(page, true);
    steps.enrolled = true;
    steps.recoveryRevealedOnce = true;
    expect(await pool.credentialCount(authPrimary)).toBe(1);

    // 4. The epoch change ended every other session and the agent grant.
    stage('revocation');
    expect(await meStatus(otherPage)).toBe(401);
    steps.otherSessionRevoked = true;
    expect(await agentToolsStatus(page, agentToken)).toBe(401);
    steps.agentRevoked = true;
    expect(await meStatus(page)).toBe(200);
    await otherContext.close();

    // 5. A second passkey on a second device adds no recovery codes.
    stage('second-passkey');
    const authSecondary = await pool.add();
    await gotoHydrated(page, '/app/settings/sessions');
    expect(await addPasskey(page, false)).toEqual([]);
    expect(await pool.credentialCount(authSecondary)).toBe(1);
    await pool.present(authPrimary);
    let state = await factorState(page);
    expect(state.enabled).toBe(true);
    expect(state.passkeyCount).toBe(2);
    expect(state.recoveryRemaining).toBe(10);
    steps.secondPasskeyAdded = true;

    // 6. Regeneration replaces the set and moves the epoch, which kills a
    //    pending login another browser is still holding.
    stage('stale-epoch');
    const staleContext = await browser.newContext();
    extraContexts.push(staleContext);
    await setLocale(staleContext, 'en');
    await installExternalRequestFirewall(staleContext, counters);
    const stalePage = await staleContext.newPage();
    attach(stalePage);
    expect(await passwordSignIn(stalePage, primaryEmail, primaryPassword))
      .toBe('pending');
    await gotoHydrated(page, '/app/settings/sessions');
    await page.getByTestId('recovery-regenerate').click();
    await page.locator('[data-action="recovery-regenerate-confirm"]').click();
    const primaryCodes = await readRevealedCodes(page);
    expect(canonicalRecovery(primaryCodes.join()))
      .not.toBe(canonicalRecovery(firstCodes.join()));
    await closeRevealAndProveCleared(page, primaryCodes);
    steps.recoveryRegenerated = true;
    primaryCleanupCode = primaryCodes[9] as string;
    await stalePage.reload({ timeout: WAIT_NAVIGATION_MS });
    await hydrated(stalePage, WAIT_HYDRATE_MS);
    await expect(stalePage.getByTestId('second-factor-expired')).toBeVisible();
    steps.staleEpochRejected = true;
    await staleContext.close();

    // 7. Removing one of two passkeys keeps enforcement on. The list is in
    //    creation order, so the newer credential goes and the one the present
    //    authenticator holds stays.
    stage('remove-one');
    await gotoHydrated(page, '/app/settings/sessions');
    await page.locator('[data-testid^="passkey-remove-"]').last().click();
    await expect(page.getByRole('alertdialog')).toContainText(
      'Every other device and connected agent will be signed out.',
    );
    await page.locator('[data-action="passkey-remove-confirm"]').click();
    await expect(page.getByTestId('passkey-removed-success')).toBeVisible();
    state = await factorState(page);
    expect(state.enabled).toBe(true);
    expect(state.passkeyCount).toBe(1);
    steps.oneRemoved = true;

    // 8. Password sign-in stops at the pending page; only a verified passkey
    //    finishes it.
    stage('password-pending');
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('pending');
    expect(await meStatus(page)).toBe(401);
    await expect(page.getByTestId('second-factor-passkey')).toBeVisible();
    await expect(page.getByTestId('second-factor-recovery')).toBeVisible();
    steps.passwordPending = true;

    stage('pending-wrong-origin');
    const foreignOrigin = await trustedPost(
      ca,
      '/api/v1/auth/second-factor/passkey/options',
      {
        'Content-Type': 'application/json',
        'Cookie': await pendingCookieHeader(context),
        'Origin': 'https://passkey-proof.invalid',
        'X-CSRF-Token': await pendingCSRFToken(page),
      },
      '{}',
    );
    expect(foreignOrigin).toEqual({ code: 'csrf_rejected', status: 403 });
    steps.wrongOriginRejected = true;

    stage('pending-user-verification');
    await pool.setUserVerified(authPrimary, false);
    await clickPasskey(page);
    // Bounded on purpose: an authenticator that can never verify must fail the
    // ceremony, not hold the page until the five-minute WebAuthn timeout.
    await expect(page.getByTestId('second-factor-passkey-error'))
      .toBeVisible({ timeout: 30_000 });
    expect(new URL(page.url()).pathname).toBe('/login/second-factor');
    expect(await meStatus(page)).toBe(401);
    await pool.setUserVerified(authPrimary, true);
    // A rejected ceremony must not stay outstanding: a second concurrent
    // request would be refused by the browser, not by the server. Reloading
    // discards it before the completion that follows.
    await page.reload({ timeout: WAIT_NAVIGATION_MS });
    await hydrated(page, WAIT_HYDRATE_MS);
    steps.userVerificationRequired = true;

    stage('pending-passkey-completion');
    await completeWithPasskey(page);
    await expectSignedInApp(page);
    steps.passkeyCompletion = true;

    // 9. A settings reauthentication opens a pending row bound to this exact
    //    session, and the binding is enforced.
    stage('reauth-pending');
    await gotoHydrated(page, '/app/settings/sessions');
    await forceReauthOnce(page, '/api/v1/me/second-factor/recovery-codes');
    await page.getByTestId('recovery-regenerate').click();
    await page.locator('[data-action="recovery-regenerate-confirm"]').click();
    await page.getByTestId('second-factor-reauth-password').waitFor();
    await page.getByLabel('Current password', { exact: true })
      .fill(primaryPassword);
    await Promise.all([
      page.waitForURL(`${ORIGIN}/login/second-factor`, { timeout: WAIT_NAVIGATION_MS }),
      page.getByTestId('second-factor-reauth-submit').click(),
    ]);
    await hydrated(page, WAIT_HYDRATE_MS);
    steps.reauthPending = true;

    stage('reauth-wrong-binding');
    const unbound = await trustedPost(
      ca,
      '/api/v1/auth/second-factor/passkey/options',
      {
        'Content-Type': 'application/json',
        'Cookie': await pendingCookieHeader(context),
        'Origin': ORIGIN,
        'X-CSRF-Token': await pendingCSRFToken(page),
      },
      '{}',
    );
    expect(unbound).toEqual({ code: 'authentication_required', status: 401 });
    steps.wrongBindingRejected = true;
    await completeWithRecovery(page, primaryCodes[0] as string);
    expectLanding(page, 'landing-app-settings');

    // 10. Provider sign-in on the enrolled account also stops at pending.
    stage('provider-pending');
    await signOut(page);
    await watchingCallback(page, () => signInWithGoogle(page, {
      accountLabel: LINK_ACCOUNT_LABEL,
      fromLoginPage: true,
      returnPath: '/login/second-factor',
    }));
    await hydrated(page, WAIT_HYDRATE_MS);
    expect(await meStatus(page)).toBe(401);
    await expect(page.getByTestId('second-factor-passkey')).toBeVisible();
    steps.providerPending = true;
    await completeWithRecovery(page, primaryCodes[1] as string);
    await expectSignedInApp(page);

    // 11. Both locales at both proof widths on the pending page.
    stage('locales');
    await provePendingLocale(page, context, {
      code: primaryCodes[2] as string,
      email: primaryEmail,
      locale: 'vi',
      password: primaryPassword,
      viewport: PHONE,
    });
    await provePendingLocale(page, context, {
      code: primaryCodes[3] as string,
      email: primaryEmail,
      locale: 'en',
      password: primaryPassword,
      viewport: DESKTOP,
    });
    steps.locales = true;
    steps.viewports = true;

    // 12. A password reset revokes sessions but preserves enforcement.
    stage('password-reset');
    const resetPassword = runPassword();
    await gotoHydrated(page, '/forgot-password');
    await page.getByLabel('Email').fill(primaryEmail);
    await page.getByRole('button', { name: 'Send reset link' }).click();
    await expect(page.getByTestId('forgot-success')).toBeVisible();
    const resetToken = await capture.waitForToken('reset', primaryEmail);
    await gotoFirstVisit(page, `${ORIGIN}/reset-password#token=${resetToken}`);
    await page.getByLabel('New password', { exact: true }).fill(resetPassword);
    await page.getByLabel('Confirm password', { exact: true })
      .fill(resetPassword);
    await page.getByRole('button', { name: 'Reset password' }).click();
    await expect(page.getByTestId('reset-success')).toBeVisible();
    primaryFinalPassword = resetPassword;
    expect(await passwordSignIn(page, primaryEmail, resetPassword))
      .toBe('pending');
    steps.resetPreservesEnforcement = true;
    await completeWithRecovery(page, primaryCodes[4] as string);
    await expectSignedInApp(page);

    // 13. Removing the final factor turns second-factor sign-in off.
    stage('final-removal');
    await gotoHydrated(page, '/app/settings/sessions');
    await page.locator('[data-testid^="passkey-remove-"]').first().click();
    await expect(page.getByRole('alertdialog'))
      .toContainText('This is your last passkey.');
    await page.locator('[data-action="passkey-remove-confirm"]').click();
    await expect(page.getByTestId('passkey-removed-success')).toBeVisible();
    state = await factorState(page);
    expect(state)
      .toEqual({
        enabled: false,
        passkeyCount: 0,
        recoveryRemaining: 0,
        status: 200,
      });
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, resetPassword))
      .toBe('session');
    primaryCleanupCode = '';
    steps.finalRemoved = true;

    // 14. A second fictional account carries the recovery-code and ceremony
    //     cases, which need their own attempt budget.
    role('recovery');
    stage('recovery-account');
    await signOut(page);
    authRecovery = await pool.add();
    await registerVerified(
      page,
      capture,
      recoveryEmail,
      recoveryPassword,
      'Recovery Proof',
    );
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('session');
    const codes = await enrollFirstPasskey(page, recoveryPassword);
    recoveryCleanupCode = codes[9] as string;
    expect(await pool.credentialCount(authRecovery)).toBe(1);

    stage('ceremony-replay');
    await signOut(page);
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('pending');
    const captured = await captureAssertion(page);
    const consumedPending = await trustedPost(
      ca,
      '/api/v1/auth/second-factor/passkey/verify',
      {
        'Content-Type': 'application/json',
        'Cookie': `${PENDING_COOKIE}=${captured.pendingToken}`,
        'Origin': ORIGIN,
        'X-CSRF-Token': captured.csrfToken,
      },
      captured.body,
    );
    expect(consumedPending)
      .toEqual({ code: 'authentication_required', status: 401 });

    await signOut(page);
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('pending');
    const foreignCeremony = await trustedPost(
      ca,
      '/api/v1/auth/second-factor/passkey/verify',
      {
        'Content-Type': 'application/json',
        'Cookie': await pendingCookieHeader(context),
        'Origin': ORIGIN,
        'X-CSRF-Token': await pendingCSRFToken(page),
      },
      captured.body,
    );
    expect(foreignCeremony).toEqual({ code: 'challenge_invalid', status: 400 });
    steps.ceremonyReplayRejected = true;

    stage('recovery-completion');
    await completeWithRecovery(page, codes[0] as string);
    await expectSignedInApp(page);
    steps.recoveryCompletion = true;

    stage('recovery-reuse');
    await signOut(page);
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('pending');
    expect(await submitRecoveryCode(page, codes[0] as string)).toBe(401);
    await expect(page.getByTestId('second-factor-recovery-error'))
      .toBeVisible();
    expect(await meStatus(page)).toBe(401);
    await completeWithRecovery(page, codes[1] as string);
    steps.recoveryReuseRejected = true;

    stage('recovery-concurrent');
    await signOut(page);
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('pending');
    const concurrentHeaders = {
      'Content-Type': 'application/json',
      'Cookie': await pendingCookieHeader(context),
      'Origin': ORIGIN,
      'X-CSRF-Token': await pendingCSRFToken(page),
    };
    const concurrentBody = JSON.stringify({ code: codes[2] });
    const race = await Promise.all([
      trustedPost(ca, '/api/v1/auth/second-factor/recovery/verify',
        concurrentHeaders, concurrentBody),
      trustedPost(ca, '/api/v1/auth/second-factor/recovery/verify',
        concurrentHeaders, concurrentBody),
    ]);
    expect(race.filter((result) => result.status === 204)).toHaveLength(1);
    expect(race.filter((result) => result.status === 401)).toHaveLength(1);
    steps.concurrentCompletion = true;

    // 15. A third fictional account carries attempt exhaustion, whose five
    //     failures would otherwise spend another account's whole budget.
    role('attempts');
    stage('attempts-account');
    await gotoHydrated(page, '/login');
    authAttempts = await pool.add();
    await registerVerified(
      page,
      capture,
      attemptsEmail,
      attemptsPassword,
      'Attempts Proof',
    );
    expect(await passwordSignIn(page, attemptsEmail, attemptsPassword))
      .toBe('session');
    const attemptsCodes = await enrollFirstPasskey(page, attemptsPassword);
    attemptsCleanupCode = attemptsCodes[9] as string;
    expect(await pool.credentialCount(authAttempts)).toBe(1);

    stage('attempts-exhausted');
    await signOut(page);
    expect(await passwordSignIn(page, attemptsEmail, attemptsPassword))
      .toBe('pending');
    for (let attempt = 1; attempt <= 5; attempt += 1) {
      expect(await submitRecoveryCode(
        page,
        fabricatedRecoveryCode(attemptsCodes),
      )).toBe(401);
    }
    expect(await cookieValue(context, PENDING_COOKIE)).toBeNull();
    await page.reload({ timeout: WAIT_NAVIGATION_MS });
    await hydrated(page, WAIT_HYDRATE_MS);
    await expect(page.getByTestId('second-factor-expired')).toBeVisible();
    await expect(page.getByTestId('second-factor-sign-in-again')).toBeVisible();
    expect(await meStatus(page)).toBe(401);
    steps.attemptsExhausted = true;
  } finally {
    beginTeardown();
    const removed = [
      await deleteAccount(page, pool, {
        authenticatorId: authPrimary,
        email: primaryEmail,
        password: primaryFinalPassword,
        recoveryCode: primaryCleanupCode,
      }),
      await deleteAccount(page, pool, {
        authenticatorId: authRecovery,
        email: recoveryEmail,
        password: recoveryPassword,
        recoveryCode: recoveryCleanupCode,
      }),
      await deleteAccount(page, pool, {
        authenticatorId: authAttempts,
        email: attemptsEmail,
        password: attemptsPassword,
        recoveryCode: attemptsCleanupCode,
      }),
    ];
    steps.cleanup = removed.every(Boolean);
    for (const extra of extraContexts) {
      await extra.close().catch(() => undefined);
    }
    await writeFile(
      ENABLED_EVIDENCE_PATH,
      `${JSON.stringify({
        errors: {
          certificate: counters.certificateErrors,
          console: counters.consoleErrors,
          externalRequest: counters.externalRequests,
          page: counters.pageErrors,
        },
        origin: ORIGIN,
        scenario: 'passkey-second-factor',
        schemaVersion: 1,
        steps,
      }, null, 2)}\n`,
      { flag: 'wx', mode: 0o600 },
    );
  }
});

// --- Disabled-enrollment journey -------------------------------------------

test('proves disabled passkey enrollment answers as an unregistered route',
  async ({ context, page }) => {
    test.skip(
      MODE !== 'second-factor-disabled',
      'disabled enrollment mode only',
    );

    const steps = {
      assertionRouteRegistered: false,
      capabilityClosed: false,
      cleanup: false,
      completionNotFound: false,
      enrollmentHidden: false,
      locales: false,
      optionsNotFound: false,
      recoveryRouteRegistered: false,
      removalRouteRegistered: false,
      stateAvailable: false,
      unregisteredRouteMatches: false,
      viewports: false,
    };

    const counters = newDiagnosticCounters();
    const attach = pageDiagnosticsAttacher(counters, {
      countConsoleError: isUnexpectedSecondFactorConsole,
    });
    attach(page);
    context.on('page', attach);
    await installExternalRequestFirewall(context, counters);
    await installExternalWebSocketFirewall(context, counters);
    await setLocale(context, 'en');
    await page.setViewportSize(DESKTOP);

    try {
      role('disabled');
      await warmRoutes(page, DISABLED_WARM_ROUTES, counters);

      stage('disabled-capability');
      await signInWithGoogle(page, {
        accountLabel: DISABLED_ACCOUNT_LABEL,
        fromLoginPage: true,
      });
      await warmRoutes(page, SIGNED_IN_WARM_ROUTES, counters);
      const capability = await page.evaluate(async () => {
        const response = await fetch('/api/v1/capabilities', {
          cache: 'no-store',
        });
        const body = (await response.json()) as {
          data?: { passkeyEnrollment?: unknown };
        };
        return {
          hasTotp: Object.hasOwn(body.data ?? {}, 'totpEnrollment'),
          passkeyEnrollment: body.data?.passkeyEnrollment,
          status: response.status,
        };
      });
      expect(capability).toEqual({
        hasTotp: false,
        passkeyEnrollment: false,
        status: 200,
      });
      steps.capabilityClosed = true;

      stage('disabled-settings');
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('second-factor-settings')).toBeVisible();
      await expect(page.getByTestId('second-factor-empty')).toBeVisible();
      await expect(page.getByTestId('passkey-add')).toHaveCount(0);
      await expect(page.getByTestId('second-factor-unsupported'))
        .toHaveCount(0);
      steps.enrollmentHidden = true;

      stage('disabled-state');
      expect(await factorState(page)).toEqual({
        enabled: false,
        passkeyCount: 0,
        recoveryRemaining: 0,
        status: 200,
      });
      steps.stateAvailable = true;

      stage('disabled-routes');
      const probes = await probeDisabledRoutes(page, await freshCSRF(page));
      expect(probes.options).toEqual({ code: 'not_found', status: 404 });
      steps.optionsNotFound = true;
      expect(probes.completion).toEqual({ code: 'not_found', status: 404 });
      steps.completionNotFound = true;
      expect(probes.unregistered).toEqual(probes.options);
      steps.unregisteredRouteMatches = true;
      expect(probes.removal).toEqual({ code: 'factor_not_found', status: 404 });
      steps.removalRouteRegistered = true;
      expect(probes.assertion)
        .toEqual({ code: 'authentication_required', status: 401 });
      steps.assertionRouteRegistered = true;
      expect(probes.recovery)
        .toEqual({ code: 'authentication_required', status: 401 });
      steps.recoveryRouteRegistered = true;

      stage('disabled-locales');
      await setLocale(context, 'vi');
      await page.setViewportSize(PHONE);
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('second-factor-settings'))
        .toContainText('Chưa có passkey nào.');
      await expect(page.getByTestId('passkey-add')).toHaveCount(0);
      expect(await page.evaluate(() =>
        document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await setLocale(context, 'en');
      await page.setViewportSize(DESKTOP);
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('second-factor-settings'))
        .toContainText('No passkeys yet.');
      await expect(page.getByTestId('passkey-add')).toHaveCount(0);
      steps.locales = true;
      steps.viewports = true;
    } finally {
      beginTeardown();
      steps.cleanup = await deleteSignedInAccount(page).catch(() => false);
      await writeFile(
        DISABLED_EVIDENCE_PATH,
        `${JSON.stringify({
          errors: {
            certificate: counters.certificateErrors,
            console: counters.consoleErrors,
            externalRequest: counters.externalRequests,
            page: counters.pageErrors,
          },
          origin: ORIGIN,
          scenario: 'passkey-enrollment-disabled',
          schemaVersion: 1,
          steps,
        }, null, 2)}\n`,
        { flag: 'wx', mode: 0o600 },
      );
    }
  });

// --- Shared journey helpers -------------------------------------------------

/** The pending row's own CSRF token, read through its status route. */
async function pendingCSRFToken(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/second-factor', {
      cache: 'no-store',
      credentials: 'include',
    });
    const body = (await response.json()) as { data?: { csrfToken?: unknown } };
    if (response.status !== 200 || typeof body.data?.csrfToken !== 'string') {
      throw new Error('pending status read failed');
    }
    return body.data.csrfToken;
  });
}

interface CapturedAssertion {
  readonly body: string;
  readonly csrfToken: string;
  readonly pendingToken: string;
}

/**
 * Completes one pending sign-in with the virtual authenticator and keeps the
 * exact request bytes, so a replay of the same consumed ceremony can be proved
 * from Node. The captured value never leaves process memory.
 */
async function captureAssertion(page: Page): Promise<CapturedAssertion> {
  const pattern = `${ORIGIN}/api/v1/auth/second-factor/passkey/verify`;
  const pendingToken = await cookieValue(page.context(), PENDING_COOKIE);
  const csrfToken = await pendingCSRFToken(page);
  let body = '';
  await page.route(pattern, async (route: Route) => {
    body = route.request().postData() ?? '';
    await route.continue();
  });
  await completeWithPasskey(page);
  await page.unroute(pattern);
  if (body === '' || pendingToken === null) {
    throw new Error('assertion capture failed');
  }
  return { body, csrfToken, pendingToken };
}

/** Registers a connected agent and completes one consent round trip. */
async function createAgentGrant(
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
async function agentToolsStatus(page: Page, token: string): Promise<number> {
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

interface PendingLocaleCase {
  readonly code: string;
  readonly email: string;
  readonly locale: 'en' | 'vi';
  readonly password: string;
  readonly viewport: { readonly height: number; readonly width: number };
}

/**
 * Signs in again in one locale at one proof width and completes the pending
 * page entirely by keyboard, checking the localized copy, the accessible
 * names, the focus order, and that nothing overflows the viewport.
 */
async function provePendingLocale(
  page: Page,
  context: BrowserContext,
  options: PendingLocaleCase,
): Promise<void> {
  const vietnamese = options.locale === 'vi';
  await signOut(page);
  await setLocale(context, options.locale);
  await page.setViewportSize({
    height: options.viewport.height,
    width: options.viewport.width,
  });
  expect(await passwordSignIn(page, options.email, options.password))
    .toBe('pending');
  await expect(page.getByRole('heading', {
    name: vietnamese ? 'Xác thực hai bước' : 'Two-factor verification',
  })).toBeVisible();
  await expect(page.getByTestId('second-factor-page')).toContainText(
    vietnamese
      ? 'Hoàn tất đăng nhập bằng passkey hoặc mã khôi phục.'
      : 'Finish signing in with a passkey or a recovery code.',
  );
  await expect(page.getByRole('button', {
    name: vietnamese ? 'Tiếp tục với passkey' : 'Continue with passkey',
  })).toBeVisible();
  await expect(page.getByLabel(
    vietnamese ? 'Mã khôi phục' : 'Recovery code',
    { exact: true },
  )).toBeVisible();
  const passkeyButton = page.getByTestId('second-factor-passkey-button');
  await passkeyButton.focus();
  await expect(passkeyButton).toBeFocused();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await completeWithRecovery(page, options.code);
  await expectSignedInApp(page);
}

interface DisabledProbes {
  readonly assertion: TrustedResult;
  readonly completion: TrustedResult;
  readonly options: TrustedResult;
  readonly recovery: TrustedResult;
  readonly removal: TrustedResult;
  readonly unregistered: TrustedResult;
}

/**
 * Probes every second-factor route from the signed-in page while enrollment
 * is off: the two enrollment routes must match the never-registered TOTP
 * path, and the flag-independent routes must answer with their own codes.
 */
async function probeDisabledRoutes(
  page: Page,
  csrf: string,
): Promise<DisabledProbes> {
  return page.evaluate(async (token) => {
    const call = async (
      path: string,
      method: string,
      body: string | null,
    ): Promise<{ status: number; code: string | null }> => {
      const response = await fetch(path, {
        body,
        cache: 'no-store',
        credentials: 'include',
        headers: body === null
          ? { 'X-CSRF-Token': token }
          : { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
        method,
      });
      let code: string | null = null;
      try {
        const parsed = (await response.json()) as {
          error?: { code?: unknown };
        };
        if (typeof parsed.error?.code === 'string') code = parsed.error.code;
      } catch {
        // A bodiless response leaves the code null.
      }
      return { code, status: response.status };
    };
    const unknownID = '01900000-0000-7000-8000-000000000001';
    return {
      assertion: await call(
        '/api/v1/auth/second-factor/passkey/options', 'POST', '{}'),
      completion: await call(
        '/api/v1/me/second-factor/passkeys',
        'POST',
        JSON.stringify({
          ceremonyId: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
          credential: {
            clientExtensionResults: {},
            id: 'AAAA',
            rawId: 'AAAA',
            response: {
              attestationObject: 'AAAA',
              clientDataJSON: 'AAAA',
              transports: ['internal'],
            },
            type: 'public-key',
          },
        }),
      ),
      options: await call(
        '/api/v1/me/second-factor/passkeys/options', 'POST', '{}'),
      recovery: await call(
        '/api/v1/auth/second-factor/recovery/verify',
        'POST',
        JSON.stringify({ code: 'amr_00000000000000000000000000' }),
      ),
      removal: await call(
        `/api/v1/me/second-factor/passkeys/${unknownID}`, 'DELETE', null),
      unregistered: await call(
        '/api/v1/me/second-factor/totp/enrollment', 'POST', '{}'),
    };
  }, csrf);
}

// --- Teardown ---------------------------------------------------------------

interface AccountTeardown {
  readonly authenticatorId: string;
  readonly email: string;
  readonly password: string;
  /** An unused recovery code, when the account is still enrolled. */
  readonly recoveryCode: string;
}

/**
 * Removes one fictional account. An enrolled account finishes its pending
 * sign-in with a spare recovery code, which sets both verification times and
 * therefore satisfies the deletion boundary without a further round trip.
 */
async function deleteAccount(
  page: Page,
  pool: AuthenticatorPool,
  account: AccountTeardown,
): Promise<boolean> {
  if (account.email === '') return true;
  try {
    if (account.authenticatorId !== '') {
      await pool.present(account.authenticatorId);
    }
    await page.context().clearCookies();
    await setLocale(page.context(), 'en');
    const outcome = await passwordSignIn(page, account.email, account.password);
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
async function deleteSignedInAccount(page: Page): Promise<boolean> {
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
