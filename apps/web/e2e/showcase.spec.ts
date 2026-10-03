import {
  expect,
  type Locator,
  type Page,
  type Request,
  test,
} from '@playwright/test';

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
// 005, 008, 013), plus the page layout of docs/design/ui/showcase.md: the
// filter sheet below 1024 px, the rail from 1024 px, the active-filter chips,
// the count line, the invite card, and focus clearing the sticky bar. The
// rail and an open sheet both hold the shared filter fields, so every filter
// locator is scoped to one of them.

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

async function serve(
  page: Page,
  handler: Handler,
  capabilities: Record<string, unknown> = {},
): Promise<URL[]> {
  const seen: URL[] = [];
  await mockSignedOutSession(page, capabilities);
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
      headers: { 'cache-control': 'no-store, no-transform' },
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

function rail(page: Page): Locator {
  return page.getByTestId('showcase-filter-rail');
}

function sheet(page: Page): Locator {
  return page.getByTestId('showcase-filter-sheet');
}

function filtersButton(page: Page): Locator {
  return page.locator('[data-action="showcase-filters-open"]');
}

function chip(page: Page, name: string): Locator {
  return page.getByRole('group', { name: /Active filters|Bộ lọc đang bật/u })
    .getByRole('button', { name });
}

// The number of grid columns of the tile list, from its computed tracks.
async function columns(page: Page): Promise<number> {
  return page.locator('[data-showcase-slug]').first().evaluate((node) =>
    getComputedStyle(node.parentElement!).gridTemplateColumns
      .split(' ').length);
}

const SIX = Array.from({ length: 6 }, (_, index) => item(index + 1));

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
  // Both filter layouts render on the server and switch by CSS alone
  // (docs/design/ui/showcase.md "Page frame"); the sheet mounts when opened.
  expect(html).toContain('data-testid="showcase-filter-bar"');
  expect(html).toContain('data-testid="showcase-filter-rail"');
  expect(html).not.toContain('data-testid="showcase-filter-sheet"');
  expect(html).not.toContain('data-testid="showcase-active-filters"');
  expect(html).not.toContain('data-testid="showcase-filters-badge"');
  expect(html).toMatch(
    /<meta(?=[^>]*name="robots")(?=[^>]*content="noindex, nofollow")/u,
  );
});

test('the server HTML holds the badge and chips for a filtered URL', async ({
  request,
}) => {
  const response = await request.get(
    '/showcase?role=backend&lang=vi&template=custom',
  );
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(html).toContain('data-testid="showcase-filters-badge"');
  expect(html).toContain('data-testid="showcase-active-filters"');
  expect(html).toContain('data-action="showcase-filter-remove"');
  expect(html).toContain('data-action="showcase-filters-clear-all"');
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
  await expect(first.getByRole('link', {
    name: 'Report resume resume-1 by email',
  })).toBeVisible();
  // The Report link keeps a 40 by 40 px target (docs/design/ui/showcase.md,
  // Tile footer).
  const report = await first.locator('[data-action="showcase-report"]')
    .first().boundingBox();
  expect(report!.width).toBeGreaterThanOrEqual(40);
  expect(report!.height).toBeGreaterThanOrEqual(40);
  // The footer row shows the slug as plain text outside the tile link
  // (docs/design/ui/showcase.md "Footer row").
  await expect(first.getByText('aboutme.vn/resume-1', { exact: true }))
    .toBeVisible();
  await expect(link.getByText('aboutme.vn/resume-1')).toHaveCount(0);
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
  // Only the app's own bundles and its two first-party bootstrap scripts
  // load: no counting or third-party script.
  const sources = await page.locator('script[src]')
    .evaluateAll((nodes) => nodes.map((node) => node.getAttribute('src')));
  const own = new Set(['/theme-bootstrap.js', '/csp-bootstrap.js']);
  expect(sources.filter(
    (source) => !source?.startsWith('/_nuxt/') && !own.has(source ?? ''),
  )).toEqual([]);
});

test('reads the filters from the URL and writes changes back', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const seen = await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase?role=backend&lang=vi&page=2');
  await expect(page.locator('[data-showcase-slug]')).toHaveCount(1);
  expect(seen.map((url) => url.search)).toEqual([
    '?role=backend&lang=vi&page=2',
  ]);
  const fields = rail(page);
  await expect(fields.locator('[data-role="backend"]'))
    .toHaveAttribute('aria-pressed', 'true');
  await expect(fields.locator('[data-lang="vi"]'))
    .toHaveAttribute('aria-pressed', 'true');

  await fields.locator('[data-role="frontend"]').click();
  await expect(page).toHaveURL(/\/showcase\?role=frontend&lang=vi$/u);
  await expect(fields.locator('[data-role="frontend"]')).toBeFocused();
  await expect.poll(() => seen.at(-1)?.search)
    .toBe('?role=frontend&lang=vi');

  await fields.locator('[data-lang="all"]').click();
  await expect(page).toHaveURL(/\/showcase\?role=frontend$/u);

  await fields.locator('select[name="template"]').selectOption('custom');
  await expect(page).toHaveURL(/\/showcase\?role=frontend&template=custom$/u);
  await expect.poll(() => seen.at(-1)?.search)
    .toBe('?role=frontend&template=custom');

  await fields.locator('[data-role="all"]').click();
  await fields.locator('select[name="template"]').selectOption('');
  await expect(page).toHaveURL(/\/showcase$/u);
});

test('treats unknown query values as All and does not send them', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const seen = await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase?role=wizard&lang=fr&template=zzz&page=500');
  await expect(page.locator('[data-showcase-slug]')).toHaveCount(1);
  expect(seen.map((url) => url.search)).toEqual(['']);
  await expect(rail(page).locator('[data-role="all"]'))
    .toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByTestId('showcase-active-filters')).toHaveCount(0);
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
  await page.setViewportSize({ width: 1440, height: 900 });
  await serve(page, () => ({ body: listing([], { pageCount: 0 }) }));
  await open(page, '/showcase');
  const empty = page.locator('[data-state="empty"]');
  await expect(empty).toContainText('No resumes here yet.');
  await expect(empty.getByRole('link', { name: 'Create your resume' }))
    .toHaveAttribute('href', '/register');
  await expect(rail(page).getByTestId('showcase-roles')).toBeVisible();
  // The invite card belongs to the list state only.
  await expect(page.getByTestId('showcase-invite')).toHaveCount(0);

  await rail(page).locator('[data-role="qa"]').click();
  const noMatch = page.locator('[data-state="no-match"]');
  await expect(noMatch).toHaveText('No resume matches these filters.');
  // The count line is the page's live region, so the line has no role.
  await expect(noMatch).not.toHaveAttribute('role', 'status');
  await expect(page.getByTestId('showcase-count'))
    .toHaveText('0 resumes · earliest added first');
  await expect(page.getByTestId('showcase-pager')).toHaveCount(0);
  await expect(page.getByTestId('showcase-invite')).toHaveCount(0);
});

test('shows the no-match line and Previous past the end', async ({ page }) => {
  await serve(page, () => ({
    body: listing([], { page: 5, pageCount: 3, total: 30 }),
  }));
  await open(page, '/showcase?page=5');
  await expect(page.locator('[data-state="no-match"]')).toBeVisible();
  await expect(page.getByTestId('showcase-invite')).toHaveCount(0);
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
  await expect(page.getByTestId('showcase-invite')).toHaveCount(0);
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
      })), { page: 100, pageCount: 100, total: 1200 }),
    }));
    await open(page, '/showcase?page=100', 'vi');
    await expect(page.locator('h1')).toHaveText('CV từ cộng đồng');
    await expect(page.locator('[data-showcase-slug]')).toHaveCount(12);
    await expect(page.getByTestId('showcase-count'))
      .toHaveText('1.200 CV · sớm nhất trước');
    await expect(page.getByTestId('showcase-invite'))
      .toContainText('Muốn CV của bạn ở đây?');
    await expect(page.locator('[data-action="showcase-invite-create"]'))
      .toHaveText('Tạo CV miễn phí');
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
  // The request cache mode is not a header; the response is what the server
  // marks no-store.
  const reply = await requests[0]!.response();
  expect(reply?.headers()['cache-control']).toContain('no-store');
  const storage = await page.evaluate(() => ({
    local: localStorage.length,
    session: sessionStorage.length,
  }));
  expect(storage).toEqual({ local: 0, session: 0 });
  // Only the cookie the test set; the page added none.
  expect((await context.cookies()).map((cookie) => cookie.name))
    .toEqual(['aboutme-locale']);
});

test('the count line reads the listing total in both languages', async ({
  page,
}) => {
  await serve(page, () => ({
    body: listing([item(1), item(2), item(3)]),
  }));
  await open(page, '/showcase');
  const count = page.getByTestId('showcase-count');
  await expect(count).toHaveText('3 resumes · earliest added first');
  await expect(count).toHaveAttribute('role', 'status');
  await expect(count).toHaveAttribute('aria-atomic', 'true');
  // The header holds the title and lead and no focusable element.
  const header = page.getByTestId('showcase-header');
  await expect(header.locator('h1')).toHaveText('Community resumes');
  await expect(header.locator('a, button, input, select, [tabindex]'))
    .toHaveCount(0);

  await open(page, '/showcase', 'vi');
  await expect(page.getByTestId('showcase-count'))
    .toHaveText('3 CV · sớm nhất trước');
});

test('the count line says 1 resume for a total of 1', async ({ page }) => {
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  await expect(page.getByTestId('showcase-count'))
    .toHaveText('1 resume · earliest added first');
});

test('opens the filter sheet on a phone and keeps focus inside it', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const seen = await serve(page, () => ({
    body: listing([item(1), item(2), item(3)], { pageCount: 2 }),
  }));
  await open(page, '/showcase?role=backend&page=2');
  const button = filtersButton(page);
  await expect(button).toBeVisible();
  await expect(rail(page)).toBeHidden();
  await expect(sheet(page)).toHaveCount(0);
  const box = await button.boundingBox();
  expect(box!.width).toBeGreaterThanOrEqual(44);
  expect(box!.height).toBeGreaterThanOrEqual(44);
  await expect(button).toHaveAttribute('aria-haspopup', 'dialog');

  await button.click();
  const dialog = page.getByRole('dialog', { name: 'Filters' });
  await expect(dialog).toBeVisible();
  await expect(sheet(page)).toBeVisible();
  await expect(button).toHaveAttribute('aria-expanded', 'true');
  const fields = sheet(page);
  await expect(fields.getByTestId('showcase-roles').locator('[data-role]'))
    .toHaveCount(11);
  await expect(
    fields.getByTestId('showcase-languages').locator('[data-lang]'),
  ).toHaveCount(3);
  // All templates, the 20 presets, and Custom design.
  await expect(fields.locator('select[name="template"] option'))
    .toHaveCount(22);
  await expect(fields.locator('[data-action="showcase-filters-clear"]'))
    .toHaveText('Clear filters');
  await expect(fields.locator('[data-action="showcase-filters-apply"]'))
    .toHaveText('Show 3 resumes');
  // The close button takes focus first.
  await expect(dialog.getByRole('button', { name: 'Close' })).toBeFocused();

  // Tab wraps inside the dialog: close, role, language, template, Clear
  // filters, Show results, then back to close.
  for (let step = 0; step < 8; step += 1) {
    await page.keyboard.press('Tab');
    expect(await dialog.evaluate((node) =>
      node.contains(document.activeElement))).toBe(true);
  }

  // A role change applies at once, drops page, and keeps the sheet open.
  await fields.getByTestId('showcase-roles').locator('[data-role="frontend"]')
    .click();
  await expect(page).toHaveURL(/\/showcase\?role=frontend$/u);
  await expect.poll(() => seen.at(-1)?.search).toBe('?role=frontend');
  await expect(dialog).toBeVisible();

  // Escape closes the sheet, keeps the filters, and returns focus.
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(button).toBeFocused();
  await expect(button).toHaveAttribute('aria-expanded', 'false');
  await expect(page).toHaveURL(/\/showcase\?role=frontend$/u);

  // Clear filters empties every filter, keeps the sheet open and the focus.
  await button.click();
  await expect(dialog).toBeVisible();
  const clear = fields.locator('[data-action="showcase-filters-clear"]');
  await clear.click();
  await expect(page).toHaveURL(/\/showcase$/u);
  await expect(dialog).toBeVisible();
  await expect(clear).toBeFocused();
  await expect(
    fields.getByTestId('showcase-roles').locator('[data-role="all"]'),
  ).toHaveAttribute('aria-pressed', 'true');

  // Show results closes the sheet.
  await fields.locator('[data-action="showcase-filters-apply"]').click();
  await expect(dialog).toHaveCount(0);
  await expect(page).toHaveURL(/\/showcase$/u);
  expect(await overflow(page)).toBe(0);
});

// The suite runs with reduced motion, so the global rule caps the sheet's
// slide (docs/design/ui/showcase.md, Motion).
test('opens the filter sheet with no animation under reduced motion', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  await filtersButton(page).click();
  const content = page.locator('[data-slot="sheet-content"]');
  await expect(content).toBeVisible();
  const longest = await content.evaluate((node) => {
    const seconds = (value: string): number => (
      value.endsWith('ms')
        ? Number.parseFloat(value) / 1000
        : Number.parseFloat(value)
    );
    return Math.max(...getComputedStyle(node).animationDuration.split(',')
      .map((part) => seconds(part.trim())));
  });
  expect(longest).toBeLessThanOrEqual(0.001);
});

test('closes the filter sheet with its own close button', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  await filtersButton(page).click();
  const dialog = page.getByRole('dialog', { name: 'Filters' });
  await dialog.getByRole('button', { name: 'Close' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(filtersButton(page)).toBeFocused();
});

test('names the sheet and its fields in Vietnamese', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serve(page, () => ({ body: listing([item(1), item(2)]) }));
  await open(page, '/showcase?role=backend', 'vi');
  await page.getByRole('button', { name: 'Bộ lọc, 1 đang bật' }).click();
  const dialog = page.getByRole('dialog', { name: 'Bộ lọc' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Đóng' })).toBeVisible();
  await expect(dialog.getByRole('group', { name: 'Lọc theo vị trí' }))
    .toBeVisible();
  await expect(dialog.locator('[data-action="showcase-filters-clear"]'))
    .toHaveText('Xóa bộ lọc');
  await expect(dialog.locator('[data-action="showcase-filters-apply"]'))
    .toHaveText('Xem 2 CV');
});

test('shows the active-filter count on the Filters button', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase');
  await expect(page.getByRole('button', { name: 'Filters', exact: true }))
    .toBeVisible();
  await expect(page.getByTestId('showcase-filters-badge')).toHaveCount(0);

  await open(page, '/showcase?role=backend&lang=vi');
  const button = page.getByRole('button', { name: 'Filters, 2 active' });
  await expect(button).toBeVisible();
  await expect(button.getByTestId('showcase-filters-badge')).toHaveText('2');

  await open(page, '/showcase?role=backend&lang=vi&template=custom');
  await expect(page.getByRole('button', { name: 'Filters, 3 active' }))
    .toBeVisible();
});

test('removes chips and moves focus as the focus table says', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const seen = await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase?role=backend&lang=vi&template=custom');
  const group = page.getByRole('group', { name: 'Active filters' });
  await expect(group).toBeVisible();
  // Role, language, template, then Clear all.
  expect(await group.locator('[data-action="showcase-filter-remove"]')
    .evaluateAll((nodes) => nodes.map((node) =>
      node.getAttribute('data-filter')))).toEqual(['role', 'lang', 'template']);
  await expect(chip(page, 'Backend, remove filter')).toBeVisible();
  await expect(chip(page, 'Vietnamese, remove filter')).toBeVisible();
  await expect(chip(page, 'Custom design, remove filter')).toBeVisible();
  await expect(group.locator('[data-action="showcase-filters-clear-all"]'))
    .toHaveText('Clear all');

  // The first chip goes, focus moves to the next chip.
  await chip(page, 'Backend, remove filter').click();
  await expect(page).toHaveURL(/\/showcase\?lang=vi&template=custom$/u);
  await expect.poll(() => seen.at(-1)?.search)
    .toBe('?lang=vi&template=custom');
  await expect(chip(page, 'Vietnamese, remove filter')).toBeFocused();

  // The last chip goes, focus moves to the previous chip.
  await chip(page, 'Custom design, remove filter').click();
  await expect(page).toHaveURL(/\/showcase\?lang=vi$/u);
  await expect(chip(page, 'Vietnamese, remove filter')).toBeFocused();

  // The only chip goes, focus moves to the Filters button.
  await chip(page, 'Vietnamese, remove filter').click();
  await expect(page).toHaveURL(/\/showcase$/u);
  await expect(page.getByTestId('showcase-active-filters')).toHaveCount(0);
  await expect(filtersButton(page)).toBeFocused();
});

test('Clear all empties the filters and moves focus to the Filters button',
  async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await serve(page, () => ({ body: listing([item(1)]) }));
    await open(page, '/showcase?role=backend&lang=vi');
    await page.locator('[data-action="showcase-filters-clear-all"]').click();
    await expect(page).toHaveURL(/\/showcase$/u);
    await expect(page.getByTestId('showcase-active-filters')).toHaveCount(0);
    await expect(filtersButton(page)).toBeFocused();
  });

test('at 1440 px the rail filters and chips hand focus to the rail',
  async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await serve(page, () => ({ body: listing(SIX) }));
    await open(page, '/showcase?role=backend&lang=vi');
    const fields = rail(page);
    await expect(fields).toBeVisible();
    await expect(fields).toHaveAccessibleName('Filters');
    await expect(filtersButton(page)).toBeHidden();
    await expect(sheet(page)).toHaveCount(0);
    expect(await columns(page)).toBe(3);
    await expect(fields.locator('[data-lang]')).toHaveText([
      'All languages',
      'Vietnamese',
      'English',
    ]);
    await expect(fields.getByTestId('showcase-roles').locator('[data-role]'))
      .toHaveCount(11);

    // The last chip goes, focus lands on the rail's pressed Role row.
    await chip(page, 'Vietnamese, remove filter').click();
    await expect(chip(page, 'Backend, remove filter')).toBeFocused();
    await chip(page, 'Backend, remove filter').click();
    await expect(page).toHaveURL(/\/showcase$/u);
    await expect(fields.locator('[data-role="all"]')).toBeFocused();

    // Clear all ends on the same row.
    await fields.locator('[data-role="qa"]').click();
    await fields.locator('[data-lang="en"]').click();
    await page.locator('[data-action="showcase-filters-clear-all"]').click();
    await expect(page).toHaveURL(/\/showcase$/u);
    await expect(fields.locator('[data-role="all"]')).toBeFocused();
    await expect(fields.locator('[data-role="all"]'))
      .toHaveAttribute('aria-pressed', 'true');
  });

// docs/design/ui/showcase.md "Page frame", "Grid", and "Header".
const LAYOUTS = [
  { width: 390, rail: false, columns: 1, h1: '28px' },
  { width: 768, rail: false, columns: 2, h1: '32px' },
  { width: 1024, rail: true, columns: 2, h1: '40px' },
  { width: 1440, rail: true, columns: 3, h1: '40px' },
] as const;

for (const layout of LAYOUTS) {
  test(`lays out the filters and the grid at ${layout.width} px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: layout.width, height: 900 });
    await serve(page, () => ({ body: listing(SIX) }));
    await open(page, '/showcase');
    await expect(page.locator('[data-showcase-slug]')).toHaveCount(6);
    if (layout.rail) {
      await expect(rail(page)).toBeVisible();
      await expect(filtersButton(page)).toBeHidden();
    } else {
      await expect(rail(page)).toBeHidden();
      await expect(filtersButton(page)).toBeVisible();
      await expect(page.getByTestId('showcase-filter-bar')).toBeVisible();
    }
    expect(await columns(page)).toBe(layout.columns);
    await expect(page.locator('h1')).toHaveCSS('font-size', layout.h1);
    expect(await overflow(page)).toBe(0);
  });
}

test('the filter bar sticks and no ancestor sets overflow', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 700 });
  await serve(page, () => ({
    body: listing(Array.from({ length: 12 }, (_, index) => item(index + 1))),
  }));
  await open(page, '/showcase');
  const bar = page.getByTestId('showcase-filter-bar');
  await expect(bar).toHaveCSS('position', 'sticky');
  const clipped = await bar.evaluate((node) => {
    const found: string[] = [];
    for (let up = node.parentElement; up !== null; up = up.parentElement) {
      const style = getComputedStyle(up);
      if (style.overflowX !== 'visible' || style.overflowY !== 'visible') {
        found.push(up.tagName.toLowerCase());
      }
    }
    return found;
  });
  // Only the root may scroll; any other overflow breaks iOS Safari sticky.
  expect(clipped.filter((tag) => tag !== 'html' && tag !== 'body'))
    .toEqual([]);
  await page.evaluate(() => window.scrollTo(0, 600));
  await expect.poll(async () => (await bar.boundingBox())!.y).toBe(0);
});

// A tile link, Report, or pager link focused with the keyboard never rests
// under the sticky bar (docs/design/ui/showcase.md "Sticky bar"; WCAG 2.4.11).
async function expectClearOfBar(page: Page): Promise<void> {
  // The poll reads "clear" or a description of what is wrong, so a failure
  // names the focused element and both boxes.
  await expect.poll(() => page.evaluate(() => {
    const active = document.activeElement;
    const bar = document.querySelector('[data-testid="showcase-filter-bar"]');
    if (active === null || bar === null) return 'no focus or no bar';
    const box = active.getBoundingClientRect();
    const barBottom = bar.getBoundingClientRect().bottom;
    // Layout boxes are fractional while scroll offsets are whole pixels, so
    // an element scrolled flush to the bottom edge can end up to 1 px past it.
    if (box.top >= barBottom && box.bottom < window.innerHeight + 1) {
      return 'clear';
    }
    const name = `${active.tagName.toLowerCase()}`
      + `[${active.getAttribute('data-action') ?? ''}]`
      + `${active.hasAttribute('data-showcase-tile') ? '[tile]' : ''}`;
    return `${name} top ${box.top} bottom ${box.bottom}; bar bottom `
      + `${barBottom}; viewport ${window.innerHeight}; scrollY `
      + `${window.scrollY}`;
  })).toBe('clear');
}

for (const width of [390, 768]) {
  test(`keyboard focus clears the sticky bar at ${width} px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 700 });
    await serve(page, () => ({
      body: listing(
        Array.from({ length: 12 }, (_, index) => item(index + 1)),
        { pageCount: 3, total: 30 },
      ),
    }));
    await open(page, '/showcase');
    await expect(page.locator('[data-showcase-slug]')).toHaveCount(12);
    await filtersButton(page).focus();

    const kinds = new Set<string>();
    let scrolled = false;
    const record = async () => {
      kinds.add(await page.evaluate(() => {
        const active = document.activeElement;
        if (active?.hasAttribute('data-showcase-tile') === true) return 'tile';
        if (active?.getAttribute('data-action') === 'showcase-report') {
          return 'report';
        }
        return 'other';
      }));
      scrolled ||= await page.evaluate(() => window.scrollY > 0);
      await expectClearOfBar(page);
    };
    for (let step = 0; step < 12; step += 1) {
      await page.keyboard.press('Tab');
      await record();
    }
    for (let step = 0; step < 8; step += 1) {
      await page.keyboard.press('Shift+Tab');
      await record();
    }
    expect([...kinds].sort()).toEqual(['report', 'tile']);
    expect(scrolled).toBe(true);

    // From the invite button, Tab reaches the pager's Next link.
    await page.locator('[data-action="showcase-invite-create"]').focus();
    await page.keyboard.press('Tab');
    await expect(page.locator('a[data-action="showcase-next"]')).toBeFocused();
    await expectClearOfBar(page);
  });
}

test('closes the sheet and focuses the rail when the viewport grows', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await serve(page, () => ({ body: listing([item(1)]) }));
  await open(page, '/showcase?role=backend');
  await filtersButton(page).click();
  const dialog = page.getByRole('dialog', { name: 'Filters' });
  await expect(dialog).toBeVisible();

  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(dialog).toHaveCount(0);
  await expect(rail(page)).toBeVisible();
  const pressed = rail(page).locator('[data-role][aria-pressed="true"]');
  await expect(pressed).toHaveAttribute('data-role', 'backend');
  await expect(pressed).toBeFocused();
});

test('the invite card ends the list and links by sign-in and registration',
  async ({ page }) => {
    await serve(page, () => ({
      body: listing([item(1), item(2), item(3)]),
    }));
    await open(page, '/showcase');
    const invite = page.getByTestId('showcase-invite');
    await expect(invite).toBeVisible();
    await expect(invite.locator('h2')).toHaveText('Want your resume here?');
    await expect(invite).toContainText(
      'When you publish, turn on Show in the community showcase. '
      + 'You can turn it off any time.',
    );
    // The last cell of the grid, and not a tile.
    expect(await invite.evaluate((node) =>
      node.tagName === 'LI' && node.nextElementSibling === null)).toBe(true);
    await expect(invite.locator('[data-showcase-tile]')).toHaveCount(0);
    await expect(page.locator('[data-showcase-slug]')).toHaveCount(3);
    const create = invite.locator('[data-action="showcase-invite-create"]');
    await expect(create).toHaveText('Create a free resume');
    await expect(create).toHaveAttribute('href', '/register');
  });

test('the invite button opens sign-in when registration is off', async ({
  page,
}) => {
  await serve(
    page,
    () => ({ body: listing([item(1)]) }),
    { passwordRegistration: false },
  );
  await open(page, '/showcase');
  await expect(page.locator('[data-action="showcase-invite-create"]'))
    .toHaveAttribute('href', '/login');
});

interface ShiftSource {
  readonly node: Node | null;
  readonly previousRect: DOMRectReadOnly;
  readonly currentRect: DOMRectReadOnly;
}
interface ShiftEntry extends PerformanceEntry {
  readonly hadRecentInput: boolean;
  readonly sources: readonly ShiftSource[];
}

// docs/design/ui/showcase.md "Page frame": the header, the bar, the chip
// row, and the rail render on the server and never move after load.
for (const width of [390, 1440]) {
  test(`no layout shift in the header, bar, chips, or rail at ${width} px`,
    async ({ page }) => {
      await page.addInitScript(() => {
        const regions = [
          'showcase-header',
          'showcase-filter-bar',
          'showcase-active-filters',
          'showcase-filter-rail',
        ];
        const hits: string[] = [];
        (window as unknown as { __shiftHits: string[] }).__shiftHits = hits;
        new PerformanceObserver((list) => {
          for (const entry of list.getEntries() as ShiftEntry[]) {
            if (entry.hadRecentInput) continue;
            for (const source of entry.sources) {
              const node = source.node;
              const element = node instanceof Element
                ? node
                : node?.parentElement ?? null;
              for (const region of regions) {
                const inside = element?.closest(
                  `[data-testid="${region}"]`,
                );
                if (inside) {
                  // Name the moved node and both boxes, so a failure says
                  // what moved and by how much.
                  const box = (rect: DOMRectReadOnly): string =>
                    `${rect.x},${rect.y} ${rect.width}x${rect.height}`;
                  const tag = element?.tagName.toLowerCase() ?? 'text';
                  hits.push(`${region} ${tag} `
                    + `${box(source.previousRect)} -> `
                    + `${box(source.currentRect)}`);
                }
              }
            }
          }
        }).observe({ type: 'layout-shift', buffered: true });
      });
      await page.setViewportSize({ width, height: 900 });
      await serve(page, () => ({ body: listing(SIX) }));
      await open(page, '/showcase?role=backend&lang=vi');
      await expect(page.locator('[data-showcase-slug]')).toHaveCount(6);
      await expect(page.getByTestId('showcase-active-filters')).toBeVisible();
      await waitForImages(page);
      await page.evaluate(() => new Promise<void>((done) => {
        requestAnimationFrame(() => requestAnimationFrame(() => done()));
      }));
      const hits = await page.evaluate(() =>
        (window as unknown as { __shiftHits: string[] }).__shiftHits);
      expect(hits).toEqual([]);
    });
}
