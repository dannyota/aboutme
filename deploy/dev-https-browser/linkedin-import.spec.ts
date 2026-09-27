/**
 * LinkedIn import browser proof (docs/design/linkedin-import.md, "Tests" and
 * "Browser proof"; docs/design/linkedin-import-ui.md; ADR 0023).
 *
 * One Chromium test proves the case the design's own "Browser proof"
 * paragraph describes: the create dialog's entry link, the app-page CSP,
 * picking a synthetic fixture, the review's contact defaults, the locale and
 * viewport persistence across a live review, deselecting a section, Create,
 * and the imported content landing in the editor, plus the request-log
 * privacy claims (no file bytes on the wire, no external request, and
 * nothing but the one create request between the pick and Create).
 *
 * A second test, run once against Chromium and once against a WebKit
 * browser launched directly (the harness's own project always runs
 * Chromium, so WebKit is opened the way apps/web/e2e/preview-gap.spec.ts
 * does), proves the four cases the design names for both engines: the
 * not-LinkedIn message, the not-English message, the 15-second time limit,
 * and the resume cap.
 */
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
// useResumeList.ts RESUME_CAP; the page reads that constant directly.
const RESUME_CAP = 3;
// read.ts TIME_LIMIT_MS: the fixed cap the page applies to every pick.
const TIME_LIMIT_MS = 15_000;

const recordedResumeIDs = new Set<string>();

// The last stage reached before a failure, so failure evidence can name a
// concrete checkpoint without the withheld browser console (gotchas.md
// "Browser proofs withhold console output").
let lastStage = 'start';
function stage(name: string): void {
  lastStage = name;
  console.log(`linkedin-import-stage:${name}`);
}

// The two possible default titles a create can carry (importCopy en and vi
// "defaultTitle"); a resume under either title that this run never recorded
// is a stray from an earlier failed run whose assertions threw after the
// server had already accepted the create, never one a person made by hand.
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

/** Sets the hidden file input directly: Playwright's setInputFiles works on
 * a hidden input by design, matching how the page only ever opens it
 * through the visible Choose button (docs/design/linkedin-import-ui.md,
 * "Pick state"). */
async function pickFile(page: Page, name: string, buffer: Buffer): Promise<void> {
  await page.locator('[data-testid="import-file-input"]').setInputFiles({
    buffer, mimeType: 'application/pdf', name,
  });
}

/** Fails the test if any request this page ever sends carries the picked
 * file's magic header in its body: the strongest available proof that the
 * file itself never leaves the browser (docs/design/linkedin-import.md,
 * "Privacy"). */
function watchForFileBytes(page: Page, violations: string[]): void {
  page.on('request', (request) => {
    const body = request.postData();
    if (body !== null && body.includes('%PDF-')) {
      violations.push(`${request.method()} ${new URL(request.url()).pathname}`);
    }
  });
}

// The two same-origin polls the app itself starts unprompted, and the only
// ones a same-page navigation is expected to interrupt mid flight (see
// isExpectedWebKitInterruptedFetch below): Nuxt's dev-server build-meta poll
// (buildAssetsURL('builds/meta/<id>.json')) and the app's own capabilities
// read.
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

// A local run of this proof observed WebKit report the capabilities poll's
// interruption with its scheme and one slash dropped ("/localhost:20443/api/
// v1/capabilities due to access control checks." instead of "https://
// localhost:20443/api/v1/capabilities due to access control checks."), which
// new URL cannot parse without a base. isKnownInterruptedFetchToken matches
// that exact shape too, still scoped to this origin's host and the same two
// known pathnames, so an unrelated blocked request is never mistaken for
// this noise.
const ORIGIN_HOST = new URL(ALLOWED_ORIGIN).host;

function isKnownInterruptedFetchToken(token: string): boolean {
  if (isKnownInterruptedFetchURL(token)) return true;
  const hostPrefix = `/${ORIGIN_HOST}`;
  return token.startsWith(hostPrefix)
    && isKnownInterruptedFetchPathname(token.slice(hostPrefix.length));
}

const INTERRUPTED_FETCH_URL = /(\S+) due to access control checks\.$/u;

/** WebKit-only: true for a page error or console error that is the
 * browser's own report of a fetch or worker module import a same-page
 * navigation interrupted mid flight (one of the two known polls above, and
 * pdf.js's per-pick worker churn). Chromium reports the same interruption
 * as a silently dropped abort; WebKit raises it as an uncaught page error
 * (sometimes echoed to the console too) instead, on every engine this proof
 * runs, not a LinkedIn import defect. Narrowed to the known polls' URLs so
 * an unrelated blocked request is never mistaken for this noise. */
function isExpectedWebKitInterruptedFetch(
  engine: 'chromium' | 'webkit',
  text: string,
): boolean {
  if (engine !== 'webkit') return false;
  if (text === 'TypeError: Importing a module script failed.') return true;
  const match = INTERRUPTED_FETCH_URL.exec(text);
  return match !== null && isKnownInterruptedFetchToken(match[1]!);
}

// isExpectedAnonymousMeConsole (network-policy.ts) matches only Chromium's
// exact wording for a failed fetch's status line, which leaves the status
// text empty ("...status of 401 ()"). A local WebKit run of this proof
// showed the same single anonymous /api/v1/me read loginAsDevelopmentUser
// triggers before sign-in completes reported with the status text filled in
// instead ("...status of 401 (Unauthorized)"): the same expected event, in
// WebKit's own wording, not a second, new failure.
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

/** Starts recording every same-origin request; the returned function stops
 * recording and returns what it saw, for "no request happens between the
 * pick and Create other than those the design allows"
 * (docs/design/linkedin-import.md, "Tests"). */
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

/** Creates only as many blank resumes as the signed-in account still needs
 * to reach the cap, so a leftover resume from an unrelated proof never
 * makes this test create (or delete) more than it must; every created id is
 * recorded for the shared cleanup hook immediately, before the loop
 * continues. */
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
}

// Resume ids are UUIDs; the Nuxt build-meta poll's id is an opaque hash
// segment ending in ".json" (isKnownInterruptedFetchPathname above). Both
// are replaced so failure evidence never carries an identifier that could
// be looked up against the harness (same bar as exports.spec.ts's
// redactSavePath).
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

// pathnameOf never throws: failure evidence is best-effort, and a console
// message's own location URL is not guaranteed to parse.
function pathnameOf(url: string): string {
  try {
    return redactPathname(new URL(url).pathname);
  } catch {
    return '';
  }
}

// --- The core create flow (Chromium) ----------------------------------------

test('entry link, review, and create land the imported content in the editor', async ({
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  const fileByteViolations: string[] = [];
  await pinEnglish(context);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  // Every test here starts signed out, like entry.spec.ts: the one expected
  // console failure is the anonymous /api/v1/me read loginAsDevelopmentUser
  // triggers before sign-in completes.
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
    // ADR 0023 decision 4; docs/design/linkedin-import.md "Security": the
    // app page policy is the only one carrying worker-src 'self'.
    expect(importResponse.headers()['content-security-policy'])
      .toContain('worker-src \'self\'');
    steps.cspWorkerSrc = true;

    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Import from LinkedIn' }),
    ).toBeVisible();

    stage('create-pick-window-start');
    // Picked while the interface is still English, so the review's default
    // title (set once, at the pick, from the active locale's copy) is the
    // English string the create assertion below expects; the locale switch
    // that follows changes displayed copy only, never that already-set
    // title (docs/design/linkedin-import-ui.md, "Review state").
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
    // Email and phone start unchecked; the LinkedIn URL and the website
    // start checked (docs/design/linkedin-import.md, Owner approval I5).
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
    // The switch above changed only displayed copy, never the file result
    // or the selections (docs/design/linkedin-import-ui.md, "States"); the
    // still-unchecked email checkbox is the same proof in the Vietnamese
    // render.
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
    // The id is recorded for cleanup before any content assertion below can
    // throw: a server that already accepted the create must never leave an
    // untracked resume on the shared development account just because a
    // later check in this test fails.
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
    // No worker rearms during a successful pick (linkedin.vue's pick(): the
    // worker already armed on page load is consumed, and a fresh one is
    // only armed after a failed pick or Choose another PDF, neither of
    // which happens on this path), so the create request is the only data
    // request in this whole window (docs/design/linkedin-import.md, "Reading
    // the file": no request happens between a pick that reaches the review
    // and Create). The editor page's own module fetches can start landing in
    // this same window once the browser's post-create navigation begins;
    // `/_nuxt/` is asset traffic the dev server serves on demand, the same
    // classification second-factor.spec.ts already gives that path prefix,
    // not a second data request. Only a GET is ever asset traffic, so the
    // drop is scoped to GET and every dropped request is proven to be one.
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
    // The evidence keeps the named stage; the console gets the exact source
    // line too (exports.spec.ts), so this test's own failure is never lost
    // behind a later test's stage line in the shared console log.
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

// --- Messages, the time limit, and the cap, in both engines -----------------

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
    // The chromium fixture's own browser carries the config's baseURL to any
    // context it creates. A directly launched WebKit browser has no baseURL,
    // so it needs one explicitly; it needs no certificate bypass, because
    // run.sh overlays the image's system trust bundle with the harness's
    // exported root read-only for this mode (WebKit validates against that
    // bundle, not the NSS database the certutil import prepares for
    // Chromium), so WebKit trusts the same root Chromium does.
    const context = await source.newContext(
      engine === 'webkit' ? { baseURL: ORIGIN } : {},
    );
    try {
      await pinEnglish(context);
      await installExternalRequestFirewall(context, counters);
      await installExternalWebSocketFirewall(context, counters);
      const page = await context.newPage();
      // Same reasoning as the create-flow test above: filter the expected
      // anonymous /api/v1/me read at sign-in. Page errors keep the shared
      // counter (other proofs read it the same way) but this proof checks
      // its own filtered list, so a WebKit-only interrupted-fetch report
      // never fails the check.
      pageDiagnosticsAttacher(counters, {
        countConsoleError: (message) =>
          !isExpectedAnonymousMeConsoleAnyWording(
            engine, message.text(), message.location().url,
          )
          && !isExpectedWebKitInterruptedFetch(engine, message.text()),
        onCountedConsoleError: (message) => {
          unexpectedConsoleMessages.push({
            pathname: pathnameOf(message.location().url),
            type: message.type(),
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
      // A failed pick arms a fresh worker for the next one before Pick
      // shows again (docs/design/linkedin-import.md, "Reading the file"),
      // so this window may show one more same-origin GET than the create
      // flow's window does; it must still never mutate and never leave the
      // origin (the external-request firewall already covers the latter).
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

      // Inside the try, not after it: a failure here must reach the catch
      // below the same way a failure earlier in this test does, so it too
      // gets a failure-evidence record naming the last stage, the engine,
      // and every unexpected console message this run actually saw.
      expect(fileByteViolations).toEqual([]);
      expect(counters.certificateErrors).toBe(0);
      expect(counters.externalRequests).toBe(0);
      expect(counters.consoleErrors).toBe(0);
      expect(unexpectedPageErrors).toEqual([]);
    } catch (error) {
      // Same reasoning as the create-flow test's catch block: keep the named
      // stage in the evidence and add the exact source line to the console.
      const stageAtFailure = lastStage;
      const sourceLine = error instanceof Error
        ? /linkedin-import\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? '')?.[1]
        : undefined;
      if (sourceLine !== undefined) {
        stage(`${engine}-failure-at-line-${sourceLine}`);
      }
      try {
        await writeFailureEvidence(
          `/evidence/linkedin-import-failure-${engine}.json`, stageAtFailure,
          { engine, unexpectedConsoleMessages },
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
