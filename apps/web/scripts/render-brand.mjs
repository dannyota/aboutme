#!/usr/bin/env node
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
