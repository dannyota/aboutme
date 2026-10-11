import { expect, type Page } from '@playwright/test';
import { deleteRemoteEntry, type BrowserPersistenceProbe } from './editor-fixtures';
import { waitForHydration } from './harness-lib';
import {
  ORIGIN,
  type EditorProofState,
  type Diagnostics,
  type URLBaseline,
  expectURLUnchanged,
  expectNoPersistenceWrites,
  isResumeMutation,
} from './editor-proof-state';

export async function proveKeyboardStructureAndContextActions(
  state: EditorProofState,
  page: Page,
  resumeID: string,
  diagnostics: Diagnostics,
  probes: BrowserPersistenceProbe,
  baseline: URLBaseline,
): Promise<void> {
  state.editorDiagnosticStage = 'structure-open';
  await page
    .getByRole('button', { name: '+ Add section', exact: true })
    .press('Enter');
  await page.getByLabel('Section type').selectOption('work');
  state.editorDiagnosticStage = 'structure-add-work';
  await page
    .getByTestId('section-create-form')
    .getByRole('button', { name: 'Add section', exact: true })
    .press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  // Adding a section opens it in the inspector; reopen Sections to add
  // the next one.
  await page
    .getByRole('button', { name: '+ Add section', exact: true })
    .press('Enter');
  await page.getByLabel('Section type').selectOption('skill');
  state.editorDiagnosticStage = 'structure-add-skill';
  await page
    .getByTestId('section-create-form')
    .getByRole('button', { name: 'Add section', exact: true })
    .press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await page
    .getByRole('button', { name: '+ Add section', exact: true })
    .press('Enter');
  state.editorDiagnosticStage = 'structure-move';
  await page.locator('[data-section="work"]').getByRole('button', { name: 'Move to sidebar' }).press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  state.editorDiagnosticStage = 'entries-open';
  await proveResponsiveEditorSurface(page, baseline.href);
  state.editorDiagnosticStage = 'customization-labels';
  await page.getByRole('button', { name: 'Design' }).press('Enter');
  for (const groupName of ['Type', 'Spacing', 'Headings', 'Layout', 'Colors']) {
    await expect(page.getByRole('group', { name: groupName })).toBeVisible();
  }
  const typeGroup = page.getByRole('group', { name: 'Type' });
  await expect(typeGroup.getByLabel('Font', { exact: true })).toBeVisible();
  await expect(typeGroup.getByLabel('Base size (px)', { exact: true }))
    .toBeVisible();
  const font = typeGroup.getByLabel('Font', { exact: true });
  await expect(font.getByRole('option', { name: 'Be Vietnam Pro', exact: true }))
    .toHaveAttribute('value', 'be-vietnam-pro');
  const currentFont = await font.inputValue();
  const fontSaved = currentFont === 'be-vietnam-pro'
    ? undefined
    : page.waitForResponse((response) =>
      isResumeMutation(response.request(), resumeID),
    );
  await font.selectOption({ label: 'Be Vietnam Pro' });
  if (fontSaved !== undefined) expect((await fontSaved).status()).toBe(200);
  await expect(page.getByTestId('save-status')).toHaveAttribute(
    'data-state',
    'saved',
  );

  state.editorDiagnosticStage = 'contact-document';
  state.editorDiagnosticStage = 'contact-personal-details';
  await page
    .getByRole('navigation', { name: 'Resume outline' })
    .getByRole('button', { name: 'Personal details', exact: true })
    .press('Enter');
  state.editorDiagnosticStage = 'contact-add';
  const detailAdded = page.waitForResponse(
    (response) => isResumeMutation(response.request(), resumeID),
    { timeout: 15_000 },
  );
  await page
    .getByRole('button', {
      name: 'Add detail',
      exact: true,
    })
    .press('Enter');
  state.editorDiagnosticStage = 'contact-add-save';
  expect((await detailAdded).status()).toBe(200);
  await expect(page.getByTestId('save-status')).toHaveAttribute(
    'data-state',
    'saved',
  );
  state.editorDiagnosticStage = 'contact-fill';
  const detailSaved = page.waitForResponse(
    (response) => isResumeMutation(response.request(), resumeID),
    { timeout: 15_000 },
  );
  const detailValue = page.getByLabel('Value', { exact: true });
  await detailValue.fill('editor-proof@example.invalid');
  await detailValue.press('Enter');
  state.editorDiagnosticStage = 'contact-save';
  expect((await detailSaved).status()).toBe(200);
  state.editorDiagnosticStage = 'contact-menu-button';
  const detailMenuButton = page.getByRole('button', {
    name: 'More options for contact detail 1',
    exact: true,
  });
  await expect(detailMenuButton).toBeVisible();
  await detailMenuButton.press('Enter');
  state.editorDiagnosticStage = 'contact-menu-open';
  const detailMenu = page.getByRole('menu');
  await expect(detailMenu).toHaveCount(1);
  state.editorDiagnosticStage = 'contact-menu-items';
  await expect(detailMenu.getByRole('menuitem', {
    name: 'Set label…',
    exact: true,
  })).toBeVisible();
  await expect(detailMenu.getByRole('menuitemcheckbox', {
    name: 'Hide this detail',
    exact: true,
  })).toBeVisible();
  await expect(detailMenu.getByRole('menuitem', {
    name: 'Move up',
    exact: true,
  })).toBeVisible();
  await expect(detailMenu.getByRole('menuitem', {
    name: 'Move down',
    exact: true,
  })).toBeVisible();
  await expect(detailMenu.getByRole('menuitem', {
    name: 'Remove detail',
    exact: true,
  })).toBeVisible();
  await detailMenu.getByRole('menuitem', {
    name: 'Set label…',
    exact: true,
  }).press('Enter');
  state.editorDiagnosticStage = 'contact-label';
  await expect(page.getByLabel('Label', { exact: true })).toBeVisible();

  state.editorDiagnosticStage = 'entries-open';
  await page.getByRole('navigation', { name: 'Resume outline' }).getByRole('button', { name: 'Experience' }).press('Enter');
  const firstEntryCreated = page.waitForResponse((response) =>
    isResumeMutation(response.request(), resumeID),
  );
  await page.getByRole('button', { name: 'Add entry' }).press('Enter');
  expect((await firstEntryCreated).status()).toBe(200);
  await expect(page.locator('[data-entry-id]').first()).toBeVisible();
  const firstTitle = page.locator('[data-entry-id]').first().getByLabel('Job title');
  const firstTitleSaved = page.waitForResponse((response) =>
    isResumeMutation(response.request(), resumeID),
  );
  await firstTitle.fill('First role');
  await firstTitle.press('Tab');
  expect((await firstTitleSaved).status()).toBe(200);
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  const secondEntryCreated = page.waitForResponse((response) =>
    isResumeMutation(response.request(), resumeID),
  );
  await page.getByRole('button', { name: 'Add entry' }).press('Enter');
  expect((await secondEntryCreated).status()).toBe(200);
  await expect(page.locator('[data-entry-id]').nth(1)).toBeVisible();
  const secondTitle = page.locator('[data-entry-id]').nth(1).getByLabel('Job title');
  const secondTitleSaved = page.waitForResponse((response) =>
    isResumeMutation(response.request(), resumeID),
  );
  await secondTitle.fill('Second role');
  await secondTitle.press('Tab');
  expect((await secondTitleSaved).status()).toBe(200);
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  const deletedEntryID = await requiredAttribute(page.locator('[data-entry-id]').first(), 'data-entry-id');
  diagnostics.expectHTTPFailure(
    'PATCH',
    `/api/v1/resumes/${resumeID}/entries/work`,
    412,
  );
  const pausedEntryPatch = diagnostics.pauseNextMutation(
    'PATCH',
    `/api/v1/resumes/${resumeID}/entries/work`,
  );
  const staleEntryMutation = page.waitForResponse((response) =>
    response.status() === 412
    && isResumeMutation(response.request(), resumeID),
  );
  await firstTitle.fill('Deleted remotely');
  await firstTitle.press('Tab');
  await pausedEntryPatch.waitUntilPaused();
  state.editorDiagnosticStage = 'entries-missing-conflict';
  try {
    await deleteRemoteEntry(page, resumeID, 'work', deletedEntryID);
  } finally {
    pausedEntryPatch.release();
  }
  expect((await staleEntryMutation).status()).toBe(412);
  const saveStatus = page.locator('[role="status"][data-state]');
  await expect.poll(
    async () => saveStatus.getAttribute('data-state'),
  ).not.toBe('saving');
  const saveState = await saveStatus.getAttribute('data-state');
  state.editorDiagnosticStage = saveState === 'conflict'
    ? 'entries-missing-state-conflict'
    : saveState === 'error'
      ? 'entries-missing-state-error'
      : saveState === 'saving'
        ? 'entries-missing-state-saving'
        : 'entries-missing-state-other';
  const missingConflict = page.locator('[data-conflict]').first();
  await expect(missingConflict).toBeVisible();
  const missingConflictKind = await missingConflict.getAttribute('data-conflict');
  state.editorDiagnosticStage = missingConflictKind === 'membership-changed:entryField'
    ? 'entries-missing-select'
    : missingConflictKind === 'target-changed:entryUpsert'
      ? 'entries-missing-recreate'
      : 'entries-missing-unexpected';
  const selectAnother = page.getByRole('button', {
    name: 'Select another entry',
  });
  const selectAnotherCount = await selectAnother.count();
  state.editorDiagnosticStage
    = `entries-missing-select-count-${Math.min(selectAnotherCount, 3)}`;
  await expect(selectAnother).toHaveCount(1);
  await expect(selectAnother).toBeVisible();
  await selectAnother.press('Enter');

  const reorderEntryCreated = page.waitForResponse((response) =>
    isResumeMutation(response.request(), resumeID),
  );
  await page.getByRole('button', { name: 'Add entry' }).press('Enter');
  expect((await reorderEntryCreated).status()).toBe(200);
  await expect(page.locator('[data-entry-id]').nth(1)).toBeVisible();
  const reorderTitle = page.locator('[data-entry-id]').nth(1).getByLabel('Job title');
  const reorderTitleSaved = page.waitForResponse((response) =>
    isResumeMutation(response.request(), resumeID),
  );
  await reorderTitle.fill('Reorder role');
  await reorderTitle.press('Tab');
  expect((await reorderTitleSaved).status()).toBe(200);
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  const remoteDeletedID = await requiredAttribute(page.locator('[data-entry-id]').nth(1), 'data-entry-id');
  state.editorDiagnosticStage = 'entries-reorder-conflict';
  await page
    .getByRole('button', { name: '+ Add section', exact: true })
    .press('Enter');
  const structurePath = `/api/v1/resumes/${resumeID}/sections/work`;
  diagnostics.expectHTTPFailure(
    'PATCH',
    structurePath,
    412,
  );
  const reorderPause = diagnostics.pauseNextMutation('PATCH', structurePath);
  const staleReorder = page.waitForResponse((response) =>
    response.status() === 412
    && new URL(response.url()).pathname === structurePath,
  );
  await page.locator('[data-entry-order="work"]').getByRole('button', { name: 'Move entry down' }).first().press('Enter');
  await reorderPause.waitUntilPaused();
  try {
    await deleteRemoteEntry(page, resumeID, 'work', remoteDeletedID);
  } finally {
    reorderPause.release();
  }
  expect((await staleReorder).status()).toBe(412);
  await expect(page.getByRole('button', { name: 'Reopen entry order' })).toBeVisible();
  await page.getByRole('button', { name: 'Reopen entry order' }).press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
}

export async function proveResponsiveEditorSurface(
  page: Page,
  href: string,
): Promise<void> {
  const narrowPage = await page.context().newPage();
  try {
    await narrowPage.setViewportSize({ width: 390, height: 844 });
    await narrowPage.goto(href);
    await waitForHydration(narrowPage);
    await expect(narrowPage.getByTestId('save-status')).toBeVisible();

    const switcher = narrowPage.getByRole('tablist', { name: 'Editor view' });
    await expect(switcher).toBeVisible();
    await expect(switcher).toHaveCSS('position', 'fixed');
    const editTab = switcher.getByRole('tab', { name: 'Edit', exact: true });
    const previewTab = switcher.getByRole('tab', {
      name: 'Preview',
      exact: true,
    });
    await expect(editTab).toHaveAttribute('data-action', 'show-editor');
    await expect(previewTab).toHaveAttribute('data-action', 'show-preview');

    await previewTab.press('Enter');
    await expect(previewTab).toHaveAttribute('aria-pressed', 'true');
    const sheet = narrowPage.getByTestId('preview-sheet');
    await expect(sheet).toBeVisible();
    await expect.poll(async () =>
      Number(await sheet.getAttribute('data-sheet-zoom')),
    ).toBeLessThan(1);
    await expect.poll(async () =>
      Number(await sheet.getAttribute('data-scaled-width')),
    ).toBeLessThan(390);
    await expect(narrowPage.getByTestId('page-count')).not.toContainText('—');

    await editTab.press('Enter');
    const inspector = narrowPage.locator('[data-region="inspector"]');
    await expect(inspector).toHaveAttribute('data-narrow-active', 'true');
    const dimensions = await inspector.evaluate((element) => ({
      clientWidth: element.clientWidth,
      scrollWidth: element.scrollWidth,
    }));
    expect(dimensions.clientWidth).toBeLessThanOrEqual(390);
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  } finally {
    await narrowPage.close();
  }
}

export async function deleteThroughListKeyboard(
  state: EditorProofState,
  page: Page,
  resumeID: string,
): Promise<void> {
  state.editorDiagnosticStage = 'teardown-list';
  await page.goto('/app/resumes');
  const row = page.locator(`[data-testid="resume-row-${resumeID}"]`);
  await expect(row).toBeVisible();
  state.editorDiagnosticStage = 'teardown-open';
  const menuButton = row.getByRole('button', {
    name: /^More actions for /,
  });
  const menuLabel = await menuButton.getAttribute('aria-label');
  expect(menuLabel).toMatch(/^More actions for .+$/);
  const title = menuLabel?.slice('More actions for '.length) ?? '';
  await menuButton.press('Enter');
  const rowMenu = page.getByRole('menu');
  await expect(rowMenu).toHaveCount(1);
  await rowMenu
    .getByRole('menuitem', { name: `Delete ${title}`, exact: true })
    .press('Enter');
  const dialog = page.getByRole('alertdialog', { name: 'Delete resume' });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Type DELETE to confirm').fill('DELETE');
  const deletion = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'DELETE'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}`;
  });
  state.editorDiagnosticStage = 'teardown-confirm';
  const confirm = dialog.getByRole('button', { name: 'Delete' });
  await expect(confirm).toBeEnabled();
  await confirm.press('Enter');
  const response = await Promise.race([
    deletion,
    page.waitForTimeout(3_000).then(() => null),
  ]);
  if (response === null) {
    const dialogState = await dialog.isVisible() ? 'open' : 'closed';
    state.editorDiagnosticStage = `teardown-no-dispatch-dialog-${dialogState}`;
    throw new Error('resume delete did not dispatch');
  }
  expect(response.status()).toBe(204);
  state.editorDiagnosticStage = 'teardown-row';
  await expect(row).toHaveCount(0);
  state.editorDiagnosticStage = 'teardown-complete';
}

export async function requiredAttribute(
  locator: ReturnType<Page['locator']>,
  name: string,
): Promise<string> {
  const value = await locator.getAttribute(name);
  expect(value).not.toBeNull();
  return value as string;
}
