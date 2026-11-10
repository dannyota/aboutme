import { expect, type Page } from '@playwright/test';

import {
  expectGalleryDetails, expectLibraryStructure,
} from './gallery-details';

import {
  expectScreen, galleryFontLoaded, measureGalleryDetail, measureGalleryGrid,
  measureTextContainment, measureVerticalFlow,
} from './display-assertions';
import {
  galleryDetailContract, libraryContract, prepareScreen,
} from './support';

export async function assertGalleryDetail(
  page: Page,
  locale: 'vi' | 'en',
  width: number,
  tab: 'page' | 'pdf' | 'ats',
  theme: 'light' | 'dark' = 'light',
): Promise<void> {
  await expectScreen(page, galleryDetailContract(locale, theme, tab));
  await expectGalleryDetails(page, locale, tab);
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
    'Gallery detail geometry.',
  ).toEqual({ stacked: width < 900, columns: width >= 900, contained: true });
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1);
  if (tab === 'page') {
    const article = page.locator('.resume-document');
    await expect(article).toHaveCSS('font-family',
      '"Source Sans 3", "Noto Sans"');
    await expect(article).toHaveCSS('font-size', '15px');
    await expect(article).toHaveCSS('font-synthesis', 'none');
    await expect(article).toHaveCSS('color', 'rgb(31, 26, 23)');
    await expect(article.locator('.resume-header')).toHaveCSS(
      'background-color', 'rgb(255, 240, 235)');
    await expect.poll(() => page.evaluate(() =>
      ['Source Sans 3', 'Noto Sans'].every((family) =>
        Array.from(document.fonts).some((face) =>
          face.family.replaceAll('"', '') === family
          && face.status === 'loaded'))), 'Template document fonts.')
      .toBe(true);
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

export async function assertLibrary(
  page: Page,
  locale: 'vi' | 'en',
  theme: 'light' | 'dark',
  width: number,
  filtered: boolean,
): Promise<void> {
  await prepareScreen(page);
  await expectScreen(page, libraryContract(locale, theme));
  await expectLibraryStructure(page, filtered);
  await expect.poll(() => galleryFontLoaded(page.locator('html'))).toBe(true);
  await expect(page.getByTestId('gallery-header').getByRole('heading'))
    .toHaveCSS(
      'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
    );
  const grid = page.locator('.gallery-grid');
  const columns = width < 641 ? 2 : width < 900 ? 3 : width < 1180 ? 4 : 5;
  await expect.poll(() => measureGalleryGrid(grid), 'Library grid geometry.')
    .toEqual({
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
