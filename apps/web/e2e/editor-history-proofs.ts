import { expect, test, type Page } from '@playwright/test';

import {
  chooseVersion,
  expectBarButtons,
  expectBarFit,
  expectBarWrap,
  expectCount,
  expectCurrentFirst,
  expectEditTab,
  expectHistoryTool,
  expectLineTruncation,
  expectOptionContainment,
  expectOptionLayout,
  expectPreviewTab,
  expectRailFit,
  expectRailGap,
  expectWideSelection,
  isNarrowHistory,
  openHistoryEditor,
  openHistoryPanel,
  type HistoryLocale,
} from './editor-history-details';
import {
  blurActive,
  confirmRestore,
  expectRestoreDialogFit,
  expectRestoreFocusReturn,
  expectRestoreFocusTrap,
  expectRestoreInitialFocus,
  expectRestoredBanner,
  restoreDialog,
} from './editor-history-focus';
import {
  applyStyleMutation, mutateAttribute, mutateText, mutationTest,
  proveMutation,
} from './style-mutation';

const CELLS: readonly (readonly [number, HistoryLocale])[] = [
  [320, 'vi'], [320, 'en'], [390, 'vi'], [390, 'en'], [1440, 'vi'],
  [1440, 'en'],
];

async function openPanel(
  page: Page, width: number, locale: HistoryLocale,
) {
  const harness = await openHistoryEditor(page, locale, width);
  await expectHistoryTool(page, locale, width, false);
  await openHistoryPanel(page, locale);
  return harness;
}

async function openDialog(page: Page, width: number, locale: HistoryLocale) {
  const harness = await openPanel(page, width, locale);
  await chooseVersion(page, locale);
  await page.locator('[data-action="history-restore"]').click();
  await expectRestoreInitialFocus(page, locale);
  return harness;
}

for (const [width, locale] of CELLS) {
  const name = `the editor History panel at ${width}px in ${locale}`;
  test.describe(name, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });

    test(`${name} keeps the rail, list, bar and focus rules`, async ({
      page,
    }) => {
      test.setTimeout(90_000);
      const harness = await openHistoryEditor(page, locale, width);
      await expectRailFit(page);
      await expectRailGap(page, width);
      await expectHistoryTool(page, locale, width, false);
      await openHistoryPanel(page, locale);
      await expectHistoryTool(page, locale, width, true);
      await expectRailFit(page);
      await expectCurrentFirst(page, locale);
      await expectCount(page, locale);
      await expectOptionLayout(page);
      await expectOptionContainment(page);
      await expectLineTruncation(page, locale);

      await chooseVersion(page, locale);
      await expectBarWrap(page, width);
      await expectBarFit(page);
      await expectBarButtons(page, width);
      if (isNarrowHistory(width)) {
        await expectPreviewTab(page, locale);
        await page.locator('[data-action="history-back"]').click();
        await expectEditTab(page, locale);
        await page.locator('[data-action="open-history"]').click();
        await chooseVersion(page, locale);
      } else {
        await expectWideSelection(page, locale);
      }

      await page.locator('[data-action="history-restore"]').click();
      await expectRestoreInitialFocus(page, locale);
      await expectRestoreDialogFit(page);
      await expectRestoreFocusTrap(page);
      await expectRestoreFocusReturn(page);
      await page.locator('[data-action="history-restore"]').click();
      await expectRestoreInitialFocus(page, locale);
      await confirmRestore(page);
      await expectRestoredBanner(
        page, locale, harness, isNarrowHistory(width),
      );
    });
  });
}

test.describe('editor History panel mutations', () => {
  mutationTest('the History rail rejects an 8px gap at 320px', async ({
    page,
  }) => {
    test.setTimeout(60_000);
    await openHistoryEditor(page, 'vi', 320);
    await proveMutation(() => expectRailGap(page, 320),
      () => applyStyleMutation(page.locator('[data-region="app-rail"]'),
        { gap: '8px' }), 'History rail gap.');
  });

  mutationTest('the History rail rejects a rail that overflows at 320px',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openHistoryEditor(page, 'en', 320);
      await proveMutation(() => expectRailFit(page),
        () => applyStyleMutation(page.locator('[data-region="app-rail"]'), {
          'padding-left': '60px', 'padding-right': '60px',
        }), 'History rail fit.');
    });

  mutationTest('the History tool rejects a 40px size', async ({ page }) => {
    test.setTimeout(60_000);
    await openHistoryEditor(page, 'en', 390);
    await proveMutation(() => expectHistoryTool(page, 'en', 390, false),
      () => applyStyleMutation(page.locator('[data-action="open-history"]'), {
        width: '40px', height: '40px',
      }), 'History tool size.');
  });

  mutationTest('the History tool rejects an unpressed state while open',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await proveMutation(() => expectHistoryTool(page, 'en', 390, true),
        () => mutateAttribute(page.locator('[data-action="open-history"]'),
          'aria-pressed', 'false'), 'History tool pressed.');
    });

  mutationTest('the History list rejects Current version moved last',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'vi');
      await proveMutation(() => expectCurrentFirst(page, 'vi'),
        () => applyStyleMutation(
          page.locator('[data-version-id="current"]'), { order: '99' }),
        'History current first.');
    });

  mutationTest('the History count rejects changed text', async ({ page }) => {
    test.setTimeout(60_000);
    await openPanel(page, 390, 'en');
    await proveMutation(() => expectCount(page, 'en'),
      () => mutateText(page.locator('[data-history-count]'), '12 / 50'),
      'History count.');
  });

  mutationTest('the History list rejects widened option spacing',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await proveMutation(() => expectOptionLayout(page),
        () => applyStyleMutation(page.locator('[role="listbox"]'),
          { gap: '12px' }), 'History option layout.');
    });

  mutationTest('the History list rejects an option that overflows',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'vi');
      await proveMutation(() => expectOptionContainment(page),
        () => applyStyleMutation(
          page.locator('[data-version-id="cp-2"] > span'),
          { 'min-width': 'max-content' }),
        'History option containment.');
    });

  mutationTest('the History list rejects a wrapped assistant line',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'vi');
      await proveMutation(() => expectLineTruncation(page, 'vi'),
        () => applyStyleMutation(page.locator(
          '[data-version-id="cp-2"] > span > span:nth-child(2)'),
        { 'white-space': 'normal' }), 'History line truncation.');
    });

  mutationTest('the History version bar rejects buttons that do not wrap',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await chooseVersion(page, 'en');
      await proveMutation(() => expectBarWrap(page, 390),
        () => applyStyleMutation(page.locator('[data-history-bar]'),
          { 'flex-wrap': 'nowrap' }), 'History version bar wrap.');
    });

  mutationTest('the History version bar rejects overflowing buttons',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await chooseVersion(page, 'en');
      await proveMutation(() => expectBarFit(page),
        () => applyStyleMutation(
          page.locator('[data-history-bar] > div:nth-child(2)'),
          { 'min-width': '600px' }), 'History version bar overflow.');
    });

  mutationTest('the History version bar rejects unwrappable labels at 320px',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 320, 'vi');
      await chooseVersion(page, 'vi');
      await proveMutation(() => expectBarFit(page),
        () => applyStyleMutation(
          page.locator('[data-action="history-back"], '
            + '[data-action="history-restore"]'),
          { 'white-space': 'nowrap' }), 'History version bar overflow.');
    });

  mutationTest('the History version bar rejects a 36px Delete at 390px',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await chooseVersion(page, 'en');
      await proveMutation(() => expectBarButtons(page, 390),
        () => applyStyleMutation(
          page.locator('[data-action="history-delete"]'),
          { width: '36px', height: '36px' }), 'History version bar buttons.');
    });

  mutationTest('the History Preview tab rejects an unselected tab',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await chooseVersion(page, 'en');
      await proveMutation(() => expectPreviewTab(page, 'en'),
        () => mutateAttribute(page.getByRole('tab', { name: 'Preview' }),
          'aria-selected', 'false'), 'History preview tab.');
    });

  mutationTest('the History Edit tab rejects an unselected tab after Back',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 390, 'en');
      await chooseVersion(page, 'en');
      await page.locator('[data-action="history-back"]').click();
      await proveMutation(() => expectEditTab(page, 'en'),
        () => mutateAttribute(page.getByRole('tab', { name: 'Edit' }),
          'aria-selected', 'false'), 'History edit tab.');
    });

  mutationTest('the History wide selection rejects an unselected option',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openPanel(page, 1440, 'en');
      await chooseVersion(page, 'en');
      await proveMutation(() => expectWideSelection(page, 'en'),
        () => mutateAttribute(page.locator('[data-version-id="cp-1"]'),
          'aria-selected', 'false'), 'History wide selection.');
    });

  mutationTest('the History restore dialog rejects a wider dialog',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openDialog(page, 320, 'vi');
      await proveMutation(() => expectRestoreDialogFit(page),
        () => applyStyleMutation(restoreDialog(page),
          { 'min-width': '600px' }), 'History restore dialog fit.');
    });

  mutationTest('the History restore dialog rejects initial focus off Cancel',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openDialog(page, 390, 'en');
      await proveMutation(() => expectRestoreInitialFocus(page, 'en'),
        async () => {
          await restoreDialog(page).locator('[data-action="restore-confirm"]')
            .focus();
          return async () => restoreDialog(page)
            .locator('[data-action="restore-cancel"]').focus();
        }, 'History restore initial focus.');
    });

  mutationTest('the History restore dialog rejects a missing tab stop',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openDialog(page, 390, 'vi');
      await proveMutation(() => expectRestoreFocusTrap(page),
        () => mutateAttribute(restoreDialog(page)
          .locator('[data-action="restore-confirm"]'), 'tabindex', '-1'),
        'History restore tab order.');
    });

  mutationTest('the History restore dialog rejects lost focus on close',
    async ({ page }) => {
      test.setTimeout(60_000);
      await openDialog(page, 390, 'en');
      const opener = page.locator('[data-action="history-restore"]');
      await restoreDialog(page).locator('[data-action="restore-cancel"]')
        .press('Enter');
      await expect(restoreDialog(page)).toBeHidden();
      await proveMutation(async () => {
        await expect(opener, 'History restore focus return.').toBeFocused();
      }, async () => {
        const restore = await blurActive(page);
        return restore;
      }, 'History restore focus return.');
    });

  mutationTest('the History restored banner rejects lost focus',
    async ({ page }) => {
      test.setTimeout(60_000);
      const harness = await openDialog(page, 390, 'en');
      await confirmRestore(page);
      await expectRestoredBanner(page, 'en', harness, true);
      await proveMutation(
        async () => {
          await expect(page.locator('[data-history-restored]'),
            'History restored banner focus.').toBeFocused();
        },
        () => blurActive(page), 'History restored banner focus.');
    });
});
