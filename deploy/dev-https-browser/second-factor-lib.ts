/**
 * Shared harness for the two second-factor browser proofs,
 * `second-factor.spec.ts` (passkeys) and `totp.spec.ts` (authenticator app):
 * harness constants and wait bounds, the one-line failure report, landing
 * names, console error words, trusted loopback requests, captured mail, and
 * virtual authenticators. Page journeys live in `second-factor-pages.ts`.
 *
 * Each proof runs in its own Playwright worker, so the stage and role this
 * module records belong to that one proof. A spec calls `configureProof`
 * once, at load, before any test body runs.
 */
import { expect, type BrowserContext, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { request as httpsRequest } from 'node:https';
import { ALLOWED_ORIGIN } from './network-policy';

// --- Harness constants -----------------------------------------------------

export const ORIGIN = ALLOWED_ORIGIN;
export const CA_PATH = '/uat-input/caddy-root.crt';
export const CAPTURE_TOKEN_PATH = '/uat-input/mail-capture-token';
export const CLIENT_NAME_PATH = '/uat-input/mcp-client-name';
export const CAPTURE_URL = 'http://127.0.0.1:20444/api/messages';
export const REDIRECT_URI = 'http://127.0.0.1:20090/callback';
export const LINK_ACCOUNT_LABEL = 'Bob Local — bob@example.invalid';
export const DISABLED_ACCOUNT_LABEL
  = 'Development User — developer@example.invalid';
export const PENDING_COOKIE = '__Host-auth-pending';
export const SESSION_COOKIE = '__Host-session';
export const LOCALE_COOKIE = 'aboutme-locale';
export const RECOVERY_INPUT = '#second-factor-recovery-code';
export const PHONE = { height: 844, width: 390 };
export const DESKTOP = { height: 900, width: 1440 };
export const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

// Explicit bounds for the waits Playwright's action and navigation options do
// not cover: response and event waits, DevTools commands, loopback requests
// from Node, and the proofs' own polling. Every one is far above the slowest
// healthy hosted step, so only a genuinely stuck wait trips it, and a trip
// fails at its own stage instead of consuming the whole test budget.
export const WAIT_RESPONSE_MS = 30_000;
export const WAIT_NAVIGATION_MS = 60_000;
// The first visit to an app route pulls that route's whole module graph from
// the harness's Vite dev server, transformed on demand and replayed through
// each proof's request interception, so it is far slower than any later
// navigation. Three minutes is a ceiling, not a cost: only the first landing
// approaches it, and it stays well inside each enabled journey's test budget.
export const WAIT_LANDING_MS = 180_000;
// A first visit to a page compiles its route on the harness's dev server and
// pulls its module graph through each proof's request interception, so it is
// far slower than any later visit. The journey pays that once up front, under
// the warm bound; every visit after it uses the tight hydration bound, which
// is what makes a hydration failure inside the journey mean something.
export const WAIT_WARM_MS = 90_000;
export const WAIT_HYDRATE_MS = 30_000;
export const WAIT_CDP_MS = 30_000;
export const WAIT_LOOPBACK_MS = 20_000;
export const WAIT_MAIL_MS = 45_000;

// --- Failure reporting -----------------------------------------------------
//
// The runner prints only the last stage line it finds and withholds every
// other byte of browser output, so that one line has to carry the whole
// diagnosis. It is assembled from three closed vocabularies and never from an
// exception message, a URL, a response body, or an account value. Teardown
// stops updating the recorded stage, so a failure keeps the stage it happened
// in instead of being relabelled as cleanup.

export type FailureOutcome
  = | 'timeout'
    | 'navigation'
    | 'response'
    | 'locator'
    | 'ceremony'
    | 'capture'
    | 'assertion'
    | 'unknown';

let MODE = 'unconfigured';
let activeRoles: ReadonlySet<string> | null = null;
export let recordedStage = 'start';
export let recordedRole = 'none';
export let tearingDown = false;

/**
 * Sets the browser mode that prefixes every stage line and selects the
 * enabled-proof shard. CI runs an enabled journey as parallel shards, each
 * its own harness and evidence file, named by `shardVariable` (read by run.sh
 * from the host environment). Unset or empty proves every role. Returns the
 * roles the active shard proves, or null for every role.
 */
export function configureProof<R extends string>(
  mode: string,
  shardVariable: string,
  shardRoles: Readonly<Record<string, readonly R[]>>,
): ReadonlySet<R> | null {
  MODE = mode;
  const raw = process.env[shardVariable];
  if (raw === undefined || raw === '') {
    activeRoles = null;
    return null;
  }
  if (!Object.hasOwn(shardRoles, raw)) {
    throw new Error(
      `${shardVariable} must be one of ${Object.keys(shardRoles).join(', ')}, not ${JSON.stringify(raw)}`,
    );
  }
  const roles = new Set(shardRoles[raw]);
  activeRoles = roles;
  return roles;
}

/** True when the active shard (or the unsharded default) proves `r`. */
export function runsRole(r: string): boolean {
  return activeRoles === null || activeRoles.has(r);
}

/** True when this run proves only one shard's roles. */
export function isSharded(): boolean {
  return activeRoles !== null;
}

export function stage(name: string): void {
  if (tearingDown) return;
  recordedStage = name;
  console.log(`${MODE}-stage:${name}`);
}

export function role(next: string): void {
  if (!tearingDown) recordedRole = next;
}

/**
 * Marks the point after which stage and role stop being recorded, and names
 * the stage the body stopped at. The runner forwards only the last stage line,
 * so a bare teardown line would hide exactly what a reader needs.
 */
export function beginTeardown(): void {
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

export function outcomeOf(status: string, message: string): FailureOutcome {
  if (status === 'timedOut') return 'timeout';
  for (const [pattern, outcome] of OUTCOME_PATTERNS) {
    if (pattern.test(message)) return outcome;
  }
  return 'unknown';
}

// --- Landings --------------------------------------------------------------

/**
 * Names where a provider round trip landed, from a closed set. The harness
 * redirects a failed link or login back to the settings or login page with an
 * `error` code, and waiting for the clean URL alone would hang on a page that
 * has already finished loading. Recording the category turns that into a
 * named stage.
 */
export function callbackCategory(value: string): string {
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
export const APP_LANDINGS: readonly string[] = [
  'landing-app-new',
  'landing-app-other',
  'landing-app-resume',
  'landing-app-resumes',
  'landing-app-settings',
];

/**
 * Names where a sign-in or a pending completion landed, from a closed set.
 * The proof asserts the class of destination it depends on, not one exact
 * path, so an app route a proof did not predict is named rather than
 * waited on until the budget runs out.
 */
export function landingCategory(value: string): string {
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
export async function submitState(page: Page): Promise<string> {
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
export async function landedAfter(
  page: Page,
  from: string,
  timeoutMs: number = WAIT_LANDING_MS,
): Promise<string> {
  let settled = true;
  try {
    await page.waitForURL(
      (url) => url.origin === ORIGIN && url.pathname !== from,
      { timeout: timeoutMs },
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
export function expectLanding(page: Page, want: string): void {
  // landedAfter already recorded this landing, with its discriminator when
  // its wait gave up; recording the plain name again would drop that.
  expect(landingCategory(page.url())).toBe(want);
}

/**
 * Runs a provider round trip while recording where each main-frame navigation
 * landed. A failure is re-stamped with that closed category, so the withheld
 * log still names the exact callback outcome.
 */
export async function watchingCallback(
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

// --- Unexpected console errors ---------------------------------------------

// Closed words for each unexpected console error, each prefixed with the
// stage it happened in, so a failure names where and what without printing
// the message or its URL.
const unexpectedConsole: string[] = [];

/**
 * Records one unexpected console error by its closed word. Teardown stops
 * recording stages, so its errors are marked apart from the journey stage
 * they would otherwise inherit.
 */
export function recordUnexpectedConsole(word: string): void {
  const where = tearingDown ? `teardown-${recordedStage}` : recordedStage;
  unexpectedConsole.push(`${where}-${word}`);
}

/**
 * Fails a completed journey whose page logged an unexpected error, naming
 * the first two in closed words so the hosted log shows them. The evidence
 * check would reject the run anyway, but only by count.
 */
export function failOnUnexpectedConsole(journeyDone: boolean): void {
  if (!journeyDone || unexpectedConsole.length === 0) return;
  // Teardown has begun, so stage() is silent; record the words directly.
  recordedStage
    = `console-unexpected-${unexpectedConsole.slice(0, 2).join('-')}`;
  console.log(`${MODE}-stage:${recordedStage}`);
  throw new Error('the page logged unexpected console errors');
}

// --- Fictional run identity ------------------------------------------------

/** A runtime-random password inside the accepted length policy. */
export function runPassword(): string {
  return randomBytes(24).toString('base64url');
}

export function canonicalRecovery(value: string): string {
  return value.replace(/[- ]/gu, '').toUpperCase();
}

/** A canonical recovery-code shape that is none of the issued codes. */
export function fabricatedRecoveryCode(issued: readonly string[]): string {
  for (;;) {
    // 26 characters carry 130 bits for a 128-bit code, so the first one keeps
    // its two high bits zero; any other first character is a malformed code
    // (400), not a wrong one (401).
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

// --- Trusted loopback requests ---------------------------------------------

export interface TrustedResult {
  readonly status: number;
  readonly code: string | null;
}

/**
 * One bounded HTTPS request to the harness origin, made from Node with the
 * exported Caddy root. Used only for states the page cannot produce, such as
 * a foreign Origin, a missing bound session cookie, a replayed ceremony or
 * code, and two genuinely concurrent completions.
 */
export function trustedRequest(
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

export function trustedPost(
  ca: Buffer,
  path: string,
  headers: Readonly<Record<string, string>>,
  body: string,
): Promise<TrustedResult> {
  return trustedRequest(ca, 'POST', path, headers, body);
}

/** The `error.code` of a JSON envelope, or null for any other body. */
export function errorCode(body: string): string | null {
  try {
    const parsed = JSON.parse(body) as { error?: { code?: unknown } };
    return typeof parsed.error?.code === 'string' ? parsed.error.code : null;
  } catch {
    return null;
  }
}

export async function cookieValue(
  context: BrowserContext,
  name: string,
): Promise<string | null> {
  const jar = await context.cookies(ORIGIN);
  return jar.find((entry) => entry.name === name)?.value ?? null;
}

/** The pending cookie header for a Node request, or a fixed failure. */
export async function pendingCookieHeader(context: BrowserContext): Promise<string> {
  const token = await cookieValue(context, PENDING_COOKIE);
  if (token === null) throw new Error('no pending cookie to carry');
  return `${PENDING_COOKIE}=${token}`;
}

/** The pending row's own CSRF token, read through its status route. */
export async function pendingCSRFToken(page: Page): Promise<string> {
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

export interface CapturedMessage {
  kind: string;
  to: string;
  text_body: string;
}

export interface CaptureClient {
  reset(): Promise<void>;
  waitForKind(kind: string, to: string): Promise<void>;
  waitForToken(kind: string, to: string): Promise<string>;
}

export function captureClient(token: string): CaptureClient {
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

// --- Virtual authenticators ------------------------------------------------

export type CDPSend = (
  method: string,
  params?: Record<string, unknown>,
) => Promise<Record<string, unknown>>;

/**
 * Bounds one DevTools round trip. Nothing in Playwright's action or
 * navigation options covers DevTools, so an unanswered command would stall the
 * whole test. The rejection carries fixed words only.
 */
export async function boundedCDPCall<T>(work: Promise<T>): Promise<T> {
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

export function boundedCDP(send: CDPSend): CDPSend {
  return (method, params) => boundedCDPCall(send(method, params));
}

/** A pool of virtual authenticators on one page, one present at a time. */
export class AuthenticatorPool {
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
