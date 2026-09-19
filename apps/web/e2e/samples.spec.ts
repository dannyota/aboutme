import { expect, test, type Page } from '@playwright/test';
import { FILLER_LANGUAGES, SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';

import { SAMPLE_PAGES } from '../app/templates/samplePages';
import { denyExternalRequests } from './support';

// Gallery samples keep their page count in the pinned browser, so a spacing
// or header change that pushes a one-page sample onto a second page fails
// here. The generic filler stays within two pages under every template.

async function pageCount(page: Page, url: string): Promise<number> {
  const response = await page.goto(url);
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  await expect(page.locator('[data-pagination-settled="true"]'))
    .toHaveCount(1);
  return page.locator('[data-page-index]').count();
}

test('every gallery sample has a pinned page count', () => {
  expect(Object.keys(SAMPLE_PAGES).sort()).toEqual(
    [...new Set(SAMPLES.map(({ templateId }) => templateId))].sort(),
  );
});

for (const { templateId, lng } of SAMPLES) {
  test(`sample ${templateId} (${lng}) keeps its page count`, async ({
    page,
  }) => {
    const external = await denyExternalRequests(page);
    expect(await pageCount(
      page,
      `/_harness/render?fixture=sample-${templateId}-${lng}`
      + `&template=${templateId}&mode=paged`,
    )).toBe(SAMPLE_PAGES[templateId]);
    expect(external).toEqual([]);
  });
}

for (const lng of FILLER_LANGUAGES) {
  test(`filler (${lng}) stays within two pages`, async ({ page }) => {
    test.setTimeout(120_000);
    await denyExternalRequests(page);
    for (const { id } of TEMPLATES) {
      const pages = await pageCount(
        page,
        `/_harness/render?fixture=filler-${lng}&template=${id}&mode=paged`,
      );
      expect(pages, id).toBeLessThanOrEqual(2);
    }
  });
}
