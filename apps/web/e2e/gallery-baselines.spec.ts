import { expect, test, type Page } from '@playwright/test';
import { SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';

import { captureReviewImage } from './review-images';
import {
  expectScreen, expectScreenClass,
  measureGalleryGrid,
} from './display-assertions';
import {
  applyStyleMutation, mutateAttribute, mutateText, proveMutation,
  screenMutationTests, mutationTest,
} from './style-mutation';
import { expectLibraryStructure } from './gallery-details';
import { expectLibraryGrid } from './gallery-display';
import { expectSheetStructure, sheetType } from './filter-sheet-details';
import { styleProofs } from './screen-style-proofs';
import {
  assertLibrary,
  filterSheetContract,
  expectThumbnailRenders,
  libraryContract,
  prepareScreen,
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
  test.describe(`display locale ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    const title = `Library ${name} ${locale} ${theme} ${width} `
      + 'keeps content and layout';
    test(title, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      await page.context().addCookies([
        { name: 'aboutme-locale', value: locale,
          url: 'http://127.0.0.1:20092' },
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
      await prepareScreen(page);
      if (name === 'sheet') {
        await page.locator('[data-action="gallery-filters-open"]').click();
        await expect(page.getByTestId('gallery-filter-sheet')).toBeVisible();
        await expect(
          page.locator('[data-action="gallery-filters-close"]'),
        ).toBeFocused();
        await assertLibrarySheet(page, locale, theme);
      } else {
        await prepareScreen(page);
        if (name === 'rows') {
          await expectThumbnailRenders(page, LIVE_THUMBNAILS);
        }
      }
      expect(
        await page.evaluate(
          () =>
            document.documentElement.scrollWidth
            - document.documentElement.clientWidth,
        ),
      ).toBe(0);
      if (name === 'sheet' && locale === 'en' && theme === 'light'
        && width === 390) {
        await captureReviewImage(testInfo, 'library-sheet-en-light-390', () =>
          page.screenshot({ animations: 'disabled', timeout: 750 }));
      }
    });
  });
}

async function assertLibrarySheet(
  page: Page,
  locale: 'vi' | 'en',
  theme: 'light' | 'dark',
): Promise<void> {
  await expectScreen(page, filterSheetContract('gallery', locale, theme));
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
  const scrollPositions = await sheet.evaluate((element) =>
    [element, ...element.querySelectorAll('*')].map((node) => node.scrollTop));
  const controls = sheet.locator('[data-role], [data-filter], [data-action]');
  try {
    for (const control of await controls.all()) {
      await expect(control).toBeVisible();
      await control.scrollIntoViewIfNeeded();
      expect(await control.evaluate((element) => {
        const box = element.getBoundingClientRect();
        const sheet = element.closest('[data-testid="gallery-filter-sheet"]');
        if (!sheet || box.width <= 0 || box.height <= 0) {
          return false;
        }
        for (let parent = element.parentElement; parent;
          parent = parent.parentElement) {
          const bounds = parent.getBoundingClientRect();
          const style = getComputedStyle(parent);
          const clipsX = parent === sheet
            || /^(auto|scroll|hidden|clip)$/u.test(style.overflowX);
          const clipsY = parent === sheet
            || /^(auto|scroll|hidden|clip)$/u.test(style.overflowY);
          if ((clipsX && (box.left < bounds.left - 1
            || box.right > bounds.right + 1))
          || (clipsY && (box.top < bounds.top - 1
            || box.bottom > bounds.bottom + 1))) {
            return false;
          }
          if (parent === sheet) {
            break;
          }
        }
        return true;
      })).toBe(true);
    }
  } finally {
    await sheet.evaluate((element, positions) => {
      [element, ...element.querySelectorAll('*')].forEach((node, index) => {
        node.scrollTop = positions[index] ?? 0;
      });
    }, scrollPositions);
  }
  await expect.poll(() => sheet.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return box.width > 0 && box.height > 0 && box.left >= -1
      && box.right <= innerWidth + 1 && box.top >= -1
      && box.bottom <= innerHeight + 1 && box.height <= innerHeight * 0.85 + 1
      && element.scrollWidth <= element.clientWidth + 1;
  })).toBe(true);
}

for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
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
  });
}

for (const [name, selector, styles] of [
  ['columns', '.gallery-grid', {
    'grid-template-columns': ['1fr', /^\d+(?:\.\d+)?px$/u],
  }],
  ['overlap', '.gallery-grid > :nth-child(2)', {
    transform: ['translateX(-18px)', 'matrix(1, 0, 0, 1, -18, 0)'],
  }],
  ['row gap', '.gallery-grid > :nth-child(n+3)', {
    transform: ['translateY(4px)', 'matrix(1, 0, 0, 1, 0, 4)'],
  }],
] as const) {
  mutationTest(`Library assertions reject changed ${name}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto('/templates');
    await assertLibrary(page, 'vi', 'light', 390, false);
    await proveMutation(() => expectLibraryGrid(page, 390), () =>
      applyStyleMutation(page.locator(selector), styles),
    'Library grid geometry.');
  });
}

for (const name of ['control', 'heading'] as const) {
  mutationTest(`Library sheet rejects a hidden ${name}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto('/templates');
    await page.locator('[data-action="gallery-filters-open"]').click();
    const sheet = page.getByTestId('gallery-filter-sheet');
    const assertSheet = () => assertLibrarySheet(page, 'vi', 'light');
    const target = name === 'control'
      ? sheet.locator('[data-filter="ats"]')
      : sheet.locator('h2').first();
    await assertSheet();
    const assertion = () => name === 'control'
      ? expect(target, 'Library sheet control visibility.').toBeVisible()
      : expectSheetStructure(page, 'gallery', 'vi');
    await assertion();
    const restore = name === 'control'
      ? await applyStyleMutation(target, { display: 'none' })
      : async () => {
        await target.evaluate((element) =>
          element.removeAttribute('aria-hidden'));
      };
    if (name === 'heading') {
      await target.evaluate((element) =>
        element.setAttribute('aria-hidden', 'true'));
      await expect(target).toHaveAttribute('aria-hidden', 'true');
    }
    await expect(assertion()).rejects.toThrow(name === 'control'
      ? 'Library sheet control visibility.'
      : 'Sheet accessible structure.');
    await restore();
    await assertSheet();
  });
}

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
        const colors = theme === 'light'
          ? { background: 'rgb(38, 64, 156)', text: 'rgb(255, 255, 255)' }
          : { background: 'rgb(143, 166, 240)', text: 'rgb(12, 16, 32)' };
        for (const role of ['backend', 'frontend']) {
          const selected = fields.locator(`[data-role="${role}"]`);
          if (role === 'frontend') {
            await selected.click();
          }
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
  if (header !== null) {
    expect(header.x + header.width).toBeLessThanOrEqual(360);
  }
});

for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const theme of ['light', 'dark'] as const) {
      test(`Library layouts fit in ${locale} ${theme}`, async ({ page }) => {
        await page.context().addCookies([
          {
            name: 'aboutme-locale',
            value: locale,
            url: 'http://127.0.0.1:20092',
          },
          { name: 'aboutme-theme', value: theme,
            url: 'http://127.0.0.1:20092' },
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
              await sheet.evaluate((node) =>
                node.scrollWidth > node.clientWidth),
            ).toBe(false);
            await sheet.locator('[data-action="gallery-filters-close"]')
              .click();
          } else {
            await expect(trigger).toBeHidden();
            await expect(page.getByTestId('gallery-active-filters'))
              .toBeHidden();
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

screenMutationTests('Library', async (page) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  await prepareScreen(page);
}, libraryContract('vi', 'light'));

screenMutationTests('Library sheet', async (page) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  await page.locator('[data-action="gallery-filters-open"]').click();
}, filterSheetContract('gallery', 'vi', 'light'));

for (const defect of ['name', 'visibility', 'box'] as const) {
  mutationTest(`Library sheet rejects changed Close ${defect}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto('/templates');
    await page.locator('[data-action="gallery-filters-open"]').click();
    const contract = filterSheetContract('gallery', 'vi', 'light');
    await expectScreen(page, contract);
    const close = page.locator('[data-action="gallery-filters-close"]');
    await expect(close).toHaveText('');
    await proveMutation(() => expectScreenClass(page, contract, 'containment'),
      () => defect === 'name'
        ? mutateAttribute(close, 'aria-label', 'MUTATED')
        : applyStyleMutation(close, defect === 'visibility'
            ? { opacity: '0' }
            : { transform: ['translateY(-500px)', /matrix/u] }),
      defect === 'name'
        ? 'Screen control name.'
        : defect === 'visibility'
          ? 'Screen control visibility.'
          : 'Screen control box.');
  });
}

mutationTest('Library rejects missing list semantics', async ({ page }) => {
  await page.goto('/templates');
  await prepareScreen(page);
  await proveMutation(() => expectLibraryStructure(page, false),
    () => mutateAttribute(page.locator('.gallery-grid'),
      'role', 'presentation'),
    'Library accessible list.');
});

styleProofs('Library sheet', async (page) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  await page.locator('[data-action="gallery-filters-open"]').click();
}, sheetType('gallery'));

mutationTest('Library sheet rejects missing option semantics', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/templates');
  await page.locator('[data-action="gallery-filters-open"]').click();
  await proveMutation(() => expectSheetStructure(page, 'gallery', 'vi'),
    () => mutateAttribute(page.getByTestId('gallery-filter-sheet')
      .getByTestId('gallery-roles'), 'role', 'presentation'),
    'Sheet accessible structure.');
});

mutationTest('Library rejects changed accessible template names', async ({
  page,
}) => {
  await page.goto('/templates');
  await prepareScreen(page);
  await proveMutation(() => expectLibraryStructure(page, false),
    () => mutateText(page.locator('[data-template]').first()
      .locator(':scope > span > span:first-child'), 'MUTATED'),
    'Library accessible list.');
});
