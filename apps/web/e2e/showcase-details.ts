import { expect, type Page } from '@playwright/test';

import { expectStyleRules, type StyleRule } from './screen-expectations';

export async function expectShowcaseHeading(
  page: Page, property: 'color' | 'font-family', theme: 'light' | 'dark',
): Promise<void> {
  const expected = property === 'font-family'
    ? '"Be Vietnam Pro", Inter, system-ui, sans-serif'
    : theme === 'light' ? 'rgb(16, 27, 63)' : 'rgb(243, 241, 236)';
  await expect(page.getByTestId('showcase-header').locator('h1'),
    `Showcase heading ${property}.`).toHaveCSS(property, expected);
}

export function showcaseType(width: number, empty: boolean): StyleRule[] {
  return [
    {
      selector: '[data-testid="showcase-header"] p',
      message: 'Showcase body typography',
      css: { 'font-size': width >= 1024 ? '16px' : '15px',
        'line-height': width >= 1024 ? '24.8px' : '22.5px',
        'font-weight': '400', 'letter-spacing': 'normal' },
    },
    ...(!empty
      ? [
          {
            selector: '.showcase-tile__language',
            message: 'Showcase language typography',
            css: { 'font-size': '14px', 'line-height': '21px',
              'font-weight': '400', 'letter-spacing': 'normal' },
          },
          {
            selector: '.showcase-tile__template',
            message: 'Showcase template typography',
            css: { 'font-size': '14px', 'line-height': '21px',
              'font-weight': '500', 'letter-spacing': 'normal' },
          },
          {
            selector: '.showcase-tile__role',
            message: 'Showcase role typography',
            css: { 'font-size': '12px', 'line-height': '18px',
              'font-weight': '600', 'letter-spacing': 'normal' },
          },
          {
            selector: '.showcase-tile__slug',
            message: 'Showcase slug typography',
            css: { 'font-size': '13px', 'line-height': '19.5px',
              'font-weight': '400', 'letter-spacing': 'normal' },
          },
        ]
      : []),
  ];
}

export async function expectShowcaseFlow(page: Page): Promise<void> {
  await expect.poll(() => page.evaluate(() => {
    const rect = (id: string) => document.querySelector(
      `[data-testid="showcase-${id}"]`,
    )!.getBoundingClientRect();
    const header = rect('header');
    const bar = rect('filter-bar');
    const results = rect('results');
    if (innerWidth < 1024) {
      if (bar.top < header.bottom - 1 || results.top < bar.bottom - 1) {
        return false;
      }
    } else {
      const rail = rect('filter-rail');
      if (bar.left < header.right - 1
        || results.top < Math.max(header.bottom, bar.bottom) - 1
        || rail.top < Math.max(header.bottom, bar.bottom) - 1
        || results.left < rail.right - 1) {
        return false;
      }
    }
    const children = Array.from(document.querySelector(
      '[data-testid="showcase-results"]',
    )!.children).filter((node) => getComputedStyle(node).position !== 'absolute'
      && getComputedStyle(node).display !== 'none');
    const boxes = children.map((node) => node.getBoundingClientRect());
    return boxes.every((box, index) => box.width > 0 && box.height > 0
      && (index === 0 || box.top >= boxes[index - 1]!.bottom - 1));
  }), 'Showcase flow separation.').toBe(true);
}

export async function expectShowcaseList(
  page: Page, locale: 'vi' | 'en',
): Promise<void> {
  const grid = page.locator('.showcase-grid');
  const message = 'Showcase accessible list.';
  await expect(grid, message).toHaveRole('list');
  await expect(grid, message).not.toHaveAttribute('aria-busy', 'true');
  await expect(grid.getByRole('listitem'), message).toHaveCount(13);
  await expect(grid.getByRole('link'), message).toHaveCount(25);
  await expect(grid, message).toMatchAriaSnapshot('- list:\n'
    + Array.from({ length: 12 }, () =>
      '  - listitem:\n    - link:\n      - img\n    - link').join('\n')
    + '\n  - listitem:\n    - heading [level=2]\n    - link');
  const roles = ['Backend', 'Frontend', '', 'DevOps/SRE', 'Data/AI',
    'QA/Tester', 'Fresher/Intern', locale === 'vi' ? 'Khác' : 'Other', ''];
  const languages = locale === 'vi'
    ? ['Tiếng Việt', 'Tiếng Anh', 'Ngôn ngữ khác']
    : ['Vietnamese', 'English', 'Other language'];
  const templates = ['Engineer Compact', 'One Page Tight',
    locale === 'vi' ? 'Thiết kế riêng' : 'Custom design', 'Nordic Muted',
    'Executive Band', 'Modern Sidebar'];
  const tiles = page.locator('[data-showcase-slug]');
  for (let index = 0; index < 12; index++) {
    const tile = tiles.nth(index);
    const name = [`Resume card ${index + 1}`, roles[index % roles.length],
      languages[index % languages.length], templates[index % templates.length]]
      .filter(Boolean).join(' ');
    await expect(tile, message).toHaveRole('listitem');
    await expect(tile.getByRole('link'), message).toHaveCount(2);
    await expect(tile.locator('[data-showcase-tile]'), message)
      .toHaveAccessibleName(name);
    await expect(tile.locator('[data-action="showcase-report"]'), message)
      .toHaveAccessibleName(locale === 'vi'
        ? `Báo cáo CV resume-${index} qua email`
        : `Report resume resume-${index} by email`);
  }
}

export async function expectShowcasePager(
  page: Page, locale: 'vi' | 'en', filtered: boolean,
): Promise<void> {
  const pager = page.getByTestId('showcase-pager');
  const message = 'Showcase pager structure.';
  const title = locale === 'vi' ? 'Trang 2/3' : 'Page 2 of 3';
  const previous = locale === 'vi' ? 'Trang trước' : 'Previous';
  const next = locale === 'vi' ? 'Trang sau' : 'Next';
  await expect(pager, message).toMatchAriaSnapshot(
    `- navigation "${title}":\n  - link "${previous}"\n`
    + `  - paragraph: ${title}\n  - link "${next}"`,
  );
  await expect(pager.getByRole('link'), message).toHaveCount(2);
  await expect(pager.getByRole('button'), message).toHaveCount(0);
  const query = filtered
    ? 'role=backend&lang=vi&template=engineer-compact&'
    : '';
  for (const [action, number] of [['previous', 1], ['next', 3]] as const) {
    const link = pager.locator(`[data-action="showcase-${action}"]`);
    await expect(link, message).toHaveRole('link');
    await expect(link, message).not.toHaveAttribute('aria-disabled', 'true');
    await expect(link, message).toHaveAttribute('href',
      number === 1
        ? filtered
          ? `/showcase?${query.slice(0, -1)}`
          : '/showcase'
        : `/showcase?${query}page=${number}`);
  }
}

export async function expectShowcaseDetails(
  page: Page, locale: 'vi' | 'en', width: number,
  empty: boolean, filtered: boolean,
): Promise<void> {
  await expectShowcaseFlow(page);
  await expectStyleRules(page, showcaseType(width, empty));
  if (!empty) {
    await expectShowcaseList(page, locale);
    await expectShowcasePager(page, locale, filtered);
  }
}
