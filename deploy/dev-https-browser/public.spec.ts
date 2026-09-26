import { expect, test } from '@playwright/test';
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
  pinEnglish,
  waitForHydration,
} from './harness-lib';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/public-proof.json';
const SCHEMA_VERSION = '4';
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

function stage(name: string): void {
  console.log('public-stage:' + name);
}

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
      revision: detailWrite.revision as string,
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
    // Email and phone link after the server-checked rules (ADR 0043).
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
    const ogImageURL = `${ORIGIN}/api/v1/public/resumes/${publishedSlug}/og.png`;
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

  await writeFile(EVIDENCE_PATH, `${JSON.stringify({
    schemaVersion: 1,
    scenario: 'public-resume-hydration',
    origin: ORIGIN,
    errors: { console: consoleErrors.length, externalRequest: externalRequests.length, page: pageErrors.length },
    steps: { published: true, ssr: true, hydrated: true },
  })}\n`, { flag: 'wx', mode: 0o600 });
});
