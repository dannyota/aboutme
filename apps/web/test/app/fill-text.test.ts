import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { describe, expect, it } from 'vitest';

const appRoot = join(process.cwd(), 'app');
const FILL_AS_TEXT = /\b(?:text|fill|stroke)-(?:primary|brand)(?![\w-])/u;

function sources(directory: string): string[] {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.(?:vue|ts)$/u.test(name) ? [path] : [];
  });
}

describe('main fill tokens', () => {
  it('never color text or strokes', () => {
    const offenders = sources(appRoot)
      .filter((path) => FILL_AS_TEXT.test(readFileSync(path, 'utf8')))
      .map((path) => relative(appRoot, path));
    expect(offenders).toEqual([]);
  });
});
