#!/usr/bin/env node
// render-brand.mjs: render the brand art in docs/brand/src/*.html to the
// JPEGs and PNGs the README, GitHub, the site icons, and the site's Open
// Graph tags use.
//
// Each source names its own size and output in a meta tag:
//   <meta name="brand-render" content="width=1200;height=630;out=../x.jpg">
// `out` is relative to docs/brand/src. A `.jpg` renders opaque; a `.png`
// keeps a transparent background unless the source adds `opaque=1`. The art
// is plain HTML and CSS set in the vendored Be Vietnam Pro, so the tokens and
// the fonts match the product (docs/brand/README.md, ADR 0065).
//
// Usage, from apps/web:  node scripts/render-brand.mjs [name ...]
// With names (e.g. banner-dark og-image-vi), only those sources render.
// It uses the Playwright Chromium the e2e suite installs; set
// BRAND_CHROMIUM to a Chromium binary to use another one.
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { chromium } from '@playwright/test';

const here = dirname(fileURLToPath(import.meta.url));
const srcDir = resolve(here, '../../../docs/brand/src');
const only = new Set(process.argv.slice(2));

function renderSpec(html, file) {
  const match = /<meta name="brand-render" content="([^"]+)">/u.exec(html);
  if (match === null) throw new Error(`${file}: no brand-render meta tag`);
  const spec = Object.fromEntries(
    match[1].split(';').map((pair) => pair.split('=').map((s) => s.trim())),
  );
  const width = Number(spec.width);
  const height = Number(spec.height);
  if (!Number.isInteger(width) || !Number.isInteger(height) || !spec.out) {
    throw new Error(`${file}: brand-render needs width, height, and out`);
  }
  let type = null;
  if (spec.out.endsWith('.jpg')) type = 'jpeg';
  if (spec.out.endsWith('.png')) type = 'png';
  if (type === null) throw new Error(`${file}: out must be .jpg or .png`);
  return {
    width,
    height,
    type,
    transparent: type === 'png' && spec.opaque !== '1',
    out: resolve(srcDir, spec.out),
  };
}

const sources = readdirSync(srcDir)
  .filter((file) => file.endsWith('.html'))
  .filter((file) => only.size === 0 || only.has(file.replace(/\.html$/u, '')))
  .sort();
if (sources.length === 0) throw new Error('no brand sources matched');

const browser = await chromium.launch(
  process.env.BRAND_CHROMIUM
    ? { executablePath: process.env.BRAND_CHROMIUM }
    : {},
);
try {
  for (const file of sources) {
    const path = join(srcDir, file);
    const html = readFileSync(path, 'utf8');
    const { width, height, type, transparent, out } = renderSpec(html, file);
    const page = await browser.newPage({
      viewport: { width, height },
      deviceScaleFactor: 1,
    });
    await page.goto(pathToFileURL(path).href);
    if (html.includes('Be Vietnam Pro')) {
      // A missing font would silently fall back to Inter or system-ui.
      const loaded = await page.evaluate(async () => {
        await document.fonts.load('800 20px "Be Vietnam Pro"');
        return document.fonts.check('800 20px "Be Vietnam Pro"');
      });
      if (!loaded) throw new Error(`${file}: Be Vietnam Pro did not load`);
    }
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot(type === 'jpeg'
      ? { path: out, type, quality: 90 }
      : { path: out, type, omitBackground: transparent });
    await page.close();
    console.log(`${file} -> ${out} (${width} x ${height})`);
  }
} finally {
  await browser.close();
}
