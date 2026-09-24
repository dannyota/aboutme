/**
 * Authenticator-app (TOTP) second-factor journey over the trusted HTTPS
 * harness. One spec serves two browser modes, mirroring
 * `second-factor.spec.ts`. `totp` runs against a server whose TOTP
 * enrollment flag is on and walks enrollment, step acceptance, replay and
 * concurrency rejection, replacement, recovery, passkey coexistence,
 * revocation, and removal. `totp-disabled` runs against the same stack
 * restarted with the flag off and proves enrollment answers exactly like an
 * unregistered route while verification, removal, state, passkeys, and
 * recovery keep working.
 *
 * The setup secret, provisioning URI, and every code are computed in this
 * process with `totp-fixture.ts` and never leave it: the evidence file holds
 * only booleans, the fixed scenario name, and the origin.
 *
 * Contract: docs/design/totp-second-factor-contract.md,
 * docs/design/totp-key-management.md, and ADR 0049.
 */
import {
  expect,
  test,
  type BrowserContext,
  type ConsoleMessage,
  type Page,
  type Request,
  type Route,
} from '@playwright/test';
import { randomBytes } from 'node:crypto';
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
import {
  codeForStep,
  codeNextStep,
  codeNow,
  codePreviousStep,
  mismatchedCode,
  stepAt,
  systemClock,
  toFullwidthDigits,
  TOTP_PERIOD_SECONDS,
} from './totp-fixture';

const ORIGIN = ALLOWED_ORIGIN;
const MODE = process.env.ABOUTME_BROWSER_MODE ?? 'totp';
const CA_PATH = '/uat-input/caddy-root.crt';
const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
const CLIENT_NAME_PATH = '/uat-input/mcp-client-name';
const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
const ENABLED_EVIDENCE_PATH = '/evidence/totp-second-factor-proof.json';
const DISABLED_EVIDENCE_PATH = '/evidence/totp-enrollment-disabled-proof.json';
const REDIRECT_URI = 'http://127.0.0.1:20090/callback';
const LINK_ACCOUNT_LABEL = 'Bob Local — bob@example.invalid';
const DISABLED_ACCOUNT_LABEL = 'Development User — developer@example.invalid';
const PENDING_COOKIE = '__Host-auth-pending';
const SESSION_COOKIE = '__Host-session';
const RECOVERY_INPUT = '#second-factor-recovery-code';
const TOTP_LOGIN_INPUT = '#second-factor-totp-code';
const TOTP_SETUP_INPUT = '#totp-code';
const PHONE = { height: 844, width: 390 };
const DESKTOP = { height: 900, width: 1440 };
const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

// See second-factor.spec.ts for why each of these is bounded rather than
// left to Playwright's own defaults.
const WAIT_RESPONSE_MS = 30_000;
const WAIT_NAVIGATION_MS = 60_000;
const WAIT_LANDING_MS = 180_000;
const WAIT_WARM_MS = 90_000;
const WAIT_HYDRATE_MS = 30_000;
const WAIT_LOOPBACK_MS = 20_000;
const WAIT_MAIL_MS = 45_000;

const EXPECTED_PAGE_FAILURES: ReadonlyMap<string, readonly number[]> = new Map([
  ['/api/v1/me', [401]],
  ['/api/v1/me/second-factor', [401]],
  ['/api/v1/me/second-factor/totp', [403, 404]],
  ['/api/v1/me/second-factor/totp/enrollment', [403, 404]],
  ['/api/v1/me/second-factor/unregistered/enrollment', [404]],
  ['/api/v1/auth/second-factor', [401]],
  ['/api/v1/auth/second-factor/totp/verify', [401]],
  ['/api/v1/auth/second-factor/recovery/verify', [401]],
  ['/api/v1/auth/password/login', [401]],
  ['/api/v1/auth/password/reauth', [401]],
  ['/mcp', [401]],
]);

// --- Failure reporting, modeled on second-factor.spec.ts --------------------

type FailureOutcome
  = | 'timeout'
    | 'navigation'
    | 'response'
    | 'capture'
    | 'locator'
    | 'assertion'
    | 'unknown';

type AccountRole =
  | 'none'
  | 'primary'
  | 'replay'
  | 'concurrent'
  | 'replace'
  | 'epoch'
  | 'locale'
  | 'recovery'
  | 'attempts'
  | 'disabled';

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

function beginTeardown(): void {
  tearingDown = true;
  console.log(`${MODE}-stage:cleanup-after-${recordedStage}`);
}

const OUTCOME_PATTERNS: ReadonlyArray<readonly [RegExp, FailureOutcome]> = [
  [/^Test timeout of \d+ms exceeded/u, 'timeout'],
  [/waitForURL|waiting for navigation/u, 'navigation'],
  [/waitForResponse|waitForEvent/u, 'response'],
  [/capture (read|reset) failed|within its capture bound/u, 'capture'],
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

/**
 * Closed test ids a failure may name. They are component ids, not secrets,
 * and each maps to one word inside run.sh's [a-z0-9-] stage filter.
 */
const DIAG_TEST_IDS: ReadonlyArray<readonly [string, string]> = [
  ['second-factor-reauth-password', 'reauth'],
  ['totp-setup-dialog', 'setupdialog'],
  ['totp-start-error', 'starterror'],
  ['totp-setup-error', 'setuperror'],
  ['totp-remove-error', 'removeerror'],
  ['totp-setup-replace', 'replacebutton'],
  ['totp-setup-start', 'startbutton'],
  ['totp-replace-notice', 'replacenotice'],
  ['totp-replaced-success', 'replacedsuccess'],
  ['totp-settings', 'settings'],
  ['second-factor-totp', 'pendingtotp'],
];

/** Names the closed test id a failed locator waited for, if any. */
function waitedFor(message: string): string {
  const match = /getByTestId\('([a-z0-9-]+)'\)/u.exec(message);
  if (match === null) return 'none';
  const known = DIAG_TEST_IDS.find(([id]) => id === match[1]);
  return known === undefined ? 'unlisted' : known[1];
}

let failureSeen = 'none';
let failurePage = 'unknown';

/** Lists which closed test ids are visible on the page right now. */
async function visibleWords(page: Page): Promise<string> {
  const words: string[] = [];
  for (const [id, word] of DIAG_TEST_IDS) {
    try {
      if (await page.getByTestId(id).first().isVisible()) words.push(word);
    } catch {
      // A closed page shows nothing.
    }
  }
  return words.length > 0 ? words.join('-') : 'none';
}

test.afterEach(({}, testInfo) => {
  if (testInfo.status === 'skipped') return;
  if (testInfo.status === testInfo.expectedStatus) return;
  const outcome = outcomeOf(
    testInfo.status ?? 'unknown',
    testInfo.error?.message ?? '',
  );
  // run.sh prints only the last stage line, so the closed diagnostic rides
  // on the failure line itself.
  const message = testInfo.error?.message ?? '';
  const errorKind = /strict mode violation/u.test(message) ? 'strict'
    : /waitForResponse/u.test(message) ? 'response-wait'
      : /toBe/u.test(message) ? 'status-mismatch' : 'other';
  console.log(
    `${MODE}-stage:fail-${outcome}-at-${recordedStage}-for-${recordedRole}`
      + `-verify-${verifyTrace}-error-${errorKind}`
      + `-waited-${waitedFor(message)}-page-${failurePage}`
      + `-seen-${failureSeen}`,
  );
});

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
  return url.searchParams.get('error') === null
    ? 'callback-settings-clean'
    : 'callback-settings-error';
}

const APP_LANDINGS: readonly string[] = [
  'landing-app-new',
  'landing-app-other',
  'landing-app-resume',
  'landing-app-resumes',
  'landing-app-settings',
];

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

async function submitState(page: Page): Promise<string> {
  const submit = page
    .getByTestId('login-form')
    .locator('button[type="submit"]');
  if (await submit.count() === 0) return 'submit-absent';
  return await submit.isDisabled() ? 'submit-busy' : 'submit-idle';
}

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

function expectLanding(page: Page, want: string): void {
  expect(landingCategory(page.url())).toBe(want);
}

async function expectSignedInApp(page: Page): Promise<void> {
  expect(APP_LANDINGS).toContain(landingCategory(page.url()));
  expect(await meStatus(page)).toBe(200);
}

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

/**
 * Closed words for each unexpected console error: the index of a known path
 * in EXPECTED_PAGE_FAILURES (or `other`, `offorigin`, `nonhttp`) plus the
 * status. No URL, query, or body text is kept.
 */
const unexpectedConsole: string[] = [];

function isUnexpectedTotpConsole(message: ConsoleMessage): boolean {
  const unexpected = classifyTotpConsole(message);
  if (unexpected !== null) unexpectedConsole.push(unexpected);
  return unexpected !== null;
}

function classifyTotpConsole(message: ConsoleMessage): string | null {
  const text = message.text();
  const location = message.location().url;
  if (isExpectedNegativeHTTPConsole(text, location)) return null;
  const status = httpFailureStatus(text);
  if (status === null) return 'nonhttp';
  let url: URL;
  try {
    url = new URL(location);
  } catch {
    return 'nonhttp';
  }
  if (url.origin !== ORIGIN) return `offorigin-${status}`;
  const allowed = EXPECTED_PAGE_FAILURES.get(url.pathname);
  if (allowed !== undefined && allowed.includes(status)) return null;
  const index = [...EXPECTED_PAGE_FAILURES.keys()].indexOf(url.pathname);
  return `${index < 0 ? 'other' : `path${index}`}-${status}`;
}

/**
 * Fails a completed journey whose page logged an unexpected error, naming
 * the first few in closed words so the hosted log shows them.
 */
function failOnUnexpectedConsole(journeyDone: boolean): void {
  if (!journeyDone || unexpectedConsole.length === 0) return;
  // Teardown has begun, so stage() is silent; record the words directly.
  recordedStage
    = `console-unexpected-${unexpectedConsole.slice(0, 4).join('-')}`;
  console.log(`${MODE}-stage:${recordedStage}`);
  throw new Error('the page logged unexpected console errors');
}

// --- Fictional run identity --------------------------------------------

const RUN_MARKER = randomBytes(6).toString('hex');
let accountSequence = 0;

function runEmail(role: string): string {
  accountSequence += 1;
  return `totp-${role}-${RUN_MARKER}-${accountSequence}@example.invalid`;
}

function runPassword(): string {
  return randomBytes(24).toString('base64url');
}

function canonicalRecovery(value: string): string {
  return value.replace(/[- ]/gu, '').toUpperCase();
}

function fabricatedRecoveryCode(issued: readonly string[]): string {
  for (;;) {
    let body = '';
    for (const [index, byte] of [...randomBytes(26)].entries()) {
      body += CROCKFORD[byte % (index === 0 ? 8 : 32)];
    }
    const code = `amr_${body}`;
    const wanted = canonicalRecovery(code);
    if (!issued.some((value) => canonicalRecovery(value) === wanted)) {
      return code;
    }
  }
}

// --- Trusted loopback requests -------------------------------------------

interface TrustedResult {
  readonly status: number;
  readonly code: string | null;
}

function trustedRequest(
  ca: Buffer,
  method: 'POST' | 'PUT',
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
        method,
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

function trustedPost(
  ca: Buffer,
  path: string,
  headers: Readonly<Record<string, string>>,
  body: string,
): Promise<TrustedResult> {
  return trustedRequest(ca, 'POST', path, headers, body);
}

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

async function pendingCookieHeader(context: BrowserContext): Promise<string> {
  const token = await cookieValue(context, PENDING_COOKIE);
  if (token === null) throw new Error('no pending cookie to carry');
  return `${PENDING_COOKIE}=${token}`;
}

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

// --- Captured security mail ------------------------------------------------

interface CapturedMessage {
  kind: string;
  to: string;
  text_body: string;
}

interface CaptureClient {
  reset(): Promise<void>;
  waitForToken(kind: string, to: string): Promise<string>;
  waitForKind(kind: string, to: string): Promise<void>;
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
    async waitForKind(kind, to) {
      const deadline = Date.now() + WAIT_MAIL_MS;
      while (Date.now() < deadline) {
        for (const message of await read()) {
          if (message.kind === kind && message.to === to) return;
        }
        await new Promise((resolve) => setTimeout(resolve, 250));
      }
      throw new Error('no security message within its capture bound');
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

// --- Page helpers ------------------------------------------------------

async function setLocale(
  context: BrowserContext,
  locale: 'en' | 'vi',
): Promise<void> {
  await context.addCookies([
    { name: 'aboutme-locale', url: ORIGIN, value: locale },
  ]);
}

const WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/register', 'register'],
  ['/login', 'login'],
  ['/login/second-factor', 'login-second-factor'],
  ['/forgot-password', 'forgot-password'],
];

const SIGNED_IN_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/app/resumes', 'app-resumes'],
  ['/app/settings/sessions', 'app-settings-sessions'],
];

const DISABLED_WARM_ROUTES: ReadonlyArray<readonly [string, string]> = [
  ['/login', 'login'],
];

type CounterClass = 'certificate' | 'console' | 'external' | 'page';

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
  readonly totpEnabled: boolean;
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
        recoveryRemaining: -1,
        status: response.status,
        totpEnabled: false,
      };
    }
    const body = (await response.json()) as {
      data?: {
        enabled?: unknown;
        totpEnabled?: unknown;
        recoveryCodesRemaining?: unknown;
      };
    };
    return {
      enabled: body.data?.enabled === true,
      recoveryRemaining: typeof body.data?.recoveryCodesRemaining === 'number'
        ? body.data.recoveryCodesRemaining
        : -1,
      status: response.status,
      totpEnabled: body.data?.totpEnabled === true,
    };
  });
}

async function registerVerified(
  page: Page,
  capture: CaptureClient,
  email: string,
  password: string,
  name: string,
): Promise<void> {
  stage('register-open');
  await gotoHydrated(page, '/register');
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

async function signOut(page: Page): Promise<void> {
  await setLocale(page.context(), 'en');
  await gotoHydrated(page, '/app/settings/sessions');
  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(`${ORIGIN}/login`, { timeout: WAIT_NAVIGATION_MS });
  await setLocale(page.context(), 'en');
}

/** Submits one TOTP code on the pending login page and returns the status. */
/**
 * A closed, secret-free trace of the last pending TOTP submission, printed
 * with a failure so a hosted run shows how far the request got.
 */
let verifyTrace = 'none';
const TOTP_VERIFY_PATH = '/api/v1/auth/second-factor/totp/verify';

async function submitPendingTotpCode(page: Page, code: string): Promise<number> {
  verifyTrace = 'filled';
  const onRequest = (request: { url(): string }): void => {
    if (new URL(request.url()).pathname === TOTP_VERIFY_PATH) {
      verifyTrace = 'requested';
    }
  };
  page.on('request', onRequest);
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/auth/second-factor/totp/verify';
  }, { timeout: WAIT_RESPONSE_MS }).then((answer) => {
    verifyTrace = `status-${answer.status()}`;
    return answer;
  }).finally(() => page.off('request', onRequest));
  const input = page.locator(TOTP_LOGIN_INPUT);
  await input.click();
  await input.fill(code);
  // The recovery form's button has the same name, so scope to the TOTP form.
  await page.getByTestId('second-factor-totp-form')
    .locator('button[type="submit"]').click();
  return (await response).status();
}

/** Completes the open pending authentication with a valid TOTP code. */
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
  tracker: StepTracker,
): Promise<void> {
  await page.getByTestId('second-factor-reauth-password').waitFor();
  await page.getByLabel('Current password', { exact: true }).fill(password);
  await Promise.all([
    page.waitForURL(`${ORIGIN}/login/second-factor`,
      { timeout: WAIT_NAVIGATION_MS }),
    page.getByTestId('second-factor-reauth-submit').click(),
  ]);
  await hydrated(page, WAIT_HYDRATE_MS);
  await completeWithTotp(page, await freshCode(page, secret, tracker));
  await page.waitForURL(`${ORIGIN}/app/settings/sessions`,
    { timeout: WAIT_NAVIGATION_MS });
  await hydrated(page, WAIT_HYDRATE_MS);
}

async function completeWithTotp(page: Page, code: string): Promise<void> {
  expect(await submitPendingTotpCode(page, code)).toBe(204);
  await landedAfter(page, '/login/second-factor');
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
  expect(await submitRecoveryCode(page, code)).toBe(204);
  await landedAfter(page, '/login/second-factor');
}

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

/** Reads the grouped secret from the open setup dialog. */
async function readSetupSecret(page: Page): Promise<string> {
  await expect(page.getByTestId('totp-setup-dialog')).toBeVisible();
  await expect(page.getByTestId('totp-qr')).toBeVisible();
  const grouped = (await page.getByTestId('totp-secret').textContent()) ?? '';
  const secret = grouped.replace(/\s+/gu, '');
  expect(secret).toMatch(/^[A-Z2-7]{32}$/u);
  return secret;
}

/** Proves opening the setup dialog issues no external network request. */
async function proveQrIsLocal(
  page: Page,
  counters: DiagnosticCounters,
): Promise<void> {
  const before = counters.externalRequests;
  const seen: string[] = [];
  const onRequest = (request: Request): void => { seen.push(request.url()); };
  page.on('request', onRequest);
  try {
    await expect(page.getByTestId('totp-qr')).toBeVisible();
    await page.waitForTimeout(250);
  } finally {
    page.off('request', onRequest);
  }
  expect(counters.externalRequests).toBe(before);
  const foreign = seen.some((url) => {
    try {
      return new URL(url).origin !== ORIGIN;
    } catch {
      return true;
    }
  });
  expect(foreign).toBe(false);
}

/**
 * The newest step one account's accepted codes have used. The server accepts
 * only a step greater than the credential's last used step, so every later
 * accepted code must come from a newer step. Each fictional account gets its
 * own tracker because last_used_step is per credential.
 */
interface StepTracker { last: number }

function newStepTracker(): StepTracker {
  return { last: -1 };
}

/** Returns a code from a step newer than every step the tracker has used, taking the next step without waiting unless it is already spent. */
async function freshCode(
  page: Page,
  secret: string,
  tracker: StepTracker,
): Promise<string> {
  let step = stepAt(systemClock().nowSeconds()) + 1;
  if (step <= tracker.last) {
    await waitForStepAtLeast(page, tracker.last);
    step = stepAt(systemClock().nowSeconds()) + 1;
  }
  tracker.last = step;
  return codeForStep(secret, step);
}

/** Waits on the real clock until the current step reaches `step` (totp-second-factor-contract.md, "TOTP profile and code verification"). */
async function waitForStepAtLeast(page: Page, step: number): Promise<void> {
  const target = step * TOTP_PERIOD_SECONDS;
  const waitMs = (target - Date.now() / 1000) * 1000;
  if (waitMs > 0) await page.waitForTimeout(Math.ceil(waitMs) + 500);
}

/** Submits one TOTP code in the settings setup dialog and returns status. */
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

/**
 * Starts (or replaces) TOTP setup and returns the freshly read secret plus
 * the enrollment token the server issued. The token is held only in process
 * memory, for the HTTP-level supersession fixture; it never reaches evidence
 * or console output.
 */
async function startTotpSetupCapturing(
  page: Page,
): Promise<{ enrollmentId: string; secret: string }> {
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
  const response = await created;
  expect(response.status()).toBe(200);
  const body = (await response.json()) as { data?: { enrollmentId?: unknown } };
  const enrollmentId = body.data?.enrollmentId;
  if (typeof enrollmentId !== 'string' || enrollmentId === '') {
    throw new Error('enrollment response is missing its token');
  }
  const secret = await readSetupSecret(page);
  return { enrollmentId, secret };
}

/** Starts (or replaces) TOTP setup and returns the freshly read secret. */
async function startTotpSetup(page: Page): Promise<string> {
  return (await startTotpSetupCapturing(page)).secret;
}

/**
 * Enrolls a first TOTP credential on a freshly signed-in account, proving
 * the reauthentication gate and the one-time recovery reveal.
 */
async function enrollFirstTotp(
  page: Page,
  password: string,
  tracker: StepTracker,
): Promise<{ codes: string[]; secret: string }> {
  await gotoHydrated(page, '/app/settings/sessions');
  await expect(page.getByTestId('totp-status')).toContainText('Not set up.');
  await forceReauthOnce(page, '/api/v1/me/second-factor/totp/enrollment');
  await page.getByTestId('totp-setup-start').click();
  await expect(page.getByTestId('second-factor-reauth-password')).toBeVisible();
  await reauthenticateWithPassword(page, password);
  const secret = await startTotpSetup(page);
  const code = await freshCode(page, secret, tracker);
  expect(await submitSetupCode(page, code)).toBe(200);
  const codes = await readRevealedCodes(page);
  await closeRevealAndProveCleared(page, codes);
  await expect(page.getByTestId('totp-added-success')).toBeVisible();
  return { codes, secret };
}

// --- Enabled-enrollment journey ------------------------------------------

test('proves the authenticator-app second factor over native HTTPS', async ({
  browser,
  context,
  page,
}) => {
  test.skip(MODE !== 'totp', 'enabled enrollment mode only');

  const steps = {
    agentGranted: false,
    agentRevoked: false,
    attemptsExhausted: false,
    cleanup: false,
    concurrentUseRejected: false,
    currentStepAccepted: false,
    enrolled: false,
    finalRemoved: false,
    invalidCodeRejected: false,
    locales: false,
    nextStepAccepted: false,
    oneRemoved: false,
    otherSessionRevoked: false,
    otherSessionStarted: false,
    passkeyCoexistence: false,
    passwordPending: false,
    previousStepAccepted: false,
    providerAccount: false,
    providerPending: false,
    qrIsLocal: false,
    reauthRequired: false,
    recoveryCompletion: false,
    recoveryRevealedOnce: false,
    replaced: false,
    resetPreservesEnforcement: false,
    sameStepReplayRejected: false,
    supersededEnrollmentRejected: false,
    unicodeDigitsRejected: false,
    viewports: false,
    wrongEpochRejected: false,
    wrongSessionRejected: false,
  };

  const counters = newDiagnosticCounters();
  const attach = pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedTotpConsole,
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

  const primaryEmail = runEmail('primary');
  const primaryPassword = runPassword();
  const replayEmail = runEmail('replay');
  const replayPassword = runPassword();
  const concurrentEmail = runEmail('concurrent');
  const concurrentPassword = runPassword();
  const replaceEmail = runEmail('replace');
  const replacePassword = runPassword();
  const epochEmail = runEmail('epoch');
  const epochPassword = runPassword();
  const localeEmail = runEmail('locale');
  const localePassword = runPassword();
  const recoveryEmail = runEmail('recovery');
  const recoveryPassword = runPassword();
  const attemptsEmail = runEmail('attempts');
  const attemptsPassword = runPassword();

  const primaryTracker = newStepTracker();
  const replayTracker = newStepTracker();
  const concurrentTracker = newStepTracker();
  const replaceTracker = newStepTracker();
  const epochTracker = newStepTracker();
  const localeTracker = newStepTracker();
  const recoveryTracker = newStepTracker();
  const attemptsTracker = newStepTracker();

  let primaryCleanupCode = '';
  let replayCleanupCode = '';
  let concurrentCleanupCode = '';
  let replaceCleanupCode = '';
  let epochCleanupCode = '';
  let localeFinalPassword = localePassword;
  let localeCleanupCode = '';
  let recoveryCleanupCode = '';
  let attemptsCleanupCode = '';
  const extraContexts: BrowserContext[] = [];

  try {
    // 1. A fictional primary account with a password and a linked provider,
    //    plus a connected agent and a second session, all created before
    //    enrollment so completion's epoch change can be proved against them.
    role('primary');
    stage('primary-register');
    await registerVerified(
      page,
      capture,
      primaryEmail,
      primaryPassword,
      'TOTP Proof',
    );
    stage('primary-first-sign-in');
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('session');
    await warmRoutes(page, SIGNED_IN_WARM_ROUTES, counters);

    stage('primary-open-settings');
    await gotoHydrated(page, '/app/settings/sessions');
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

    // 2. First enrollment demands recent reauthentication, renders the QR
    //    with no external request, and issues the one-time recovery set.
    //    Primary spends 2 admitted attempts here: the superseded-enrollment
    //    completion and the accepted completion.
    stage('enroll-first-totp');
    await gotoHydrated(page, '/app/settings/sessions');
    await expect(page.getByTestId('totp-status')).toContainText('Not set up.');
    await forceReauthOnce(page, '/api/v1/me/second-factor/totp/enrollment');
    await page.getByTestId('totp-setup-start').click();
    await expect(page.getByTestId('second-factor-reauth-password'))
      .toBeVisible();
    steps.reauthRequired = true;
    await reauthenticateWithPassword(page, primaryPassword);
    const firstEnrollment = await startTotpSetupCapturing(page);
    let secret = firstEnrollment.secret;
    await proveQrIsLocal(page, counters);
    steps.qrIsLocal = true;

    // 3. Abandoning setup and starting again supersedes the first enrollment
    //    row. Proving the old enrollmentId with the new secret's own code
    //    against the completion route (an authenticated HTTP fixture, since
    //    the settings dialog always names its own current enrollment) shows
    //    the superseded row is gone, not merely that a stale code fails.
    stage('enrollment-superseded');
    await page.getByTestId('totp-setup-cancel').click();
    await expect(page.getByTestId('totp-setup-dialog')).toHaveCount(0);
    const secondEnrollment = await startTotpSetupCapturing(page);
    secret = secondEnrollment.secret;
    expect(secret).not.toBe(firstEnrollment.secret);
    const sessionCookie = await cookieValue(context, SESSION_COOKIE);
    if (sessionCookie === null) throw new Error('no session cookie to carry');
    const supersededResult = await trustedRequest(
      ca,
      'PUT',
      '/api/v1/me/second-factor/totp/enrollment',
      {
        'Content-Type': 'application/json',
        'Cookie': `${SESSION_COOKIE}=${sessionCookie}`,
        'Origin': ORIGIN,
        'X-CSRF-Token': await freshCSRF(page),
      },
      JSON.stringify({
        code: codeNow(secret, systemClock()),
        enrollmentId: firstEnrollment.enrollmentId,
      }),
    );
    expect(supersededResult).toEqual({ code: 'enrollment_invalid', status: 400 });
    steps.supersededEnrollmentRejected = true;

    // Non-ASCII digits never leave the settings form, and the server
    // rejects them as a malformed request when sent directly
    // (totp-second-factor-contract.md, "TOTP profile and code verification").
    // The decode failure spends no admitted attempt.
    stage('unicode-digits-rejected');
    const unicodeCode = toFullwidthDigits(codeNow(secret, systemClock()));
    const setupInput = page.locator(TOTP_SETUP_INPUT);
    await setupInput.fill(unicodeCode);
    await page.getByTestId('totp-code-submit').click();
    await expect(setupInput).toHaveAttribute('aria-invalid', 'true');
    await setupInput.fill('');
    const unicodeResult = await trustedRequest(
      ca,
      'PUT',
      '/api/v1/me/second-factor/totp/enrollment',
      {
        'Content-Type': 'application/json',
        'Cookie': `${SESSION_COOKIE}=${sessionCookie}`,
        'Origin': ORIGIN,
        'X-CSRF-Token': await freshCSRF(page),
      },
      JSON.stringify({
        code: unicodeCode,
        enrollmentId: secondEnrollment.enrollmentId,
      }),
    );
    expect(unicodeResult).toEqual({ code: 'request_invalid', status: 400 });
    steps.unicodeDigitsRejected = true;

    stage('enroll-complete');
    expect(await submitSetupCode(page, await freshCode(page, secret, primaryTracker)))
      .toBe(200);
    const firstCodes = await readRevealedCodes(page);
    await closeRevealAndProveCleared(page, firstCodes);
    await expect(page.getByTestId('totp-added-success')).toBeVisible();
    steps.enrolled = true;
    steps.recoveryRevealedOnce = true;

    // 4. The epoch change ended every other session and the agent grant.
    stage('revocation');
    expect(await meStatus(otherPage)).toBe(401);
    steps.otherSessionRevoked = true;
    expect(await agentToolsStatus(page, agentToken)).toBe(401);
    steps.agentRevoked = true;
    expect(await meStatus(page)).toBe(200);
    await otherContext.close();

    let state = await factorState(page);
    expect(state.enabled).toBe(true);
    expect(state.totpEnabled).toBe(true);
    expect(state.recoveryRemaining).toBe(10);

    // 5. A passkey enrolled alongside TOTP adds no recovery codes and the
    //    pending page lists passkey before TOTP before recovery. Account
    //    mutation, not a pending completion, so it spends nothing.
    stage('passkey-coexistence');
    const pool = await AuthenticatorPool.attach(context, page);
    const authPrimary = await pool.add();
    const created = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return url.origin === ORIGIN
        && url.pathname === '/api/v1/me/second-factor/passkeys'
        && candidate.request().method() === 'POST';
    }, { timeout: WAIT_RESPONSE_MS });
    await page.getByTestId('passkey-add').click();
    expect((await created).status()).toBe(201);
    await expect(page.getByTestId('passkey-added-success')).toBeVisible();
    await expect(page.getByTestId('recovery-codes-list')).toHaveCount(0);
    steps.passkeyCoexistence = true;

    // 6. Password sign-in stops at the pending page; the fixed method order
    //    is passkey, then authenticator app, then recovery code. A pending
    //    status read, not a completion, so it spends nothing.
    stage('password-pending');
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('pending');
    expect(await meStatus(page)).toBe(401);
    const order = await page.evaluate(() =>
      [...document.querySelectorAll('section[data-testid]')]
        .map((el) => el.getAttribute('data-testid'))
        .filter((id): id is string => id !== null
          && ['second-factor-passkey', 'second-factor-totp', 'second-factor-recovery']
            .includes(id)));
    expect(order).toEqual([
      'second-factor-passkey',
      'second-factor-totp',
      'second-factor-recovery',
    ]);
    steps.passwordPending = true;

    // 7. Wrong-session HTTP fixture: the pending verify route bound to one
    //    session rejects a foreign pending cookie. Pending authentication
    //    fails before admission, so it spends nothing.
    stage('wrong-session');
    const foreignSessionResult = await trustedPost(
      ca,
      '/api/v1/auth/second-factor/totp/verify',
      {
        'Content-Type': 'application/json',
        'Cookie': `${PENDING_COOKIE}=not-a-real-pending-token`,
        'Origin': ORIGIN,
        'X-CSRF-Token': 'not-a-real-token',
      },
      JSON.stringify({ code: codeNow(secret, systemClock()) }),
    );
    expect(foreignSessionResult.status).toBe(401);
    steps.wrongSessionRejected = true;

    // 8. Previous, current, and next step each complete one pending login,
    //    each on a fresh sign-in because a step only accepts a code greater
    //    than the credential's last used step. Three admitted attempts.
    //    Earlier stages used steps up to the current one, so wait until the
    //    previous step is newer than any of them.
    stage('previous-step');
    await waitForStepAtLeast(page, primaryTracker.last + 2);
    primaryTracker.last = stepAt(systemClock().nowSeconds()) - 1;
    await completeWithTotp(page, codePreviousStep(secret, systemClock()));
    await expectSignedInApp(page);
    steps.previousStepAccepted = true;

    stage('current-step');
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('pending');
    await completeWithTotp(page, await freshCode(page, secret, primaryTracker));
    await expectSignedInApp(page);
    steps.currentStepAccepted = true;

    stage('next-step');
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('pending');
    await waitForStepAtLeast(page, primaryTracker.last);
    primaryTracker.last = stepAt(systemClock().nowSeconds()) + 1;
    await completeWithTotp(page, codeNextStep(secret, systemClock()));
    await expectSignedInApp(page);
    steps.nextStepAccepted = true;

    // 9. Provider sign-in on the enrolled account also stops at pending, then
    //    completes with one admitted attempt.
    stage('provider-pending');
    await signOut(page);
    await watchingCallback(page, () => signInWithGoogle(page, {
      accountLabel: LINK_ACCOUNT_LABEL,
      fromLoginPage: true,
      returnPath: '/login/second-factor',
    }));
    await hydrated(page, WAIT_HYDRATE_MS);
    expect(await meStatus(page)).toBe(401);
    await expect(page.getByTestId('second-factor-totp')).toBeVisible();
    steps.providerPending = true;
    await completeWithTotp(page, await freshCode(page, secret, primaryTracker));
    await expectSignedInApp(page);

    // 10. Removing TOTP while a passkey remains is non-final (one admitted
    //     reauthentication attempt); removing the remaining passkey then
    //     turns enforcement off.
    stage('remove-totp');
    await gotoHydrated(page, '/app/settings/sessions');
    await forceReauthOnce(page, '/api/v1/me/second-factor/totp');
    await page.getByTestId('totp-remove').click();
    await expect(page.getByRole('alertdialog')).toBeVisible();
    await page.locator('[data-action="totp-remove-confirm"]').click();
    await expect(page.getByTestId('second-factor-reauth-password'))
      .toBeVisible();
    await reauthenticateEnrolled(page, primaryPassword, secret, primaryTracker);
    await page.getByTestId('totp-remove').click();
    await expect(page.getByRole('alertdialog')).toBeVisible();
    await page.locator('[data-action="totp-remove-confirm"]').click();
    await expect(page.getByTestId('totp-removed-success')).toBeVisible();
    state = await factorState(page);
    expect(state.enabled).toBe(true);
    expect(state.totpEnabled).toBe(false);
    steps.oneRemoved = true;

    stage('final-removal');
    await page.locator('[data-testid^="passkey-remove-"]').first().click();
    await expect(page.getByRole('alertdialog'))
      .toContainText('This is your last passkey.');
    await page.locator('[data-action="passkey-remove-confirm"]').click();
    await expect(page.getByTestId('passkey-removed-success')).toBeVisible();
    state = await factorState(page);
    expect(state).toEqual({
      enabled: false,
      recoveryRemaining: 0,
      status: 200,
      totpEnabled: false,
    });
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('session');
    primaryCleanupCode = '';
    steps.finalRemoved = true;
    // Primary's admitted attempts: 2 (enrollment) + 3 (skew) + 1 (provider
    // pending) + 1 (remove-totp reauth) = 7.

    // 11. A second fictional account carries the same-step replay and
    //     invalid-code cases against its own enrollment.
    role('replay');
    stage('replay-account');
    await signOut(page);
    await registerVerified(
      page,
      capture,
      replayEmail,
      replayPassword,
      'TOTP Replay Proof',
    );
    expect(await passwordSignIn(page, replayEmail, replayPassword))
      .toBe('session');
    const replayEnrolled = await enrollFirstTotp(
      page, replayPassword, replayTracker,
    );
    let replaySecret = replayEnrolled.secret;
    replayCleanupCode = replayEnrolled.codes[9] as string;

    // 12. A replayed code and an invalid code are both rejected as failed
    //     verification, then a valid code still completes the same pending
    //     row. Replay's admitted attempts: 1 (enrollment) + 2 (same-step
    //     replay) + 2 (invalid code) = 5.
    stage('same-step-replay');
    expect(await passwordSignIn(page, replayEmail, replayPassword))
      .toBe('pending');
    const usedCode = await freshCode(page, replaySecret, replayTracker);
    expect(await submitPendingTotpCode(page, usedCode)).toBe(204);
    await landedAfter(page, '/login/second-factor');
    await expectSignedInApp(page);
    await signOut(page);
    expect(await passwordSignIn(page, replayEmail, replayPassword))
      .toBe('pending');
    // The already-consumed step is no longer greater than last_used_step.
    expect(await submitPendingTotpCode(page, usedCode)).toBe(401);
    steps.sameStepReplayRejected = true;

    stage('invalid-code');
    expect(await submitPendingTotpCode(page, mismatchedCode(usedCode)))
      .toBe(401);
    await expect(page.getByTestId('second-factor-totp-error')).toBeVisible();
    expect(await meStatus(page)).toBe(401);
    steps.invalidCodeRejected = true;
    await completeWithTotp(page, await freshCode(page, replaySecret, replayTracker));
    await expectSignedInApp(page);

    // 13. A third fictional account carries the concurrent-submission case
    //     against its own enrollment. Concurrent's admitted attempts: 1
    //     (enrollment) + 2 (race) + 1 (browser resume) = 4.
    role('concurrent');
    stage('concurrent-account');
    await signOut(page);
    await registerVerified(
      page,
      capture,
      concurrentEmail,
      concurrentPassword,
      'TOTP Concurrent Proof',
    );
    expect(await passwordSignIn(page, concurrentEmail, concurrentPassword))
      .toBe('session');
    const concurrentEnrolled = await enrollFirstTotp(
      page, concurrentPassword, concurrentTracker,
    );
    const concurrentSecret = concurrentEnrolled.secret;
    concurrentCleanupCode = concurrentEnrolled.codes[9] as string;

    stage('concurrent-use');
    expect(await passwordSignIn(page, concurrentEmail, concurrentPassword))
      .toBe('pending');
    const concurrentHeaders = {
      'Content-Type': 'application/json',
      'Cookie': await pendingCookieHeader(context),
      'Origin': ORIGIN,
      'X-CSRF-Token': await pendingCSRFToken(page),
    };
    const concurrentBody = JSON.stringify({
      code: await freshCode(page, concurrentSecret, concurrentTracker),
    });
    const race = await Promise.all([
      trustedPost(ca, '/api/v1/auth/second-factor/totp/verify',
        concurrentHeaders, concurrentBody),
      trustedPost(ca, '/api/v1/auth/second-factor/totp/verify',
        concurrentHeaders, concurrentBody),
    ]);
    expect(race.filter((result) => result.status === 204)).toHaveLength(1);
    expect(race.filter((result) => result.status === 401)).toHaveLength(1);
    steps.concurrentUseRejected = true;
    // The race winner's session went to this test's own HTTP client, not the
    // browser, so the browser signs in again before it opens settings.
    stage('concurrent-browser-sign-in');
    expect(await passwordSignIn(page, concurrentEmail, concurrentPassword))
      .toBe('pending');
    await completeWithTotp(
      page, await freshCode(page, concurrentSecret, concurrentTracker),
    );
    await expectSignedInApp(page);

    // 14. A fourth fictional account carries replacement against its own
    //     enrollment. Replace's admitted attempts: 1 (enrollment) + 1
    //     (reauth) + 1 (replacement completion) + 1 (old-secret rejection)
    //     + 1 (new-secret login) = 5.
    role('replace');
    stage('replace-account');
    await signOut(page);
    await registerVerified(
      page,
      capture,
      replaceEmail,
      replacePassword,
      'TOTP Replace Proof',
    );
    expect(await passwordSignIn(page, replaceEmail, replacePassword))
      .toBe('session');
    const replaceEnrolled = await enrollFirstTotp(
      page, replacePassword, replaceTracker,
    );
    let replaceSecret = replaceEnrolled.secret;
    replaceCleanupCode = replaceEnrolled.codes[9] as string;

    stage('replace-settings');
    await gotoHydrated(page, '/app/settings/sessions');
    stage('replace-click');
    await forceReauthOnce(page, '/api/v1/me/second-factor/totp/enrollment');
    await page.getByTestId('totp-setup-replace').click();
    stage('replace-reauth-open');
    await expect(page.getByTestId('second-factor-reauth-password'))
      .toBeVisible();
    stage('replace-reauth-submit');
    await reauthenticateEnrolled(
      page, replacePassword, replaceSecret, replaceTracker,
    );
    // The replace notice lives in the setup dialog, which opens only after
    // the retried start succeeds.
    stage('replace-start');
    const oldReplaceSecret = replaceSecret;
    replaceSecret = await startTotpSetup(page);
    stage('replace-notice');
    await expect(page.getByTestId('totp-replace-notice')).toBeVisible();
    expect(replaceSecret).not.toBe(oldReplaceSecret);
    stage('replace-code');
    expect(await submitSetupCode(
      page, await freshCode(page, replaceSecret, replaceTracker),
    )).toBe(200);
    stage('replace-complete');
    await expect(page.getByTestId('totp-replaced-success')).toBeVisible();
    await expect(page.getByTestId('recovery-codes-list')).toHaveCount(0);
    steps.replaced = true;

    stage('replace-old-secret-rejected');
    await signOut(page);
    expect(await passwordSignIn(page, replaceEmail, replacePassword))
      .toBe('pending');
    expect(await submitPendingTotpCode(
      page, codeNow(oldReplaceSecret, systemClock()),
    )).toBe(401);
    stage('replace-new-secret-login');
    await completeWithTotp(
      page, await freshCode(page, replaceSecret, replaceTracker),
    );
    await expectSignedInApp(page);

    // 15. A fifth fictional account carries the wrong-epoch fixture against
    //     its own enrollment. Epoch's admitted attempts: 1 (enrollment) + 1
    //     (reauth) + 1 (bump completion) + 1 (stale attempt) = 4.
    role('epoch');
    stage('epoch-account');
    await signOut(page);
    await registerVerified(
      page,
      capture,
      epochEmail,
      epochPassword,
      'TOTP Epoch Proof',
    );
    expect(await passwordSignIn(page, epochEmail, epochPassword))
      .toBe('session');
    const epochEnrolled = await enrollFirstTotp(
      page, epochPassword, epochTracker,
    );
    let epochSecret = epochEnrolled.secret;
    epochCleanupCode = epochEnrolled.codes[9] as string;

    stage('wrong-epoch-setup');
    const staleContext = await browser.newContext();
    extraContexts.push(staleContext);
    await setLocale(staleContext, 'en');
    await installExternalRequestFirewall(staleContext, counters);
    const stalePage = await staleContext.newPage();
    attach(stalePage);
    expect(await passwordSignIn(stalePage, epochEmail, epochPassword))
      .toBe('pending');
    const staleCookie = await pendingCookieHeader(staleContext);
    const staleCsrf = await pendingCSRFToken(stalePage);

    stage('wrong-epoch-bump');
    await gotoHydrated(page, '/app/settings/sessions');
    await forceReauthOnce(page, '/api/v1/me/second-factor/totp/enrollment');
    await page.getByTestId('totp-setup-replace').click();
    await reauthenticateEnrolled(page, epochPassword, epochSecret, epochTracker);
    const bumpSecret = await startTotpSetup(page);
    expect(await submitSetupCode(
      page, await freshCode(page, bumpSecret, epochTracker),
    )).toBe(200);
    await expect(page.getByTestId('totp-replaced-success')).toBeVisible();
    epochSecret = bumpSecret;

    stage('wrong-epoch-assert');
    const staleAttempt = await trustedPost(
      ca,
      '/api/v1/auth/second-factor/totp/verify',
      {
        'Content-Type': 'application/json',
        'Cookie': staleCookie,
        'Origin': ORIGIN,
        'X-CSRF-Token': staleCsrf,
      },
      JSON.stringify({ code: codeNow(epochSecret, systemClock()) }),
    );
    expect(staleAttempt.status).toBe(401);
    steps.wrongEpochRejected = true;
    await staleContext.close();

    // 16. A sixth fictional account carries both locales and the
    //     password-reset preservation case against its own enrollment.
    //     Locale's admitted attempts: 1 (enrollment) + 2 (locales) + 1
    //     (post-reset completion) = 4.
    role('locale');
    stage('locale-account');
    await signOut(page);
    await registerVerified(
      page,
      capture,
      localeEmail,
      localePassword,
      'TOTP Locale Proof',
    );
    expect(await passwordSignIn(page, localeEmail, localePassword))
      .toBe('session');
    const localeEnrolled = await enrollFirstTotp(
      page, localePassword, localeTracker,
    );
    const localeSecret = localeEnrolled.secret;
    localeCleanupCode = localeEnrolled.codes[9] as string;

    stage('locales');
    await signOut(page);
    await provePendingLocale(page, context, {
      code: await freshCode(page, localeSecret, localeTracker),
      email: localeEmail,
      locale: 'vi',
      password: localePassword,
      viewport: PHONE,
    });
    await provePendingLocale(page, context, {
      code: await freshCode(page, localeSecret, localeTracker),
      email: localeEmail,
      locale: 'en',
      password: localePassword,
      viewport: DESKTOP,
    });
    steps.locales = true;
    steps.viewports = true;

    // 17. A password reset revokes sessions but preserves enforcement.
    stage('password-reset');
    const resetPassword = runPassword();
    await gotoHydrated(page, '/forgot-password');
    await page.getByLabel('Email').fill(localeEmail);
    await page.getByRole('button', { name: 'Send reset link' }).click();
    await expect(page.getByTestId('forgot-success')).toBeVisible();
    const resetToken = await capture.waitForToken('reset', localeEmail);
    await gotoFirstVisit(page, `${ORIGIN}/reset-password#token=${resetToken}`);
    await page.getByLabel('New password', { exact: true }).fill(resetPassword);
    await page.getByLabel('Confirm password', { exact: true })
      .fill(resetPassword);
    await page.getByRole('button', { name: 'Reset password' }).click();
    await expect(page.getByTestId('reset-success')).toBeVisible();
    localeFinalPassword = resetPassword;
    expect(await passwordSignIn(page, localeEmail, resetPassword))
      .toBe('pending');
    steps.resetPreservesEnforcement = true;
    await completeWithTotp(page, await freshCode(page, localeSecret, localeTracker));
    await expectSignedInApp(page);

    // 18. A seventh fictional account carries the recovery-completion case,
    //     which needs a still-enrolled TOTP credential to remain. Recovery's
    //     admitted attempts: 1 (enrollment) + 1 (recovery completion) = 2.
    role('recovery');
    stage('recovery-account');
    await signOut(page);
    await registerVerified(
      page,
      capture,
      recoveryEmail,
      recoveryPassword,
      'TOTP Recovery Proof',
    );
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('session');
    const recoveryEnrolled = await enrollFirstTotp(
      page, recoveryPassword, recoveryTracker,
    );
    recoveryCleanupCode = recoveryEnrolled.codes[9] as string;

    stage('recovery-completion');
    await signOut(page);
    expect(await passwordSignIn(page, recoveryEmail, recoveryPassword))
      .toBe('pending');
    await completeWithRecovery(page, recoveryEnrolled.codes[0] as string);
    await expectSignedInApp(page);
    steps.recoveryCompletion = true;

    // 19. An eighth fictional account carries attempt exhaustion, whose five
    //     failures would otherwise spend another account's whole budget.
    //     Attempts' admitted attempts: 1 (enrollment) + 5 (exhaustion) = 6.
    role('attempts');
    stage('attempts-account');
    await gotoHydrated(page, '/login');
    await registerVerified(
      page,
      capture,
      attemptsEmail,
      attemptsPassword,
      'TOTP Attempts Proof',
    );
    expect(await passwordSignIn(page, attemptsEmail, attemptsPassword))
      .toBe('session');
    const attemptsEnrolled = await enrollFirstTotp(
      page, attemptsPassword, attemptsTracker,
    );
    attemptsCleanupCode = attemptsEnrolled.codes[9] as string;

    stage('attempts-exhausted');
    await signOut(page);
    expect(await passwordSignIn(page, attemptsEmail, attemptsPassword))
      .toBe('pending');
    for (let attempt = 1; attempt <= 5; attempt += 1) {
      expect(await submitRecoveryCode(
        page,
        fabricatedRecoveryCode(attemptsEnrolled.codes),
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
    // Snapshot the page before teardown navigates away from the failure.
    try {
      failureSeen = await visibleWords(page);
      failurePage = landingCategory(page.url());
    } catch {
      // A closed page leaves the defaults.
    }
    beginTeardown();
    const removed = [
      await deleteAccount(page, {
        email: primaryEmail,
        password: primaryPassword,
        recoveryCode: primaryCleanupCode,
      }),
      await deleteAccount(page, {
        email: replayEmail,
        password: replayPassword,
        recoveryCode: replayCleanupCode,
      }),
      await deleteAccount(page, {
        email: concurrentEmail,
        password: concurrentPassword,
        recoveryCode: concurrentCleanupCode,
      }),
      await deleteAccount(page, {
        email: replaceEmail,
        password: replacePassword,
        recoveryCode: replaceCleanupCode,
      }),
      await deleteAccount(page, {
        email: epochEmail,
        password: epochPassword,
        recoveryCode: epochCleanupCode,
      }),
      await deleteAccount(page, {
        email: localeEmail,
        password: localeFinalPassword,
        recoveryCode: localeCleanupCode,
      }),
      await deleteAccount(page, {
        email: recoveryEmail,
        password: recoveryPassword,
        recoveryCode: recoveryCleanupCode,
      }),
      await deleteAccount(page, {
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
        scenario: 'totp-second-factor',
        schemaVersion: 1,
        steps,
      }, null, 2)}\n`,
      { flag: 'wx', mode: 0o600 },
    );
    failOnUnexpectedConsole(steps.attemptsExhausted);
  }
});

// --- Disabled-enrollment journey -----------------------------------------

test('proves disabled TOTP enrollment answers as an unregistered route',
  async ({ context, page }) => {
    test.skip(MODE !== 'totp-disabled', 'disabled enrollment mode only');

    const steps = {
      capabilityClosed: false,
      cleanup: false,
      completionNotFound: false,
      enrollmentHidden: false,
      locales: false,
      passkeyStillWorks: false,
      recoveryStillWorks: false,
      removalStillWorks: false,
      startNotFound: false,
      stateAvailable: false,
      unregisteredRouteMatches: false,
      viewports: false,
    };

    const counters = newDiagnosticCounters();
    const attach = pageDiagnosticsAttacher(counters, {
      countConsoleError: isUnexpectedTotpConsole,
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
          data?: { totpEnrollment?: unknown };
        };
        return { status: response.status, totpEnrollment: body.data?.totpEnrollment };
      });
      expect(capability).toEqual({ status: 200, totpEnrollment: false });
      steps.capabilityClosed = true;

      stage('disabled-settings');
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('totp-settings')).toBeVisible();
      await expect(page.getByTestId('totp-status')).toContainText('Not set up.');
      await expect(page.getByTestId('totp-setup-start')).toHaveCount(0);
      await expect(page.getByTestId('totp-setup-replace')).toHaveCount(0);
      steps.enrollmentHidden = true;

      stage('disabled-state');
      const state = await factorState(page);
      expect(state).toEqual({
        enabled: false,
        recoveryRemaining: 0,
        status: 200,
        totpEnabled: false,
      });
      steps.stateAvailable = true;

      stage('disabled-routes');
      const csrf = await freshCSRF(page);
      const probes = await probeDisabledRoutes(page, csrf);
      expect(probes.start).toEqual({ code: 'not_found', status: 404 });
      steps.startNotFound = true;
      expect(probes.complete).toEqual({ code: 'not_found', status: 404 });
      steps.completionNotFound = true;
      expect(probes.unregistered).toEqual(probes.start);
      steps.unregisteredRouteMatches = true;
      expect(probes.removal).toEqual({ code: 'factor_not_found', status: 404 });
      steps.removalStillWorks = true;
      expect(probes.totpVerify)
        .toEqual({ code: 'authentication_required', status: 401 });
      expect(probes.recovery)
        .toEqual({ code: 'authentication_required', status: 401 });
      steps.recoveryStillWorks = true;
      // Passkey enrollment stays on, and this fresh sign-in is recent
      // primary proof for a first factor, so options are issued.
      expect(probes.passkeyOptions.status).toBe(200);
      steps.passkeyStillWorks = true;

      stage('disabled-locales');
      await setLocale(context, 'vi');
      await page.setViewportSize(PHONE);
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('totp-status')).toContainText('Chưa thiết lập.');
      await expect(page.getByTestId('totp-setup-start')).toHaveCount(0);
      expect(await page.evaluate(() =>
        document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await setLocale(context, 'en');
      await page.setViewportSize(DESKTOP);
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('totp-status')).toContainText('Not set up.');
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
          scenario: 'totp-enrollment-disabled',
          schemaVersion: 1,
          steps,
        }, null, 2)}\n`,
        { flag: 'wx', mode: 0o600 },
      );
      failOnUnexpectedConsole(steps.locales);
    }
  });

// --- Shared journey helpers ------------------------------------------------

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
  const { createHash, randomUUID } = await import('node:crypto');
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
 * page entirely by keyboard through the authenticator-app field, checking
 * the localized copy, the phishing guidance, the accessible names, the
 * focus order, and that nothing overflows the viewport.
 */
async function provePendingLocale(
  page: Page,
  context: BrowserContext,
  options: PendingLocaleCase,
): Promise<void> {
  const vietnamese = options.locale === 'vi';
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
  await expect(page.getByTestId('second-factor-totp')).toContainText(
    vietnamese
      ? 'Chỉ nhập mã này trên aboutme.vn'
      : 'Only enter this code on aboutme.vn',
  );
  const totpInput = page.locator(TOTP_LOGIN_INPUT);
  await totpInput.focus();
  await expect(totpInput).toBeFocused();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await totpInput.fill(options.code);
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.origin === ORIGIN
      && url.pathname === '/api/v1/auth/second-factor/totp/verify';
  }, { timeout: WAIT_RESPONSE_MS });
  await page.keyboard.press('Enter');
  expect((await response).status()).toBe(204);
  await landedAfter(page, '/login/second-factor');
  await expectSignedInApp(page);
}

interface DisabledProbes {
  readonly complete: TrustedResult;
  readonly passkeyOptions: TrustedResult;
  readonly recovery: TrustedResult;
  readonly removal: TrustedResult;
  readonly start: TrustedResult;
  readonly totpVerify: TrustedResult;
  readonly unregistered: TrustedResult;
}

/**
 * Probes every TOTP route from the signed-in page while enrollment is off:
 * the two enrollment routes must match a never-registered path, and the
 * flag-independent routes must answer with their own codes.
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
    return {
      complete: await call(
        '/api/v1/me/second-factor/totp/enrollment',
        'PUT',
        JSON.stringify({ code: '000000', enrollmentId: 'A'.repeat(43) }),
      ),
      passkeyOptions: await call(
        '/api/v1/me/second-factor/passkeys/options', 'POST', '{}'),
      recovery: await call(
        '/api/v1/auth/second-factor/recovery/verify',
        'POST',
        JSON.stringify({ code: 'amr_00000000000000000000000000' }),
      ),
      removal: await call('/api/v1/me/second-factor/totp', 'DELETE', null),
      start: await call(
        '/api/v1/me/second-factor/totp/enrollment', 'POST', '{}'),
      totpVerify: await call(
        '/api/v1/auth/second-factor/totp/verify',
        'POST',
        JSON.stringify({ code: '000000' }),
      ),
      unregistered: await call(
        '/api/v1/me/second-factor/unregistered/enrollment', 'POST', '{}'),
    };
  }, csrf);
}

// --- Virtual authenticators (passkey coexistence only) ---------------------

type CDPSend = (
  method: string,
  params?: Record<string, unknown>,
) => Promise<Record<string, unknown>>;

const WAIT_CDP_MS = 30_000;

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

/** One virtual authenticator, used only to prove TOTP and passkeys coexist. */
class AuthenticatorPool {
  private constructor(private readonly send: CDPSend) {}

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

  async add(): Promise<string> {
    const result = await this.send('WebAuthn.addVirtualAuthenticator', {
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
}

// --- Teardown ---------------------------------------------------------------

interface AccountTeardown {
  readonly email: string;
  readonly password: string;
  /** An unused recovery code, when the account is still enrolled. */
  readonly recoveryCode: string;
}

async function deleteAccount(
  page: Page,
  account: AccountTeardown,
): Promise<boolean> {
  if (account.email === '') return true;
  try {
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
