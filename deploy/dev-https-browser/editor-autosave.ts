import { expect, test, type BrowserContext, type Page } from '@playwright/test';
import {
  createBlankResume,
  installBrowserPersistenceProbes,
  settledVisiblePageCount,
  uniqueTitle,
} from './editor-fixtures';
import {
  ORIGIN,
  SCHEMA_VERSION,
  type EditorProofState,
  type EditorSteps,
  type EditorScenario,
  expectURLUnchanged,
  expectNoPersistenceWrites,
  readURLBaseline,
  isOwnerRead,
  isResumeMutation,
  object,
} from './editor-proof-state';

export async function proveListLoadAutosave(
  state: EditorProofState,
  page: Page,
  context: BrowserContext,
  createdIDs: Set<string>,
  steps: EditorSteps,
): Promise<EditorScenario> {
  state.editorDiagnosticStage = 'list-create-resume';
  const initialTitle = uniqueTitle();
  const created = await createBlankResume(page, initialTitle, (stage) => {
    state.editorDiagnosticStage = `list-${stage}`;
  }, (accepted) => {
    createdIDs.add(accepted.metadata.id);
  });

  state.editorDiagnosticStage = 'list-open';
  await page.goto('/app/resumes');
  const row = page.locator(`[data-testid="resume-row-${created.metadata.id}"]`);
  await expect(row).toBeVisible();
  state.editorDiagnosticStage = 'list-rename-open';
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
  state.editorDiagnosticStage = 'list-rename-save';
  await rename.getByRole('button', { name: 'Save' }).press('Enter');
  expect((await renameMutation).status()).toBe(200);
  await expect(rename).toBeHidden();
  state.editorDiagnosticStage = 'list-reload';
  await page.reload();
  await expect(row.getByRole('link')).toContainText(renamedTitle);

  state.editorDiagnosticStage = 'editor-open';
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

  state.editorDiagnosticStage = 'autosave';
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
  state.editorDiagnosticStage = 'autosave-request';
  expect(autosaveRequest.headers()['if-match']).toBe(observedETag);
  state.editorDiagnosticStage = 'autosave-saved-state';
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  state.editorDiagnosticStage = 'autosave-delay';
  const autosaveDelay = await page.evaluate((requestURL) => {
    const inputAt = Reflect.get(globalThis, '__aboutmeAutosaveInputAt');
    Reflect.deleteProperty(globalThis, '__aboutmeAutosaveInputAt');
    const requests = performance.getEntriesByName(requestURL, 'resource');
    const request = requests.at(-1);
    if (typeof inputAt !== 'number' || request === undefined) return -1;
    return request.startTime - inputAt;
  }, autosaveRequest.url());
  expect(autosaveDelay).toBeGreaterThanOrEqual(1000);
  state.editorDiagnosticStage = 'autosave-complete';
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  await test.step('loads the accepted autosave in a fresh page', async () => {
    state.editorDiagnosticStage = 'persistence-new-page';
    const verificationPage = await context.newPage();
    state.editorDiagnosticStage = 'persistence-install-probes';
    const verificationProbes = await installBrowserPersistenceProbes(
      verificationPage,
    );
    try {
      state.editorDiagnosticStage = 'persistence-navigate';
      const loaded = await verificationPage.goto(baseline.href, {
        timeout: 10_000,
        waitUntil: 'domcontentloaded',
      });
      expect(loaded?.status()).toBe(200);
      state.editorDiagnosticStage = 'persistence-headline';
      await expect(verificationPage.getByLabel('Headline')).toHaveValue(
        'Autosaved headline',
      );
      state.editorDiagnosticStage = 'persistence-reset';
      await verificationProbes.reset();
      state.editorDiagnosticStage = 'persistence-assert';
      await expectURLUnchanged(verificationPage, baseline);
      await expectNoPersistenceWrites(verificationProbes);
    } finally {
      state.editorDiagnosticStage = 'persistence-close';
      await verificationPage.close();
    }
  }, { timeout: 15_000 });
  state.editorDiagnosticStage = 'structure';
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  steps.ifMatch = true;
  steps.autosave = true;
  return { accepted: created, baseline, probes };
}

export async function captureOwnerReadHeaders(
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
