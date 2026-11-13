import { defineConfig } from '@playwright/test';

process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1';

const baseURL = 'https://aboutme.vn';
const modes = [
  'totp-prod-flag-off',
  'totp-prod-enabled',
  'totp-prod-cleanup',
] as const;
type ProductionMode = typeof modes[number];
const requestedMode = process.env.ABOUTME_BROWSER_MODE ?? '';

if (!modes.includes(requestedMode as ProductionMode)) {
  throw new Error('invalid production browser mode');
}

for (const name of ['UPDATE_GOLDEN', 'PLAYWRIGHT_UPDATE_SNAPSHOTS']) {
  if (Object.hasOwn(process.env, name)) {
    throw new Error(`${name} must be absent.`);
  }
}

export default defineConfig({
  forbidOnly: true,
  fullyParallel: false,
  outputDir: '/tmp/totp-proof-artifacts',
  preserveOutput: 'never',
  reporter: [['line']],
  retries: 0,
  testDir: import.meta.dirname,
  testMatch: ['totp-production.spec.ts'],
  timeout: 900_000,
  updateSnapshots: 'none',
  use: {
    acceptDownloads: false,
    actionTimeout: 30_000,
    baseURL,
    browserName: 'chromium',
    chromiumSandbox: true,
    locale: 'en-US',
    navigationTimeout: 60_000,
    screenshot: 'off',
    serviceWorkers: 'block',
    timezoneId: 'UTC',
    trace: 'off',
    video: 'off',
  },
  workers: 1,
});
