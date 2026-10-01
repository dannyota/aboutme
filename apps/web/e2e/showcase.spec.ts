import { expect, type Page, type Request, test } from '@playwright/test';

import {
  denyExternalRequests,
  expectCspClean,
  mockSignedOutSession,
  trackCsp,
  waitForImages,
} from './support';

// The community showcase (/showcase) in the production build, with the
// listing API stubbed: the loading state in the server HTML, tiles and card
// images under the app CSP, filters and paging in the URL, every state, the
// phone header, and the privacy rules (docs/design/showcase.md; AC-SHOW-002,
// 005, 008, 013).

const ORIGIN = 'http://127.0.0.1:20092';
const LISTING = /\/api\/v1\/public\/showcase(?:\?.*)?$/u;
const CARD = /\/api\/v1\/public\/resumes\/[a-z0-9-]+\/og\/[0-9a-f]{16}\.png$/u;
// A 1x1 PNG, so the card image decodes under the CSP.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGA'
  + 'hKmMIQAAAABJRU5ErkJggg==',
  'base64',
);
// Chrome logs the signed-out 401 from GET /api/v1/me.
const ANONYMOUS_ME_401 = 'a status of 401';

interface Item {
  readonly slug: string;
  readonly cardVersion: string;
  readonly imageText: string;
  readonly language: 'vi' | 'en' | 'other';
  readonly templateId: string | null;
  readonly role: string | null;
}

function item(index: number, extra: Partial<Item> = {}): Item {
  return {
    slug: `resume-${index}`,
    cardVersion: '0123456789abcdef',
    imageText: `Card of resume ${index}`,
    language: 'en',
    templateId: 'engineer-compact',
    role: 'backend',
    ...extra,
  };
}

function listing(
  items: readonly Item[],
  extra: { page?: number; pageCount?: number; total?: number } = {},
) {
  return {
    items,
    page: extra.page ?? 1,
    pageCount: extra.pageCount ?? 1,
    total: extra.total ?? items.length,
  };
}

type Handler = (url: URL) => { status?: number; body: unknown };

async function serve(page: Page, handler: Handler): Promise<URL[]> {
  const seen: URL[] = [];
  await mockSignedOutSession(page);
  await page.route(CARD, (route) => route.fulfill({
    status: 200,
    contentType: 'image/png',
    body: PNG,
  }));
  await page.route(LISTING, (route) => {
    const url = new URL(route.request().url());
    seen.push(url);
    const reply = handler(url);
    return route.fulfill({
      status: reply.status ?? 200,
      contentType: 'application/json',
      headers: { 'cache-control': 'no-store' },
      body: JSON.stringify(reply.body),
    });
  });
  return seen;
}

async function open(page: Page, path: string, locale: 'vi' | 'en' = 'en') {
  await page.context().addCookies([
    { name: 'aboutme-locale', value: locale, url: ORIGIN },
  ]);
  const response = await page.goto(path);
  expect(response?.status()).toBe(200);
}

async function overflow(page: Page): Promise<number> {
  return page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
}

test('the server HTML holds the loading state and no listing', async ({
  request,
}) => {
  const response = await request.get('/showcase');
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(response.headers()['x-robots-tag']).toBe('noindex, nofollow');
  expect(response.headers()['set-cookie']).toBeUndefined();
  expect(html).toContain('data-state="loading"');
  expect(html).toContain('aria-busy="true"');
  expect(html).toContain('<h1');
  expect(html).not.toContain('data-showcase-slug');
  expect(html).not.toContain('data-state="empty"');
  expect(html).toMatch(
    /<meta(?=[^>]*name="robots")(?=[^>]*content="noindex, nofollow")/u,
  );
});

test('lists tiles with lazy card images under the app CSP', async ({
  page,
}) => {
  const probe = await trackCsp(page);
  await denyExternalRequests(page);
  await serve(page, () => ({
    body: listing([
      item(1),
      item(2, { templateId: null, role: null, language: 'vi' }),
      item(3, { role: 'other', language: 'other' }),
    ]),
  }));
  await open(page, '/showcase');
  await expect(page.locator('[data-showcase-slug]')).toHaveCount(3);
  await expect(page.locator('[data-state="list"]'))
    .not.toHaveAttribute('aria-busy', 'true');

  const first = page.locator('[data-showcase-slug="resume-1"]');
  const link = first.locator('[data-showcase-tile]');
  await expect(link).toHaveAttribute('href', '/resume-1');
  await expect(link).toHaveAttribute('rel', 'nofollow');
  await expect(first.locator('img')).toHaveAttribute('loading', 'lazy');
  await expect(first.locator('img')).toHaveAttribute(
    'alt',
    'Card of resume 1',
  );
  await expect(first).toContainText('Engineer Compact');
  await expect(first).toContainText('English');
  await expect(first).toContainText('Backend');
  await expect(first.locator('[data-action="showcase-report"]'))
    .toHaveAttribute(
      'href',
      'mailto:danny@aboutme.vn?subject='
      + encodeURIComponent('Report showcase: resume-1'),
    );
  const custom = page.locator('[data-showcase-slug="resume-2"]');
  await expect(custom).toContainText('Custom design');
  await expect(custom.locator('[data-showcase-role]')).toHaveCount(0);

  for (const image of await page.locator('img').all()) {
    await image.scrollIntoViewIfNeeded();
  }
  await waitForImages(page);
  await expectCspClean(probe, [ANONYMOUS_ME_401]);
});

test('sends noindex, nofollow once and no counting script', async ({
  page,
}) => {
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  await expect(page.locator('meta[name="robots"]')).toHaveCount(1);
  await expect(page.locator('meta[name="robots"]'))
    .toHaveAttribute('content', 'noindex, nofollow');
  await expect(page.locator('link[rel="canonical"]')).toHaveCount(0);
  await expect(page.locator('script[src]:not([src*="/_nuxt/"])'))
    .toHaveCount(0);
});

test('reads the filters from the URL and writes changes back', async ({
  page,
}) => {
  const seen = await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase?role=backend&lang=vi&page=2');
  await expect(page.locator('[data-showcase-slug]')).toHaveCount(1);
  expect(seen.map((url) => url.search)).toEqual([
    '?role=backend&lang=vi&page=2',
  ]);
  await expect(page.locator('[data-role="backend"]'))
    .toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('[data-lang="vi"]'))
    .toHaveAttribute('aria-pressed', 'true');

  await page.locator('[data-role="frontend"]').click();
  await expect(page).toHaveURL(/\/showcase\?role=frontend&lang=vi$/u);
  await expect(page.locator('[data-role="frontend"]')).toBeFocused();
  await expect.poll(() => seen.at(-1)?.search)
    .toBe('?role=frontend&lang=vi');

  await page.locator('[data-lang="all"]').click();
  await expect(page).toHaveURL(/\/showcase\?role=frontend$/u);

  await page.locator('select').selectOption('custom');
  await expect(page).toHaveURL(/\/showcase\?role=frontend&template=custom$/u);
  await expect.poll(() => seen.at(-1)?.search)
    .toBe('?role=frontend&template=custom');

  await page.locator('[data-role="all"]').click();
  await page.locator('select').selectOption('');
  await expect(page).toHaveURL(/\/showcase$/u);
});

test('treats unknown query values as All and does not send them', async ({
  page,
}) => {
  const seen = await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase?role=wizard&lang=fr&template=zzz&page=500');
  await expect(page.locator('[data-showcase-slug]')).toHaveCount(1);
  expect(seen.map((url) => url.search)).toEqual(['']);
  await expect(page.locator('[data-role="all"]'))
    .toHaveAttribute('aria-pressed', 'true');
});

test('pages with Previous and Next, keeping the filters', async ({ page }) => {
  const seen = await serve(page, (url) => {
    const number = Number(url.searchParams.get('page') ?? '1');
    return {
      body: listing([item(number)], { page: number, pageCount: 3, total: 3 }),
    };
  });
  await open(page, '/showcase?role=backend');
  await expect(page.locator('[data-showcase-slug="resume-1"]')).toBeVisible();
  const pager = page.getByTestId('showcase-pager');
  await expect(pager).toContainText('Page 1 of 3');
  await expect(pager.locator('span[data-action="showcase-previous"]'))
    .toHaveAttribute('aria-disabled', 'true');

  await pager.locator('a[data-action="showcase-next"]').click();
  await expect(page).toHaveURL(/\/showcase\?role=backend&page=2$/u);
  await expect(page.locator('[data-showcase-slug="resume-2"]')).toBeVisible();
  // Focus lands on the first tile link once the page loads.
  await expect(page.locator('[data-showcase-tile]').first()).toBeFocused();
  expect(seen.at(-1)?.search).toBe('?role=backend&page=2');

  await pager.locator('a[data-action="showcase-next"]').click();
  await expect(page.locator('[data-showcase-slug="resume-3"]')).toBeVisible();
  await expect(pager.locator('span[data-action="showcase-next"]'))
    .toHaveAttribute('aria-disabled', 'true');
  await pager.locator('a[data-action="showcase-previous"]').click();
  await expect(page).toHaveURL(/\/showcase\?role=backend&page=2$/u);
});

test('shows the empty state, then no-match with a filter', async ({
  page,
}) => {
  await serve(page, () => ({ body: listing([], { pageCount: 0 }) }));
  await open(page, '/showcase');
  const empty = page.locator('[data-state="empty"]');
  await expect(empty).toContainText('No resumes here yet.');
  await expect(empty.getByRole('link', { name: 'Create your resume' }))
    .toHaveAttribute('href', '/register');
  await expect(page.locator('[data-testid="showcase-roles"]')).toBeVisible();

  await page.locator('[data-role="qa"]').click();
  await expect(page.locator('[data-state="no-match"]'))
    .toHaveText('No resume matches these filters.');
  await expect(page.getByTestId('showcase-pager')).toHaveCount(0);
});

test('shows the no-match line and Previous past the end', async ({ page }) => {
  await serve(page, () => ({
    body: listing([], { page: 5, pageCount: 3, total: 30 }),
  }));
  await open(page, '/showcase?page=5');
  await expect(page.locator('[data-state="no-match"]')).toBeVisible();
  const pager = page.getByTestId('showcase-pager');
  await expect(pager.locator('a[data-action="showcase-previous"]'))
    .toHaveAttribute('href', '/showcase?page=3');
  await expect(pager.locator('span[data-action="showcase-next"]'))
    .toHaveAttribute('aria-disabled', 'true');
});

test('shows the failed state and Retry reloads the same query', async ({
  page,
}) => {
  let calls = 0;
  const seen = await serve(page, () => {
    calls += 1;
    return calls === 1
      ? { status: 429, body: { error: { code: 'rate_limited' } } }
      : { body: listing([item(1)]) };
  });
  await open(page, '/showcase?lang=en');
  const banner = page.locator('[data-state="failed"]');
  await expect(banner).toContainText('Could not load the list. Try again.');
  await banner.getByRole('button', { name: 'Try again' }).click();
  await expect(page.locator('[data-showcase-slug="resume-1"]')).toBeVisible();
  await expect(page.locator('[data-state="failed"]')).toHaveCount(0);
  expect(seen.map((url) => url.search)).toEqual(['?lang=en', '?lang=en']);
});

test('speaks Vietnamese, fits phone width, and marks the logo down',
  async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await serve(page, () => ({
      body: listing(Array.from({ length: 12 }, (_, index) => item(index + 1, {
        role: 'fresher',
        language: 'other',
        templateId: 'international-lang',
      })), { pageCount: 100, total: 1200 }),
    }));
    await open(page, '/showcase?page=100', 'vi');
    await expect(page.locator('h1')).toHaveText('CV từ cộng đồng');
    await expect(page.locator('[data-showcase-slug]')).toHaveCount(12);
    expect(await overflow(page)).toBe(0);
    await expect(page.getByTestId('showcase-pager'))
      .toContainText('Trang 100/100');
    // Below 64rem, signed out, the logo is the mark alone.
    const logos = page.locator('[data-testid="app-shell"] [data-logo-size]');
    const shown = logos.filter({ visible: true });
    await expect(shown).toHaveCount(1);
    await expect(shown.locator('[data-logo-part="wordmark"]')).toHaveCount(0);
    const community = page.getByRole('link', { name: 'Cộng đồng' });
    await expect(community).toHaveAttribute('aria-current', 'page');
    await expect(community).toHaveAttribute('href', '/showcase');
  });

test('keeps the full logo and header link from 64rem', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  const logos = page.locator('[data-testid="app-shell"] [data-logo-size]');
  const shown = logos.filter({ visible: true });
  await expect(shown).toHaveCount(1);
  await expect(shown.locator('[data-logo-part="wordmark"]')).toHaveCount(1);
  await expect(page.getByRole('link', { name: 'Community' }))
    .toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('link', { name: 'Library' }))
    .not.toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('link', { name: 'Create your resume' }).first())
    .toBeVisible();
  expect(await overflow(page)).toBe(0);
});

test('stores nothing and sends no cookie with the listing', async ({
  page,
  context,
}) => {
  const requests: Request[] = [];
  page.on('request', (request) => {
    if (LISTING.test(request.url())) requests.push(request);
  });
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  await expect(page.locator('[data-showcase-slug]')).toHaveCount(1);
  expect(requests).toHaveLength(1);
  const headers = await requests[0]!.allHeaders();
  expect(headers.cookie).toBeUndefined();
  expect(headers['cache-control']).toBe('no-store');
  const storage = await page.evaluate(() => ({
    local: localStorage.length,
    session: sessionStorage.length,
  }));
  expect(storage).toEqual({ local: 0, session: 0 });
  // Only the cookie the test set; the page added none.
  expect((await context.cookies()).map((cookie) => cookie.name))
    .toEqual(['aboutme-locale']);
});
