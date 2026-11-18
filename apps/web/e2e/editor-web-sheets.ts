import { expect, type Page } from '@playwright/test';

import { DESK, rgb } from './public-sheets';

export const EDITOR_WEB_SHEETS = 'Editor Web sheets.';
const LIGHT_PAPER = '#faf6ec';

export async function expectEditorWebSheets(page: Page): Promise<void> {
  const pane = page.getByTestId('preview-scroll');
  const article = pane.locator('.resume-document.resume-sheets');
  await expect(article, EDITOR_WEB_SHEETS).toHaveCount(1);
  await expect(pane.locator('.preview-sheet'), EDITOR_WEB_SHEETS)
    .toHaveCount(0);
  await expect(article, EDITOR_WEB_SHEETS)
    .toHaveCSS('background-color', rgb(DESK.light));
  const sheets = article.locator(':scope > .resume-sheet');
  expect(await sheets.count(), EDITOR_WEB_SHEETS).toBeGreaterThan(1);
  for (const sheet of await sheets.all()) {
    await expect(sheet, EDITOR_WEB_SHEETS)
      .toHaveCSS('background-color', rgb(LIGHT_PAPER));
  }
  const measured = await article.evaluate((root) => {
    const pane = root.closest('[data-testid="preview-scroll"]')!;
    const paneBox = pane.getBoundingClientRect();
    const paneCss = getComputedStyle(pane);
    const box = root.getBoundingClientRect();
    const sheet = root.querySelector(':scope > .resume-sheet')!;
    const clipping: string[] = [];
    for (let node: Element | null = sheet; node !== null && node !== pane;
      node = node.parentElement) {
      const css = getComputedStyle(node);
      if (css.overflowX !== 'visible' || css.overflowY !== 'visible') {
        clipping.push(node.className);
      }
    }
    return {
      clipping,
      shadow: getComputedStyle(sheet).boxShadow,
      paneFill: paneCss.backgroundColor,
      fillsPane: Math.abs(box.left - paneBox.left) <= 1
        && Math.abs(box.right - paneBox.right) <= 1
        && Math.abs(box.top - paneBox.top) <= 1,
    };
  });
  expect(measured, EDITOR_WEB_SHEETS).toEqual({
    clipping: [],
    shadow: expect.stringContaining('0px 32px 60px -28px'),
    paneFill: 'rgba(0, 0, 0, 0)',
    fillsPane: true,
  });
}
