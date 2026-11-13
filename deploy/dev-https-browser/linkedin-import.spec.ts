import {
  expect,
  test,
  webkit,
  type Browser,
  type Page,
  type Request,
} from '@playwright/test';
import { writeFile } from 'node:fs/promises';

import {
  createBlankResume,
  deleteRecordedResume,
  loginAsDevelopmentUser,
  uniqueTitle,
} from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pinEnglish,
  waitForHydration,
} from './harness-lib';
import {
  BASIC_EN_PDF_BASE64,
  LOCALIZED_VI_PDF_BASE64,
  OTHER_PDF_BASE64,
  slowContentPdf,
} from './linkedin-import-fixtures';
import { ALLOWED_ORIGIN, isExpectedAnonymousMeConsole } from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/linkedin-import-proof.json';
const FAILURE_EVIDENCE_PATH = '/evidence/linkedin-import-failure.json';
const RESUME_CAP = 3;
const TIME_LIMIT_MS = 15_000;

const recordedResumeIDs = new Set<string>();

let lastStage = 'start';
function stage(name: string): void {
  lastStage = name;
  console.log(`linkedin-import-stage:${name}`);
}

const STRAY_TITLES = new Set(['LinkedIn resume', 'CV từ LinkedIn']);

async function strayLinkedInResumeIDs(page: Page): Promise<string[]> {
  const items = await page.evaluate(async () => {
    const response = await fetch('/api/v1/resumes', {
      cache: 'no-store',
      credentials: 'include',
    });
    const body = await response.json() as {
      data?: { id?: unknown; title?: unknown }[];
    };
    return Array.isArray(body.data) ? body.data : [];
  });
  return items
    .filter((item): item is { id: string; title: string } =>
      typeof item.id === 'string' && typeof item.title === 'string'
      && STRAY_TITLES.has(item.title))
    .map((item) => item.id);
}

test.afterEach(async ({ browser }) => {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await loginAsDevelopmentUser(page);
    for (const id of await strayLinkedInResumeIDs(page)) {
      recordedResumeIDs.add(id);
    }
    for (const id of recordedResumeIDs) await deleteRecordedResume(page, id);
    recordedResumeIDs.clear();
  } catch {
    stage('cleanup-hook-failed');
    throw new Error('linkedin-import cleanup hook failed');
  } finally {
    await context.close();
  }
});

function pdfBuffer(base64: string): Buffer {
  return Buffer.from(base64, 'base64');
}

// Playwright's setInputFiles works on a hidden file input by design (Playwright setInputFiles).
async function pickFile(page: Page, name: string, buffer: Buffer): Promise<void> {
  await page.locator('[data-testid="import-file-input"]').setInputFiles({
    buffer, mimeType: 'application/pdf', name,
  });
}

function watchForFileBytes(page: Page, violations: string[]): void {
  page.on('request', (request) => {
    const body = request.postData();
    if (body !== null && body.includes('%PDF-')) {
      violations.push(`${request.method()} ${new URL(request.url()).pathname}`);
    }
  });
}

function isKnownInterruptedFetchPathname(pathname: string): boolean {
  return pathname.startsWith('/_nuxt/builds/meta/')
    || pathname === '/api/v1/capabilities';
}

function isKnownInterruptedFetchURL(url: string): boolean {
  let pathname: string;
  try {
    ({ pathname } = new URL(url));
  } catch {
    return false;
  }
  return isKnownInterruptedFetchPathname(pathname);
}

// WebKit reports this poll's interruption with the scheme and one slash dropped ("/localhost:20443/api/..."), which new URL cannot parse (WebKit).
const ORIGIN_HOST = new URL(ALLOWED_ORIGIN).host;

function isKnownInterruptedFetchToken(token: string): boolean {
  if (isKnownInterruptedFetchURL(token)) return true;
  const hostPrefix = `/${ORIGIN_HOST}`;
  return token.startsWith(hostPrefix)
    && isKnownInterruptedFetchPathname(token.slice(hostPrefix.length));
}

const INTERRUPTED_FETCH_URL = /(\S+) due to access control checks\.$/u;

// WebKit raises a fetch or worker import that a navigation interrupted as an uncaught page error; Chromium drops it silently (WebKit).
function isExpectedWebKitInterruptedFetch(
  engine: 'chromium' | 'webkit',
  text: string,
): boolean {
  if (engine !== 'webkit') return false;
  if (text === 'TypeError: Importing a module script failed.') return true;
  const match = INTERRUPTED_FETCH_URL.exec(text);
  return match !== null && isKnownInterruptedFetchToken(match[1]!);
}

// WebKit fills the status text of a failed fetch's console line ("401 (Unauthorized)") where Chromium leaves it empty (WebKit console).
const WEBKIT_ANONYMOUS_ME_CONSOLE =
  /^Failed to load resource: the server responded with a status of 401 \([^)]*\)$/u;

function isExpectedAnonymousMeConsoleAnyWording(
  engine: 'chromium' | 'webkit',
  text: string,
  url: string,
): boolean {
  if (isExpectedAnonymousMeConsole(text, url)) return true;
  if (engine !== 'webkit' || !WEBKIT_ANONYMOUS_ME_CONSOLE.test(text)) {
    return false;
  }
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  return parsed.origin === ORIGIN && parsed.pathname === '/api/v1/me'
    && parsed.search === '';
}

interface WindowRequest {
  readonly method: string;
  readonly pathname: string;
}

function recordSameOriginRequests(page: Page): () => WindowRequest[] {
  const seen: WindowRequest[] = [];
  const onRequest = (request: Request): void => {
    const url = new URL(request.url());
    if (url.origin === ORIGIN) {
      seen.push({ method: request.method(), pathname: url.pathname });
    }
  };
  page.on('request', onRequest);
  return () => {
    page.off('request', onRequest);
    return seen;
  };
}

async function currentResumeCount(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/resumes', {
      cache: 'no-store',
      credentials: 'include',
    });
    const body = await response.json() as { data?: unknown[] };
    return Array.isArray(body.data) ? body.data.length : 0;
  });
}

async function fillToResumeCap(page: Page): Promise<void> {
  let count = await currentResumeCount(page);
  while (count < RESUME_CAP) {
    const created = await createBlankResume(page, uniqueTitle());
    recordedResumeIDs.add(created.metadata.id);
    count += 1;
  }
}

async function writeFailureEvidence(
  name: string,
  stageName: string,
  extra: Record<string, unknown> = {},
): Promise<void> {
  await writeFile(
    name,
    `${JSON.stringify({ lastStage: stageName, schemaVersion: 1, ...extra })}\n`,
    { flag: 'wx', mode: 0o600 },
  );
}

interface RedactedConsoleMessage {
  readonly type: string;
  readonly pathname: string;
  readonly text: string;
}

const ID_PLACEHOLDER = '{id}';
const UUID_SEGMENT =
  /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/giu;
const NUXT_BUILD_META_PATH = /^\/_nuxt\/builds\/meta\/[^/]+\.json$/u;

function redactPathname(pathname: string): string {
  if (NUXT_BUILD_META_PATH.test(pathname)) {
    return `/_nuxt/builds/meta/${ID_PLACEHOLDER}.json`;
  }
  return pathname.replace(UUID_SEGMENT, ID_PLACEHOLDER);
}

// Failure evidence is a public CI artifact, so message text is reduced to its
// first line, stripped of everything that could identify a host, account, or
// credential, and cut to a fixed length. Redaction runs before truncation so
// a cut never leaves half of a secret behind. Order matters: URLs and
// scheme-less host paths go first, then emails, ids, and long opaque runs.
const EVIDENCE_TEXT_LIMIT = 200;
const URL_IN_TEXT = /[a-z][a-z0-9+.-]*:\/\/\S*/giu;
const HOST_PATH_IN_TEXT =
  /\/?(?:localhost|127\.0\.0\.1|\[::1\])(?::\d+)?(?:\/\S*)?/giu;
const EMAIL_IN_TEXT = /[^\s@<>"']+@[^\s@<>"']+/gu;
const OPAQUE_RUN = /[A-Za-z0-9_+/=.-]{20,}/gu;
// eslint-disable-next-line no-control-regex
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/gu;

function redactEvidenceText(text: string): string {
  const firstLine = text.split(/\r?\n/u, 1)[0] ?? '';
  return firstLine
    .replace(URL_IN_TEXT, '{url}')
    .replace(HOST_PATH_IN_TEXT, '{url}')
    .replace(EMAIL_IN_TEXT, '{email}')
    .replace(UUID_SEGMENT, ID_PLACEHOLDER)
    .replace(OPAQUE_RUN, '{token}')
    .replace(CONTROL_CHARS, ' ')
    .slice(0, EVIDENCE_TEXT_LIMIT);
}

function pathnameOf(url: string): string {
  try {
    return redactPathname(new URL(url).pathname);
  } catch {
    return '';
  }
}

function diagnosticURL(token: string): { origin: string; path: string; shape: string } {
  let value = token;
  let shape = 'absolute';
  if (token.startsWith(`/${ORIGIN_HOST}/`)) {
    value = `https:/${token}`;
    shape = 'scheme-less-host';
  }
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return { origin: 'unknown', path: 'unknown', shape: 'unparseable' };
  }
  const origin = url.origin === ORIGIN ? 'same' : 'other';
  const path = url.pathname === '/api/v1/capabilities' ? 'capabilities'
    : url.pathname.startsWith('/_nuxt/builds/meta/') ? 'build-meta'
      : url.pathname.startsWith('/_nuxt/') ? 'nuxt-asset'
        : url.pathname.startsWith('/api/') ? 'api'
          : url.pathname.startsWith('/app/') ? 'app'
            : 'other';
  return { origin, path, shape };
}

test('entry link, review, and create land the imported content in the editor', async ({
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  const fileByteViolations: string[] = [];
  await pinEnglish(context);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url),
  })(page);
  watchForFileBytes(page, fileByteViolations);

  const steps = {
    contactDefaults: false,
    created: false,
    cspWorkerSrc: false,
    deselectSection: false,
    editorContent: false,
    entryLink: false,
    localePersists: false,
    requestWindowClean: false,
  };
  let createdID: string | undefined;
  let stopRecording: (() => WindowRequest[]) | undefined;

  try {
    stage('create-sign-in');
    await loginAsDevelopmentUser(page);
    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();

    stage('create-entry-link');
    await page.getByTestId('create-resume').click();
    const dialog = page.getByRole('dialog', { name: 'Create resume' });
    await expect(dialog).toBeVisible();
    const [importResponse] = await Promise.all([
      page.waitForResponse((response) =>
        response.url() === `${ORIGIN}/app/import/linkedin`
        && response.request().resourceType() === 'document'),
      dialog.locator('[data-action="create-import-linkedin"]').click(),
    ]);
    expect(importResponse.status()).toBe(200);
    steps.entryLink = true;

    stage('create-csp');
    expect(importResponse.headers()['content-security-policy'])
      .toContain('worker-src \'self\'');
    steps.cspWorkerSrc = true;

    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Import from LinkedIn' }),
    ).toBeVisible();

    stage('create-pick-window-start');
    await page.setViewportSize({ width: 390, height: 844 });
    stopRecording = recordSameOriginRequests(page);
    await pickFile(page, 'basic-en.pdf', pdfBuffer(BASIC_EN_PDF_BASE64));

    stage('create-review-en');
    const reviewHeadingEn = page.getByRole('heading', {
      name: 'Check your import',
    });
    await expect(reviewHeadingEn).toBeVisible();
    await expect(reviewHeadingEn).toBeFocused();

    stage('create-contact-defaults');
    await expect(
      page.getByRole('checkbox', { name: 'sample.person@example.com' }),
    ).not.toBeChecked();
    await expect(
      page.getByRole('checkbox', { name: '0900000000' }),
    ).not.toBeChecked();
    await expect(
      page.getByRole('checkbox', {
        name: 'https://linkedin.com/in/sample-person',
      }),
    ).toBeChecked();
    await expect(
      page.getByRole('checkbox', {
        name: 'https://example.com/sample-person',
      }),
    ).toBeChecked();
    steps.contactDefaults = true;

    stage('create-locale-vi-mid-review');
    await page.getByTestId('landing-locale-vi').click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'vi');
    const reviewHeadingVi = page.getByRole('heading', {
      name: 'Kiểm tra nội dung từ LinkedIn',
    });
    await expect(reviewHeadingVi).toBeVisible();
    await expect(
      page.getByRole('checkbox', { name: 'sample.person@example.com' }),
    ).not.toBeChecked();
    steps.localePersists = true;

    stage('create-locale-en-for-create');
    await page.getByTestId('landing-locale-en').click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(reviewHeadingEn).toBeVisible();
    await page.setViewportSize({ width: 1280, height: 800 });

    stage('create-deselect-certifications');
    const certificatesGroup = page.getByRole('checkbox', {
      name: 'Import all of Certifications',
    });
    await expect(certificatesGroup).toBeChecked();
    await certificatesGroup.click();
    await expect(certificatesGroup).not.toBeChecked();
    steps.deselectSection = true;

    stage('create-submit');
    const createRequest = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'POST'
        && url.origin === ORIGIN && url.pathname === '/api/v1/resumes';
    });
    await page.locator('[data-action="import-create"]').click();
    const createResponse = await createRequest;
    expect(createResponse.status()).toBe(201);
    const createBody = await createResponse.json() as { data?: { id?: unknown } };
    createdID = typeof createBody.data?.id === 'string'
      ? createBody.data.id
      : undefined;
    expect(createdID).toBeDefined();
    if (createdID !== undefined) recordedResumeIDs.add(createdID);
    const requestBody = createResponse.request().postDataJSON() as {
      document: { content: Record<string, unknown> };
      lng: string;
      title: string;
    };
    expect(requestBody.lng).toBe('en');
    expect(requestBody.title).toBe('LinkedIn resume');
    expect(Object.hasOwn(requestBody.document.content, 'certificate'))
      .toBe(false);
    expect(Object.hasOwn(requestBody.document.content, 'work')).toBe(true);
    steps.created = true;

    stage('create-window-check');
    const windowRequests = stopRecording();
    stopRecording = undefined;
    const droppedNuxtAssetRequests = windowRequests.filter((request) =>
      request.pathname.startsWith('/_nuxt/'));
    expect(droppedNuxtAssetRequests.every(
      (request) => request.method === 'GET',
    )).toBe(true);
    const dataRequests = windowRequests.filter((request) =>
      !(request.method === 'GET' && request.pathname.startsWith('/_nuxt/')));
    expect(dataRequests).toEqual([
      { method: 'POST', pathname: '/api/v1/resumes' },
    ]);
    steps.requestWindowClean = true;

    stage('create-editor-lands');
    await page.waitForURL((url) =>
      url.origin === ORIGIN && url.pathname === `/app/resumes/${createdID}`);
    await waitForHydration(page);
    await expect(page.getByLabel('Full name')).toHaveValue('Sample Person');
    await expect(page.getByLabel('Headline')).toHaveValue(
      'Product Manager # Example-first & Co',
    );
    const preview = page.getByTestId('preview-sheet');
    await expect(preview).toContainText('Sample Person');
    await expect(preview).toContainText('Example Co.');
    await expect(preview).toContainText('University of Sample Studies');
    await expect(preview).toContainText('Product Management');
    await expect(preview).not.toContainText('Example Certified Professional');
    steps.editorContent = true;
    stage('create-done');
  } catch (error) {
    stopRecording?.();
    const stageAtFailure = lastStage;
    const sourceLine = error instanceof Error
      ? /linkedin-import\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? '')?.[1]
      : undefined;
    if (sourceLine !== undefined) stage(`create-failure-at-line-${sourceLine}`);
    try {
      await writeFailureEvidence(FAILURE_EVIDENCE_PATH, stageAtFailure);
    } catch {
      stage('failure-evidence-write-failed');
    }
    throw error;
  }

  expect(fileByteViolations).toEqual([]);
  expect(counters).toEqual({
    certificateErrors: 0, consoleErrors: 0, externalRequests: 0, pageErrors: 0,
  });
  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify({
      errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
      origin: ORIGIN,
      scenario: 'linkedin-import',
      schemaVersion: 1,
      steps,
    })}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});

let webkitBrowser: Browser | undefined;

test.beforeAll(async () => {
  webkitBrowser = await webkit.launch();
});

test.afterAll(async () => {
  await webkitBrowser?.close();
});

for (const engine of ['chromium', 'webkit'] as const) {
  test(`not-LinkedIn, not-English, the time limit, and the cap (${engine})`, async ({
    browser,
  }) => {
    const source = engine === 'chromium' ? browser : webkitBrowser;
    if (source === undefined) throw new Error(`${engine} did not launch`);
    const counters = newDiagnosticCounters();
    const fileByteViolations: string[] = [];
    const unexpectedPageErrors: string[] = [];
    const unexpectedConsoleMessages: RedactedConsoleMessage[] = [];
    const diagnosticConsole: Record<string, string>[] = [];
    const diagnosticRequests: Record<string, string>[] = [];
    const recordDiagnostic = (events: Record<string, string>[], event: Record<string, string>): void => {
      if (events.length === 16) events.shift();
      events.push(event);
    };
    // A directly launched WebKit has no baseURL and validates against the system trust bundle, not Chromium's NSS database (Playwright WebKit).
    const context = await source.newContext(
      engine === 'webkit' ? { baseURL: ORIGIN } : {},
    );
    try {
      await pinEnglish(context);
      await installExternalRequestFirewall(context, counters);
      await installExternalWebSocketFirewall(context, counters);
      const page = await context.newPage();
      page.on('requestfailed', (request) => {
        const failure = request.failure()?.errorText ?? '';
        recordDiagnostic(diagnosticRequests, {
          event: 'request-failed', stage: lastStage,
          ...diagnosticURL(request.url()),
          resource: ['document', 'script', 'fetch', 'xhr'].includes(request.resourceType())
            ? request.resourceType() : 'other',
          failure: /cancel|abort/i.test(failure) ? 'cancel-or-abort' : 'other',
        });
      });
      pageDiagnosticsAttacher(counters, {
        countConsoleError: (message) =>
          !isExpectedAnonymousMeConsoleAnyWording(
            engine, message.text(), message.location().url,
          )
          && !isExpectedWebKitInterruptedFetch(engine, message.text()),
        onCountedConsoleError: (message) => {
          const match = INTERRUPTED_FETCH_URL.exec(message.text());
          recordDiagnostic(diagnosticConsole, {
            event: 'console-error', stage: lastStage,
            ...(match ? diagnosticURL(match[1]!) : {
              origin: 'unknown', path: 'unknown', shape: 'no-access-control-match',
            }),
          });
          unexpectedConsoleMessages.push({
            pathname: pathnameOf(message.location().url),
            type: message.type(),
            text: redactEvidenceText(message.text()),
          });
        },
        onPageError: (error) => {
          if (!isExpectedWebKitInterruptedFetch(engine, error.message)) {
            unexpectedPageErrors.push(error.message);
          }
        },
      })(page);
      watchForFileBytes(page, fileByteViolations);

      stage(`${engine}-sign-in`);
      await loginAsDevelopmentUser(page);

      stage(`${engine}-not-linkedin`);
      await page.goto('/app/import/linkedin');
      await waitForHydration(page);
      await expect(
        page.getByRole('heading', { name: 'Import from LinkedIn' }),
      ).toBeVisible();
      let stopRecording = recordSameOriginRequests(page);
      await pickFile(page, 'other.pdf', pdfBuffer(OTHER_PDF_BASE64));
      await expect(page.locator('[data-import-error]')).toContainText(
        'This is not a LinkedIn profile PDF.',
      );
      let windowRequests = stopRecording();
      expect(windowRequests.every((request) => request.method === 'GET'))
        .toBe(true);

      stage(`${engine}-not-english`);
      stopRecording = recordSameOriginRequests(page);
      await pickFile(
        page, 'localized-vi.pdf', pdfBuffer(LOCALIZED_VI_PDF_BASE64),
      );
      await expect(page.locator('[data-import-error]')).toContainText(
        'This LinkedIn PDF is not in English.',
      );
      windowRequests = stopRecording();
      expect(windowRequests.every((request) => request.method === 'GET'))
        .toBe(true);

      stage(`${engine}-timeout`);
      const before = Date.now();
      await pickFile(page, 'slow.pdf', slowContentPdf());
      await expect(page.locator('[data-import-error]')).toContainText(
        'took longer than 15 seconds',
        { timeout: 90_000 },
      );
      expect(Date.now() - before).toBeGreaterThanOrEqual(TIME_LIMIT_MS);

      stage(`${engine}-cap`);
      await fillToResumeCap(page);
      await page.goto('/app/import/linkedin');
      await waitForHydration(page);
      await expect(page.locator('[data-import-cap]')).toContainText(
        `You have ${RESUME_CAP} resumes.`,
      );
      await expect(
        page.locator('[data-testid="import-file-input"]'),
      ).toHaveCount(0);

      stage(`${engine}-done`);

      expect(fileByteViolations).toEqual([]);
      expect(counters.certificateErrors).toBe(0);
      expect(counters.externalRequests).toBe(0);
      expect(counters.consoleErrors).toBe(0);
      expect(unexpectedPageErrors).toEqual([]);
    } catch (error) {
      const stageAtFailure = lastStage;
      const sourceLine = error instanceof Error
        ? /linkedin-import\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? '')?.[1]
        : undefined;
      if (sourceLine !== undefined) {
        stage(`${engine}-failure-at-line-${sourceLine}`);
      }
      try {
        await writeFile(
          `/evidence/linkedin-import-diagnostic-${engine}.json`,
          `${JSON.stringify({ schemaVersion: 1, console: diagnosticConsole, requests: diagnosticRequests })}\n`,
          { flag: 'wx', mode: 0o600 },
        );
      } catch {
        stage('diagnostic-evidence-write-failed');
      }
      try {
        await writeFailureEvidence(
          `/evidence/linkedin-import-failure-${engine}.json`, stageAtFailure,
          {
            engine,
            thrownError: redactEvidenceText(
              error instanceof Error ? error.message : String(error),
            ),
            unexpectedConsoleMessages,
          },
        );
      } catch {
        stage('failure-evidence-write-failed');
      }
      throw error;
    } finally {
      await context.close();
    }
  });
}
