import { expect, type Locator, type Page } from '@playwright/test';

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
    const actual = getComputedStyle(node).getPropertyValue(input.property);
    const paintedShadows = (value: string) =>
      (value.match(/(?:[^,(]|\([^)]*\))+/gu) ?? [])
        .map((shadow) => shadow.trim())
        // Tailwind composes four transparent, zero-size ring placeholders.
        .filter((shadow) =>
          shadow !== 'rgba(0, 0, 0, 0) 0px 0px 0px 0px').join(', ');
    return input.property === 'box-shadow'
      ? paintedShadows(actual) === paintedShadows(expected)
      : actual === expected;
  }, { property, value }), message).toBe(true);
}

export async function expectChromeDecoration(
  page: Page, theme: 'light' | 'dark', kind?: 'shadow' | 'panels',
): Promise<void> {
  if (kind !== 'panels') {
    await expectDeclaration(page.getByTestId('landing-sheet'), 'box-shadow',
      '0 36px 56px -28px rgba(0,0,0,0.5), 0 2px 6px rgba(0,0,0,0.16)',
      'Chrome paper shadow.');
  }
  if (kind !== 'shadow') {
    const panels = theme === 'light'
      ? [['landing-hero', '#de1b5a'], ['landing-editor', '#f1effa'],
          ['landing-publish', '#1a1650'], ['landing-ai', '#f1effa'],
          ['landing-data', '#fff2c2'], ['landing-maker', '#ffe4ec'],
          ['landing-footer', '#1a1650']]
      : [['landing-hero', '#de1b5a'], ['landing-editor', '#191640'],
          ['landing-publish', '#322c8f'], ['landing-ai', '#191640'],
          ['landing-data', '#332a0e'], ['landing-maker', '#3b1431'],
          ['landing-footer', '#191640']];
    for (const [id, color] of panels) {
      await expectDeclaration(page.getByTestId(id!), 'background-color',
        color!, 'Chrome panel colors.');
    }
    await expectDeclaration(page.getByTestId('landing-hero'), 'color',
      '#ffffff', 'Chrome panel colors.');
    await expectDeclaration(page.getByTestId('landing-template-fan'),
      'background-color', theme === 'light' ? '#ffe4ec' : '#3b1431',
      'Chrome panel colors.');
  }
}

export async function expectChromeFan(page: Page): Promise<void> {
  const fan = page.getByTestId('landing-template-fan');
  await expect.poll(() => fan.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return Array.from(element.querySelectorAll('img'), (image) => {
      const matrix = new DOMMatrixReadOnly(getComputedStyle(image).transform);
      return {
        angle: Math.round(Math.atan2(matrix.b, matrix.a) * 180 / Math.PI),
        width: Math.abs(image.offsetWidth - box.width * 0.4) <= 1,
      };
    });
  }), 'Chrome fan geometry.').toEqual([-9, -3, 3, 9].map((angle) =>
    ({ angle, width: true })));
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
  const list = page.getByTestId('landing-template-list');
  const message = 'Chrome template list structure.';
  await expect(list, message).toMatchAriaSnapshot(
    '- list:\n' + Array.from({ length: 4 }, () =>
      '  - listitem:\n    - link').join('\n'),
  );
  await expect(list.getByRole('listitem'), message).toHaveCount(4);
  await expect(list.getByRole('link'), message).toHaveCount(4);
  for (const [index, [id, name]] of [
    ['ats-plain', 'ATS Plain'], ['engineer-compact', 'Engineer Compact'],
    ['graduate-friendly', 'Graduate Friendly'],
    ['executive-band', 'Executive Band'],
  ].entries()) {
    const link = list.getByRole('link').nth(index);
    await expect(link, message).toHaveAccessibleName(new RegExp(name!, 'u'));
    await expect(link, message).toHaveAttribute('href', `/templates/${id}`);
  }
}
