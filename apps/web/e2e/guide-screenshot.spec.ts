import { expect, test, type Locator, type Page } from '@playwright/test';

import { measureVerticalFlow } from './display-assertions';
import {
  CHROME_PIXEL_TOLERANCE,
  denyExternalRequests,
  mockSignedInSession,
  mockSignedOutSession,
  verifyScreenshot,
  waitForImages,
} from './support';

const GUIDE_BASE_URL = 'http://127.0.0.1:20090';

async function assertGuideFlow(sections: Locator): Promise<void> {
  await expect.poll(() => measureVerticalFlow(sections)).toBe(true);
}

interface GuideCell {
  readonly locale: 'en' | 'vi';
  readonly name: string;
  readonly theme: 'light' | 'dark';
  readonly width: number;
}

// The MCP guide page (/guide/mcp), both locales and themes, at phone and
// desktop measures (docs/design/mcp-guide.md, "Layout").
const GUIDE_CELLS: readonly GuideCell[] = [
  { locale: 'vi', name: 'guide--light--390.png', theme: 'light', width: 390 },
  { locale: 'vi', name: 'guide--light--1280.png', theme: 'light', width: 1280 },
  { locale: 'vi', name: 'guide--dark--390.png', theme: 'dark', width: 390 },
  { locale: 'vi', name: 'guide--dark--1280.png', theme: 'dark', width: 1280 },
  {
    locale: 'en',
    name: 'guide--en--light--390.png',
    theme: 'light',
    width: 390,
  },
  {
    locale: 'en',
    name: 'guide--en--light--1280.png',
    theme: 'light',
    width: 1280,
  },
  {
    locale: 'en',
    name: 'guide--en--dark--390.png',
    theme: 'dark',
    width: 390,
  },
  {
    locale: 'en',
    name: 'guide--en--dark--1280.png',
    theme: 'dark',
    width: 1280,
  },
];

test.describe('guide page pixel baselines', () => {
  for (const cell of GUIDE_CELLS) {
    test(cell.name, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: cell.width, height: 900 });
      await page.context().addCookies([
        { name: 'aboutme-locale', value: cell.locale, url: GUIDE_BASE_URL },
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
      if (cell.locale === 'en') {
        await expect(page.getByRole('heading', {
          level: 1,
          name: 'Connect your AI assistant to aboutme.vn',
        })).toBeVisible();
      }
      await page.evaluate(() => document.fonts.ready);
      await waitForImages(page);

      const guide = page.getByTestId('guide-mcp-page');
      const title = cell.locale === 'vi'
        ? 'Kết nối trợ lý AI với aboutme.vn'
        : 'Connect your AI assistant to aboutme.vn';
      const headings = cell.locale === 'vi'
        ? [
            'Trợ lý AI làm được gì', 'Kết nối Claude', 'Thử một yêu cầu',
            'Quyền riêng tư và quyền kiểm soát', 'Ứng dụng khác',
            'Khắc phục sự cố',
          ]
        : [
            'What your assistant can do', 'Connect Claude', 'Try a request',
            'Privacy and control', 'Other apps', 'Troubleshooting',
          ];
      await expect(guide.getByRole('heading', { level: 1 }))
        .toHaveText(title);
      await expect(page.getByTestId('guide-header')).toMatchAriaSnapshot(
        `- heading "${title}" [level=1]`,
      );
      await expect(guide.getByRole('heading', { level: 2 }))
        .toHaveText(headings);
      await expect(page.getByTestId('guide-can').locator('li')).toHaveCount(5);
      await expect(page.getByTestId('guide-cannot').locator('li'))
        .toHaveCount(5);
      await expect(page.getByTestId('guide-claude-web-disk')).toHaveCount(6);
      await expect(page.getByTestId('guide-troubleshooting').locator('dt'))
        .toHaveCount(6);
      await assertGuideFlow(
        guide.locator(':scope > header, :scope > section'),
      );

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

test('guide flow detects overlapping sections', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await mockSignedOutSession(page);
  const response = await page.goto('/guide/mcp');
  expect(response?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  const sections = page.getByTestId('guide-mcp-page')
    .locator(':scope > header, :scope > section');
  const assertFlow = () => assertGuideFlow(sections);
  await assertFlow();
  const mutation = await page.addStyleTag({
    content: '[data-testid="guide-can-cannot"] '
      + '{ margin-top: -36px !important; }',
  });
  await expect(assertFlow()).rejects.toThrow();
  await mutation.evaluate((element) => element.remove());
  await assertFlow();
});

/**
 * Describes the shell and its direct children, the nav's, and the right-side
 * group's: tag, test id or text, computed display, and width in pixels.
 */
async function describeHeader(page: Page): Promise<string> {
  return page.evaluate(() => {
    const shell = document.querySelector<HTMLElement>(
      '[data-testid="app-shell"]',
    )!;
    const line = (element: Element): string => {
      const box = element.getBoundingClientRect();
      const name = element.getAttribute('data-testid')
        ?? (element.textContent ?? '').trim().replace(/\s+/gu, ' ')
          .slice(0, 24);
      const style = getComputedStyle(element);
      return `${element.tagName.toLowerCase()} "${name}" `
        + `display=${style.display} width=${box.width.toFixed(1)}`;
    };
    const rows = [
      `shell clientWidth=${shell.clientWidth} `
      + `scrollWidth=${shell.scrollWidth} `
      + `viewport=${document.documentElement.clientWidth}`,
    ];
    const walk = (parent: Element, label: string): void => {
      for (const child of parent.children) {
        rows.push(`${label}${line(child)}`);
      }
    };
    walk(shell, 'shell > ');
    const nav = shell.querySelector('nav');
    if (nav !== null) walk(nav, 'nav > ');
    const right = shell.querySelector(':scope > div');
    if (right !== null) walk(right, 'right > ');
    return `\n${rows.join('\n')}`;
  });
}

// The header must not overflow at 704, 768, 896, 1024, or 1152px in either
// language, signed in or out, and the Connect AI link shows exactly where
// docs/design/mcp-guide.md, "Navigation", puts it: signed out, from 44rem
// (704px); signed in, from 64rem (1024px), because the signed-in bar also
// carries Resumes, Views, and Settings. /guide/mcp carries both states,
// since the page reads no API and renders the same content either way.
const HEADER_WIDTHS = [704, 768, 896, 1024, 1152] as const;
const GUIDE_LINK_VISIBLE_AT: Record<'in' | 'out', readonly number[]> = {
  out: [704, 768, 896, 1024, 1152],
  in: [1024, 1152],
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
        // A failure lists every part of the header with its width, so the
        // message names what overflows without a trace artifact.
        expect(
          overflow,
          overflow > 0 ? await describeHeader(page) : '',
        ).toBeLessThanOrEqual(0);

        // The logo link never shrinks away, even in the compact header.
        const logo = await page.locator('[data-testid="app-shell"] > a')
          .first().boundingBox();
        expect(logo?.width ?? 0).toBeGreaterThan(0);

        // The Open source link shows signed out from 72rem (1152px), where
        // the full header fits with it (docs/design/ui/landing-and-library.md,
        // Navigation). The signed-in header never carries it.
        const openSource = page.getByTestId('app-shell-open-source');
        if (session === 'out' && width >= 1152) {
          await expect(openSource).toBeVisible();
        } else {
          await expect(openSource).toBeHidden();
        }

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
