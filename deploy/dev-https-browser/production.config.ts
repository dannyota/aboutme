import { defineConfig } from '@playwright/test';

// Keep failure-page ARIA snapshots (`error-context.md`) suppressed for the
// pinned Playwright 1.64.0 runner. A page-content snapshot could hold a
// secret; see docs/runbooks/totp-keys.md "Production proofs".
// This runner-internal environment switch requires verification whenever
// the pinned Playwright package changes.
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

// The production journey touches a live host over the network, not a local
// dev server, so every step budget is wider than the local harness uses.
// A step that never settles must still fail at its own stage rather than
// silently consume the run; see totp-production.spec.ts for the per-step
// wrap that turns any error into one fixed, secret-free message.
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
