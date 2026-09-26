import { createCanvas, loadImage } from '@napi-rs/canvas';
import {
  expect,
  type Locator,
  type Page,
  type TestInfo,
} from '@playwright/test';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

export interface CspViolation {
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

// Renderer baselines stay exact (ADR 0029). Chrome captures allow a few
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
 * (DESIGN.md; ADR 0050): any difference fails the assertion below.
 */
export async function compareRaster(
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
): Promise<void> {
  const options = {
    animations: 'disabled',
    caret: 'hide',
    scale: 'css',
    type: 'png',
  } as const;
  const bytes = locator === undefined
    ? await page.screenshot({ ...options, fullPage: true })
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
