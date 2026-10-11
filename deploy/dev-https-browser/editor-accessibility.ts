import { AxeBuilder } from '@axe-core/playwright';
import { expect, type Page } from '@playwright/test';
import { waitForHydration } from './harness-lib';
import { ORIGIN, type EditorProofState } from './editor-proof-state';

export async function proveAccessibility(
  state: EditorProofState,
  page: Page,
  resumeID: string,
): Promise<void> {
  state.editorDiagnosticStage = 'accessibility-list';
  await page.getByRole('link', { name: 'aboutme' }).press('Enter');
  await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();
  await expect(page.locator(`[data-testid="resume-row-${resumeID}"]`)).toBeVisible();
  state.editorDiagnosticStage = 'accessibility-list-audit';
  const listFindings = await new AxeBuilder({ page }).analyze();
  const listViolations = seriousOrCritical(listFindings.violations);
  if (listViolations.length > 0) {
    const contrast = listFindings.violations.find(
      ({ id }) => id === 'color-contrast',
    );
    const safeTarget = JSON.stringify(contrast?.nodes[0]?.target ?? [])
      .replaceAll(/[0-9a-f]{8}-[0-9a-f-]{27}/gi, 'resume-id')
      .toLowerCase()
      .replaceAll(/[^a-z0-9-]+/g, '-')
      .replaceAll(/^-+|-+$/g, '')
      .slice(0, 120);
    state.editorDiagnosticStage = `accessibility-list-${listViolations
      .map(({ id }) => id)
      .sort()
      .join('-')}-${safeTarget || 'unknown'}`;
  }
  expect(listViolations).toEqual([]);
  state.editorDiagnosticStage = 'accessibility-editor-open';
  await page
    .locator(`[data-testid="resume-row-${resumeID}"]`)
    .getByRole('link')
    .press('Enter');
  await setEditorTheme(page, 'light');
  await scanEditorAccessibility(state, page, 'accessibility-editor-light');
  await setEditorTheme(page, 'dark');
  await scanEditorAccessibility(state, page, 'accessibility-editor-dark');
  state.editorDiagnosticStage = 'accessibility-settings-open';
  await page.goto(`${ORIGIN}/app/settings/sessions`);
  await waitForHydration(page);
  state.editorDiagnosticStage = 'accessibility-settings-theme';
  await expect.poll(() => page.evaluate(
    () => document.documentElement.dataset.theme,
  )).toBe('dark');
  state.editorDiagnosticStage = 'accessibility-list-reopen';
  await page.goto(`${ORIGIN}/app/resumes`);
  await waitForHydration(page);
  state.editorDiagnosticStage = 'accessibility-list-theme';
  await expect.poll(() => page.evaluate(
    () => document.documentElement.dataset.theme,
  )).toBe('dark');
  state.editorDiagnosticStage = 'accessibility-editor-reopen';
  await page
    .locator(`[data-testid="resume-row-${resumeID}"]`)
    .getByRole('link')
    .press('Enter');
  await waitForHydration(page);
  state.editorDiagnosticStage = 'accessibility-theme-reset';
  await setEditorTheme(page, 'light');
}

export async function setEditorTheme(page: Page, target: 'light' | 'dark'): Promise<void> {
  await expect.poll(() => page.evaluate(
    () => document.documentElement.dataset.theme ?? '',
  )).not.toBe('');
  const current = await page.evaluate(
    () => document.documentElement.dataset.theme,
  );
  if (current === target) return;
  await page.getByRole('button', { name: 'Account menu' }).press('Enter');
  const menu = page.getByRole('menu');
  await expect(menu).toHaveCount(1);
  await menu.getByRole('menuitem', {
    name: target === 'dark' ? 'Dark theme' : 'Light theme',
    exact: true,
  }).press('Enter');
  await expect.poll(() => page.evaluate(
    () => document.documentElement.dataset.theme ?? '',
  )).toBe(target);
  await page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .map((animation) => animation.finished.catch(() => undefined)),
    );
  });
}

export async function scanEditorAccessibility(
  state: EditorProofState,
  page: Page,
  stage: string,
): Promise<void> {
  state.editorDiagnosticStage = stage;
  const findings = await new AxeBuilder({ page }).analyze();
  const violations = seriousOrCritical(findings.violations);
  if (violations.length > 0) {
    const first = findings.violations.find(({ id }) => id === violations[0]?.id);
    const safeTarget = JSON.stringify(first?.nodes[0]?.target ?? [])
      .replaceAll(/[0-9a-f]{8}-[0-9a-f-]{27}/gi, 'resume-id')
      .toLowerCase()
      .replaceAll(/[^a-z0-9-]+/g, '-')
      .replaceAll(/^-+|-+$/g, '')
      .slice(0, 120);
    state.editorDiagnosticStage = `${stage}-${violations
      .map(({ id }) => id)
      .sort()
      .join('-')}-${safeTarget || 'unknown'}`;
  }
  expect(violations).toEqual([]);
}

export function seriousOrCritical(
  violations: readonly {
    readonly id: string;
    readonly impact?: string | null;
  }[],
): readonly { readonly id: string; readonly impact?: string | null }[] {
  return violations.filter(
    ({ impact }) => impact === 'serious' || impact === 'critical',
  );
}
