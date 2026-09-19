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

import {
  buildPrintAssets,
  publicScriptVersion,
  publicStyleVersion,
} from '../../server/utils/print/assets';

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

describe('public style version', () => {
  it('is 16 hex characters that change with either stylesheet', () => {
    const base = publicStyleVersion('a{}', 'b{}');
    expect(base).toMatch(/^[0-9a-f]{16}$/u);
    expect(publicStyleVersion('a{}', 'b{}')).toBe(base);
    expect(publicStyleVersion('a{color:red}', 'b{}')).not.toBe(base);
    expect(publicStyleVersion('a{}', 'b{color:red}')).not.toBe(base);
    // The separator keeps a byte moved across the boundary distinct.
    expect(publicStyleVersion('a{}b', '{}')).not.toBe(base);
  });
});

describe('public script version', () => {
  it('is 16 hex characters that change with the bundle', () => {
    const base = publicScriptVersion('export default 1;');
    expect(base).toMatch(/^[0-9a-f]{16}$/u);
    expect(publicScriptVersion('export default 1;')).toBe(base);
    expect(publicScriptVersion('export default 2;')).not.toBe(base);
  });
});
