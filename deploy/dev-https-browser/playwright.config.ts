import { defineConfig } from '@playwright/test';

const baseURL = 'https://localhost:20443';

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
  testMatch: ['auth.spec.ts', 'transport.spec.ts'],
  timeout: 30_000,
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
