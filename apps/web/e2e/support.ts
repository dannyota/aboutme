import { expect, type Page } from '@playwright/test';

import type { ScreenContract } from './display-assertions';
import { expectSheetStructure, sheetType } from './filter-sheet-details';

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
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
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
    if (image === null) {
      throw new Error('Photo frame has no image.');
    }
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

export { assertGalleryDetail, assertLibrary } from './gallery-display';

/** Count live renders while each thumbnail is mounted in the viewport. */
export async function expectThumbnailRenders(
  page: Page, count: number,
): Promise<void> {
  const position = await page.evaluate(() => ({ x: scrollX, y: scrollY }));
  const frames = page.locator('[data-sheet-thumbnail]');
  await expect(frames).toHaveCount(count);
  try {
    for (const frame of await frames.all()) {
      await frame.scrollIntoViewIfNeeded();
      const render = frame.locator('[data-sheet-thumbnail-render]');
      await expect(render).toHaveCount(1);
      await expect(render.locator('h1')).toHaveCount(0);
      for (const image of await render.locator('img').all()) {
        await image.evaluate((node) => (node as HTMLImageElement).decode());
      }
      await page.evaluate(() => document.fonts.ready);
    }
  } finally {
    await page.evaluate(({ x, y }) => window.scrollTo(x, y), position);
  }
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
      : tab === 'ats'
        ? '[data-ats-text]'
        : 'figcaption'),
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
      `${root} :is(h2,h3,button,a,label):visible`
      + `:not([data-action="${surface}-filters-close"])`, `${root} > div`,
      `${root} h2:first-of-type`, title, theme),
    scrolling: [`${root} > .overflow-y-auto`],
    styles: sheetType(surface),
    structure: (page) => expectSheetStructure(page, surface, locale),
    controls: [[`${root} [data-action="${surface}-filters-close"]`,
      locale === 'vi' ? 'Đóng' : 'Close']],
    paint: [root, 'background-color', theme === 'light'
      ? 'rgb(255, 255, 255)'
      : 'rgb(26, 33, 56)'],
    copy: [apply, surface === 'gallery'
      ? locale === 'vi'
        ? 'Xem 20 mẫu'
        : 'Show 20 templates'
      : locale === 'vi'
        ? 'Xem 30 CV'
        : 'Show 30 resumes'],
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

export { expectRendererPageMembership } from './renderer-membership';
