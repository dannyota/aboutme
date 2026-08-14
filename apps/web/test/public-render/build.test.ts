// @vitest-environment node

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { chdir, cwd } from 'node:process';
import { resolve } from 'node:path';
import { Worker } from 'node:worker_threads';
import { beforeAll, describe, expect, it } from 'vitest';

const webRoot = resolve(process.cwd());
const output = resolve(webRoot, '.output/normal-test');
const workerPath = resolve(output, 'server/workers/public-render.mjs');
const hydrationPath = resolve(output, 'public/_nuxt/assets/public-resume.mjs');

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

const request = {
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
      workerData: request,
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

beforeAll(() => {
  execFileSync('npm', ['run', 'build'], {
    cwd: webRoot,
    stdio: 'pipe',
    env: { ...process.env, NUXT_BUILD_TEST: '1' },
  });
}, 120_000);

describe('production public render artifacts', () => {
  it('emits only the closed worker and external hydration asset', () => {
    expect(existsSync(workerPath)).toBe(true);
    expect(existsSync(hydrationPath)).toBe(true);
    expect(
      existsSync(resolve(output, 'public/_nuxt/assets/public-render.mjs')),
    ).toBe(false);
    expect(
      existsSync(
        resolve(output, 'public/_nuxt/assets/public-resume-validator.mjs'),
      ),
    ).toBe(false);
    expect(readFileSync(workerPath, 'utf8')).not.toContain('js-scheme-bare');
    expect(readFileSync(hydrationPath, 'utf8')).not.toContain('js-scheme-bare');
  });

  it('renders deterministic bytes and exits cleanly from another cwd',
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
});
