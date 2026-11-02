import { expect, test, type Locator, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import { measureTextContainment } from './display-assertions';

import {
  denyExternalRequests,
  FIXTURE_PHOTO_CROP_GEOMETRY,
  photoCropGeometry,
  verifyScreenshot,
  waitForImages,
} from './support';

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

async function assertRendererText(paragraph: Locator): Promise<void> {
  await expect.poll(() => measureTextContainment(paragraph)).toEqual({
    visible: true,
    contained: true,
  });
}

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
      const documents = page.locator(
        '.resume-document:not(.pagination-measurement)',
      );
      const name = cell.fixture === 'vn-full'
        ? 'Nguyễn Thị Minh Châu'
        : 'Ada Lovelace';
      const job = cell.fixture === 'vn-full'
        ? 'Kỹ sư trưởng'
        : 'Principal Engineer';
      await expect(documents.getByText(name, { exact: true })).toHaveCount(1);
      await expect(documents.getByText(name, { exact: true })).toBeVisible();
      await expect(documents.getByText(job, { exact: true })).toHaveCount(1);
      await expect(documents.getByText(job, { exact: true })).toBeVisible();
      const paragraphs = documents.locator('.entry-body p');
      expect(await paragraphs.count()).toBeGreaterThan(0);
      for (const paragraph of await paragraphs.all()) {
        await assertRendererText(paragraph);
      }
      await verifyScreenshot(page, cell.name, testInfo);
    });
  }
});

test('renderer text containment detects clipping', async ({ page }) => {
  const response = await page.goto(
    '/_harness/render?fixture=vn-full&template=modern-sidebar&mode=continuous',
  );
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const paragraph = page.locator('.resume-document .entry-body p').first();
  const assertContained = () => assertRendererText(paragraph);
  await assertContained();
  const mutation = await page.addStyleTag({
    content: '.entry-body p { height: 1px !important; '
      + 'overflow: hidden !important; }',
  });
  await expect(assertContained()).rejects.toThrow();
  await mutation.evaluate((element) => element.remove());
  await assertContained();
});

for (const [name, css] of [
  ['clipping', '.entry-body { height: 1px !important; '
  + 'overflow: hidden !important; }'],
  ['opacity', '.entry-body { opacity: 0 !important; }'],
] as const) {
  test(`renderer text containment detects ancestor ${name}`, async ({
    page,
  }) => {
    const response = await page.goto(
      '/_harness/render?fixture=vn-full&template=modern-sidebar'
      + '&mode=continuous',
    );
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const paragraph = page.locator('.resume-document .entry-body p').first();
    const assertContained = () => assertRendererText(paragraph);
    await assertContained();
    const mutation = await page.addStyleTag({ content: css });
    await expect(assertContained()).rejects.toThrow();
    await mutation.evaluate((element) => element.remove());
    await assertContained();
  });
}

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
  // Wide template margins on a phone: the text keeps its measure and the
  // padding drops to 16 px (docs/design/web.md, "Pagination and print").
  {
    name: 'public--classic-serif--390.png',
    template: 'classic-serif',
    width: 390,
  },
  {
    name: 'public--editorial-wide--390.png',
    template: 'editorial-wide',
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
        // The band depends on the template's typeface and is tuned on the
        // 2560 cells, not on width.
        if (cell.width === 2560) {
          expect(geometry.charsPerLine).toBeGreaterThanOrEqual(88);
          expect(geometry.charsPerLine).toBeLessThanOrEqual(112);
        }
      }
      await page.emulateMedia({ media: 'print' });
      await expect(page.locator('.public-toolbar')).toBeHidden();
      await page.emulateMedia({ media: 'screen' });
      const dark = cell.scheme === 'dark'
        || (cell.scheme === 'system' && cell.colorPreference === 'dark');
      await expect(page.locator('.public-toolbar')).toHaveCSS(
        'background-color',
        dark ? 'rgb(12, 16, 32)' : 'rgb(249, 248, 245)',
      );
      await expect(page.locator('.public-credit'))
        .toHaveText('Tạo bằng aboutme.vn');
      await expect(page.locator('.public-download')).toHaveText('Tải PDF');
      await expect(page.locator('.resume-name'))
        .toHaveText('Nguyễn Thị Minh Châu');
      const paragraphs = page.locator('.resume-document .entry-body p');
      expect(await paragraphs.count()).toBeGreaterThan(0);
      for (const paragraph of await paragraphs.all()) {
        await assertRendererText(paragraph);
      }
      await verifyScreenshot(page, cell.name, testInfo);
    });
  }
});

// The article's padding grows from 16 px toward the template margin as the
// width allows the measure (docs/design/web.md, "Pagination and print"; the
// page bar's inner row follows it, docs/design/public-page-theme.md).
test.describe('public page screen margins', () => {
  const MM_TO_PX = 96 / 25.4;
  // Template page margins, mm.
  const TEMPLATES_MARGIN_X = { 'classic-serif': 25, 'editorial-wide': 35 };

  for (const template of ['classic-serif', 'editorial-wide'] as const) {
    for (const width of [390, 768, 1024, 1440]) {
      test(`${template} at ${width}`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        const response = await page.goto(
          `/_harness/render?fixture=vn-full&template=${template}&mode=public`,
        );
        expect(response?.ok()).toBe(true);
        await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);

        const geometry = await page.evaluate(() => {
          const article = document.querySelector('.resume-document');
          const mark = document.querySelector('.public-mark');
          if (article === null || mark === null) throw new Error('Missing');
          const style = getComputedStyle(article);
          const left = Number.parseFloat(style.paddingLeft);
          const right = Number.parseFloat(style.paddingRight);
          const box = article.getBoundingClientRect();
          return {
            client: document.documentElement.clientWidth,
            fontSize: Number.parseFloat(style.fontSize),
            left,
            markLeft: mark.getBoundingClientRect().left,
            right,
            textLeft: box.left + left,
            text: box.width - left - right,
          };
        });
        const margin = TEMPLATES_MARGIN_X[template] * MM_TO_PX;
        // Padding is clamp(16, (width - measure) / 2, template margin).
        const expected = Math.min(
          margin,
          Math.max(16, (geometry.client - 52 * geometry.fontSize) / 2),
        );
        expect(geometry.left).toBeCloseTo(expected, 0);
        expect(geometry.right).toBeCloseTo(expected, 0);
        // The page bar's mark and the body text share one left edge.
        expect(geometry.markLeft).toBeCloseTo(geometry.textLeft, 0);
        if (width === 390) {
          expect(geometry.left).toBe(16);
          expect(geometry.text).toBeGreaterThanOrEqual(geometry.client - 32.5);
        }
        if (width === 1440) expect(geometry.left).toBeCloseTo(margin, 0);
      });
    }
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
