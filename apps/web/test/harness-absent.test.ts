// @vitest-environment node

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, expect, it } from 'vitest';

const webRoot = resolve(import.meta.dirname, '..');
const nuxtBin = resolve(webRoot, 'node_modules/nuxt/bin/nuxt.mjs');
const outputRoot = resolve(webRoot, '.output');

function build(harness: boolean): void {
  const env = { ...process.env };
  if (harness) env.NUXT_HARNESS = '1';
  else delete env.NUXT_HARNESS;

  const result = spawnSync(process.execPath, [nuxtBin, 'build'], {
    cwd: webRoot,
    encoding: 'utf8',
    env,
    maxBuffer: 64 * 1024 * 1024,
  });
  expect(
    result.status,
    [result.stdout, result.stderr].filter(Boolean).join('\n'),
  ).toBe(0);
}

function readTextTree(root: string): string {
  const contents: string[] = [];
  const walk = (directory: string): void => {
    for (const name of readdirSync(directory)) {
      const path = join(directory, name);
      if (statSync(path).isDirectory()) {
        walk(path);
      } else if (/\.(?:css|html|js|json|mjs)$/.test(name)) {
        contents.push(readFileSync(path, 'utf8'));
      }
    }
  };
  walk(root);
  return contents.join('\n');
}

function readApplicationBundles(root: string): string {
  return [
    resolve(root, 'public/_nuxt'),
    resolve(root, 'server/chunks/build'),
    resolve(root, 'server/chunks/virtual'),
  ]
    .filter(existsSync)
    .map(readTextTree)
    .join('\n');
}

describe('build-only renderer harness', () => {
  it(
    'is present only in the isolated harness build',
    { timeout: 300_000 },
    () => {
      build(true);

      const harnessServer = resolve(outputRoot, 'harness/server');
      expect(existsSync(join(harnessServer, 'index.mjs'))).toBe(true);
      const harnessOutput = resolve(outputRoot, 'harness');
      const harnessBytes = readApplicationBundles(harnessOutput);
      const harnessRoutes = readFileSync(
        resolve(harnessServer, 'chunks/virtual/entry.mjs'),
        'utf8',
      );
      expect(harnessBytes).toContain('/_harness/render');
      expect(harnessBytes).toContain(
        'aa570fab912ce9b49b805b07decf274178b3c0092b983c1a1127bed3b213252e',
      );
      expect(harnessBytes).toContain('print-sidebar-overflow');
      expect(harnessBytes).toContain('print-main-overflow');
      expect(harnessBytes).toContain('js-scheme-bare');
      expect(harnessRoutes).toContain('path: "/_harness/render"');
      expect(harnessRoutes).not.toContain('path: "/_harness/photo-fixture"');
      expect(harnessRoutes).not.toContain('path: "/_harness/print-fixtures"');

      build(false);

      expect(existsSync(resolve(outputRoot, 'harness'))).toBe(false);
      const normalBytes = readApplicationBundles(outputRoot);
      expect(normalBytes).toContain('/login');
      expect(normalBytes).not.toContain('/_harness');
      expect(normalBytes).not.toContain(
        'aa570fab912ce9b49b805b07decf274178b3c0092b983c1a1127bed3b213252e',
      );
      expect(normalBytes).not.toContain('print-sidebar-overflow');
      expect(normalBytes).not.toContain('print-main-overflow');
      expect(normalBytes).not.toContain('js-scheme-bare');
    },
  );
});
