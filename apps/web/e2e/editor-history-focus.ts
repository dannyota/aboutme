import { expect, type Locator, type Page } from '@playwright/test';

import {
  HISTORY_COPY, type HistoryHarness, type HistoryLocale,
} from './editor-history-details';

export function restoreDialog(page: Page): Locator {
  return page.getByRole('alertdialog');
}

export async function expectRestoreInitialFocus(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  const dialog = restoreDialog(page);
  await expect(dialog, 'History restore dialog.').toBeVisible();
  await expect(dialog, 'History restore dialog.').toHaveAccessibleName(
    HISTORY_COPY[locale].restoreTitle);
  await expect(dialog.locator('[data-action="restore-cancel"]'),
    'History restore initial focus.').toBeFocused();
}

export async function expectRestoreDialogFit(page: Page): Promise<void> {
  const fit = await restoreDialog(page).evaluate((dialog) => {
    const box = dialog.getBoundingClientRect();
    const parts = Array.from(dialog.querySelectorAll('p, button, h2'));
    return {
      own: dialog.scrollWidth <= dialog.clientWidth + 1,
      width: box.width <= 512 + 1,
      viewport: box.left >= 0 && box.right <= innerWidth + 1
        && box.top >= 0 && box.bottom <= innerHeight + 1,
      parts: parts.every((part) => {
        const b = part.getBoundingClientRect();
        return b.left >= box.left - 1 && b.right <= box.right + 1;
      }),
      paragraphs: dialog.querySelectorAll('p').length,
    };
  });
  expect(fit, 'History restore dialog fit.').toEqual({
    own: true, width: true, viewport: true, parts: true, paragraphs: 6,
  });
}

export async function expectRestoreFocusTrap(page: Page): Promise<void> {
  const dialog = restoreDialog(page);
  const cancel = dialog.locator('[data-action="restore-cancel"]');
  const confirm = dialog.locator('[data-action="restore-confirm"]');
  await page.keyboard.press('Tab');
  await expect(confirm, 'History restore tab order.').toBeFocused();
  await page.keyboard.press('Tab');
  await expect(cancel, 'History restore tab order.').toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(confirm, 'History restore tab order.').toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(cancel, 'History restore tab order.').toBeFocused();
  for (let step = 0; step < 4; step += 1) {
    await page.keyboard.press('Tab');
    expect(await dialog.evaluate((node) =>
      node.contains(document.activeElement)),
    'History restore tab order.').toBe(true);
  }
  await expect(cancel, 'History restore tab order.').toBeFocused();
}

export async function expectRestoreFocusReturn(page: Page): Promise<void> {
  const dialog = restoreDialog(page);
  await dialog.locator('[data-action="restore-cancel"]').press('Enter');
  await expect(dialog, 'History restore focus return.').toBeHidden();
  await expect(page.locator('[data-action="history-restore"]'),
    'History restore focus return.').toBeFocused();
}

export async function confirmRestore(page: Page): Promise<void> {
  const dialog = restoreDialog(page);
  await page.keyboard.press('Tab');
  await expect(dialog.locator('[data-action="restore-confirm"]')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(dialog).toBeHidden();
}

export async function expectRestoredBanner(
  page: Page, locale: HistoryLocale, harness: HistoryHarness, narrow: boolean,
): Promise<void> {
  const banner = page.locator('[data-history-restored]');
  await expect(banner, 'History restored banner focus.').toBeVisible();
  await expect(banner, 'History restored banner focus.')
    .toContainText(HISTORY_COPY[locale].restored);
  expect(harness.restores.map((entry) => ({
    id: entry.id, ifMatch: entry.ifMatch, key: entry.key !== null
      && entry.key.length > 0,
  })), 'History restored banner focus.').toEqual([
    { id: 'cp-1', ifMatch: '"r1"', key: true },
  ]);
  await expect(page.locator('[data-version-id="current"]'),
    'History restored banner focus.').toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('[data-version-id]').nth(1).locator(
    ':scope > span > span:nth-child(2)'),
  'History restored banner focus.').toHaveText(
    HISTORY_COPY[locale].causeRestore);
  if (narrow) {
    await expect(page.locator('[data-region="inspector"]'),
      'History restored banner focus.')
      .toHaveAttribute('data-narrow-active', 'true');
  }
  await expect(banner, 'History restored banner focus.').toBeFocused();
}

export async function blurActive(page: Page): Promise<() => Promise<void>> {
  const handle = await page.evaluateHandle(() => document.activeElement);
  await page.evaluate(() => (document.activeElement as HTMLElement).blur());
  return async () => {
    await handle.evaluate((node) => (node as HTMLElement).focus());
    await handle.dispose();
  };
}
