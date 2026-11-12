import { expect, type Page, type Route } from '@playwright/test';
import {
  mutateRemoteHeadline,
  mutateRemoteMetadata,
  uniqueTitle,
  type AcceptedResume,
  type BrowserPersistenceProbe,
} from './editor-fixtures';
import {
  type EditorProofState,
  type Diagnostics,
  type URLBaseline,
  expectURLUnchanged,
  expectNoPersistenceWrites,
  expectSavedOrDiagnose,
  isTemplateChildRequest,
} from './editor-proof-state';

export async function proveConflictAndTemplate(
  state: EditorProofState,
  page: Page,
  accepted: AcceptedResume,
  diagnostics: Diagnostics,
  probes: BrowserPersistenceProbe,
  baseline: URLBaseline,
): Promise<void> {
  state.editorDiagnosticStage = 'conflict-safe-rebase';
  await page
    .getByRole('navigation', { name: 'Resume outline' })
    .getByRole('button', { name: 'Personal details', exact: true })
    .press('Enter');
  const personalPath
    = `/api/v1/resumes/${accepted.metadata.id}/personal-details`;
  diagnostics.expectHTTPFailure(
    'PATCH',
    personalPath,
    412,
  );
  const safeRebasePause = diagnostics.pauseNextMutation('PATCH', personalPath);
  const staleSafeRebase = page.waitForResponse((response) =>
    response.status() === 412
    && new URL(response.url()).pathname === personalPath,
  );
  const headline = page.getByLabel('Headline');
  await headline.fill('Safe stale rebase');
  await headline.press('Tab');
  state.editorDiagnosticStage = 'conflict-safe-local-submitted';
  await safeRebasePause.waitUntilPaused();
  state.editorDiagnosticStage = 'conflict-safe-local-paused';
  try {
    await mutateRemoteMetadata(page, accepted.metadata.id, uniqueTitle());
  } finally {
    safeRebasePause.release();
  }
  state.editorDiagnosticStage = 'conflict-safe-remote-complete';
  expect((await staleSafeRebase).status()).toBe(412);
  state.editorDiagnosticStage = 'conflict-safe-stale-complete';
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  state.editorDiagnosticStage = 'conflict-same-value';
  // A realtime refetch after the remote save could adopt the winner before
  // the paused write's 412 arrives. Reads of the resume issued after that
  // save are held until the 412 has set the read barrier, so the editor
  // itself must complete the barrier before it sends the next edit.
  const resumePath = `/api/v1/resumes/${accepted.metadata.id}`;
  let personalWrites = 0;
  let holdReads = false;
  let releaseReads = (): void => {};
  const readsReleased = new Promise<void>((resolve) => {
    releaseReads = resolve;
  });
  const sameValueRoutes = (url: URL): boolean =>
    url.pathname === personalPath || url.pathname === resumePath;
  const orderSameValue = async (route: Route): Promise<void> => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (pathname === personalPath && request.method() === 'PATCH') {
      personalWrites += 1;
      // The editor's write comes first; the second is the remote save.
      if (personalWrites === 2) holdReads = true;
    } else if (holdReads && request.method() === 'GET') {
      await readsReleased;
    }
    await route.fallback();
  };
  await page.route(sameValueRoutes, orderSameValue);
  try {
    diagnostics.expectHTTPFailure(
      'PATCH',
      personalPath,
      412,
    );
    const sameValuePause = diagnostics.pauseNextMutation('PATCH', personalPath);
    const staleSameValue = page.waitForResponse((response) =>
      response.status() === 412
      && new URL(response.url()).pathname === personalPath,
    );
    await headline.fill('Same value elsewhere');
    await headline.press('Tab');
    await sameValuePause.waitUntilPaused();
    try {
      await mutateRemoteHeadline(
        page,
        accepted.metadata.id,
        'Same value elsewhere',
      );
    } finally {
      sameValuePause.release();
    }
    expect((await staleSameValue).status()).toBe(412);
    await expect(page.getByTestId('save-status'))
      .toHaveAttribute('data-state', 'dirty');
  } finally {
    holdReads = false;
    releaseReads();
    await page.unroute(sameValueRoutes, orderSameValue);
  }
  const laterSave = page.waitForResponse((response) =>
    response.request().method() === 'PATCH'
    && new URL(response.url()).pathname === personalPath,
  );
  await headline.fill('Typed after the same value');
  await headline.press('Tab');
  expect((await laterSave).status()).toBe(200);
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  state.editorDiagnosticStage = 'conflict-accept-latest';
  diagnostics.expectHTTPFailure(
    'PATCH',
    personalPath,
    412,
  );
  const acceptLatestPause = diagnostics.pauseNextMutation('PATCH', personalPath);
  const staleAcceptLatest = page.waitForResponse((response) =>
    response.status() === 412
    && new URL(response.url()).pathname === personalPath,
  );
  await headline.fill('Local conflict value');
  await headline.press('Tab');
  await acceptLatestPause.waitUntilPaused();
  try {
    await mutateRemoteHeadline(
      page,
      accepted.metadata.id,
      'Remote conflict winner',
    );
  } finally {
    acceptLatestPause.release();
  }
  expect((await staleAcceptLatest).status()).toBe(412);
  const conflict = page.getByRole('status').filter({ hasText: 'Review changes' });
  await expect(conflict).toBeVisible();
  await conflict.getByRole('button', { name: 'Accept latest' }).press('Enter');
  await expect(conflict).toBeHidden();

  state.editorDiagnosticStage = 'conflict-apply-mine';
  diagnostics.expectHTTPFailure(
    'PATCH',
    personalPath,
    412,
  );
  const applyMinePause = diagnostics.pauseNextMutation('PATCH', personalPath);
  const staleApplyMine = page.waitForResponse((response) =>
    response.status() === 412
    && new URL(response.url()).pathname === personalPath,
  );
  await headline.fill('Apply mine value');
  await headline.press('Tab');
  await applyMinePause.waitUntilPaused();
  try {
    await mutateRemoteHeadline(
      page,
      accepted.metadata.id,
      'Second remote winner',
    );
  } finally {
    applyMinePause.release();
  }
  expect((await staleApplyMine).status()).toBe(412);
  await expect(page.getByRole('button', { name: 'Apply my value' })).toBeVisible();
  await page.getByRole('button', { name: 'Apply my value' }).press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);

  state.editorDiagnosticStage = 'template-apply-undo';
  await page.getByRole('button', { name: 'Templates' }).press('Enter');
  await applyTemplate(page, 'academic-dense');
  await expect(
    page.getByRole('status').filter({ hasText: 'Template saved' }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Undo template changes' }).press('Enter');
  await expect(
    page.getByRole('status').filter({ hasText: 'Template saved' }),
  ).toBeVisible();
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);

  state.editorDiagnosticStage = 'template-partial';
  diagnostics.armTemplateFailure();
  const firstTemplateChild = page.waitForResponse((response) =>
    isTemplateChildRequest(response.request()),
  );
  await applyTemplate(page, 'modern-sidebar');
  state.editorDiagnosticStage = 'template-partial-first-child';
  expect((await firstTemplateChild).status()).toBe(200);
  state.editorDiagnosticStage = 'template-partial-dialog';
  const partial = page.getByRole('alertdialog', { name: 'Template only partly applied' });
  await expect(partial).toBeVisible();
  await expect(partial.getByRole('button', { name: 'English', exact: true }))
    .toBeVisible();
  await expect(partial.getByRole('button', { name: 'Tiếng Việt', exact: true }))
    .toBeVisible();
  // The primary action comes last so it stacks first on phones.
  const partialActions = partial.locator(
    '[data-action="keep-partial"], [data-action="restore-pre-apply"], [data-action="retry-remaining"]',
  );
  await expect(partialActions).toHaveText([
    'Keep as is',
    'Undo template',
    'Apply the rest',
  ]);
  await partial.getByRole('button', { name: 'Keep as is' }).press('Enter');
  state.editorDiagnosticStage = 'template-keep-partial-saved';
  await expectSavedOrDiagnose(state, page, 'template-keep-partial');
  diagnostics.assertTemplateFailureSecondChild();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
}

export async function applyTemplate(page: Page, id: string): Promise<void> {
  const template = page.locator(`[data-template="${id}"]`);
  await expect(template).toBeVisible();
  await template.getByRole('button', { name: 'Apply' }).press('Enter');
}
