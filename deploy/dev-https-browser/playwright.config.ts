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
  'linkedin',
  'linkedin-import',
  'second-factor',
  'second-factor-disabled',
  'totp',
  'totp-disabled',
  'mcp-sdk',
  'mcp-ts-sdk',
] as const;
type BrowserMode = typeof browserModes[number];
const requestedMode = process.env.ABOUTME_BROWSER_MODE ?? 'auth';

if (!browserModes.includes(requestedMode as BrowserMode)) {
  throw new Error('invalid browser mode');
}

const mode = requestedMode as BrowserMode;
const secondFactor = mode === 'second-factor'
  || mode === 'second-factor-disabled'
  || mode === 'totp'
  || mode === 'totp-disabled';
const timeout = mode === 'totp'
  ? 1_800_000
  : mode === 'second-factor'
    ? 1_200_000
    : mode === 'second-factor-disabled' || mode === 'totp-disabled'
      || mode === 'mcp-sdk'
      ? 420_000
      : mode === 'linkedin-import'
        ? 180_000
        : mode === 'editor' || mode === 'public' || mode === 'password-auth'
          || mode === 'mcp' || mode === 'mcp-ts-sdk' || mode === 'publish' || mode === 'exports'
          || mode === 'privacy' || mode === 'sample-start' || mode === 'entry'
          || mode === 'linkedin'
          ? 120_000 : 30_000;

const specName = mode === 'second-factor-disabled'
  ? 'second-factor'
  : mode === 'totp-disabled' ? 'totp' : mode;

for (const name of ['UPDATE_GOLDEN', 'PLAYWRIGHT_UPDATE_SNAPSHOTS']) {
  if (Object.hasOwn(process.env, name)) {
    throw new Error(`${name} must be absent.`);
  }
}

// navigationTimeout and actionTimeout exist only under `use`; at the top level of the config nobody reads them (Playwright TestOptions).
const actionTimeout = secondFactor ? 20_000 : 0;
const navigationTimeout = secondFactor ? 120_000 : 0;

const totpShards = process.env.ABOUTME_TOTP_SHARD ?? '';
const workers = mode === 'totp' && totpShards !== ''
  ? totpShards.split(',').length
  : 1;

export default defineConfig({
  forbidOnly: true,
  fullyParallel: false,
  outputDir: '/tmp/playwright-artifacts',
  preserveOutput: 'never',
  reporter: [['line']],
  retries: 0,
  testDir: import.meta.dirname,
  testMatch: [`${specName}.spec.ts`],
  timeout,
  updateSnapshots: 'none',
  use: {
    acceptDownloads: false,
    actionTimeout,
    baseURL,
    browserName: 'chromium',
    chromiumSandbox: true,
    locale: 'en-US',
    navigationTimeout,
    screenshot: 'off',
    serviceWorkers: 'block',
    timezoneId: 'UTC',
    trace: 'off',
    video: 'off',
  },
  workers,
});
