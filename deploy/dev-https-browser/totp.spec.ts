import {
  expect,
  test,
  type Browser,
  type BrowserContext,
  type ConsoleMessage,
  type Page,
  type Request,
  type Route,
} from '@playwright/test';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { freshCSRF } from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  locatorStateWord,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pageStateWords,
  type DiagnosticCounters,
  signInWithGoogle,
} from './harness-lib';
import {
  httpFailureStatus,
  isExpectedNegativeHTTPConsole,
} from './network-policy';
import { TOTP_SHARD_ROLES } from './proof-shards.mjs';
import {
  activateRoles,
  AuthenticatorPool,
  CA_PATH,
  callbackCategory,
  CAPTURE_TOKEN_PATH,
  captureClient,
  configureShardList,
  cookieValue,
  DESKTOP,
  DISABLED_ACCOUNT_LABEL,
  fabricatedRecoveryCode,
  failOnUnexpectedConsole,
  landedAfter,
  landingCategory,
  LINK_ACCOUNT_LABEL,
  ORIGIN,
  outcomeOf,
  PENDING_COOKIE,
  pendingCookieHeader,
  pendingCSRFToken,
  PHONE,
  beginTeardown as recordTeardown,
  recordedRole,
  recordedStage,
  role as recordRole,
  recordUnexpectedConsole,
  runPassword,
  runsRole,
  SESSION_COOKIE,
  stage,
  tearingDown,
  type TrustedResult,
  trustedPost,
  trustedRequest,
  WAIT_HYDRATE_MS,
  WAIT_NAVIGATION_MS,
  WAIT_RESPONSE_MS,
  watchingCallback,
} from './second-factor-lib';
import {
  agentToolsStatus,
  closeRevealAndProveCleared,
  completeWithRecovery,
  createAgentGrant,
  deleteAccount,
  deleteSignedInAccount,
  DISABLED_WARM_ROUTES,
  expectSignedInApp,
  forceReauthOnce,
  gotoFirstVisit,
  gotoHydrated,
  hydrated,
  meStatus,
  passwordSignIn,
  type PendingLocaleCase,
  readRevealedCodes,
  readSecondFactorAccounts,
  reauthenticateWithPassword,
  setLocale,
  signOut,
  SIGNED_IN_WARM_ROUTES,
  submitRecoveryCode,
  WARM_ROUTES,
  warmRoutes,
} from './second-factor-pages';
import {
  codeForStep,
  codeNextStep,
  codeNow,
  codePreviousStep,
  mismatchedCode,
  newSectionTimer,
  stepAt,
  systemClock,
  toFullwidthDigits,
  TOTP_PERIOD_SECONDS,
} from './totp-fixture';

const MODE = process.env.ABOUTME_BROWSER_MODE ?? 'totp';
const ENABLED_EVIDENCE_NAME = 'totp-second-factor-proof.json';
const DISABLED_EVIDENCE_PATH = '/evidence/totp-enrollment-disabled-proof.json';
const ENABLED_TIMING_NAME = 'totp-timing.json';
const TOTP_LOGIN_INPUT = '#second-factor-totp-code';
const TOTP_SETUP_INPUT = '#totp-code';

const CLEANUP_LANDING_MS = 15_000;

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

type AccountRole =
  | 'none'
  | 'primary'
  | 'skew'
  | 'replay'
  | 'concurrent'
  | 'replace'
  | 'epoch'
  | 'locale'
  | 'recovery'
  | 'attempts'
  | 'disabled';

// --- Enabled-proof sharding --------------------------------------------------
//
// CI splits the enabled journey into six shards (proof-shards.mjs, shared
// with the coverage check) and runs several in one job: ABOUTME_TOTP_SHARD
// (read by run.sh from the host environment) lists them, comma-separated.
// Each listed shard is its own test in its own Playwright worker, and all of
// them run at the same time against one harness. Each proves only its own
// fictional accounts, and each writes its evidence to /evidence/<shard>/.
// Unset (every local run) keeps one test that proves every role and writes
// its evidence to /evidence itself. `primary` alone carries most of the
// account's own step count, so it is its own shard.
const SHARDS = configureShardList(
  MODE, 'ABOUTME_TOTP_SHARD', TOTP_SHARD_ROLES,
);
if (SHARDS !== null && SHARDS.length > 1) {
  test.describe.configure({ mode: 'parallel' });
}

const timer = newSectionTimer();

function role(next: AccountRole): void {
  if (tearingDown) return;
  timer.enter(next);
  recordRole(next);
}

function beginTeardown(): void {
  timer.enter('cleanup');
  recordTeardown();
}

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

function waitedFor(message: string): string {
  const match = /getByTestId\('([a-z0-9-]+)'\)/u.exec(message);
  if (match === null) return 'none';
  const known = DIAG_TEST_IDS.find(([id]) => id === match[1]);
  return known === undefined ? 'unlisted' : known[1];
}

let failureSeen = 'none';
let failurePage = 'unknown';
let failurePageState = 'page-unread';

async function visibleWords(page: Page): Promise<string> {
  const words: string[] = [];
  for (const [id, word] of DIAG_TEST_IDS) {
    try {
      if (await page.getByTestId(id).first().isVisible()) words.push(word);
    } catch {
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
  const message = testInfo.error?.message ?? '';
  const errorKind = /strict mode violation/u.test(message) ? 'strict'
    : /waitForResponse/u.test(message) ? 'response-wait'
      : /toBe/u.test(message) ? 'status-mismatch' : 'other';
  console.log(
    `${MODE}-stage:fail-${outcome}-at-${recordedStage}-for-${recordedRole}`
      + `-verify-${verifyTrace}-error-${errorKind}`
      + `-waited-${waitedFor(message)}-page-${failurePage}`
      + `-seen-${failureSeen}-${failurePageState}-${locatorStateWord(message)}`,
  );
});

function isUnexpectedTotpConsole(message: ConsoleMessage): boolean {
  const unexpected = classifyTotpConsole(message);
  if (unexpected !== null) recordUnexpectedConsole(unexpected);
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

// A settings mutation that replaces the session, such as TOTP removal, sets
// a new session cookie with a new CSRF secret, and the page keeps the old
// token until its own /api/v1/me refetch lands (sessions.vue
// onSecondFactorChanged). A mutation sent before then gets 403
// csrf_rejected, and useAuth's mutate refreshes and retries once, which the
// console check counts as unexpected. The refetch is held for
// TOKEN_REFETCH_HOLD_MS, so a next step that does not wait for it always
// sends the stale token and fails that check instead of failing only when
// the runner is slow.
const TOKEN_REFETCH_HOLD_MS = 1_000;

async function holdTokenRefetch(page: Page): Promise<() => Promise<void>> {
  await page.route(`${ORIGIN}/api/v1/me`, async (route: Route) => {
    await new Promise((resolve) => {
      setTimeout(resolve, TOKEN_REFETCH_HOLD_MS);
    });
    await route.continue().catch(() => undefined);
  }, { times: 1 });
  const refetched = page.waitForResponse(
    (response) => response.url() === `${ORIGIN}/api/v1/me`
      && response.request().method() === 'GET',
    { timeout: WAIT_RESPONSE_MS + TOKEN_REFETCH_HOLD_MS },
  ).then(async (response) => {
    await response.finished();
  });
  refetched.catch(() => undefined);
  return () => refetched;
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
  await page.getByTestId('second-factor-totp-form')
    .locator('button[type="submit"]').click();
  return (await response).status();
}

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

async function readSetupSecret(page: Page): Promise<string> {
  await expect(page.getByTestId('totp-setup-dialog')).toBeVisible();
  await expect(page.getByTestId('totp-qr')).toBeVisible();
  const grouped = (await page.getByTestId('totp-secret').textContent()) ?? '';
  const secret = grouped.replace(/\s+/gu, '');
  expect(secret).toMatch(/^[A-Z2-7]{32}$/u);
  return secret;
}

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

interface StepTracker { last: number }

function newStepTracker(): StepTracker {
  return { last: -1 };
}

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

async function waitForStepAtLeast(page: Page, step: number): Promise<void> {
  const target = step * TOTP_PERIOD_SECONDS;
  const waitMs = (target - Date.now() / 1000) * 1000;
  if (waitMs > 0) await page.waitForTimeout(Math.ceil(waitMs) + 500);
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

async function startTotpSetup(page: Page): Promise<string> {
  return (await startTotpSetupCapturing(page)).secret;
}

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

const ENABLED_TITLE = 'proves the authenticator-app second factor over native HTTPS';
const SHARD_ROLES: Readonly<Record<string, readonly string[]>> = TOTP_SHARD_ROLES;

for (const shard of SHARDS ?? [null]) {
  test(shard === null ? ENABLED_TITLE : `${ENABLED_TITLE} (${shard})`, async ({
    browser,
    context,
    page,
  }) => {
    test.skip(MODE !== 'totp', 'enabled enrollment mode only');
    await provesEnabledJourney(browser, context, page, shard);
  });
}

async function provesEnabledJourney(
  browser: Browser,
  context: BrowserContext,
  page: Page,
  shard: string | null,
): Promise<void> {
  const activeRoles = shard === null
    ? null
    : new Set(SHARD_ROLES[shard]);
  activateRoles(activeRoles);
  const evidenceDir = shard === null ? '/evidence' : `/evidence/${shard}`;
  if (shard !== null) await mkdir(evidenceDir, { mode: 0o700 });

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
  let registrations = 0;
  await context.route(`${ORIGIN}/api/v1/auth/password/register`, async (route) => {
    registrations += 1;
    await route.abort();
  });
  await setLocale(context, 'en');
  await page.setViewportSize(DESKTOP);

  await warmRoutes(page, WARM_ROUTES, counters);

  // The host clears captured mail before the run. Parallel shards share the
  // capture and read only mail sent to their own addresses, so none clears
  // it again under another shard's pending mail.
  stage('capture-open');
  const ca = await readFile(CA_PATH);
  const capture = captureClient(
    (await readFile(CAPTURE_TOKEN_PATH, 'utf8')).trim(),
  );

  const accountFor = await readSecondFactorAccounts('totp');
  const { email: primaryEmail, password: primaryPassword } = accountFor('primary');
  const { email: skewEmail, password: skewPassword } = accountFor('skew');
  const { email: replayEmail, password: replayPassword } = accountFor('replay');
  const { email: concurrentEmail, password: concurrentPassword } = accountFor('concurrent');
  const { email: replaceEmail, password: replacePassword } = accountFor('replace');
  const { email: epochEmail, password: epochPassword } = accountFor('epoch');
  const { email: localeEmail, password: localePassword } = accountFor('locale');
  const { email: recoveryEmail, password: recoveryPassword } = accountFor('recovery');
  const { email: attemptsEmail, password: attemptsPassword } = accountFor('attempts');

  const primaryTracker = newStepTracker();
  const skewTracker = newStepTracker();
  const replayTracker = newStepTracker();
  const concurrentTracker = newStepTracker();
  const replaceTracker = newStepTracker();
  const epochTracker = newStepTracker();
  const localeTracker = newStepTracker();
  const recoveryTracker = newStepTracker();
  const attemptsTracker = newStepTracker();

  let primaryCleanupCode = '';
  let skewCleanupCode = '';
  let replayCleanupCode = '';
  let concurrentCleanupCode = '';
  let replaceCleanupCode = '';
  let epochCleanupCode = '';
  let localeFinalPassword = localePassword;
  let localeCleanupCode = '';
  let recoveryCleanupCode = '';
  let attemptsCleanupCode = '';
  const extraContexts: BrowserContext[] = [];
  const visitedAccounts: Partial<Record<AccountRole, true>> = {};

  try {
    if (runsRole('primary')) {
    role('primary');
    stage('primary-account');
    visitedAccounts.primary = true;
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
    const enrollCode = await freshCode(page, secret, primaryTracker);
    const enrolledTokenRefetched = await holdTokenRefetch(page);
    expect(await submitSetupCode(page, enrollCode)).toBe(200);
    const firstCodes = await readRevealedCodes(page);
    await closeRevealAndProveCleared(page, firstCodes);
    await expect(page.getByTestId('totp-added-success')).toBeVisible();
    await enrolledTokenRefetched();
    steps.enrolled = true;
    steps.recoveryRevealedOnce = true;

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

    stage('password-pending');
    await signOut(page);
    expect(await passwordSignIn(page, primaryEmail, primaryPassword))
      .toBe('pending');
    expect(await meStatus(page)).toBe(401);
    await expect(page.getByTestId('second-factor-passkey')).toBeVisible();
    await expect(page.getByTestId('second-factor-totp')).toBeVisible();
    await expect(page.getByTestId('second-factor-recovery')).toBeVisible();
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
    const tokenRefetched = await holdTokenRefetch(page);
    await page.locator('[data-action="totp-remove-confirm"]').click();
    await expect(page.getByTestId('totp-removed-success')).toBeVisible();
    state = await factorState(page);
    expect(state.enabled).toBe(true);
    expect(state.totpEnabled).toBe(false);
    steps.oneRemoved = true;

    stage('final-removal');
    await tokenRefetched();
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
    }

    if (runsRole('skew')) {
    role('skew');
    stage('skew-account');
    await signOut(page);
    visitedAccounts.skew = true;
    expect(await passwordSignIn(page, skewEmail, skewPassword))
      .toBe('session');
    const skewEnrolled = await enrollFirstTotp(
      page, skewPassword, skewTracker,
    );
    const skewSecret = skewEnrolled.secret;
    skewCleanupCode = skewEnrolled.codes[9] as string;

    stage('previous-step');
    await signOut(page);
    expect(await passwordSignIn(page, skewEmail, skewPassword))
      .toBe('pending');
    await waitForStepAtLeast(page, skewTracker.last + 2);
    skewTracker.last = stepAt(systemClock().nowSeconds()) - 1;
    await completeWithTotp(page, codePreviousStep(skewSecret, systemClock()));
    await expectSignedInApp(page);
    steps.previousStepAccepted = true;

    stage('current-step');
    await signOut(page);
    expect(await passwordSignIn(page, skewEmail, skewPassword))
      .toBe('pending');
    await completeWithTotp(page, await freshCode(page, skewSecret, skewTracker));
    await expectSignedInApp(page);
    steps.currentStepAccepted = true;

    stage('next-step');
    await signOut(page);
    expect(await passwordSignIn(page, skewEmail, skewPassword))
      .toBe('pending');
    await waitForStepAtLeast(page, skewTracker.last);
    skewTracker.last = stepAt(systemClock().nowSeconds()) + 1;
    await completeWithTotp(page, codeNextStep(skewSecret, systemClock()));
    await expectSignedInApp(page);
    steps.nextStepAccepted = true;
    }

    if (runsRole('replay')) {
    role('replay');
    stage('replay-account');
    await signOut(page);
    visitedAccounts.replay = true;
    expect(await passwordSignIn(page, replayEmail, replayPassword))
      .toBe('session');
    const replayEnrolled = await enrollFirstTotp(
      page, replayPassword, replayTracker,
    );
    let replaySecret = replayEnrolled.secret;
    replayCleanupCode = replayEnrolled.codes[9] as string;

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
    }

    if (runsRole('concurrent')) {
    role('concurrent');
    stage('concurrent-account');
    await signOut(page);
    visitedAccounts.concurrent = true;
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
    stage('concurrent-browser-sign-in');
    expect(await passwordSignIn(page, concurrentEmail, concurrentPassword))
      .toBe('pending');
    await completeWithTotp(
      page, await freshCode(page, concurrentSecret, concurrentTracker),
    );
    await expectSignedInApp(page);
    }

    if (runsRole('replace')) {
    role('replace');
    stage('replace-account');
    await signOut(page);
    visitedAccounts.replace = true;
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
    }

    if (runsRole('epoch')) {
    role('epoch');
    stage('epoch-account');
    await signOut(page);
    visitedAccounts.epoch = true;
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
    }

    if (runsRole('locale')) {
    role('locale');
    stage('locale-account');
    await signOut(page);
    visitedAccounts.locale = true;
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
    }

    if (runsRole('recovery')) {
    role('recovery');
    stage('recovery-account');
    await signOut(page);
    visitedAccounts.recovery = true;
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
    }

    if (runsRole('attempts')) {
    role('attempts');
    stage('attempts-account');
    await gotoHydrated(page, '/login');
    visitedAccounts.attempts = true;
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
    }
    expect(registrations).toBe(0);
  } finally {
    try {
      failureSeen = await visibleWords(page);
      failurePage = landingCategory(page.url());
      failurePageState = await pageStateWords(page);
    } catch {
    }
    beginTeardown();
    const removed: boolean[] = [];
    if (visitedAccounts.primary) {
      removed.push(await deleteAccount(page, {
        email: primaryEmail,
        password: primaryPassword,
        recoveryCode: primaryCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.skew) {
      removed.push(await deleteAccount(page, {
        email: skewEmail,
        password: skewPassword,
        recoveryCode: skewCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.replay) {
      removed.push(await deleteAccount(page, {
        email: replayEmail,
        password: replayPassword,
        recoveryCode: replayCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.concurrent) {
      removed.push(await deleteAccount(page, {
        email: concurrentEmail,
        password: concurrentPassword,
        recoveryCode: concurrentCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.replace) {
      removed.push(await deleteAccount(page, {
        email: replaceEmail,
        password: replacePassword,
        recoveryCode: replaceCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.epoch) {
      removed.push(await deleteAccount(page, {
        email: epochEmail,
        password: epochPassword,
        recoveryCode: epochCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.locale) {
      removed.push(await deleteAccount(page, {
        email: localeEmail,
        password: localeFinalPassword,
        recoveryCode: localeCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.recovery) {
      removed.push(await deleteAccount(page, {
        email: recoveryEmail,
        password: recoveryPassword,
        recoveryCode: recoveryCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    if (visitedAccounts.attempts) {
      removed.push(await deleteAccount(page, {
        email: attemptsEmail,
        password: attemptsPassword,
        recoveryCode: attemptsCleanupCode,
      }, CLEANUP_LANDING_MS));
    }
    steps.cleanup = removed.length > 0 && removed.every(Boolean);
    for (const extra of extraContexts) {
      await extra.close().catch(() => undefined);
    }
    await writeFile(
      `${evidenceDir}/${ENABLED_TIMING_NAME}`,
      `${JSON.stringify({ schemaVersion: 1, sections: timer.finish() }, null, 2)}\n`,
      { flag: 'wx', mode: 0o600 },
    );
    const provenSteps = Object.fromEntries(
      Object.entries(steps).filter(([, proved]) => proved === true),
    );
    await writeFile(
      `${evidenceDir}/${ENABLED_EVIDENCE_NAME}`,
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
        steps: provenSteps,
      }, null, 2)}\n`,
      { flag: 'wx', mode: 0o600 },
    );
    const journeyDone = activeRoles === null || activeRoles.has('attempts')
      ? steps.attemptsExhausted
      : activeRoles.has('recovery') ? steps.recoveryCompletion
        : activeRoles.has('concurrent') ? steps.concurrentUseRejected
          : activeRoles.has('epoch') ? steps.wrongEpochRejected
            : activeRoles.has('skew') ? steps.nextStepAccepted
              : steps.finalRemoved;
    failOnUnexpectedConsole(journeyDone);
  }
}

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
      expect(probes.passkeyOptions.status).toBe(200);
      steps.passkeyStillWorks = true;

      stage('disabled-locales');
      await setLocale(context, 'vi');
      await page.setViewportSize(PHONE);
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('totp-status')).toContainText('Chưa thiết lập.');
      await expect(page.getByTestId('totp-setup-start')).toHaveCount(0);
      await expect(page.getByTestId('account-menu')).toBeVisible();
      expect(await page.evaluate(() =>
        document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await setLocale(context, 'en');
      await page.setViewportSize(DESKTOP);
      await gotoHydrated(page, '/app/settings/sessions');
      await expect(page.getByTestId('totp-status')).toContainText('Not set up.');
      steps.locales = true;
      steps.viewports = true;
    } finally {
      try {
        failureSeen = await visibleWords(page);
        failurePage = landingCategory(page.url());
        failurePageState = await pageStateWords(page);
      } catch {
      }
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
