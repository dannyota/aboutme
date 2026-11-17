import {
  type Browser,
  chromium,
  expect,
  type Page,
  test,
  webkit,
} from '@playwright/test';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';
import { readFileSync } from 'node:fs';
import { resolve as resolvePath } from 'node:path';

import { captureReviewImage } from './review-images';
import {
  expectScreen,
  measureChromeGrid,
  measureChromeSheet,
  measureTextContainment,
  measureVerticalFlow,
} from './display-assertions';
import {
  applyStyleMutation, mutateAttribute, mutationTest, proveMutation,
  screenMutationTests,
} from './style-mutation';
import {
  expectChromeDecoration, expectChromeFan, expectChromeList,
  expectAuthArtworkClear, expectChromeNavigation, expectLibraryFan,
  expectRendererBoundary, expectSampleIsolation, expectSlugPrefixCell,
  leakChromeIntoResume, placeAuthArtworkOverPoints,
} from './chrome-details';

import type { ScreenContract } from './screen-contract';
import {
  chromeScreenContract,
  expectThumbnailRenders,
  mockSignedInSession,
  prepareScreen,
} from './support';

const LIVE_THUMBNAILS = TEMPLATES.length
  - new Set(SAMPLES.map(({ templateId }) => templateId)).size;

const PAGES = [
  { name: 'home', path: '/', thumbnails: 0 },
  { name: 'login', path: '/login', thumbnails: 0 },
  { name: 'templates', path: '/templates', thumbnails: LIVE_THUMBNAILS },
] as const;
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;

test('live thumbnails are checked at the requested viewport', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => {
    const root = document.documentElement;
    root.dataset.maxViewportHeight = String(innerHeight);
    addEventListener('resize', () => {
      root.dataset.maxViewportHeight = String(Math.max(innerHeight,
        Number(root.dataset.maxViewportHeight)));
    });
  });
  await expectThumbnailRenders(page, LIVE_THUMBNAILS);
  expect(page.viewportSize()).toEqual({ width: 390, height: 900 });
  await expect(page.locator('html'))
    .toHaveAttribute('data-max-viewport-height', '900');
  expect(await page.evaluate(() => ({ x: scrollX, y: scrollY })))
    .toEqual({ x: 0, y: 0 });
  await expect(page.locator('h1')).toHaveCount(1);
});

const EDITOR_DOCUMENT = JSON.parse(readFileSync(resolvePath(
  import.meta.dirname, '../../../packages/schema/fixtures/full.json',
), 'utf8')) as { personalDetails?: { photo?: unknown } };
delete EDITOR_DOCUMENT.personalDetails?.photo;

async function openEditorPreview(page: Page): Promise<void> {
  await mockSignedInSession(page);
  await page.route('**/api/v1/events', async (route) => {
    await route.fulfill({
      status: 200, contentType: 'text/event-stream', body: '',
    });
  });
  await page.route('**/api/v1/resumes/resume-1', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      headers: {
        'Content-Type': 'application/json',
        'Cache-Control': 'no-store, no-transform',
        'ETag': '"r1"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
      json: {
        data: {
          id: 'resume-1', title: 'Fixture', lng: 'en', live: false,
          downloadEnabled: false, seoGeoEnabled: false, slug: null,
          publicTitle: null, faviconEmoji: null,
          schemaVersion: CURRENT_VERSION,
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
          revision: '1',
          document: EDITOR_DOCUMENT,
        },
      },
    });
  });
  const response = await page.goto('/app/resumes/resume-1');
  expect(response?.status()).toBe(200);
  await expect(page.getByTestId('save-status')).toBeVisible();
}

for (const surface of ['template page', 'editor preview'] as const) {
  test(`chrome selection, scrollbar and weight stop at the resume in the `
    + `${surface}`, async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    if (surface === 'editor preview') {
      await openEditorPreview(page);
    } else {
      const response = await page.goto('/templates/creative-accent');
      expect(response?.status()).toBe(200);
    }
    await expectRendererBoundary(page);
  });
}

async function openPublishDialog(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await openEditorPreview(page);
  await page.locator('[data-action="publish"]').click();
}

test('the publish slug prefix sits in a mist cell inside the input', async ({
  page,
}) => {
  await openPublishDialog(page);
  await expectSlugPrefixCell(page);
});

for (const [kind, styles] of [
  ['a cell moved off the input', {
    transform: ['translateY(4px)', /matrix/u],
  }],
  ['a transparent cell', { opacity: ['0', '0'] }],
] as const) {
  mutationTest(`the publish slug prefix rejects ${kind}`, async ({ page }) => {
    test.setTimeout(60_000);
    await openPublishDialog(page);
    await proveMutation(() => expectSlugPrefixCell(page),
      () => applyStyleMutation(page.getByTestId('publish-slug-prefix'),
        styles), 'Publish slug prefix cell.');
  });
}

type ChromePage = typeof PAGES[number]['name'];
type Locale = 'vi' | 'en';
type Theme = typeof THEMES[number];

async function expectChrome(
  page: Page,
  name: ChromePage,
  locale: Locale,
  theme: Theme,
  width: number,
) {
  await prepareScreen(page);
  await expectScreen(page, chromeContract(name, locale, theme));
  await expectChromeNavigation(page, locale, width, name === 'templates');
  const vi = locale === 'vi';
  const header = page.getByTestId('app-shell');
  const nav = header.getByRole('navigation', {
    name: vi ? 'Điều hướng chính' : 'Primary navigation',
  });
  await expect(nav).toMatchAriaSnapshot(vi
    ? '- link "Thư viện"\n- link "Cộng đồng"'
    : '- link "Library"\n- link "Community"');
  await expect(nav.getByRole('link', {
    name: vi ? 'Thư viện' : 'Library', exact: true,
  })).toHaveAttribute('href', '/templates');
  await expect(nav.getByRole('link', {
    name: vi ? 'Cộng đồng' : 'Community', exact: true,
  })).toHaveCount(1);
  const home = name === 'home';
  if (home) {
    await expect(page.locator('html')).toHaveAttribute('data-page', 'home');
  } else {
    await expect(page.locator('html')).not.toHaveAttribute('data-page');
  }
  await expect(header).toHaveCSS('background-color', theme === 'light'
    ? 'rgb(255, 255, 255)'
    : 'rgb(14, 10, 13)');
  await expect(header).toHaveCSS('border-bottom-color', theme === 'light'
    ? 'rgb(227, 224, 242)'
    : 'rgb(46, 36, 44)');
  await expect(header).toHaveCSS('border-bottom-width', '1px');
  await expect(header).toHaveCSS('min-height', width < 768 ? '56px' : '64px');
  await expect(page.locator('body')).toHaveCSS('background-color',
    theme === 'light' ? 'rgb(255, 255, 255)' : 'rgb(14, 10, 13)');
  await expect(page.locator('body')).toHaveCSS('background-image', 'none');
  await expect(page.locator('body'), 'Chrome body font-size.')
    .toHaveCSS('font-size', '15px');
  await expect(page.locator('body')).toHaveCSS('line-height', '22.5px');
  await expect(page.locator('body')).toHaveCSS('color', bodyColor(theme));
  await expect(page.locator('body')).toHaveCSS('font-family',
    '"Plus Jakarta Sans", "Be Vietnam Pro", system-ui, sans-serif');
  expect(await page.evaluate((face) => ({
    loaded: document.fonts.check(`15px "${face}"`),
    face: Array.from(document.fonts).some((font) =>
      font.family === face && font.status === 'loaded'),
    overflow: document.documentElement.scrollWidth
      <= document.documentElement.clientWidth + 1,
  }), 'Plus Jakarta Sans'))
    .toEqual({ loaded: true, face: true, overflow: true });

  if (name === 'login') {
    const form = page.getByTestId('login-form');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(
      vi ? 'Đăng nhập' : 'Sign in',
    );
    await expect(form).toMatchAriaSnapshot(vi
      ? '- textbox "Email"\n- button "Hiện mật khẩu"\n- button "Đăng nhập"'
      : '- textbox "Email"\n- button "Show password"\n- button "Sign in"');
    await expect(form.locator('input')).toHaveCount(2);
    await expect.poll(() => measureTextContainment(form),
      'Chrome form containment.').toEqual({ visible: true, contained: true });
    await expect(form.getByRole('button', {
      name: vi ? 'Đăng nhập' : 'Sign in', exact: true,
    })).toBeEnabled();
    await expect.poll(() => measureVerticalFlow(form.locator(':scope > div, '
      + ':scope > button'))).toBe(true);
    await expect(page.getByTestId('auth-brand-panel'))[width >= 1024
      ? 'toBeVisible'
      : 'toBeHidden']();
    return;
  }

  const title = page.getByRole('heading', { level: 1 });
  const titleText = home
    ? HOME_TITLE[locale]
    : (vi ? 'Thư viện' : 'Library');
  await expect(title).toHaveText(titleText);
  await expect(title).toMatchAriaSnapshot(
    `- heading "${titleText}" [level=1]`,
  );
  await expect(title).toHaveCSS('font-weight', '800');
  await expect(title).toHaveCSS('font-size', home
    ? (width < 768 ? '40px' : width < 1280 ? '48px' : '60px')
    : (width < 768 ? '32px' : '40px'));
  await expect.poll(() => title.evaluate((node) => {
    const css = getComputedStyle(node);
    const line = parseFloat(css.lineHeight) || parseFloat(css.fontSize);
    return node.getBoundingClientRect().height >= line - 1;
  }), 'Chrome heading line box.').toBe(true);
  const titleContainer = home
    ? '[data-testid="landing-hero"]'
    : '[data-testid="gallery-header"]';
  await expect.poll(() => measureTextContainment(title, titleContainer),
    'Chrome text containment.')
    .toEqual({ visible: true, contained: true });

  if (home) {
    await expectChromeDecoration(page, theme);
    await expectChromeList(page);
    await expectChromeFan(page);
    await expect.poll(() => measureChromeSheet(
      page.getByTestId('landing-art'),
    ), 'Chrome sheet alignment.').toBe(true);
    const sheet = page.getByTestId('landing-sheet');
    await expect(sheet).toHaveCSS('background-color', 'rgb(255, 255, 255)');
    await expect(sheet).toHaveCSS('border-radius', '2px');
    await expectSampleIsolation(page);
    await expect(page.getByTestId('landing-chips'))
      .toHaveAttribute('aria-hidden', 'true');
    await expect(page.getByTestId('landing-chips').locator('[data-app-seal]'))
      .toHaveCSS('font-family', /^"Be Vietnam Pro"/u);
    await expect(page.getByTestId('landing-templates').getByRole('heading', {
      level: 2,
    })).toHaveText(vi
      ? 'Chọn mẫu hợp với nghề của bạn.'
      : 'Pick a template that fits your work.');
    const fan = page.getByTestId('landing-template-fan');
    expect(await fan.locator('img').evaluateAll((images) =>
      images.map((image) => image.getAttribute('data-template')))).toEqual([
      'ats-plain', 'engineer-compact', 'graduate-friendly', 'executive-band',
    ]);
    const images = fan.locator('img');
    for (let index = 0; index < 4; index += 1) {
      const image = images.nth(index);
      await image.scrollIntoViewIfNeeded();
      await expect.poll(() => image.evaluate(async (element, lng) => {
        const img = element as HTMLImageElement;
        try {
          await img.decode();
        } catch {
          return false;
        }
        return img.naturalWidth === 1240 && img.naturalHeight === 1754
          && img.currentSrc.endsWith(`/templates/pages/${
            img.dataset.template}-${lng}-p1.png`);
      }, locale)).toBe(true);
    }
    const avatar = page.getByTestId('landing-maker-avatar');
    await avatar.scrollIntoViewIfNeeded();
    await expect(avatar).toHaveAttribute('alt', AVATAR_ALT[locale]);
    await expect(avatar).toHaveCSS('object-fit', 'cover');
    await expect.poll(() => avatar.evaluate((element) => {
      const box = element.parentElement!.getBoundingClientRect();
      return Math.round(box.width);
    })).toBe(width < 768 ? 168 : width < 1024 ? 176 : 232);
  } else {
    const galleryHeader = page.getByTestId('gallery-header');
    await expect(galleryHeader).toHaveCSS('background-color', theme === 'light'
      ? 'rgb(236, 226, 255)'
      : 'rgb(28, 23, 47)');
    await expect(galleryHeader).toHaveCSS('border-radius',
      width < 768 ? '28px' : width < 1024 ? '36px' : '44px');
    await expectLibraryFan(page);
    const grid = page.locator('.gallery-grid');
    await expect(grid.locator(':scope > li')).toHaveCount(TEMPLATES.length);
    await expect.poll(() => measureChromeGrid(grid), 'Chrome grid geometry.')
      .toEqual({
        columns: width < 641 ? 2 : 5,
        gapX: width < 641 ? 14 : 28,
        gapY: width < 641 ? 24 : 40,
        separated: true,
      });
    const images = grid.locator('img[data-page-image]');
    await expect(images).toHaveCount(TEMPLATES.length - LIVE_THUMBNAILS);
    for (let index = 0; index < await images.count(); index += 1) {
      const image = images.nth(index);
      await image.scrollIntoViewIfNeeded();
      await expect.poll(() => image.evaluate(async (element, lng) => {
        const img = element as HTMLImageElement;
        try {
          await img.decode();
        } catch {
          return false;
        }
        const id = img.closest('[data-template]')
          ?.getAttribute('data-template');
        return img.naturalWidth === 1240 && img.naturalHeight === 1754
          && img.currentSrc.endsWith(`/templates/pages/${id}-${lng}-p1.png`)
          && Math.abs(img.naturalWidth / img.naturalHeight
            - Number(img.getAttribute('width'))
            / Number(img.getAttribute('height'))) < 0.001;
      }, locale)).toBe(true);
    }
  }
  await page.evaluate(() => window.scrollTo(0, 0));
}

test.describe('Vietnamese chrome', () => {
  test.use({ locale: 'vi-VN' });
  for (const page of PAGES) {
    for (const theme of THEMES) {
      for (const width of WIDTHS) {
        const title = `chrome ${page.name} ${theme} ${width}px `
          + 'keeps content and layout';
        test(title, async ({ page: browserPage }, testInfo) => {
          await browserPage.setViewportSize({ width, height: 900 });
          await browserPage.context().addCookies([
            {
              name: 'aboutme-locale',
              value: 'vi',
              url: 'http://127.0.0.1:20092',
            },
            {
              name: 'aboutme-theme',
              value: theme,
              url: 'http://127.0.0.1:20092',
            },
          ]);

          const response = await browserPage.goto(page.path);
          expect(response?.status()).toBe(200);
          await browserPage.addStyleTag({
            content: '*, *::before, *::after { transition: none !important; '
              + 'animation: none !important; }',
          });
          await expect(browserPage.locator('[data-testid="app-shell"]'))
            .toBeVisible();
          await browserPage.evaluate(() => document.fonts.ready);
          await prepareScreen(browserPage);

          const overflow = await browserPage.evaluate(() =>
            document.documentElement.scrollWidth
            - document.documentElement.clientWidth);
          expect(overflow).toBe(0);

          await expectChrome(browserPage, page.name, 'vi', theme, width);

          if (page.thumbnails > 0) {
            await expectThumbnailRenders(browserPage, page.thumbnails);
            await expect(browserPage.locator('h1')).toHaveCount(1);
          }

          if (page.name === 'home') {
            await expect(browserPage.locator('[data-sheet-thumbnail]'))
              .toHaveCount(0);
          }
          if (page.name === 'home' && theme === 'light' && width === 390) {
            await captureReviewImage(
              testInfo, 'home-vi-light-390', async () => {
                await browserPage.evaluate(() => window.scrollTo(0, 0));
                return browserPage.screenshot({
                  animations: 'disabled', timeout: 750,
                });
              },
            );
          }
        });
      }
    }
  }
});

for (const entry of PAGES) {
  for (const theme of THEMES) {
    for (const width of WIDTHS) {
      test(`chrome ${entry.name} en ${theme} ${width}px`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.context().addCookies([
          {
            name: 'aboutme-locale', value: 'en',
            url: 'http://127.0.0.1:20092',
          },
          {
            name: 'aboutme-theme', value: theme,
            url: 'http://127.0.0.1:20092',
          },
        ]);
        expect((await page.goto(entry.path))?.status()).toBe(200);
        await expect(page.getByTestId('app-shell')).toBeVisible();
        await page.evaluate(() => document.fonts.ready);
        await prepareScreen(page);
        await expectChrome(page, entry.name, 'en', theme, width);
      });
    }
  }
}

for (const locale of ['vi', 'en'] as const) {
  for (const theme of THEMES) {
    test(`Library grid boundaries ${locale} ${theme}`, async ({ page }) => {
      await page.context().addCookies([
        {
          name: 'aboutme-locale', value: locale,
          url: 'http://127.0.0.1:20092',
        },
        {
          name: 'aboutme-theme', value: theme,
          url: 'http://127.0.0.1:20092',
        },
      ]);
      expect((await page.goto('/templates'))?.status()).toBe(200);
      for (const width of [640, 641, 899, 900, 1023, 1024, 1179, 1180]) {
        await page.setViewportSize({ width, height: 900 });
        await expect.poll(
          () => measureChromeGrid(page.locator('.gallery-grid')),
        )
          .toEqual({
            columns: width < 641 ? 2 : width < 900 ? 3 : width < 1180 ? 4 : 5,
            gapX: width < 641 ? 14 : 28,
            gapY: width < 641 ? 24 : 40,
            separated: true,
          });
        const button = page.getByRole('button', {
          name: locale === 'vi' ? 'Bộ lọc' : 'Filters', exact: true,
        });
        if (width < 1024) {
          await expect(button).toBeVisible();
        } else {
          await expect(button).toBeHidden();
        }
      }
    });
  }
}

for (const locale of ['vi', 'en'] as const) {
  for (const width of [1023, 1024] as const) {
    test(`the Library fan shows from 64rem and clears the heading at ${width} `
      + `px (${locale})`, async ({ page, baseURL }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.context().addCookies([
        { name: 'aboutme-locale', value: locale, url: baseURL! },
      ]);
      expect((await page.goto('/templates'))?.status()).toBe(200);
      await page.evaluate(() => document.fonts.ready);
      await expectLibraryFan(page);
    });
  }
}

for (const [kind, width, target, styles] of [
  ['under the heading', 1024, 'gallery-header', { 'padding-right': '56px' }],
  ['inside the panel edge', 1024, 'gallery-fan', { right: '40px' }],
  ['not cropped', 1024, 'gallery-fan-layer', { overflow: 'visible' }],
  ['when transparent', 1024, 'gallery-fan', { opacity: '0' }],
  ['below 64rem', 1023, 'gallery-fan', { display: 'flex' }],
] as const) {
  mutationTest(`chrome rejects the Library fan ${kind}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    expect((await page.goto('/templates'))?.status()).toBe(200);
    await page.evaluate(() => document.fonts.ready);
    await proveMutation(() => expectLibraryFan(page),
      () => applyStyleMutation(page.getByTestId(target), styles),
      'Library fan geometry.');
  });
}

mutationTest('style mutations restore without animating back', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 900 });
  expect((await page.goto('/templates'))?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  const slow = await page.addStyleTag({
    content: '[data-testid="gallery-fan"] { transition: right 5s linear; }',
  });
  try {
    await proveMutation(() => expectLibraryFan(page),
      () => applyStyleMutation(page.getByTestId('gallery-fan'),
        { right: '40px' }),
      'Library fan geometry.');
  } finally {
    await slow.evaluate((node) => node.remove());
  }
});

const MUTATION_MESSAGES = {
  'sheet alignment': 'Chrome sheet alignment.',
  'hero overhang': 'Chrome sheet alignment.',
  'fan rotation': 'Chrome fan geometry.',
  'title clipping': 'Screen containment.',
  'body color': 'Screen color.',
  'body font size': 'font-size',
  'navigation': 'Chrome navigation structure.',
} as const;

for (const [name, selector, styles] of [
  ['sheet alignment', '[data-testid="landing-sheet"]', { translate: '4px' }],
  ['hero overhang', '[data-testid="landing-art"]', {
    'margin-bottom': '0px',
  }],
  ['fan rotation', '[data-testid="landing-template-fan"] img:nth-child(2)', {
    transform: 'none',
  }],
  ['title clipping', '[data-testid="landing-title"]', {
    'height': '20px', 'overflow': 'hidden', 'white-space': 'nowrap',
  }],
  ['body color', 'body', { color: 'rgb(1, 2, 3)' }],
  ['body font size', 'body', { 'font-size': '17px' }],
  ['navigation', 'nav a[href="/templates"]', { display: 'none' }],
] as const) {
  mutationTest(`chrome assertions reject changed ${name}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    expect((await page.goto('/'))?.status()).toBe(200);
    await page.evaluate(() => document.fonts.ready);
    const assertClean = () => expectChrome(page, 'home', 'vi', 'light', 390);
    await assertClean();
    const restore = await applyStyleMutation(page.locator(selector), styles);
    await expect(assertClean()).rejects.toThrow(MUTATION_MESSAGES[name]);
    await restore();
    await assertClean();
  });
}

for (const path of ['/', '/templates'] as const) {
  for (const overflow of ['visible', 'hidden'] as const) {
    const desktopHeadingMutationTitle
      = `desktop heading rejects ${overflow} overflow on ${path}`;
    mutationTest(desktopHeadingMutationTitle, async ({
      page,
    }) => {
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto(path);
      await page.evaluate(() => document.fonts.ready);
      const assertClean = () => expectChrome(
        page, path === '/' ? 'home' : 'templates', 'vi', 'light', 1440,
      );
      await assertClean();
      const restore = await applyStyleMutation(page.locator('h1'), {
        height: '20px', overflow,
      });
      await expect(assertClean()).rejects.toThrow(overflow === 'visible'
        ? 'Chrome heading line box.'
        : 'Screen containment.');
      await restore();
      await assertClean();
    });
  }
}

const PHONE_WIDTHS = [360, 375, 390, 414, 430] as const;

async function expectCenteredSheet(page: Page, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  const boxes = await page.evaluate(() => {
    const box = (id: string) => {
      const element = document.querySelector(`[data-testid="${id}"]`);
      if (element === null) {
        throw new Error(`missing ${id}`);
      }
      const rect = element.getBoundingClientRect();
      return {
        left: rect.left,
        right: rect.right,
        width: rect.width,
        height: rect.height,
      };
    };
    return {
      art: box('landing-art'),
      sheet: box('landing-sheet'),
      overflow: document.documentElement.scrollWidth
        - document.documentElement.clientWidth,
    };
  });
  expect(boxes.overflow).toBe(0);
  expect(Math.abs(boxes.art.width - 210 * 96 / 25.4 * 0.36)).toBeLessThan(1);
  expect(Math.abs((boxes.sheet.left + boxes.sheet.right) / 2
    - (boxes.art.left + boxes.art.right) / 2)).toBeLessThan(1);
  expect(boxes.sheet.left).toBeGreaterThanOrEqual(0);
  expect(boxes.sheet.right).toBeLessThanOrEqual(width);
  const leftGap = boxes.sheet.left;
  const rightGap = width - boxes.sheet.right;
  expect(Math.abs(leftGap - rightGap)).toBeLessThan(2);
}

for (const engine of ['chromium', 'webkit'] as const) {
  test.describe(engine, () => {
    let browser: Browser;

    test.beforeAll(async () => {
      const launcher = engine === 'webkit' ? webkit : chromium;
      browser = await launcher.launch({ args: [] });
    });

    test.afterAll(async () => {
      await browser?.close();
    });

    for (const theme of ['light', 'dark'] as const) {
      for (const locale of ['vi', 'en'] as const) {
        for (const width of PHONE_WIDTHS) {
          const title = `hero sheet centered on phones: ${engine} ${theme} `
            + `${locale} ${width}px`;
          test(title, async ({ baseURL }) => {
            const origin = baseURL ?? 'http://127.0.0.1:20092';
            const context = await browser.newContext({
              baseURL: origin, locale: locale === 'vi' ? 'vi-VN' : 'en-US',
              timezoneId: 'UTC', deviceScaleFactor: 1, reducedMotion: 'reduce',
            });
            try {
              await context.addCookies([
                { name: 'aboutme-locale', value: locale, url: origin },
                { name: 'aboutme-theme', value: theme, url: origin },
              ]);
              const page = await context.newPage();
              await expectCenteredSheet(page, width);
            } finally {
              await context.close();
            }
          });
        }
      }
    }
  });
}

const AVATAR_ALT = {
  vi: 'Tranh vẽ chân dung Danny, bên cạnh là vợ và con anh trong khung '
    + 'hình trái tim',
  en: 'Drawn portrait of Danny, with his wife and child in a heart-shaped '
    + 'frame',
} as const;

const HOME_TITLE = {
  vi: 'Làm CV ở đây, gửi đi bằng một đường link.',
  en: 'Write your resume here. Send it as one link.',
} as const;

function bodyColor(theme: Theme): string {
  return theme === 'light' ? 'rgb(26, 22, 80)' : 'rgb(211, 202, 208)';
}

function chromeContract(
  name: ChromePage, locale: Locale, theme: Theme,
): ScreenContract {
  const vi = locale === 'vi';
  const title = name === 'home'
    ? HOME_TITLE[locale]
    : name === 'login'
      ? vi ? 'Đăng nhập' : 'Sign in'
      : vi ? 'Thư viện' : 'Library';
  const contract = chromeScreenContract('body',
    'main :is(h1,h2,p):visible:not(.sr-only):not(.resume-document *)',
    '[data-testid="app-shell"] nav a:visible', 'h1', title, theme,
    name === 'templates' ? 'img[data-page-image]:visible' : undefined);
  return name === 'home'
    ? {
        ...contract,
        type: ['body',
          '"Plus Jakarta Sans", "Be Vietnam Pro", system-ui, sans-serif',
          'Plus Jakarta Sans'],
        paint: ['body', 'color', bodyColor(theme)],
        faces: ['Be Vietnam Pro'],
      }
    : contract;
}

for (const entry of PAGES) {
  screenMutationTests(`chrome ${entry.name}`, async (page) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto(entry.path);
    await prepareScreen(page);
  }, chromeContract(entry.name, 'vi', 'light'));
}

async function openForgotPassword(
  page: Page, baseURL: string | undefined, locale: Locale, width: number,
): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  await page.context().addCookies([
    { name: 'aboutme-locale', value: locale, url: baseURL! },
  ]);
  const response = await page.goto('/forgot-password');
  expect(response?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
}

for (const locale of ['vi', 'en'] as const) {
  for (const width of [1024, 1440] as const) {
    test(`the auth artwork clears the points at ${width} px (${locale})`,
      async ({ page, baseURL }) => {
        await openForgotPassword(page, baseURL, locale, width);
        await expectAuthArtworkClear(page);
      });
  }
}

mutationTest('chrome rejects auth artwork over the points', async ({
  page, baseURL,
}) => {
  await openForgotPassword(page, baseURL, 'vi', 1024);
  await proveMutation(() => expectAuthArtworkClear(page),
    () => placeAuthArtworkOverPoints(page), 'Chrome auth artwork clearance.');
});

for (const kind of ['selection', 'scrollbar'] as const) {
  mutationTest(`chrome rejects a ${kind} that reaches the resume`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const response = await page.goto('/templates/creative-accent');
    expect(response?.status()).toBe(200);
    await proveMutation(() => expectRendererBoundary(page),
      () => leakChromeIntoResume(page, kind), 'Chrome renderer boundary.');
  });
}

for (const kind of ['shadow', 'panels'] as const) {
  mutationTest(`chrome rejects changed ${kind} values`, async ({ page }) => {
    await page.goto('/');
    await proveMutation(() => expectChromeDecoration(page, 'light', kind),
      () => applyStyleMutation(page.getByTestId(kind === 'shadow'
        ? 'landing-sheet'
        : 'landing-publish'), kind === 'shadow'
        ? { 'box-shadow': 'rgb(0, 0, 0) 1px 1px 1px 0px' }
        : { 'background-color': 'rgb(1, 2, 3)' }),
      kind === 'shadow' ? 'Chrome paper shadow.' : 'Chrome panel colors.');
  });
}

for (const [kind, styles] of [
  ['weight', { 'font-weight': '500' }],
  ['focus', { 'outline-style': 'solid' }],
] as const) {
  mutationTest(`chrome rejects changed sample ${kind}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto('/');
    await prepareScreen(page);
    const sample = page.getByTestId('landing-sample')
      .locator('.resume-document');
    await proveMutation(() => expectSampleIsolation(page),
      () => applyStyleMutation(kind === 'weight'
        ? sample
        : sample.locator('a[href]').first(), styles),
      'Chrome sample isolation.');
  });
}

for (const kind of ['navigation', 'template list'] as const) {
  mutationTest(`chrome rejects changed ${kind} semantics`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto('/');
    await prepareScreen(page);
    const assertion = () => kind === 'navigation'
      ? expectChromeNavigation(page, 'vi', 390, false)
      : expectChromeList(page);
    await proveMutation(assertion, () => mutateAttribute(
      kind === 'navigation'
        ? page.getByTestId('app-shell').locator('nav')
        : page.getByTestId('landing-template-list'), 'role', 'group',
    ), kind === 'navigation'
      ? 'Chrome navigation structure.'
      : 'Chrome template list structure.');
  });
}
