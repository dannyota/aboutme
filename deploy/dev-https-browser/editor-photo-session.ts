import {
  expect,
  type BrowserContext,
  type Page,
  type Request,
  type Response,
  type Route,
} from '@playwright/test';
import {
  installPhotoSourceReadProbes,
  loginAsDevelopmentUser,
  ownerPhotoHasNoCrop,
  replaceRemotePhoto,
  type BrowserPersistenceProbe,
} from './editor-fixtures';
import {
  ORIGIN,
  VALID_PNG_BASE64,
  type EditorProofState,
  type Diagnostics,
  type URLBaseline,
  expectURLUnchanged,
  expectNoPersistenceWrites,
  expectSavedOrDiagnose,
  safeSaveState,
} from './editor-proof-state';

export async function provePhotoSessionPersistence(
  state: EditorProofState,
  page: Page,
  context: BrowserContext,
  resumeID: string,
  diagnostics: Diagnostics,
  probes: BrowserPersistenceProbe,
  baseline: URLBaseline,
): Promise<Page> {
  state.editorDiagnosticStage = 'photo-open';
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
  // A photo uploaded in this session saves its default square crop once
  // the image loads.
  const defaultCrop = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'PATCH'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await upload.setInputFiles({
    buffer: Buffer.from(VALID_PNG_BASE64, 'base64'),
    mimeType: 'image/png',
    name: 'editor-proof.png',
  });
  state.editorDiagnosticStage = 'photo-upload-paused';
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
  state.editorDiagnosticStage = 'photo-upload-accepted';
  expect((await acceptedPhoto).status()).toBe(200);
  state.editorDiagnosticStage = 'photo-upload-saved';
  await expectSavedOrDiagnose(state, page, 'photo-upload');
  expect((await ownerPhotoRead).headers()['content-type']).toMatch(/^image\/(?:jpeg|png)/);
  await expect(page.locator('[data-photo-preview] img')).toBeVisible();
  await expect.poll(async () => (await sourceReads.read()).dataURL).toBeGreaterThan(0);
  await expectURLUnchanged(page, baseline);
  state.editorDiagnosticStage = 'photo-default-crop';
  expect((await defaultCrop).status()).toBe(200);
  await expectSavedOrDiagnose(state, page, 'photo-default-crop');
  state.editorDiagnosticStage = 'photo-crop';
  const acceptedCrop = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'PATCH'
      && url.origin === ORIGIN
      && url.pathname === `/api/v1/resumes/${resumeID}/photo`;
  });
  await page.locator('summary', { hasText: 'Exact values' }).press('Enter');
  await page.getByLabel('Width').fill('0.75');
  await page.getByRole('button', { name: 'Save crop' }).press('Enter');
  state.editorDiagnosticStage = 'photo-crop-response';
  expect((await acceptedCrop).status()).toBe(200);
  state.editorDiagnosticStage = 'photo-crop-saved';
  await expectSavedOrDiagnose(state, page, 'photo-crop');
  state.editorDiagnosticStage = 'photo-crop-url';
  await expectURLUnchanged(page, baseline);
  state.editorDiagnosticStage = 'photo-crop-persistence';
  await expectNoPersistenceWrites(probes);

  state.editorDiagnosticStage = 'photo-remote-conflict';
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
  state.editorDiagnosticStage = 'photo-remote-conflict-submitted';
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
    state.editorDiagnosticStage = invalid
      ? 'photo-remote-conflict-invalid'
      : draftWidth === '0.5'
        ? 'photo-remote-conflict-not-dispatched'
        : 'photo-remote-conflict-reset';
    throw new Error('photo crop did not dispatch');
  }
  state.editorDiagnosticStage = 'photo-remote-conflict-queued';
  state.editorDiagnosticStage = 'photo-remote-conflict-winner';
  await replaceRemotePhoto(page, resumeID, VALID_PNG_BASE64);
  cropPause.release();
  state.editorDiagnosticStage = 'photo-remote-conflict-response';
  expect((await staleCrop).status()).toBe(412);
  state.editorDiagnosticStage = 'photo-remote-conflict-reopen';
  await expect(page.locator('[data-state="conflict"]')).toBeVisible();
  state.editorDiagnosticStage = 'photo-remote-conflict-state';
  const photoPanel = page.locator('section[aria-labelledby="photo-title"]');
  const reopenCrop = photoPanel.getByRole('button', { name: 'Reopen crop' });
  await expect(reopenCrop).toBeVisible();
  state.editorDiagnosticStage = 'photo-remote-conflict-visible';
  await reopenCrop.press('Enter');
  state.editorDiagnosticStage = 'photo-remote-conflict-accepted';
  await expect(page.getByLabel('Replace photo')).toBeVisible();
  state.editorDiagnosticStage = 'photo-read-failure';
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

  state.editorDiagnosticStage = 'photo-delete';
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

  state.editorDiagnosticStage = 'session-personal-details';
  await page
    .getByRole('navigation', { name: 'Resume outline' })
    .getByRole('button', { name: 'Personal details', exact: true })
    .press('Enter');
  await expect(page.getByLabel('Headline')).toBeVisible();
  state.editorDiagnosticStage = 'session-destroyed';
  const reauthPage = await context.newPage();
  await reauthPage.goto('/');
  // The edit's own save must be the first request to meet the ended session.
  // Otherwise the editor's realtime stream can read the resume first, once
  // the server ends the stream of the dead session; the editor then turns
  // signed out and refuses the edit, and the Headline drops the value checked
  // below. So the save is dispatched and held before the session ends.
  state.editorDiagnosticStage = 'session-local-edit';
  const personalPath = `/api/v1/resumes/${resumeID}/personal-details`;
  diagnostics.expectHTTPFailure('PATCH', personalPath, 401);
  const editPause = diagnostics.pauseNextMutation('PATCH', personalPath);
  let sessionWindow: ReturnType<typeof observeSessionWindow> | undefined;
  let readHold: Awaited<ReturnType<typeof holdSessionReads>> | undefined;
  try {
    readHold = await holdSessionReads(page, resumeID, diagnostics);
    await page.getByLabel('Headline').fill('Retained after session loss');
    state.editorDiagnosticStage = 'session-local-edit-blur';
    await page.getByLabel('Headline').press('Tab');
    state.editorDiagnosticStage = 'session-local-edit-dispatched';
    const editDispatched = await Promise.race([
      editPause.waitUntilPaused().then(() => true),
      page.waitForTimeout(10_000).then(() => false),
    ]);
    if (!editDispatched) throw new Error('headline save did not dispatch');
    sessionWindow = observeSessionWindow(page, resumeID);
    state.editorDiagnosticStage = 'session-destroy-request';
    await destroySessionInSecondPage(reauthPage);
    const editResponse = page.waitForResponse((response) => {
      const request = response.request();
      const url = new URL(response.url());
      return request.method() === 'PATCH'
        && url.origin === ORIGIN
        && url.pathname === personalPath;
    });
    editPause.release();
    state.editorDiagnosticStage = 'session-save-response';
    expect((await editResponse).status()).toBe(401);
    await readHold.releaseBeforeSignIn();
    state.editorDiagnosticStage = 'session-loss-alert';
    await expect(page.getByRole('alertdialog', { name: 'Sign in to continue editing' })).toBeVisible();
    await expectURLUnchanged(page, baseline);
    await expectNoPersistenceWrites(probes);
    state.editorDiagnosticStage = 'session-reauthenticated';
    await loginAsDevelopmentUser(reauthPage);
    await readHold.release();
    state.editorDiagnosticStage = 'session-reauth-url';
    await expectURLUnchanged(page, baseline);
    state.editorDiagnosticStage = 'session-reauth-persistence';
    await expectNoPersistenceWrites(probes);
    state.editorDiagnosticStage = 'session-reauth-headline';
    await expectHeadlineOrDiagnose(state, page, 'session-reauth');
    state.editorDiagnosticStage = 'session-resume';
    await page.getByRole('button', { name: 'Resume after sign-in' }).press('Enter');
    state.editorDiagnosticStage = 'session-resume-saved';
    await expectSavedOrDiagnose(state, page, 'session-resume');
    state.editorDiagnosticStage = 'session-resume-headline';
    await expectHeadlineOrDiagnose(state, page, 'session-resume');
    state.editorDiagnosticStage = 'session-resume-url';
    await expectURLUnchanged(page, baseline);
    await expectNoPersistenceWrites(probes);
  } catch (error) {
    if (sessionWindow !== undefined) {
      state.editorDiagnosticStage = `${state.editorDiagnosticStage}-${sessionWindow.label()}`;
    }
    throw error;
  } finally {
    editPause.release();
    try {
      await readHold?.release(true);
    } finally {
      sessionWindow?.dispose();
    }
  }
  return reauthPage;
}

async function holdSessionReads(
  page: Page,
  resumeID: string,
  diagnostics: Diagnostics,
): Promise<{ releaseBeforeSignIn(): Promise<void>; release(expectFailures?: boolean): Promise<void> }> {
  const held: { request: Request; release(): void }[] = [];
  let released = false;
  const matches = (url: URL): boolean => url.origin === ORIGIN && (
    url.pathname === `/api/v1/resumes/${resumeID}`
    || url.pathname === '/api/v1/events'
  );
  const handle = async (route: Route): Promise<void> => {
    const request = route.request();
    if (request.method() === 'GET' && !released) {
      await new Promise<void>((resolve) => held.push({ request, release: resolve }));
    }
    await route.fallback();
  };
  await page.route(matches, handle);
  return {
    releaseBeforeSignIn: async (): Promise<void> => {
      // Reads arriving during this batch stay held until sign-in completes.
      const pending = [...held];
      pending.sort((left, right) => (
        Number(new URL(right.request.url()).pathname === '/api/v1/events')
        - Number(new URL(left.request.url()).pathname === '/api/v1/events')
      ));
      for (const item of pending) {
        diagnostics.expectHTTPFailure('GET', new URL(item.request.url()).pathname, 401);
        held.splice(held.indexOf(item), 1);
        item.release();
        expect((await item.request.response())?.status()).toBe(401);
      }
    },
    release: async (expectFailures = false): Promise<void> => {
      if (released) return;
      released = true;
      const pending = held.splice(0);
      try {
        while (pending.length > 0) {
          const item = pending.shift()!;
          const cancel = diagnostics.expectHTTPFailure(
            'GET', new URL(item.request.url()).pathname, 401,
          );
          item.release();
          if (!expectFailures && (await item.request.response())?.status() !== 401) cancel();
        }
      } finally {
        for (const item of pending) {
          diagnostics.expectHTTPFailure('GET', new URL(item.request.url()).pathname, 401);
          item.release();
        }
        await page.unroute(matches, handle);
      }
    },
  };
}

// Counts what the editor page does once its session ends, for the failure
// stage: resume reads refused with 401, realtime stream requests, and
// document loads (a reload loses the retained edit).
function observeSessionWindow(
  page: Page,
  resumeID: string,
): { dispose(): void; label(): string } {
  let refusedReads = 0;
  let streams = 0;
  let loads = 0;
  const onResponse = (response: Response): void => {
    const request = response.request();
    const url = new URL(response.url());
    if (url.origin !== ORIGIN || request.method() !== 'GET') return;
    if (url.pathname === `/api/v1/resumes/${resumeID}` && response.status() === 401) {
      refusedReads += 1;
    }
    if (url.pathname === '/api/v1/events') streams += 1;
  };
  const onLoad = (): void => {
    loads += 1;
  };
  page.on('response', onResponse);
  page.on('load', onLoad);
  return {
    dispose: (): void => {
      page.off('response', onResponse);
      page.off('load', onLoad);
    },
    label: (): string => `reads401-${refusedReads}-streams-${streams}-loads-${loads}`,
  };
}

// Checks the retained Headline. On failure, the stage names what the field
// shows (never its text), the save state, and whether the sign-in dialog is open.
async function expectHeadlineOrDiagnose(
  state: EditorProofState,
  page: Page,
  boundary: 'session-reauth' | 'session-resume',
): Promise<void> {
  const headline = page.getByLabel('Headline');
  try {
    await expect(headline).toHaveValue('Retained after session loss');
  } catch (error) {
    const value = await headline.inputValue({ timeout: 1_000 }).catch(() => null);
    const shown = value === null ? 'missing' : value === '' ? 'empty' : 'other';
    const saveState = await page.getByTestId('save-status')
      .getAttribute('data-state', { timeout: 1_000 })
      .catch(() => null);
    const dialog = await page
      .getByRole('alertdialog', { name: 'Sign in to continue editing' })
      .isVisible();
    state.editorDiagnosticStage = [
      boundary,
      'headline',
      shown,
      safeSaveState(saveState),
      dialog ? 'dialog' : 'no-dialog',
    ].join('-');
    throw error;
  }
}

export async function destroySessionInSecondPage(page: Page): Promise<void> {
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
