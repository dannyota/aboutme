import { expect, type BrowserContext, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { request as httpsRequest } from 'node:https';
import { ALLOWED_ORIGIN } from './network-policy';

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

export const WAIT_RESPONSE_MS = 30_000;
export const WAIT_NAVIGATION_MS = 60_000;
export const WAIT_LANDING_MS = 180_000;
export const WAIT_WARM_MS = 90_000;
export const WAIT_HYDRATE_MS = 30_000;
export const WAIT_CDP_MS = 30_000;
export const WAIT_LOOPBACK_MS = 20_000;
export const WAIT_MAIL_MS = 45_000;

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

export function configureShardList(
  mode: string,
  shardVariable: string,
  shardRoles: Readonly<Record<string, readonly string[]>>,
): readonly string[] | null {
  MODE = mode;
  activeRoles = null;
  const raw = process.env[shardVariable];
  if (raw === undefined || raw === '') return null;
  const names = raw.split(',');
  for (const name of names) {
    if (!Object.hasOwn(shardRoles, name)) {
      throw new Error(
        `${shardVariable} must list names from ${Object.keys(shardRoles).join(', ')}, not ${JSON.stringify(raw)}`,
      );
    }
  }
  if (new Set(names).size !== names.length) {
    throw new Error(`${shardVariable} lists a shard twice: ${JSON.stringify(raw)}`);
  }
  return names;
}

export function activateRoles(roles: ReadonlySet<string> | null): void {
  activeRoles = roles;
}

export function runsRole(r: string): boolean {
  return activeRoles === null || activeRoles.has(r);
}

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

export function beginTeardown(): void {
  tearingDown = true;
  console.log(`${MODE}-stage:cleanup-after-${recordedStage}`);
}

const OUTCOME_PATTERNS: ReadonlyArray<readonly [RegExp, FailureOutcome]> = [
  [/^Test timeout of \d+ms exceeded/u, 'timeout'],
  [/waitForURL|waiting for navigation/u, 'navigation'],
  [/waitForResponse|waitForEvent/u, 'response'],
  [
    /credentials\.(get|create)|WebAuthn|NotAllowedError|InvalidStateError|virtual authenticator/u,
    'ceremony',
  ],
  [/capture (read|reset) failed|within its capture bound/u, 'capture'],
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

export const APP_LANDINGS: readonly string[] = [
  'landing-app-new',
  'landing-app-other',
  'landing-app-resume',
  'landing-app-resumes',
  'landing-app-settings',
];

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

export async function submitState(page: Page): Promise<string> {
  const submit = page
    .getByTestId('login-form')
    .locator('button[type="submit"]');
  if (await submit.count() === 0) return 'submit-absent';
  return await submit.isDisabled() ? 'submit-busy' : 'submit-idle';
}

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

export function expectLanding(page: Page, want: string): void {
  expect(landingCategory(page.url())).toBe(want);
}

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

const unexpectedConsole: string[] = [];

export function recordUnexpectedConsole(word: string): void {
  const where = tearingDown ? `teardown-${recordedStage}` : recordedStage;
  unexpectedConsole.push(`${where}-${word}`);
}

export function failOnUnexpectedConsole(journeyDone: boolean): void {
  if (!journeyDone || unexpectedConsole.length === 0) return;
  recordedStage
    = `console-unexpected-${unexpectedConsole.slice(0, 2).join('-')}`;
  console.log(`${MODE}-stage:${recordedStage}`);
  throw new Error('the page logged unexpected console errors');
}

export function runPassword(): string {
  return randomBytes(24).toString('base64url');
}

export function canonicalRecovery(value: string): string {
  return value.replace(/[- ]/gu, '').toUpperCase();
}

export function fabricatedRecoveryCode(issued: readonly string[]): string {
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

export interface TrustedResult {
  readonly status: number;
  readonly code: string | null;
}

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

export async function pendingCookieHeader(context: BrowserContext): Promise<string> {
  const token = await cookieValue(context, PENDING_COOKIE);
  if (token === null) throw new Error('no pending cookie to carry');
  return `${PENDING_COOKIE}=${token}`;
}

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

export type CDPSend = (
  method: string,
  params?: Record<string, unknown>,
) => Promise<Record<string, unknown>>;

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

export class AuthenticatorPool {
  private readonly ids: string[] = [];

  private constructor(private readonly send: CDPSend) {}

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

  // Chrome rejects a second internal authenticator in one environment, so later ones are USB security keys (Chrome DevTools WebAuthn).
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
