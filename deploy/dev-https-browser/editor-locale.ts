import {
  expect,
  type BrowserContext,
  type Locator,
  type Page,
} from '@playwright/test';
import {
  createBlankResume,
  loginAsDevelopmentUser,
  readRemoteResumeRevision,
  uniqueTitle,
} from './editor-fixtures';
import {
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
} from './harness-lib';
import { isExpectedAnonymousMeConsole } from './network-policy';
import { type EditorProofState, isResumeMutation } from './editor-proof-state';

type LocaleFailure = 'disabled' | 'detached' | 'hidden' | 'pointer-interception' | 'timeout' | 'unknown';

export function classifyLocaleFailure(error: unknown): LocaleFailure {
  const message = error instanceof Error ? error.message : '';
  if (/intercepts pointer events|receives pointer events/u.test(message)) {
    return 'pointer-interception';
  }
  if (/disabled/u.test(message)) return 'disabled';
  if (/not visible|hidden/u.test(message)) return 'hidden';
  if (/not attached|detached/u.test(message)) return 'detached';
  if (/timeout|TimeoutError/u.test(message)) return 'timeout';
  return 'unknown';
}

export async function expectTopbarFitsViewport(
  page: Page,
  expectedHeight: number,
): Promise<void> {
  const viewport = page.viewportSize();
  if (viewport === null) throw new Error('viewport is missing');
  const topbar = page.locator('[data-region="topbar"]');
  await expect(topbar).toBeVisible();
  const topbarMetrics = await topbar.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    return {
      bottom: bounds.bottom,
      height: bounds.height,
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
    };
  });
  expect(topbarMetrics.height).toBe(expectedHeight);
  expect(topbarMetrics.bottom).toBe(expectedHeight);
  expect(topbarMetrics.scrollWidth).toBeLessThanOrEqual(topbarMetrics.clientWidth);

  const actions = [
    page.getByTestId('workspace-locale-vi'),
    page.getByTestId('workspace-locale-en'),
    page.locator('[data-action="download-pdf"]'),
    page.locator('[data-action="publish"]'),
    page.getByTestId('account-menu'),
  ];
  let previousBounds: { height: number; width: number; x: number; y: number } | undefined;
  for (const action of actions) {
    await expect(action).toBeVisible();
    const bounds = await action.boundingBox();
    if (bounds === null) throw new Error('topbar action is missing');
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.y).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(viewport.height);
    if (previousBounds !== undefined) {
      const sameRow = Math.abs(bounds.y - previousBounds.y) < 1;
      if (sameRow) {
        expect(bounds.x).toBeGreaterThanOrEqual(previousBounds.x + previousBounds.width);
      }
      const separated = bounds.x >= previousBounds.x + previousBounds.width
        || previousBounds.x >= bounds.x + bounds.width
        || bounds.y >= previousBounds.y + previousBounds.height
        || previousBounds.y >= bounds.y + bounds.height;
      expect(separated).toBe(true);
    }
    previousBounds = bounds;
  }

  const workspace = page.locator('[data-region="app-rail"]');
  await expect(workspace).toBeVisible();
  const workspaceBounds = await workspace.boundingBox();
  if (workspaceBounds === null) throw new Error('workspace is missing');
  expect(workspaceBounds.y).toBeGreaterThanOrEqual(expectedHeight);
}

export async function expectVisibleKeyboardFocus(locator: Locator): Promise<void> {
  await expect(locator).toBeVisible();
  await expect(locator).toBeFocused();
  expect(await locator.evaluate((element) => element.matches(':focus-visible'))).toBe(true);
}

export async function expectAccountTooltipDoesNotCoverLocaleButton(
  page: Page,
): Promise<void> {
  const account = page.getByTestId('account-menu');
  const accountLabel = await account.getAttribute('aria-label');
  if (accountLabel === null) throw new Error('account label is missing');
  const tooltipContent = page.locator('[data-slot="tooltip-content"]');
  await expect(tooltipContent).toBeVisible();
  await expect(tooltipContent).toHaveAttribute('data-side', 'bottom');
  await expect(page.getByRole('tooltip', { includeHidden: true })).toHaveText(accountLabel);
  await expect(account).toHaveAccessibleDescription(accountLabel);
  const localeTarget = page.getByTestId('workspace-locale-en');
  expect(await localeTarget.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    const target = document.elementFromPoint(
      bounds.left + bounds.width / 2,
      bounds.top + bounds.height / 2,
    );
    return target === element || element.contains(target);
  })).toBe(true);
}

export interface LocaleScenarioState {
  viewport: string;
  target: string;
  resumeID: string | undefined;
  writes: number;
}

export async function installLocaleDiagnostics(
  context: BrowserContext,
  page: Page,
  localeSetup: <T>(operation: () => Promise<T>) => Promise<T>,
  locale: LocaleScenarioState,
) {
  const counters = newDiagnosticCounters();
  await localeSetup(() => installExternalRequestFirewall(context, counters));
  await localeSetup(() => installExternalWebSocketFirewall(context, counters));
  const attachPageDiagnostics = pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url),
  });
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  context.on('request', (request) => {
    if (
      locale.resumeID !== undefined
      && request.method() === 'PATCH'
      && isResumeMutation(request, locale.resumeID)
    ) {
      locale.writes += 1;
    }
  });
  return counters;
}

export async function proveLocaleDraft(
  page: Page,
  state: EditorProofState,
  locale: LocaleScenarioState,
): Promise<void> {
  state.localeDiagnosticStage = 'locale-sign-in';
  await loginAsDevelopmentUser(page);
  state.localeDiagnosticStage = 'locale-create';
  const created = await createBlankResume(
    page,
    uniqueTitle(),
    undefined,
    (accepted) => state.recordedResumeIDs.add(accepted.metadata.id),
  );
  locale.resumeID = created.metadata.id;
  state.localeDiagnosticStage = 'locale-structure';
  await page.locator('[data-action="open-structure"]').click();
  await page.locator('[data-action="section-type"]').selectOption('work');
  await page.getByTestId('section-create-form').locator('[data-action="create"]').click();
  const section = page.locator('[data-section-key="work"]');
  await expect(section).toBeVisible();
  await section.locator('[data-action="add-entry"]').click();
  await expect(page.getByTestId('save-status')).toHaveAttribute('data-state', 'saved');
  const revision = await readRemoteResumeRevision(page, created.metadata.id);
  locale.writes = 0;
  for (const viewport of [
    { width: 1440, height: 900, stage: 'desktop', topbarHeight: 64 },
    { width: 390, height: 844, stage: 'phone390', topbarHeight: 96 },
    { width: 320, height: 844, stage: 'phone320', topbarHeight: 96 },
  ]) {
    locale.viewport = viewport.stage;
    await page.setViewportSize(viewport);
    if (viewport.width < 1440) {
      await page.locator('[data-action="show-editor"]').click();
    }
    state.localeDiagnosticStage = 'locale-draft';
    const startYear = page.locator(
      '[data-entry-id] [data-entry-field="dates"] [data-part="start-year"]',
    );
    await startYear.fill('2026');
    await startYear.press('Tab');
    const error = page.locator('[data-error="date-order"]');
    await expect(error).toHaveText('Add an end date or tick Present to save this date.');
    await startYear.click();
    await startYear.press('Control+A');
    const selection = await startYear.evaluate((element) => ({
      end: (element as HTMLInputElement).selectionEnd,
      start: (element as HTMLInputElement).selectionStart,
    }));
    const node = await startYear.elementHandle();
    if (node === null) throw new Error('date input is missing');
    state.localeDiagnosticStage = 'locale-toggle';
    await expectTopbarFitsViewport(page, viewport.topbarHeight);
    locale.target = 'vi';
    await page.getByTestId('workspace-locale-vi').click();
    state.localeDiagnosticStage = 'locale-assert';
    await expect(page.locator('html')).toHaveAttribute('lang', 'vi');
    await expectTopbarFitsViewport(page, viewport.topbarHeight);
    const preview = page
      .getByTestId('preview-sheet')
      .locator('.resume-document.resume-page')
      .first();
    await expect(preview).toHaveAttribute('lang', 'en');
    await expect(preview.locator('.section-heading h2')).toHaveText('Experience');
    await expect(startYear).toHaveValue('2026');
    await expect(error).toHaveText('Thêm ngày kết thúc hoặc chọn Hiện tại để lưu ngày này.');
    expect(await startYear.evaluate((element, previous) => element === previous, node)).toBe(true);
    expect(await startYear.evaluate((element) => globalThis.document.activeElement === element)).toBe(true);
    expect(await startYear.evaluate((element) => ({
      end: (element as HTMLInputElement).selectionEnd,
      start: (element as HTMLInputElement).selectionStart,
    }))).toEqual(selection);
    await page.locator('.editor-brand').focus();
    await page.keyboard.press('Tab');
    await expectVisibleKeyboardFocus(page.getByTestId('workspace-locale-vi'));
    await page.keyboard.press('Tab');
    await expectVisibleKeyboardFocus(page.getByTestId('workspace-locale-en'));
    await page.keyboard.press('Tab');
    await expectVisibleKeyboardFocus(page.locator('[data-action="download-pdf"]'));
    await page.keyboard.press('Tab');
    await expectVisibleKeyboardFocus(page.locator('[data-action="publish"]'));
    await page.keyboard.press('Tab');
    await expectVisibleKeyboardFocus(page.getByTestId('account-menu'));
    await expectAccountTooltipDoesNotCoverLocaleButton(page);
    locale.target = 'en';
    await page.getByTestId('workspace-locale-en').click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expectTopbarFitsViewport(page, viewport.topbarHeight);
    await expect(error).toHaveText('Add an end date or tick Present to save this date.');
    await page.getByTestId('account-menu').click();
    const accountMenu = page.getByRole('menu');
    await expect(accountMenu).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(accountMenu).toBeHidden();
  }
  await page.waitForTimeout(500);
  expect(locale.writes).toBe(0);
  expect(await readRemoteResumeRevision(page, created.metadata.id)).toBe(revision);
}
