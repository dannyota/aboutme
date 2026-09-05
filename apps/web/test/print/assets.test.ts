// @vitest-environment node

import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { join } from 'node:path';

import { afterEach, describe, expect, it } from 'vitest';

import { buildPrintAssets } from '../../server/utils/print/assets';

const directories: string[] = [];

afterEach(() => {
  for (const directory of directories.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

describe('print static assets', () => {
  it('joins print CSS with the existing served asset directory', () => {
    const directory = mkdtempSync(join(process.cwd(), '.nuxt/print-assets-'));
    directories.push(directory);
    const assets = join(directory, 'assets');
    const fonts = join(directory, 'fonts');
    const printCSS = join(directory, 'worker.css');
    writeFileSync(printCSS, '.resume-document { color: black; }');
    mkdirSync(assets);
    writeFileSync(join(assets, 'public-resume.mjs'), 'export default true;');

    buildPrintAssets(assets, fonts, printCSS);

    expect(readFileSync(join(assets, 'public-resume.mjs'), 'utf8'))
      .toBe('export default true;');
    expect(readFileSync(join(assets, 'print.css'), 'utf8'))
      .toBe('.resume-document { color: black; }');
    expect(readFileSync(join(assets, 'print-fonts.css'), 'utf8'))
      .toContain('@font-face');
    expect(existsSync(join(fonts, 'inter-var.woff2'))).toBe(true);
    expect(existsSync(join(assets, 'assets'))).toBe(false);
  });
});
