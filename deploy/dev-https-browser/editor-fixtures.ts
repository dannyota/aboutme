import { expect, type Page, type Response } from '@playwright/test';

import { signInWithGoogle } from './harness-lib';

export interface PersistenceCounts {
  readonly localStorage: number;
  readonly sessionStorage: number;
  readonly indexedDB: number;
  readonly history: number;
  readonly sendBeacon: number;
}

export interface BrowserPersistenceProbe {
  read(): Promise<PersistenceCounts>;
  reset(): Promise<void>;
}

export interface PhotoSourceReadCounts {
  readonly blobArrayBuffer: number;
  readonly dataURL: number;
  readonly fileReader: number;
  readonly imageDecode: number;
  readonly objectURL: number;
}

export interface PhotoSourceReadProbe {
  read(): Promise<PhotoSourceReadCounts>;
}

export interface AcceptedResume {
  readonly metadata: {
    readonly id: string;
    readonly title: string;
  };
  readonly revision: string;
}

interface OwnerRequestResult {
  readonly body: unknown;
  readonly etag: string;
  readonly status: number;
}

const ORIGIN = 'https://localhost:20443';
const SCHEMA_VERSION = '2';

export function uniqueTitle(): string {
  return `Editor proof ${crypto.randomUUID()}`;
}

export async function loginAsDevelopmentUser(page: Page): Promise<void> {
  await signInWithGoogle(page, { fromLoginPage: true, keyboard: true });
}

export async function createBlankResume(
  page: Page,
  title: string,
): Promise<AcceptedResume> {
  await page.goto('/app/resumes');
  await expect(page.getByRole('heading', { name: 'Resumes' })).toBeVisible();
  const created = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return request.method() === 'POST'
      && url.origin === ORIGIN
      && url.pathname === '/api/v1/resumes';
  });
  await page.getByTestId('create-resume').press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Create resume' });
  await dialog.getByLabel('Title').fill(title);
  await dialog.getByRole('button', { name: 'Create' }).press('Enter');
  const response = await created;
  if (response.status() === 409 && await responseErrorCode(response) === 'resume_cap_exceeded') {
    throw new Error('resume_cap_exceeded');
  }
  const accepted = await acceptedResume(response, 201);
  await page.waitForURL((url) =>
    url.origin === ORIGIN
    && url.pathname === `/app/resumes/${accepted.metadata.id}`
  );
  return accepted;
}

export async function acceptedResume(
  response: Response,
  expectedStatus: 200 | 201,
): Promise<AcceptedResume> {
  expect(response.status()).toBe(expectedStatus);
  expect(response.headers()['cache-control']).toBe('no-store, no-transform');
  const etag = response.headers().etag;
  expect(etag).toMatch(/^"r[1-9][0-9]*"$/);
  const body = await response.json() as { data?: unknown };
  const data = objectOf(body.data);
  const id = data.id;
  const title = data.title;
  const revision = data.revision;
  expect(typeof id).toBe('string');
  expect(typeof title).toBe('string');
  expect(typeof revision).toBe('string');
  expect(revision).toMatch(/^[1-9][0-9]*$/);
  return {
    metadata: { id: id as string, title: title as string },
    revision: revision as string,
  };
}

export async function freshCSRF(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/me', {
      cache: 'no-store',
      credentials: 'include',
    });
    const body = await response.json() as { data?: { csrfToken?: unknown } };
    const token = body.data?.csrfToken;
    if (response.status !== 200 || typeof token !== 'string' || token === '') {
      throw new Error('authenticated CSRF read failed');
    }
    return token;
  });
}

export async function mutateRemoteMetadata(
  page: Page,
  id: string,
  title: string,
): Promise<AcceptedResume> {
  const result = await ownerMutation(page, id, { title });
  return acceptedFromOwnerResult(result);
}

export async function mutateRemoteHeadline(
  page: Page,
  id: string,
  headline: string,
): Promise<AcceptedResume> {
  const result = await ownerMutation(page, id, undefined, headline);
  return acceptedFromOwnerResult(result);
}

export async function deleteRecordedResume(
  page: Page,
  id: string,
): Promise<void> {
  const token = await freshCSRF(page);
  const owner = await page.evaluate(
    async ({ id, schemaVersion, token }): Promise<OwnerRequestResult> => {
      const read = await fetch(`/api/v1/resumes/${encodeURIComponent(id)}`, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      if (read.status === 404) return { body: null, etag: '', status: 404 };
      const etag = read.headers.get('etag') ?? '';
      const body: unknown = await read.json();
      if (read.status !== 200 || !/^"r[1-9][0-9]*"$/.test(etag)) {
        throw new Error('recorded resume read failed');
      }
      const removed = await fetch(`/api/v1/resumes/${encodeURIComponent(id)}`, {
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': etag,
          'X-CSRF-Token': token,
          'X-Resume-Schema-Version': schemaVersion,
        },
        method: 'DELETE',
      });
      return {
        body,
        etag: removed.headers.get('etag') ?? '',
        status: removed.status,
      };
    },
    { id, schemaVersion: SCHEMA_VERSION, token },
  );
  if (owner.status === 404) return;
  expect(owner.status).toBe(204);
}

export async function deleteRemoteEntry(
  page: Page,
  resumeID: string,
  sectionKey: string,
  entryID: string,
): Promise<void> {
  const token = await freshCSRF(page);
  const status = await page.evaluate(
    async ({ entryID, resumeID, schemaVersion, sectionKey, token }): Promise<number> => {
      const read = await fetch(`/api/v1/resumes/${encodeURIComponent(resumeID)}`, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const etag = read.headers.get('etag') ?? '';
      if (read.status !== 200 || !/^"r[1-9][0-9]*"$/.test(etag)) {
        throw new Error('remote entry winner read failed');
      }
      const response = await fetch(
        `/api/v1/resumes/${encodeURIComponent(resumeID)}/entries/${encodeURIComponent(sectionKey)}/${encodeURIComponent(entryID)}`,
        {
          cache: 'no-store',
          credentials: 'include',
          headers: {
            'Idempotency-Key': crypto.randomUUID(),
            'If-Match': etag,
            'X-CSRF-Token': token,
            'X-Resume-Schema-Version': schemaVersion,
          },
          method: 'DELETE',
        },
      );
      return response.status;
    },
    { entryID, resumeID, schemaVersion: SCHEMA_VERSION, sectionKey, token },
  );
  expect(status).toBe(204);
}

export async function replaceRemotePhoto(
  page: Page,
  resumeID: string,
  base64PNG: string,
): Promise<void> {
  const token = await freshCSRF(page);
  const status = await page.evaluate(
    async ({ base64PNG, resumeID, schemaVersion, token }): Promise<number> => {
      const read = await fetch(`/api/v1/resumes/${encodeURIComponent(resumeID)}`, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const etag = read.headers.get('etag') ?? '';
      if (read.status !== 200 || !/^"r[1-9][0-9]*"$/.test(etag)) {
        throw new Error('remote photo winner read failed');
      }
      const bytes = Uint8Array.from(atob(base64PNG), (character) => character.charCodeAt(0));
      const body = new FormData();
      body.append('file', new File([bytes], 'replacement.png', { type: 'image/png' }));
      const response = await fetch(`/api/v1/resumes/${encodeURIComponent(resumeID)}/photo`, {
        body,
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': etag,
          'X-CSRF-Token': token,
          'X-Resume-Schema-Version': schemaVersion,
        },
        method: 'POST',
      });
      return response.status;
    },
    { base64PNG, resumeID, schemaVersion: SCHEMA_VERSION, token },
  );
  expect(status).toBe(200);
}

export async function ownerPhotoHasNoCrop(
  page: Page,
  resumeID: string,
): Promise<boolean> {
  return page.evaluate(
    async ({ resumeID, schemaVersion }): Promise<boolean> => {
      const response = await fetch(`/api/v1/resumes/${encodeURIComponent(resumeID)}`, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const body = await response.json() as { data?: unknown };
      if (response.status !== 200) throw new Error('photo crop owner read failed');
      const record = (value: unknown): Record<string, unknown> => {
        if (typeof value !== 'object' || value === null || Array.isArray(value)) {
          throw new Error('photo crop owner response was invalid');
        }
        return value as Record<string, unknown>;
      };
      const source = record(body.data);
      const document = record(source.document);
      const details = record(document.personalDetails);
      const photo = details.photo;
      if (photo === null || photo === undefined) throw new Error('owner photo is missing');
      return !Object.hasOwn(record(photo), 'crop');
    },
    { resumeID, schemaVersion: SCHEMA_VERSION },
  );
}

export async function installPhotoSourceReadProbes(
  page: Page,
): Promise<PhotoSourceReadProbe> {
  await page.evaluate(() => {
    const key = '__aboutmeEditorPhotoSourceReadProbe';
    const probeStore = window as unknown as Record<string, unknown>;
    const existing = probeStore[key];
    if (existing !== undefined) throw new Error('photo source probe already installed');
    const counts = {
      blobArrayBuffer: 0,
      dataURL: 0,
      fileReader: 0,
      imageDecode: 0,
      objectURL: 0,
    };
    const blobArrayBuffer = Blob.prototype.arrayBuffer;
    Blob.prototype.arrayBuffer = function blobArrayBufferProbe(this: Blob): Promise<ArrayBuffer> {
      counts.blobArrayBuffer += 1;
      return blobArrayBuffer.call(this);
    };
    const dataURL = window.btoa.bind(window);
    window.btoa = function dataURLProbe(value: string): string {
      counts.dataURL += 1;
      return dataURL(value);
    };
    const fileReaderMethods = [
      'readAsArrayBuffer',
      'readAsBinaryString',
      'readAsDataURL',
      'readAsText',
    ] as const;
    const fileReader = FileReader.prototype as unknown as Partial<Record<
      string,
      (this: FileReader, ...args: unknown[]) => unknown
    >>;
    for (const name of fileReaderMethods) {
      const original = fileReader[name];
      if (original === undefined) {
        throw new Error(`FileReader.${name} is unavailable`);
      }
      fileReader[name] = function fileReaderProbe(
        this: FileReader,
        ...args: unknown[]
      ): unknown {
        counts.fileReader += 1;
        return original.apply(this, args);
      };
    }
    const imageDecode = HTMLImageElement.prototype.decode;
    HTMLImageElement.prototype.decode = function imageDecodeProbe(
      this: HTMLImageElement,
    ): Promise<void> {
      counts.imageDecode += 1;
      return imageDecode.call(this);
    };
    const objectURL = URL.createObjectURL.bind(URL);
    URL.createObjectURL = function objectURLProbe(
      ...args: Parameters<typeof URL.createObjectURL>
    ): string {
      counts.objectURL += 1;
      return objectURL(...args);
    };
    probeStore[key] = counts;
  });
  return {
    read: async (): Promise<PhotoSourceReadCounts> => page.evaluate(() => {
      const key = '__aboutmeEditorPhotoSourceReadProbe';
      const probeStore = window as unknown as Record<string, unknown>;
      const value = probeStore[key];
      if (typeof value !== 'object' || value === null) {
        throw new Error('photo source probe is unavailable');
      }
      const count = (item: unknown): number => {
        if (typeof item !== 'number' || !Number.isSafeInteger(item) || item < 0) {
          throw new Error('invalid photo source probe count');
        }
        return item;
      };
      const counts = value as Record<string, unknown>;
      return {
        blobArrayBuffer: count(counts.blobArrayBuffer),
        dataURL: count(counts.dataURL),
        fileReader: count(counts.fileReader),
        imageDecode: count(counts.imageDecode),
        objectURL: count(counts.objectURL),
      };
    }),
  };
}

export async function installBrowserPersistenceProbes(
  page: Page,
): Promise<BrowserPersistenceProbe> {
  await page.addInitScript(() => {
    const key = '__aboutmeEditorPersistenceProbe';
    const probeStore = window as unknown as Record<string, unknown>;
    const existing = probeStore[key];
    if (existing !== undefined) throw new Error('persistence probe already installed');
    const counts = {
      history: 0,
      indexedDB: 0,
      localStorage: 0,
      sendBeacon: 0,
      sessionStorage: 0,
    };
    const local = window.localStorage;
    const session = window.sessionStorage;
    const storage = Storage.prototype as Storage & {
      clear: () => void;
      getItem: (key: string) => string | null;
      removeItem: (key: string) => void;
      setItem: (key: string, value: string) => void;
    };
    const countStorage = (target: Storage): void => {
      if (target === local) counts.localStorage += 1;
      if (target === session) counts.sessionStorage += 1;
    };
    const getItem = storage.getItem;
    const setItem = storage.setItem;
    const removeItem = storage.removeItem;
    const clear = storage.clear;
    storage.getItem = function getItemProbe(this: Storage, value: string): string | null {
      countStorage(this);
      return getItem.call(this, value);
    };
    storage.setItem = function setItemProbe(this: Storage, name: string, value: string): void {
      countStorage(this);
      setItem.call(this, name, value);
    };
    storage.removeItem = function removeItemProbe(this: Storage, value: string): void {
      countStorage(this);
      removeItem.call(this, value);
    };
    storage.clear = function clearProbe(this: Storage): void {
      countStorage(this);
      clear.call(this);
    };
    const open = window.indexedDB.open.bind(window.indexedDB);
    const removeDatabase = window.indexedDB.deleteDatabase.bind(window.indexedDB);
    window.indexedDB.open = function indexedDBOpenProbe(...args: Parameters<IDBFactory['open']>): IDBOpenDBRequest {
      counts.indexedDB += 1;
      return open(...args);
    };
    window.indexedDB.deleteDatabase = function indexedDBDeleteProbe(
      ...args: Parameters<IDBFactory['deleteDatabase']>
    ): IDBOpenDBRequest {
      counts.indexedDB += 1;
      return removeDatabase(...args);
    };
    const pushState = history.pushState.bind(history);
    const replaceState = history.replaceState.bind(history);
    history.pushState = function pushStateProbe(...args: Parameters<History['pushState']>): void {
      counts.history += 1;
      pushState(...args);
    };
    history.replaceState = function replaceStateProbe(...args: Parameters<History['replaceState']>): void {
      counts.history += 1;
      replaceState(...args);
    };
    const beacon = navigator.sendBeacon.bind(navigator);
    navigator.sendBeacon = function sendBeaconProbe(...args: Parameters<Navigator['sendBeacon']>): boolean {
      counts.sendBeacon += 1;
      return beacon(...args);
    };
    probeStore[key] = counts;
  });
  return {
    read: async (): Promise<PersistenceCounts> => page.evaluate(() => {
      const count = (value: unknown): number => {
        if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
          throw new Error('invalid persistence probe count');
        }
        return value;
      };
      const probeStore = window as unknown as {
        __aboutmeEditorPersistenceProbe?: unknown;
      };
      const value = probeStore.__aboutmeEditorPersistenceProbe;
      if (typeof value !== 'object' || value === null) {
        throw new Error('persistence probe is unavailable');
      }
      const counts = value as Record<string, unknown>;
      return {
        history: count(counts.history),
        indexedDB: count(counts.indexedDB),
        localStorage: count(counts.localStorage),
        sendBeacon: count(counts.sendBeacon),
        sessionStorage: count(counts.sessionStorage),
      };
    }),
    reset: async (): Promise<void> => page.evaluate(() => {
      const probeStore = window as unknown as {
        __aboutmeEditorPersistenceProbe?: unknown;
      };
      const value = probeStore.__aboutmeEditorPersistenceProbe;
      if (typeof value !== 'object' || value === null) {
        throw new Error('persistence probe is unavailable');
      }
      const counts = value as Record<string, unknown>;
      for (const key of [
        'history',
        'indexedDB',
        'localStorage',
        'sendBeacon',
        'sessionStorage',
      ]) {
        if (typeof counts[key] !== 'number') {
          throw new Error('invalid persistence probe count');
        }
        counts[key] = 0;
      }
    }),
  };
}

export async function settledVisiblePageCount(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const snapshot = (): number | null => {
      const pages = [...document.querySelectorAll<HTMLElement>('.resume-page[data-page-index]')]
        .filter((element) => {
          if (element.closest('.pagination-measurement') !== null) return false;
          const style = getComputedStyle(element);
          const rect = element.getBoundingClientRect();
          return style.display !== 'none'
            && style.visibility !== 'hidden'
            && !element.inert
            && rect.width > 0
            && rect.height > 0;
        })
        .map((element) => Number(element.dataset.pageIndex));
      if (pages.length === 0 || pages.some((index) => !Number.isInteger(index))) {
        return null;
      }
      pages.sort((left, right) => left - right);
      return pages.every((index, position) => index === position) ? pages.length : null;
    };
    const frame = (): Promise<void> => new Promise((resolve) => requestAnimationFrame(() => resolve()));
    const first = snapshot();
    await frame();
    await frame();
    const second = snapshot();
    return first !== null && first === second ? first : -1;
  });
}

async function ownerMutation(
  page: Page,
  id: string,
  metadata: { readonly title: string } | undefined,
  headline?: string,
): Promise<OwnerRequestResult> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ headline, id, metadata, schemaVersion, token }): Promise<OwnerRequestResult> => {
      const record = (value: unknown): Record<string, unknown> => {
        if (typeof value !== 'object' || value === null || Array.isArray(value)) {
          throw new Error('remote winner response was invalid');
        }
        return value as Record<string, unknown>;
      };
      const read = await fetch(`/api/v1/resumes/${encodeURIComponent(id)}`, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const readBody = await read.json() as { data?: unknown };
      const source = record(readBody.data);
      const etag = read.headers.get('etag') ?? '';
      if (read.status !== 200 || !/^"r[1-9][0-9]*"$/.test(etag)) {
        throw new Error('remote winner read failed');
      }
      const path = metadata === undefined
        ? `/api/v1/resumes/${encodeURIComponent(id)}/personal-details`
        : `/api/v1/resumes/${encodeURIComponent(id)}`;
      const details = record(source.document).personalDetails;
      const body = metadata ?? {
        ...record(details),
        headline,
      };
      if (metadata === undefined) delete (body as Record<string, unknown>).photo;
      const response = await fetch(path, {
        body: JSON.stringify(body),
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': etag,
          'X-CSRF-Token': token,
          'X-Resume-Schema-Version': schemaVersion,
        },
        method: 'PATCH',
      });
      return {
        body: await response.json(),
        etag: response.headers.get('etag') ?? '',
        status: response.status,
      };
    },
    { headline, id, metadata, schemaVersion: SCHEMA_VERSION, token },
  );
}

function acceptedFromOwnerResult(result: OwnerRequestResult): AcceptedResume {
  expect(result.status).toBe(200);
  expect(result.etag).toMatch(/^"r[1-9][0-9]*"$/);
  const data = objectOf(objectOf(result.body).data);
  const id = data.id;
  const title = data.title;
  const revision = data.revision;
  expect(typeof id).toBe('string');
  expect(typeof title).toBe('string');
  expect(typeof revision).toBe('string');
  return {
    metadata: { id: id as string, title: title as string },
    revision: revision as string,
  };
}

async function responseErrorCode(response: Response): Promise<string | undefined> {
  const body = await response.json() as { error?: { code?: unknown } };
  return typeof body.error?.code === 'string' ? body.error.code : undefined;
}

function objectOf(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('expected object response');
  }
  return value as Record<string, unknown>;
}
