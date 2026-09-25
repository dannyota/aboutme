import { expect, test } from '@playwright/test';

// The template gallery and template pages in the production build: no
// horizontal page scroll at phone or desktop width, the filter row scrolls on
// phones, a filter narrows the grid, the template page tabs switch views, and
// an unknown template is a 404 (DESIGN.md, template gallery).

for (const width of [390, 1440]) {
  test(`gallery at ${width} px fits the viewport`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.context().addCookies([{
      name: 'aboutme-locale',
      value: 'vi',
      url: 'http://127.0.0.1:20092',
    }]);
    const response = await page.goto('/templates');
    expect(response?.status()).toBe(200);
    await expect(page.locator('[data-template]')).toHaveCount(20);
    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
      - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
    const filters = page.locator('[data-testid="template-gallery"] nav');
    const scrolls = await filters.evaluate((element) =>
      element.scrollWidth > element.clientWidth);
    expect(scrolls).toBe(width === 390);

    await page.goto('/templates?filter=ats');
    await expect(page.locator('[data-template]')).toHaveCount(7);
    await expect(page.locator('[data-filter="ats"]'))
      .toHaveAttribute('aria-current', 'page');
  });
}

test('gallery header at 360 px fits the viewport in English', async ({
  page,
}) => {
  // The narrowest supported phone width; the English header labels are the
  // longer ones, so English is the tighter fit.
  await page.setViewportSize({ width: 360, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates');
  expect(response?.status()).toBe(200);
  const overflow = await page.evaluate(() =>
    document.documentElement.scrollWidth
    - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
  const header = await page.locator('[data-testid="app-shell"]').boundingBox();
  expect(header).not.toBeNull();
  if (header !== null) expect(header.x + header.width).toBeLessThanOrEqual(360);
});

test('template page switches between the page and the ATS text', async ({
  page,
}) => {
  await page.context().addCookies([{
    name: 'aboutme-locale',
    value: 'en',
    url: 'http://127.0.0.1:20092',
  }]);
  const response = await page.goto('/templates/engineer-compact');
  expect(response?.status()).toBe(200);
  await expect(page.locator('.template-detail__info h1'))
    .toHaveText('Engineer Compact');
  // The embedded sample's name renders as a p, not an h1, so the page
  // keeps exactly one h1: the template name.
  await expect(page.locator('h1')).toHaveCount(1);
  await expect(page.locator('.resume-document')).toHaveCount(1);
  await page.getByRole('tab', { name: 'What an ATS reads' }).click();
  await expect(page.locator('[data-ats-text]')).toContainText('Khoa Vu');
  await expect(page.locator('[data-action="use-sample"]'))
    .toHaveAttribute('href', '/app/new?sample=engineer-compact&lng=en');
});

test('an unknown template is not found', async ({ page }) => {
  const response = await page.goto('/templates/not-a-template');
  expect(response?.status()).toBe(404);
});
