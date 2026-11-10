import { expect, type Page } from '@playwright/test';

import type { StyleRule } from './screen-expectations';

export function sheetType(surface: 'gallery' | 'showcase'): StyleRule[] {
  const root = `[data-testid="${surface}-filter-sheet"]`;
  return [
    {
      selector: `${root} > div > h2`,
      message: 'Sheet title typography',
      css: { 'font-size': '20px', 'font-weight': '700',
        'line-height': '26px', 'letter-spacing': 'normal' },
    },
    {
      selector: `${root} [data-action="${surface}-filters-apply"]`,
      message: 'Sheet action typography',
      css: { 'font-size': '15px', 'font-weight': '600',
        'line-height': '22.5px', 'letter-spacing': 'normal' },
    },
    {
      selector: `${root} [data-role]:not([aria-pressed="true"])`,
      message: 'Sheet option typography',
      css: { 'font-size': '14px', 'font-weight': '500',
        'line-height': '21px', 'letter-spacing': 'normal' },
    },
    {
      selector: `${root} [data-role][aria-pressed="true"]`,
      message: 'Sheet selected option typography',
      css: { 'font-size': '14px', 'font-weight': '600',
        'line-height': '21px', 'letter-spacing': 'normal' },
    },
  ];
}

export async function expectSheetStructure(
  page: Page, surface: 'gallery' | 'showcase', locale: 'vi' | 'en',
): Promise<void> {
  const root = page.getByTestId(`${surface}-filter-sheet`);
  const title = locale === 'vi' ? 'Bộ lọc' : 'Filters';
  const close = locale === 'vi' ? 'Đóng' : 'Close';
  const clear = locale === 'vi' ? 'Xóa bộ lọc' : 'Clear filters';
  const apply = surface === 'gallery'
    ? locale === 'vi'
      ? 'Xem 20 mẫu'
      : 'Show 20 templates'
    : locale === 'vi'
      ? 'Xem 30 CV'
      : 'Show 30 resumes';
  const message = 'Sheet accessible structure.';
  await expect(root, message).toMatchAriaSnapshot(
    `- dialog "${title}":\n  - heading "${title}" [level=2]\n`
    + `  - button "${close}"\n  - button "${clear}"\n`
    + `  - button "${apply}"`,
  );
  await expect(root.getByRole('heading'), message)
    .toHaveCount(surface === 'gallery' ? 3 : 1);
  await expect(root.getByRole('button'), message)
    .toHaveCount(surface === 'gallery' ? 13 : 17);
  const roles = root.getByTestId(`${surface}-roles`);
  await expect(roles, message).toHaveRole('group');
  await expect(roles, message).toHaveAccessibleName(locale === 'vi'
    ? 'Lọc theo vị trí'
    : 'Filter by role');
  const labels = [locale === 'vi' ? 'Mọi vị trí' : 'All roles',
    'Backend', 'Frontend', 'Mobile', 'DevOps/SRE', 'Data/AI', 'QA/Tester',
    'Fresher/Intern', 'BrSE', locale === 'vi' ? 'Bảo mật' : 'Security',
    ...(surface === 'showcase' ? [locale === 'vi' ? 'Khác' : 'Other'] : [])];
  const buttons = roles.getByRole('button');
  await expect(buttons, message).toHaveCount(labels.length);
  for (let index = 0; index < labels.length; index++) {
    await expect(buttons.nth(index), message)
      .toHaveAccessibleName(labels[index]!);
    await expect(buttons.nth(index), message)
      .toHaveAttribute('aria-pressed', String(index === 0));
  }
  await expect(roles, message).toMatchAriaSnapshot('- group:\n'
    + labels.map((label, index) =>
      `  - button "${label}"${index === 0 ? ' [pressed]' : ''}`).join('\n'));
  if (surface === 'gallery') {
    const styles = root.getByTestId('gallery-styles');
    await expect(styles, message).toHaveRole('navigation');
    await expect(styles, message).toHaveAccessibleName(locale === 'vi'
      ? 'Lọc theo phong cách'
      : 'Filter by style');
    await expect(styles.getByRole('list'), message).toHaveCount(1);
    await expect(styles.getByRole('listitem'), message).toHaveCount(8);
    const links = styles.getByRole('link');
    const names = locale === 'vi'
      ? ['Tất cả 20', 'Có CV mẫu 13', 'Chuẩn ATS', 'Một trang', 'Hợp với ảnh',
          'Mới tốt nghiệp', 'Kỹ thuật', 'Quản lý']
      : ['All 20', 'With a sample 13', 'ATS-friendly', 'One page',
          'Suits a photo', 'First job', 'Technical', 'Management'];
    await expect(links, message).toHaveCount(8);
    await expect(links, message).toHaveText(names);
    await expect(styles.locator('[aria-current="page"]'), message)
      .toHaveCount(1);
  } else {
    const languages = root.getByTestId('showcase-languages');
    const names = locale === 'vi'
      ? ['Mọi ngôn ngữ', 'Tiếng Việt', 'Tiếng Anh']
      : ['All languages', 'Vietnamese', 'English'];
    await expect(languages, message).toMatchAriaSnapshot('- group:\n'
      + names.map((label, index) =>
        `  - button "${label}"${index === 0 ? ' [pressed]' : ''}`)
        .join('\n'));
    await expect(languages.getByRole('button'), message).toHaveCount(3);
    await expect(root.getByRole('combobox'), message).toHaveCount(1);
    await expect(root.getByRole('combobox'), message).toHaveValue('');
  }
}
