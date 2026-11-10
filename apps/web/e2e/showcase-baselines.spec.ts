import { createCanvas } from '@napi-rs/canvas';
import { expect, test, type Page } from '@playwright/test';

import { captureReviewImage } from './review-images';
import {
  expectScreen,
  expectScreenClass,
  measureShowcaseGrid,
  measureShowcaseSheet,
  measureTextContainment,
} from './display-assertions';
import {
  applyStyleMutation, mutateAttribute, mutationTest, proveMutation,
  screenMutationTests,
} from './style-mutation';
import {
  expectShowcaseDetails, expectShowcaseFlow, expectShowcaseHeading,
  expectShowcaseList,
  expectShowcasePager, showcaseType,
} from './showcase-details';
import { styleProofs } from './screen-style-proofs';
import { expectSheetStructure, sheetType } from './filter-sheet-details';

import {
  chromeScreenContract,
  filterSheetContract,
  mockSignedOutSession,
  waitForImages,
} from './support';

// Synthetic cards keep the showcase independent of committed test images.
const CARDS = Array.from({ length: 5 }, (_, index) => {
  const canvas = createCanvas(1200, 630);
  const context = canvas.getContext('2d');
  context.fillStyle = ['#fbfaf8', '#f3f1ec', '#ede9e1',
    '#ffffff', '#e0e7ff'][index]!;
  context.fillRect(0, 0, 1200, 630);
  context.fillStyle = '#101b3f';
  context.fillRect(120 + index * 30, 100, 300, 430);
  return canvas.toBuffer('image/png');
});

const ORIGIN = 'http://127.0.0.1:20092';
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
    test.describe(`interface ${locale}`, () => {
      test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
      for (const theme of ['light', 'dark'] as const) {
        for (const width of [390, 1440]) {
          CASES.push({ name, path, body, locale, theme, width });
        }
      }
    });
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
      const image = CARDS[index % CARDS.length]!;
      await route.fulfill({
        status: 200,
        contentType: 'image/png',
        body: image,
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
  const state = body.items.length === 0 ? 'empty' : 'list';
  await expect(page.locator(`[data-state="${state}"]`)).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
}

async function assertShowcase(
  page: Page,
  name: Case['name'],
  locale: Case['locale'],
  theme: Case['theme'],
  width: number,
): Promise<void> {
  await expectScreen(page, showcaseContract(locale, theme, name === 'empty'));
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
  await expectShowcaseHeading(page, 'color', theme);
  await expect(header.locator('h1')).toHaveCSS('font-weight', '700');
  await expect(header.locator('h1')).toHaveCSS(
    'font-size', width >= 1024 ? '40px' : width >= 641 ? '32px' : '28px',
  );
  await expectShowcaseHeading(page, 'font-family', theme);
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
    await expect(tiles, 'Showcase tile count.').toHaveCount(0);
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
    await expectShowcaseDetails(page, locale, width, true, false);
    return;
  }
  await expect(tiles, 'Showcase tile count.').toHaveCount(12);
  for (let index = 0; index < 12; index++) {
    const tile = tiles.nth(index);
    await expect(tile, 'Showcase tile order.').toHaveAttribute(
      'data-showcase-slug', `resume-${index}`);
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
      ? 'rgb(255, 255, 255)'
      : 'rgb(20, 26, 46)');
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
  const columns = width < 641
    ? 1
    : width >= 1024 && resultsWidth >= 860
      ? 3
      : 2;
  const gap = width >= 1024 ? 28 : width >= 641 ? 24 : 16;
  await expect.poll(() => measureShowcaseGrid(grid, columns, gap),
    'Showcase grid geometry.').toBe(true);
  const invite = page.getByTestId('showcase-invite');
  await expect(invite.getByRole('heading')).toHaveText(locale === 'vi'
    ? 'Muốn CV của bạn ở đây?'
    : 'Want your resume here?');
  await expect(invite.getByRole('link')).toHaveText(locale === 'vi'
    ? 'Tạo CV miễn phí'
    : 'Create a free resume');
  await expect(invite).toHaveCSS('background-color', theme === 'light'
    ? 'rgb(246, 238, 223)'
    : 'rgb(35, 29, 22)');
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
  await expectShowcaseDetails(page, locale, width, false, name === 'filtered');
}

for (const { name, path, body, locale, theme, width } of CASES) {
  test.describe(`display locale ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    const title = `showcase ${name} ${locale} ${theme} ${width}px `
      + 'keeps content and layout';
    test(title, async ({ page }, testInfo) => {
      test.setTimeout(60_000);
      await openShowcase(page, { name, path, body, locale, theme, width });
      await assertShowcase(page, name, locale, theme, width);

      if (name !== 'empty') {
        await expect(page.locator('[data-showcase-slug]')).toHaveCount(12);
        await waitForImages(page);
      }
      if (name === 'sheet') {
        // The sheet is a fixed overlay: a phone-height viewport, captured
        // without the full page, shows it as a visitor sees it.
        await page.locator('[data-action="showcase-filters-open"]').click();
        await expect(page.getByRole('dialog')).toBeVisible();
        await expect(page.getByTestId('showcase-filter-sheet')).toBeVisible();
        const dialog = page.getByRole('dialog');
        await expectScreen(page,
          filterSheetContract('showcase', locale, theme));
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
          if (!body) {
            return false;
          }
          body.scrollTop = body.scrollHeight;
          const reachesEnd = body.scrollTop + body.clientHeight
            >= body.scrollHeight - 1;
          body.scrollTop = 0;
          return reachesEnd;
        })).toBe(true);
        await expect(dialog.locator('[data-action="showcase-filters-apply"]'))
          .toHaveText(locale === 'vi' ? 'Xem 30 CV' : 'Show 30 resumes');
        await expect.poll(() => measureShowcaseSheet(dialog)).toBe(true);
      }
      const overflow = await page.evaluate(() =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth);
      expect(overflow).toBe(0);

      if (name === 'filled' && locale === 'en' && theme === 'dark'
        && width === 1440) {
        await captureReviewImage(testInfo, 'showcase-en-dark-1440', () =>
          page.screenshot({ animations: 'disabled', timeout: 750 }));
      }
    });
  });
}

for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
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
          await expect.poll(() => measureShowcaseGrid(grid, columns, gap),
            'Showcase grid geometry.')
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
  });
}

const SHOWCASE_MUTATIONS = [
  ['columns', '.showcase-grid', {
    'grid-template-columns': [
      'repeat(2, 1fr)', /^\d+(?:\.\d+)?px \d+(?:\.\d+)?px$/u,
    ],
  }],
  ['row gap', '.showcase-grid > :nth-child(2)', { 'margin-top': '4px' }],
  ['hidden filter', '[data-action="showcase-filters-open"]', {
    display: 'none',
  }],
  ['heading color', '[data-testid="showcase-header"] h1', {
    color: 'rgb(0, 0, 0)',
  }],
  ['heading font', '[data-testid="showcase-header"] h1', {
    'font-family': 'serif',
  }],
  ['clipped heading', '[data-testid="showcase-header"] h1', {
    'width': '20px', 'overflow': 'hidden', 'white-space': 'nowrap',
  }],
] as const;

for (const [name, selector, styles] of SHOWCASE_MUTATIONS) {
  mutationTest(`showcase assertions reject ${name}`, async ({ page }) => {
    test.setTimeout(60_000);
    await openShowcase(page, {
      name: 'filled', path: '/showcase?page=2', body: FILLED,
      locale: 'vi', theme: 'light', width: 390,
    });
    const assertClean = () =>
      assertShowcase(page, 'filled', 'vi', 'light', 390);
    await assertClean();
    const grid = name === 'columns' || name === 'row gap';
    const property = name === 'heading color' ? 'color' : 'font-family';
    const message = grid
      ? 'Showcase grid geometry.'
      : name === 'clipped heading'
        ? 'Screen containment.'
        : name === 'hidden filter'
          ? 'Showcase filter visibility.'
          : `Showcase heading ${property}.`;
    const assertion = () => grid
      ? expect.poll(() => measureShowcaseGrid(
          page.locator('.showcase-grid'), 1, 16,
        ), message).toBe(true)
      : name === 'clipped heading'
        ? expectScreenClass(page, showcaseContract('vi', 'light'),
            'containment')
        : name === 'hidden filter'
          ? expect(page.locator(selector), message).toBeVisible()
          : expectShowcaseHeading(page, property, 'light');
    await proveMutation(assertion, () => applyStyleMutation(
      page.locator(selector), styles,
    ), message);
    await assertClean();
  });
}

for (const name of ['order', 'count', 'copy', 'heading role'] as const) {
  mutationTest(`showcase assertions reject changed ${name}`, async ({
    page,
  }) => {
    test.setTimeout(60_000);
    await openShowcase(page, {
      name: 'filled', path: '/showcase?page=2', body: FILLED,
      locale: 'vi', theme: 'light', width: 390,
    });
    const assertClean = () =>
      assertShowcase(page, 'filled', 'vi', 'light', 390);
    await assertClean();
    const before = await page.locator('.showcase-wrap').innerHTML();
    await page.evaluate((kind) => {
      const grid = document.querySelector('.showcase-grid')!;
      const heading = document.querySelector(
        '[data-testid="showcase-header"] h1',
      )!;
      if (kind === 'order') {
        grid.prepend(grid.children[1]!);
      }
      if (kind === 'count') {
        grid.append(grid.children[0]!.cloneNode(true));
      }
      if (kind === 'copy') {
        heading.textContent = 'CV tu cong dong';
      }
      if (kind === 'heading role') {
        heading.setAttribute('role', 'presentation');
      }
    }, name);
    expect(await page.locator('.showcase-wrap').innerHTML()).not.toBe(before);
    await expect(assertClean()).rejects.toThrow(
      name === 'copy'
        ? 'Screen content.'
        : name === 'heading role'
          ? 'Screen accessibility.'
          : name === 'order'
            ? 'Showcase tile order.'
            : 'Showcase tile count.');
    await page.reload();
    await assertClean();
  });
}

function showcaseContract(
  locale: 'vi' | 'en', theme: 'light' | 'dark', empty = false,
) {
  return chromeScreenContract('[data-testid="showcase-page"]',
    '[data-testid="showcase-header"] :is(h1,p), '
    + '.showcase-tile__meta, [data-testid="showcase-invite"] :is(h2,p,a), '
    + '[data-state="empty"] p',
    '[data-testid="showcase-header"] h1, [data-testid="showcase-header"] p',
    '[data-testid="showcase-header"] h1', locale === 'vi'
      ? 'CV từ cộng đồng'
      : 'Community resumes', theme,
    empty ? undefined : '[data-showcase-slug] img');
}

screenMutationTests('showcase', async (page) => {
  await openShowcase(page, {
    name: 'filled', path: '/showcase?page=2', body: FILLED,
    locale: 'vi', theme: 'light', width: 390,
  });
}, showcaseContract('vi', 'light'));

for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const theme of ['light', 'dark'] as const) {
      for (const width of [390, 1440]) {
        test(`showcase filtered display ${locale} ${theme} ${width}`, async ({
          page,
        }) => {
          await openShowcase(page, {
            name: 'filtered', path: FILTERED_PATH, body: FILLED,
            locale, theme, width,
          });
          await assertShowcase(page, 'filtered', locale, theme, width);
          if (width === 390) {
            await page.locator('[data-action="showcase-filters-open"]').click();
            await expect.poll(() =>
              measureShowcaseSheet(page.getByRole('dialog')))
              .toBe(true);
          } else await expect(page.getByRole('dialog')).toHaveCount(0);
        });
      }
    }
  });
}

screenMutationTests('showcase sheet', async (page) => {
  await openShowcase(page, {
    name: 'filled', path: '/showcase?page=2', body: FILLED,
    locale: 'vi', theme: 'light', width: 390,
  });
  await page.locator('[data-action="showcase-filters-open"]').click();
}, filterSheetContract('showcase', 'vi', 'light'));

async function openFilledShowcase(page: Page): Promise<void> {
  await openShowcase(page, {
    name: 'filled', path: '/showcase?page=2', body: FILLED,
    locale: 'vi', theme: 'light', width: 390,
  });
}

styleProofs('showcase', openFilledShowcase, showcaseType(390, false));

mutationTest('showcase rejects displaced result flow', async ({ page }) => {
  await openFilledShowcase(page);
  await proveMutation(() => expectShowcaseFlow(page),
    () => applyStyleMutation(page.getByTestId('showcase-results'), {
      transform: ['translateY(-1000px)', /matrix/u],
    }), 'Showcase flow separation.');
});

for (const scope of ['list', 'pager'] as const) {
  mutationTest(`showcase rejects changed ${scope} semantics`, async ({
    page,
  }) => {
    await openFilledShowcase(page);
    await proveMutation(() => scope === 'list'
      ? expectShowcaseList(page, 'vi')
      : expectShowcasePager(page, 'vi', false),
    () => mutateAttribute(scope === 'list'
      ? page.locator('.showcase-grid')
      : page.getByTestId('showcase-pager'),
    'role', 'group'), scope === 'list'
      ? 'Showcase accessible list.'
      : 'Showcase pager structure.');
  });
}

styleProofs('showcase sheet', async (page) => {
  await openFilledShowcase(page);
  await page.locator('[data-action="showcase-filters-open"]').click();
}, sheetType('showcase'));

mutationTest('showcase sheet rejects missing option semantics', async ({
  page,
}) => {
  await openFilledShowcase(page);
  await page.locator('[data-action="showcase-filters-open"]').click();
  await proveMutation(() => expectSheetStructure(page, 'showcase', 'vi'),
    () => mutateAttribute(page.getByTestId('showcase-filter-sheet')
      .getByTestId('showcase-roles'), 'role', 'presentation'),
    'Sheet accessible structure.');
});
