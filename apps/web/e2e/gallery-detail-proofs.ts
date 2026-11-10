import { expect, test } from '@playwright/test';

import { expectScreenClass } from './display-assertions';
import {
  expectGalleryOrder, GALLERY_PAGE_TYPE, GALLERY_ATS_TYPE,
} from './gallery-details';
import { styleProofs } from './screen-style-proofs';
import { applyStyleMutation, proveMutation } from './style-mutation';
import { galleryDetailContract } from './support';

export function galleryDetailProofs(): void {
  for (const tab of ['page', 'ats'] as const) {
    styleProofs(`gallery ${tab}`, async (page) => {
      await page.setViewportSize({ width: 390, height: 900 });
      await page.goto(`/templates/${tab === 'page'
        ? 'creative-accent'
        : 'executive-band'}`);
      if (tab === 'ats') {
        await page.getByRole('tab', { name: 'ATS đọc được gì' }).click();
      }
      await page.evaluate(() => document.fonts.ready);
    }, tab === 'page' ? GALLERY_PAGE_TYPE : GALLERY_ATS_TYPE);
  }

  test('gallery PDF rejects an invisible preview', async ({ page }) => {
    await page.goto('/templates/executive-band');
    await page.getByRole('tab', { name: 'PDF', exact: true }).click();
    await proveMutation(() => expectScreenClass(page,
      galleryDetailContract('vi', 'light', 'pdf'), 'images'),
    () => applyStyleMutation(page.locator('img[data-pdf-page]'), {
      opacity: '0',
    }),
    'Screen images.');
  });

  for (const selector of ['.section-heading', '.entry'] as const) {
    test(`gallery preview rejects reordered ${selector}`, async ({ page }) => {
      await page.goto('/templates/creative-accent');
      const assertion = () => expectGalleryOrder(page, 'vi');
      await assertion();
      const target = page.locator(selector).filter({
        has: page.locator(selector === '.entry' ? '.entry-title' : 'h2'),
      });
      const first = await target.first().innerHTML();
      const second = await target.nth(1).innerHTML();
      await target.first().evaluate((node, html) => {
        node.innerHTML = html;
      }, second);
      await target.nth(1).evaluate((node, html) => {
        node.innerHTML = html;
      }, first);
      expect(await target.first().innerHTML()).toBe(second);
      await expect(assertion()).rejects.toThrow(selector === '.entry'
        ? 'Gallery preview entry order.'
        : 'Gallery preview section order.');
    });
  }
}
