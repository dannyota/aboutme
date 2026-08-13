/**
 * Browser baseline: mcr.microsoft.com/playwright:v1.62.1-noble
 * AMD64 digest:
 * sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac
 * Executable: /ms-playwright/chromium-1234/chrome-linux64/chrome
 * Version: Google Chrome for Testing 151.0.7922.34
 */
import { defineConfig } from '@playwright/test';
import { isAbsolute, resolve } from 'node:path';

const surface = process.env.PLAYWRIGHT_SURFACE;
if (surface !== 'harness' && surface !== 'normal') {
  throw new Error('PLAYWRIGHT_SURFACE must be harness or normal.');
}
const resultsRoot = process.env.PLAYWRIGHT_RESULTS_DIR;
if (resultsRoot === undefined || resultsRoot.length === 0) {
  throw new Error('PLAYWRIGHT_RESULTS_DIR is required.');
}
if (!isAbsolute(resultsRoot)) {
  throw new Error('PLAYWRIGHT_RESULTS_DIR must be absolute.');
}
for (const name of ['UPDATE_GOLDEN', 'PLAYWRIGHT_UPDATE_SNAPSHOTS']) {
  if (Object.hasOwn(process.env, name)) {
    throw new Error(`${name} must be absent.`);
  }
}

const surfaceResults = resolve(resultsRoot, surface);
const port = surface === 'harness' ? 20090 : 20092;
const baseURL = `http://127.0.0.1:${port}`;
const buildCommand = 'node node_modules/nuxt/bin/nuxt.mjs build';
const serverCommand = surface === 'harness'
  ? 'node .output/harness/server/index.mjs'
  : 'node .output/server/index.mjs';
const buildEnvironment = {
  ...process.env,
  NITRO_HOST: '127.0.0.1',
  NITRO_PORT: String(port),
};
if (surface === 'harness') buildEnvironment.NUXT_HARNESS = '1';
else delete buildEnvironment.NUXT_HARNESS;

export default defineConfig({
  expect: {
    toHaveScreenshot: {
      animations: 'disabled',
      caret: 'hide',
      maxDiffPixelRatio: 0,
      maxDiffPixels: 0,
      scale: 'css',
      threshold: 0,
    },
  },
  forbidOnly: true,
  fullyParallel: false,
  outputDir: resolve(surfaceResults, 'artifacts'),
  preserveOutput: 'always',
  reporter: [
    ['line'],
    ['html', { open: 'never', outputFolder: resolve(surfaceResults, 'html') }],
    ['json', { outputFile: resolve(surfaceResults, 'results.json') }],
  ],
  retries: 0,
  snapshotPathTemplate: resolve(import.meta.dirname, 'baselines/{arg}{ext}'),
  testDir: import.meta.dirname,
  testMatch: surface === 'harness'
    ? [
        'screenshot.spec.ts',
        'fonts-offline.spec.ts',
        'corpus.spec.ts',
        'print.spec.ts',
      ]
    : ['normal-csp.spec.ts'],
  timeout: 20_000,
  updateSnapshots: 'none',
  use: {
    baseURL,
    browserName: 'chromium',
    colorScheme: 'light',
    deviceScaleFactor: 1,
    launchOptions: {
      args: [
        '--force-color-profile=srgb',
        '--font-render-hinting=none',
        '--disable-lcd-text',
        '--disable-gpu',
        '--hide-scrollbars',
      ],
    },
    locale: 'en-US',
    reducedMotion: 'reduce',
    screenshot: 'only-on-failure',
    timezoneId: 'UTC',
    trace: 'retain-on-failure',
    video: 'off',
    viewport: { height: 1123, width: 794 },
  },
  webServer: {
    command: `${buildCommand} && ${serverCommand}`,
    cwd: resolve(import.meta.dirname, '..'),
    env: buildEnvironment,
    reuseExistingServer: false,
    stderr: 'pipe',
    stdout: 'pipe',
    timeout: 300_000,
    url: baseURL,
  },
  workers: 1,
});
