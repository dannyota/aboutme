import { expect, test, type Page } from '@playwright/test';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';

import {
  galleryFontLoaded,
  measureGalleryGrid,
  measureTextContainment,
  measureVerticalFlow,
} from './display-assertions';
import {
  CHROME_PIXEL_TOLERANCE,
  verifyScreenshot,
  waitForImages,
} from './support';

const CASES = [
  { name: 'sheet', locale: 'vi', theme: 'dark', width: 390 },
  { name: 'sheet', locale: 'en', theme: 'light', width: 390 },
  { name: 'filtered', locale: 'vi', theme: 'dark', width: 390 },
  { name: 'filtered', locale: 'en', theme: 'light', width: 1440 },
  { name: 'rows', locale: 'vi', theme: 'light', width: 1024 },
] as const;
const LIVE_THUMBNAILS
  = TEMPLATES.length
    - new Set(SAMPLES.map(({ templateId }) => templateId)).size;

for (const { name, locale, theme, width } of CASES) {
  test(`Library ${name} ${locale} ${theme} ${width} matches baseline`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([
      { name: 'aboutme-locale', value: locale, url: 'http://127.0.0.1:20092' },
      { name: 'aboutme-theme', value: theme, url: 'http://127.0.0.1:20092' },
    ]);
    const path
      = name === 'filtered'
        ? '/templates?role=backend&filter=technical'
        : '/templates';
    const response = await page.goto(path);
    expect(response?.status()).toBe(200);
    await page.addStyleTag({
      content:
        '*, *::before, *::after { transition: none !important; '
        + 'animation: none !important; }',
    });
    await page.evaluate(() => document.fonts.ready);
    await assertLibrary(page, locale, theme, width, name === 'filtered');
    await page.locator('img[loading="lazy"]').evaluateAll((images) => {
      for (const image of images) (image as HTMLImageElement).loading = 'eager';
    });
    await waitForImages(page);
    if (name === 'sheet') {
      await page.locator('[data-action="gallery-filters-open"]').click();
      await expect(page.getByTestId('gallery-filter-sheet')).toBeVisible();
      await expect(
        page.locator('[data-action="gallery-filters-close"]'),
      ).toBeFocused();
      await assertLibrarySheet(page, locale, theme);
    } else {
      const height = await page.evaluate(
        () => document.documentElement.scrollHeight,
      );
      await page.setViewportSize({ width, height });
      if (name === 'rows') {
        await expect(page.locator('[data-sheet-thumbnail-render]')).toHaveCount(
          LIVE_THUMBNAILS,
        );
      }
      const settled = await page.evaluate(
        () => document.documentElement.scrollHeight,
      );
      if (settled !== height) {
        await page.setViewportSize({ width, height: settled });
      }
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);
    }
    await page.evaluate(
      () =>
        new Promise<void>((done) => {
          requestAnimationFrame(() => requestAnimationFrame(() => done()));
        }),
    );
    expect(
      await page.evaluate(
        () =>
          document.documentElement.scrollWidth
          - document.documentElement.clientWidth,
      ),
    ).toBe(0);
    await verifyScreenshot(
      page,
      `gallery--${name}--${locale}--${theme}--${width}.png`,
      testInfo,
      undefined,
      CHROME_PIXEL_TOLERANCE,
      name !== 'sheet',
    );
  });
}

async function assertLibrary(
  page: Page,
  locale: 'vi' | 'en',
  theme: 'light' | 'dark',
  width: number,
  filtered: boolean,
): Promise<void> {
  await expect.poll(() => galleryFontLoaded(page.locator('html'))).toBe(true);
  await expect(page.getByTestId('gallery-header').getByRole('heading'))
    .toHaveCSS(
      'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
    );
  const grid = page.locator('.gallery-grid');
  const columns = width < 641 ? 2 : width < 900 ? 3 : width < 1180 ? 4 : 5;
  await expect.poll(() => measureGalleryGrid(grid)).toEqual({
    columns,
    columnGap: width < 641 ? 14 : 28,
    rowGap: width < 641 ? 24 : 40,
    ordered: true,
  });
  if (width >= 1024) {
    const fields = page.locator('.gallery-desktop');
    await expect(fields).toBeVisible();
    await expect(page.locator('[data-action="gallery-filters-open"]'))
      .toBeHidden();
    await expect(fields.locator('.library-filters'))
      .toHaveCSS('row-gap', '12px');
    await expect.poll(() =>
      measureVerticalFlow(fields.locator('.library-filters > section')),
    ).toBe(true);
    await expect(fields.locator('[data-role="'
      + (filtered ? 'backend' : 'all') + '"]')).toHaveCSS(
      'background-color', theme === 'light'
        ? 'rgb(38, 64, 156)'
        : 'rgb(143, 166, 240)',
    );
  }
  const cards = page.locator('[data-template]:visible');
  await expect(cards).toHaveCount(filtered ? 1 : 20);
  if (filtered) {
    await expect(cards).toHaveAttribute('data-template', 'one-page-tight');
    await expect(cards).toHaveAttribute('href', '/templates/one-page-tight');
    await expect(page.getByTestId('gallery-count')).toHaveText(
      locale === 'vi' ? '1 mẫu' : '1 template',
    );
    if (width < 1024) {
      const chips = page.getByTestId('gallery-active-filters');
      await expect(chips.locator('[data-action="gallery-filter-remove"]'))
        .toHaveText(['Backend', locale === 'vi' ? 'Kỹ thuật' : 'Technical']);
      await expect.poll(() => measureTextContainment(chips))
        .toEqual({ visible: true, contained: true });
    }
  } else {
    const ids = [
      'one-page-tight', 'engineer-compact', 'creative-accent', 'nordic-muted',
      'elegant-serif-two', 'mono-print', 'minimal-air', 'international-lang',
      'consulting-formal', 'ats-plain', 'graduate-friendly', 'executive-band',
      'modern-sidebar', 'academic-dense', 'classic-serif', 'designer-tag',
      'editorial-wide', 'government-formal', 'high-contrast', 'startup-bold',
    ];
    await expect(cards).toHaveCount(ids.length);
    for (let index = 0; index < ids.length; index += 1) {
      await expect(cards.nth(index))
        .toHaveAttribute('data-template', ids[index]!);
      await expect(cards.nth(index)).toHaveAttribute(
        'href', `/templates/${ids[index]}`,
      );
    }
  }
  for (const card of await cards.all()) {
    await card.scrollIntoViewIfNeeded();
    const image = card.locator('img[data-page-image]');
    if (await image.count()) {
      const id = await card.getAttribute('data-template');
      await expect(image).toHaveAttribute(
        'src', `/templates/pages/${id}-${locale}-p1.png`,
      );
      await image.evaluate((node) => (node as HTMLImageElement).decode());
      await expect.poll(() => image.evaluate((node) => {
        const img = node as HTMLImageElement;
        const box = img.getBoundingClientRect();
        return img.naturalWidth === 1240 && img.naturalHeight === 1754
          && Math.abs(box.width / box.height - 1240 / 1754) <= 0.001;
      })).toBe(true);
    } else {
      const thumbnail = card.locator('[data-sheet-thumbnail]');
      await expect(thumbnail.locator('[data-sheet-thumbnail-render]'))
        .toHaveCount(1);
      await expect.poll(() => thumbnail.evaluate((node) => {
        const box = node.getBoundingClientRect();
        const content = node.querySelector('.scaled-sheet-content')!;
        const transform = new DOMMatrixReadOnly(
          getComputedStyle(content).transform,
        );
        return Math.abs(transform.a - box.width / 794) <= 0.001
          && Math.abs(box.width / box.height - 794 / 1123) <= 0.001;
      })).toBe(true);
    }
  }
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1);
}

async function assertLibrarySheet(
  page: Page,
  locale: 'vi' | 'en',
  theme: 'light' | 'dark',
): Promise<void> {
  const sheet = page.getByTestId('gallery-filter-sheet');
  const heading = locale === 'vi' ? 'Bộ lọc' : 'Filters';
  await expect(sheet).toHaveRole('dialog');
  await expect(sheet).toHaveCSS('background-color', theme === 'light'
    ? 'rgb(255, 255, 255)'
    : 'rgb(26, 33, 56)');
  await expect(sheet.locator('[data-role="all"]')).toHaveCSS(
    'background-color', theme === 'light'
      ? 'rgb(38, 64, 156)'
      : 'rgb(143, 166, 240)',
  );
  await expect(sheet).toMatchAriaSnapshot(`- dialog "${heading}":
  - heading "${heading}" [level=2]`);
  await expect(sheet.getByRole('heading')).toHaveCount(3);
  await expect(sheet.locator('[data-role]')).toHaveCount(10);
  await expect(sheet.locator('[data-filter]')).toHaveCount(8);
  await expect(sheet.locator('[data-role="all"]'))
    .toHaveAttribute('aria-pressed', 'true');
  await expect(sheet.locator('[data-filter="all"]'))
    .toHaveAttribute('aria-current', 'page');
  await expect(sheet.locator('[data-action="gallery-filters-close"]'))
    .toBeFocused();
  await expect(sheet.locator('[data-action="gallery-filters-apply"]'))
    .toHaveText(locale === 'vi' ? 'Xem 20 mẫu' : 'Show 20 templates');
  await expect.poll(() => sheet.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return box.width > 0 && box.height > 0 && box.left >= -1
      && box.right <= innerWidth + 1 && box.top >= -1
      && box.bottom <= innerHeight + 1 && box.height <= innerHeight * 0.85 + 1
      && element.scrollWidth <= element.clientWidth + 1;
  })).toBe(true);
}

for (const locale of ['vi', 'en'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [390, 1440]) {
      for (const filtered of [false, true]) {
        for (const viewer of ['light', 'dark'] as const) {
          test(`Library assertions ${locale} ${theme} ${viewer} ${width} `
            + `${filtered}`, async ({ page }) => {
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
            await page.goto(filtered
              ? '/templates?role=backend&filter=technical'
              : '/templates');
            await assertLibrary(page, locale, theme, width, filtered);
            if (width === 390 && !filtered) {
              await page.locator('[data-action="gallery-filters-open"]')
                .click();
              await assertLibrarySheet(page, locale, theme);
            }
          });
        }
      }
    }
  }
}

test('gallery geometry detects changed columns and overlapping cards', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  const grid = page.locator('.gallery-grid');
  expect((await measureGalleryGrid(grid)).columns).toBe(2);
  expect((await measureGalleryGrid(grid)).ordered).toBe(true);
  await grid.evaluate((element) => {
    (element as HTMLElement).style.gridTemplateColumns = '1fr';
  });
  await expect.poll(async () => (await measureGalleryGrid(grid)).columns)
    .not.toBe(2);
  await grid.evaluate((element) => {
    (element as HTMLElement).style.gridTemplateColumns = '';
    const first = element.children[0]!.getBoundingClientRect();
    const second = element.children[1] as HTMLElement;
    const box = second.getBoundingClientRect();
    second.style.setProperty('transition', 'none', 'important');
    second.style.marginLeft = `${first.right - box.left - 4}px`;
  });
  await expect.poll(() => grid.evaluate((element) => {
    const first = element.children[0]!.getBoundingClientRect();
    const second = element.children[1]!.getBoundingClientRect();
    return first.right - second.left;
  })).toBeCloseTo(4, 0);
  await expect.poll(() => grid.evaluate((element) => {
    const first = element.children[0]!.getBoundingClientRect();
    const second = element.children[1]!.getBoundingClientRect();
    return first.height > 0 && second.height > 0
      && Math.abs(first.top - second.top) <= 1;
  })).toBe(true);
  expect((await measureGalleryGrid(grid)).ordered).toBe(false);
});

test('filter sheet exposes its heading and controls', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  await page.locator('[data-action="gallery-filters-open"]').click();
  const sheet = page.getByTestId('gallery-filter-sheet');
  const control = sheet.locator('[data-filter="ats"]');
  await expect(control).toBeVisible();
  await expect(sheet.getByRole('heading', { name: 'Bộ lọc' })).toBeVisible();
  await control.evaluate((element) => {
    (element as HTMLElement).style.display = 'none';
  });
  await expect(control).toBeHidden();
  await sheet.getByRole('heading', { name: 'Bộ lọc' }).evaluate((element) => {
    element.setAttribute('aria-hidden', 'true');
  });
  await expect(sheet.getByRole('heading', { name: 'Bộ lọc' })).toHaveCount(0);
});
