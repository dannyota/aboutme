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
const secondFactor = mode === 'second-factor'
  || mode === 'second-factor-disabled';
// The enabled second-factor journey warms every page it uses, then walks
// enrollment, four pending sign-ins, the negative cases, and teardown in one
// test, so it gets its own budget. The disabled journey opens three pages and
// makes a handful of calls, so it gets a much smaller one. Together with the
// stack restart between them, both fit inside the hosted job's own limit.
const timeout = mode === 'second-factor'
  ? 1_200_000
  : mode === 'second-factor-disabled'
    ? 420_000
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

// A second-factor step that never settles must fail at its own stage, not
// silently consume the whole test budget and report the teardown stage
// instead. Both waits are bounded well above the slowest observed hosted
// navigation, so only a genuinely stuck step trips them.
//
// These two are `use` options. Playwright declares them on
// PlaywrightTestOptions only, so a value at the top level of the config is
// read by nobody and every mode runs unbounded. Bounding another mode's proof
// needs its own brief, so every mode but this one keeps the unlimited default
// it runs with today.
// The navigation bound covers one landing per journey that reaches an app
// page before the signed-in warm pass has run, so it is wider than an
// ordinary warm navigation needs.
const actionTimeout = secondFactor ? 20_000 : 0;
const navigationTimeout = secondFactor ? 120_000 : 0;

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
  workers: 1,
});
