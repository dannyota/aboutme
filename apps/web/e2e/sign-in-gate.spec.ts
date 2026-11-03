import { expect, test, type Locator } from '@playwright/test';

import {
  measureTextContainment,
  measureVerticalFlow,
} from './display-assertions';
import { applyStyleMutation } from './style-mutation';

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

async function assertTextContained(text: Locator): Promise<void> {
  await expect.poll(() => measureTextContainment(text))
    .toEqual({ visible: true, contained: true });
}

async function assertFlow(items: Locator): Promise<void> {
  await expect.poll(() => measureVerticalFlow(items)).toBe(true);
}

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
      const labels = cell.providers.split(',').map((provider) => {
        const name = provider === 'google' ? 'Google' : 'LinkedIn';
        return cell.lng === 'vi'
          ? `Tiếp tục với ${name}`
          : `Continue with ${name}`;
      });
      await expect(page.locator('.gate-provider')).toHaveText(labels);
      await expect(page.locator('.gate-providers')).toMatchAriaSnapshot(
        labels.map((label) => `- link "${label}"`).join('\n'),
      );
      await expect(page.locator('.gate-text strong')).toHaveText(
        cell.lng === 'vi'
          ? 'Đăng nhập để xem CV này.'
          : 'Sign in to view this resume.',
      );
      if (cell.message === 'failed') {
        await expect(page.locator('.gate-message')).toHaveText(cell.lng === 'vi'
          ? 'Chúng tôi không thể xác minh đăng nhập của bạn. Vui lòng thử lại.'
          : 'We could not verify your sign-in. Please try again.');
      }
      await expect(page.locator('.resume-document')).toHaveCount(0);
      await expect(page.locator('.gate-card'))
        .toHaveCSS('background-color', 'rgb(255, 255, 255)');
      await expect(page.locator('.gate-text'))
        .toHaveCSS('color', 'rgb(16, 27, 63)');
      await expect(page.locator('.gate-provider').first())
        .toHaveCSS('height', '44px');
      await assertTextContained(page.locator('.gate-card'));
      await assertFlow(page.locator('.gate-provider'));
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

      // The entrance is a Web Animations API slide, which the screenshot's
      // animation-off option does not cover and which page.clock does not
      // drive. finish() jumps every running animation on the region to its
      // end state, so the geometry and the capture are the settled ones.
      await region.evaluate((element) => {
        for (const animation of element.getAnimations()) animation.finish();
      });

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

for (const kind of ['long text', 'nested clipping', 'overlap'] as const) {
  test(`display assertions reject ${kind}`, async ({ page }) => {
    await page.setContent(`
      <div id="text" style="width: 100px; height: 20px; overflow: hidden">
        <span>One line</span>
      </div>
      <div id="flow"><div style="height: 20px"></div>
        <div style="height: 20px"></div></div>
    `);
    const text = page.locator('#text');
    const siblings = page.locator('#flow > div');
    const assertClean = () => kind === 'overlap'
      ? assertFlow(siblings)
      : assertTextContained(text);
    await assertClean();
    const span = page.locator('#text span');
    if (kind !== 'overlap') {
      await span.evaluate((element, name) => {
        element.textContent = name === 'long text'
          ? 'A deliberately long unbroken line that cannot fit'
          : 'Nested clipping';
      }, kind);
    }
    const restore = kind === 'overlap'
      ? await applyStyleMutation(siblings.nth(1), { 'margin-top': '-4px' })
      : await applyStyleMutation(span, kind === 'long text'
          ? { 'white-space': 'nowrap' }
          : { display: 'inline-block', width: '20px', overflow: 'hidden' });
    await expect(assertClean()).rejects.toThrow();
    await restore();
    await page.setContent(`
      <div id="text" style="width: 100px; height: 20px; overflow: hidden">
        <span>One line</span>
      </div>
      <div id="flow"><div style="height: 20px"></div>
        <div style="height: 20px"></div></div>
    `);
    await assertClean();
  });
}
