import { expect, type Page } from '@playwright/test';

/** Page membership recorded from the accepted Vietnamese renderer captures. */
export async function expectRendererPageMembership(
  page: Page, template: string, justify = false, letter = false,
  language: 'vi' | 'en' = 'vi',
): Promise<void> {
  const pages = page.locator('.resume-page:not(.pagination-measurement)');
  if (language === 'en') {
    await expectEnglishMembership(page, template, justify, letter);
    return;
  }
  const titles = [
    'Kỹ sư trưởng', 'Kỹ sư Công nghệ thông tin', 'Thiết kế hệ thống',
    'Lập trình TypeScript', 'Tiếng Việt', 'Tiếng Anh',
    'Chứng chỉ Kiến trúc phần mềm', 'Từ điển dữ liệu mở',
    'Giải thưởng Sáng tạo trẻ',
  ];
  const split = template === 'classic-serif'
    ? justify
      ? 5
      : 6
    : template === 'executive-band'
      ? 6
      : template === 'consulting-formal'
        ? letter
          ? 7
          : 8
        : 8;
  const twoColumns = ['modern-sidebar', 'engineer-compact'].includes(template);
  await expect(pages, 'Renderer page count.').toHaveCount(twoColumns ? 1 : 2);
  if (twoColumns) {
    await expect(pages.locator('.entry-title'), 'Renderer page membership.')
      .toHaveCount(titles.length);
  } else {
    await expect(pages.nth(0).locator('.entry-title'),
      'Renderer page membership.')
      .toHaveText(titles.slice(0, split));
    await expect(pages.nth(1).locator('.entry-title'),
      'Renderer page membership.')
      .toHaveText(titles.slice(split));
  }
  await expect(pages.locator('.resume-name'), 'Renderer first-page header.')
    .toHaveCount(1);
  await expect(pages.nth(0).locator('.resume-name'),
    'Renderer first-page header.')
    .toHaveText('Nguyễn Thị Minh Châu');
  for (const root of await pages.all()) {
    await expect(root, 'Renderer page overflow.')
      .toHaveAttribute('data-page-overflow', 'false');
  }
}

const ENGLISH_TITLES = [
  'Principal Engineer', 'B.Sc. Mathematics', 'Go', 'TypeScript', 'English',
  'Vietnamese', 'Certified Analytical Engineer', 'Note G Interpreter',
  'Lovelace Medal', 'Hackathon Winner',
] as const;

// Approved against hosted Linux output. Executive-band and academic-dense
// break at the same section as the accepted Vietnamese captures.
const ENGLISH_PAGES: Record<string, readonly (readonly number[])[]> = {
  'classic-serif': [[0, 1, 2, 3, 4, 5, 6], [7, 8, 9]],
  'classic-serif-justify': [[0, 1, 2, 3, 4, 5], [6, 7, 8, 9]],
  'engineer-compact': [[0, 7, 8, 9, 2, 3, 6, 4, 5, 1]],
  'modern-sidebar': [[0, 1, 7, 8, 9, 2, 3, 4, 5, 6]],
  'executive-band': [[0, 1, 2, 3, 4, 5], [6, 7, 8, 9]],
  'consulting-formal': [[0, 1, 2, 3, 4, 5, 6, 7, 8], [9]],
  'consulting-formal-letter': [[0, 1, 2, 3, 4, 5, 6, 7], [8, 9]],
  'academic-dense': [[0, 1, 2, 3, 4, 5, 6, 7], [8, 9]],
};

async function expectEnglishMembership(
  page: Page, template: string, justify: boolean, letter: boolean,
): Promise<void> {
  const key = template + (justify ? '-justify' : letter ? '-letter' : '');
  const expected = ENGLISH_PAGES[key]!.map((indices) =>
    indices.map((index) => ENGLISH_TITLES[index]!));
  const pages = page.locator('.resume-page:not(.pagination-measurement)');
  const actual = await pages.evaluateAll((nodes) => nodes.map((node) =>
    Array.from(node.querySelectorAll('.entry-title'), (entry) =>
      entry.textContent!.normalize('NFC').replace(/\s+/gu, ' ').trim())));
  const detail = ` Actual per-page membership: ${JSON.stringify(actual)}`;
  expect(actual.length, 'Renderer page count.' + detail)
    .toBe(expected.length);
  expect(actual, 'Renderer page membership.' + detail).toEqual(expected);
  await expect(pages.locator('.resume-name'), 'Renderer first-page header.')
    .toHaveCount(1);
  await expect(pages.nth(0).locator('.resume-name'),
    'Renderer first-page header.').toHaveText('Ada Lovelace');
  for (const root of await pages.all()) {
    await expect(root, 'Renderer page overflow.')
      .toHaveAttribute('data-page-overflow', 'false');
  }
}
