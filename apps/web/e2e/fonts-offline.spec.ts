import { expect, test, type Page } from '@playwright/test';

import catalog from '../app/assets/fonts/catalog.json' with { type: 'json' };

interface CatalogEntry {
  readonly assets: readonly { readonly path: string }[];
  readonly cssFamily: string;
  readonly fallback: { readonly cssFamily: string; readonly id: string };
  readonly id: string;
}

const ENTRIES = catalog.entries as readonly CatalogEntry[];
const BY_ID = new Map(ENTRIES.map((entry) => [entry.id, entry]));

async function denyExternalRequests(page: Page): Promise<string[]> {
  const attempted: string[] = [];
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url());
    if (
      (url.protocol === 'http:' || url.protocol === 'https:')
      && url.hostname !== '127.0.0.1'
      && url.hostname !== 'localhost'
      && url.hostname !== '[::1]'
    ) {
      attempted.push(url.href);
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  });
  return attempted;
}

test.describe('offline font catalog', () => {
  expect(ENTRIES).toHaveLength(26);

  for (const entry of ENTRIES) {
    test(entry.id, async ({ page }) => {
      const external = await denyExternalRequests(page);
      const fontRequests = new Set<string>();
      page.on('request', (request) => {
        const url = new URL(request.url());
        if (url.pathname.endsWith('.woff2')) {
          fontRequests.add(url.pathname.split('/').at(-1)!);
        }
      });

      const response = await page.goto(
        '/_harness/render?fixture=vn-full&template=modern-sidebar'
        + `&mode=continuous&font=${entry.id}`,
      );
      expect(response?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);

      const fallback = BY_ID.get(entry.fallback.id);
      expect(fallback, `unknown fallback ${entry.fallback.id}`).toBeDefined();
      const families = [entry.cssFamily, fallback!.cssFamily];
      const unloaded = await page.evaluate((expectedFamilies) => {
        const relevant = [...document.fonts].filter((face) =>
          expectedFamilies.includes(face.family.replaceAll('"', '')),
        );
        return {
          count: relevant.length,
          failed: relevant
            .filter(({ status }) => status !== 'loaded')
            .map(({ family, status, weight }) => ({ family, status, weight })),
        };
      }, families);
      expect(unloaded.count).toBeGreaterThanOrEqual(families.length);
      expect(unloaded.failed).toEqual([]);

      const expectedAssets = new Set([
        ...entry.assets.map(({ path }) => path),
        ...fallback!.assets.map(({ path }) => path),
      ]);
      expect([...fontRequests].sort()).toEqual([...expectedAssets].sort());
      expect(external).toEqual([]);
    });
  }
});
