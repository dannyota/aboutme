import {
  type Browser,
  chromium,
  expect,
  type Page,
  test,
  webkit,
} from '@playwright/test';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';

import { captureReviewImage } from './review-images';
import {
  expectScreen,
  measureChromeGrid,
  measureChromeSheet,
  measureChromeThumbnail,
  measureTextContainment,
  measureVerticalFlow,
} from './display-assertions';
import {
  applyStyleMutation, mutateAttribute, proveMutation, screenMutationTests,
} from './style-mutation';
import {
  expectChromeDecoration, expectChromeList, expectChromeNavigation,
} from './chrome-details';

import {
  chromeScreenContract,
  prepareScreen,
} from './support';

// Gallery templates with a sample show a stored page image; the others
// mount a live thumbnail.
const LIVE_THUMBNAILS = TEMPLATES.length
  - new Set(SAMPLES.map(({ templateId }) => templateId)).size;

const PAGES = [
  { name: 'home', path: '/', thumbnails: 4 },
  { name: 'login', path: '/login', thumbnails: 0 },
  { name: 'templates', path: '/templates', thumbnails: LIVE_THUMBNAILS },
] as const;
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;

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
  await expect(header).toHaveCSS('background-color', theme === 'light'
    ? 'rgb(255, 255, 255)'
    : 'rgb(20, 26, 46)');
  await expect(page.locator('body')).toHaveCSS('background-color',
    theme === 'light' ? 'rgb(249, 248, 245)' : 'rgb(12, 16, 32)');
  await expect(page.locator('body'), 'Chrome body font-size.')
    .toHaveCSS('font-size', '15px');
  await expect(page.locator('body')).toHaveCSS('line-height', '22.5px');
  await expect(page.locator('body')).toHaveCSS('color', theme === 'light'
    ? 'rgb(16, 27, 63)'
    : 'rgb(243, 241, 236)');
  await expect(page.locator('body')).toHaveCSS(
    'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
  );
  expect(await page.evaluate(() => ({
    loaded: document.fonts.check('15px "Be Vietnam Pro"'),
    face: Array.from(document.fonts).some((font) =>
      font.family === 'Be Vietnam Pro' && font.status === 'loaded'),
    overflow: document.documentElement.scrollWidth
      <= document.documentElement.clientWidth + 1,
  }))).toEqual({ loaded: true, face: true, overflow: true });

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
  await expect(title).toHaveText(name === 'home'
    ? (vi
        ? 'CV của bạn. Chia sẻ theo cách của bạn.'
        : 'Your resume. Your link. Your control.')
    : (vi ? 'Thư viện' : 'Library'));
  await expect(title).toMatchAriaSnapshot(name === 'home'
    ? (vi
        ? '- heading "CV của bạn. Chia sẻ theo cách của bạn." [level=1]'
        : '- heading "Your resume. Your link. Your control." [level=1]')
    : (vi
        ? '- heading "Thư viện" [level=1]'
        : '- heading "Library" [level=1]'));
  await expect(title).toHaveCSS('font-weight', '700');
  await expect(title).toHaveCSS('font-size', name === 'home'
    ? (width < 1024 ? '48px' : '60px')
    : (width < 640 ? '40px' : '48px'));
  await expect.poll(() => title.evaluate((node) => {
    const css = getComputedStyle(node);
    const line = parseFloat(css.lineHeight) || parseFloat(css.fontSize);
    return node.getBoundingClientRect().height >= line - 1;
  }), 'Chrome heading line box.').toBe(true);
  const titleContainer = name === 'home'
    ? '[data-testid="landing-hero"]'
    : '[data-testid="gallery-header"]';
  await expect.poll(() => measureTextContainment(title, titleContainer),
    'Chrome text containment.')
    .toEqual({ visible: true, contained: true });

  if (name === 'home') {
    await expectChromeDecoration(page, theme);
    await expectChromeList(page);
    await expect.poll(() => measureChromeSheet(
      page.getByTestId('landing-stage'),
    ), 'Chrome sheet alignment.').toBe(true);
    await expect(page.getByTestId('landing-sheet'))
      .toHaveCSS('background-color', 'rgb(255, 255, 255)');
    await expect(page.getByTestId('landing-sheet'))
      .toHaveCSS('border-radius', '2px');
    await expect(page.getByTestId('landing-sheet'))
      .not.toHaveCSS('box-shadow', 'none');
    await expect(page.getByTestId('landing-glow'))
      .toHaveCSS('background-image', /radial-gradient/u);
    await expect(page.getByTestId('landing-templates').getByRole('heading', {
      level: 2,
    })).toHaveText(vi ? 'Chọn phong cách cho CV' : 'Choose your style');
    const grid = page.getByTestId('landing-template-grid');
    await expect(grid.locator(':scope > li')).toHaveCount(4);
    expect(await grid.locator('[data-template]').evaluateAll((cards) =>
      cards.map((card) => card.getAttribute('data-template')))).toEqual([
      'ats-plain', 'engineer-compact', 'graduate-friendly', 'executive-band',
    ]);
    await expect.poll(() => measureChromeGrid(grid), 'Chrome grid geometry.')
      .toEqual({
        columns: width < 1024 ? 2 : 4,
        gapX: width < 1024 ? 16 : 24,
        gapY: 32,
        separated: true,
      });
    const cards = grid.locator('[data-template]');
    for (let index = 0; index < 4; index += 1) {
      const card = cards.nth(index);
      await card.scrollIntoViewIfNeeded();
      await expect.poll(() => measureChromeThumbnail(card)).toBe(true);
    }
  } else {
    const galleryHeader = page.getByTestId('gallery-header');
    await expect(galleryHeader).toHaveCSS('background-color', theme === 'light'
      ? 'rgb(236, 238, 247)'
      : 'rgb(21, 29, 54)');
    await expect(galleryHeader).toHaveCSS('border-radius', '20px');
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
            await prepareScreen(browserPage);
            await expect(browserPage.locator('[data-sheet-thumbnail]'))
              .toHaveCount(page.thumbnails);
            // Each embedded sheet's name renders as a p, not an h1, so the
            // page keeps exactly one h1 once every thumbnail has mounted.
            await expect(browserPage.locator('h1')).toHaveCount(1);
          }

          if (page.thumbnails > 0) {
            await expect(browserPage.locator('[data-sheet-thumbnail-render]'))
              .toHaveCount(page.thumbnails);
            await expect(browserPage.locator('h1')).toHaveCount(1);
          }
          if (page.name === 'home') {
            const templates = browserPage.locator(
              '[data-testid="landing-templates"]',
            );
            await templates.scrollIntoViewIfNeeded();
            await expect(browserPage.locator('[data-sheet-thumbnail-render]'))
              .toHaveCount(4);
            await prepareScreen(browserPage);
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

for (const [name, selector, styles] of [
  ['sheet alignment', '[data-testid="landing-sheet"]', { translate: '4px' }],
  ['columns', '[data-testid="landing-template-grid"]', {
    'grid-template-columns': ['1fr', /^\d+(?:\.\d+)?px$/u],
  }],
  ['card overlap', '[data-testid="landing-template-grid"] > :nth-child(2)', {
    transform: ['translateX(-20px)', 'matrix(1, 0, 0, 1, -20, 0)'],
  }],
  ['row gap', '[data-testid="landing-template-grid"] > :nth-child(n+3)', {
    transform: ['translateY(4px)', 'matrix(1, 0, 0, 1, 0, 4)'],
  }],
  ['title clipping', '[data-testid="landing-title"]', {
    'height': '20px', 'overflow': 'hidden', 'white-space': 'nowrap',
  }],
  ['body color', 'body', { color: 'rgb(1, 2, 3)' }],
  ['body font size', 'body', { 'font-size': '17px' }],
  ['navigation', 'nav a[href="/templates"]', { display: 'none' }],
] as const) {
  test(`chrome assertions reject changed ${name}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    expect((await page.goto('/'))?.status()).toBe(200);
    await page.evaluate(() => document.fonts.ready);
    const assertClean = () => expectChrome(page, 'home', 'vi', 'light', 390);
    await assertClean();
    const restore = await applyStyleMutation(page.locator(selector), styles);
    await expect(assertClean()).rejects.toThrow(
      name === 'sheet alignment'
        ? 'Chrome sheet alignment.'
        : ['columns', 'card overlap', 'row gap'].includes(name)
            ? 'Chrome grid geometry.'
            : name === 'title clipping'
              ? 'Screen containment.'
              : name === 'body color'
                ? 'Screen color.'
                : name === 'navigation'
                  ? 'Chrome navigation structure.'
                  : 'font-size');
    await restore();
    await assertClean();
  });
}

for (const path of ['/', '/templates'] as const) {
  for (const overflow of ['visible', 'hidden'] as const) {
    test(`desktop heading rejects ${overflow} overflow on ${path}`, async ({
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

// The hero sheet and its ghost stay the same size and centered at phone
// widths in both engines. iPhone Safari once laid the zoomed sheet out
// narrower than its stage, leaving the ghost wider and the pair off center
// (DESIGN.md, landing).
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
      stage: box('landing-stage'),
      sheet: box('landing-sheet'),
      overflow: document.documentElement.scrollWidth
        - document.documentElement.clientWidth,
    };
  });
  expect(boxes.overflow).toBe(0);
  expect(Math.abs(boxes.sheet.width - boxes.stage.width)).toBeLessThan(1);
  expect(Math.abs(boxes.sheet.height - boxes.stage.height)).toBeLessThan(1);
  expect(Math.abs(boxes.sheet.left - boxes.stage.left)).toBeLessThan(1);
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

function chromeContract(name: ChromePage, locale: Locale, theme: Theme) {
  const vi = locale === 'vi';
  const title = name === 'home'
    ? vi
      ? 'CV của bạn. Chia sẻ theo cách của bạn.'
      : 'Your resume. Your link. Your control.'
    : name === 'login'
      ? vi ? 'Đăng nhập' : 'Sign in'
      : vi ? 'Thư viện' : 'Library';
  return chromeScreenContract('body',
    'main :is(h1,h2,p):visible:not(.sr-only):not(.resume-document *)',
    '[data-testid="app-shell"] nav a:visible', 'h1', title, theme,
    name === 'templates' ? 'img[data-page-image]:visible' : undefined);
}

for (const entry of PAGES) {
  screenMutationTests(`chrome ${entry.name}`, async (page) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto(entry.path);
    await prepareScreen(page);
  }, chromeContract(entry.name, 'vi', 'light'));
}

for (const kind of ['shadow', 'gradient'] as const) {
  test(`chrome rejects changed ${kind} values`, async ({ page }) => {
    await page.goto('/');
    await proveMutation(() => expectChromeDecoration(page, 'light', kind),
      () => applyStyleMutation(page.getByTestId(kind === 'shadow'
        ? 'landing-sheet'
        : 'landing-glow'), kind === 'shadow'
        ? { 'box-shadow': 'rgb(0, 0, 0) 1px 1px 1px 0px' }
        : { 'background-image':
            'radial-gradient(rgb(255, 0, 0), rgb(0, 0, 255))' }),
      kind === 'shadow' ? 'Chrome paper shadow.' : 'Chrome hero gradient.');
  });
}

for (const kind of ['navigation', 'template list'] as const) {
  test(`chrome rejects changed ${kind} semantics`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto('/');
    await prepareScreen(page);
    const assertion = () => kind === 'navigation'
      ? expectChromeNavigation(page, 'vi', 390, false)
      : expectChromeList(page);
    await proveMutation(assertion, () => mutateAttribute(
      kind === 'navigation'
        ? page.getByTestId('app-shell').locator('nav')
        : page.getByTestId('landing-template-grid'), 'role', 'group',
    ), kind === 'navigation'
      ? 'Chrome navigation structure.'
      : 'Chrome template list structure.');
  });
}
