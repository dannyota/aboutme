import { expect, type Page } from '@playwright/test';

import { expectSvg, lucideExpectation } from './screen-paint';

export async function expectVerifyIcon(
  page: Page, state: 'verified' | 'unavailable', theme: 'light' | 'dark',
): Promise<void> {
  const svg = page.locator('.verify-mark svg');
  const stroke = state === 'verified'
    ? theme === 'light' ? 'rgb(255, 255, 255)' : 'rgb(20, 26, 46)'
    : theme === 'light' ? 'rgb(92, 97, 120)' : 'rgb(165, 171, 191)';
  const shield = [
    'M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 '
    + '13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 '
    + '0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z',
    'm9 12 2 2 4-4',
  ];
  const expected = state === 'verified'
    ? lucideExpectation(shield, stroke)
    : {
        viewBox: '0 0 24 24',
        primitives: [
          { selector: 'circle', attributes: { cx: '12', cy: '12', r: '10' },
            fill: 'none', stroke, width: '2px' },
          ...lucideExpectation(['M12 16v-4', 'M12 8h.01'], stroke).primitives,
        ],
      };
  await expectSvg(svg, expected, 'Verify status SVG.');
}

export async function expectVerifyStructure(
  page: Page, state: 'verified' | 'unavailable', locale: 'vi' | 'en',
  scope?: 'chain' | 'components' | 'limits',
): Promise<void> {
  if (!scope || scope === 'limits') {
    const limits = page.getByTestId('verify-limits');
    const heading = locale === 'vi'
      ? 'Trang này chứng minh gì'
      : 'What this proves';
    const labels = locale === 'vi'
      ? ['Chứng minh', 'Không chứng minh']
      : ['It proves', 'It does not prove'];
    const message = 'Verify limits structure.';
    await expect(limits, message).toHaveRole('region');
    await expect(limits, message).toHaveAccessibleName(heading);
    await expect(limits.getByRole('heading'), message).toHaveCount(3);
    await expect(limits.getByRole('heading', { level: 3 }), message)
      .toHaveText(labels);
    await expect(limits.getByRole('list'), message).toHaveCount(2);
    for (const [index, count] of [2, 3].entries()) {
      const list = limits.getByRole('list').nth(index);
      await expect(list, message).toMatchAriaSnapshot('- list:\n'
        + Array.from({ length: count }, () => '  - listitem').join('\n'));
      await expect(list.getByRole('listitem'), message).toHaveCount(count);
    }
    await expect(limits.getByRole('link'), message).toHaveCount(1);
    await expect(limits.getByRole('link'), message).toHaveAccessibleName(
      locale === 'vi'
        ? 'Danh sách giới hạn đầy đủ trên GitHub'
        : 'Full list of limits on GitHub');
  }
  if (state === 'unavailable') {
    return;
  }
  for (const [name, title, count] of [
    ['chain', locale === 'vi'
      ? 'Từ mã nguồn đến bản đang chạy'
      : 'From source to running', 4],
    ['components', locale === 'vi' ? 'Thành phần' : 'Components', 3],
  ] as const) {
    if (scope && scope !== name) {
      continue;
    }
    const region = page.getByTestId(`verify-${name}`);
    const message = `Verify ${name} structure.`;
    await expect(region, message).toHaveRole('region');
    await expect(region, message).toHaveAccessibleName(title);
    await expect(region.getByRole('heading'), message).toHaveCount(1);
    await expect(region.getByRole('list'), message).toHaveCount(1);
    await expect(region.getByRole('listitem'), message).toHaveCount(count);
    await expect(region, message).toMatchAriaSnapshot(
      `- region "${title}":\n  - heading "${title}" [level=2]\n  - list:\n`
      + Array.from({ length: count }, () => '    - listitem').join('\n'),
    );
    if (name === 'components') {
      await expect(region.getByRole('button'), message).toHaveCount(3);
      for (const component of ['server', 'web', 'caddy']) {
        await expect(region.getByRole('button', { name: locale === 'vi'
          ? `Sao chép mã băm của ${component}`
          : `Copy the ${component} digest`, exact: true }), message)
          .toHaveCount(1);
      }
    }
  }
}
