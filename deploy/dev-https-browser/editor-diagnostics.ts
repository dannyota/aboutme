import { expect, type BrowserContext, type Page } from '@playwright/test';
import { newDiagnosticCounters, pageDiagnosticsAttacher } from './harness-lib';
import {
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
  isExpectedNegativeHTTPConsole,
  httpFailureStatus,
} from './network-policy';
import {
  type EditorProofState,
  type Diagnostics,
  type PausedPhotoRequest,
  type PausedPhotoUpload,
  type PendingRequestGate,
  createPendingRequestGate,
  isTemplateChildRequest,
  isPhotoReadRequest,
  isPhotoUploadRequest,
  isPhotoCropRequest,
} from './editor-proof-state';

export async function installDiagnostics(
  state: EditorProofState,
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
        firstOtherConsoleStage = state.editorDiagnosticStage;
      }
      if (firstConsoleError === 'none') {
        firstConsoleError = classifyDiagnosticText(message.text());
        firstConsoleStage = state.editorDiagnosticStage;
      }
    },
    onPageError: (error) => {
      if (firstPageError === 'none') {
        firstPageError = classifyDiagnosticText(error.message);
        firstPageErrorLead = classifyDiagnosticLead(error.message);
        firstPageErrorName = classifyDiagnosticName(error.name);
        firstPageStage = state.editorDiagnosticStage;
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
      state.editorDiagnosticStage = [
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

export function classifyDiagnosticText(message: string): string {
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

export function classifyDiagnosticName(name: string): string {
  const normalized = name.toLowerCase();
  if (normalized === 'error') return 'error';
  if (normalized === 'typeerror') return 'typeerror';
  if (normalized === 'aborterror') return 'aborterror';
  if (normalized === 'fetcherror') return 'fetcherror';
  if (normalized === 'domexception') return 'domexception';
  const safe = normalized.replace(/[^a-z]/g, '').slice(0, 24);
  return safe === '' ? 'other' : safe;
}

export function classifyDiagnosticLead(message: string): string {
  const lead = message.trim().match(/[A-Za-z]+/)?.[0]?.toLowerCase() ?? '';
  return lead.slice(0, 24) || 'other';
}
