import { expect, type Page } from '@playwright/test';

export const BAR_COLORS = {
  light: {
    ground: 'rgb(255, 255, 255)', border: 'rgb(227, 224, 242)',
    mark: 'rgb(222, 27, 90)', credit: 'rgb(85, 81, 125)',
    label: 'rgb(26, 22, 80)',
  },
  dark: {
    ground: 'rgb(14, 10, 13)', border: 'rgb(46, 36, 44)',
    mark: 'rgb(226, 120, 143)', credit: 'rgb(163, 149, 159)',
    label: 'rgb(211, 202, 208)',
  },
} as const;

const NO_FILL = 'rgba(0, 0, 0, 0)';

export async function expectBarPaint(
  page: Page, dark: boolean, message: string,
): Promise<void> {
  const color = BAR_COLORS[dark ? 'dark' : 'light'];
  const bar = page.locator('.public-toolbar');
  await expect(bar, message).toHaveCSS('background-color', NO_FILL);
  await expect(bar, message).toHaveCSS('border-bottom-style', 'none');
  await expect(bar, message).toHaveCSS('padding-top', '8px');
  await expect(bar, message).toHaveCSS('padding-bottom', '8px');
  await expect(page.locator('.public-mark'), message)
    .toHaveCSS('color', color.mark);
  await expect(page.locator('.public-credit'), message)
    .toHaveCSS('color', color.credit);
  const buttons = page.locator(
    '.public-toolbar :is(.public-download, .public-projects, .public-back)',
  );
  expect(await buttons.count(), message).toBeGreaterThan(0);
  for (const button of await buttons.all()) {
    await expect(button, message).toHaveCSS('color', color.label);
    await expect(button, message)
      .toHaveCSS('border-top-color', color.label);
    await expect(button, message).toHaveCSS('background-color', NO_FILL);
    await expect(button, message).toHaveCSS('box-shadow', 'none');
    await expect(button, message)
      .toHaveCSS('border-top-left-radius', '999px');
  }
}
