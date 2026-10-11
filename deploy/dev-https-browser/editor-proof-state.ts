import { expect, type Page, type Request } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import {
  loginAsDevelopmentUser,
  type AcceptedResume,
  type BrowserPersistenceProbe,
} from './editor-fixtures';
import { ALLOWED_ORIGIN } from './network-policy';

export const ORIGIN = ALLOWED_ORIGIN;
export const EVIDENCE_PATH = '/evidence/editor-proof.json';
export const SCHEMA_VERSION = '5';
export const VALID_PNG_BASE64
  = 'iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAANElEQVR4nOzNsQkA'
    + 'MAwDQRWGrJn9pwjZweruEWpvknuS3uZfMwAAAAAAAAAAALDVCwAA///3/wKTiM0y'
    + 'DAAAAABJRU5ErkJggg==';

export interface EditorProofState {
  editorDiagnosticStage: string;
  localeDiagnosticStage: string;
  readonly recordedResumeIDs: Set<string>;
}

export async function prepareEditor<T>(
  state: EditorProofState,
  operation: () => Promise<T>,
): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    process.stderr.write(`editor-stage:${state.editorDiagnosticStage}\n`);
    throw error;
  }
}

export interface EditorSteps {
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

export interface Diagnostics {
  assertClean(): void;
  assertTemplateFailureSecondChild(): void;
  armPhotoReadFailure(): void;
  armTemplateFailure(): void;
  expectHTTPFailure(method: string, pathname: string, status: 401 | 412): void;
  pauseNextMutation(method: string, pathname: string): PausedPhotoRequest;
  pauseNextPhotoCrop(): PausedPhotoRequest;
  pauseNextPhotoUpload(): PausedPhotoUpload;
}

export interface PausedPhotoRequest {
  release(): void;
  waitUntilPaused(): Promise<void>;
}

export type PausedPhotoUpload = PausedPhotoRequest;

export interface URLBaseline {
  readonly hash: string;
  readonly href: string;
  readonly search: string;
}

export interface EditorScenario {
  readonly accepted: AcceptedResume;
  readonly baseline: URLBaseline;
  readonly probes: BrowserPersistenceProbe;
}

export interface PendingRequestGate {
  readonly release: () => void;
  readonly releasePromise: Promise<void>;
  readonly paused: Promise<void>;
  readonly resolvePaused: () => void;
}

export function createPendingRequestGate(): {
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

export async function expectURLUnchanged(page: Page, baseline: URLBaseline): Promise<void> {
  await expect.poll(() => readURLBaseline(page)).toEqual(baseline);
}

export async function expectNoPersistenceWrites(
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

export async function expectSavedOrDiagnose(
  state: EditorProofState,
  page: Page,
  boundary: 'template-keep-partial' | 'photo-upload' | 'photo-crop',
): Promise<void> {
  try {
    await expect(page.locator('[data-state="saved"]')).toBeVisible();
  } catch (error) {
    const saveState = await page.getByTestId('save-status').getAttribute('data-state');
    const safeState = [
      'conflict',
      'dirty',
      'error',
      'offline',
      'saved',
      'saving',
      'session-lost',
    ].includes(saveState ?? '') ? saveState : 'other';
    const conflict = page.locator('[data-conflict]').first();
    const category = await conflict.count() > 0
      ? classifyConflict(await conflict.getAttribute('data-conflict'))
      : await page.getByRole('alert').count() > 0
        ? 'alert'
        : 'none';
    state.editorDiagnosticStage = `${boundary}-saved-${safeState}-${category}`;
    throw error;
  }
}

export function classifyConflict(value: string | null): string {
  if (value === 'template') return 'template';
  const separator = value?.indexOf(':') ?? -1;
  const kind = separator > 0 ? value?.slice(0, separator) : undefined;
  const command = separator > 0 ? value?.slice(separator + 1) : undefined;
  const safeKinds = [
    'context-changed',
    'destructive-reconfirmation',
    'identity-missing',
    'identity-retyped',
    'membership-changed',
    'photo-changed',
    'superseded-after-success',
    'target-changed',
  ];
  const safeCommand = command === 'customization'
    ? 'customization'
    : command === 'photoCrop'
      ? 'photo-crop'
      : command === 'photoDelete'
        ? 'photo-delete'
        : command === 'photoUpload'
          ? 'photo-upload'
          : command === 'structure'
            ? 'structure'
            : undefined;
  return kind !== undefined
    && safeKinds.includes(kind)
    && safeCommand !== undefined
    ? `${kind}-${safeCommand}`
    : 'conflict-other';
}

export async function ensureAuthenticated(page: Page): Promise<void> {
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

export async function readURLBaseline(page: Page): Promise<URLBaseline> {
  return page.evaluate(() => ({
    hash: location.hash,
    href: location.href,
    search: location.search,
  }));
}

export function isOwnerRead(request: Request, resumeID: string): boolean {
  const url = new URL(request.url());
  return request.method() === 'GET'
    && url.origin === ORIGIN
    && url.pathname === `/api/v1/resumes/${resumeID}`;
}

export function isResumeMutation(request: Request, resumeID: string): boolean {
  const url = new URL(request.url());
  return request.method() !== 'GET'
    && url.origin === ORIGIN
    && url.pathname.startsWith(`/api/v1/resumes/${resumeID}`);
}

export function isTemplateChildRequest(request: Request): boolean {
  if (request.method() !== 'PATCH') return false;
  const url = new URL(request.url());
  return url.origin === ORIGIN
    && /^\/api\/v1\/resumes\/[0-9a-f-]{36}\/(?:customization|structure)$/.test(url.pathname);
}

export function isPhotoReadRequest(request: Request): boolean {
  if (request.method() !== 'GET') return false;
  return isPhotoRequest(request);
}

export function isPhotoUploadRequest(request: Request): boolean {
  if (request.method() !== 'POST') return false;
  return isPhotoRequest(request);
}

export function isPhotoCropRequest(request: Request): boolean {
  if (request.method() !== 'PATCH') return false;
  return isPhotoRequest(request);
}

export function isPhotoRequest(request: Request): boolean {
  const url = new URL(request.url());
  return url.origin === ORIGIN
    && /^\/api\/v1\/resumes\/[0-9a-f-]{36}\/photo$/.test(url.pathname);
}

export function object(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

export async function writeEditorEvidence(steps: EditorSteps): Promise<void> {
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
