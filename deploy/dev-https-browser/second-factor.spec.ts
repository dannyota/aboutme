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
import { randomBytes } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { freshCSRF } from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  locatorStateWord,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pageStateWords,
  signInWithGoogle,
} from './harness-lib';
import {
  httpFailureStatus,
  isExpectedNegativeHTTPConsole,
} from './network-policy';
import { PASSKEY_SHARD_ROLES } from './proof-shards.mjs';
import {
  AuthenticatorPool,
  beginTeardown,
  CA_PATH,
  callbackCategory,
  canonicalRecovery,
  CAPTURE_TOKEN_PATH,
  captureClient,
  configureProof,
  cookieValue,
  DESKTOP,
  DISABLED_ACCOUNT_LABEL,
  expectLanding,
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
  recordedRole,
  recordedStage,
  recordUnexpectedConsole,
  role,
  runPassword,
  runsRole,
  stage,
  type TrustedResult,
  trustedPost,
  WAIT_HYDRATE_MS,
  WAIT_NAVIGATION_MS,
  WAIT_RESPONSE_MS,
  watchingCallback,
} from './second-factor-lib';
import {
  type AccountTeardown,
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
  reauthenticateWithPassword,
  registerVerified,
  setLocale,
  signOut,
  SIGNED_IN_WARM_ROUTES,
  submitRecoveryCode,
  WARM_ROUTES,
  warmRoutes,
} from './second-factor-pages';

const MODE = process.env.ABOUTME_BROWSER_MODE ?? 'second-factor';
const ENABLED_EVIDENCE_PATH = '/evidence/passkey-second-factor-proof.json';
const DISABLED_EVIDENCE_PATH
  = '/evidence/passkey-enrollment-disabled-proof.json';

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
  ['/api/v1/me/second-factor/unregistered/enrollment', [404]],
  ['/api/v1/auth/second-factor', [401]],
  ['/api/v1/auth/second-factor/passkey/options', [400, 401]],
  ['/api/v1/auth/second-factor/passkey/verify', [400, 401]],
  ['/api/v1/auth/second-factor/recovery/verify', [401]],
  ['/api/v1/auth/password/login', [401]],
  ['/api/v1/auth/password/reauth', [401]],
  // The revoked agent grant's tool call.
  ['/mcp', [401]],
]);
// Startup reads of the app pages the journey lands on, named apart so a 401
// says which page was still loading.
const NAMED_API_READS: ReadonlyMap<string, string> = new Map([
  ['/api/v1/resumes', 'resumes'],
  ['/api/v1/sessions', 'sessions'],
  ['/api/v1/me/agents', 'agents'],
]);

const REMOVAL_PATH = /^\/api\/v1\/me\/second-factor\/passkeys\/[^/]+$/u;

type AccountRole = 'none' | 'primary' | 'recovery' | 'attempts' | 'disabled';

// --- Enabled-proof sharding --------------------------------------------------
//
// CI runs the enabled journey as up to two parallel shards, each its own
// harness and evidence file (ABOUTME_PASSKEY_SHARD, read by run.sh from the
// host environment). Unset (every local or single-shard run) proves every
// role. The disabled-enrollment phase runs with primary-disabled, restoring
// the unsharded order (primary, then disabled) that phase's first Vietnamese
// render depends on: primary's own `locales` stage is what first renders the
// settings page in `vi` on that harness.
// The shard map itself lives in proof-shards.mjs, shared with the coverage
// check.
const ACTIVE_ROLES = configureProof(
  MODE, 'ABOUTME_PASSKEY_SHARD', PASSKEY_SHARD_ROLES,
);

// The failed page's state in closed words, read just before teardown
// navigates away from it.
let failurePageState = 'page-unread';

test.afterEach(({}, testInfo) => {
  // One spec serves both modes, so the other mode's test is always skipped.
  // Only a real failure may add a line.
  if (testInfo.status === 'skipped') return;
  if (testInfo.status === testInfo.expectedStatus) return;
  const message = testInfo.error?.message ?? '';
  const outcome = outcomeOf(testInfo.status ?? 'unknown', message);
  const detail = `${failurePageState}-${locatorStateWord(message)}`;
  console.log(
    `${MODE}-stage:fail-${outcome}-at-${recordedStage}-for-${recordedRole}-${detail}`,
  );
});

function isUnexpectedSecondFactorConsole(message: ConsoleMessage): boolean {
  const unexpected = classifySecondFactorConsole(message);
  if (unexpected !== null) recordUnexpectedConsole(unexpected);
  return unexpected !== null;
}

function classifySecondFactorConsole(message: ConsoleMessage): string | null {
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
  const allowed = EXPECTED_PAGE_FAILURES.get(url.pathname)
    ?? (REMOVAL_PATH.test(url.pathname) ? [404] : undefined);
  if (allowed !== undefined && allowed.includes(status)) return null;
  const index = [...EXPECTED_PAGE_FAILURES.keys()].indexOf(url.pathname);
  const path = index >= 0 ? `path${index}`
    : REMOVAL_PATH.test(url.pathname) ? 'removal'
      : NAMED_API_READS.get(url.pathname)
        ?? (url.pathname.startsWith('/api/') ? 'api'
          : url.pathname.startsWith('/_nuxt/') ? 'asset' : 'other');
  return `${path}-${status}`;
}

// --- Fictional run identity -----------------------------------------------

const RUN_MARKER = randomBytes(6).toString('hex');
let accountSequence = 0;

/** A fictional, per-run address in the reserved invalid top-level domain. */
function runEmail(role: string): string {
  accountSequence += 1;
  return `pk-${role}-${RUN_MARKER}-${accountSequence}@example.invalid`;
}

// --- Page helpers -----------------------------------------------------------

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

  const primaryEmail = runEmail('primary');
  const primaryPassword = runPassword();
  const recoveryEmail = runEmail('recovery');
  const recoveryPassword = runPassword();
  const attemptsEmail = runEmail('attempts');
  const attemptsPassword = runPassword();

  let authPrimary = '';
  let authRecovery = '';
  let authAttempts = '';
  let primaryFinalPassword = primaryPassword;
  let primaryCleanupCode = '';
  let recoveryCleanupCode = '';
  let attemptsCleanupCode = '';
  const extraContexts: BrowserContext[] = [];
  // Set right before each role's registerVerified call, so teardown can tell
  // a role whose account creation was never attempted (for example a shard's
  // first role failing before it gets that far) from one whose account
  // exists and needs deleting.
  const createdAccounts: Partial<Record<AccountRole, true>> = {};

  try {
    // 1. A fictional primary account with a password and a linked provider.
    if (runsRole('primary')) {
    createdAccounts.primary = true;
    authPrimary = await pool.add();
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
    }

    // 14. A second fictional account carries the recovery-code and ceremony
    //     cases, which need their own attempt budget.
    if (runsRole('recovery')) {
    createdAccounts.recovery = true;
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
    }

    // 15. A third fictional account carries attempt exhaustion, whose five
    //     failures would otherwise spend another account's whole budget.
    if (runsRole('attempts')) {
    createdAccounts.attempts = true;
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
    }
  } finally {
    // Read the page before teardown navigates away from a failure.
    failurePageState = await pageStateWords(page)
      + `-path-${landingCategory(page.url()).replace(/^landing-/u, '')}`;
    beginTeardown();
    // Deletes only the accounts the active shard created (every account on
    // the unsharded default), so a shard's cleanup step reports on exactly
    // its own accounts.
    const removed: boolean[] = [];
    if (createdAccounts.primary) {
      removed.push(await deletePasskeyAccount(page, pool, {
        authenticatorId: authPrimary,
        email: primaryEmail,
        password: primaryFinalPassword,
        recoveryCode: primaryCleanupCode,
      }));
    }
    if (createdAccounts.recovery) {
      removed.push(await deletePasskeyAccount(page, pool, {
        authenticatorId: authRecovery,
        email: recoveryEmail,
        password: recoveryPassword,
        recoveryCode: recoveryCleanupCode,
      }));
    }
    if (createdAccounts.attempts) {
      removed.push(await deletePasskeyAccount(page, pool, {
        authenticatorId: authAttempts,
        email: attemptsEmail,
        password: attemptsPassword,
        recoveryCode: attemptsCleanupCode,
      }));
    }
    steps.cleanup = removed.length > 0 && removed.every(Boolean);
    for (const extra of extraContexts) {
      await extra.close().catch(() => undefined);
    }
    // A shard's evidence lists only the steps it proved: the closed-list
    // schema check in verify-evidence.mjs accepts any true subset of the
    // full step set for this scenario and expects every step to be present
    // and true only once its results are combined across every shard.
    const provenSteps = Object.fromEntries(
      Object.entries(steps).filter(([, proved]) => proved === true),
    );
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
        steps: provenSteps,
      }, null, 2)}\n`,
      { flag: 'wx', mode: 0o600 },
    );
    // The completion step of the active shard's last role, or
    // attemptsExhausted on the unsharded default.
    const journeyDone = ACTIVE_ROLES === null || ACTIVE_ROLES.has('attempts')
      ? steps.attemptsExhausted
      : ACTIVE_ROLES.has('recovery') ? steps.recoveryCompletion
        : steps.finalRemoved;
    failOnUnexpectedConsole(journeyDone);
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
        hasTotp: true,
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
      // The header shows the signed-out account links until its own /me read
      // settles, and this block's text can arrive first. Those links are
      // wider than the phone width in Vietnamese, so the width is measured
      // on the signed-in header.
      await expect(page.getByTestId('account-menu')).toBeVisible();
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
      // Read the page before teardown navigates away from a failure.
      failurePageState = await pageStateWords(page)
        + `-path-${landingCategory(page.url()).replace(/^landing-/u, '')}`;
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
      ? 'Hoàn tất đăng nhập bằng passkey, ứng dụng xác thực hoặc mã khôi phục.'
      : 'Finish signing in with a passkey, an authenticator app, or a '
        + 'recovery code.',
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
 * is off: the two enrollment routes must match a never-registered
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
        '/api/v1/me/second-factor/unregistered/enrollment', 'POST', '{}'),
    };
  }, csrf);
}

// --- Teardown ---------------------------------------------------------------

interface PasskeyAccountTeardown extends AccountTeardown {
  readonly authenticatorId: string;
}

/**
 * Removes one fictional passkey account, first making its own virtual
 * authenticator the one that answers.
 */
async function deletePasskeyAccount(
  page: Page,
  pool: AuthenticatorPool,
  account: PasskeyAccountTeardown,
): Promise<boolean> {
  if (account.email === '') return true;
  try {
    if (account.authenticatorId !== '') {
      await pool.present(account.authenticatorId);
    }
  } catch {
    return false;
  }
  return deleteAccount(page, account);
}
