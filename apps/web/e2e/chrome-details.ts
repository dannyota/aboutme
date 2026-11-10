import { expect, type Locator, type Page } from '@playwright/test';

/** Normalize a separately specified declaration, never a production token. */
async function expectDeclaration(
  target: Locator, property: string, value: string, message: string,
): Promise<void> {
  await expect.poll(() => target.evaluate((node, input) => {
    const reference = document.createElement('div');
    reference.style.setProperty(input.property, input.value, 'important');
    document.body.append(reference);
    const expected = getComputedStyle(reference)
      .getPropertyValue(input.property);
    reference.remove();
    return getComputedStyle(node).getPropertyValue(input.property) === expected;
  }, { property, value }), message).toBe(true);
}

export async function expectChromeDecoration(
  page: Page, theme: 'light' | 'dark', kind?: 'shadow' | 'gradient',
): Promise<void> {
  if (kind !== 'gradient') {
    await expectDeclaration(page.getByTestId('landing-sheet'), 'box-shadow',
      theme === 'light'
        ? '0 1px 2px rgba(23,26,24,0.06), 0 12px 32px rgba(23,26,24,0.1)'
        : '0 1px 2px rgba(0,0,0,0.4), 0 12px 32px rgba(0,0,0,0.5)',
      'Chrome paper shadow.');
  }
  if (kind !== 'shadow') {
    const colors = theme === 'light'
      ? ['15 124 110 / 0.16', '38 64 156 / 0.18', '143 111 13 / 0.16']
      : ['95 207 187 / 0.14', '143 166 240 / 0.22', '230 195 95 / 0.12'];
    await expectDeclaration(page.getByTestId('landing-glow'),
      'background-image',
      `radial-gradient(55% 40% at 22% 28%, rgb(${colors[0]}), `
      + 'transparent 70%), '
      + `radial-gradient(55% 42% at 78% 36%, rgb(${colors[1]}), `
      + 'transparent 70%), radial-gradient(60% 40% at 50% 82%, '
      + `rgb(${colors[2]}), transparent 70%)`, 'Chrome hero gradient.');
  }
}

export async function expectChromeNavigation(
  page: Page, locale: 'vi' | 'en', width: number, templates: boolean,
): Promise<void> {
  const nav = page.getByTestId('app-shell').getByRole('navigation');
  const names = locale === 'vi'
    ? ['Thư viện', 'Cộng đồng', 'Kết nối AI', 'Mã nguồn mở']
    : ['Library', 'Community', 'Connect AI', 'Open source'];
  const message = 'Chrome navigation structure.';
  await expect(nav, message).toHaveCount(1);
  await expect(nav, message).toHaveAccessibleName(locale === 'vi'
    ? 'Điều hướng chính'
    : 'Primary navigation');
  await expect(nav.locator('a'), message).toHaveCount(4);
  const visible = names.slice(0, width < 704 ? 2 : width < 1152 ? 3 : 4);
  await expect(nav.getByRole('link'), message).toHaveCount(visible.length);
  await expect(nav, message).toMatchAriaSnapshot('- navigation:\n'
    + visible.map((name) => `  - link "${name}"`).join('\n'));
  const links = ['/templates', '/showcase', '/guide/mcp',
    'https://github.com/dannyota/aboutme'];
  for (let index = 0; index < links.length; index++) {
    await expect(nav.locator('a').nth(index), message)
      .toHaveAttribute('href', links[index]!);
    if (templates && index === 0) {
      await expect(nav.locator('a').nth(index), message)
        .toHaveAttribute('aria-current', 'page');
    } else {
      await expect(nav.locator('a').nth(index), message)
        .not.toHaveAttribute('aria-current');
    }
  }
}

export async function expectChromeList(page: Page): Promise<void> {
  const grid = page.getByTestId('landing-template-grid');
  const message = 'Chrome template list structure.';
  await expect(grid, message).toMatchAriaSnapshot(
    '- list:\n' + Array.from({ length: 4 }, () =>
      '  - listitem:\n    - link').join('\n'),
  );
  await expect(grid.getByRole('listitem'), message).toHaveCount(4);
  await expect(grid.getByRole('link'), message).toHaveCount(4);
  for (const [index, name] of ['ATS Plain', 'Engineer Compact',
    'Graduate Friendly', 'Executive Band'].entries()) {
    await expect(grid.getByRole('link').nth(index), message)
      .toHaveAccessibleName(new RegExp(name, 'u'));
  }
}
