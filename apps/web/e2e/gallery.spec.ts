import { expect, test, type Page } from '@playwright/test';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';

import { SAMPLE_PAGES } from '../app/templates/samplePages';
import {
  galleryFontLoaded,
  measureGalleryDetail,
  measureGalleryGrid,
  measureTextContainment,
} from './display-assertions';
import {
  CHROME_PIXEL_TOLERANCE,
  verifyScreenshot,
  waitForImages,
} from './support';

// The template gallery and template pages in the production build: no
// horizontal page scroll, wrapping desktop rows and a phone filter sheet,
// intersecting filters, native Style links, template page tabs, and
// an unknown template is a 404 (DESIGN.md, template gallery).

// Templates with a sample show its stored page image; the rest render live.
const SAMPLE_TEMPLATES = new Set(SAMPLES.map(({ templateId }) => templateId));

for (const theme of ['light', 'dark'] as const) {
  for (const width of [390, 1440]) {
    test(`Role chips have their size and selected colors in ${theme} ${width}`,
      async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.context().addCookies([
          {
            name: 'aboutme-theme',
            value: theme,
            url: 'http://127.0.0.1:20092',
          },
        ]);
        await page.goto('/templates?role=backend');
        if (width === 390) {
          await page.locator('[data-action="gallery-filters-open"]').click();
        }
        const fields = width === 390
          ? page.getByTestId('gallery-filter-sheet')
          : page.locator('.gallery-desktop');
        const roles = fields.locator('[data-role]');
        await expect(roles).toHaveCount(10);
        const sizes = await roles.evaluateAll((nodes) => nodes.map((node) => {
          const style = getComputedStyle(node);
          return {
            height: style.height,
            paddingLeft: style.paddingLeft,
            paddingRight: style.paddingRight,
            borderRadius: style.borderRadius,
            fontSize: style.fontSize,
          };
        }));
        for (const size of sizes) {
          expect(size).toEqual({
            height: width === 390 ? '40px' : '36px',
            paddingLeft: width === 390 ? '14px' : '16px',
            paddingRight: width === 390 ? '14px' : '16px',
            borderRadius: '9999px',
            fontSize: '14px',
          });
        }
        const colors = await page.evaluate(() => {
          const probe = document.createElement('span');
          probe.style.backgroundColor = 'var(--primary)';
          probe.style.color = 'var(--primary-foreground)';
          document.body.append(probe);
          const style = getComputedStyle(probe);
          const result = {
            background: style.backgroundColor,
            text: style.color,
          };
          probe.remove();
          return result;
        });
        for (const role of ['backend', 'frontend']) {
          const selected = fields.locator(`[data-role="${role}"]`);
          if (role === 'frontend') await selected.click();
          await expect(selected).toHaveAttribute('aria-pressed', 'true');
          await expect(selected).toHaveCSS(
            'background-color', colors.background,
          );
          await expect(selected).toHaveCSS(
            'border-top-color', colors.background,
          );
          await expect(selected).toHaveCSS('color', colors.text);
          await expect(selected).toHaveCSS(
            'font-weight', width === 390 ? '600' : '500',
          );
        }
        if (width === 1440) {
          const divider = fields.locator('.library-divider');
          await expect(divider).toHaveCSS('margin-left', '4px');
          await expect(divider).toHaveCSS('margin-right', '4px');
        }
      });
  }
}

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
    if (width === 390) await trigger.click();
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
      await expect(async () => {
        const naturalWidth = await image.evaluate((element) =>
          (element as HTMLImageElement).naturalWidth);
        expect(naturalWidth).toBeGreaterThan(0);
      }).toPass();
    }

    await expect(page.locator('[data-sheet-thumbnail]'))
      .toHaveCount(TEMPLATES.length - SAMPLE_TEMPLATES.size);
    await expect(page.locator('h1')).toHaveCount(1);
  });
}

test('gallery header at 360 px fits the viewport in English', async ({
  page,
}) => {
  // The narrowest supported phone width; the English header labels are the
  // longer ones, so English is the tighter fit.
  await page.setViewportSize({ width: 360, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates');
  expect(response?.status()).toBe(200);
  const overflow = await page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
  const header = await page.locator('[data-testid="app-shell"]').boundingBox();
  expect(header).not.toBeNull();
  if (header !== null) expect(header.x + header.width).toBeLessThanOrEqual(360);
});

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
    await expect(async () => {
      const naturalWidth = await image.evaluate((element) =>
        (element as HTMLImageElement).naturalWidth);
      expect(naturalWidth).toBeGreaterThan(0);
    }).toPass();
    const geometry = await image.evaluate((element) => {
      const sheet = element.closest('.template-paper');
      if (sheet === null) throw new Error('PDF page sheet is missing.');
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

// Pixel baselines for the executive band PDF tab (two stored pages) and the
// ATS tab in dark theme (DESIGN.md, Library), at phone and desktop widths.
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [390, 1440] as const;

test('creative accent Page at 390px matches baseline', async ({
  page,
}, testInfo) => {
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
  await verifyScreenshot(
    page,
    'template-page--creative-accent--light--390.png',
    testInfo,
    undefined,
    CHROME_PIXEL_TOLERANCE,
  );
});

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`template PDF tab ${theme} ${width}px matches baseline`, async ({
      page,
    }, testInfo) => {
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
        await expect(async () => {
          const naturalWidth = await image.evaluate((element) =>
            (element as HTMLImageElement).naturalWidth);
          expect(naturalWidth).toBeGreaterThan(0);
        }).toPass();
      }
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);

      const overflow = await page.evaluate(() =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth);
      expect(overflow).toBe(0);

      await assertGalleryDetail(page, 'vi', width, 'pdf');
      await verifyScreenshot(
        page,
        `template-pdf--${theme}--${width}.png`,
        testInfo,
        undefined,
        CHROME_PIXEL_TOLERANCE,
      );
    });
  }
}

for (const width of WIDTHS) {
  test(`template ATS tab dark ${width}px matches baseline`, async ({
    page,
  }, testInfo) => {
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

    await assertGalleryDetail(page, 'vi', width, 'ats');
    await verifyScreenshot(
      page,
      `template-ats--dark--${width}.png`,
      testInfo,
      undefined,
      CHROME_PIXEL_TOLERANCE,
    );
  });
}
for (const locale of ['vi', 'en'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    test(`Library layouts fit in ${locale} ${theme}`, async ({ page }) => {
      await page.context().addCookies([
        {
          name: 'aboutme-locale',
          value: locale,
          url: 'http://127.0.0.1:20092',
        },
        { name: 'aboutme-theme', value: theme, url: 'http://127.0.0.1:20092' },
      ]);
      await page.goto('/templates?role=fresher&filter=first-job');
      for (const width of [
        360, 390, 640, 641, 768, 899, 900, 1023, 1024, 1179, 1180, 1440,
      ]) {
        await page.setViewportSize({ width, height: 900 });
        expect(
          await page.evaluate(
            () =>
              document.documentElement.scrollWidth
              - document.documentElement.clientWidth,
          ),
        ).toBe(0);
        const trigger = page.locator('[data-action="gallery-filters-open"]');
        if (width < 1024) {
          await expect(trigger).toBeVisible();
          await expect(
            page.getByTestId('gallery-active-filters'),
          ).toBeVisible();
          await expect(page.locator('.gallery-desktop')).toBeHidden();
          await trigger.click();
          const sheet = page.getByTestId('gallery-filter-sheet');
          await expect(sheet).toBeVisible();
          expect(
            await sheet.evaluate((node) => node.scrollWidth > node.clientWidth),
          ).toBe(false);
          await sheet.locator('[data-action="gallery-filters-close"]').click();
        } else {
          await expect(trigger).toBeHidden();
          await expect(page.getByTestId('gallery-active-filters')).toBeHidden();
          await expect(page.locator('.gallery-desktop')).toBeVisible();
        }
        const expected
          = width < 641 ? 2 : width < 900 ? 3 : width < 1180 ? 4 : 5;
        expect(
          await page
            .locator('.gallery-grid')
            .evaluate(
              (node) =>
                getComputedStyle(node).gridTemplateColumns.split(' ').length,
            ),
        ).toBe(expected);
        await expect.poll(() =>
          measureGalleryGrid(page.locator('.gallery-grid')),
        ).toEqual({
          columns: expected,
          columnGap: width < 641 ? 14 : 28,
          rowGap: width < 641 ? 24 : 40,
          ordered: true,
        });
      }
    });
  }
}

test('sheet traps links and resize repairs hidden focus', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1023, height: 900 });
  await page.goto('/templates?role=backend&filter=technical');
  const trigger = page.locator('[data-action="gallery-filters-open"]');
  await trigger.click();
  const sheet = page.getByTestId('gallery-filter-sheet');
  await expect(
    sheet.locator('[data-action="gallery-filters-close"]'),
  ).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(sheet.locator('[data-role="backend"]')).toBeFocused();
  for (const value of [
    'all',
    'sample',
    'ats',
    'one-page',
    'photo',
    'first-job',
    'technical',
    'management',
  ]) {
    await page.keyboard.press('Tab');
    await expect(sheet.locator(`[data-filter="${value}"]`)).toBeFocused();
  }
  await page.keyboard.press('Tab');
  await expect(
    sheet.locator('[data-action="gallery-filters-clear"]'),
  ).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(
    sheet.locator('[data-action="gallery-filters-apply"]'),
  ).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(
    sheet.locator('[data-action="gallery-filters-close"]'),
  ).toBeFocused();
  await page.setViewportSize({ width: 1024, height: 900 });
  await expect(sheet).toHaveCount(0);
  await expect(
    page.locator('.gallery-desktop [data-role="backend"]'),
  ).toBeFocused();
  await page.setViewportSize({ width: 1023, height: 900 });
  await expect(trigger).toBeFocused();
  const roleChip = page.locator(
    '[data-action="gallery-filter-remove"][data-filter="role"]',
  );
  const styleChip = page.locator(
    '[data-action="gallery-filter-remove"][data-filter="style"]',
  );
  await roleChip.click();
  await expect(styleChip).toBeFocused();
  await styleChip.click();
  await expect(trigger).toBeFocused();
  await trigger.click();
  await sheet.locator('[data-filter="technical"]').click();
  await page
    .locator('[data-slot="sheet-overlay"]')
    .click({ position: { x: 1, y: 1 } });
  await expect(sheet).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await expect(page).toHaveURL(/filter=technical$/u);
  const card = page.locator('[data-template]:visible').first();
  await card.focus();
  await page.setViewportSize({ width: 1024, height: 900 });
  await expect(card).toBeFocused();
  await page.setViewportSize({ width: 1023, height: 900 });
  await expect(card).toBeFocused();
});

for (const locale of ['vi', 'en']) {
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

test('incoming URLs and history derive both groups without remounting', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale', value: 'en', url: 'http://127.0.0.1:20092',
  }]);
  await page.goto(
    '/templates?role=backend&filter=technical&source=a%20%26%20b',
  );
  const fields = page.locator('.gallery-desktop');
  const count = page.getByTestId('gallery-count');
  await fields.locator('[data-filter="ats"]').click();
  await expect(page).toHaveURL(
    /source=a(?:%20|\+)%(?:26)(?:%20|\+)b&role=backend&filter=ats$/u,
  );
  await expect(count).toContainText('0');
  await page.evaluate(() => {
    const node = document.querySelector('[data-testid="gallery-count"] span')!;
    (window as Window & { countNode?: Element }).countNode = node;
  });
  await fields.locator('[data-filter="ats"]').click();
  expect(
    await page.evaluate(
      () =>
        document.querySelector('[data-testid="gallery-count"] span')
        === (window as Window & { countNode?: Element }).countNode,
    ),
  ).toBe(true);
  await page.locator('[data-template="one-page-tight"]').evaluate((node) => {
    node.setAttribute('data-retained', 'true');
  });
  await page
    .getByRole('link', { name: 'Library', exact: true })
    .first()
    .click();
  await expect(page.locator('[data-template]:visible')).toHaveCount(20);
  await page.goBack();
  await expect(count).toContainText('0');
  await expect(fields.locator('[data-role="backend"]')).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(fields.locator('[data-filter="ats"]')).toHaveAttribute(
    'aria-current',
    'page',
  );
  await page.goForward();
  await expect(fields.locator('[data-role="all"]')).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(
    page.locator('[data-template="one-page-tight"]'),
  ).toHaveAttribute('data-retained', 'true');
});

async function assertGalleryDetail(
  page: Page,
  locale: 'vi' | 'en',
  width: number,
  tab: 'page' | 'pdf' | 'ats',
): Promise<void> {
  await expect.poll(() => galleryFontLoaded(page.locator('html'))).toBe(true);
  await expect(page.locator('.template-detail__info h1')).toHaveCSS(
    'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
  );
  const names = locale === 'vi'
    ? ['Trang CV', 'PDF', 'ATS đọc được gì']
    : ['Page', 'PDF', 'What an ATS reads'];
  const selected = tab === 'page' ? 0 : tab === 'pdf' ? 1 : 2;
  const tabs = page.getByRole('tab');
  await expect(tabs).toHaveCount(3);
  await expect(tabs).toHaveText(names);
  for (let index = 0; index < names.length; index += 1) {
    await expect(tabs.nth(index)).toHaveAttribute(
      'aria-selected', String(index === selected),
    );
  }
  await expect(page.getByRole('tablist')).toMatchAriaSnapshot(
    `- tablist "${locale === 'vi'
      ? 'Cách xem CV mẫu'
      : 'How to view the sample'}":\n`
      + names.map((name, index) =>
        `  - tab "${name}"${index === selected ? ' [selected]' : ''}`,
      ).join('\n'),
  );
  await expect.poll(
    () => measureGalleryDetail(page.locator('.template-detail__layout')),
  ).toEqual({ stacked: width < 900, columns: width >= 900, contained: true });
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1);
  if (tab === 'page') {
    await expect(page.locator('.resume-document')).toContainText(
      'Đinh Công Khánh',
    );
    await expect(page.locator('.template-paper')).toHaveCSS(
      'background-color', 'rgb(255, 255, 255)',
    );
  } else if (tab === 'pdf') {
    const image = page.locator('img[data-pdf-page]');
    await expect(image).toHaveCount(1);
    await expect(image).toHaveAttribute(
      'src', `/templates/pages/executive-band-${locale}-p1.png`,
    );
    await expect(image).toHaveAttribute('alt', locale === 'vi'
      ? 'Trang 1/1 của CV mẫu Executive Band'
      : 'Page 1 of 1 of the Executive Band sample resume');
    await image.scrollIntoViewIfNeeded();
    await image.evaluate((element) => (element as HTMLImageElement).decode());
    await expect.poll(() => image.evaluate((element) => {
      const img = element as HTMLImageElement;
      const box = img.getBoundingClientRect();
      const sheet = img.closest('.template-paper')!.getBoundingClientRect();
      return img.naturalWidth === 1240 && img.naturalHeight === 1754
        && Math.abs(box.width - sheet.width) <= 1
        && Math.abs(box.width / box.height - 1240 / 1754) <= 0.001;
    })).toBe(true);
    await expect(page.locator('figcaption')).toHaveText(
      locale === 'vi' ? 'Trang 1/1' : 'Page 1 of 1',
    );
    await page.evaluate(() => window.scrollTo(0, 0));
  } else {
    const text = page.locator('[data-ats-text]');
    await expect(text).toBeVisible();
    await expect(text).toHaveCSS('white-space', 'pre-wrap');
    await expect(text).toHaveCSS('background-color', 'rgb(255, 255, 255)');
    await expect(text).toHaveCSS('color', 'rgb(23, 26, 24)');
    await expect.poll(() => measureTextContainment(text))
      .toEqual({ visible: true, contained: true });
    const content = (await text.innerText()).normalize('NFC');
    const terms = locale === 'vi'
      ? ['Vũ Đức Trung', 'TÓM TẮT', 'KINH NGHIỆM LÀM VIỆC', 'HỌC VẤN']
      : ['Lan Hoang', 'EXECUTIVE SUMMARY', 'EXPERIENCE', 'EDUCATION'];
    let previous = -1;
    for (const term of terms) {
      const position = content.indexOf(term);
      expect(position).toBeGreaterThan(previous);
      expect(content.indexOf(term, position + term.length)).toBe(-1);
      previous = position;
    }
  }
}

for (const locale of ['vi', 'en'] as const) {
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
              const id = tab === 'page' ? 'creative-accent' : 'executive-band';
              await page.goto(`/templates/${id}`);
              if (tab !== 'page') {
                const name = tab === 'pdf'
                  ? 'PDF'
                  : locale === 'vi' ? 'ATS đọc được gì' : 'What an ATS reads';
                await page.getByRole('tab', { name }).click();
              }
              await page.evaluate(() => document.fonts.ready);
              await assertGalleryDetail(page, locale, width, tab);
            });
        }
      }
    }
  }
}

for (const locale of ['vi', 'en'] as const) {
  for (const theme of THEMES) {
    test(`template detail changes layout at 900px ${locale} ${theme}`,
      async ({ page }) => {
        await page.context().addCookies([
          { name: 'aboutme-locale', value: locale,
            url: 'http://127.0.0.1:20092' },
          { name: 'aboutme-theme', value: theme,
            url: 'http://127.0.0.1:20092' },
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
}

test('template detail measurements detect displacement and clipped ATS text',
  async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/templates/executive-band');
    const layout = page.locator('.template-detail__layout');
    expect((await measureGalleryDetail(layout)).contained).toBe(true);
    await page.addStyleTag({
      content: '.template-detail__info { '
        + 'transform: translateX(4px) !important; }',
    });
    await expect.poll(async () => (await measureGalleryDetail(layout))
      .contained).toBe(false);
    await page.getByRole('tab', { name: 'ATS đọc được gì' }).click();
    const text = page.locator('[data-ats-text]');
    expect((await measureTextContainment(text)).contained).toBe(true);
    await text.evaluate((element) => {
      Object.assign((element as HTMLElement).style, {
        height: '24px', overflow: 'hidden', whiteSpace: 'nowrap',
      });
    });
    expect((await measureTextContainment(text)).contained).toBe(false);
  });
