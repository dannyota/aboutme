import { AxeBuilder } from '@axe-core/playwright';
import {
  expect,
  test,
  type BrowserContext,
  type Page,
  type Request,
} from '@playwright/test';
import { writeFile } from 'node:fs/promises';

import {
  createBlankResume,
  deleteRecordedResume,
  deleteRemoteEntry,
  installBrowserPersistenceProbes,
  installPhotoSourceReadProbes,
  loginAsDevelopmentUser,
  mutateRemoteHeadline,
  mutateRemoteMetadata,
  ownerPhotoHasNoCrop,
  replaceRemotePhoto,
  settledVisiblePageCount,
  uniqueTitle,
  type AcceptedResume,
  type BrowserPersistenceProbe,
} from './editor-fixtures';
import {
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  waitForHydration,
} from './harness-lib';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
  isExpectedNegativeHTTPConsole,
  httpFailureStatus,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/editor-proof.json';
const SCHEMA_VERSION = '2';
const VALID_PNG_BASE64
  = 'iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAANElEQVR4nOzNsQkA'
    + 'MAwDQRWGrJn9pwjZweruEWpvknuS3uZfMwAAAAAAAAAAALDVCwAA///3/wKTiM0y'
    + 'DAAAAABJRU5ErkJggg==';
let editorDiagnosticStage = 'setup';

interface EditorSteps {
  accessibility: boolean;
  auth: boolean;
  autosave: boolean;
  cache: boolean;
  conflict: boolean;
  etag: boolean;
  ifMatch: boolean;
  persistence: boolean;
  photo: boolean;
  session: boolean;
  teardown: boolean;
  template: boolean;
}

interface Diagnostics {
  assertClean(): void;
  assertTemplateFailureSecondChild(): void;
  armPhotoReadFailure(): void;
  armTemplateFailure(): void;
  expectHTTPFailure(method: string, pathname: string, status: 401 | 412): void;
  pauseNextMutation(method: string, pathname: string): PausedPhotoRequest;
  pauseNextPhotoCrop(): PausedPhotoRequest;
  pauseNextPhotoUpload(): PausedPhotoUpload;
}

interface PausedPhotoRequest {
  release(): void;
  waitUntilPaused(): Promise<void>;
}

type PausedPhotoUpload = PausedPhotoRequest;

interface URLBaseline {
  readonly hash: string;
  readonly href: string;
  readonly search: string;
}

interface EditorScenario {
  readonly accepted: AcceptedResume;
  readonly baseline: URLBaseline;
  readonly probes: BrowserPersistenceProbe;
}

test('proves authenticated editor behavior over trusted HTTPS', async ({
  context,
  page,
}) => {
  const createdIDs = new Set<string>();
  const steps: EditorSteps = {
    auth: false,
    cache: false,
    etag: false,
    ifMatch: false,
    autosave: false,
    conflict: false,
    template: false,
    photo: false,
    session: false,
    persistence: false,
    accessibility: false,
    teardown: false,
  };
  const diagnostics = await installDiagnostics(context, page);
  let cleanupPage: Page | undefined;
  let reauthPage: Page | undefined;
  let scenarioSucceeded = false;

  try {
    await loginAsDevelopmentUser(page);
    steps.auth = true;
    const scenario = await proveListLoadAutosave(
      page,
      context,
      createdIDs,
      steps,
    );
    await proveKeyboardStructureAndContextActions(
      page,
      scenario.accepted.metadata.id,
      diagnostics,
      scenario.probes,
      scenario.baseline,
    );
    editorDiagnosticStage = 'conflict-template';
    await proveConflictAndTemplate(
      page,
      scenario.accepted,
      diagnostics,
      scenario.probes,
      scenario.baseline,
    );
    steps.conflict = true;
    steps.template = true;
    editorDiagnosticStage = 'photo-session';
    reauthPage = await provePhotoSessionPersistence(
      page,
      context,
      scenario.accepted.metadata.id,
      diagnostics,
      scenario.probes,
      scenario.baseline,
    );
    steps.photo = true;
    steps.session = true;
    steps.persistence = true;
    editorDiagnosticStage = 'accessibility';
    await proveAccessibility(page, scenario.accepted.metadata.id);
    steps.accessibility = true;
    editorDiagnosticStage = 'teardown';
    await deleteThroughListKeyboard(page, scenario.accepted.metadata.id);
    createdIDs.delete(scenario.accepted.metadata.id);
    steps.teardown = true;
    scenarioSucceeded = true;
  } finally {
    if (!scenarioSucceeded) {
      process.stderr.write(`editor-stage:${editorDiagnosticStage}\n`);
    }
    if (reauthPage !== undefined && !reauthPage.isClosed()) await reauthPage.close();
    if (createdIDs.size > 0) {
      cleanupPage = await context.newPage();
      try {
        await ensureAuthenticated(cleanupPage);
        for (const id of createdIDs) await deleteRecordedResume(cleanupPage, id);
      } finally {
        await cleanupPage.close();
      }
    }
  }

  try {
    editorDiagnosticStage = 'post-scenario';
    expect(scenarioSucceeded).toBe(true);
    editorDiagnosticStage = 'post-diagnostics';
    diagnostics.assertClean();
    editorDiagnosticStage = 'post-steps';
    expect(steps).toEqual({
      auth: true,
      cache: true,
      etag: true,
      ifMatch: true,
      autosave: true,
      conflict: true,
      template: true,
      photo: true,
      session: true,
      persistence: true,
      accessibility: true,
      teardown: true,
    });
    editorDiagnosticStage = 'post-evidence';
    await writeEditorEvidence(steps);
  } catch (error) {
    process.stderr.write(`editor-stage:${editorDiagnosticStage}\n`);
    throw error;
  }
});

async function proveListLoadAutosave(
  page: Page,
  context: BrowserContext,
  createdIDs: Set<string>,
  steps: EditorSteps,
): Promise<EditorScenario> {
  editorDiagnosticStage = 'list-create-resume';
  const initialTitle = uniqueTitle();
  const created = await createBlankResume(page, initialTitle);
  createdIDs.add(created.metadata.id);

  editorDiagnosticStage = 'list-open';
  await page.goto('/app/resumes');
  const row = page.locator(`[data-testid="resume-row-${created.metadata.id}"]`);
  await expect(row).toBeVisible();
  editorDiagnosticStage = 'list-rename-open';
  await row
    .getByRole('button', {
      name: `More actions for ${initialTitle}`,
      exact: true,
    })
    .press('Enter');
  const rowMenu = page.getByRole('menu');
  await expect(rowMenu).toHaveCount(1);
  await rowMenu
    .getByRole('menuitem', { name: `Rename ${initialTitle}`, exact: true })
    .press('Enter');
  const rename = page.getByRole('dialog', { name: 'Rename resume' });
  const renamedTitle = uniqueTitle();
  await rename.getByLabel('Title').fill(renamedTitle);
  const renameMutation = page.waitForResponse((response) =>
    isResumeMutation(response.request(), created.metadata.id),
  );
  editorDiagnosticStage = 'list-rename-save';
  await rename.getByRole('button', { name: 'Save' }).press('Enter');
  expect((await renameMutation).status()).toBe(200);
  await expect(rename).toBeHidden();
  editorDiagnosticStage = 'list-reload';
  await page.reload();
  await expect(row.getByRole('link')).toContainText(renamedTitle);

  editorDiagnosticStage = 'editor-open';
  const probes = await installBrowserPersistenceProbes(page);
  const ownerRead = page.waitForResponse((response) => isOwnerRead(response.request(), created.metadata.id));
  const headers = await captureOwnerReadHeaders(page, created.metadata.id);
  await page.goto(`/app/resumes/${created.metadata.id}`);
  const response = await ownerRead;
  expect(response.headers()['cache-control']).toBe('no-store, no-transform');
  const observedETag = response.headers().etag;
  expect(observedETag).toMatch(/^"r[1-9][0-9]*"$/);
  const body = await response.json() as { data?: { schemaVersion?: unknown } };
  expect(body.data?.schemaVersion).toBe(Number(SCHEMA_VERSION));
  await expect(page.locator('[data-resume-title]')).toHaveText(renamedTitle);
  await expect.poll(headers.acceptEncoding).not.toBe('');
  const pageCountMark = page.getByTestId('page-count');
  await expect(pageCountMark).toBeVisible();
  await expect.poll(async () => {
    const settled = await settledVisiblePageCount(page);
    const displayed = (await pageCountMark.textContent())?.trim();
    const expected = `${settled} page${settled === 1 ? '' : 's'}`;
    return displayed === expected ? settled : -1;
  }).toBeGreaterThan(0);
  const baseline = await readURLBaseline(page);
  await probes.reset();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  steps.cache = true;
  steps.etag = true;

  editorDiagnosticStage = 'autosave';
  const headline = page.getByLabel('Headline');
  await headline.evaluate((element) => {
    performance.clearResourceTimings();
    element.addEventListener('input', () => {
      Reflect.set(globalThis, '__aboutmeAutosaveInputAt', performance.now());
    }, { capture: true, once: true });
  });
  const mutationRequest = page.waitForRequest((request) =>
    isResumeMutation(request, created.metadata.id),
  );
  await headline.fill('Autosaved headline');
  await headline.press('Tab');
  const autosaveRequest = await mutationRequest;
  editorDiagnosticStage = 'autosave-request';
  expect(autosaveRequest.headers()['if-match']).toBe(observedETag);
  editorDiagnosticStage = 'autosave-saved-state';
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  editorDiagnosticStage = 'autosave-delay';
  const autosaveDelay = await page.evaluate((requestURL) => {
    const inputAt = Reflect.get(globalThis, '__aboutmeAutosaveInputAt');
    Reflect.deleteProperty(globalThis, '__aboutmeAutosaveInputAt');
    const requests = performance.getEntriesByName(requestURL, 'resource');
    const request = requests.at(-1);
    if (typeof inputAt !== 'number' || request === undefined) return -1;
    return request.startTime - inputAt;
  }, autosaveRequest.url());
  expect(autosaveDelay).toBeGreaterThanOrEqual(1000);
  editorDiagnosticStage = 'autosave-complete';
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  await test.step('loads the accepted autosave in a fresh page', async () => {
    editorDiagnosticStage = 'persistence-new-page';
    const verificationPage = await context.newPage();
    editorDiagnosticStage = 'persistence-install-probes';
    const verificationProbes = await installBrowserPersistenceProbes(
      verificationPage,
    );
    try {
      editorDiagnosticStage = 'persistence-navigate';
      const loaded = await verificationPage.goto(baseline.href, {
        timeout: 10_000,
        waitUntil: 'domcontentloaded',
      });
      expect(loaded?.status()).toBe(200);
      editorDiagnosticStage = 'persistence-headline';
      await expect(verificationPage.getByLabel('Headline')).toHaveValue(
        'Autosaved headline',
      );
      editorDiagnosticStage = 'persistence-reset';
      await verificationProbes.reset();
      editorDiagnosticStage = 'persistence-assert';
      await expectURLUnchanged(verificationPage, baseline);
      await expectNoPersistenceWrites(verificationProbes);
    } finally {
      editorDiagnosticStage = 'persistence-close';
      await verificationPage.close();
    }
  }, { timeout: 15_000 });
  editorDiagnosticStage = 'structure';
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  steps.ifMatch = true;
  steps.autosave = true;
  return { accepted: created, baseline, probes };
}

async function proveConflictAndTemplate(
  page: Page,
  accepted: AcceptedResume,
  diagnostics: Diagnostics,
  probes: BrowserPersistenceProbe,
  baseline: URLBaseline,
): Promise<void> {
  editorDiagnosticStage = 'conflict-safe-rebase';
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
  editorDiagnosticStage = 'conflict-safe-local-submitted';
  await safeRebasePause.waitUntilPaused();
  editorDiagnosticStage = 'conflict-safe-local-paused';
  try {
    await mutateRemoteMetadata(page, accepted.metadata.id, uniqueTitle());
  } finally {
    safeRebasePause.release();
  }
  editorDiagnosticStage = 'conflict-safe-remote-complete';
  expect((await staleSafeRebase).status()).toBe(412);
  editorDiagnosticStage = 'conflict-safe-stale-complete';
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  editorDiagnosticStage = 'conflict-accept-latest';
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

  editorDiagnosticStage = 'conflict-apply-mine';
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

  editorDiagnosticStage = 'template-apply-undo';
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

  editorDiagnosticStage = 'template-partial';
  diagnostics.armTemplateFailure();
  const firstTemplateChild = page.waitForResponse((response) =>
    isTemplateChildRequest(response.request()),
  );
  await applyTemplate(page, 'modern-sidebar');
  editorDiagnosticStage = 'template-partial-first-child';
  expect((await firstTemplateChild).status()).toBe(200);
  editorDiagnosticStage = 'template-partial-dialog';
  const partial = page.getByRole('alertdialog', { name: 'Template changes need review' });
  await expect(partial).toBeVisible();
  await expect(partial.getByRole('button')).toHaveText([
    'Retry remaining',
    'Restore pre-apply',
    'Keep partial',
  ]);
  await partial.getByRole('button', { name: 'Keep partial' }).press('Enter');
  diagnostics.assertTemplateFailureSecondChild();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
}

async function proveKeyboardStructureAndContextActions(
  page: Page,
  resumeID: string,
  diagnostics: Diagnostics,
  probes: BrowserPersistenceProbe,
  baseline: URLBaseline,
): Promise<void> {
  editorDiagnosticStage = 'structure-open';
  await page
    .getByRole('button', { name: '+ Add section', exact: true })
    .press('Enter');
  await page.getByLabel('Section type').selectOption('work');
  editorDiagnosticStage = 'structure-add-work';
  await page
    .getByTestId('section-create-form')
    .getByRole('button', { name: 'Add section', exact: true })
    .press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await page.getByLabel('Section type').selectOption('skill');
  editorDiagnosticStage = 'structure-add-skill';
  await page
    .getByTestId('section-create-form')
    .getByRole('button', { name: 'Add section', exact: true })
    .press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  editorDiagnosticStage = 'structure-move';
  await page.locator('[data-section="work"]').getByRole('button', { name: 'Move to sidebar' }).press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  editorDiagnosticStage = 'entries-open';
  await proveResponsiveEditorSurface(page, baseline.href);
  editorDiagnosticStage = 'customization-labels';
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

  editorDiagnosticStage = 'contact-document';
  editorDiagnosticStage = 'contact-personal-details';
  await page
    .getByRole('navigation', { name: 'Resume outline' })
    .getByRole('button', { name: 'Personal details', exact: true })
    .press('Enter');
  editorDiagnosticStage = 'contact-add';
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
  editorDiagnosticStage = 'contact-add-save';
  expect((await detailAdded).status()).toBe(200);
  await expect(page.getByTestId('save-status')).toHaveAttribute(
    'data-state',
    'saved',
  );
  editorDiagnosticStage = 'contact-fill';
  const detailSaved = page.waitForResponse(
    (response) => isResumeMutation(response.request(), resumeID),
    { timeout: 15_000 },
  );
  const detailValue = page.getByLabel('Value', { exact: true });
  await detailValue.fill('editor-proof@example.invalid');
  await detailValue.press('Enter');
  editorDiagnosticStage = 'contact-save';
  expect((await detailSaved).status()).toBe(200);
  editorDiagnosticStage = 'contact-menu-button';
  const detailMenuButton = page.getByRole('button', {
    name: 'More options for contact detail 1',
    exact: true,
  });
  await expect(detailMenuButton).toBeVisible();
  await detailMenuButton.press('Enter');
  editorDiagnosticStage = 'contact-menu-open';
  const detailMenu = page.getByRole('menu');
  await expect(detailMenu).toHaveCount(1);
  editorDiagnosticStage = 'contact-menu-items';
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
  editorDiagnosticStage = 'contact-label';
  await expect(page.getByLabel('Label', { exact: true })).toBeVisible();

  editorDiagnosticStage = 'entries-open';
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
  editorDiagnosticStage = 'entries-missing-conflict';
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
  editorDiagnosticStage = saveState === 'conflict'
    ? 'entries-missing-state-conflict'
    : saveState === 'error'
      ? 'entries-missing-state-error'
      : saveState === 'saving'
        ? 'entries-missing-state-saving'
        : 'entries-missing-state-other';
  const missingConflict = page.locator('[data-conflict]').first();
  await expect(missingConflict).toBeVisible();
  const missingConflictKind = await missingConflict.getAttribute('data-conflict');
  editorDiagnosticStage = missingConflictKind === 'membership-changed:entryField'
    ? 'entries-missing-select'
    : missingConflictKind === 'target-changed:entryUpsert'
      ? 'entries-missing-recreate'
      : 'entries-missing-unexpected';
  const selectAnother = page.getByRole('button', {
    name: 'Select another entry',
  });
  const selectAnotherCount = await selectAnother.count();
  editorDiagnosticStage
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
  editorDiagnosticStage = 'entries-reorder-conflict';
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

async function provePhotoSessionPersistence(
  page: Page,
  context: BrowserContext,
  resumeID: string,
  diagnostics: Diagnostics,
  probes: BrowserPersistenceProbe,
  baseline: URLBaseline,
): Promise<Page> {
  editorDiagnosticStage = 'photo-open';
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  await page.getByRole('button', { name: 'Photo' }).press('Enter');
  const upload = page.getByLabel('Upload photo');
  await expect(page.locator('[data-photo-preview] img')).toHaveCount(0);
  const sourceReads = await installPhotoSourceReadProbes(page);
  const uploadPause = diagnostics.pauseNextPhotoUpload();
  const ownerPhotoRead = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'GET'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  const acceptedPhoto = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'POST'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await upload.setInputFiles({
    buffer: Buffer.from(VALID_PNG_BASE64, 'base64'),
    mimeType: 'image/png',
    name: 'editor-proof.png',
  });
  editorDiagnosticStage = 'photo-upload-paused';
  await uploadPause.waitUntilPaused();
  expect(await sourceReads.read()).toEqual({
    blobArrayBuffer: 0,
    dataURL: 0,
    fileReader: 0,
    imageDecode: 0,
    objectURL: 0,
  });
  await expect(page.locator('[data-photo-preview] img')).toHaveCount(0);
  uploadPause.release();
  editorDiagnosticStage = 'photo-upload-accepted';
  expect((await acceptedPhoto).status()).toBe(200);
  expect((await ownerPhotoRead).headers()['content-type']).toMatch(/^image\/(?:jpeg|png)/);
  await expect(page.locator('[data-photo-preview] img')).toBeVisible();
  await expect.poll(async () => (await sourceReads.read()).dataURL).toBeGreaterThan(0);
  await expectURLUnchanged(page, baseline);
  editorDiagnosticStage = 'photo-crop';
  const acceptedCrop = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'PATCH'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await page.getByLabel('Width').fill('0.75');
  await page.getByRole('button', { name: 'Save crop' }).press('Enter');
  expect((await acceptedCrop).status()).toBe(200);
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);

  editorDiagnosticStage = 'photo-remote-conflict';
  diagnostics.expectHTTPFailure(
    'PATCH',
    `/api/v1/resumes/${resumeID}/photo`,
    412,
  );
  const cropPause = diagnostics.pauseNextPhotoCrop();
  const staleCrop = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'PATCH'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await page.getByLabel('Width').fill('0.5');
  await expect(page.getByLabel('Width')).toHaveValue('0.5');
  await page.getByRole('button', { name: 'Save crop' }).press('Enter');
  editorDiagnosticStage = 'photo-remote-conflict-submitted';
  const cropDispatched = await Promise.race([
    cropPause.waitUntilPaused().then(() => true),
    page.waitForTimeout(3_000).then(() => false),
  ]);
  if (!cropDispatched) {
    const draftWidth = await page.getByLabel('Width').inputValue();
    const invalid = await page.getByText(
      'Enter a crop within the image bounds.',
      { exact: true },
    ).isVisible();
    editorDiagnosticStage = invalid
      ? 'photo-remote-conflict-invalid'
      : draftWidth === '0.5'
        ? 'photo-remote-conflict-not-dispatched'
        : 'photo-remote-conflict-reset';
    throw new Error('photo crop did not dispatch');
  }
  editorDiagnosticStage = 'photo-remote-conflict-queued';
  editorDiagnosticStage = 'photo-remote-conflict-winner';
  await replaceRemotePhoto(page, resumeID, VALID_PNG_BASE64);
  cropPause.release();
  editorDiagnosticStage = 'photo-remote-conflict-response';
  expect((await staleCrop).status()).toBe(412);
  editorDiagnosticStage = 'photo-remote-conflict-reopen';
  await expect(page.locator('[data-state="conflict"]')).toBeVisible();
  editorDiagnosticStage = 'photo-remote-conflict-state';
  const photoPanel = page.locator('section[aria-labelledby="photo-title"]');
  const reopenCrop = photoPanel.getByRole('button', { name: 'Reopen crop' });
  await expect(reopenCrop).toBeVisible();
  editorDiagnosticStage = 'photo-remote-conflict-visible';
  await reopenCrop.press('Enter');
  editorDiagnosticStage = 'photo-remote-conflict-accepted';
  await expect(page.getByLabel('Replace photo')).toBeVisible();
  editorDiagnosticStage = 'photo-read-failure';
  diagnostics.armPhotoReadFailure();
  const replacementAccepted = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'POST'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await page.getByLabel('Replace photo').setInputFiles({
    buffer: Buffer.from(VALID_PNG_BASE64, 'base64'),
    mimeType: 'image/png',
    name: 'editor-proof-replacement.png',
  });
  expect((await replacementAccepted).status()).toBe(200);
  await expect(page.getByText('Photo preview is unavailable.', { exact: true })).toBeVisible();
  expect(await ownerPhotoHasNoCrop(page, resumeID)).toBe(true);
  await expect(page.getByLabel('Replace photo')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete photo' })).toBeVisible();

  editorDiagnosticStage = 'photo-delete';
  await page.getByRole('button', { name: 'Delete photo' }).press('Enter');
  const deleteDialog = page.getByRole('alertdialog', { name: 'Delete photo' });
  await expect(deleteDialog).toBeVisible();
  const deleteAccepted = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'DELETE'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await deleteDialog.getByRole('button', { name: 'Delete photo' }).press('Enter');
  expect((await deleteAccepted).status()).toBe(204);
  await expect(page.locator('[data-photo-preview] img')).toHaveCount(0);
  await expect(page.getByLabel('Upload photo')).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);

  editorDiagnosticStage = 'session-personal-details';
  await page
    .getByRole('navigation', { name: 'Resume outline' })
    .getByRole('button', { name: 'Personal details', exact: true })
    .press('Enter');
  await expect(page.getByLabel('Headline')).toBeVisible();
  editorDiagnosticStage = 'session-destroyed';
  const reauthPage = await context.newPage();
  editorDiagnosticStage = 'session-destroy-request';
  await destroySessionInSecondPage(reauthPage);
  editorDiagnosticStage = 'session-local-edit';
  diagnostics.expectHTTPFailure(
    'PATCH',
    `/api/v1/resumes/${resumeID}/personal-details`,
    401,
  );
  await page.getByLabel('Headline').fill('Retained after session loss');
  editorDiagnosticStage = 'session-local-edit-blur';
  await page.getByLabel('Headline').press('Tab');
  editorDiagnosticStage = 'session-loss-alert';
  await expect(page.getByRole('alertdialog', { name: 'Sign in to continue editing' })).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  editorDiagnosticStage = 'session-reauthenticated';
  await loginAsDevelopmentUser(reauthPage);
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  await expect(page.getByLabel('Headline')).toHaveValue('Retained after session loss');
  await page.getByRole('button', { name: 'Resume after sign-in' }).press('Enter');
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await expect(page.getByLabel('Headline')).toHaveValue('Retained after session loss');
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  return reauthPage;
}

async function proveResponsiveEditorSurface(
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

async function proveAccessibility(page: Page, resumeID: string): Promise<void> {
  editorDiagnosticStage = 'accessibility-list';
  await page.getByRole('link', { name: 'aboutme' }).press('Enter');
  await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();
  await expect(page.locator(`[data-testid="resume-row-${resumeID}"]`)).toBeVisible();
  editorDiagnosticStage = 'accessibility-list-audit';
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
    editorDiagnosticStage = `accessibility-list-${listViolations
      .map(({ id }) => id)
      .sort()
      .join('-')}-${safeTarget || 'unknown'}`;
  }
  expect(listViolations).toEqual([]);
  editorDiagnosticStage = 'accessibility-editor-open';
  await page
    .locator(`[data-testid="resume-row-${resumeID}"]`)
    .getByRole('link')
    .press('Enter');
  await setEditorTheme(page, 'light');
  await scanEditorAccessibility(page, 'accessibility-editor-light');
  await setEditorTheme(page, 'dark');
  await scanEditorAccessibility(page, 'accessibility-editor-dark');
  editorDiagnosticStage = 'accessibility-settings-open';
  await page.goto(`${ORIGIN}/app/settings/sessions`);
  await waitForHydration(page);
  editorDiagnosticStage = 'accessibility-settings-theme';
  await expect.poll(() => page.evaluate(
    () => document.documentElement.dataset.theme,
  )).toBe('dark');
  editorDiagnosticStage = 'accessibility-list-reopen';
  await page.goto(`${ORIGIN}/app/resumes`);
  await waitForHydration(page);
  editorDiagnosticStage = 'accessibility-list-theme';
  await expect.poll(() => page.evaluate(
    () => document.documentElement.dataset.theme,
  )).toBe('dark');
  editorDiagnosticStage = 'accessibility-editor-reopen';
  await page
    .locator(`[data-testid="resume-row-${resumeID}"]`)
    .getByRole('link')
    .press('Enter');
  await waitForHydration(page);
  editorDiagnosticStage = 'accessibility-theme-reset';
  await setEditorTheme(page, 'light');
}

async function setEditorTheme(page: Page, target: 'light' | 'dark'): Promise<void> {
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

async function scanEditorAccessibility(page: Page, stage: string): Promise<void> {
  editorDiagnosticStage = stage;
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
    editorDiagnosticStage = `${stage}-${violations
      .map(({ id }) => id)
      .sort()
      .join('-')}-${safeTarget || 'unknown'}`;
  }
  expect(violations).toEqual([]);
}

async function deleteThroughListKeyboard(page: Page, resumeID: string): Promise<void> {
  editorDiagnosticStage = 'teardown-list';
  await page.goto('/app/resumes');
  const row = page.locator(`[data-testid="resume-row-${resumeID}"]`);
  await expect(row).toBeVisible();
  editorDiagnosticStage = 'teardown-open';
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
  await dialog.getByLabel('Current title').fill(title);
  const deletion = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'DELETE'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}`;
  });
  editorDiagnosticStage = 'teardown-confirm';
  const confirm = dialog.getByRole('button', { name: 'Delete' });
  await expect(confirm).toBeEnabled();
  await confirm.press('Enter');
  const response = await Promise.race([
    deletion,
    page.waitForTimeout(3_000).then(() => null),
  ]);
  if (response === null) {
    const dialogState = await dialog.isVisible() ? 'open' : 'closed';
    editorDiagnosticStage = `teardown-no-dispatch-dialog-${dialogState}`;
    throw new Error('resume delete did not dispatch');
  }
  expect(response.status()).toBe(204);
  editorDiagnosticStage = 'teardown-row';
  await expect(row).toHaveCount(0);
  editorDiagnosticStage = 'teardown-complete';
}

async function installDiagnostics(
  context: BrowserContext,
  page: Page,
): Promise<Diagnostics> {
  const counters = newDiagnosticCounters();
  const consoleStatusCounts = new Map<number | 'other', number>();
  let downloads = 0;
  let firstConsoleError = 'none';
  let firstConsoleStage = 'none';
  let firstOtherConsoleError = 'none';
  let firstOtherConsoleLead = 'none';
  let firstOtherConsoleStage = 'none';
  let firstPageError = 'none';
  let firstPageErrorLead = 'none';
  let firstPageErrorName = 'none';
  let firstPageStage = 'none';
  let serviceWorkers = 0;
  let failPhotoRead = false;
  let failTemplateChildAt: number | undefined;
  let templateChildCount = 0;
  let templateSecondChildForced = false;
  let pausedMutation:
    | (PendingRequestGate & { readonly method: string; readonly pathname: string })
    | undefined;
  let pausedPhotoCrop: PendingRequestGate | undefined;
  let pausedPhotoUpload: PendingRequestGate | undefined;
  const expectedRequestFailures: {
    readonly method: string;
    readonly pathname: string;
    readonly status: 401 | 412;
  }[] = [];
  const expectedConsoleFailures = new Map<string, number[]>();
  const expectConsoleFailure = (url: string, status: number): void => {
    const pending = expectedConsoleFailures.get(url) ?? [];
    pending.push(status);
    expectedConsoleFailures.set(url, pending);
  };
  const consumeExpectedConsoleFailure = (
    url: string,
    status: number | null,
  ): boolean => {
    if (status === null) return false;
    const pending = expectedConsoleFailures.get(url);
    if (pending?.[0] !== status) return false;
    pending.shift();
    if (pending.length === 0) expectedConsoleFailures.delete(url);
    return true;
  };
  const attachCorePageDiagnostics = pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) => !consumeExpectedConsoleFailure(
      message.location().url,
      httpFailureStatus(message.text()),
    ) && !isExpectedNegativeHTTPConsole(message.text(), message.location().url),
    onCountedConsoleError: (message) => {
      const countedStatus = httpFailureStatus(message.text()) ?? 'other';
      consoleStatusCounts.set(
        countedStatus,
        (consoleStatusCounts.get(countedStatus) ?? 0) + 1,
      );
      if (countedStatus === 'other' && firstOtherConsoleError === 'none') {
        firstOtherConsoleError = classifyDiagnosticText(message.text());
        firstOtherConsoleLead = classifyDiagnosticLead(message.text());
        firstOtherConsoleStage = editorDiagnosticStage;
      }
      if (firstConsoleError === 'none') {
        firstConsoleError = classifyDiagnosticText(message.text());
        firstConsoleStage = editorDiagnosticStage;
      }
    },
    onPageError: (error) => {
      if (firstPageError === 'none') {
        firstPageError = classifyDiagnosticText(error.message);
        firstPageErrorLead = classifyDiagnosticLead(error.message);
        firstPageErrorName = classifyDiagnosticName(error.name);
        firstPageStage = editorDiagnosticStage;
      }
    },
  });
  const attachPageDiagnostics = (openedPage: Page): void => {
    attachCorePageDiagnostics(openedPage);
    openedPage.on('download', () => {
      downloads += 1;
    });
  };
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  context.on('serviceworker', () => {
    serviceWorkers += 1;
  });
  await context.route('**/*', async (route) => {
    const request = route.request();
    if (!isAllowedHTTPURL(request.url())) {
      counters.externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    const requestURL = new URL(request.url());
    const expectedRequestIndex = expectedRequestFailures.findIndex(
      (expected) => expected.method === request.method()
        && expected.pathname === requestURL.pathname,
    );
    if (expectedRequestIndex !== -1) {
      const [expected] = expectedRequestFailures.splice(expectedRequestIndex, 1);
      expectConsoleFailure(request.url(), expected!.status);
    }
    if (
      pausedMutation !== undefined
      && request.method() === pausedMutation.method
      && requestURL.pathname === pausedMutation.pathname
    ) {
      const gate = pausedMutation;
      pausedMutation = undefined;
      gate.resolvePaused();
      await gate.releasePromise;
      await route.continue();
      return;
    } else if (failPhotoRead && isPhotoReadRequest(request)) {
      failPhotoRead = false;
      expectConsoleFailure(request.url(), 500);
      await route.fulfill({
        body: '{"error":{"code":"forced_failure"}}',
        contentType: 'application/json',
        headers: { 'Cache-Control': 'no-store, no-transform' },
        status: 500,
      });
      return;
    } else if (pausedPhotoCrop !== undefined && isPhotoCropRequest(request)) {
      const gate = pausedPhotoCrop;
      pausedPhotoCrop = undefined;
      gate.resolvePaused();
      await gate.releasePromise;
      await route.continue();
      return;
    } else if (pausedPhotoUpload !== undefined && isPhotoUploadRequest(request)) {
      const gate = pausedPhotoUpload;
      pausedPhotoUpload = undefined;
      gate.resolvePaused();
      await gate.releasePromise;
      await route.continue();
      return;
    } else if (
      failTemplateChildAt !== undefined
      && isTemplateChildRequest(request)
    ) {
      templateChildCount += 1;
      if (templateChildCount === failTemplateChildAt) {
        failTemplateChildAt = undefined;
        templateSecondChildForced = true;
        expectConsoleFailure(request.url(), 422);
        await route.fulfill({
          body: JSON.stringify({
            error: {
              code: 'document_invalid',
              message: 'Invalid template child.',
              details: {
                issues: [{ path: 'customization', code: 'invalid' }],
              },
            },
          }),
          contentType: 'application/json',
          headers: { 'Cache-Control': 'no-store, no-transform' },
          status: 422,
        });
        return;
      }
      await route.continue();
      return;
    } else {
      await route.continue();
    }
  });
  await context.routeWebSocket('**/*', async (webSocket) => {
    if (!isAllowedWebSocketURL(webSocket.url())) {
      counters.externalRequests += 1;
      await webSocket.close({ code: 1008, reason: 'blocked' });
      return;
    }
    webSocket.connectToServer();
  });
  return {
    armPhotoReadFailure: (): void => {
      failPhotoRead = true;
    },
    armTemplateFailure: (): void => {
      templateChildCount = 0;
      templateSecondChildForced = false;
      failTemplateChildAt = 2;
    },
    expectHTTPFailure: (
      method: string,
      pathname: string,
      status: 401 | 412,
    ): void => {
      expectedRequestFailures.push({ method, pathname, status });
    },
    pauseNextMutation: (
      method: string,
      pathname: string,
    ): PausedPhotoRequest => {
      if (pausedMutation !== undefined) {
        throw new Error('mutation pause already armed');
      }
      const gate = createPendingRequestGate();
      pausedMutation = { ...gate.pending, method, pathname };
      return gate.control;
    },
    pauseNextPhotoCrop: (): PausedPhotoRequest => {
      if (pausedPhotoCrop !== undefined) {
        throw new Error('photo crop pause already armed');
      }
      const gate = createPendingRequestGate();
      pausedPhotoCrop = gate.pending;
      return gate.control;
    },
    pauseNextPhotoUpload: (): PausedPhotoUpload => {
      if (pausedPhotoUpload !== undefined) {
        throw new Error('photo upload pause already armed');
      }
      const gate = createPendingRequestGate();
      pausedPhotoUpload = gate.pending;
      return gate.control;
    },
    assertTemplateFailureSecondChild: (): void => {
      expect({ childCount: templateChildCount, forced: templateSecondChildForced }).toEqual({
        childCount: 2,
        forced: true,
      });
    },
    assertClean: (): void => {
      editorDiagnosticStage = [
        'post-diagnostics',
        `certificate-${counters.certificateErrors}`,
        `console-${counters.consoleErrors}`,
        `consolekind-${firstConsoleError}`,
        `consolestage-${firstConsoleStage}`,
        `console401-${consoleStatusCounts.get(401) ?? 0}`,
        `console412-${consoleStatusCounts.get(412) ?? 0}`,
        `console422-${consoleStatusCounts.get(422) ?? 0}`,
        `console500-${consoleStatusCounts.get(500) ?? 0}`,
        `consoleother-${consoleStatusCounts.get('other') ?? 0}`,
        `consoleotherkind-${firstOtherConsoleError}`,
        `consoleotherlead-${firstOtherConsoleLead}`,
        `consoleotherstage-${firstOtherConsoleStage}`,
        `external-${counters.externalRequests}`,
        `page-${counters.pageErrors}`,
        `pagekind-${firstPageError}`,
        `pagelead-${firstPageErrorLead}`,
        `pagename-${firstPageErrorName}`,
        `pagestage-${firstPageStage}`,
        `download-${downloads}`,
        `worker-${serviceWorkers}`,
      ].join('-');
      expect({
        certificate: counters.certificateErrors,
        console: counters.consoleErrors,
        externalRequest: counters.externalRequests,
        page: counters.pageErrors,
      }).toEqual({ certificate: 0, console: 0, externalRequest: 0, page: 0 });
      expect(expectedRequestFailures).toEqual([]);
      expect([...expectedConsoleFailures.entries()]).toEqual([]);
      expect(downloads).toBe(0);
      expect(serviceWorkers).toBe(0);
    },
  };
}

interface PendingRequestGate {
  readonly release: () => void;
  readonly releasePromise: Promise<void>;
  readonly paused: Promise<void>;
  readonly resolvePaused: () => void;
}

function createPendingRequestGate(): {
  readonly control: PausedPhotoRequest;
  readonly pending: PendingRequestGate;
} {
  let resolvePaused: () => void;
  let release: () => void;
  const paused = new Promise<void>((resolve) => {
    resolvePaused = resolve;
  });
  const releasePromise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return {
    control: {
      release: (): void => release!(),
      waitUntilPaused: (): Promise<void> => paused,
    },
    pending: {
      paused,
      release: release!,
      releasePromise,
      resolvePaused: resolvePaused!,
    },
  };
}

async function applyTemplate(page: Page, id: string): Promise<void> {
  const template = page.locator(`[data-template="${id}"]`);
  await expect(template).toBeVisible();
  await template.getByRole('button', { name: 'Apply' }).press('Enter');
}

async function captureOwnerReadHeaders(
  page: Page,
  resumeID: string,
): Promise<{ acceptEncoding(): string }> {
  let acceptEncoding = '';
  const session = await page.context().newCDPSession(page);
  const reads = new Set<string>();
  const headers = new Map<string, Record<string, unknown>>();
  const capture = (requestID: string): void => {
    if (!reads.has(requestID)) return;
    const value = headers.get(requestID);
    if (value === undefined) return;
    const entry = Object.entries(value).find(
      ([name]) => name.toLowerCase() === 'accept-encoding',
    );
    if (typeof entry?.[1] === 'string') acceptEncoding = entry[1];
  };
  session.on('Network.requestWillBeSent', (event: unknown) => {
    const value = object(event);
    const request = object(value?.request);
    const requestID = value?.requestId;
    if (
      typeof requestID === 'string'
      && request?.method === 'GET'
      && request.url === `${ORIGIN}/api/v1/resumes/${resumeID}`
    ) {
      reads.add(requestID);
      capture(requestID);
    }
  });
  session.on('Network.requestWillBeSentExtraInfo', (event: unknown) => {
    const value = object(event);
    const requestID = value?.requestId;
    const extra = object(value?.headers);
    if (typeof requestID !== 'string' || extra === null) return;
    headers.set(requestID, extra);
    capture(requestID);
  });
  await session.send('Network.enable');
  return { acceptEncoding: (): string => acceptEncoding };
}

async function destroySessionInSecondPage(page: Page): Promise<void> {
  await page.goto('/');
  const status = await page.evaluate(async () => {
    const me = await fetch('/api/v1/me', { cache: 'no-store', credentials: 'include' });
    const body = await me.json() as { data?: { csrfToken?: unknown } };
    const token = body.data?.csrfToken;
    if (typeof token !== 'string' || token === '') return 0;
    const logout = await fetch('/api/v1/auth/logout', {
      cache: 'no-store',
      credentials: 'include',
      headers: { 'X-CSRF-Token': token },
      method: 'POST',
    });
    return logout.status;
  });
  expect(status).toBe(204);
}

async function expectURLUnchanged(page: Page, baseline: URLBaseline): Promise<void> {
  await expect.poll(() => readURLBaseline(page)).toEqual(baseline);
}

async function expectNoPersistenceWrites(
  probes: BrowserPersistenceProbe,
): Promise<void> {
  expect(await probes.read()).toEqual({
    history: 0,
    indexedDB: 0,
    localStorage: 0,
    sendBeacon: 0,
    sessionStorage: 0,
  });
}

async function ensureAuthenticated(page: Page): Promise<void> {
  await page.goto('/');
  const status = await page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    return response.status;
  });
  if (status === 200) return;
  expect(status).toBe(401);
  await loginAsDevelopmentUser(page);
}

async function readURLBaseline(page: Page): Promise<URLBaseline> {
  return page.evaluate(() => ({
    hash: location.hash,
    href: location.href,
    search: location.search,
  }));
}

function isOwnerRead(request: Request, resumeID: string): boolean {
  const url = new URL(request.url());
  return request.method() === 'GET'
    && url.origin === ORIGIN
    && url.pathname === `/api/v1/resumes/${resumeID}`;
}

function isResumeMutation(request: Request, resumeID: string): boolean {
  const url = new URL(request.url());
  return request.method() !== 'GET'
    && url.origin === ORIGIN
    && url.pathname.startsWith(`/api/v1/resumes/${resumeID}`);
}

function isTemplateChildRequest(request: Request): boolean {
  if (request.method() !== 'PATCH') return false;
  const url = new URL(request.url());
  return url.origin === ORIGIN
    && /^\/api\/v1\/resumes\/[0-9a-f-]{36}\/(?:customization|structure)$/.test(url.pathname);
}

function isPhotoReadRequest(request: Request): boolean {
  if (request.method() !== 'GET') return false;
  return isPhotoRequest(request);
}

function isPhotoUploadRequest(request: Request): boolean {
  if (request.method() !== 'POST') return false;
  return isPhotoRequest(request);
}

function isPhotoCropRequest(request: Request): boolean {
  if (request.method() !== 'PATCH') return false;
  return isPhotoRequest(request);
}

function isPhotoRequest(request: Request): boolean {
  const url = new URL(request.url());
  return url.origin === ORIGIN
    && /^\/api\/v1\/resumes\/[0-9a-f-]{36}\/photo$/.test(url.pathname);
}

function seriousOrCritical(
  violations: readonly {
    readonly id: string;
    readonly impact?: string | null;
  }[],
): readonly { readonly id: string; readonly impact?: string | null }[] {
  return violations.filter(
    ({ impact }) => impact === 'serious' || impact === 'critical',
  );
}

function classifyDiagnosticText(message: string): string {
  const checks: readonly [RegExp, string][] = [
    [/structuredclone|could not be cloned|datacloneerror/i, 'clone'],
    [/cannot read propert|undefined/i, 'undefined'],
    [/cannot assign|read only/i, 'readonly'],
    [/unhandled error.*event handler/i, 'event-handler'],
    [/unhandled error.*watch/i, 'watcher'],
    [/failed to fetch|load failed|networkerror/i, 'network'],
    [/401|unauthorized|session/i, 'session'],
    [/412|revision|conflict/i, 'conflict'],
    [/422|document.invalid|invalid template/i, 'validation'],
    [/500|forced.failure|photo preview/i, 'forced-failure'],
  ];
  return checks.find(([pattern]) => pattern.test(message))?.[1] ?? 'other';
}

function classifyDiagnosticName(name: string): string {
  const normalized = name.toLowerCase();
  if (normalized === 'error') return 'error';
  if (normalized === 'typeerror') return 'typeerror';
  if (normalized === 'aborterror') return 'aborterror';
  if (normalized === 'fetcherror') return 'fetcherror';
  if (normalized === 'domexception') return 'domexception';
  const safe = normalized.replace(/[^a-z]/g, '').slice(0, 24);
  return safe === '' ? 'other' : safe;
}

function classifyDiagnosticLead(message: string): string {
  const lead = message.trim().match(/[A-Za-z]+/)?.[0]?.toLowerCase() ?? '';
  return lead.slice(0, 24) || 'other';
}

async function requiredAttribute(
  locator: ReturnType<Page['locator']>,
  name: string,
): Promise<string> {
  const value = await locator.getAttribute(name);
  expect(value).not.toBeNull();
  return value as string;
}

function object(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

async function writeEditorEvidence(steps: EditorSteps): Promise<void> {
  const evidence = {
    schemaVersion: 1,
    scenario: 'authenticated-editor',
    origin: ORIGIN,
    errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
    steps,
  };
  const serialized = `${JSON.stringify(evidence, null, 2)}\n`;
  if (
    Buffer.byteLength(serialized) > 8_192
    || /(?:csrf|cookie|idempotency|oauth|filename|object key|@|\b[a-f0-9]{8}-[a-f0-9-]{27}\b)/i.test(serialized)
  ) {
    throw new Error('editor evidence would violate its bounded privacy contract');
  }
  await writeFile(EVIDENCE_PATH, serialized, { flag: 'wx', mode: 0o600 });
}
