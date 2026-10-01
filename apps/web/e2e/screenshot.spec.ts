import { expect, test, type Page } from '@playwright/test';
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
  // The viewer's emulated color preference, set before the page loads.
  readonly colorPreference?: 'dark' | 'light';
  readonly name: string;
  // The owner's color scheme (docs/design/public-page-theme.md); a light
  // page sets none.
  readonly scheme?: 'dark' | 'system';
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
  // The dark and Match device schemes (docs/design/public-page-theme.md,
  // "Baselines and tests"): a plain, a tinted-sidebar, and a tinted-band
  // template, plus a Match device page under a dark preference.
  {
    name: 'public--modern-sidebar--dark--390.png',
    scheme: 'dark',
    template: 'modern-sidebar',
    width: 390,
  },
  {
    name: 'public--creative-accent--dark--1440.png',
    scheme: 'dark',
    template: 'creative-accent',
    width: 1440,
  },
  {
    name: 'public--executive-band--dark--1440.png',
    scheme: 'dark',
    template: 'executive-band',
    width: 1440,
  },
  {
    colorPreference: 'dark',
    name: 'public--classic-serif--system--1440.png',
    scheme: 'system',
    template: 'classic-serif',
    width: 1440,
  },
];

test.describe('public page measure', () => {
  for (const cell of PUBLIC_CELLS) {
    test(cell.name, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: cell.width, height: 900 });
      const external = await denyExternalRequests(page);
      if (cell.colorPreference !== undefined) {
        await page.emulateMedia({ colorScheme: cell.colorPreference });
      }
      const response = await page.goto(
        `/_harness/render?fixture=vn-full&template=${cell.template}`
        + '&mode=public'
        + (cell.scheme === undefined ? '' : `&scheme=${cell.scheme}`),
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
          bar: box('.public-toolbar').width,
          measure: { left: measure.left, right: measure.right },
          charsPerLine: paragraph.getBoundingClientRect().width
            / averageCharacter,
          lines,
          linkRight: link.right,
          linkTop: link.top,
          linkHeight: link.height,
          articleTop: box('.resume-document').top,
        };
      });
      expect(geometry.page).toBe(cell.width);
      // The bar is the first child of .public-resume-page, full width,
      // above the resume article (docs/design/public-page-theme.md,
      // "Structure" and "Size and placement").
      expect(geometry.bar).toBe(cell.width);
      expect(geometry.linkTop).toBeLessThan(geometry.articleTop);
      // A fine pointer (the default here) keeps the download button at
      // 32 px high; a coarse pointer grows it to 40 px.
      expect(geometry.linkHeight).toBeLessThanOrEqual(32);
      if (cell.width === 390) {
        // Phones keep the full width.
        expect(geometry.measure.left).toBe(0);
        expect(geometry.measure.right).toBe(390);
        expect(geometry.linkRight).toBeLessThan(390);
      } else {
        // Centred, with the page background on both sides.
        const left = geometry.measure.left;
        const right = cell.width - geometry.measure.right;
        // The 400 px floor is tuned for 2560; a 1440 page has less room
        // beside the measure but still shows the page background on both sides.
        if (cell.width === 2560) expect(left).toBeGreaterThan(400);
        else expect(left).toBeGreaterThan(0);
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

// What a public page paints with its colors: the resume article, the header
// (a tinted band shows its own ground), and the page bar.
interface PublicSurfaces {
  readonly article: string;
  readonly bar: string;
  readonly header: string;
  readonly rootScheme: string;
  readonly schemeAttribute: string | null;
}

async function openPublic(
  page: Page,
  template: string,
  scheme: 'dark' | 'system' | undefined,
): Promise<void> {
  await page.setViewportSize({ width: 1280, height: 900 });
  const response = await page.goto(
    `/_harness/render?fixture=vn-full&template=${template}&mode=public`
    + (scheme === undefined ? '' : `&scheme=${scheme}`),
  );
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  // The public render worker's page loads no theme bootstrap, but every Nuxt
  // page head here does, and it writes an inline `color-scheme: light` on the
  // root. Drop it so the harness matches the public page.
  await page.evaluate(() => {
    document.documentElement.style.removeProperty('color-scheme');
  });
}

async function readSurfaces(page: Page): Promise<PublicSurfaces> {
  return page.evaluate(() => {
    const paint = (selector: string): string => {
      const element = document.querySelector(selector);
      if (element === null) throw new Error(`Missing ${selector}`);
      return getComputedStyle(element).backgroundColor;
    };
    return {
      article: paint('.resume-document'),
      bar: paint('.public-toolbar'),
      header: paint('.resume-header'),
      rootScheme: getComputedStyle(document.documentElement).colorScheme,
      schemeAttribute: document
        .querySelector('.public-resume-page')
        ?.getAttribute('data-color-scheme') ?? null,
    };
  });
}

// The scheme CSS is screen only and keys off the owner's choice plus, for
// Match device, the viewer's preference (docs/design/public-page-theme.md,
// "What stays light"). These checks compare computed colors across renders,
// so they need no baseline.
test.describe('public page color scheme', () => {
  for (const template of ['classic-serif', 'executive-band']) {
    test(`a dark ${template} page turns dark on screen and light in print`,
      async ({ page }) => {
        await openPublic(page, template, undefined);
        const light = await readSurfaces(page);
        expect(light.schemeAttribute).toBeNull();

        await openPublic(page, template, 'dark');
        const dark = await readSurfaces(page);
        expect(dark.schemeAttribute).toBe('dark');
        expect(dark.article).not.toBe(light.article);
        if (template === 'executive-band') {
          // A tinted band shows its own dark ground.
          expect(dark.header).not.toBe(light.header);
        }
        // The page bar takes its dark ground; the light page keeps the
        // light one.
        expect(light.bar).toBe('rgb(249, 248, 245)');
        expect(dark.bar).toBe('rgb(12, 16, 32)');
        expect(dark.rootScheme).toBe('dark');

        await page.emulateMedia({ media: 'print' });
        const printed = await readSurfaces(page);
        expect(printed.article).toBe(light.article);
        expect(printed.header).toBe(light.header);
        expect(printed.rootScheme).not.toBe('dark');
      });
  }

  test('a Match device page follows the viewer preference', async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: 'light' });
    await openPublic(page, 'classic-serif', undefined);
    const light = await readSurfaces(page);
    await openPublic(page, 'classic-serif', 'dark');
    const dark = await readSurfaces(page);

    await openPublic(page, 'classic-serif', 'system');
    const matchLight = await readSurfaces(page);
    expect(matchLight.schemeAttribute).toBe('system');
    expect(matchLight.article).toBe(light.article);
    expect(matchLight.bar).toBe(light.bar);
    expect(matchLight.rootScheme).not.toBe('dark');

    await page.emulateMedia({ colorScheme: 'dark' });
    const matchDark = await readSurfaces(page);
    expect(matchDark.article).toBe(dark.article);
    expect(matchDark.header).toBe(dark.header);
    expect(matchDark.bar).toBe(dark.bar);
    expect(matchDark.rootScheme).toBe('dark');

    // Print stays light whatever the viewer prefers.
    await page.emulateMedia({ media: 'print' });
    expect((await readSurfaces(page)).article).toBe(light.article);
  });

  test('a light page ignores a dark viewer preference', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' });
    await openPublic(page, 'classic-serif', undefined);
    const light = await readSurfaces(page);
    await page.emulateMedia({ colorScheme: 'dark' });
    const preferred = await readSurfaces(page);
    expect(preferred.schemeAttribute).toBeNull();
    expect(preferred.article).toBe(light.article);
    expect(preferred.bar).toBe(light.bar);
  });
});

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
