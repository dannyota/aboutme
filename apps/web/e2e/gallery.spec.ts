import { expect, test } from '@playwright/test';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';
import { SAMPLE_PAGES } from '../app/templates/samplePages';
import {
  measureGalleryDetail,
} from './display-assertions';
import {
  applyStyleMutation, mutationTest, screenMutationTests,
} from './style-mutation';
import { galleryDetailProofs } from './gallery-detail-proofs';
import {
  assertGalleryDetail,
  galleryDetailContract,
  waitForImages,
} from './support';
// The template gallery and template pages in the production build: no
// horizontal page scroll, wrapping desktop rows and a phone filter sheet,
// intersecting filters, native Style links, template page tabs, and
// an unknown template is a 404 (DESIGN.md, template gallery).
// Templates with a sample show its stored page image; the rest render live.
const SAMPLE_TEMPLATES = new Set(SAMPLES.map(({ templateId }) => templateId));
for (const width of [390, 1440]) {
  test(`gallery at ${width} px fits the viewport`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([
      {
        name: 'aboutme-locale',
        value: 'vi',
        url: 'http://127.0.0.1:20092',
      },
    ]);
    const response = await page.goto('/templates');
    expect(response?.status()).toBe(200);
    await expect(page.locator('[data-template]')).toHaveCount(20);
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth,
    );
    expect(overflow).toBe(0);
    const trigger = page.locator('[data-action="gallery-filters-open"]');
    if (width === 390) {
      await expect(trigger).toBeVisible();
      await expect(page.getByTestId('gallery-roles')).toBeHidden();
      await trigger.click();
    } else {
      await expect(trigger).toBeHidden();
    }
    const fields
      = width === 390
        ? page.getByTestId('gallery-filter-sheet')
        : page.locator('.gallery-desktop');
    await expect(fields.getByTestId('gallery-styles')).toBeVisible();
    expect(
      await fields
        .getByTestId('gallery-styles')
        .evaluate((node) => node.scrollWidth > node.clientWidth),
    ).toBe(false);
    const ats = fields.locator('[data-filter="ats"]');
    await expect(ats).toHaveAttribute('href', '/templates?filter=ats');
    await ats.focus();
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/filter=ats$/u);
    await expect(ats).toHaveAttribute('aria-current', 'page');
    await expect(ats).toBeFocused();
    if (width === 390) {
      await expect(fields).toBeVisible();
      await expect(fields.getByTestId('gallery-sheet-status')).toHaveText(
        '7 mẫu',
      );
      await fields.locator('[data-action="gallery-filters-apply"]').click();
      await expect(trigger).toBeFocused();
    }
    await expect(page.locator('[data-template]:visible')).toHaveCount(7);
  });
  test(`gallery at ${width} px filters by role and style`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([
      {
        name: 'aboutme-locale',
        value: 'en',
        url: 'http://127.0.0.1:20092',
      },
    ]);
    await page.goto('/templates?role=backend');
    const trigger = page.locator('[data-action="gallery-filters-open"]');
    if (width === 390) {
      await trigger.click();
    }
    const fields
      = width === 390
        ? page.getByTestId('gallery-filter-sheet')
        : page.locator('.gallery-desktop');
    const backend = fields.locator('[data-role="backend"]');
    await expect(backend).toHaveAttribute('aria-pressed', 'true');
    await backend.focus();
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/role=frontend$/u);
    const frontend = fields.locator('[data-role="frontend"]');
    await expect(frontend).toHaveAttribute('aria-pressed', 'true');
    await frontend.click();
    await expect(page).toHaveURL(/role=frontend$/u);
    const technical = fields.locator('[data-filter="technical"]');
    await expect(technical).toHaveAttribute(
      'href',
      '/templates?role=frontend&filter=technical',
    );
    const [tab] = await Promise.all([
      page.context().waitForEvent('page'),
      technical.click({ button: 'middle' }),
    ]);
    await tab.waitForLoadState();
    await expect(tab).toHaveURL(/role=frontend&filter=technical$/u);
    await expect(page).toHaveURL(/role=frontend$/u);
    await tab.close();
    await technical.click();
    await expect(page).toHaveURL(/role=frontend&filter=technical$/u);
    if (width === 390) {
      await expect(
        fields.locator('[data-action="gallery-filters-apply"]'),
      ).toHaveText('Show 1 template');
    } else {
      await expect(page.locator('[data-template]:visible')).toHaveAttribute(
        'data-template',
        'engineer-compact',
      );
    }
    await fields.locator('[data-filter="ats"]').click();
    if (width === 390) {
      await expect(fields.getByTestId('gallery-sheet-status')).toHaveText(
        '0 templates',
      );
      await fields.locator('[data-action="gallery-filters-apply"]').click();
    }
    await expect(page.getByTestId('gallery-count')).toHaveText('0 templates');
    await expect(
      page.getByText('No template matches this filter.'),
    ).toBeVisible();
    await expect(page.getByText('No template matches this filter.'))
      .toHaveCSS('font-size', '15px');
    if (width === 390) {
      await page.locator('[data-action="gallery-filters-clear-all"]').click();
      await expect(trigger).toBeFocused();
    } else {
      await fields.locator('[data-role="all"]').click();
      await fields.locator('[data-filter="all"]').click();
    }
    await expect(page).toHaveURL(/\/templates$/u);
    await expect(page.locator('[data-template]:visible')).toHaveCount(20);
  });
  test(`gallery at ${width} px shows the stored sample pages`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const response = await page.goto('/templates');
    expect(response?.status()).toBe(200);
    const images = page.locator('img[data-page-image]');
    await expect(images).toHaveCount(SAMPLE_TEMPLATES.size);
    const count = await images.count();
    for (let index = 0; index < count; index += 1) {
      const image = images.nth(index);
      await expect(image).toHaveAttribute('alt', /.+/);
      await expect(image).toHaveAttribute(
        'loading',
        index < 2 ? 'eager' : 'lazy',
      );
      await expect(image).toHaveAttribute('width', /^\d+$/);
      await expect(image).toHaveAttribute('height', /^\d+$/);
      await image.scrollIntoViewIfNeeded();
      await expect.poll(() => image.evaluate((element) =>
        (element as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    }
    await expect(page.locator('[data-sheet-thumbnail]'))
      .toHaveCount(TEMPLATES.length - SAMPLE_TEMPLATES.size);
    await expect(page.locator('h1')).toHaveCount(1);
  });
}
test('template page switches between the page and the ATS text', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await expect(page.locator('.template-detail__info h1'))
    .toHaveText('Engineer Compact');
  // The embedded sample's name renders as a p, not an h1, so the page
  // keeps exactly one h1: the template name.
  await expect(page.locator('h1')).toHaveCount(1);
  await expect(page.locator('.resume-document')).toHaveCount(1);
  const tabs = page.getByRole('tab');
  await expect(tabs).toHaveText(['Page', 'PDF', 'What an ATS reads']);
  await page.getByRole('tab', { name: 'What an ATS reads' }).click();
  await expect(page.locator('[data-ats-text]')).toContainText('Đỗ Hoàng Nam');
  await expect(page.locator('[data-action="use-sample"]'))
    .toHaveAttribute('href', '/app/new?sample=engineer-compact&lng=en');
  await expect(page.locator('h1')).toHaveCount(1);
});
for (const template of [
  { id: 'creative-accent', columns: 1, marginMm: 22 },
  { id: 'engineer-compact', columns: 2, marginMm: 11 },
] as const) {
  for (const width of [390, 1440]) {
    test(`${template.id} Page at ${width}px uses its reading width`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const response = await page.goto(`/templates/${template.id}`);
      expect(response?.status()).toBe(200);
      const sheet = page.locator('.template-paper[data-web-columns]');
      await expect(sheet).toHaveAttribute(
        'data-web-columns',
        String(template.columns),
      );
      const geometry = await sheet.locator('.resume-document')
        .evaluate((article) => {
          const style = getComputedStyle(article);
          const box = article.getBoundingClientRect();
          return {
            fontSize: Number.parseFloat(style.fontSize),
            left: Number.parseFloat(style.paddingLeft),
            right: Number.parseFloat(style.paddingRight),
            width: box.width,
          };
        });
      const sheetWidth = await sheet.evaluate((element) =>
        element.getBoundingClientRect().width);
      const measure = (template.columns === 1 ? 52 : 70)
        * geometry.fontSize;
      const templateMargin = template.marginMm * 96 / 25.4;
      const expected = Math.min(
        templateMargin,
        Math.max(16, (sheetWidth - measure) / 2),
      );
      expect(geometry.left).toBeCloseTo(expected, 0);
      expect(geometry.right).toBeCloseTo(expected, 0);
      expect(geometry.width).toBeCloseTo(sheetWidth, 0);
      if (width === 390) {
        expect(geometry.left).toBe(16);
        expect(geometry.width - geometry.left - geometry.right)
          .toBeGreaterThanOrEqual(sheetWidth - 32.5);
      }
      const overflow = await page.evaluate(() =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth);
      expect(overflow).toBe(0);
    });
  }
}
// The executive band sample's PDF tab, whose page count the gallery pins.
const EXECUTIVE_PAGES = SAMPLE_PAGES['executive-band']!;
test('the executive band PDF tab shows every stored page', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates/executive-band');
  expect(response?.status()).toBe(200);
  await page.getByRole('tab', { name: 'PDF' }).click();
  await expect(page.locator('.template-paper[data-web-columns]'))
    .toHaveCount(0);
  const images = page.locator('img[data-pdf-page]');
  await expect(images).toHaveCount(EXECUTIVE_PAGES);
  for (let index = 0; index < EXECUTIVE_PAGES; index += 1) {
    const image = images.nth(index);
    await expect(image).toHaveAttribute('alt', /.+/);
    await image.scrollIntoViewIfNeeded();
    await expect.poll(() => image.evaluate((element) =>
      (element as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    const geometry = await image.evaluate((element) => {
      const sheet = element.closest('.template-paper');
      if (sheet === null) {
        throw new Error('PDF page sheet is missing.');
      }
      return {
        image: element.getBoundingClientRect().width,
        sheet: sheet.getBoundingClientRect().width,
      };
    });
    expect(geometry.image).toBeCloseTo(geometry.sheet, 0);
  }
  await expect(page.locator('figcaption')).toHaveText(
    Array.from(
      { length: EXECUTIVE_PAGES },
      (_, index) => `Page ${index + 1} of ${EXECUTIVE_PAGES}`,
    ),
  );
  await expect(page.locator('h1')).toHaveCount(1);
});
test('the engineer compact page renders server-side with one h1', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.request.get('/templates/engineer-compact');
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(html).toContain('Đỗ Hoàng Nam');
  expect(html.match(/<h1/g)).toHaveLength(1);
});
test('the ATS tab keeps dark ink on a white sheet in dark theme', async ({
  page,
}) => {
  await page.context().addCookies([
    { name: 'aboutme-locale', value: 'en', url: 'http://127.0.0.1:20092' },
    { name: 'aboutme-theme', value: 'dark', url: 'http://127.0.0.1:20092' },
  ]);
  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await page.getByRole('tab', { name: 'What an ATS reads' }).click();
  const panel = page.locator('[data-ats-text]');
  await expect(panel).toBeVisible();
  const colors = await panel.evaluate((element) => {
    const style = getComputedStyle(element);
    return { color: style.color, background: style.backgroundColor };
  });
  expect(colors.color).toBe('rgb(23, 26, 24)');
  expect(colors.background).toBe('rgb(255, 255, 255)');
});
test('an unknown template is not found', async ({ page }) => {
  const response = await page.goto('/templates/not-a-template');
  expect(response?.status()).toBe(404);
});
test('the role-filtered gallery still lists every template server-side',
  async ({ page }) => {
    const response = await page.request.get('/templates?role=backend');
    expect(response.status()).toBe(200);
    const html = await response.text();
    for (const template of TEMPLATES) {
      expect(html).toContain(`href="/templates/${template.id}"`);
    }
  });
// PDF and ATS tab content and layout at phone and desktop widths.
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;
test('creative accent Page at 390px keeps content and layout', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.context().addCookies([
    { name: 'aboutme-locale', value: 'vi', url: 'http://127.0.0.1:20092' },
    { name: 'aboutme-theme', value: 'light', url: 'http://127.0.0.1:20092' },
  ]);
  const response = await page.goto('/templates/creative-accent');
  expect(response?.status()).toBe(200);
  await expect(page.locator('.resume-document')).toContainText(
    'Đinh Công Khánh',
  );
  await page.evaluate(() => document.fonts.ready);
  await waitForImages(page);
  const overflow = await page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
  await assertGalleryDetail(page, 'vi', 390, 'page');
});
for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`template PDF tab ${theme} ${width}px has ordered pages`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.context().addCookies([
        { name: 'aboutme-locale', value: 'vi', url: 'http://127.0.0.1:20092' },
        { name: 'aboutme-theme', value: theme, url: 'http://127.0.0.1:20092' },
      ]);
      const response = await page.goto('/templates/executive-band');
      expect(response?.status()).toBe(200);
      await page.addStyleTag({
        content: '*, *::before, *::after { transition: none !important; '
          + 'animation: none !important; }',
      });
      await page.getByRole('tab', { name: 'PDF' }).click();
      const pages = page.locator('img[data-pdf-page]');
      await expect(pages).toHaveCount(EXECUTIVE_PAGES);
      // Later pages load lazily; bring each page into view until it has
      // decoded, so the full-page capture never shows an empty sheet.
      for (let index = 0; index < EXECUTIVE_PAGES; index += 1) {
        const image = pages.nth(index);
        await image.scrollIntoViewIfNeeded();
        await expect.poll(() => image.evaluate((element) =>
          (element as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
      }
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);
      const overflow = await page.evaluate(() =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth);
      expect(overflow).toBe(0);
      await assertGalleryDetail(page, 'vi', width, 'pdf', theme);
    });
  }
}
for (const width of WIDTHS) {
  test(`template ATS tab dark ${width}px keeps content and layout`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([
      { name: 'aboutme-locale', value: 'vi', url: 'http://127.0.0.1:20092' },
      { name: 'aboutme-theme', value: 'dark', url: 'http://127.0.0.1:20092' },
    ]);
    const response = await page.goto('/templates/executive-band');
    expect(response?.status()).toBe(200);
    await page.addStyleTag({
      content: '*, *::before, *::after { transition: none !important; '
        + 'animation: none !important; }',
    });
    await page.getByRole('tab', { name: 'ATS đọc được gì' }).click();
    await expect(page.locator('[data-ats-text]')).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    await waitForImages(page);
    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
      - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
    await assertGalleryDetail(page, 'vi', width, 'ats', 'dark');
  });
}
for (const locale of ['vi', 'en']) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const width of [390, 1440]) {
      test(`Library hydrates without mismatches in ${locale} at ${width}`,
        async ({ page }) => {
          await page.setViewportSize({ width, height: 900 });
          await page.context().addCookies([
            {
              name: 'aboutme-locale',
              value: locale,
              url: 'http://127.0.0.1:20092',
            },
          ]);
          const mismatches: string[] = [];
          page.on('console', (message) => {
            if (/hydration.*mismatch/iu.test(message.text())) {
              mismatches.push(message.text());
            }
          });
          for (const query of ['', '?role=backend&filter=technical']) {
            await page.goto(`/templates${query}`);
            await expect(
              page.locator('[data-action="gallery-filters-open"]'),
            ).toBeEnabled();
            const fallback = page.locator('noscript');
            await expect(fallback).toHaveCount(1);
            expect(await fallback.evaluate((element) => ({
              elements: element.childElementCount,
              markup: element.textContent?.includes('gallery-fallback'),
            }))).toEqual({ elements: 0, markup: true });
            await expect(page.locator('[data-template]')).toHaveCount(20);
            expect(mismatches).toEqual([]);
          }
        });
    }
  });
}
for (const width of [390, 1440]) {
  test(`Style filters and Clear all work without scripts at ${width}`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      javaScriptEnabled: false,
      viewport: { width, height: 900 },
    });
    await context.addCookies([
      { name: 'aboutme-locale', value: 'en', url: 'http://127.0.0.1:20092' },
    ]);
    const page = await context.newPage();
    await page.goto(
      'http://127.0.0.1:20092/templates?role=backend&source=a%20%26%20b',
    );
    await expect(page.locator('[data-template]')).toHaveCount(20);
    await expect(page.locator('[data-template]:visible')).toHaveCount(1);
    await expect(
      page.locator('.gallery-desktop [data-role="backend"]'),
    ).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByTestId('gallery-filters-badge')).toHaveText('1');
    await expect(page.getByTestId('gallery-active-filters')).toContainText(
      'Backend',
    );
    await expect(page.getByTestId('gallery-filter-sheet')).toHaveCount(0);
    const styles
      = width < 1024
        ? page.locator('.gallery-fallback')
        : page.locator('.gallery-desktop');
    if (width < 1024) {
      await expect(page.locator('.gallery-fallback')).toBeVisible();
    } else {
      await expect(page.locator('.gallery-fallback')).toBeHidden();
    }
    await expect(styles.locator('[data-filter="ats"]')).toHaveAttribute(
      'href',
      /^\/templates\?source=a(%20|\+)%26(%20|\+)b&role=backend&filter=ats$/u,
    );
    await styles.locator('[data-filter="ats"]').click();
    await expect(page).toHaveURL(/role=backend&filter=ats$/u);
    await expect(page.locator('[data-template]:visible')).toHaveCount(0);
    const clear = page.getByRole('link', { name: 'Clear all', exact: true });
    await expect(clear).toBeVisible();
    await expect(clear).toHaveAttribute(
      'href', /^\/templates\?source=a(?:%20|\+)%26(?:%20|\+)b$/u,
    );
    await clear.click();
    await expect(page).toHaveURL(
      /\/templates\?source=a(?:%20|\+)%26(?:%20|\+)b$/u,
    );
    await expect(clear).toHaveCount(0);
    await expect(page.locator('[data-template]:visible')).toHaveCount(20);
    await expect(page.locator('[data-role="all"]')).toBeDisabled();
    await context.close();
  });
}
for (const width of [390, 768]) {
  test(`sticky focus and route exit at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/templates');
    const cards = page.locator('[data-template]');
    await cards.nth(8).focus();
    await page.keyboard.press('Tab');
    await page.keyboard.press('Shift+Tab');
    const top = await cards
      .nth(8)
      .evaluate((node) => node.getBoundingClientRect().top);
    expect(top).toBeGreaterThanOrEqual(68);
    expect(
      await page
        .locator('html')
        .evaluate((node) => getComputedStyle(node).scrollPaddingTop),
    ).toBe('68px');
    await cards.nth(8).click();
    await expect(page.locator('html')).not.toHaveClass(
      /filter-bar-scroll-padding/u,
    );
  });
}
for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const theme of THEMES) {
      for (const width of WIDTHS) {
        for (const tab of ['page', 'pdf', 'ats'] as const) {
          for (const viewer of THEMES) {
            test(`template detail ${tab} ${locale} ${theme} ${viewer} ${width}`,
              async ({ page }) => {
                await page.setViewportSize({ width, height: 900 });
                await page.emulateMedia({
                  colorScheme: viewer, reducedMotion: 'reduce',
                });
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
                const id = tab === 'page'
                  ? 'creative-accent'
                  : 'executive-band';
                await page.goto(`/templates/${id}`);
                if (tab !== 'page') {
                  const name = tab === 'pdf'
                    ? 'PDF'
                    : locale === 'vi' ? 'ATS đọc được gì' : 'What an ATS reads';
                  await page.getByRole('tab', { name }).click();
                }
                await page.evaluate(() => document.fonts.ready);
                await assertGalleryDetail(page, locale, width, tab, theme);
              });
          }
        }
      }
    }
  });
}
for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const theme of THEMES) {
      test(`template detail changes layout at 900px ${locale} ${theme}`,
        async ({ page }) => {
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
          await page.goto('/templates/creative-accent');
          for (const width of [899, 900]) {
            await page.setViewportSize({ width, height: 900 });
            await expect.poll(() =>
              measureGalleryDetail(page.locator('.template-detail__layout')),
            ).toEqual({
              stacked: width < 900, columns: width >= 900, contained: true,
            });
          }
        });
    }
  });
}
for (const [name, selector, styles] of [
  ['displacement', '.template-detail__info', {
    transform: ['translateX(4px)', 'matrix(1, 0, 0, 1, 4, 0)'],
  }],
  ['clipped ATS text', '[data-ats-text]', {
    height: ['24px', /^\d+(?:\.\d+)?px$/u], overflow: 'hidden',
  }],
] as const) {
  mutationTest(`template detail assertions reject ${name}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/templates/executive-band');
    await page.getByRole('tab', { name: 'ATS đọc được gì' }).click();
    await page.evaluate(() => document.fonts.ready);
    const assertClean = () => assertGalleryDetail(page, 'vi', 1440, 'ats');
    await assertClean();
    const restore = await applyStyleMutation(page.locator(selector), styles);
    await expect(assertClean()).rejects.toThrow(name === 'displacement'
      ? 'Gallery detail geometry.'
      : 'Screen containment.');
    await restore();
    await assertClean();
  });
}

for (const tab of ['page', 'pdf', 'ats'] as const) {
  screenMutationTests(`template ${tab}`, async (page) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto(`/templates/${tab === 'page'
      ? 'creative-accent'
      : 'executive-band'}`);
    if (tab !== 'page') {
      await page.getByRole('tab', {
        name: tab === 'pdf' ? 'PDF' : 'ATS đọc được gì', exact: true,
      }).click();
    }
  }, galleryDetailContract('vi', 'light', tab));
}

galleryDetailProofs();
