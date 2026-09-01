import { defineConfig } from '@playwright/test';

const baseURL = 'https://localhost:20443';
const browserModes = ['auth', 'transport', 'editor', 'public', 'password-auth', 'mcp'] as const;
type BrowserMode = typeof browserModes[number];
const requestedMode = process.env.ABOUTME_BROWSER_MODE ?? 'auth';

if (!browserModes.includes(requestedMode as BrowserMode)) {
  throw new Error('invalid browser mode');
}

const mode = requestedMode as BrowserMode;
const timeout = mode === 'editor' || mode === 'public' || mode === 'password-auth'
  || mode === 'mcp' ? 120_000 : 30_000;

for (const name of ['UPDATE_GOLDEN', 'PLAYWRIGHT_UPDATE_SNAPSHOTS']) {
  if (Object.hasOwn(process.env, name)) {
    throw new Error(`${name} must be absent.`);
  }
}

export default defineConfig({
  forbidOnly: true,
  fullyParallel: false,
  outputDir: '/tmp/playwright-artifacts',
  preserveOutput: 'never',
  reporter: [['line']],
  retries: 0,
  testDir: import.meta.dirname,
  testMatch: [`${mode}.spec.ts`],
  timeout,
  updateSnapshots: 'none',
  use: {
    acceptDownloads: false,
    baseURL,
    browserName: 'chromium',
    chromiumSandbox: true,
    locale: 'en-US',
    screenshot: 'off',
    serviceWorkers: 'block',
    timezoneId: 'UTC',
    trace: 'off',
    video: 'off',
  },
  workers: 1,
});
