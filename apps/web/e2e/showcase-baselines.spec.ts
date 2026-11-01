import { expect, test, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import {
  measureShowcaseGrid,
  measureShowcaseSheet,
  measureTextContainment,
} from './display-assertions';

import {
  CHROME_PIXEL_TOLERANCE,
  mockSignedOutSession,
  verifyScreenshot,
  waitForImages,
} from './support';

// Pixel baselines for the community showcase (/showcase) with a stubbed
// listing: twelve tiles and a pager over three pages, and the empty state,
// in both languages and themes at phone and desktop width, signed out. Five
// more cases cover the open filter sheet, three active filters, and the
// tablet width (docs/design/ui/showcase.md). The card images are stored card
// baselines, so the tiles hold real card art.

const ORIGIN = 'http://127.0.0.1:20092';
const CARDS = [
  'card--accent-light',
  'card--no-photo-en',
  'card--short-name-en',
  'card--long-name-vi',
  'card--cjk-name',
] as const;
const TEMPLATES = [
  'engineer-compact',
  'one-page-tight',
  null,
  'nordic-muted',
  'executive-band',
  'modern-sidebar',
] as const;
const ROLES = [
  'backend',
  'frontend',
  null,
  'devops',
  'data-ai',
  'qa',
  'fresher',
  'other',
  null,
] as const;
const LANGUAGES = ['vi', 'en', 'other'] as const;

const FILLED = {
  items: Array.from({ length: 12 }, (_, index) => ({
    slug: `resume-${index}`,
    cardVersion: '0123456789abcdef',
    imageText: `Resume card ${index + 1}`,
    language: LANGUAGES[index % LANGUAGES.length],
    templateId: TEMPLATES[index % TEMPLATES.length],
    role: ROLES[index % ROLES.length],
  })),
  page: 2,
  pageCount: 3,
  total: 30,
};
const EMPTY = { items: [], page: 1, pageCount: 0, total: 0 };

const FILTERED_PATH = '/showcase?role=backend&lang=vi'
  + '&template=engineer-compact&page=2';

interface Case {
  readonly name: 'filled' | 'empty' | 'sheet' | 'filtered';
  readonly path: string;
  readonly body: typeof FILLED | typeof EMPTY;
  readonly locale: 'vi' | 'en';
  readonly theme: 'light' | 'dark';
  readonly width: number;
}

const CASES: Case[] = [];
for (const [name, path, body] of [
  ['filled', '/showcase?page=2', FILLED],
  ['empty', '/showcase', EMPTY],
] as const) {
  for (const locale of ['vi', 'en'] as const) {
    for (const theme of ['light', 'dark'] as const) {
      for (const width of [390, 1440]) {
        CASES.push({ name, path, body, locale, theme, width });
      }
    }
  }
}
CASES.push(
  {
    name: 'sheet',
    path: '/showcase?page=2',
    body: FILLED,
    locale: 'vi',
    theme: 'dark',
    width: 390,
  },
  {
    name: 'sheet',
    path: '/showcase?page=2',
    body: FILLED,
    locale: 'en',
    theme: 'light',
    width: 390,
  },
  {
    name: 'filtered',
    path: FILTERED_PATH,
    body: FILLED,
    locale: 'vi',
    theme: 'dark',
    width: 390,
  },
  {
    name: 'filtered',
    path: FILTERED_PATH,
    body: FILLED,
    locale: 'en',
    theme: 'light',
    width: 1440,
  },
  {
    name: 'filled',
    path: '/showcase?page=2',
    body: FILLED,
    locale: 'vi',
    theme: 'light',
    width: 768,
  },
);

async function openShowcase(
  page: Page,
  { path, body, locale, theme, width }: Case,
): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  await mockSignedOutSession(page);
  await page.route(/\/api\/v1\/public\/showcase(?:\?.*)?$/u, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: { 'cache-control': 'no-store, no-transform' },
      body: JSON.stringify(body),
    }));
  await page.route(
    /\/api\/v1\/public\/resumes\/resume-(\d+)\/og\/[0-9a-f]{16}\.png$/u,
    async (route) => {
      const index = Number(
        /resume-(\d+)/u.exec(route.request().url())?.[1] ?? '0',
      );
      const file = CARDS[index % CARDS.length]!;
      await route.fulfill({
        status: 200,
        contentType: 'image/png',
        body: await readFile(resolve(
          import.meta.dirname,
          'baselines',
          `${file}.png`,
        )),
      });
    },
  );
  await page.context().addCookies([
    { name: 'aboutme-locale', value: locale, url: ORIGIN },
    { name: 'aboutme-theme', value: theme, url: ORIGIN },
  ]);

  const response = await page.goto(path);
  expect(response?.status()).toBe(200);
  await page.addStyleTag({
    content: '*, *::before, *::after { transition: none !important; '
      + 'animation: none !important; }',
  });
  await expect(page.getByTestId('app-shell')).toBeVisible();
  await expect(page.locator(`[data-state="${
    body.items.length === 0 ? 'empty' : 'list'}"]`)).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
}

async function assertShowcase(
  page: Page,
  name: Case['name'],
  locale: Case['locale'],
  theme: Case['theme'],
  width: number,
): Promise<void> {
  const header = page.getByTestId('showcase-header');
  const title = locale === 'vi' ? 'CV từ cộng đồng' : 'Community resumes';
  const lead = locale === 'vi'
    ? 'CV thật do người dùng aboutme.vn xuất bản và chọn hiện ở đây.'
    : 'Real resumes that aboutme.vn users published and chose to show here.';
  await expect(header).toMatchAriaSnapshot(`- heading "${title}" [level=1]`);
  await expect(header.getByRole('heading')).toHaveText(title);
  await expect(header.locator('p')).toHaveText(lead);
  await expect.poll(() => measureTextContainment(header))
    .toEqual({ visible: true, contained: true });
  await expect(header.locator('h1')).toHaveCSS(
    'color', theme === 'light' ? 'rgb(16, 27, 63)' : 'rgb(243, 241, 236)',
  );
  await expect(header.locator('h1')).toHaveCSS('font-weight', '700');
  await expect(header.locator('h1')).toHaveCSS(
    'font-size', width >= 1024 ? '40px' : width >= 641 ? '32px' : '28px',
  );
  await expect(header.locator('h1')).toHaveCSS(
    'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
  );
  expect(await page.evaluate(() => (
    document.fonts.check('700 28px "Be Vietnam Pro"', 'CV từ cộng đồng')
    && Array.from(document.fonts).some((face) =>
      face.family === 'Be Vietnam Pro' && face.status === 'loaded')
  ))).toBe(true);
  await expect(page.getByTestId('showcase-count')).toHaveText(
    locale === 'vi'
      ? `${name === 'empty' ? '0' : '30'} CV · sớm nhất trước`
      : `${name === 'empty' ? '0' : '30'} resumes · earliest added first`,
  );
  const filter = page.locator('[data-action="showcase-filters-open"]');
  if (width < 1024) {
    await expect(filter).toBeVisible();
    await expect(page.getByTestId('showcase-filter-rail')).toBeHidden();
  } else {
    await expect(filter).toBeHidden();
    await expect(page.getByTestId('showcase-filter-rail')).toBeVisible();
  }
  if (name === 'filtered') {
    const chips = page.locator('[data-action="showcase-filter-remove"]');
    await expect(chips).toHaveCount(3);
    expect(await chips.evaluateAll((nodes) =>
      nodes.map((node) => node.getAttribute('data-filter'))))
      .toEqual(['role', 'lang', 'template']);
    expect(await page.getByTestId('showcase-active-filters')
      .evaluate((group) => {
        const bounds = group.getBoundingClientRect();
        return Array.from(group.children).every((child) => {
          const box = child.getBoundingClientRect();
          return box.width > 0 && box.height > 0
            && box.left >= bounds.left - 1 && box.right <= bounds.right + 1
            && box.top >= bounds.top - 1 && box.bottom <= bounds.bottom + 1;
        });
      })).toBe(true);
    await expect(page.getByTestId('showcase-active-filters'))
      .toMatchAriaSnapshot(locale === 'vi'
        ? '- group "Bộ lọc đang bật"'
        : '- group "Active filters"');
  } else {
    await expect(page.getByTestId('showcase-active-filters')).toHaveCount(0);
  }
  const tiles = page.locator('[data-showcase-slug]');
  if (name === 'empty') {
    await expect(tiles).toHaveCount(0);
    await expect(page.getByTestId('showcase-invite')).toHaveCount(0);
    await expect(page.getByTestId('showcase-pager')).toHaveCount(0);
    await expect(page.locator('[data-state="empty"] p')).toHaveText(
      locale === 'vi'
        ? 'Chưa có CV nào ở đây. Xuất bản CV của bạn và bật Hiện trong '
          + 'trang Cộng đồng để là người đầu tiên.'
        : 'No resumes here yet. Publish your resume and turn on Show in the '
          + 'community showcase to be the first.',
    );
    await expect(page.locator('[data-action="showcase-empty-action"]'))
      .toHaveText(locale === 'vi' ? 'Tạo CV' : 'Create your resume');
    return;
  }
  await expect(tiles).toHaveCount(12);
  for (let index = 0; index < 12; index++) {
    const tile = tiles.nth(index);
    await expect(tile).toHaveAttribute('data-showcase-slug', `resume-${index}`);
    await expect(tile.locator('[data-showcase-tile]'))
      .toHaveAttribute('href', `/resume-${index}`);
    const image = tile.locator('img');
    await image.scrollIntoViewIfNeeded();
    await expect(image).toHaveAttribute('alt', `Resume card ${index + 1}`);
    await expect(image).toHaveAttribute('src',
      `/api/v1/public/resumes/resume-${index}/og/0123456789abcdef.png`);
    expect(await image.evaluate(async (element) => {
      const img = element as HTMLImageElement;
      await img.decode();
      const box = img.getBoundingClientRect();
      const frame = img.parentElement!.getBoundingClientRect();
      return img.naturalWidth === 1200 && img.naturalHeight === 630
        && Math.abs(frame.width - frame.height * 1200 / 630) <= 1
        && Math.abs(box.width - frame.width) <= 1
        && box.height > 0 && getComputedStyle(img).objectFit === 'cover';
    })).toBe(true);
    await expect(tile).toHaveCSS('background-color', theme === 'light'
      ? 'rgb(255, 255, 255)' : 'rgb(20, 26, 46)');
    await expect.poll(() => measureTextContainment(
      tile.locator('.showcase-tile__meta'),
    )).toEqual({ visible: true, contained: true });
  }
  await page.evaluate(() => window.scrollTo(0, 0));
  const grid = page.locator('.showcase-grid');
  await expect(grid).toMatchAriaSnapshot('- list');
  await expect(grid.locator(':scope > li')).toHaveCount(13);
  const resultsWidth = await page.getByTestId('showcase-results')
    .evaluate((element) => element.getBoundingClientRect().width);
  const columns = width < 641 ? 1
    : width >= 1024 && resultsWidth >= 860 ? 3 : 2;
  const gap = width >= 1024 ? 28 : width >= 641 ? 24 : 16;
  await expect.poll(() => measureShowcaseGrid(grid, columns, gap)).toBe(true);
  const invite = page.getByTestId('showcase-invite');
  await expect(invite.getByRole('heading')).toHaveText(locale === 'vi'
    ? 'Muốn CV của bạn ở đây?' : 'Want your resume here?');
  await expect(invite.getByRole('link')).toHaveText(locale === 'vi'
    ? 'Tạo CV miễn phí' : 'Create a free resume');
  await expect(invite).toHaveCSS('background-color', theme === 'light'
    ? 'rgb(246, 238, 223)' : 'rgb(35, 29, 22)');
  await expect.poll(() => measureTextContainment(invite))
    .toEqual({ visible: true, contained: true });
  const pager = page.getByTestId('showcase-pager');
  await expect(pager).toMatchAriaSnapshot(locale === 'vi'
    ? '- navigation "Trang 2/3"'
    : '- navigation "Page 2 of 3"');
  await expect(pager.locator('[data-action="showcase-previous"]'))
    .toHaveText(locale === 'vi' ? 'Trang trước' : 'Previous');
  await expect(pager.locator('[data-action="showcase-next"]'))
    .toHaveText(locale === 'vi' ? 'Trang sau' : 'Next');
}

for (const { name, path, body, locale, theme, width } of CASES) {
  const title = `showcase ${name} ${locale} ${theme} ${width}px `
    + 'matches baseline';
  test(title, async ({ page }, testInfo) => {
    test.setTimeout(60_000);
    await openShowcase(page, { name, path, body, locale, theme, width });
    await assertShowcase(page, name, locale, theme, width);

    if (name !== 'empty') {
      await expect(page.locator('[data-showcase-slug]')).toHaveCount(12);
      // Load every lazy card, so the capture never depends on scroll or
      // decode timing.
      await page.locator('img[loading="lazy"]').evaluateAll((images) => {
        for (const image of images) {
          (image as HTMLImageElement).loading = 'eager';
        }
      });
      await waitForImages(page);
    }
    if (name === 'filled' || name === 'filtered') {
      // Grow the viewport to the page, so the capture holds every tile.
      const height = await page.evaluate(() =>
        document.documentElement.scrollHeight);
      await page.setViewportSize({ width, height });
    }
    if (name === 'sheet') {
      // The sheet is a fixed overlay: a phone-height viewport, captured
      // without the full page, shows it as a visitor sees it.
      await page.locator('[data-action="showcase-filters-open"]').click();
      await expect(page.getByRole('dialog')).toBeVisible();
      await expect(page.getByTestId('showcase-filter-sheet')).toBeVisible();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toMatchAriaSnapshot(locale === 'vi'
        ? '- dialog "Bộ lọc"'
        : '- dialog "Filters"');
      await expect.poll(() => measureShowcaseSheet(dialog)).toBe(true);
      await expect.poll(() => dialog.evaluate((node) =>
        node.contains(document.activeElement))).toBe(true);
      await expect(dialog.getByTestId('showcase-roles')).toBeVisible();
      await expect(dialog.getByTestId('showcase-languages')).toBeVisible();
      await expect(dialog.getByRole('combobox')).toBeVisible();
      expect(await dialog.evaluate((node) => {
        const body = node.querySelector<HTMLElement>('.overflow-y-auto');
        if (!body) return false;
        body.scrollTop = body.scrollHeight;
        const reachesEnd = body.scrollTop + body.clientHeight
          >= body.scrollHeight - 1;
        body.scrollTop = 0;
        return reachesEnd;
      })).toBe(true);
      await expect(dialog.locator('[data-action="showcase-filters-apply"]'))
        .toHaveText(locale === 'vi' ? 'Xem 30 CV' : 'Show 30 resumes');
      await page.setViewportSize({ width, height: 844 });
      // Wait for the slide to end: the sheet's box holds still over frames.
      await page.getByRole('dialog').evaluate((node) =>
        new Promise<void>((done) => {
          let last = node.getBoundingClientRect().top;
          let still = 0;
          const frame = () => {
            const top = node.getBoundingClientRect().top;
            still = top === last ? still + 1 : 0;
            last = top;
            if (still >= 3) done();
            else requestAnimationFrame(frame);
          };
          requestAnimationFrame(frame);
        }));
    }
    await page.evaluate(() => new Promise<void>((done) => {
      requestAnimationFrame(() => requestAnimationFrame(() => done()));
    }));

    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
      - document.documentElement.clientWidth);
    expect(overflow).toBe(0);

    await verifyScreenshot(
      page,
      `showcase--${name}--${locale}--${theme}--${width}.png`,
      testInfo,
      undefined,
      CHROME_PIXEL_TOLERANCE,
      name !== 'sheet',
    );
  });
}

for (const locale of ['vi', 'en'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [640, 641, 1023, 1024]) {
      test(`showcase grid boundary ${locale} ${theme} ${width}`, async ({
        page,
      }) => {
        await openShowcase(page, {
          name: 'filled', path: '/showcase?page=2', body: FILLED,
          locale, theme, width,
        });
        const grid = page.locator('.showcase-grid');
        const columns = width < 641 ? 1 : 2;
        const gap = width >= 1024 ? 28 : width >= 641 ? 24 : 16;
        await expect.poll(() => measureShowcaseGrid(grid, columns, gap))
          .toBe(true);
        const filter = page.locator('[data-action="showcase-filters-open"]');
        if (width < 1024) {
          await expect(filter).toBeVisible();
          await filter.click();
          await expect.poll(() => measureShowcaseSheet(
            page.getByRole('dialog'),
          )).toBe(true);
        } else {
          await expect(filter).toBeHidden();
          await expect(page.getByRole('dialog')).toHaveCount(0);
        }
      });
    }
    for (const size of [859, 860]) {
      test(`showcase container boundary ${locale} ${theme} ${size}`, async ({
        page,
      }) => {
        await openShowcase(page, {
          name: 'filled', path: '/showcase?page=2', body: FILLED,
          locale, theme, width: 1440,
        });
        await page.getByTestId('showcase-results').evaluate((node, value) => {
          (node as HTMLElement).style.width = `${value}px`;
        }, size);
        await expect.poll(() => measureShowcaseGrid(
          page.locator('.showcase-grid'), size === 859 ? 2 : 3, 28,
        )).toBe(true);
      });
    }
  }
}

test('showcase assertions detect grid, content, control and color changes',
  async ({ page }) => {
    await openShowcase(page, {
      name: 'filled', path: '/showcase?page=2', body: FILLED,
      locale: 'vi', theme: 'light', width: 390,
    });
    const grid = page.locator('.showcase-grid');
    const tiles = page.locator('[data-showcase-slug]');
    const filter = page.locator('[data-action="showcase-filters-open"]');
    const heading = page.getByTestId('showcase-header').locator('h1');
    expect(await measureShowcaseGrid(grid, 1, 16)).toBe(true);
    await grid.evaluate((node) => {
      (node as HTMLElement).style.gridTemplateColumns = 'repeat(2, 1fr)';
    });
    expect(await measureShowcaseGrid(grid, 1, 16)).toBe(false);
    await grid.evaluate((node) => {
      (node as HTMLElement).style.gridTemplateColumns = '';
      (node.children[1] as HTMLElement).style.marginTop = '-4px';
    });
    expect(await measureShowcaseGrid(grid, 1, 16)).toBe(false);
    await tiles.nth(1).evaluate((node) => {
      (node as HTMLElement).style.marginTop = '';
      node.parentElement!.prepend(node);
    });
    expect(await tiles.first().getAttribute('data-showcase-slug'))
      .not.toBe('resume-0');
    await tiles.first().evaluate((node) => {
      node.after(node.cloneNode(true));
    });
    expect(await tiles.count()).toBe(13);
    await filter.evaluate((node) => {
      (node as HTMLElement).style.display = 'none';
    });
    expect(await filter.isVisible()).toBe(false);
    await heading.evaluate((node) => {
      (node as HTMLElement).style.color = 'rgb(0, 0, 0)';
    });
    expect(await heading.evaluate((node) => getComputedStyle(node).color))
      .not.toBe('rgb(16, 27, 63)');
    await heading.evaluate((node) => {
      node.textContent = 'CV tu cong dong';
      node.setAttribute('role', 'presentation');
      (node as HTMLElement).style.fontFamily = 'serif';
    });
    expect(await heading.textContent()).not.toBe('CV từ cộng đồng');
    expect(await page.getByTestId('showcase-header')
      .getByRole('heading').count()).toBe(0);
    expect(await heading.evaluate((node) => getComputedStyle(node).fontFamily))
      .not.toContain('Be Vietnam Pro');
    await heading.evaluate((node) => {
      Object.assign((node as HTMLElement).style, {
        width: '20px', overflow: 'hidden', whiteSpace: 'nowrap',
      });
    });
    expect((await measureTextContainment(heading)).contained).toBe(false);
  });
