// @vitest-environment node

import {
  existsSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
} from 'node:fs';
import { chdir, cwd } from 'node:process';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { Worker } from 'node:worker_threads';
import { beforeAll, describe, expect, it } from 'vitest';

const webRoot = resolve(import.meta.dirname, '..');
const nuxtBin = resolve(webRoot, 'node_modules/nuxt/bin/nuxt.mjs');
const outputRoot = resolve(webRoot, '.output');
const normalNuxtRoot = resolve(webRoot, '.nuxt');
const harnessNuxtRoot = resolve(webRoot, '.nuxt', 'harness');
const normalTestNuxtRoot = resolve(webRoot, '.nuxt', 'normal-test');
const normalTestOutputRoot = resolve(outputRoot, 'normal-test');
const workerPath = resolve(
  normalTestOutputRoot,
  'server/workers/public-render.mjs',
);
const hydrationPath = resolve(
  normalTestOutputRoot,
  'public/_nuxt/assets/public-resume.mjs',
);

const document = JSON.parse(
  readFileSync(
    resolve(webRoot, '../../packages/schema/fixtures/minimal.json'),
    'utf8',
  ),
);
document.content = {
  profile: {
    sectionType: 'profile',
    entries: [{ id: '00000000-0000-4000-8000-000000000001' }],
  },
};
document.customization.layout.sections.main = ['profile'];

const renderRequest = {
  publicResume: {
    slug: 'ada1',
    revision: '1',
    lng: 'en',
    downloadEnabled: false,
    document,
  },
  mode: 'continuous',
  canonicalOrigin: 'https://resume.example',
  discoveryEnabled: false,
};

const render = async (): Promise<{ html: string; exit: number }> =>
  new Promise((resolveRender, reject) => {
    let html: string | undefined;
    const worker = new Worker(new URL(`file://${workerPath}`), {
      execArgv: [],
      workerData: renderRequest,
    });
    worker.once('message', (message: { html?: unknown }) => {
      if (typeof message.html === 'string') html = message.html;
    });
    worker.once('error', reject);
    worker.once('exit', (exit) => {
      if (html === undefined) reject(new Error('worker produced no HTML'));
      else resolveRender({ html, exit });
    });
  });

function build(harness: boolean): void {
  const env = { ...process.env };
  env.NUXT_BUILD_TEST = '1';
  // These two builds run back-to-back and write to distinct build directories.
  // Neither build may inherit a caller's harness selection.
  env.NUXT_IGNORE_LOCK = '1';
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

describe('build-only renderer harness', () => {
  let harnessBytes = '';
  let harnessRoutes = '';
  let normalBytes = '';
  let normalRoutes = '';

  beforeAll(() => {
    rmSync(harnessNuxtRoot, { force: true, recursive: true });
    rmSync(normalTestNuxtRoot, { force: true, recursive: true });
    rmSync(resolve(outputRoot, 'harness'), { force: true, recursive: true });
    rmSync(normalTestOutputRoot, { force: true, recursive: true });
    build(true);

    const harnessServer = resolve(outputRoot, 'harness/server');
    const harnessOutput = resolve(outputRoot, 'harness');
    harnessBytes = readTextTree(harnessOutput);
    harnessRoutes = readFileSync(
      resolve(harnessServer, 'chunks/virtual/entry.mjs'),
      'utf8',
    );

    build(false);
    normalBytes = readTextTree(normalTestOutputRoot);
    normalRoutes = readFileSync(
      resolve(normalTestOutputRoot, 'server/chunks/virtual/entry.mjs'),
      'utf8',
    );
  }, 300_000);

  it('is present only in the isolated harness build', () => {
    expect(existsSync(join(harnessNuxtRoot, 'tsconfig.app.json'))).toBe(true);

    expect(existsSync(resolve(outputRoot, 'harness/server/index.mjs'))).toBe(
      true,
    );
    expect(harnessBytes).toContain('/_harness/render');
    expect(harnessBytes).toContain(
      'aa570fab912ce9b49b805b07decf274178b3c0092b983c1a1127bed3b213252e',
    );
    expect(harnessBytes).toContain('print-sidebar-overflow');
    expect(harnessBytes).toContain('print-main-overflow');
    expect(harnessBytes).toContain('js-scheme-bare');
    expect(harnessRoutes).toContain('path: "/_harness/render"');
    expect(harnessRoutes).not.toContain(
      'path: "/_harness/photo-fixture"',
    );
    expect(harnessRoutes).not.toContain(
      'path: "/_harness/print-fixtures"',
    );

    expect(existsSync(join(normalNuxtRoot, 'tsconfig.app.json'))).toBe(true);
    expect(
      existsSync(join(normalTestNuxtRoot, 'tsconfig.app.json')),
    ).toBe(true);
    expect(normalBytes).toContain('/login');
    expect(normalRoutes).not.toContain('path: "/_harness/render"');
    expect(normalBytes).not.toContain(
      'aa570fab912ce9b49b805b07decf274178b3c0092b983c1a1127bed3b213252e',
    );
    expect(normalBytes).not.toContain('print-sidebar-overflow');
    expect(normalBytes).not.toContain('print-main-overflow');
    expect(normalBytes).not.toContain('js-scheme-bare');
  });

  it('emits only the closed worker and external hydration asset', () => {
    expect(existsSync(workerPath)).toBe(true);
    expect(existsSync(hydrationPath)).toBe(true);
    expect(
      existsSync(
        resolve(
          normalTestOutputRoot,
          'public/_nuxt/assets/public-render.mjs',
        ),
      ),
    ).toBe(false);
    expect(
      existsSync(
        resolve(
          normalTestOutputRoot,
          'public/_nuxt/assets/public-resume-validator.mjs',
        ),
      ),
    ).toBe(false);
    expect(readFileSync(workerPath, 'utf8')).not.toContain('js-scheme-bare');
    expect(readFileSync(hydrationPath, 'utf8')).not.toContain('js-scheme-bare');
  });

  it(
    'renders deterministic bytes and exits cleanly from another cwd',
    async () => {
      const original = cwd();
      try {
        chdir('/tmp');
        const first = await render();
        const second = await render();
        expect(first).toEqual(second);
        expect(first.exit).toBe(0);
        expect(first.html).toContain('<title>Ada Lovelace — Resume</title>');
      } finally {
        chdir(original);
      }
    },
  );

  it('removes screen paper geometry and backdrop from print output', () => {
    const source = readFileSync(
      resolve(webRoot, 'app/pages/_harness/render.vue'),
      'utf8',
    );
    expect(source).toContain(
      ':style="printFixture ? undefined : paperStyle"',
    );
    expect(source).toMatch(new RegExp([
      '@media\\s+print\\s*\\{[\\s\\S]*html',
      '[\\s\\S]*body\\.resume-print\\s*\\{[^}]*background:\\s*#fff;',
    ].join('')));
  });
});
