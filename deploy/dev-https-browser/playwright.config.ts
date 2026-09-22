import { defineConfig } from '@playwright/test';

const baseURL = 'https://localhost:20443';
const browserModes = [
  'auth',
  'transport',
  'editor',
  'public',
  'password-auth',
  'mcp',
  'entry',
  'publish',
  'exports',
  'privacy',
  'sample-start',
  'second-factor',
  'second-factor-disabled',
] as const;
type BrowserMode = typeof browserModes[number];
const requestedMode = process.env.ABOUTME_BROWSER_MODE ?? 'auth';

if (!browserModes.includes(requestedMode as BrowserMode)) {
  throw new Error('invalid browser mode');
}

const mode = requestedMode as BrowserMode;
// The enabled second-factor journey walks enrollment, four pending sign-ins,
// the negative cases, and teardown in one test, so it gets its own budget.
const timeout = mode === 'second-factor' || mode === 'second-factor-disabled'
  ? 600_000
  : mode === 'editor' || mode === 'public' || mode === 'password-auth'
    || mode === 'mcp' || mode === 'publish' || mode === 'exports'
    || mode === 'privacy' || mode === 'sample-start' ? 120_000 : 30_000;

// Both second-factor modes run one spec. The server enrollment flag, not the
// spec file, is what differs between them; the spec selects its own test from
// ABOUTME_BROWSER_MODE.
const specName = mode === 'second-factor-disabled' ? 'second-factor' : mode;

for (const name of ['UPDATE_GOLDEN', 'PLAYWRIGHT_UPDATE_SNAPSHOTS']) {
  if (Object.hasOwn(process.env, name)) {
    throw new Error(`${name} must be absent.`);
  }
}

export default defineConfig({
  actionTimeout: mode === 'publish' ? 10_000 : 0,
  forbidOnly: true,
  fullyParallel: false,
  outputDir: '/tmp/playwright-artifacts',
  preserveOutput: 'never',
  reporter: [['line']],
  retries: 0,
  navigationTimeout: mode === 'publish' ? 20_000 : 0,
  testDir: import.meta.dirname,
  testMatch: [`${specName}.spec.ts`],
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
