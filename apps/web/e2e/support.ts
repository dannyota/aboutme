import { createCanvas, loadImage } from '@napi-rs/canvas';
import { expect, type Page, type TestInfo } from '@playwright/test';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

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

export async function waitForImages(page: Page): Promise<void> {
  await page.locator('img').evaluateAll(async (images) => {
    await Promise.all(images.map(async (image) => {
      if (!image.complete || image.naturalWidth === 0) await image.decode();
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
 * Puts a harness render (any fixture, `mode=continuous`) into the same print
 * state the production print worker gives its standalone page: the
 * `body.resume-print` class, which zeroes the on-screen page padding so only
 * the injected `@page` margin remains, and the `@page` rule itself, read back
 * from the geometry the renderer already resolved onto `.resume-document`'s
 * `--page-margin-x`/`--page-margin-y` custom properties. The harness only
 * wires this up for its own dedicated print fixtures (`print-fixtures.ts`),
 * so any other fixture that prints through the harness needs it done here.
 * Returns the `@page` CSS text to inject with `page.addStyleTag` before
 * `page.emulateMedia({ media: 'print' })` and `page.pdf(...)`.
 */
export async function preparePrintPage(page: Page): Promise<string> {
  const geometry = await page.evaluate(() => {
    document.body.classList.add('resume-print');
    const article = document.querySelector('.resume-document');
    const paper = document.querySelector('.harness-paper');
    if (article === null || paper === null) {
      throw new Error('Harness render did not produce a resume document.');
    }
    const style = getComputedStyle(article);
    return {
      widthPx: paper.getBoundingClientRect().width,
      marginXmm: Number.parseFloat(style.getPropertyValue('--page-margin-x')),
      marginYmm: Number.parseFloat(style.getPropertyValue('--page-margin-y')),
    };
  });
  const widthPx = Math.round(geometry.widthPx);
  if (widthPx !== 794 && widthPx !== 816) {
    throw new Error(`Unexpected page width ${geometry.widthPx}px.`);
  }
  const size = widthPx === 794 ? '210mm 297mm' : '8.5in 11in';
  const margin = `${geometry.marginYmm}mm ${geometry.marginXmm}mm`;
  return `@page {\n  size: ${size};\n  margin: ${margin};\n}`;
}

/**
 * Diffs `actual` against `expected` pixel by pixel, writing a red/white
 * diff image to `diffPath` when they differ. Baselines are pinned exactly
 * (DESIGN.md; ADR 0050): any difference fails the assertion below.
 */
export async function compareRaster(
  actual: Buffer,
  expected: Buffer,
  diffPath: string,
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
  expect(changed).toBe(0);
}

/**
 * Captures a full-page screenshot and either writes it as an update
 * candidate under `PLAYWRIGHT_RESULTS_DIR/candidate-baselines/baselines`
 * (`make web-e2e-update`) or compares it against the pinned baseline of the
 * same name in `apps/web/e2e/baselines`.
 */
export async function verifyScreenshot(
  page: Page,
  filename: string,
  testInfo: TestInfo,
): Promise<void> {
  const bytes = await page.screenshot({
    animations: 'disabled',
    caret: 'hide',
    fullPage: true,
    scale: 'css',
    type: 'png',
  });
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
  );
}
