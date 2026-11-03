import { createCanvas, loadImage } from '@napi-rs/canvas';
import { chromium, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { isAbsolute, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { SAMPLES } from '../../../packages/schema/gen/ts/samples.ts';

import {
  assertCandidatePath,
  BROWSER_FLAGS,
  browserImage,
  PDF_FLAGS,
  pngDimensions,
  RASTER_DPI,
  REPOSITORY,
  sha256,
  sourceFingerprint,
  validatePages,
  type ManifestEntry,
} from './validate-template-pages.mts';

const ORIGIN = 'http://127.0.0.1:20090';
const WEB = join(REPOSITORY, 'apps/web');

function metadata(source: ReturnType<typeof sourceFingerprint>): string {
  const fields = [
    ['commit', process.env.TEMPLATE_PAGES_COMMIT],
    ['manifest_sha256', process.env.TEMPLATE_PAGES_MANIFEST_SHA256],
    ['tar_sha256', process.env.TEMPLATE_PAGES_TAR_SHA256],
  ];
  for (const [key, value] of fields) {
    const pattern = key === 'commit' ? /^[a-f0-9]{40}$/u : /^[a-f0-9]{64}$/u;
    if (value === undefined || !pattern.test(value)) {
      throw new Error('invalid source metadata');
    }
  }
  fields.push(
    ['source_sha256', source.sha256],
    ['browser_image_digest', source.browserDigest],
    ['pdf_flags_sha256', sha256(JSON.stringify(PDF_FLAGS))],
    ['raster_dpi', String(RASTER_DPI)],
  );
  return `${fields.map(([key, value]) => `${key}=${value}`).join('\n')}\n`;
}

async function startServer(): Promise<ChildProcess> {
  const server = spawn(process.execPath, ['.output/harness/server/index.mjs'], {
    cwd: WEB,
    env: {
      ...process.env,
      NITRO_HOST: '127.0.0.1',
      NITRO_PORT: '20090',
    },
    stdio: 'ignore',
  });
  let failed = false;
  server.on('error', () => {
    failed = true;
  });
  try {
    await expect.poll(async () => {
      if (failed || server.exitCode !== null || server.signalCode !== null) {
        throw new Error('harness server exited');
      }
      try {
        const response = await fetch(ORIGIN, {
          signal: AbortSignal.timeout(1000),
          redirect: 'error',
        });
        await response.body?.cancel();
        return response.ok;
      } catch {
        return false;
      }
    }, { timeout: 30_000 }).toBe(true);
    return server;
  } catch {
    await stopServer(server);
    throw new Error('harness server did not start');
  }
}

async function stopServer(server: ChildProcess): Promise<void> {
  if (server.exitCode !== null || server.signalCode !== null
    || server.pid === undefined) return;
  await new Promise<void>((done) => {
    const timer = setTimeout(() => server.kill('SIGKILL'), 5000);
    server.once('exit', () => {
      clearTimeout(timer);
      done();
    });
    server.kill('SIGTERM');
  });
}

export async function generatePages(directory: string): Promise<void> {
  assertCandidatePath(directory, REPOSITORY);
  if (process.platform !== 'linux' || process.arch !== 'x64'
    || process.env.TZ !== 'UTC' || process.env.LANG !== 'C.UTF-8'
    || process.env.LC_ALL !== 'C.UTF-8') {
    throw new Error('generation requires the pinned Linux environment');
  }
  browserImage(REPOSITORY);
  const source = sourceFingerprint(REPOSITORY);
  const sourceMetadata = metadata(source);
  const server = await startServer();
  try {
    const browser = await chromium.launch({ args: BROWSER_FLAGS });
    try {
      expect(browser.version()).toBe('156.0.8078.4');
      const context = await browser.newContext({
        colorScheme: 'light',
        deviceScaleFactor: 1,
        locale: 'en-US',
        reducedMotion: 'reduce',
        serviceWorkers: 'block',
        timezoneId: 'UTC',
        viewport: { height: 1123, width: 794 },
      });
      let denied = false;
      let failed = false;
      await context.route('**/*', async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== ORIGIN) {
          denied = true;
          await route.abort('blockedbyclient');
        } else {
          await route.continue();
        }
      });
      await context.routeWebSocket('**/*', (socket) => {
        denied = true;
        socket.close();
      });
      const page = await context.newPage();
      page.on('requestfailed', () => {
        failed = true;
      });
      page.on('pageerror', () => {
        failed = true;
      });
      page.on('response', (response) => {
        if (!response.ok()) failed = true;
      });
      page.setDefaultTimeout(20_000);
      await page.clock.setFixedTime(new Date('2026-01-01T00:00:00Z'));
      const entries: ManifestEntry[] = [];
      const images = new Map<string, Buffer>();
      for (const { templateId, lng } of SAMPLES) {
        const response = await page.goto(
          `${ORIGIN}/_harness/render?fixture=sample-${templateId}-${lng}`
          + `&template=${templateId}&mode=continuous&print=1`,
        );
        expect(response?.ok()).toBe(true);
        await expect(page.locator(
          '[data-render-mode="continuous"][data-fonts-ready="true"]',
        )).toHaveCount(1);
        await page.evaluate(async () => {
          await document.fonts.ready;
          if ([...document.fonts].some((font) => font.status === 'error')) {
            throw new Error('font failed');
          }
          await Promise.all(
            [...document.images].map((image) => image.decode()),
          );
        });
        expect(denied || failed).toBe(false);
        await page.emulateMedia({ media: 'print' });
        const pdf = await page.pdf({ ...PDF_FLAGS, timeout: 20_000 });
        if (pdf.length > 20 * 1024 * 1024) throw new Error('PDF size limit');
        const loading = getDocument({
          data: new Uint8Array(pdf),
          isImageDecoderSupported: false,
          isOffscreenCanvasSupported: false,
          useSystemFonts: false,
        });
        try {
          const document = await loading.promise;
          expect(document.numPages).toBe(1);
          const pdfPage = await document.getPage(1);
          const viewport = pdfPage.getViewport({ scale: RASTER_DPI / 72 });
          const width = Math.ceil(viewport.width);
          const height = Math.ceil(viewport.height);
          if (width > 2048 || height > 2048 || width * height > 4_000_000) {
            throw new Error('raster size limit');
          }
          const canvas = createCanvas(width, height);
          await pdfPage.render({
            canvas: canvas as unknown as HTMLCanvasElement,
            canvasContext: canvas.getContext('2d') as unknown as
              CanvasRenderingContext2D,
            intent: 'print',
            viewport,
          }).promise;
          const raw = canvas.toBuffer('image/png');
          pngDimensions(raw);
          // Decode and re-encode only bounded pixels into the deliverable.
          const clean = createCanvas(width, height);
          clean.getContext('2d').drawImage(await loadImage(raw), 0, 0);
          const png = clean.toBuffer('image/png');
          pngDimensions(png);
          const file = `${templateId}-${lng}-p1.png`;
          images.set(file, png);
          entries.push({
            templateId,
            lng,
            pages: 1,
            width,
            height,
            files: [file],
            sourceSha256: source.sha256,
            outputSha256: [sha256(png)],
          });
        } finally {
          await loading.destroy();
        }
      }
      expect(denied || failed).toBe(false);
      expect(sourceFingerprint(REPOSITORY).sha256).toBe(source.sha256);
      mkdirSync(directory, { mode: 0o700 });
      try {
        for (const [file, bytes] of images) {
          writeFileSync(join(directory, file), bytes, { flag: 'wx' });
        }
        writeFileSync(join(directory, 'manifest.json'),
          `${JSON.stringify(entries, null, 2)}\n`, { flag: 'wx' });
        writeFileSync(join(directory, 'source-metadata.txt'), sourceMetadata, {
          flag: 'wx',
        });
        validatePages(REPOSITORY, directory);
      } catch {
        rmSync(directory, { recursive: true, force: true });
        throw new Error('candidate validation failed');
      }
    } finally {
      await browser.close();
    }
  } finally {
    await stopServer(server);
  }
}

if (process.argv[1]
  && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  const timer = setTimeout(() => process.kill(process.pid, 'SIGTERM'), 600_000);
  try {
    if (process.argv.length !== 4 || process.argv[2] !== '--candidate'
      || !isAbsolute(process.argv[3]!)) throw new Error('invalid arguments');
    await generatePages(process.argv[3]!);
    console.log('template-pages: candidate validated');
  } catch {
    console.error('template-pages: generation failed');
    process.exitCode = 1;
  } finally {
    clearTimeout(timer);
  }
}
