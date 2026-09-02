import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

const css = (name: string): string =>
  readFileSync(`app/assets/css/${name}`, 'utf8');
const RENDERER_GUARD
  = ':not(:where(.resume-document,.paged-resume,'
    + '.resume-document *,.paged-resume *))';
const APP_GUARD = 'html[data-ui="app"] body';

function normalizeSelector(selector: string): string {
  return selector
    .replaceAll(/\s+/g, ' ')
    .replaceAll(/\s*([(),])\s*/g, '$1')
    .trim();
}

function splitSelectorList(selector: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < selector.length; index += 1) {
    const char = selector[index];
    if (char === '(' || char === '[') depth += 1;
    else if (char === ')' || char === ']') depth -= 1;
    else if (char === ',' && depth === 0) {
      parts.push(selector.slice(start, index).trim());
      start = index + 1;
    }
  }
  parts.push(selector.slice(start).trim());
  return parts;
}

function selectors(source: string): string[] {
  const clean = source.replaceAll(/\/\*[\s\S]*?\*\//g, '');
  const found: string[] = [];
  let boundary = 0;
  for (let index = 0; index < clean.length; index += 1) {
    if (clean[index] === '{') {
      const prelude = clean.slice(boundary, index).trim();
      if (prelude !== '' && !prelude.startsWith('@')) {
        found.push(...splitSelectorList(prelude));
      }
      boundary = index + 1;
    } else if (clean[index] === '}') {
      boundary = index + 1;
    }
  }
  return found;
}

function walk(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? walk(join(dir, entry.name))
      : [join(dir, entry.name)],
  );
}

describe('stylesheet contract (decisions U2)', () => {
  it('never loads Preflight', () => {
    expect(css('tailwind.css')).not.toMatch(/preflight/i);
    expect(css('tailwind.css')).toContain(
      '@import "tailwindcss/theme.css" layer(theme);',
    );
    expect(css('tailwind.css')).toContain(
      '@import "tailwindcss/utilities.css" layer(utilities)',
    );
  });

  it('declares the dark variant on the data-theme attribute', () => {
    expect(css('tailwind.css')).toContain(
      '@custom-variant dark (&:where([data-theme=\'dark\'], '
      + '[data-theme=\'dark\'] *));',
    );
  });

  it('guards every chrome reset rule', () => {
    for (const selector of selectors(css('base.css'))) {
      const normalized = normalizeSelector(selector);
      expect(normalized.startsWith(APP_GUARD), selector).toBe(true);
      if (normalized !== APP_GUARD) {
        expect(normalized, selector).toContain(RENDERER_GUARD);
      }
    }
  });

  it('defines tokens on the document root for both themes', () => {
    const theme = css('theme.css');
    expect(theme).toMatch(/:root\s*\{[^}]*--background:/);
    expect(theme).toMatch(/html\[data-theme='dark'\]\s*\{[^}]*--background:/);
    expect(theme).toContain('--color-background: var(--background);');
    expect(css('app.css')).not.toContain('--background:');
  });

  it('lists only the layered entry in nuxt css', () => {
    const config = readFileSync('nuxt.config.ts', 'utf8');
    expect(config).toContain('css: [\'~/assets/css/tailwind.css\'],');
    expect(config).not.toContain('fonts.css\'');
  });
});

describe('generated primitives', () => {
  it('import icons from the repository icon package only', () => {
    const files = walk('app/components/ui');
    expect(files.length).toBeGreaterThan(0);
    for (const file of files) {
      expect(readFileSync(file, 'utf8'), file).not.toContain(
        'lucide-vue-next',
      );
    }
  });
});
