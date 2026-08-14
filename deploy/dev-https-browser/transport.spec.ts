import { expect, test, type Page } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
} from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/transport-proof.json';
const SCHEMA_VERSION = '2';

interface TransportResult {
  acceptedETag: string;
  auth: boolean;
  cache: boolean;
  etag: boolean;
  ifMatch: boolean;
  teardown: boolean;
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function caseInsensitiveHeader(
  headers: Record<string, unknown>,
  name: string,
): string {
  const entry = Object.entries(headers).find(
    ([key]) => key.toLowerCase() === name.toLowerCase(),
  );
  return typeof entry?.[1] === 'string' ? entry[1] : '';
}

function requireStrongParentETag(value: string | null): string {
  if (value === null || !/^"r[1-9][0-9]*"$/.test(value)) {
    throw new Error('owner response did not carry a strong parent ETag');
  }
  return value;
}

test('proves authenticated transport preserves cache and precondition bytes', async ({
  context,
  page,
}) => {
  let certificateErrors = 0;
  let consoleErrors = 0;
  let externalRequests = 0;
  let pageErrors = 0;
  let observedAcceptEncoding = '';
  let capturedIfMatch = '';

  const networkSession = await context.newCDPSession(page);
  const resumeReadRequests = new Set<string>();
  const networkExtraHeaders = new Map<string, Record<string, unknown>>();
  const captureNetworkHeaders = (requestId: string): void => {
    if (!resumeReadRequests.has(requestId)) return;
    const headers = networkExtraHeaders.get(requestId);
    if (headers) {
      observedAcceptEncoding = caseInsensitiveHeader(
        headers,
        'accept-encoding',
      );
    }
  };
  networkSession.on('Network.requestWillBeSent', (event: unknown) => {
    const record = recordValue(event);
    const request = recordValue(record?.request);
    const requestId = record?.requestId;
    if (
      typeof requestId !== 'string' ||
      request?.method !== 'GET' ||
      typeof request.url !== 'string'
    ) {
      return;
    }
    const url = new URL(request.url);
    if (
      url.origin === ORIGIN &&
      /^\/api\/v1\/resumes\/[0-9a-f-]{36}$/i.test(url.pathname)
    ) {
      resumeReadRequests.add(requestId);
      captureNetworkHeaders(requestId);
    }
  });
  networkSession.on('Network.requestWillBeSentExtraInfo', (event: unknown) => {
    const record = recordValue(event);
    const requestId = record?.requestId;
    const headers = recordValue(record?.headers);
    if (typeof requestId !== 'string' || headers === null) return;
    networkExtraHeaders.set(requestId, headers);
    captureNetworkHeaders(requestId);
  });
  await networkSession.send('Network.enable');

  const attachPageDiagnostics = (openedPage: Page): void => {
    openedPage.on('console', (message) => {
      if (message.type() === 'error') consoleErrors += 1;
    });
    openedPage.on('pageerror', () => {
      pageErrors += 1;
    });
    openedPage.on('requestfailed', (request) => {
      if (/CERT/i.test(request.failure()?.errorText ?? ''))
        certificateErrors += 1;
    });
  };
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  await context.route('**/*', async (route) => {
    const request = route.request();
    if (!isAllowedHTTPURL(request.url())) {
      externalRequests += 1;
      await route.abort('blockedbyclient');
      return;
    }
    const url = new URL(request.url());
    if (
      request.method() === 'PATCH' &&
      /^\/api\/v1\/resumes\/[0-9a-f-]{36}$/i.test(url.pathname)
    ) {
      capturedIfMatch = request.headers()['if-match'] ?? '';
    }
    await route.continue();
  });
  await context.routeWebSocket('**/*', async (webSocket) => {
    if (!isAllowedWebSocketURL(webSocket.url())) {
      externalRequests += 1;
      await webSocket.close({ code: 1008, reason: 'blocked' });
      return;
    }
    webSocket.connectToServer();
  });

  const loginResponse = await page.goto('/login');
  expect(loginResponse?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await Promise.all([
    page.waitForURL(
      (url) =>
        url.origin === ORIGIN &&
        url.pathname === '/__uat/oauth/google/authorize',
    ),
    page.getByRole('link', { name: 'Continue with Google' }).click(),
  ]);
  const developmentAccount = page.getByLabel(
    'Development User — developer@example.invalid',
  );
  await expect(developmentAccount).toBeChecked();
  await Promise.all([
    page.waitForURL((url) => url.origin === ORIGIN && url.pathname === '/'),
    page.getByRole('button', { name: 'Continue with Google' }).click(),
  ]);

  const result = await page.evaluate(
    async (schemaVersion): Promise<TransportResult> => {
      const uuidPattern = /^[0-9a-f-]{36}$/i;
      const createdIds = new Set<string>();
      const headers = (csrfToken: string): Record<string, string> => ({
        'Content-Type': 'application/json',
        'Idempotency-Key': crypto.randomUUID(),
        'X-CSRF-Token': csrfToken,
        'X-Resume-Schema-Version': schemaVersion,
      });
      const responseData = async (
        response: Response,
      ): Promise<Record<string, unknown>> => {
        const body: unknown = await response.json();
        const data = (body as { data?: unknown }).data;
        if (typeof data !== 'object' || data === null || Array.isArray(data)) {
          throw new Error('owner response did not contain a data object');
        }
        return data as Record<string, unknown>;
      };
      const errorCode = async (response: Response): Promise<unknown> => {
        const body: unknown = await response.json();
        return (body as { error?: { code?: unknown } }).error?.code;
      };
      const requireETag = (value: string | null): string => {
        if (value === null || !/^"r[1-9][0-9]*"$/.test(value)) {
          throw new Error('owner response did not carry a strong parent ETag');
        }
        return value;
      };

      const me = await fetch('/api/v1/me', {
        cache: 'no-store',
        credentials: 'include',
      });
      const meBody: unknown = await me.json();
      const csrfToken = (
        meBody as { data?: { csrfToken?: unknown; user?: { email?: unknown } } }
      ).data?.csrfToken;
      const email = (meBody as { data?: { user?: { email?: unknown } } }).data
        ?.user?.email;
      if (
        me.status !== 200 ||
        typeof csrfToken !== 'string' ||
        csrfToken.length === 0 ||
        email !== 'developer@example.invalid'
      ) {
        throw new Error('fixed development account was not authenticated');
      }

      let acceptedETag = '';
      let latestETag = '';
      let teardown = false;
      try {
        const title = `Transport proof ${crypto.randomUUID()}`;
        const created = await fetch('/api/v1/resumes', {
          body: JSON.stringify({ title }),
          credentials: 'include',
          headers: headers(csrfToken),
          method: 'POST',
        });
        if (
          created.status === 409 &&
          (await errorCode(created)) === 'resume_cap_exceeded'
        ) {
          throw new Error('resume_cap_exceeded');
        }
        if (created.status !== 201) {
          throw new Error(`resume create returned ${created.status}`);
        }
        const createdData = await responseData(created);
        if (
          typeof createdData.id !== 'string' ||
          !uuidPattern.test(createdData.id) ||
          createdData.title !== title ||
          createdData.schemaVersion !== Number(schemaVersion)
        ) {
          throw new Error('resume create response was invalid');
        }
        createdIds.add(createdData.id);
        latestETag = requireETag(created.headers.get('ETag'));

        const read = await fetch(`/api/v1/resumes/${createdData.id}`, {
          cache: 'no-store',
          credentials: 'include',
          headers: { 'X-Resume-Schema-Version': schemaVersion },
        });
        if (read.status !== 200)
          throw new Error(`resume read returned ${read.status}`);
        const readData = await responseData(read);
        if (
          readData.id !== createdData.id ||
          readData.schemaVersion !== Number(schemaVersion)
        ) {
          throw new Error('resume read response was invalid');
        }
        if (read.headers.get('Cache-Control') !== 'no-store, no-transform') {
          throw new Error('authenticated cache policy changed');
        }
        latestETag = requireETag(read.headers.get('ETag'));
        acceptedETag = latestETag;

        const patchedTitle = `${title} patched`;
        const patched = await fetch(`/api/v1/resumes/${createdData.id}`, {
          body: JSON.stringify({ title: patchedTitle }),
          credentials: 'include',
          headers: { ...headers(csrfToken), 'If-Match': acceptedETag },
          method: 'PATCH',
        });
        if (patched.status !== 200) {
          throw new Error(`resume patch returned ${patched.status}`);
        }
        latestETag = requireETag(patched.headers.get('ETag'));
        const patchedData = await responseData(patched);
        if (
          patchedData.id !== createdData.id ||
          patchedData.title !== patchedTitle
        ) {
          throw new Error('resume patch response was invalid');
        }
      } finally {
        for (const id of createdIds) {
          const removed = await fetch(`/api/v1/resumes/${id}`, {
            credentials: 'include',
            headers: {
              'Idempotency-Key': crypto.randomUUID(),
              'If-Match': latestETag,
              'X-CSRF-Token': csrfToken,
              'X-Resume-Schema-Version': schemaVersion,
            },
            method: 'DELETE',
          });
          if (removed.status !== 204) {
            throw new Error(`resume cleanup returned ${removed.status}`);
          }
        }
        teardown = createdIds.size > 0;
      }
      return {
        acceptedETag,
        auth: true,
        cache: true,
        etag: true,
        ifMatch: true,
        teardown,
      };
    },
    SCHEMA_VERSION,
  );

  await expect.poll(() => observedAcceptEncoding).not.toBe('');
  expect(capturedIfMatch).toBe(result.acceptedETag);
  expect(requireStrongParentETag(result.acceptedETag)).toBe(
    result.acceptedETag,
  );
  expect({
    auth: result.auth,
    cache: result.cache,
    etag: result.etag,
    ifMatch: result.ifMatch,
    teardown: result.teardown,
  }).toEqual({
    auth: true,
    cache: true,
    etag: true,
    ifMatch: true,
    teardown: true,
  });
  expect({
    certificateErrors,
    consoleErrors,
    externalRequests,
    pageErrors,
  }).toEqual({
    certificateErrors: 0,
    consoleErrors: 0,
    externalRequests: 0,
    pageErrors: 0,
  });
  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify(
      {
        errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
        origin: ORIGIN,
        scenario: 'authenticated-transport',
        schemaVersion: 1,
        steps: {
          auth: true,
          cache: true,
          etag: true,
          ifMatch: true,
          teardown: true,
        },
      },
      null,
      2,
    )}\n`,
    { flag: 'wx', mode: 0o600 },
  );
});
