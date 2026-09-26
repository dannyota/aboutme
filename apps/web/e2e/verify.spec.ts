import { expect, type Page, test } from '@playwright/test';

import {
  CHROME_PIXEL_TOLERANCE,
  denyExternalRequests,
  expectCspClean,
  mockSignedOutSession,
  trackCsp,
  verifyScreenshot,
  waitForImages,
} from './support';
import {
  disableTransitions,
  gotoVerify,
  pageOverflow,
  readClipboard,
  serveDeployment,
  stubClipboard,
  VERIFY_BASE_URL,
} from './verify-support';

// The verify page (/verify), "Kiểm chứng phiên bản đang chạy" / "Verify
// what's running" (docs/design/deployment-transparency/page.md and
// visual.md). Covers every state the fixtures encode, the copy buttons, the
// component selector, phone width, dark mode, no-JavaScript degradation, the
// landing footer link, and pixel baselines for the verified and unavailable
// states.

const VERIFIED_TITLE_VI = 'Đang chạy đúng mã nguồn trên GitHub';
const VERIFIED_TITLE_EN = 'Running exactly what\'s on GitHub';
const UNAVAILABLE_TITLE_VI = 'Máy chủ này không công bố thông tin triển khai';
const UNAVAILABLE_TITLE_EN = 'This server publishes no deployment record';
const MISMATCH_TITLE_EN = 'Something running doesn\'t match a signed build';
const UNVERIFIED_TITLE_EN = 'Signatures not checked yet';
const ROLLING_OUT_TITLE_EN = 'Update in progress, two versions running';
const OUTDATED_TITLE_EN = 'This page is out of date. Reload it.';
// Chrome logs "Failed to load resource: ... a status of N" for the expected
// signed-out 401 from GET /api/v1/me and for the unavailable state's 404.
const ANONYMOUS_ME_401 = 'a status of 401';
const DOCUMENT_404 = 'a status of 404';

// Digests and versions read directly from the fixtures under
// fixtures/deployment/, so a copy-button assertion checks the value a real
// deployment.json would carry rather than a value invented for the test.
const VERIFIED_SERVER_DIGEST
  = 'sha256:fdafc11f142a7df36d8182dc9fca9cfc0121adeaf2265e2336961c45c46f8181';
const ROLLING_WEB_OLD_HEX
  = '0ba375ceeb4f482e0ade774c5539c848858b5911882af6236d03f7e60d87a412';

// --- State coverage: each fixture decides one page state
// (page.md#states). ---

const STATE_FIXTURES = [
  { fixture: 'verified', state: 'verified', titleEn: VERIFIED_TITLE_EN },
  {
    fixture: 'rolling-out', state: 'rolling_out',
    titleEn: ROLLING_OUT_TITLE_EN,
  },
  { fixture: 'unverified', state: 'unverified', titleEn: UNVERIFIED_TITLE_EN },
  {
    fixture: 'mismatch-not-found', state: 'mismatch',
    titleEn: MISMATCH_TITLE_EN,
  },
  {
    fixture: 'mismatch-invalid', state: 'mismatch', titleEn: MISMATCH_TITLE_EN,
  },
  {
    fixture: 'mismatch-missing', state: 'mismatch', titleEn: MISMATCH_TITLE_EN,
  },
  {
    fixture: 'mismatch-summary', state: 'mismatch', titleEn: MISMATCH_TITLE_EN,
  },
] as const;

for (const { fixture, state, titleEn } of STATE_FIXTURES) {
  test(`fixture ${fixture} renders the ${state} state`, async ({ page }) => {
    await serveDeployment(page, { fixture });
    await gotoVerify(page, 'en');
    const status = page.getByTestId('verify-status');
    await expect(status).toHaveAttribute('data-state', state);
    await expect(status).toContainText(titleEn);
    // Only the verified fixture may show the verified title (page.md#states,
    // "only the last row can show a success mark").
    if (state !== 'verified') {
      await expect(status).not.toContainText(VERIFIED_TITLE_EN);
    }
  });
}

test('the verified fixture renders its Vietnamese title', async ({ page }) => {
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'vi');
  const status = page.getByTestId('verify-status');
  await expect(status).toHaveAttribute('data-state', 'verified');
  await expect(status).toContainText(VERIFIED_TITLE_VI);
});

test('the stale fixture renders the stale state past its own '
  + 'stale_after', async ({ page }) => {
  // The fixture's own observed_at and stale_after are already 20 minutes
  // behind this Date header, so the page must call it stale on both the
  // stale_after rule and the maxDocumentAgeMs rule
  // (app/utils/deploymentDocument.ts).
  await serveDeployment(page, { fixture: 'stale', dateOffsetMs: 20 * 60_000 });
  await gotoVerify(page, 'en');
  const status = page.getByTestId('verify-status');
  await expect(status).toHaveAttribute('data-state', 'stale');
  await expect(status).toContainText('Not checked since');
  await expect(status).not.toContainText(VERIFIED_TITLE_EN);
});

test('the outdated fixture asks the visitor to reload instead of reading a '
  + 'newer schema', async ({ page }) => {
  await serveDeployment(page, { fixture: 'outdated' });
  await gotoVerify(page, 'en');
  const status = page.getByTestId('verify-status');
  await expect(status).toHaveAttribute('data-state', 'outdated');
  await expect(status).toContainText(OUTDATED_TITLE_EN);
  await expect(page.getByTestId('verify-chain')).toBeHidden();
  await expect(page.getByTestId('verify-components')).toBeHidden();
});

test('unavailable from a 404 hides the chain and components and shows '
  + 'command placeholders', async ({ page }) => {
  await serveDeployment(page, { status: 404, body: '[]' });
  await gotoVerify(page, 'en');
  const status = page.getByTestId('verify-status');
  await expect(status).toHaveAttribute('data-state', 'unavailable');
  await expect(status).toContainText(UNAVAILABLE_TITLE_EN);
  await expect(page.getByTestId('verify-chain')).toBeHidden();
  await expect(page.getByTestId('verify-components')).toBeHidden();
  await expect(page.getByTestId('verify-selector')).toBeHidden();
  await expect(page.getByTestId('verify-command-2')).toContainText('<digest>');
  await expect(page.getByTestId('verify-command-2')).toContainText('<version>');
  await expect(page.getByTestId('verify-json-link')).toHaveAttribute(
    'href', '/.well-known/deployment.json');
});

test('unavailable from a 200 response with a [] body is the same '
  + 'state', async ({ page }) => {
  await serveDeployment(page, { status: 200, body: '[]' });
  await gotoVerify(page, 'en');
  const status = page.getByTestId('verify-status');
  await expect(status).toHaveAttribute('data-state', 'unavailable');
  await expect(status).toContainText(UNAVAILABLE_TITLE_EN);
  await expect(page.getByTestId('verify-selector')).toBeHidden();
  await expect(page.getByTestId('verify-command-2')).toContainText('<digest>');
  await expect(page.getByTestId('verify-command-2')).toContainText('<version>');
});

test('unavailable renders its Vietnamese title', async ({ page }) => {
  await serveDeployment(page, { status: 404, body: '[]' });
  await gotoVerify(page, 'vi');
  await expect(page.getByTestId('verify-status')).toContainText(
    UNAVAILABLE_TITLE_VI);
});

// --- Chain chips and connectors (visual.md#chain). ---

async function expectChain(
  page: Page,
  chips: readonly string[],
  connectors: readonly string[],
): Promise<void> {
  const chain = page.getByTestId('verify-chain');
  const chipEls = chain.locator('[data-chip]');
  const connectorEls = chain.locator('[data-connector]');
  await expect(chipEls).toHaveCount(chips.length);
  await expect(connectorEls).toHaveCount(connectors.length);
  const chipValues = await chipEls.evaluateAll(
    (els) => els.map((el) => el.getAttribute('data-chip')));
  const connectorValues = await connectorEls.evaluateAll(
    (els) => els.map((el) => el.getAttribute('data-connector')));
  expect(chipValues).toEqual([...chips]);
  expect(connectorValues).toEqual([...connectors]);
}

test('the verified chain shows four matches with agreeing '
  + 'connectors', async ({ page }) => {
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');
  await expectChain(
    page,
    ['match', 'match', 'match', 'match'],
    ['agree', 'agree', 'agree'],
  );
});

test('the rolling-out chain shows an updating running step', async ({
  page,
}) => {
  await serveDeployment(page, { fixture: 'rolling-out' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'rolling_out');
  await expectChain(
    page,
    ['match', 'match', 'match', 'updating'],
    ['agree', 'agree', 'updating'],
  );
});

test('the mismatch-not-found chain fails the image step', async ({ page }) => {
  await serveDeployment(page, { fixture: 'mismatch-not-found' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'mismatch');
  await expectChain(
    page,
    ['match', 'match', 'failed', 'not_verified'],
    ['agree', 'disagree', 'unknown'],
  );
});

test('the mismatch-missing chain fails the running step', async ({ page }) => {
  await serveDeployment(page, { fixture: 'mismatch-missing' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'mismatch');
  await expectChain(
    page,
    ['match', 'match', 'match', 'failed'],
    ['agree', 'agree', 'disagree'],
  );
});

test('the stale chain shows not-rechecked chips with unknown '
  + 'connectors', async ({ page }) => {
  await serveDeployment(page, { fixture: 'stale', dateOffsetMs: 20 * 60_000 });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'stale');
  await expectChain(
    page,
    ['not_rechecked', 'not_rechecked', 'not_rechecked', 'not_rechecked'],
    ['unknown', 'unknown', 'unknown'],
  );
});

// --- Component rows (visual.md#component-rows). ---

test('the verified state lists all three serving components', async ({
  page,
}) => {
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');
  for (const component of ['server', 'web', 'caddy']) {
    await expect(page.locator(`[data-component="${component}"]`))
      .toBeVisible();
  }
});

test('a missing component keeps its row in the mismatch-missing '
  + 'fixture', async ({ page }) => {
  await serveDeployment(page, { fixture: 'mismatch-missing' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'mismatch');
  await expect(page.locator('[data-component="caddy"]')).toBeVisible();
});

// --- Copy buttons (visual.md#digest-chip-and-copy,
// visual.md#verify-it-yourself). Headless Chromium cannot reliably grant the
// clipboard-write permission, so these stub navigator.clipboard.writeText
// instead of using it for real. ---

test('copying the server digest writes the full digest and announces '
  + 'Copied', async ({ page }) => {
  await stubClipboard(page);
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');

  await page.getByRole(
    'button', { name: 'Copy the server digest', exact: true },
  ).click();
  expect(await readClipboard(page)).toBe(VERIFIED_SERVER_DIGEST);
  await expect(page.getByRole('status').filter({ hasText: 'Copied' }))
    .toBeVisible();
});

test('copying command 2 writes the full attestation command for the '
  + 'default target', async ({ page }) => {
  await stubClipboard(page);
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');

  await page.getByRole('button', { name: 'Copy command 2', exact: true })
    .click();
  const copied = await readClipboard(page);
  expect(copied).not.toBeNull();
  expect(copied!.startsWith('gh attestation verify \\')).toBe(true);
  expect(copied).toContain(
    VERIFIED_SERVER_DIGEST.slice('sha256:'.length));
  expect(copied).toContain('v0.6.0');
  expect(copied).not.toContain('$ ');
});

test('a rejecting clipboard announces the copy-failed message', async ({
  page,
}) => {
  await stubClipboard(page, { reject: true });
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');

  await page.getByRole(
    'button', { name: 'Copy the server digest', exact: true },
  ).click();
  await expect(page.getByRole('status').filter({
    hasText: 'Couldn\'t copy. Select it and copy it yourself.',
  })).toBeVisible();
});

// --- Component selector (visual.md#verify-it-yourself). ---

test('choosing web v0.5.21 during a rollout puts that digest and version '
  + 'in command 2', async ({ page }) => {
  await serveDeployment(page, { fixture: 'rolling-out' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'rolling_out');

  await page.getByTestId('verify-selector')
    .getByText('web v0.5.21', { exact: true })
    .click();
  const command2 = page.getByTestId('verify-command-2');
  await expect(command2).toContainText(ROLLING_WEB_OLD_HEX);
  await expect(command2).toContainText('v0.5.21');
});

// --- Phone width (visual.md#frame): no horizontal page overflow, and
// command blocks scroll inside themselves rather than widening the page. ---

for (const width of [360, 390] as const) {
  for (const spec of [
    { fixture: 'verified', state: 'verified' },
    { fixture: 'rolling-out', state: 'rolling_out' },
  ] as const) {
    test(`${spec.fixture} fits ${width}px with no horizontal `
      + 'overflow', async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await serveDeployment(page, { fixture: spec.fixture });
      await gotoVerify(page, 'en');
      await expect(page.getByTestId('verify-status')).toHaveAttribute(
        'data-state', spec.state);
      await page.evaluate(() => document.fonts.ready);
      expect(await pageOverflow(page)).toBe(0);

      for (const testId of ['verify-command-1', 'verify-command-2']) {
        const overflowX = await page.getByTestId(testId).locator('pre')
          .evaluate((el) => getComputedStyle(el).overflowX);
        expect(['auto', 'scroll']).toContain(overflowX);
      }
    });
  }
}

// --- Dark mode (visual.md#tokens). ---

test('dark theme sets html[data-theme=dark] and repaints the status '
  + 'card', async ({ page }) => {
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en', 'light');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');
  const lightBackground = await page.getByTestId('verify-status').evaluate(
    (el) => getComputedStyle(el).backgroundColor);

  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en', 'dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');
  const darkBackground = await page.getByTestId('verify-status').evaluate(
    (el) => getComputedStyle(el).backgroundColor);

  expect(darkBackground).not.toBe(lightBackground);
});

// --- No cross-origin requests and no CSP violation, console error, or page
// error (page.md#behavior: "The page makes no request to any other
// origin"). ---

test('the verified page raises no CSP violation and makes no cross-origin '
  + 'request', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);
  await serveDeployment(page, { fixture: 'verified' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'verified');

  await expectCspClean(probe, [ANONYMOUS_ME_401]);
  expect(external).toEqual([]);
});

test('the unavailable state raises no CSP violation and makes no '
  + 'cross-origin request', async ({ page }) => {
  const probe = await trackCsp(page);
  const external = await denyExternalRequests(page);
  await mockSignedOutSession(page);
  await serveDeployment(page, { status: 404, body: '[]' });
  await gotoVerify(page, 'en');
  await expect(page.getByTestId('verify-status')).toHaveAttribute(
    'data-state', 'unavailable');

  await expectCspClean(probe, [ANONYMOUS_ME_401, DOCUMENT_404]);
  expect(external).toEqual([]);
});

// --- No JavaScript (page.md#behavior: "Without JavaScript, the page shows
// the explanation, a link to the JSON document, and the verify commands with
// placeholders"). ---

test('without JavaScript the page still gives a JSON link and command '
  + 'placeholders, and never claims verified', async ({ browser }) => {
  const context = await browser.newContext({
    baseURL: VERIFY_BASE_URL,
    javaScriptEnabled: false,
  });
  try {
    const page = await context.newPage();
    let deploymentRequests = 0;
    await page.route('**/.well-known/deployment.json', async (route) => {
      deploymentRequests += 1;
      await route.abort();
    });
    await context.addCookies([
      { name: 'aboutme-locale', value: 'en', url: VERIFY_BASE_URL },
    ]);

    const response = await page.goto('/verify');
    expect(response?.status()).toBe(200);
    await expect(page.locator('h1[data-page-title]')).toBeVisible();
    await expect(page.getByTestId('verify-json-link')).toHaveAttribute(
      'href', '/.well-known/deployment.json');
    await expect(page.getByTestId('verify-command-2')).toContainText(
      '<digest>');
    await expect(page.getByTestId('verify-command-2')).toContainText(
      '<version>');
    await expect(page.getByTestId('verify-limits')).toBeVisible();
    await expect(page.getByTestId('verify-status')).not.toHaveAttribute(
      'data-state', 'verified');
    // Without JavaScript, onMounted never runs, so the browser-only fetch
    // (app/composables/useDeploymentDocument.ts) never starts.
    expect(deploymentRequests).toBe(0);
  } finally {
    await context.close();
  }
});

// --- Landing footer link (page.md#footer-link). ---

test('the landing footer links to /verify after the privacy link', async ({
  page,
}) => {
  await page.context().addCookies([
    { name: 'aboutme-locale', value: 'vi', url: VERIFY_BASE_URL },
  ]);
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);

  const order = await page.evaluate(() => Array.from(
    document.querySelectorAll('[data-testid]'),
  ).map((el) => el.getAttribute('data-testid')));
  const privacyIndex = order.indexOf('landing-privacy-link');
  const verifyIndex = order.indexOf('landing-verify-link');
  expect(privacyIndex).toBeGreaterThan(-1);
  expect(verifyIndex).toBeGreaterThan(privacyIndex);

  const link = page.getByTestId('landing-verify-link');
  await expect(link).toHaveAttribute('href', '/verify');
  await expect(link).toHaveText('Kiểm chứng');
});

test('the landing footer verify link reads "Verify" in English', async ({
  page,
}) => {
  await page.context().addCookies([
    { name: 'aboutme-locale', value: 'en', url: VERIFY_BASE_URL },
  ]);
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  await expect(page.getByTestId('landing-verify-link')).toHaveText('Verify');
});

// --- Pixel baselines (ADR 0050; DESIGN.md), Vietnamese locale, at 390px and
// 1280px in both themes, for the verified and unavailable states. ---

const BASELINE_STATES = [
  { name: 'verified', state: 'verified' },
  { name: 'unavailable', state: 'unavailable' },
] as const;

for (const baseline of BASELINE_STATES) {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [390, 1280] as const) {
      test(`pixel baseline: verify ${baseline.name} ${theme} ${width}`
        + 'px', async ({ page }, testInfo) => {
        await page.setViewportSize({ width, height: 900 });
        if (baseline.name === 'verified') {
          await serveDeployment(page, { fixture: 'verified' });
        } else {
          await serveDeployment(page, { status: 404, body: '[]' });
        }
        await gotoVerify(page, 'vi', theme);
        await disableTransitions(page);
        // Waiting for the settled state, not a fixed delay, keeps the
        // capture from racing the browser-only fetch
        // (app/composables/useDeploymentDocument.ts) and from landing
        // mid-way through the "vừa xong" / "just now" relative-age text.
        await expect(page.getByTestId('verify-status')).toHaveAttribute(
          'data-state', baseline.state);
        await page.evaluate(() => document.fonts.ready);
        await waitForImages(page);

        const settledHeight = await page.evaluate(
          () => document.documentElement.scrollHeight);
        await page.setViewportSize({ width, height: settledHeight });
        expect(await pageOverflow(page)).toBe(0);

        await verifyScreenshot(
          page,
          `verify--${baseline.name}--${theme}--${width}.png`,
          testInfo,
          undefined,
          CHROME_PIXEL_TOLERANCE,
        );
      });
    }
  }
}
