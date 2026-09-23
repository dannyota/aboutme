import { expect, test, type Page, type Request } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { constants } from 'node:fs';
import { lstat, open, readdir, rename, unlink } from 'node:fs/promises';

// Browser helper for the MCP owner workflow. The host Go runner owns OAuth,
// the loopback callback, and every resume call; this helper only signs in
// and approves consent through the private handoff files. See
// docs/design/mcp-owner-workflow.md#browser-helper-interface.

const BROWSER_DIR = '/mcp-browser';
const CREDENTIAL_FILE = '/mcp-credentials/login.env';
const READY_NAME = 'browser-ready';
const REQUEST_NAME = 'browser-request.json';
const RESULT_NAME = 'browser-result.json';
const RESULT_FILE = `${BROWSER_DIR}/${RESULT_NAME}`;
const HANDOFF_NAMES = new Set([READY_NAME, REQUEST_NAME, RESULT_NAME]);
const ORIGINS = {
  local: 'https://localhost:20443',
  production: 'https://aboutme.vn',
} as const;
const CLIENT_NAME = 'aboutme MCP owner workflow';
const REQUEST_KEYS = [
  'authorization_url',
  'credential_file',
  'expected_login_path',
  'expected_origin',
  'mode',
  'result_file',
  'version',
];
const HANDOFF_TIMEOUT_MS = 5 * 60_000;
const STEP_TIMEOUT_MS = 60_000;
const MAX_REQUEST_BYTES = 64 * 1024;
const MAX_CREDENTIAL_BYTES = 4096;

// The step the helper reached, reported in the final stage on failure. The
// vocabulary is fixed, like the result codes.
type HelperStep =
  | 'ready'
  | 'request'
  | 'credentials'
  | 'authorize'
  | 'login'
  | 'session'
  | 'consent'
  | 'callback'
  | 'callback-not-started'
  | `callback-failed-${CallbackError}`
  | 'callback-pending'
  | 'callback-timeout';

// CallbackError classifies Chromium's navigation failure. Only these words
// are ever printed; the browser's own error text never is.
type CallbackError =
  | 'aborted'
  | 'refused'
  | 'blocked'
  | 'name-not-resolved'
  | 'insecure'
  | 'timed-out'
  | 'reset'
  | 'other';

// callbackError maps one net:: error name to a fixed word. A 204 answer ends
// a top-level navigation with ERR_ABORTED after the request was delivered,
// which is the runner's normal callback reply, not a failure.
function callbackError(text: string): CallbackError {
  if (text.includes('ERR_ABORTED')) return 'aborted';
  if (text.includes('ERR_CONNECTION_REFUSED')) return 'refused';
  if (text.includes('ERR_BLOCKED_BY')) return 'blocked';
  if (text.includes('ERR_NAME_NOT_RESOLVED')) return 'name-not-resolved';
  if (text.includes('ERR_SSL') || text.includes('ERR_CERT') || text.includes('ERR_INSECURE')) {
    return 'insecure';
  }
  if (text.includes('ERR_TIMED_OUT') || text.includes('ERR_CONNECTION_TIMED_OUT')) {
    return 'timed-out';
  }
  if (text.includes('ERR_CONNECTION_RESET') || text.includes('ERR_EMPTY_RESPONSE')) return 'reset';
  return 'other';
}
let helperStep: HelperStep = 'ready';
let activeGuard: FlowGuard | null = null;

function step(name: HelperStep): void {
  helperStep = name;
}

type WorkflowMode = keyof typeof ORIGINS;
type HelperResult =
  | 'completed'
  | 'login_failed'
  | 'second_factor_required'
  | 'origin_rejected'
  | 'consent_failed'
  | 'timeout';

// HelperFailure carries only a fixed result code, so no URL, credential, or
// page text can reach the Playwright log through an error message.
class HelperFailure extends Error {
  constructor(readonly result: Exclude<HelperResult, 'completed'>) {
    super(`mcp-sdk helper: ${result}`);
  }
}

interface Handoff {
  readonly authorizationURL: URL;
  readonly callback: URL;
  readonly origin: string;
}

interface Login {
  readonly email: string;
  readonly password: string;
}

function stage(name: string): void {
  console.log(`mcp-sdk-stage:${name}`);
}

function workflowMode(): WorkflowMode {
  if (process.env.ABOUTME_MCP_BROWSER_DIR !== BROWSER_DIR) {
    throw new HelperFailure('origin_rejected');
  }
  const mode = process.env.ABOUTME_MCP_WORKFLOW_MODE;
  if (mode !== 'local' && mode !== 'production') {
    throw new HelperFailure('origin_rejected');
  }
  return mode;
}

async function sleep(milliseconds: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, milliseconds));
}

// readPrivate opens without following links and accepts only a regular
// mode-0600 file owned by this user and within the size bound.
async function readPrivate(path: string, limit: number): Promise<Buffer> {
  const handle = await open(
    path,
    constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
  );
  try {
    const info = await handle.stat();
    if (
      !info.isFile()
      || (info.mode & 0o777) !== 0o600
      || info.uid !== process.getuid?.()
      || info.size > limit
    ) {
      throw new HelperFailure('origin_rejected');
    }
    const data = await handle.readFile();
    if (data.length > limit) throw new HelperFailure('origin_rejected');
    return data;
  } finally {
    await handle.close();
  }
}

// writeAtomic publishes a mode-0600 file through an exclusive temporary name
// and a rename, so the runner never reads a partial result.
async function writeAtomic(name: string, contents: string): Promise<void> {
  const temporary = `${BROWSER_DIR}/.${name}-${randomBytes(8).toString('hex')}`;
  const handle = await open(
    temporary,
    constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
    0o600,
  );
  try {
    await handle.chmod(0o600);
    await handle.writeFile(contents, 'ascii');
    await handle.sync();
  } finally {
    await handle.close();
  }
  try {
    await rename(temporary, `${BROWSER_DIR}/${name}`);
  } catch (error) {
    await unlink(temporary).catch(() => undefined);
    throw error;
  }
}

async function requireHandoffDirectory(empty: boolean): Promise<void> {
  const info = await lstat(BROWSER_DIR);
  if (
    !info.isDirectory()
    || (info.mode & 0o777) !== 0o700
    || info.uid !== process.getuid?.()
  ) {
    throw new HelperFailure('origin_rejected');
  }
  const entries = await readdir(BROWSER_DIR, { withFileTypes: true });
  for (const entry of entries) {
    if (empty || !HANDOFF_NAMES.has(entry.name) || !entry.isFile()) {
      throw new HelperFailure('origin_rejected');
    }
  }
}

// waitForRequest reads the runner's request once and unlinks it at once.
async function waitForRequest(deadline: number): Promise<Buffer> {
  const path = `${BROWSER_DIR}/${REQUEST_NAME}`;
  while (Date.now() < deadline) {
    try {
      const data = await readPrivate(path, MAX_REQUEST_BYTES);
      await unlink(path);
      return data;
    } catch (error) {
      if (error instanceof HelperFailure) throw error;
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
        throw new HelperFailure('origin_rejected');
      }
    }
    await sleep(100);
  }
  throw new HelperFailure('timeout');
}

function parseRequest(data: Buffer, mode: WorkflowMode): Handoff {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data.toString('utf8'));
  } catch {
    throw new HelperFailure('origin_rejected');
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new HelperFailure('origin_rejected');
  }
  const request = parsed as Record<string, unknown>;
  const origin = ORIGINS[mode];
  if (
    JSON.stringify(Object.keys(request).sort()) !== JSON.stringify(REQUEST_KEYS)
    || request.version !== 1
    || request.mode !== mode
    || request.expected_origin !== origin
    || request.expected_login_path !== '/login'
    || request.credential_file !== CREDENTIAL_FILE
    || request.result_file !== RESULT_FILE
    || typeof request.authorization_url !== 'string'
  ) {
    throw new HelperFailure('origin_rejected');
  }
  let authorizationURL: URL;
  try {
    authorizationURL = new URL(request.authorization_url);
  } catch {
    throw new HelperFailure('origin_rejected');
  }
  if (
    authorizationURL.origin !== origin
    || authorizationURL.pathname !== '/oauth/authorize'
    || authorizationURL.username !== ''
    || authorizationURL.password !== ''
    || authorizationURL.hash !== ''
  ) {
    throw new HelperFailure('origin_rejected');
  }
  return { authorizationURL, callback: callbackOf(authorizationURL), origin };
}

// callbackOf returns the loopback callback the runner registered. The helper
// only lets the browser reach it; Go alone validates the callback itself.
function callbackOf(authorizationURL: URL): URL {
  const values = authorizationURL.searchParams.getAll('redirect_uri');
  if (values.length !== 1) throw new HelperFailure('origin_rejected');
  let callback: URL;
  try {
    callback = new URL(values[0] as string);
  } catch {
    throw new HelperFailure('origin_rejected');
  }
  const port = Number(callback.port);
  if (
    callback.protocol !== 'http:'
    || callback.hostname !== '127.0.0.1'
    || !Number.isInteger(port) || port < 1024 || port > 65535
    || callback.pathname !== '/oauth/callback'
    || callback.search !== ''
    || callback.hash !== ''
    || callback.username !== ''
    || callback.password !== ''
  ) {
    throw new HelperFailure('origin_rejected');
  }
  return callback;
}

// readLogin reads only the two named keys. Any other line is ignored and
// never kept; a duplicate or empty named key fails.
async function readLogin(): Promise<Login> {
  let text: string;
  try {
    text = (await readPrivate(CREDENTIAL_FILE, MAX_CREDENTIAL_BYTES)).toString('utf8');
  } catch {
    throw new HelperFailure('login_failed');
  }
  const values = new Map<string, string>();
  for (const line of text.split(/\r?\n/)) {
    const match = /^(ABOUTME_TEST_EMAIL|ABOUTME_TEST_PASSWORD)=(.*)$/.exec(line);
    if (match === null) continue;
    const key = match[1] as string;
    let value = match[2] as string;
    if (/^(["']).*\1$/.test(value) && value.length >= 2) value = value.slice(1, -1);
    if (values.has(key) || value === '') throw new HelperFailure('login_failed');
    values.set(key, value);
  }
  const email = values.get('ABOUTME_TEST_EMAIL');
  const password = values.get('ABOUTME_TEST_PASSWORD');
  if (email === undefined || password === undefined) {
    throw new HelperFailure('login_failed');
  }
  return { email, password };
}

function isCallback(url: URL, callback: URL): boolean {
  return url.origin === callback.origin && url.pathname === callback.pathname;
}

function parsed(value: string): URL | null {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

// FlowGuard records any navigation, redirect, or frame that leaves the
// expected origin or the exact callback. A recorded violation stops the flow
// before the next credential fill or consent action.
class FlowGuard {
  violated = false;
  // Callback and consent observations, reported as fixed words only.
  callbackRequested = false;
  callbackFailure: CallbackError | null = null;
  callbackStatus = 0;
  consentStatus = 0;

  constructor(
    private readonly page: Page,
    readonly handoff: Handoff,
  ) {}

  allowed(url: URL | null): boolean {
    return url !== null && url.username === '' && url.password === ''
      && (url.origin === this.handoff.origin || isCallback(url, this.handoff.callback));
  }

  async install(): Promise<void> {
    const context = this.page.context();
    await context.route('**/*', async (route) => {
      const request = route.request();
      const url = parsed(request.url());
      if (url?.origin === this.handoff.origin && url.username === '' && url.password === '') {
        await route.continue();
        return;
      }
      if (
        url !== null && isCallback(url, this.handoff.callback)
        && request.isNavigationRequest() && request.frame() === this.page.mainFrame()
      ) {
        await route.continue();
        return;
      }
      if (request.isNavigationRequest()) this.violated = true;
      await route.abort('blockedbyclient');
    });
    await context.routeWebSocket('**/*', async (socket) => {
      const url = parsed(socket.url());
      if (url?.protocol === 'wss:' && url.host === new URL(this.handoff.origin).host) {
        socket.connectToServer();
        return;
      }
      await socket.close({ code: 1008, reason: 'blocked' });
    });
    this.page.on('request', (request: Request) => {
      const url = parsed(request.url());
      if (request.isNavigationRequest() && !this.allowed(url)) this.violated = true;
      if (url !== null && isCallback(url, this.handoff.callback)) this.callbackRequested = true;
    });
    this.page.on('requestfailed', (request: Request) => {
      const url = parsed(request.url());
      if (url !== null && isCallback(url, this.handoff.callback)) {
        this.callbackFailure = callbackError(request.failure()?.errorText ?? '');
      }
    });
    this.page.on('response', (response) => {
      const url = parsed(response.url());
      if (url === null) return;
      if (isCallback(url, this.handoff.callback)) this.callbackStatus = response.status();
      if (url.origin === this.handoff.origin && url.pathname === '/api/v1/oauth/consent') {
        this.consentStatus = response.status();
      }
    });
    this.page.on('framenavigated', (frame) => {
      if (frame !== this.page.mainFrame() || !this.allowed(parsed(frame.url()))) {
        this.violated = true;
      }
    });
  }

  // requireLoginPage runs immediately before each saved-value fill: the top
  // page must be the exact login path on the expected origin, with no child
  // frame, and `next` must name only the relative internal authorize target.
  requireLoginPage(): void {
    const url = parsed(this.page.url());
    if (
      this.violated
      || url === null
      || url.origin !== this.handoff.origin
      || url.pathname !== '/login'
      || this.page.frames().length !== 1
    ) {
      throw new HelperFailure('origin_rejected');
    }
    const next = url.searchParams.getAll('next');
    if (next.length !== 1) throw new HelperFailure('origin_rejected');
    const value = next[0] as string;
    if (!value.startsWith('/') || value.startsWith('//') || value.includes('\\')) {
      throw new HelperFailure('origin_rejected');
    }
    const target = new URL(value, this.handoff.origin);
    if (target.origin !== this.handoff.origin || target.pathname !== '/oauth/authorize') {
      throw new HelperFailure('origin_rejected');
    }
  }

  // callbackDelivered reports the callback the runner answers with 204: the
  // navigation is cancelled after delivery, so an abort counts as delivered,
  // as does any response below 400.
  callbackDelivered(): boolean {
    return this.callbackFailure === 'aborted'
      || (this.callbackStatus > 0 && this.callbackStatus < 400);
  }

  callbackRejected(): boolean {
    return (this.callbackFailure !== null && this.callbackFailure !== 'aborted')
      || this.callbackStatus >= 400;
  }

  // callbackStep names how far the loopback navigation got, in fixed words.
  callbackStep(): HelperStep {
    if (!this.callbackRequested) return 'callback-not-started';
    if (this.callbackFailure !== null) return `callback-failed-${this.callbackFailure}`;
    if (this.callbackStatus === 0) return 'callback-pending';
    return 'callback-timeout';
  }

  // pageClass names the current route, never its URL.
  pageClass(): string {
    const url = parsed(this.page.url());
    if (url === null) return 'other';
    if (isCallback(url, this.handoff.callback)) return 'callback';
    if (url.origin !== this.handoff.origin) return 'other';
    if (url.pathname === '/authorize') return 'authorize';
    if (url.pathname === '/login') return 'login';
    return 'other';
  }

  requireConsentPage(): void {
    const url = parsed(this.page.url());
    if (
      this.violated
      || url === null
      || url.origin !== this.handoff.origin
      || url.pathname !== '/authorize'
      || this.page.frames().length !== 1
    ) {
      throw new HelperFailure('origin_rejected');
    }
  }
}

async function waitForHydration(page: Page): Promise<void> {
  await expect.poll(
    () => page.evaluate(() => Boolean(
      (document.getElementById('__nuxt') as HTMLElement & { __vue_app__?: unknown } | null)
        ?.__vue_app__,
    )),
    { timeout: STEP_TIMEOUT_MS },
  ).toBe(true);
}

async function onPath(page: Page, origin: string, paths: readonly string[]): Promise<string> {
  await page.waitForURL(
    (url) => url.origin === origin && paths.includes(url.pathname),
    { timeout: STEP_TIMEOUT_MS, waitUntil: 'commit' },
  );
  return new URL(page.url()).pathname;
}

// signIn fills the two saved values exactly once. A second login page after
// the submit is a failed login, never a refill.
async function signIn(page: Page, guard: FlowGuard, login: Login): Promise<void> {
  stage('login');
  step('login');
  await waitForHydration(page);
  const form = page.locator('[data-testid="login-form"]');
  const email = form.locator('input[type="email"]');
  const password = form.locator('input[autocomplete="current-password"]');
  const submit = form.locator('button[type="submit"]');
  if (await email.count() !== 1 || await password.count() !== 1 || await submit.count() !== 1) {
    throw new HelperFailure('login_failed');
  }
  guard.requireLoginPage();
  await email.fill(login.email);
  guard.requireLoginPage();
  await password.fill(login.password);
  guard.requireLoginPage();
  const authenticated = page.waitForResponse(
    (response) => response.request().method() === 'POST'
      && new URL(response.url()).pathname === '/api/v1/auth/password/login',
    { timeout: STEP_TIMEOUT_MS },
  );
  await submit.click();
  stage('login-submitted');
  step('session');
  let status: number;
  try {
    status = (await authenticated).status();
  } catch {
    throw new HelperFailure(guard.violated ? 'origin_rejected' : 'login_failed');
  }
  // The password login answers 202 only for an account enrolled in a second
  // factor. This helper signs in with a password alone, so it names that
  // outcome instead of reporting a generic login failure.
  if (status === 202) throw new HelperFailure('second_factor_required');
  if (status !== 204) throw new HelperFailure('login_failed');
  // The login page follows `next` through the client router, and the
  // authorization endpoint is served by the API rather than by a page route,
  // so the helper reopens the authorization URL as a full navigation.
  try {
    await page.goto(guard.handoff.authorizationURL.href, {
      timeout: STEP_TIMEOUT_MS,
      waitUntil: 'commit',
    });
    await onPath(page, guard.handoff.origin, ['/authorize']);
  } catch {
    throw new HelperFailure(guard.violated ? 'origin_rejected' : 'login_failed');
  }
}

async function approveConsent(page: Page, guard: FlowGuard, handoff: Handoff): Promise<void> {
  stage('consent');
  step('consent');
  try {
    await waitForHydration(page);
    guard.requireConsentPage();
    const clientName = page.locator('[data-testid="consent-client-name"]');
    await expect(clientName).toHaveText(CLIENT_NAME, { timeout: STEP_TIMEOUT_MS });
    await expect(page.locator('[data-testid="consent-scopes"] dt')).toHaveCount(2);
    const approve = page.locator('[data-testid="consent-form"] button[data-decision="approve"]');
    await expect(approve).toHaveCount(1);
    guard.requireConsentPage();
    step('callback');
    await approve.click();
    // The consent page approves through a POST and then navigates the top
    // level to the runner's loopback callback. The runner answers 204, which
    // completes the request without replacing the page, so the helper waits
    // for the callback response rather than for a URL change. The route
    // handler lets that one http navigation through from the https page.
    await expect.poll(
      () => guard.callbackDelivered() || guard.callbackRejected(),
      { timeout: STEP_TIMEOUT_MS },
    ).toBe(true);
    if (!guard.callbackDelivered()) {
      step(guard.callbackStep());
      throw new HelperFailure('consent_failed');
    }
  } catch (error) {
    if (!(error instanceof HelperFailure) && helperStep === 'callback') step(guard.callbackStep());
    if (error instanceof HelperFailure) throw error;
    throw new HelperFailure(guard.violated ? 'origin_rejected' : 'consent_failed');
  }
  if (guard.violated) throw new HelperFailure('origin_rejected');
}

async function authorize(page: Page, handoff: Handoff, login: Login): Promise<void> {
  const guard = new FlowGuard(page, handoff);
  activeGuard = guard;
  await guard.install();
  stage('authorize');
  step('authorize');
  let path: string;
  try {
    await page.goto(handoff.authorizationURL.href, {
      timeout: STEP_TIMEOUT_MS,
      waitUntil: 'commit',
    });
    path = await onPath(page, handoff.origin, ['/login', '/authorize']);
  } catch {
    throw new HelperFailure(guard.violated ? 'origin_rejected' : 'login_failed');
  }
  if (path === '/login') {
    try {
      await signIn(page, guard, login);
    } catch (error) {
      if (error instanceof HelperFailure) throw error;
      throw new HelperFailure(guard.violated ? 'origin_rejected' : 'login_failed');
    }
  }
  await approveConsent(page, guard, handoff);
}

test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('MCP owner workflow browser helper', async ({ page }) => {
  test.setTimeout(HANDOFF_TIMEOUT_MS + 2 * STEP_TIMEOUT_MS);
  const deadline = Date.now() + HANDOFF_TIMEOUT_MS;
  let result: HelperResult = 'timeout';
  let readyWritten = false;
  try {
    const mode = workflowMode();
    stage('handoff-ready');
    await requireHandoffDirectory(true);
    await writeAtomic(READY_NAME, 'ready');
    readyWritten = true;
    stage('handoff-request');
    step('request');
    const handoff = parseRequest(await waitForRequest(deadline), mode);
    await requireHandoffDirectory(false);
    step('credentials');
    const login = await readLogin();
    const flow = authorize(page, handoff, login);
    flow.catch(() => undefined);
    let timer: NodeJS.Timeout | undefined;
    const expiry = new Promise<never>((_, reject) => {
      timer = setTimeout(
        () => reject(new HelperFailure('timeout')),
        Math.max(0, deadline - Date.now()),
      );
    });
    try {
      await Promise.race([flow, expiry]);
    } finally {
      clearTimeout(timer);
    }
    result = 'completed';
  } catch (error) {
    result = error instanceof HelperFailure ? error.result : 'consent_failed';
  }
  if (readyWritten) {
    try {
      await writeAtomic(RESULT_NAME, result);
    } catch {
      throw new Error('mcp-sdk helper: result write failed');
    }
  }
  const consent = activeGuard === null || activeGuard.consentStatus === 0
    ? 'none'
    : String(activeGuard.consentStatus);
  const where = activeGuard === null ? 'other' : activeGuard.pageClass();
  const failure = `handoff-failed-${result.replace(/_/gu, '-')}-at-${helperStep}`
    + `-consent-${consent}-on-${where}`;
  stage(result === 'completed' ? 'handoff-complete' : failure);
  if (result !== 'completed') throw new Error(`mcp-sdk helper: ${result}`);
});
