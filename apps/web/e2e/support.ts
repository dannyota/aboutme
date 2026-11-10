import { createCanvas, loadImage } from '@napi-rs/canvas';
import {
  expect,
  type Locator,
  type Page,
  type TestInfo,
} from '@playwright/test';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import {
  expectScreen, galleryFontLoaded, measureGalleryDetail, measureGalleryGrid,
  measureTextContainment, measureVerticalFlow, type ScreenContract,
} from './display-assertions';

interface CspViolation {
  readonly blockedURI: string;
  readonly effectiveDirective: string;
  readonly sourceFile: string;
  readonly sample: string;
}

export interface CspProbe {
  readonly violations: () => Promise<readonly CspViolation[]>;
  readonly consoleErrors: readonly string[];
  readonly pageErrors: readonly string[];
}

/**
 * Installs listeners a CSP regression would trip: a real
 * `securitypolicyviolation` event, a console error, or an uncaught page
 * error. Violations reach Node through a page binding, so the probe keeps
 * those of every document the page loads, including one it has since
 * navigated away from. Call before the first `page.goto`, since
 * `addInitScript` only applies to documents created after it runs.
 */
export async function trackCsp(page: Page): Promise<CspProbe> {
  const violations: CspViolation[] = [];
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text());
  });
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.exposeFunction(
    '__reportCspViolation',
    (violation: CspViolation) => {
      violations.push(violation);
    },
  );
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (event) => {
      const report = (window as Window & {
        __reportCspViolation?: (violation: CspViolation) => Promise<void>;
      }).__reportCspViolation;
      void report?.({
        blockedURI: event.blockedURI,
        effectiveDirective: event.effectiveDirective,
        sourceFile: event.sourceFile,
        sample: event.sample.slice(0, 80),
      });
    });
  });
  return {
    violations: async () => {
      // Lets a binding call from a violation raised just before this check
      // reach Node first.
      await page.evaluate(() => undefined).catch(() => undefined);
      return [...violations];
    },
    consoleErrors,
    pageErrors,
  };
}

/**
 * Asserts a tracked page raised no CSP violation, uncaught error, or
 * unexpected console error. `allowedConsoleSubstrings` excuses console
 * errors this app produces on purpose and unrelated to CSP, matched by
 * substring; a real production visit while signed out already logs the
 * browser's own "Failed to load resource" message for the expected 401 from
 * `GET /api/v1/me` (app/composables/useAuth.ts), so a signed-out page test
 * allows that one.
 */
export async function expectCspClean(
  probe: CspProbe,
  allowedConsoleSubstrings: readonly string[] = [],
): Promise<void> {
  expect(await probe.violations()).toEqual([]);
  expect(probe.consoleErrors.filter(
    (message) => !allowedConsoleSubstrings.some((allowed) =>
      message.includes(allowed)),
  )).toEqual([]);
  expect(probe.pageErrors).toEqual([]);
}

/**
 * Fakes a signed-in `GET /api/v1/me` and `GET /api/v1/capabilities` for an
 * `/app/**` page in this build's live-backend-free "normal" e2e surface.
 * Every optional capability defaults closed, matching how a real deployment
 * degrades on a missing or malformed field
 * (app/composables/useCapabilities.ts).
 */
export async function mockSignedInSession(
  page: Page,
  capabilities: Record<string, unknown> = {},
): Promise<void> {
  await page.route('**/api/v1/me', async (route) => {
    await route.fulfill({
      status: 200,
      json: {
        data: {
          user: {
            id: 'csp-user',
            email: 'csp@example.invalid',
            name: 'CSP User',
            avatarKey: null,
            hasPassword: true,
          },
          csrfToken: 'csp-test-token',
          identities: [],
        },
      },
    });
  });
  await page.route('**/api/v1/capabilities', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { providerLogin: false, agentAccess: false, ...capabilities },
      }),
    });
  });
}

/**
 * Fakes a signed-out `GET /api/v1/me` (401, so `useAuth` resolves to
 * `anonymous` instead of retrying against a nonexistent backend) and
 * `GET /api/v1/capabilities` for a public page in this build's
 * live-backend-free "normal" e2e surface. Every page reads both once on
 * hydration regardless of its own auth requirement.
 */
export async function mockSignedOutSession(
  page: Page,
  capabilities: Record<string, unknown> = {},
): Promise<void> {
  await page.route('**/api/v1/me', async (route) => {
    await route.fulfill({
      status: 401,
      json: { error: { code: 'unauthenticated', message: 'not signed in' } },
    });
  });
  await page.route('**/api/v1/capabilities', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { providerLogin: false, agentAccess: false, ...capabilities },
      }),
    });
  });
}

export async function denyExternalRequests(page: Page): Promise<string[]> {
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

// Renderer baselines stay exact (ADR 0019). Chrome captures allow a few
// hundred pixels: Chromium's software raster anti-aliases rounded corners
// two ways between runs on a tall page, about 200 pixels. chrome.spec.ts and
// gallery.spec.ts share it so their chrome captures use one tolerance.
export const CHROME_PIXEL_TOLERANCE = 400;

// A loaded image is not yet a painted one: `complete` turns true when the
// bytes arrive, and an image marked `decoding="async"` (the library cards)
// may still be left out of the next frames until its decode finishes.
// decode() resolves only once the image is ready to paint, so every image
// waits for it, loaded or not.
export async function waitForImages(page: Page): Promise<void> {
  await page.locator('img').evaluateAll(async (images) => {
    await Promise.all(images.map(async (image) => {
      await image.decode();
      if (!image.complete || image.naturalWidth === 0) {
        throw new Error(`Image did not decode: ${image.currentSrc}`);
      }
    }));
  });
}

export interface PhotoCropGeometry {
  readonly scaleX: number;
  readonly scaleY: number;
  readonly left: number;
  readonly top: number;
  readonly overflow: string;
  readonly objectFit: string;
}

// The photo image's box relative to its frame, in frame units. The fixtures'
// crop {x: 0.1, y: 0.05, width: 0.8, height: 0.8} must scale the image by 1.25
// and shift it by -x/width and -y/height so the crop fills the frame.
export async function photoCropGeometry(
  page: Page,
): Promise<PhotoCropGeometry> {
  return page.locator('.resume-photo').first().evaluate((frame) => {
    const image = frame.querySelector('img');
    if (image === null) throw new Error('Photo frame has no image.');
    const outer = frame.getBoundingClientRect();
    const inner = image.getBoundingClientRect();
    return {
      scaleX: inner.width / outer.width,
      scaleY: inner.height / outer.height,
      left: (inner.left - outer.left) / outer.width,
      top: (inner.top - outer.top) / outer.height,
      overflow: getComputedStyle(frame).overflow,
      objectFit: getComputedStyle(image).objectFit,
    };
  });
}

export const FIXTURE_PHOTO_CROP_GEOMETRY: PhotoCropGeometry = {
  scaleX: 1.25,
  scaleY: 1.25,
  left: -0.125,
  top: -0.0625,
  overflow: 'hidden',
  objectFit: 'cover',
};

/**
 * Diffs `actual` against `expected` pixel by pixel, writing a red/white
 * diff image to `diffPath` when they differ. Baselines are pinned exactly
 * (DESIGN.md; ADR 0020): any difference fails the assertion below.
 */
async function compareRaster(
  actual: Buffer,
  expected: Buffer,
  diffPath: string,
  maxChangedPixels = 0,
): Promise<void> {
  const [actualImage, expectedImage] = await Promise.all([
    loadImage(actual),
    loadImage(expected),
  ]);
  expect(actualImage.width).toBe(expectedImage.width);
  expect(actualImage.height).toBe(expectedImage.height);
  const width = actualImage.width;
  const height = actualImage.height;
  const actualCanvas = createCanvas(width, height);
  const expectedCanvas = createCanvas(width, height);
  actualCanvas.getContext('2d').drawImage(actualImage, 0, 0);
  expectedCanvas.getContext('2d').drawImage(expectedImage, 0, 0);
  const actualPixels = actualCanvas
    .getContext('2d')
    .getImageData(0, 0, width, height).data;
  const expectedPixels = expectedCanvas
    .getContext('2d')
    .getImageData(0, 0, width, height).data;
  let changed = 0;
  const diffCanvas = createCanvas(width, height);
  const diffContext = diffCanvas.getContext('2d');
  const diff = diffContext.createImageData(width, height);
  for (let offset = 0; offset < actualPixels.length; offset += 4) {
    const differs = actualPixels[offset] !== expectedPixels[offset]
      || actualPixels[offset + 1] !== expectedPixels[offset + 1]
      || actualPixels[offset + 2] !== expectedPixels[offset + 2]
      || actualPixels[offset + 3] !== expectedPixels[offset + 3];
    if (differs) {
      changed += 1;
      diff.data.set([255, 0, 0, 255], offset);
    } else {
      diff.data.set([255, 255, 255, 255], offset);
    }
  }
  if (changed > 0) {
    diffContext.putImageData(diff, 0, 0);
    await writeFile(diffPath, diffCanvas.toBuffer('image/png'));
  }
  expect(changed).toBeLessThanOrEqual(maxChangedPixels);
}

/**
 * Captures a full-page screenshot, or an element screenshot when `locator`
 * is given, and either writes it as an update candidate under
 * `PLAYWRIGHT_RESULTS_DIR/candidate-baselines/baselines`
 * (`make web-e2e-update`) or compares it against the pinned baseline of the
 * same name in `apps/web/e2e/baselines`.
 */
export async function verifyScreenshot(
  page: Page,
  filename: string,
  testInfo: TestInfo,
  locator?: Locator,
  maxChangedPixels = 0,
  /** False captures only the viewport, as for a fixed overlay. */
  fullPage = true,
): Promise<void> {
  const options = {
    animations: 'disabled',
    caret: 'hide',
    scale: 'css',
    type: 'png',
  } as const;
  const bytes = locator === undefined
    ? await page.screenshot({ ...options, fullPage })
    : await locator.screenshot(options);
  await writeFile(testInfo.outputPath(filename), bytes);
  if (testInfo.config.updateSnapshots !== 'none') {
    const root = process.env.PLAYWRIGHT_RESULTS_DIR;
    if (root === undefined) {
      throw new Error('PLAYWRIGHT_RESULTS_DIR is required.');
    }
    const candidate = resolve(root, 'candidate-baselines/baselines', filename);
    await mkdir(resolve(candidate, '..'), { recursive: true });
    await writeFile(candidate, bytes);
    return;
  }
  const expected = await readFile(resolve(
    import.meta.dirname,
    'baselines',
    filename,
  ));
  await compareRaster(
    bytes,
    expected,
    testInfo.outputPath(filename.replace(/\.png$/, '-diff.png')),
    maxChangedPixels,
  );
}

export async function assertGalleryDetail(
  page: Page,
  locale: 'vi' | 'en',
  width: number,
  tab: 'page' | 'pdf' | 'ats',
  theme: 'light' | 'dark' = 'light',
): Promise<void> {
  await expectScreen(page, galleryDetailContract(locale, theme, tab));
  await expect.poll(() => galleryFontLoaded(page.locator('html'))).toBe(true);
  await expect(page.locator('.template-detail__info h1')).toHaveCSS(
    'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
  );
  const names = locale === 'vi'
    ? ['Trang CV', 'PDF', 'ATS đọc được gì']
    : ['Page', 'PDF', 'What an ATS reads'];
  const selected = tab === 'page' ? 0 : tab === 'pdf' ? 1 : 2;
  const tabs = page.getByRole('tab');
  await expect(tabs).toHaveCount(3);
  await expect(tabs).toHaveText(names);
  for (let index = 0; index < names.length; index += 1) {
    await expect(tabs.nth(index)).toHaveAttribute(
      'aria-selected', String(index === selected),
    );
  }
  await expect(page.getByRole('tablist')).toMatchAriaSnapshot(
    `- tablist "${locale === 'vi'
      ? 'Cách xem CV mẫu'
      : 'How to view the sample'}":\n`
      + names.map((name, index) =>
        `  - tab "${name}"${index === selected ? ' [selected]' : ''}`,
      ).join('\n'),
  );
  await expect.poll(
    () => measureGalleryDetail(page.locator('.template-detail__layout')),
    'Gallery detail geometry.',
  ).toEqual({ stacked: width < 900, columns: width >= 900, contained: true });
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1);
  if (tab === 'page') {
    const article = page.locator('.resume-document');
    await expect(article).toHaveCSS('font-family',
      '"Source Sans 3", "Noto Sans"');
    await expect(article).toHaveCSS('font-size', '15px');
    await expect(article).toHaveCSS('font-synthesis', 'none');
    await expect(article).toHaveCSS('color', 'rgb(31, 26, 23)');
    await expect(article.locator('.resume-header')).toHaveCSS(
      'background-color', 'rgb(255, 240, 235)');
    await expect.poll(() => page.evaluate(() =>
      ['Source Sans 3', 'Noto Sans'].every((family) =>
        Array.from(document.fonts).some((face) =>
          face.family.replaceAll('"', '') === family
          && face.status === 'loaded'))), 'Template document fonts.')
      .toBe(true);
    await expect(page.locator('.resume-document')).toContainText(
      'Đinh Công Khánh',
    );
    await expect(page.locator('.template-paper')).toHaveCSS(
      'background-color', 'rgb(255, 255, 255)',
    );
  } else if (tab === 'pdf') {
    const image = page.locator('img[data-pdf-page]');
    await expect(image).toHaveCount(1);
    await expect(image).toHaveAttribute(
      'src', `/templates/pages/executive-band-${locale}-p1.png`,
    );
    await expect(image).toHaveAttribute('alt', locale === 'vi'
      ? 'Trang 1/1 của CV mẫu Executive Band'
      : 'Page 1 of 1 of the Executive Band sample resume');
    await image.scrollIntoViewIfNeeded();
    await image.evaluate((element) => (element as HTMLImageElement).decode());
    await expect.poll(() => image.evaluate((element) => {
      const img = element as HTMLImageElement;
      const box = img.getBoundingClientRect();
      const sheet = img.closest('.template-paper')!.getBoundingClientRect();
      return img.naturalWidth === 1240 && img.naturalHeight === 1754
        && Math.abs(box.width - sheet.width) <= 1
        && Math.abs(box.width / box.height - 1240 / 1754) <= 0.001;
    })).toBe(true);
    await expect(page.locator('figcaption')).toHaveText(
      locale === 'vi' ? 'Trang 1/1' : 'Page 1 of 1',
    );
    await page.evaluate(() => window.scrollTo(0, 0));
  } else {
    const text = page.locator('[data-ats-text]');
    await expect(text).toBeVisible();
    await expect(text).toHaveCSS('white-space', 'pre-wrap');
    await expect(text).toHaveCSS('background-color', 'rgb(255, 255, 255)');
    await expect(text).toHaveCSS('color', 'rgb(23, 26, 24)');
    await expect.poll(() => measureTextContainment(text))
      .toEqual({ visible: true, contained: true });
    const content = (await text.innerText()).normalize('NFC');
    const terms = locale === 'vi'
      ? ['Vũ Đức Trung', 'TÓM TẮT', 'KINH NGHIỆM LÀM VIỆC', 'HỌC VẤN']
      : ['Lan Hoang', 'EXECUTIVE SUMMARY', 'EXPERIENCE', 'EDUCATION'];
    let previous = -1;
    for (const term of terms) {
      const position = content.indexOf(term);
      expect(position).toBeGreaterThan(previous);
      expect(content.indexOf(term, position + term.length)).toBe(-1);
      previous = position;
    }
  }
}

export async function assertLibrary(
  page: Page,
  locale: 'vi' | 'en',
  theme: 'light' | 'dark',
  width: number,
  filtered: boolean,
): Promise<void> {
  await prepareScreen(page);
  await expectScreen(page, libraryContract(locale, theme));
  await expect.poll(() => galleryFontLoaded(page.locator('html'))).toBe(true);
  await expect(page.getByTestId('gallery-header').getByRole('heading'))
    .toHaveCSS(
      'font-family', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
    );
  const grid = page.locator('.gallery-grid');
  const columns = width < 641 ? 2 : width < 900 ? 3 : width < 1180 ? 4 : 5;
  await expect.poll(() => measureGalleryGrid(grid), 'Library grid geometry.')
    .toEqual({
      columns,
      columnGap: width < 641 ? 14 : 28,
      rowGap: width < 641 ? 24 : 40,
      ordered: true,
    });
  if (width >= 1024) {
    const fields = page.locator('.gallery-desktop');
    await expect(fields).toBeVisible();
    await expect(page.locator('[data-action="gallery-filters-open"]'))
      .toBeHidden();
    await expect(fields.locator('.library-filters'))
      .toHaveCSS('row-gap', '12px');
    await expect.poll(() =>
      measureVerticalFlow(fields.locator('.library-filters > section')),
    ).toBe(true);
    await expect(fields.locator('[data-role="'
      + (filtered ? 'backend' : 'all') + '"]')).toHaveCSS(
      'background-color', theme === 'light'
        ? 'rgb(38, 64, 156)'
        : 'rgb(143, 166, 240)',
    );
  }
  const cards = page.locator('[data-template]:visible');
  await expect(cards).toHaveCount(filtered ? 1 : 20);
  if (filtered) {
    await expect(cards).toHaveAttribute('data-template', 'one-page-tight');
    await expect(cards).toHaveAttribute('href', '/templates/one-page-tight');
    await expect(page.getByTestId('gallery-count')).toHaveText(
      locale === 'vi' ? '1 mẫu' : '1 template',
    );
    if (width < 1024) {
      const chips = page.getByTestId('gallery-active-filters');
      await expect(chips.locator('[data-action="gallery-filter-remove"]'))
        .toHaveText(['Backend', locale === 'vi' ? 'Kỹ thuật' : 'Technical']);
      await expect.poll(() => measureTextContainment(chips))
        .toEqual({ visible: true, contained: true });
    }
  } else {
    const ids = [
      'one-page-tight', 'engineer-compact', 'creative-accent', 'nordic-muted',
      'elegant-serif-two', 'mono-print', 'minimal-air', 'international-lang',
      'consulting-formal', 'ats-plain', 'graduate-friendly', 'executive-band',
      'modern-sidebar', 'academic-dense', 'classic-serif', 'designer-tag',
      'editorial-wide', 'government-formal', 'high-contrast', 'startup-bold',
    ];
    await expect(cards).toHaveCount(ids.length);
    for (let index = 0; index < ids.length; index += 1) {
      await expect(cards.nth(index))
        .toHaveAttribute('data-template', ids[index]!);
      await expect(cards.nth(index)).toHaveAttribute(
        'href', `/templates/${ids[index]}`,
      );
    }
  }
  for (const card of await cards.all()) {
    await card.scrollIntoViewIfNeeded();
    const image = card.locator('img[data-page-image]');
    if (await image.count()) {
      const id = await card.getAttribute('data-template');
      await expect(image).toHaveAttribute(
        'src', `/templates/pages/${id}-${locale}-p1.png`,
      );
      await image.evaluate((node) => (node as HTMLImageElement).decode());
      await expect.poll(() => image.evaluate((node) => {
        const img = node as HTMLImageElement;
        const box = img.getBoundingClientRect();
        return img.naturalWidth === 1240 && img.naturalHeight === 1754
          && Math.abs(box.width / box.height - 1240 / 1754) <= 0.001;
      })).toBe(true);
    } else {
      const thumbnail = card.locator('[data-sheet-thumbnail]');
      await expect(thumbnail.locator('[data-sheet-thumbnail-render]'))
        .toHaveCount(1);
      await expect.poll(() => thumbnail.evaluate((node) => {
        const box = node.getBoundingClientRect();
        const content = node.querySelector('.scaled-sheet-content')!;
        const transform = new DOMMatrixReadOnly(
          getComputedStyle(content).transform,
        );
        return Math.abs(transform.a - box.width / 794) <= 0.001
          && Math.abs(box.width / box.height - 794 / 1123) <= 0.001;
      })).toBe(true);
    }
  }
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1);
}

/** Load lazy content without changing the captured viewport. */
export async function prepareScreen(page: Page): Promise<void> {
  const position = await page.evaluate(() => ({ x: scrollX, y: scrollY }));
  const frames = page.locator('[data-sheet-thumbnail]:visible');
  for (const frame of await frames.all()) {
    await frame.scrollIntoViewIfNeeded();
    await expect(frame.locator('[data-sheet-thumbnail-render]')).toHaveCount(1);
  }
  for (const item of await page.locator('img:visible').all()) {
    await item.scrollIntoViewIfNeeded();
    await item.evaluate((node) => (node as HTMLImageElement).decode());
    await expect.poll(() => item.evaluate((node) => {
      const image = node as HTMLImageElement;
      return image.complete && image.naturalWidth > 0
        && image.naturalHeight > 0;
    }), 'Visible image readiness.').toBe(true);
  }
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(({ x, y }) => window.scrollTo(x, y), position);
}

export function chromeScreenContract(
  root: string,
  text: string,
  flow: string,
  titleSelector: string,
  title: string,
  theme: 'light' | 'dark',
  image?: string,
): ScreenContract {
  return {
    root, text, flow,
    type: ['body', '"Be Vietnam Pro", Inter, system-ui, sans-serif',
      'Be Vietnam Pro'],
    paint: ['body', 'color', theme === 'light'
      ? 'rgb(16, 27, 63)'
      : 'rgb(243, 241, 236)'],
    copy: [titleSelector, title],
    aria: [titleSelector, `- heading "${title}" [level=1]`],
    image,
  };
}

export function libraryContract(locale: 'vi' | 'en', theme: 'light' | 'dark') {
  return chromeScreenContract('[data-testid="template-gallery"]',
    '[data-testid="gallery-header"] h1, [data-testid="gallery-header"] p, '
    + '.template-card:visible > span > span:first-child',
    '[data-testid="gallery-header"] h1, [data-testid="gallery-header"] p',
    '[data-testid="gallery-header"] h1',
    locale === 'vi' ? 'Thư viện' : 'Library', theme,
    'img[data-page-image]:visible');
}

export function galleryDetailContract(
  locale: 'vi' | 'en', theme: 'light' | 'dark', tab: 'page' | 'pdf' | 'ats',
) {
  return chromeScreenContract('[data-testid="template-detail"]',
    '.template-detail__info :is(h1,p,dt,dd), '
    + (tab === 'page'
      ? '.resume-document .entry-body p'
      : tab === 'ats' ? '[data-ats-text]' : 'figcaption'),
    '.template-detail__info > :not([hidden])',
    '.template-detail__info h1',
    tab === 'page' ? 'Creative Accent' : 'Executive Band', theme,
    tab === 'pdf' ? 'img[data-pdf-page]' : undefined);
}

export function filterSheetContract(
  surface: 'gallery' | 'showcase', locale: 'vi' | 'en', theme: 'light' | 'dark',
): ScreenContract {
  const root = `[data-testid="${surface}-filter-sheet"]`;
  const title = locale === 'vi' ? 'Bộ lọc' : 'Filters';
  const apply = `[data-action="${surface}-filters-apply"]`;
  return {
    ...chromeScreenContract(root,
      `${root} :is(h2,h3,button,a,label):visible`, `${root} > div`,
      `${root} h2:first-of-type`, title, theme),
    paint: [root, 'background-color', theme === 'light'
      ? 'rgb(255, 255, 255)'
      : 'rgb(26, 33, 56)'],
    copy: [apply, surface === 'gallery'
      ? locale === 'vi' ? 'Xem 20 mẫu' : 'Show 20 templates'
      : locale === 'vi' ? 'Xem 30 CV' : 'Show 30 resumes'],
    counts: [
      [`${root} [data-role]`, surface === 'gallery' ? 10 : 11],
      [`${root} [data-action="${surface}-filters-close"]`, 1],
      ...(surface === 'showcase'
        ? [[`${root} [data-lang]`, 3], [`${root} select`, 1]] as const
        : []),
    ],
    aria: [root, `- dialog "${title}"`],
  };
}

/** Page membership recorded from the accepted Vietnamese renderer captures. */
export async function expectRendererPageMembership(
  page: Page, template: string, justify = false, letter = false,
): Promise<void> {
  const pages = page.locator('.resume-page:not(.pagination-measurement)');
  const titles = [
    'Kỹ sư trưởng', 'Kỹ sư Công nghệ thông tin', 'Thiết kế hệ thống',
    'Lập trình TypeScript', 'Tiếng Việt', 'Tiếng Anh',
    'Chứng chỉ Kiến trúc phần mềm', 'Từ điển dữ liệu mở',
    'Giải thưởng Sáng tạo trẻ',
  ];
  const split = template === 'classic-serif'
    ? justify ? 5 : 6
    : template === 'executive-band'
      ? 6
      : template === 'consulting-formal' ? letter ? 7 : 8 : 8;
  const twoColumns = ['modern-sidebar', 'engineer-compact'].includes(template);
  await expect(pages, 'Renderer page count.').toHaveCount(twoColumns ? 1 : 2);
  if (twoColumns) {
    await expect(pages.locator('.entry-title'), 'Renderer page membership.')
      .toHaveCount(titles.length);
  } else {
    await expect(pages.nth(0).locator('.entry-title'),
      'Renderer page membership.')
      .toHaveText(titles.slice(0, split));
    await expect(pages.nth(1).locator('.entry-title'),
      'Renderer page membership.')
      .toHaveText(titles.slice(split));
  }
  await expect(pages.locator('.resume-name'), 'Renderer first-page header.')
    .toHaveCount(1);
  await expect(pages.nth(0).locator('.resume-name'),
    'Renderer first-page header.')
    .toHaveText('Nguyễn Thị Minh Châu');
  for (const root of await pages.all()) {
    await expect(root, 'Renderer page overflow.')
      .toHaveAttribute('data-page-overflow', 'false');
  }
}
