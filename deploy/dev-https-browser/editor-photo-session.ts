import { expect, type BrowserContext, type Page } from '@playwright/test';
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
  state.editorDiagnosticStage = 'session-destroy-request';
  await destroySessionInSecondPage(reauthPage);
  state.editorDiagnosticStage = 'session-local-edit';
  diagnostics.expectHTTPFailure(
    'PATCH',
    `/api/v1/resumes/${resumeID}/personal-details`,
    401,
  );
  await page.getByLabel('Headline').fill('Retained after session loss');
  state.editorDiagnosticStage = 'session-local-edit-blur';
  await page.getByLabel('Headline').press('Tab');
  state.editorDiagnosticStage = 'session-loss-alert';
  await expect(page.getByRole('alertdialog', { name: 'Sign in to continue editing' })).toBeVisible();
  await expectURLUnchanged(page, baseline);
  await expectNoPersistenceWrites(probes);
  state.editorDiagnosticStage = 'session-reauth-sign-in';
  await loginAsDevelopmentUser(reauthPage);
  state.editorDiagnosticStage = 'session-reauth-url';
  await expectURLUnchanged(page, baseline);
  state.editorDiagnosticStage = 'session-reauth-persistence';
  await expectNoPersistenceWrites(probes);
  state.editorDiagnosticStage = 'session-reauth-draft';
  await expect(page.getByLabel('Headline')).toHaveValue('Retained after session loss');
  state.editorDiagnosticStage = 'session-resume-activate';
  await page.getByRole('button', { name: 'Resume after sign-in' }).press('Enter');
  state.editorDiagnosticStage = 'session-resume-saved';
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  state.editorDiagnosticStage = 'session-resume-draft';
  await expect(page.getByLabel('Headline')).toHaveValue('Retained after session loss');
  state.editorDiagnosticStage = 'session-resume-url';
  await expectURLUnchanged(page, baseline);
  state.editorDiagnosticStage = 'session-resume-persistence';
  await expectNoPersistenceWrites(probes);
  return reauthPage;
}

export async function destroySessionInSecondPage(page: Page): Promise<void> {
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
