import { expect, test } from '@playwright/test';

import {
  denyExternalRequests,
  verifyScreenshot,
  waitForImages,
} from './support';

// The sign-in gate page and the join invite, drawn by the renderer harness
// (docs/design/viewer-analytics/sign-in-to-view.md#gate and #join-invite;
// DESIGN.md, "Sign-in gate and join invite"). The gate is the public render
// worker's own component and inline style. The invite mounts client-side on
// the public page root, as public-resume.client.ts does.

type Lng = 'en' | 'vi';

interface GateCell {
  readonly lng: Lng;
  readonly message: 'none' | 'failed';
  readonly providers: 'google' | 'google,linkedin';
  readonly width: number;
}

const GATE_CELLS: readonly GateCell[] = [
  { lng: 'en', message: 'none', providers: 'google', width: 390 },
  { lng: 'en', message: 'none', providers: 'google', width: 1280 },
  { lng: 'vi', message: 'none', providers: 'google', width: 390 },
  { lng: 'vi', message: 'none', providers: 'google', width: 1280 },
  { lng: 'en', message: 'none', providers: 'google,linkedin', width: 390 },
  { lng: 'en', message: 'failed', providers: 'google', width: 390 },
  { lng: 'vi', message: 'failed', providers: 'google', width: 390 },
];

const gateName = (cell: GateCell): string =>
  `sign-in-gate--${cell.lng}--${cell.providers.replace(',', '-')}`
  + `--${cell.message}--${cell.width}.png`;

test.describe('sign-in gate', () => {
  for (const cell of GATE_CELLS) {
    test(gateName(cell), async ({ page }, testInfo) => {
      await page.setViewportSize({ width: cell.width, height: 800 });
      const external = await denyExternalRequests(page);
      const response = await page.goto(
        `/_harness/render?mode=gate&lng=${cell.lng}`
        + `&providers=${cell.providers}&message=${cell.message}`,
      );
      expect(response?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      expect(external).toEqual([]);

      await expect(page.locator('html')).toHaveAttribute('lang', cell.lng);
      await expect(page.locator('.gate-provider')).toHaveCount(
        cell.providers.split(',').length,
      );
      await expect(page.locator('.gate-message')).toHaveCount(
        cell.message === 'none' ? 0 : 1,
      );
      // Both languages fit the phone layout with no horizontal scroll, and
      // the card never exceeds the viewport (DESIGN.md, "Gate page").
      const fit = await page.evaluate(() => ({
        card: document.querySelector('.gate-card')!
          .getBoundingClientRect().right,
        scrollWidth: document.documentElement.scrollWidth,
        width: window.innerWidth,
      }));
      expect(fit.scrollWidth).toBeLessThanOrEqual(fit.width);
      expect(fit.card).toBeLessThanOrEqual(fit.width);
      await verifyScreenshot(page, gateName(cell), testInfo);
    });
  }
});

interface InviteCell {
  readonly lng: Lng;
  readonly placement: 'bar' | 'card';
  readonly width: number;
}

const INVITE_CELLS: readonly InviteCell[] = [
  { lng: 'en', placement: 'bar', width: 390 },
  { lng: 'vi', placement: 'bar', width: 390 },
  { lng: 'en', placement: 'bar', width: 1280 },
  { lng: 'vi', placement: 'bar', width: 1280 },
  { lng: 'en', placement: 'card', width: 1920 },
  { lng: 'vi', placement: 'card', width: 1920 },
];

const inviteName = (cell: InviteCell): string =>
  `join-invite--${cell.lng}--${cell.placement}--${cell.width}.png`;

test.describe('join invite', () => {
  for (const cell of INVITE_CELLS) {
    test(inviteName(cell), async ({ page }, testInfo) => {
      // Fake timers before the page loads: the 20 s dwell floor then runs
      // from the clock, not from the wall.
      await page.clock.install();
      await page.setViewportSize({ width: cell.width, height: 900 });
      const external = await denyExternalRequests(page);
      const response = await page.goto(
        `/_harness/render?fixture=${cell.lng === 'en' ? 'full' : 'vn-full'}`
        + '&template=classic-serif&mode=public&invite=register',
      );
      expect(response?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      await expect(
        page.locator('[data-invite-mounted="true"]'),
      ).toHaveCount(1);
      await waitForImages(page);
      expect(external).toEqual([]);
      await expect(page.locator('html')).toHaveAttribute('lang', cell.lng);

      const region = page.locator('.join-invite');
      await expect(region).toHaveCount(0);
      await page.clock.runFor(20_000);
      await expect(region).toBeVisible();
      await expect(region).toHaveAttribute('data-placement', cell.placement);
      await expect(region.getByRole('link')).toHaveAttribute(
        'href',
        '/register',
      );

      const box = await region.boundingBox();
      if (cell.placement === 'card') {
        // 320 px wide, 16 px from the right and bottom edges
        // (DESIGN.md, "Join invite").
        expect(box!.width).toBe(320);
        expect(cell.width - (box!.x + box!.width)).toBe(16);
        expect(900 - (box!.y + box!.height)).toBe(16);
      } else {
        expect(box!.x).toBe(0);
        expect(box!.width).toBe(cell.width);
        // A 56 px band; the test browser has no bottom safe area. The page
        // gains the same bottom padding while the bar shows.
        expect(box!.height).toBe(56);
        expect(
          await page.evaluate(
            () => getComputedStyle(document.body).paddingBottom,
          ),
        ).toBe('56px');
        expect(box!.y + box!.height).toBe(900);
      }
      await verifyScreenshot(page, inviteName(cell), testInfo, region);
    });
  }
});
