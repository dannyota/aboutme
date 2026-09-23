import { defineConfig } from '@playwright/test';

// Suppresses the pinned Playwright 1.62.1 runner's failure-page ARIA
// snapshot (`error-context.md`). The published config surface has no field
// for this; reading the pinned package
// (node_modules/playwright/lib/index.js, `_takePageSnapshot`) shows the
// capture is skipped whenever this process environment variable is set,
// before Playwright's own base reporter fixture ever calls
// `page.ariaSnapshot()`. Setting it here, at module load, covers the whole
// run without changing `run.sh`'s environment. See
// docs/runbooks/totp-keys.md "Production proofs": no assertion may print or
// persist a secret, and a page-content snapshot could hold one.
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
