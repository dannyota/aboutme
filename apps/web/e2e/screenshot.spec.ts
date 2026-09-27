import { expect, test } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import {
  CHROME_PIXEL_TOLERANCE,
  denyExternalRequests,
  FIXTURE_PHOTO_CROP_GEOMETRY,
  mockSignedInSession,
  mockSignedOutSession,
  photoCropGeometry,
  verifyScreenshot,
  waitForImages,
} from './support';

// The harness webServer's own origin (playwright.config.ts): cookies set
// before a goto must name it explicitly, since this file's baseURL differs
// from the app pages tested in chrome.spec.ts and verify.spec.ts.
const GUIDE_BASE_URL = 'http://127.0.0.1:20090';

interface ScreenshotCell {
  readonly align?: 'justify';
  readonly fixture: 'full' | 'vn-full';
  readonly mode: 'continuous' | 'paged';
  readonly name: string;
  readonly paper?: 'letter';
  readonly template: string;
}

const CELLS: readonly ScreenshotCell[] = [
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'classic-serif--vn-full--paged.png',
    template: 'classic-serif',
  },
  {
    align: 'justify',
    fixture: 'vn-full',
    mode: 'paged',
    name: 'classic-serif--vn-full--justify--paged.png',
    template: 'classic-serif',
  },
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'engineer-compact--vn-full--paged.png',
    template: 'engineer-compact',
  },
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'modern-sidebar--vn-full--paged.png',
    template: 'modern-sidebar',
  },
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'executive-band--vn-full--paged.png',
    template: 'executive-band',
  },
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'consulting-formal--vn-full--paged.png',
    template: 'consulting-formal',
  },
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'consulting-formal--vn-full--letter--paged.png',
    paper: 'letter',
    template: 'consulting-formal',
  },
  {
    fixture: 'vn-full',
    mode: 'paged',
    name: 'academic-dense--vn-full--paged.png',
    template: 'academic-dense',
  },
  {
    fixture: 'full',
    mode: 'continuous',
    name: 'modern-sidebar--full--continuous.png',
    template: 'modern-sidebar',
  },
];

const PAGE_GEOMETRY = {
  a4: { height: 1123, width: 794 },
  letter: { height: 1056, width: 816 },
} as const;

test.describe('renderer screenshot subset', () => {
  for (const cell of CELLS) {
    test(cell.name, async ({ page }, testInfo) => {
      const preset = TEMPLATES.find(({ id }) => id === cell.template);
      expect(preset, `unknown preset ${cell.template}`).toBeDefined();
      const geometry
        = PAGE_GEOMETRY[cell.paper ?? preset!.customization.pageFormat];
      await page.setViewportSize(geometry);
      const external = await denyExternalRequests(page);

      const response = await page.goto(
        '/_harness/render'
        + `?fixture=${cell.fixture}&template=${cell.template}`
        + `&mode=${cell.mode}`
        + (cell.align === undefined ? '' : `&align=${cell.align}`)
        + (cell.paper === undefined ? '' : `&paper=${cell.paper}`),
      );
      expect(response?.ok()).toBe(true);
      const harnessRoot = page.locator(
        '.harness-render[data-render-mode]',
      );
      await expect(harnessRoot).toHaveCount(1);
      await expect(harnessRoot).toHaveAttribute(
        'data-render-mode',
        cell.mode,
      );
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      if (cell.mode === 'paged') {
        await expect(
          page.locator('[data-pagination-settled="true"]'),
        ).toHaveCount(1);
      }
      await waitForImages(page);

      const paper = await page.locator('.harness-paper').boundingBox();
      expect(paper).not.toBeNull();
      expect(paper!.width).toBe(geometry.width);
      expect(paper!.height).toBeGreaterThanOrEqual(geometry.height);
      expect(external).toEqual([]);
      if (cell.template === 'executive-band') {
        const contactContrast = await page
          .locator('.resume-header .contact-chip')
          .first()
          .evaluate((element) => {
            const channels = (value: string): readonly number[] => {
              const match = value.match(/^rgba?\((\d+), (\d+), (\d+)/);
              if (match === null) throw new Error(`Unexpected color: ${value}`);
              return match.slice(1).map(Number);
            };
            const luminance = (value: string): number => {
              const linear = channels(value).map((channel) => {
                const encoded = channel / 255;
                return encoded <= 0.04045
                  ? encoded / 12.92
                  : ((encoded + 0.055) / 1.055) ** 2.4;
              });
              return 0.2126 * linear[0]!
                + 0.7152 * linear[1]!
                + 0.0722 * linear[2]!;
            };
            const foreground = luminance(getComputedStyle(element).color);
            const header = element.closest('.resume-header');
            if (header === null) throw new Error('Contact is outside header.');
            const background = luminance(
              getComputedStyle(header).backgroundColor,
            );
            return (Math.max(foreground, background) + 0.05)
              / (Math.min(foreground, background) + 0.05);
          });
        expect(contactContrast).toBeGreaterThanOrEqual(4.5);
      }
      expect(await photoCropGeometry(page))
        .toEqual(FIXTURE_PHOTO_CROP_GEOMETRY);
      await verifyScreenshot(page, cell.name, testInfo);
    });
  }
});

interface PublicCell {
  readonly name: string;
  readonly template: string;
  readonly width: number;
}

// The public page at a wide desktop and a phone (docs/design/web.md). The
// harness draws PublicResumeApp as the public render worker does.
const PUBLIC_CELLS: readonly PublicCell[] = [
  {
    name: 'public--classic-serif--2560.png',
    template: 'classic-serif',
    width: 2560,
  },
  {
    name: 'public--modern-sidebar--2560.png',
    template: 'modern-sidebar',
    width: 2560,
  },
  {
    name: 'public--modern-sidebar--390.png',
    template: 'modern-sidebar',
    width: 390,
  },
];

test.describe('public page measure', () => {
  for (const cell of PUBLIC_CELLS) {
    test(cell.name, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: cell.width, height: 900 });
      const external = await denyExternalRequests(page);
      const response = await page.goto(
        `/_harness/render?fixture=vn-full&template=${cell.template}`
        + '&mode=public',
      );
      expect(response?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      await waitForImages(page);
      expect(external).toEqual([]);

      const geometry = await page.evaluate(() => {
        const box = (selector: string) => {
          const element = document.querySelector(selector);
          if (element === null) throw new Error(`Missing ${selector}`);
          return element.getBoundingClientRect();
        };
        // Characters a full body line holds: the paragraph's width over the
        // average width of its characters.
        const paragraph = document.querySelector('.entry-body p');
        if (paragraph === null) throw new Error('Missing body paragraph');
        const range = document.createRange();
        range.selectNodeContents(paragraph);
        const rects = [...range.getClientRects()];
        const lines = new Set(rects.map((rect) => Math.round(rect.top))).size;
        const textWidth = rects.reduce((sum, rect) => sum + rect.width, 0);
        const averageCharacter
          = textWidth / (paragraph.textContent ?? '').length;
        const link = box('.public-download');
        const measure = box('.public-measure');
        return {
          page: box('.public-resume-page').width,
          measure: { left: measure.left, right: measure.right },
          charsPerLine: paragraph.getBoundingClientRect().width
            / averageCharacter,
          lines,
          linkRight: link.right,
          linkTop: link.top,
          articleTop: box('.resume-document').top,
        };
      });
      expect(geometry.page).toBe(cell.width);
      expect(geometry.linkTop).toBeLessThan(geometry.articleTop);
      if (cell.width === 390) {
        // Phones keep the full width.
        expect(geometry.measure.left).toBe(0);
        expect(geometry.measure.right).toBe(390);
        expect(geometry.linkRight).toBeLessThan(390);
      } else {
        // Centred, with the page background on both sides.
        const left = geometry.measure.left;
        const right = cell.width - geometry.measure.right;
        expect(left).toBeGreaterThan(400);
        expect(Math.abs(left - right)).toBeLessThanOrEqual(1);
        expect(geometry.lines).toBeGreaterThan(1);
        expect(geometry.charsPerLine).toBeGreaterThanOrEqual(88);
        expect(geometry.charsPerLine).toBeLessThanOrEqual(112);
      }
      await page.emulateMedia({ media: 'print' });
      await expect(page.locator('.public-toolbar')).toBeHidden();
      await page.emulateMedia({ media: 'screen' });
      await verifyScreenshot(page, cell.name, testInfo);
    });
  }
});

interface GuideCell {
  readonly name: string;
  readonly theme: 'light' | 'dark';
  readonly width: number;
}

// The MCP guide page (/guide/mcp), Vietnamese default locale, both themes,
// at phone and desktop measures (docs/design/mcp-guide.md, "Layout").
const GUIDE_CELLS: readonly GuideCell[] = [
  { name: 'guide--light--390.png', theme: 'light', width: 390 },
  { name: 'guide--light--1280.png', theme: 'light', width: 1280 },
  { name: 'guide--dark--390.png', theme: 'dark', width: 390 },
  { name: 'guide--dark--1280.png', theme: 'dark', width: 1280 },
];

test.describe('guide page pixel baselines', () => {
  for (const cell of GUIDE_CELLS) {
    test(cell.name, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: cell.width, height: 900 });
      await page.context().addCookies([
        { name: 'aboutme-locale', value: 'vi', url: GUIDE_BASE_URL },
        { name: 'aboutme-theme', value: cell.theme, url: GUIDE_BASE_URL },
      ]);
      await mockSignedOutSession(page);
      const external = await denyExternalRequests(page);

      const response = await page.goto('/guide/mcp');
      expect(response?.status()).toBe(200);
      // Chrome transitions must not be mid-flight at capture (chrome.spec.ts
      // carries the same reasoning).
      await page.addStyleTag({
        content: '*, *::before, *::after { transition: none !important; '
          + 'animation: none !important; }',
      });
      await expect(page.locator('[data-testid="guide-mcp-page"]'))
        .toBeVisible();
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);

      const settledHeight = await page.evaluate(
        () => document.documentElement.scrollHeight);
      await page.setViewportSize({ width: cell.width, height: settledHeight });
      const overflow = await page.evaluate(() =>
        document.documentElement.scrollWidth
        - document.documentElement.clientWidth);
      expect(overflow).toBe(0);
      expect(external).toEqual([]);

      await verifyScreenshot(
        page,
        cell.name,
        testInfo,
        undefined,
        CHROME_PIXEL_TOLERANCE,
      );
    });
  }
});

// The header must not overflow at 704, 768, or 1024px in either language,
// signed in or out, and the Connect AI link shows exactly where
// docs/design/mcp-guide.md, "Navigation", puts it: signed out, from 44rem
// (704px); signed in, from 64rem (1024px), because the signed-in bar also
// carries Resumes, Views, and Settings. /guide/mcp carries both states,
// since the page reads no API and renders the same content either way.
const HEADER_WIDTHS = [704, 768, 1024] as const;
const GUIDE_LINK_VISIBLE_AT: Record<'in' | 'out', readonly number[]> = {
  out: [704, 768, 1024],
  in: [1024],
};

for (const locale of ['vi', 'en'] as const) {
  for (const session of ['out', 'in'] as const) {
    for (const width of HEADER_WIDTHS) {
      const title = `guide header fits ${width}px signed ${session} `
        + `(${locale})`;
      test(title, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.context().addCookies([
          { name: 'aboutme-locale', value: locale, url: GUIDE_BASE_URL },
        ]);
        if (session === 'in') {
          await mockSignedInSession(page);
        } else {
          await mockSignedOutSession(page);
        }

        const response = await page.goto('/guide/mcp');
        expect(response?.status()).toBe(200);
        const shell = page.locator('[data-testid="app-shell"]');
        await expect(shell).toBeVisible();
        if (session === 'in') {
          // SSR renders the signed-out shell before hydration's own
          // GET /api/v1/me settles, so wait for the signed-in-only account
          // menu before measuring (support.ts mockSignedInSession).
          await expect(page.getByTestId('account-menu')).toBeVisible();
        }

        const overflow = await shell.evaluate((element) =>
          element.scrollWidth - element.clientWidth);
        expect(overflow).toBeLessThanOrEqual(0);

        const guideLink = page.locator('nav a[href="/guide/mcp"]');
        await expect(guideLink).toHaveCount(1);
        if (GUIDE_LINK_VISIBLE_AT[session].includes(width)) {
          await expect(guideLink).toBeVisible();
        } else {
          await expect(guideLink).toBeHidden();
        }
      });
    }
  }
}
