import {
  expect,
  test,
  type BrowserContext,
  type Page,
} from '@playwright/test';
import { writeFile } from 'node:fs/promises';

import {
  createBlankResume,
  deleteRecordedResume,
  freshCSRF,
  loginAsDevelopmentUser,
  uniqueTitle,
} from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pinEnglish,
  signInWithGoogle,
  startLinkedInAuthorize,
  waitForHydration,
} from './harness-lib';
import {
  ALLOWED_ORIGIN,
  httpFailureStatus,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/public-proof.json';
const SCHEMA_VERSION = '5';
const CUSTOM_LINK = 'https://orcid.example/0000-0001';
const PAGE_TITLE = 'Danny from aboutme.vn';
const PAGE_EMOJI = '\u{1F680}';
const PDF_NAME = 'Public-proof-resume-Resume.pdf';
// The fixture adds no profile section, so the description falls back to the
// job title and employer joined by a comma (docs/design/link-previews.md,
// "Text rules", fallback 2). The image text is the scrubbed full name alone,
// since the fixture sets no headline.
const PUBLIC_PROOF_DESCRIPTION = 'Engineer, Example Corp';
const PUBLIC_PROOF_IMAGE_ALT = 'Public proof resume';
let createdID: string | undefined;

let currentStage = 'none';

function stage(name: string): void {
  currentStage = name;
  console.log('public-stage:' + name);
}

test.beforeEach(() => {
  currentStage = 'none';
});

// run.sh prints only bounded stage lines, and a later test's stages would
// hide an earlier failure, so each failing test prints one closed line naming
// its stage. The stage names are fixed words in this file; the outcome word
// is the only thing derived from the error, and never its text.
test.afterEach(({}, testInfo) => {
  if (testInfo.status === 'skipped') return;
  if (testInfo.status === testInfo.expectedStatus) return;
  const name = currentStage.toLowerCase().replace(/[^a-z0-9]+/gu, '-')
    .slice(0, 48);
  const outcome = testInfo.status === 'timedOut' ? 'timeout' : 'assertion';
  console.log(`public-stage:fail-${name}-${outcome}`);
});

// One head meta element, by its naming attribute's value (for example
// "og:title") and its decoded content.
interface HeadMetaTag {
  readonly key: string;
  readonly value: string;
}

// The preview meta elements the page head must carry, in the order
// docs/design/link-previews.md, "Page head", requires.
const LINK_PREVIEW_KEYS: readonly string[] = [
  'description',
  'og:type',
  'og:site_name',
  'og:title',
  'og:description',
  'og:url',
  'og:locale',
  'og:image',
  'og:image:type',
  'og:image:width',
  'og:image:height',
  'og:image:alt',
  'twitter:card',
  'twitter:image',
  'twitter:image:alt',
];

// Tags the page head must never carry: X and Discord fall back to Open Graph,
// and no tag may tint the mobile browser bar.
const FORBIDDEN_HEAD_KEYS: readonly string[] = [
  'twitter:title',
  'twitter:description',
  'theme-color',
];

// decodeHeadAttribute reverses the escaping the renderer applies to a
// double-quoted attribute value. &amp; decodes last, so an already-escaped
// entity such as &amp;lt; is not doubly unescaped.
function decodeHeadAttribute(value: string): string {
  return value
    .replaceAll('&quot;', '"')
    .replaceAll('&#39;', '\'')
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&amp;', '&');
}

// headMetaTags lists every name= or property= meta element inside the page's
// <head>, in document order, with its content decoded.
function headMetaTags(html: string): HeadMetaTag[] {
  const head = /<head[^>]*>([\s\S]*?)<\/head>/iu.exec(html)?.[1] ?? '';
  const pattern = /<meta\s+(?:name|property)="([^"]*)"\s+content="([^"]*)"\s*\/?>/gu;
  const tags: HeadMetaTag[] = [];
  for (const match of head.matchAll(pattern)) {
    tags.push({ key: match[1], value: decodeHeadAttribute(match[2]) });
  }
  return tags;
}

// expectLinkPreviewHead checks the page's raw HTML against every rule in
// docs/design/link-previews.md, "Page head": each expected element appears
// once, in order, with the exact value, and the forbidden ones never appear.
// A failure names only the closed tag key, never the resume-derived value.
function expectLinkPreviewHead(
  html: string,
  expected: readonly HeadMetaTag[],
): void {
  const tags = headMetaTags(html);
  for (const key of FORBIDDEN_HEAD_KEYS) {
    if (tags.some((tag) => tag.key === key)) {
      throw new Error(`head-forbidden-${key}`);
    }
  }
  const present = tags.filter((tag) => LINK_PREVIEW_KEYS.includes(tag.key));
  if (present.length !== expected.length) {
    throw new Error(`head-count-${present.length}`);
  }
  const seen = new Set<string>();
  for (const [index, want] of expected.entries()) {
    const got = present[index];
    if (got === undefined || got.key !== want.key) {
      throw new Error(`head-order-${want.key}`);
    }
    if (got.value !== want.value) {
      throw new Error(`head-mismatch-${want.key}`);
    }
    if (seen.has(got.key)) throw new Error(`head-duplicate-${got.key}`);
    seen.add(got.key);
  }
}

// The stored preview card's byte cap (docs/design/link-previews.md, "Preview
// card").
const CARD_MAX_BYTES = 524_288;

interface FetchedImage {
  readonly body: number[];
  readonly status: number;
  readonly type: string | null;
}

async function fetchImage(page: Page, url: string): Promise<FetchedImage> {
  return page.evaluate(async (href) => {
    const response = await fetch(href, { cache: 'no-store', credentials: 'omit' });
    return {
      body: Array.from(new Uint8Array(await response.arrayBuffer())),
      status: response.status,
      type: response.headers.get('content-type'),
    };
  }, url);
}

// Reads a PNG's IHDR width and height (big-endian, at byte offsets 16 and
// 20; docs/design/link-previews.md pins every card to 1200 by 630).
function expectPNGSize(body: readonly number[]): void {
  const bytes = Uint8Array.from(body);
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  expect(view.getUint32(16)).toBe(1200);
  expect(view.getUint32(20)).toBe(630);
}

test.afterEach(async ({ browser }, testInfo) => {
  if (createdID === undefined) return;
  testInfo.setTimeout(testInfo.timeout + 30_000);
  const cleanupID = createdID;
  const cleanupCounters = newDiagnosticCounters();
  try {
    const cleanupContext = await browser.newContext();
    await installExternalRequestFirewall(cleanupContext, cleanupCounters);
    await installExternalWebSocketFirewall(cleanupContext, cleanupCounters);
    await pinEnglish(cleanupContext);
    try {
      const cleanupPage = await cleanupContext.newPage();
      await loginAsDevelopmentUser(cleanupPage);
      await deleteRecordedResume(cleanupPage, cleanupID);
      expect(cleanupCounters.externalRequests).toBe(0);
      createdID = undefined;
    } finally {
      await cleanupContext.close();
    }
  } catch (error) {
    stage('cleanup-failed');
    throw error;
  }
});

// --- Sign in to view (docs/design/viewer-analytics/sign-in-to-view.md) ------

const SEVEN_DAYS_SECONDS = 7 * 24 * 60 * 60;
const JOIN_INVITE_FLOOR_MS = 5_000;
const JOIN_INVITE_CLOSED_KEY = 'aboutme.joinInvite.closedAt';
const VIEW_PASS_COOKIE = '__Host-view-pass';

// What the sign-in-to-view test proved. The hydration test writes the run's
// evidence, so it refuses to write until every step here is true.
const viewSteps = {
  gate: false,
  gatedRoutes: false,
  googlePass: false,
  linkedinPass: false,
  passCookie: false,
  ownerGated: false,
  joinInvite: false,
  joinInviteClosed: false,
  switchOff: false,
  viewCleanup: false,
};
let joinInviteMs = 0;

interface GatedStatuses {
  readonly json: number;
  readonly pdf: number;
  readonly start: number;
  readonly live: number;
}

// gatedStatuses reads the gated public routes from a page whose CSP allows
// same-origin fetches. The live stream is skipped when `withLive` is false,
// because a served stream stays open.
async function gatedStatuses(
  page: Page,
  slug: string,
  withLive: boolean,
): Promise<GatedStatuses> {
  return page.evaluate(async (input) => {
    const get = async (path: string): Promise<number> => (
      await fetch(path, { cache: 'no-store' })
    ).status;
    const start = await fetch(
      `/api/v1/public/resumes/${input.slug}/views/start`,
      {
        method: 'POST',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      },
    );
    return {
      json: await get(`/api/v1/public/resumes/${input.slug}`),
      pdf: await get(`/api/v1/public/resumes/${input.slug}/pdf`),
      start: start.status,
      live: input.withLive ? await get(`/api/v1/live/${input.slug}`) : 0,
    };
  }, { slug, withLive });
}

// expectPassCookie checks the pass cookie's attributes and size without ever
// recording its value (design "Pass cookie"; AC-VIEW-007), and that the
// sign-in made no account session (AC-VIEW-005).
async function expectPassCookie(context: BrowserContext): Promise<void> {
  const cookies = await context.cookies(ORIGIN);
  const passes = cookies.filter((cookie) => cookie.name === VIEW_PASS_COOKIE);
  expect(passes).toHaveLength(1);
  const pass = passes[0]!;
  expect(pass.httpOnly).toBe(true);
  expect(pass.secure).toBe(true);
  expect(pass.sameSite).toBe('Lax');
  expect(pass.path).toBe('/');
  expect(pass.value.length).toBeLessThan(1024);
  const remaining = pass.expires - Date.now() / 1000;
  expect(remaining).toBeGreaterThan(SEVEN_DAYS_SECONDS - 3_600);
  expect(remaining).toBeLessThanOrEqual(SEVEN_DAYS_SECONDS + 60);
  expect(cookies.map((cookie) => cookie.name)).not.toContain('__Host-session');
}

async function readResumeRevision(page: Page, id: string): Promise<string> {
  return page.evaluate(async (resumeID) => {
    const response = await fetch(`/api/v1/resumes/${resumeID}`, {
      credentials: 'include',
      cache: 'no-store',
    });
    const body = await response.json() as { data?: { revision?: unknown } };
    const revision = body.data?.revision;
    if (response.status !== 200 || typeof revision !== 'string') {
      throw new Error('resume read failed');
    }
    return revision;
  }, id);
}

// publishWithSignIn publishes the resume live at its current revision with
// the sign-in switch set as given (downloads on, discovery off).
async function publishWithSignIn(
  page: Page,
  id: string,
  slug: string,
  signInToView: boolean,
): Promise<{ status: number; body: unknown }> {
  const revision = await readResumeRevision(page, id);
  const csrf = await freshCSRF(page);
  return page.evaluate(async (input) => {
    const response = await fetch(`/api/v1/resumes/${input.id}/publish`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        'Idempotency-Key': crypto.randomUUID(),
        'If-Match': `"r${input.revision}"`,
        'X-CSRF-Token': input.csrf,
        'X-Resume-Schema-Version': input.schemaVersion,
      },
      body: JSON.stringify({
        slug: input.slug,
        live: true,
        downloadEnabled: true,
        seoGeoEnabled: false,
        publicTitle: input.publicTitle,
        faviconEmoji: input.faviconEmoji,
        signInToView: input.signInToView,
      }),
    });
    return { status: response.status, body: await response.json().catch(() => null) };
  }, {
    id,
    slug,
    revision,
    csrf,
    schemaVersion: SCHEMA_VERSION,
    publicTitle: PAGE_TITLE,
    faviconEmoji: PAGE_EMOJI,
    signInToView,
  });
}

test('proves sign in to view gates a resume, admits a pass holder, and '
  + 'opens again when switched off', async ({ browser, page }) => {
  test.setTimeout(240_000);
  const slug = `gate-${crypto.randomUUID().slice(0, 8)}`;
  const counters = newDiagnosticCounters();
  // Set right after the resume is deleted: only then is a 404 for the page
  // document itself expected.
  let deleted = false;
  // Gated routes answer 404 to an anonymous fetch on purpose, so the
  // browser's own console line for those exact reads is expected.
  const attach = pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) => {
      if (httpFailureStatus(message.text()) !== 404) return true;
      let path = '';
      try {
        path = new URL(message.location().url).pathname;
      } catch {
        return true;
      }
      return !(path.startsWith(`/api/v1/public/resumes/${slug}`)
        || path === `/api/v1/live/${slug}`
        || (deleted && path === `/${slug}`));
    },
  });
  const openContext = async (
    viewport?: { width: number; height: number },
  ): Promise<{ context: BrowserContext; page: Page }> => {
    const context = await browser.newContext(viewport ? { viewport } : {});
    await installExternalRequestFirewall(context, counters);
    await installExternalWebSocketFirewall(context, counters);
    const opened = await context.newPage();
    attach(opened);
    return { context, page: opened };
  };
  const gateLink = (target: Page, provider: string) => target.locator(
    `a.gate-provider[href="/api/v1/auth/${provider}/start?purpose=view&slug=${slug}"]`,
  );
  stage('view-locale');
  await pinEnglish(page.context());
  stage('view-sign-in');
  await loginAsDevelopmentUser(page);
  stage('view-create-resume');
  const created = await createBlankResume(
    page,
    uniqueTitle(),
    (createStage) => {
      stage('view-create-' + createStage);
    },
    (accepted) => {
      createdID = accepted.metadata.id;
    },
  );
  const resumeID = created.metadata.id;
  createdID = resumeID;

  stage('view-personal-details');
  await page.locator('[data-action="open-document"]').press('Enter');
  await page.locator('[data-field="fullName"] [data-field-input]')
    .fill('Public proof resume');
  await page.locator('[data-field="fullName"] [data-field-input]').press('Tab');
  await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');
  stage('view-work-entry');
  await page.locator('[data-action="open-structure"]').press('Enter');
  await page.locator('[data-action="section-type"]').selectOption('work');
  await page
    .getByTestId('section-create-form')
    .locator('[data-action="create"]')
    .press('Enter');
  await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');
  await page.locator('[data-outline-key="work"]').press('Enter');
  await page.locator('[data-action="add-entry"]').press('Enter');
  const entry = page.locator('[data-entry-id]').first();
  await entry.locator('[data-entry-field="jobTitle"] [data-field-input]')
    .fill('Engineer');
  await entry.locator('[data-entry-field="jobTitle"] [data-field-input]').press('Tab');
  await entry.locator('[data-entry-field="employer"] [data-field-input]')
    .fill('Example Corp');
  await entry.locator('[data-entry-field="employer"] [data-field-input]').press('Tab');
  await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');

  stage('view-publish-gated');
  const gatedPublish = await publishWithSignIn(page, resumeID, slug, true);
  expect(gatedPublish.status, JSON.stringify(gatedPublish.body)).toBe(200);

  // A phone-sized window, short enough that the resume scrolls, so the
  // invite proof below sees the bottom bar and the scroll rule.
  const anonymous = await openContext({ width: 390, height: 400 });
  const anon = anonymous.page;
  let startResponseAt = 0;
  let startBody: Promise<unknown> = Promise.resolve(null);
  anon.on('response', (response) => {
    if (response.request().method() !== 'POST') return;
    const url = new URL(response.url());
    if (url.pathname !== `/api/v1/public/resumes/${slug}/views/start`) return;
    startResponseAt = Date.now();
    startBody = response.json().catch(() => null);
  });

  stage('view-gate');
  const gateResponse = await anon.goto(`${ORIGIN}/${slug}`);
  expect(gateResponse?.status()).toBe(200);
  const gateHeaders = gateResponse?.headers() ?? {};
  expect(gateHeaders['cache-control']).toContain('no-store');
  expect(gateHeaders['x-robots-tag']).toBe('noindex, noarchive');
  expect(gateHeaders['content-security-policy']).toContain("default-src 'none'");
  expect(await gateResponse?.text() ?? '').not.toMatch(/<script/iu);
  stage('view-gate-body');
  await expect(anon).toHaveTitle(PAGE_TITLE);
  await expect(anon.locator('#public-gate h1')).toHaveText(PAGE_TITLE);
  await expect(anon.locator('#public-resume')).toHaveCount(0);
  // The gate stays light whatever the owner's color scheme
  // (docs/design/public-page-theme.md, "What stays light").
  await expect(anon.locator('[data-color-scheme]')).toHaveCount(0);
  await expect(anon.locator('script')).toHaveCount(0);
  const gateBody = await anon.evaluate(() => document.body.innerHTML);
  expect(gateBody).not.toContain('Public proof resume');
  expect(gateBody).not.toContain('Example Corp');
  stage('view-gate-providers');
  // Google and LinkedIn are offered; GitHub never is.
  await expect(anon.locator('a.gate-provider')).toHaveCount(2);
  await expect(gateLink(anon, 'google')).toHaveCount(1);
  await expect(gateLink(anon, 'linkedin')).toHaveCount(1);
  await expect(anon.locator('.gate-message')).toHaveCount(0);
  expect((await anonymous.context.cookies(ORIGIN)).map((cookie) => cookie.name))
    .not.toContain(VIEW_PASS_COOKIE);
  stage('view-gate-query');
  // Only the two closed ?signin= values reach the page.
  await anon.goto(`${ORIGIN}/${slug}?signin=cancelled`);
  await expect(anon.locator('.gate-message')).toHaveCount(1);
  await anon.goto(`${ORIGIN}/${slug}?signin=other`);
  await expect(anon.locator('.gate-message')).toHaveCount(0);
  viewSteps.gate = true;

  stage('view-gated-routes');
  await anon.goto(`${ORIGIN}/guide/mcp`);
  expect(await gatedStatuses(anon, slug, true)).toEqual({
    json: 404,
    pdf: 404,
    start: 404,
    live: 404,
  });
  viewSteps.gatedRoutes = true;

  stage('view-owner-gated');
  // Public routes never read the account session: the signed-in owner gets
  // the gate like any viewer (AC-VIEW-006).
  const ownerGate = await page.goto(`${ORIGIN}/${slug}`);
  expect(ownerGate?.status()).toBe(200);
  await expect(page.locator('#public-gate')).toHaveCount(1);
  await expect(page.locator('#public-resume')).toHaveCount(0);
  await page.goto('/app/resumes');
  viewSteps.ownerGated = true;

  stage('view-google');
  await anon.goto(`${ORIGIN}/${slug}`);
  await signInWithGoogle(anon, {
    activator: gateLink(anon, 'google'),
    returnPath: `/${slug}`,
  });
  // The invite proof runs first: its 5 s floor counts from view start, so
  // slower checks wait until it is done.
  stage('view-join-invite');
  // The invite never shows in the first 5 s after view start, then shows
  // for this pass holder once the scroll or dwell rule is met
  // (design "Join invite"; AC-VIEW-008). The window is scrolled to the
  // bottom at once, so only the 5 s floor holds it back.
  await expect.poll(() => startResponseAt).toBeGreaterThan(0);
  const startedAt = startResponseAt;
  const invite = anon.getByRole('region', {
    name: /^(Create a free resume invite|Lời mời tạo CV miễn phí)$/u,
  });
  await expect(invite).toHaveCount(0);
  expect(Date.now() - startedAt, 'invite-floor-window-missed').toBeLessThan(4_000);
  await anon.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect(invite).toBeVisible({ timeout: 45_000 });
  joinInviteMs = Date.now() - startedAt;
  expect(joinInviteMs).toBeGreaterThanOrEqual(JOIN_INVITE_FLOOR_MS - 100);
  expect(await startBody).toMatchObject({
    data: { owner: false, signedIn: false },
  });
  await expect(invite).toHaveAttribute('data-placement', 'bar');
  const inviteLink = invite.getByRole('link');
  await expect(inviteLink).toHaveAttribute('href', /^\/(register|login)$/u);
  viewSteps.joinInvite = true;

  stage('view-join-invite-close');
  await invite.getByRole('button', { name: /^(Close|Đóng)$/u }).click();
  await expect(invite).toHaveCount(0);
  expect(await anon.evaluate(
    (key) => localStorage.getItem(key) !== null,
    JOIN_INVITE_CLOSED_KEY,
  )).toBe(true);
  startResponseAt = 0;
  await anon.reload();
  await expect.poll(() => startResponseAt).toBeGreaterThan(0);
  // Wait past the 5 s floor: a closed invite must not come back.
  await anon.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await anon.waitForTimeout(JOIN_INVITE_FLOOR_MS + 1_000);
  await expect(invite).toHaveCount(0);
  viewSteps.joinInviteClosed = true;

  stage('view-google-pass');
  await expect(anon.locator('#public-resume')).toBeVisible();
  await expect(anon.locator('#public-gate')).toHaveCount(0);
  await waitForHydration(anon, 'public-resume');
  await expectPassCookie(anonymous.context);
  viewSteps.passCookie = true;
  const held = await gatedStatuses(anon, slug, false);
  expect(held.json).toBe(200);
  expect(held.pdf).toBe(200);
  viewSteps.googlePass = true;

  stage('view-linkedin');
  const second = await openContext();
  const viaLinkedIn = second.page;
  await viaLinkedIn.goto(`${ORIGIN}/${slug}`);
  await expect(viaLinkedIn.locator('#public-gate')).toHaveCount(1);
  await startLinkedInAuthorize(viaLinkedIn, gateLink(viaLinkedIn, 'linkedin'));
  await Promise.all([
    viaLinkedIn.waitForURL((url) =>
      url.origin === ORIGIN && url.pathname === `/${slug}`
    ),
    viaLinkedIn.getByRole('button', { name: 'Allow', exact: true }).click(),
  ]);
  await expect(viaLinkedIn.locator('#public-resume')).toBeVisible();
  await expect(viaLinkedIn.locator('#public-gate')).toHaveCount(0);
  await expectPassCookie(second.context);
  viewSteps.linkedinPass = true;

  stage('view-switch-off');
  const opened = await publishWithSignIn(page, resumeID, slug, false);
  expect(opened.status, JSON.stringify(opened.body)).toBe(200);
  const open = await openContext();
  const publicResponse = await open.page.goto(`${ORIGIN}/${slug}`);
  expect(publicResponse?.status()).toBe(200);
  await expect(open.page.locator('#public-resume')).toBeVisible();
  await expect(open.page.locator('#public-gate')).toHaveCount(0);
  const publicNow = await gatedStatuses(open.page, slug, false);
  expect(publicNow.json).toBe(200);
  expect(publicNow.pdf).toBe(200);
  viewSteps.switchOff = true;

  stage('view-cleanup');
  // Close every viewer first: an open page reloads to the deleted slug when
  // its resume goes away, which is noise this check does not judge.
  await anonymous.context.close();
  await second.context.close();
  await open.context.close();
  await deleteRecordedResume(page, resumeID);
  createdID = undefined;
  deleted = true;
  stage('view-cleanup-status');
  const after = await openContext();
  const gone = await after.page.goto(`${ORIGIN}/${slug}`);
  expect(gone?.status()).toBe(404);
  await after.context.close();
  viewSteps.viewCleanup = true;

  stage('view-cleanup-diagnostics');
  expect(counters.consoleErrors).toBe(0);
  expect(counters.pageErrors).toBe(0);
  expect(counters.externalRequests).toBe(0);
  expect(counters.certificateErrors).toBe(0);
});

test('proves a published resume hydrates in a real browser', async ({
  browser,
  page,
}) => {
  const consoleErrors: string[] = [];
  const dialogs: string[] = [];
  const pageErrors: string[] = [];
  const externalRequests: string[] = [];
  const privateRequests: string[] = [];

  const attachDiagnostics = (openedPage: typeof page): void => {
    openedPage.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text());
    });
    openedPage.on('dialog', async (dialog) => {
      dialogs.push(`${dialog.type()}:${dialog.message()}`);
      await dialog.dismiss();
    });
    openedPage.on('pageerror', (error) => pageErrors.push(error.message));
  };

  let publishedSlug: string | undefined;

  {
    createdID = undefined;
    stage('locale');
    await pinEnglish(page.context());
    stage('sign-in');
    await loginAsDevelopmentUser(page);
    stage('create-resume');
    const created = await createBlankResume(
      page,
      uniqueTitle(),
      (createStage) => {
        stage('create-' + createStage);
      },
      (accepted) => {
        createdID = accepted.metadata.id;
      },
    );
    createdID = created.metadata.id;
    publishedSlug = `public-${crypto.randomUUID().slice(0, 8)}`;

    // Publish requires at least a full name; fill it and wait for the autosave.
    stage('personal-details');
    await page.locator('[data-action="open-document"]').press('Enter');
    await page.locator('[data-field="fullName"] [data-field-input]')
      .fill('Public proof resume');
    await page.locator('[data-field="fullName"] [data-field-input]').press('Tab');
    await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');

    // Add one work section and entry, so the resume meets the publish
    // completeness minimum (a full name plus at least one visible entry).
    stage('work-entry');
    await page.locator('[data-action="open-structure"]').press('Enter');
    await page.locator('[data-action="section-type"]').selectOption('work');
    await page
      .getByTestId('section-create-form')
      .locator('[data-action="create"]')
      .press('Enter');
    await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');
    await page.locator('[data-outline-key="work"]').press('Enter');
    await page.locator('[data-action="add-entry"]').press('Enter');
    const entry = page.locator('[data-entry-id]').first();
    await entry.locator('[data-entry-field="jobTitle"] [data-field-input]')
      .fill('Engineer');
    await entry.locator('[data-entry-field="jobTitle"] [data-field-input]').press('Tab');
    await entry.locator('[data-entry-field="employer"] [data-field-input]')
      .fill('Example Corp');
    await entry.locator('[data-entry-field="employer"] [data-field-input]').press('Tab');
    await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');

    // The autosave advanced the revision; read the current one for If-Match.
    stage('read-revision');
    const currentRevision = await page.evaluate(async (id) => {
      const response = await fetch(`/api/v1/resumes/${id}`, {
        credentials: 'include',
        cache: 'no-store',
      });
      const body = await response.json() as { data?: { revision?: unknown } };
      const revision = body.data?.revision;
      if (response.status !== 200 || typeof revision !== 'string') {
        throw new Error('resume read failed');
      }
      return revision;
    }, createdID);

    // A custom https contact with discovery on: the Go validator requires the
    // renderer's JSON-LD sameAs to match its own, custom links included.
    stage('write-contacts');
    const detailCSRF = await freshCSRF(page);
    const detailWrite = await page.evaluate(async (input) => {
      const response = await fetch(`/api/v1/resumes/${input.id}/personal-details`, {
        method: 'PATCH',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': `"r${input.revision}"`,
          'X-CSRF-Token': input.csrf,
          'X-Resume-Schema-Version': input.schemaVersion,
        },
        body: JSON.stringify({
          fullName: 'Public proof resume',
          details: [{
            id: crypto.randomUUID(),
            type: 'custom',
            label: 'ORCID',
            value: input.customLink,
            isHidden: false,
          }, {
            id: crypto.randomUUID(),
            type: 'email',
            value: 'proof@example.com',
            isHidden: false,
          }, {
            id: crypto.randomUUID(),
            type: 'phone',
            value: '(+84) 374837720',
            isHidden: false,
          }],
        }),
      });
      const body = await response.json() as { data?: { revision?: unknown } };
      return { status: response.status, revision: body.data?.revision };
    }, {
      id: createdID,
      revision: currentRevision,
      csrf: detailCSRF,
      schemaVersion: SCHEMA_VERSION,
      customLink: CUSTOM_LINK,
    });
    expect(detailWrite.status).toBe(200);
    expect(typeof detailWrite.revision).toBe('string');

    // The owner picks the dark scheme. Only a v5 client can write it
    // (docs/design/public-page-theme.md); the public page below must carry it.
    stage('write-color-scheme');
    const schemeCSRF = await freshCSRF(page);
    const schemeWrite = await page.evaluate(async (input) => {
      const response = await fetch(`/api/v1/resumes/${input.id}/customization`, {
        method: 'PATCH',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': `"r${input.revision}"`,
          'X-CSRF-Token': input.csrf,
          'X-Resume-Schema-Version': input.schemaVersion,
        },
        body: JSON.stringify({
          deltas: [{ op: 'set', path: 'colorScheme', value: 'dark' }],
        }),
      });
      const body = await response.json().catch(() => null) as
        { data?: { revision?: unknown } } | null;
      return { status: response.status, revision: body?.data?.revision, body };
    }, {
      id: createdID,
      revision: detailWrite.revision as string,
      csrf: schemeCSRF,
      schemaVersion: SCHEMA_VERSION,
    });
    expect(schemeWrite.status, JSON.stringify(schemeWrite.body)).toBe(200);
    expect(typeof schemeWrite.revision).toBe('string');

    // Publish: the resume must already hold a live slug before any public
    // route will serve it.
    stage('publish');
    const csrf = await freshCSRF(page);
    const publishStatus = await page.evaluate(async (input) => {
      const response = await fetch(`/api/v1/resumes/${input.id}/publish`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': `"r${input.revision}"`,
          'X-CSRF-Token': input.csrf,
          'X-Resume-Schema-Version': input.schemaVersion,
        },
        body: JSON.stringify({
          slug: input.slug,
          live: true,
          downloadEnabled: true,
          seoGeoEnabled: true,
          publicTitle: input.publicTitle,
          faviconEmoji: input.faviconEmoji,
        }),
      });
      const body = await response.json().catch(() => null);
      return { status: response.status, body };
    }, {
      id: createdID,
      revision: schemeWrite.revision as string,
      csrf,
      slug: publishedSlug,
      schemaVersion: SCHEMA_VERSION,
      publicTitle: PAGE_TITLE,
      faviconEmoji: PAGE_EMOJI,
    });
    expect(publishStatus.status, JSON.stringify(publishStatus.body)).toBe(200);

    // Prove the page in a fresh context with no session cookies.
    stage('public-context');
    const publicContext = await browser.newContext();
    const publicPage = await publicContext.newPage();
    attachDiagnostics(publicPage);
    // A signed-out public page may call only public endpoints, and none may
    // answer as if a session were expected.
    publicPage.on('response', (publicResponse) => {
      const url = new URL(publicResponse.url());
      if (url.origin !== ORIGIN) return;
      if (url.pathname.startsWith('/api/')
        && !url.pathname.startsWith('/api/v1/public/')
        && !url.pathname.startsWith('/api/v1/live/')) {
        privateRequests.push(url.pathname);
      }
      if (publicResponse.status() === 401 || publicResponse.status() === 403) {
        privateRequests.push(`${publicResponse.status()} ${url.pathname}`);
      }
    });
    await publicContext.route('**/*', async (route) => {
      const url = new URL(route.request().url());
      if (!isAllowedHTTPURL(url.href) && !isAllowedWebSocketURL(url.href)) {
        externalRequests.push(url.href);
        await route.abort('blockedbyclient');
        return;
      }
      await route.continue();
    });

    stage('public-navigation');
    const response = await publicPage.goto(`${ORIGIN}/${publishedSlug}`);
    expect(response?.status()).toBe(200);
    // The raw response body carries the server-rendered head before any
    // client script can touch it, in the document order the renderer wrote.
    const publicHTML = await response?.text() ?? '';
    const structured = await publicPage
      .locator('script[type="application/ld+json"]')
      .textContent();
    expect(JSON.parse(structured ?? '{}').mainEntity?.sameAs).toEqual([CUSTOM_LINK]);
    await expect(publicPage.locator(`a[href="${CUSTOM_LINK}"]`)).toHaveText('orcid.example/0000-0001');
    // Email and phone link after the server-checked rules (ADR 0013).
    await expect(publicPage.locator('a[href="mailto:proof@example.com"]')).toHaveText('proof@example.com');
    await expect(publicPage.locator('a[href="tel:+84374837720"]')).toHaveText('(+84) 374837720');

    stage('public-head');
    // The owner's title and emoji favicon pass the server's exact-head check.
    await expect(publicPage).toHaveTitle(PAGE_TITLE);
    const icon = publicPage.locator('link[rel="icon"]');
    await expect(icon).toHaveCount(1);
    const iconHref = await icon.getAttribute('href');
    expect(iconHref?.startsWith('data:image/svg+xml,')).toBe(true);
    expect(decodeURIComponent(iconHref!.slice('data:image/svg+xml,'.length)))
      .toContain(`>${PAGE_EMOJI}</text>`);

    // The link-preview tags: exact value, exact order, exactly once, and
    // none of the tags the page must never send.
    stage('public-link-preview');
    const canonicalURL = `${ORIGIN}/${publishedSlug}`;
    // The stored card's versioned URL (docs/design/link-previews.md, "Build,
    // storage, and serving"); og:image and twitter:image share it below.
    const ogImageURL = headMetaTags(publicHTML)
      .find((tag) => tag.key === 'og:image')?.value ?? '';
    expect(new URL(ogImageURL).pathname).toMatch(new RegExp(
      `^/api/v1/public/resumes/${publishedSlug}/og/[0-9a-f]{16}\\.png$`,
    ));
    expectLinkPreviewHead(publicHTML, [
      { key: 'description', value: PUBLIC_PROOF_DESCRIPTION },
      { key: 'og:type', value: 'profile' },
      { key: 'og:site_name', value: 'aboutme.vn' },
      { key: 'og:title', value: PAGE_TITLE },
      { key: 'og:description', value: PUBLIC_PROOF_DESCRIPTION },
      { key: 'og:url', value: canonicalURL },
      { key: 'og:locale', value: 'en_US' },
      { key: 'og:image', value: ogImageURL },
      { key: 'og:image:type', value: 'image/png' },
      { key: 'og:image:width', value: '1200' },
      { key: 'og:image:height', value: '630' },
      { key: 'og:image:alt', value: PUBLIC_PROOF_IMAGE_ALT },
      { key: 'twitter:card', value: 'summary_large_image' },
      { key: 'twitter:image', value: ogImageURL },
      { key: 'twitter:image:alt', value: PUBLIC_PROOF_IMAGE_ALT },
    ]);

    stage('public-share-image');
    const versionedImage = await fetchImage(publicPage, ogImageURL);
    expect(versionedImage.status).toBe(200);
    expect(versionedImage.type).toBe('image/png');
    expect(versionedImage.body.length).toBeLessThanOrEqual(CARD_MAX_BYTES);
    expectPNGSize(versionedImage.body);
    const aliasURL = `${ORIGIN}/api/v1/public/resumes/${publishedSlug}/og.png`;
    const aliasImage = await fetchImage(publicPage, aliasURL);
    expect(aliasImage.status).toBe(200);
    expect(aliasImage.body).toEqual(versionedImage.body);

    // Every public page credits the site once, linking the canonical home.
    const credit = publicPage.locator('a.public-credit');
    await expect(credit).toHaveCount(1);
    await expect(credit).toHaveAttribute('href', `${new URL(publicPage.url()).origin}/`);
    await expect(credit).toHaveText('Built with aboutme.vn');

    // Download is enabled, so the page links its own PDF.
    stage('public-download');
    const download = publicPage.locator('a.public-download');
    await expect(download).toHaveCount(1);
    await expect(download).toHaveAttribute('href', `/api/v1/public/resumes/${publishedSlug}/pdf`);
    await expect(download).toHaveText('Download PDF');
    const pdf = await publicPage.evaluate(async (href) => {
      const response = await fetch(href, { cache: 'no-store' });
      return {
        status: response.status,
        type: response.headers.get('content-type'),
        disposition: response.headers.get('content-disposition'),
      };
    }, `/api/v1/public/resumes/${publishedSlug}/pdf`);
    expect(pdf).toEqual({
      status: 200,
      type: 'application/pdf',
      disposition: `attachment; filename="${PDF_NAME}"; filename*=UTF-8''${PDF_NAME}`,
    });
    await publicPage.emulateMedia({ media: 'print' });
    await expect(download).toBeHidden();
    await publicPage.emulateMedia({ media: 'screen' });
    expect(response?.headers()['content-security-policy']).toContain("default-src 'none'");

    // The dark scheme reaches the served page and leaves the PDF alone
    // (docs/design/public-page-theme.md, "What stays light"). The PDF fetch
    // above already returned 200 with application/pdf.
    stage('public-color-scheme');
    await expect(publicPage.locator('.public-resume-page'))
      .toHaveAttribute('data-color-scheme', 'dark');
    const darkPDF = await publicPage.evaluate(async (href) => {
      const response = await fetch(href, { cache: 'no-store' });
      const bytes = new Uint8Array(await response.arrayBuffer());
      return {
        status: response.status,
        type: response.headers.get('content-type'),
        magic: String.fromCharCode(...bytes.slice(0, 5)),
      };
    }, `/api/v1/public/resumes/${publishedSlug}/pdf`);
    expect(darkPDF).toEqual({
      status: 200,
      type: 'application/pdf',
      magic: '%PDF-',
    });

    // SSR markup is present before hydration runs.
    stage('public-ssr');
    const main = publicPage.locator('#public-resume');
    await expect(main).toBeVisible();
    await expect(main).toHaveAttribute('data-revision', /^[1-9][0-9]*$/);
    await expect(publicPage).toHaveTitle(PAGE_TITLE);

    // The client hydration mounts the Vue app on the SSR root.
    stage('public-hydration');
    await waitForHydration(publicPage, 'public-resume');

    // The template stylesheets load under the page CSP and style the resume;
    // DOM presence alone would pass on an unstyled page.
    stage('public-styling');
    const styling = await publicPage.evaluate(() => {
      const sheets = [...document.styleSheets].map((sheet) => ({
        href: sheet.href === null ? '' : new URL(sheet.href).pathname,
        search: sheet.href === null ? '' : new URL(sheet.href).search,
        rules: sheet.cssRules.length,
      }));
      const resume = document.querySelector('.resume-document');
      if (resume === null) return { sheets, resume: null };
      const computed = getComputedStyle(resume);
      return {
        sheets,
        resume: {
          boxSizing: computed.boxSizing,
          paddingLeft: computed.paddingLeft,
          fontFamily: computed.fontFamily,
          fontVariable: computed.getPropertyValue('--font-family').trim(),
        },
      };
    });
    for (const path of [
      '/_nuxt/assets/print-fonts.css',
      '/_nuxt/assets/print.css',
    ]) {
      const sheet = styling.sheets.find((candidate) => candidate.href === path);
      expect(sheet?.rules ?? 0, `${path} loaded with rules`).toBeGreaterThan(0);
      // A per-release version defeats the year-long immutable cache.
      expect(sheet?.search, `${path} version`).toMatch(/^\?v=[0-9a-f]{16}$/u);
    }
    // The hydration script is versioned too, so a returning browser never
    // runs a previous release's cached script against new HTML.
    const scriptURL = await publicPage
      .locator('script[type="module"]')
      .getAttribute('src');
    expect(scriptURL).toMatch(
      /^\/_nuxt\/assets\/public-resume\.mjs\?v=[0-9a-f]{16}$/u,
    );
    expect(styling.resume).not.toBeNull();
    expect(styling.resume?.boxSizing).toBe('border-box');
    expect(styling.resume?.paddingLeft).not.toBe('0px');
    expect(styling.resume?.fontVariable).not.toBe('');
    // Computed font-family drops quotes around single-word family names.
    const unquoted = (value?: string): string => (value ?? '').replaceAll('"', '');
    expect(unquoted(styling.resume?.fontFamily)).toBe(unquoted(styling.resume?.fontVariable));

    // The skip link stays out of view until keyboard focus reaches it.
    stage('public-skip-link');
    const skip = publicPage.getByRole('link', { name: 'Skip to content' });
    const hidden = await skip.boundingBox();
    expect(hidden === null || (hidden.width <= 1 && hidden.height <= 1)).toBe(true);
    await publicPage.keyboard.press('Tab');
    await expect(skip).toBeFocused();
    const shown = await skip.boundingBox();
    expect(shown?.width ?? 0).toBeGreaterThan(1);
    expect(shown?.height ?? 0).toBeGreaterThan(1);

    await publicContext.close();
  }

  expect(consoleErrors).toEqual([]);
  expect(dialogs).toEqual([]);
  expect(pageErrors).toEqual([]);
  expect(externalRequests).toEqual([]);
  expect(privateRequests).toEqual([]);

  // The sign-in-to-view test runs first in this file; its evidence is part
  // of this run's, so a missing step fails here rather than passing quietly.
  if (Object.values(viewSteps).some((done) => !done)) {
    stage('sign-in-to-view-incomplete');
    throw new Error('sign-in-to-view-incomplete');
  }

  await writeFile(EVIDENCE_PATH, `${JSON.stringify({
    schemaVersion: 1,
    scenario: 'public-resume-hydration',
    origin: ORIGIN,
    errors: { console: consoleErrors.length, externalRequest: externalRequests.length, page: pageErrors.length },
    steps: { published: true, ssr: true, hydrated: true, ...viewSteps },
    timings: { joinInviteMs },
  })}\n`, { flag: 'wx', mode: 0o600 });
});

// The MCP guide (/guide/mcp) is a static public page (docs/design/mcp-guide.md):
// Vietnamese by default, English behind the site's aboutme-locale cookie, no
// data fetch.
test('the MCP guide page renders through Caddy in both '
  + 'languages', async ({ page }) => {
  const viResponse = await page.goto(`${ORIGIN}/guide/mcp`);
  expect(viResponse?.status()).toBe(200);
  await expect(page.locator('[data-page-title]')).toHaveText(
    'Kết nối trợ lý AI với aboutme.vn',
  );

  await page.context().addCookies([
    { name: 'aboutme-locale', value: 'en', url: ORIGIN },
  ]);
  const enResponse = await page.goto(`${ORIGIN}/guide/mcp`);
  expect(enResponse?.status()).toBe(200);
  await expect(page.locator('[data-page-title]')).toHaveText(
    'Connect your AI assistant to aboutme.vn',
  );
});

// /guide itself reserves the root for later guides but holds no page of its
// own (docs/design/mcp-guide.md, "Route"): it renders the app's normal
// not-found page rather than a guide.
test('/guide with no further path is not found', async ({ page }) => {
  const response = await page.goto(`${ORIGIN}/guide`);
  expect(response?.status()).toBe(404);
  await expect(page.getByTestId('error-page')).toBeVisible();
});

// The guide root is a Nuxt root, and /mcp stays the Go MCP endpoint
// (docs/design/mcp-guide.md, "Route"): a request with no bearer token still
// gets the protected-resource challenge (apps/server/internal/mcpapi/errors.go).
test('POST /mcp without a token returns 401 with the metadata '
  + 'challenge', async ({ page }) => {
  // Load a same-origin page first so the fetch below is not cross-origin.
  expect((await page.goto(`${ORIGIN}/guide/mcp`))?.status()).toBe(200);
  const rejected = await page.evaluate(async (origin) => {
    const response = await fetch(new URL('/mcp', origin), {
      method: 'POST',
      headers: {
        Accept: 'application/json, text/event-stream',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        jsonrpc: '2.0', id: 1, method: 'tools/list', params: {},
      }),
    });
    return {
      status: response.status,
      body: await response.text(),
      wwwAuthenticate: response.headers.get('www-authenticate'),
    };
  }, ORIGIN);
  expect(rejected.status).toBe(401);
  expect(rejected.body).toBe('{"error":"unauthorized"}');
  expect(rejected.wwwAuthenticate).toBe(
    `Bearer resource_metadata="${ORIGIN}/.well-known/oauth-protected-resource"`,
  );
});
