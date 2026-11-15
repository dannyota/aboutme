import type { Page, Request, Response } from '@playwright/test';
import { writeFile } from 'node:fs/promises';

export const EDITOR_DIAGNOSTIC_PATH = '/evidence/editor-diagnostic.json';
const COLLECTION_BUDGET_MS = 2_000;
const MAX_BYTES = 2_048;
const MAX_STAGE_LENGTH = 256;
const UNAVAILABLE = 'unavailable';

const FAILURE_CATEGORIES = [
  'assertion-timeout',
  'assertion',
  'navigation',
  'page-closed',
  'other',
] as const;
const URL_CATEGORIES = [
  'login',
  'editor',
  'oauth-authorize',
  'oauth-callback',
  'app-other',
  'about-blank',
  'other-origin',
  UNAVAILABLE,
] as const;
const READY_STATES = ['loading', 'interactive', 'complete', UNAVAILABLE] as const;
const STATUS_CLASSES = ['2xx', '3xx', '4xx', '5xx', 'none', UNAVAILABLE] as const;
const EVENT_CATEGORIES = ['chunk-load', 'hydration', 'network', 'other'] as const;

export type FailureCategory = typeof FAILURE_CATEGORIES[number];
export type URLCategory = typeof URL_CATEGORIES[number];
export type StatusClass = typeof STATUS_CLASSES[number];
export type EventCategory = typeof EVENT_CATEGORIES[number];
type EventCounts = Record<EventCategory, number>;

export const STAGE_WORDS: ReadonlySet<string> = new Set([
  'accept', 'accepted', 'account', 'accessibility', 'add', 'after', 'alert',
  'apply', 'assert', 'audit', 'authorize', 'autosave', 'barrier', 'blank',
  'blur', 'button', 'callback', 'cap', 'changed', 'checked', 'child',
  'cleanup', 'close', 'closed', 'complete', 'confirm', 'conflict', 'contact',
  'context', 'count', 'create', 'created', 'crop', 'customization', 'dark',
  'default', 'delay', 'delete', 'destroy', 'destroyed', 'destructive',
  'details', 'diagnostics', 'dialog', 'dirty', 'dispatch', 'dispatched',
  'document', 'edit', 'editor', 'empty', 'ended', 'entries', 'error',
  'events', 'evidence', 'exceeded', 'failed', 'failure', 'fill', 'filled',
  'first', 'heading', 'headline', 'held', 'identity', 'install', 'invalid',
  'items', 'keep', 'label', 'labels', 'later', 'latest', 'light', 'line',
  'list', 'loaded', 'loads', 'local', 'login', 'loss', 'lost', 'membership',
  'menu', 'mine', 'missing', 'move', 'navigate', 'new', 'no', 'none', 'not',
  'offline', 'open', 'other', 'page', 'partial', 'paused', 'persistence',
  'personal', 'photo', 'post', 'probes', 'queued', 'read', 'reached',
  'reads', 'reads401', 'ready', 'reauth', 'rebase', 'reconfirmation',
  'recreate', 'release', 'released', 'reload', 'reloads', 'remote', 'rename',
  'reopen', 'reorder', 'request', 'reset', 'response', 'resume', 'retrying',
  'retyped', 'row', 'safe', 'same', 'save', 'saved', 'saving', 'scenario',
  'seen', 'select', 'selected', 'session', 'settings', 'setup', 'shown',
  'signin', 'skill', 'stale', 'start', 'state', 'steps', 'streams',
  'structure', 'submitted', 'success', 'superseded', 'target', 'teardown',
  'template', 'theme', 'title', 'undo', 'unexpected', 'upload', 'url',
  'value', 'visible', 'winner', 'work', 'writes',
]);

const CLOSED_STAGE_SUFFIX = /^(?:-line-[0-9]{1,4})?(?:-reloads-[0-9]{1,6})?$/;
const OPEN_STAGE_FAMILIES: readonly RegExp[] = [
  /^(post-diagnostics)(-.*)$/,
  /^(accessibility-editor-(?:light|dark))(-.*)$/,
  /^(accessibility-list)(-(?!(?:audit|reopen|theme)(?:-|$)).*)$/,
];

const PAGE_CLOSED_TEXT = /(?:Target page, context or browser|Target|Page|Browser|Context) (?:has been )?closed|TargetClosedError/i;
const ASSERTION_TEXT = /\bexpect(?:\.poll|\.soft)?\(|AssertionError|ERR_ASSERTION/;
const TIMEOUT_TEXT = /\bTimeout:? *[0-9]+ *ms\b|\bTimed out [0-9]+ *ms\b|\bTimeout [0-9]+ *ms exceeded\b|\btimeout of [0-9]+ *ms exceeded\b|\btimed out\b/i;
const NAVIGATION_TEXT = /\b(?:page|frame)\.(?:goto|reload|goBack|goForward|waitForURL|waitForNavigation|waitForLoadState)\b|net::ERR_|NS_ERROR_|interrupted by another navigation|navigation failed|frame was detached/i;
const CHUNK_TEXT = /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Loading (?:CSS )?chunk \S+ failed|ChunkLoadError|Unable to preload CSS/i;
const NETWORK_TEXT = /Failed to fetch|NetworkError|Load failed|network ?error|net::ERR_/i;
const NETWORK_FAILURE = /ERR_(?:CONNECTION_[A-Z_]+|NAME_NOT_RESOLVED|INTERNET_DISCONNECTED|NETWORK_[A-Z_]+|TIMED_OUT|ADDRESS_[A-Z_]+|EMPTY_RESPONSE|HTTP2_[A-Z_]+|QUIC_[A-Z_]+|SSL_[A-Z_]+|CERT_[A-Z_]+|PROXY_[A-Z_]+|TUNNEL_[A-Z_]+|SOCKET_[A-Z_]+|INCOMPLETE_CHUNKED_ENCODING|CONTENT_LENGTH_MISMATCH)/;
const UUID_PATH = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const EDITOR_PATH = new RegExp(`^/app/resumes/${UUID_PATH}$`);
const OAUTH_AUTHORIZE_PATH = /^\/(?:__uat\/oauth\/[a-z]+\/authorize|api\/v1\/auth\/[a-z]+\/start)$/;
const OAUTH_CALLBACK_PATH = /^\/api\/v1\/auth\/[a-z]+\/callback$/;
const NUXT_CHUNK_PATH = /^\/_nuxt\/.+\.(?:m?js|css)$/;

export interface EditorFailureDiagnostic {
  markFailure(): void;
  markReauthSignIn(): void;
  trackReauthPage(page: Page): void;
  write(errors: readonly unknown[]): Promise<void>;
}

export interface EditorFailureDiagnosticOptions {
  readonly origin: string;
  readonly budgetMs?: number;
  readonly now?: () => number;
  readonly path?: string;
}

function safely<T, F>(read: () => T, fallback: F): T | F {
  try {
    return read();
  } catch {
    return fallback;
  }
}

function errorText(error: unknown): string {
  if (typeof error === 'string') return error.slice(0, 8_192);
  if (typeof error !== 'object' || error === null) return '';
  const parts = ['name', 'message', 'value'].map((key) => safely(
    () => (error as Record<string, unknown>)[key],
    undefined,
  ));
  return parts.filter((part) => typeof part === 'string').join(': ').slice(0, 8_192);
}

function classifyError(error: unknown): FailureCategory {
  const text = errorText(error);
  if (PAGE_CLOSED_TEXT.test(text)) return 'page-closed';
  if (ASSERTION_TEXT.test(text)) {
    return TIMEOUT_TEXT.test(text) ? 'assertion-timeout' : 'assertion';
  }
  if (NAVIGATION_TEXT.test(text)) return 'navigation';
  return 'other';
}

export function classifyFailure(errors: readonly unknown[]): FailureCategory {
  for (const error of errors) {
    const category = classifyError(error);
    if (category !== 'other') return category;
  }
  return 'other';
}

export function classifyURL(value: string, origin: string): URLCategory {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return UNAVAILABLE;
  }
  if (url.protocol === 'about:') return 'about-blank';
  if (url.origin !== origin) return 'other-origin';
  if (url.pathname === '/login') return 'login';
  if (EDITOR_PATH.test(url.pathname)) return 'editor';
  if (OAUTH_AUTHORIZE_PATH.test(url.pathname)) return 'oauth-authorize';
  if (OAUTH_CALLBACK_PATH.test(url.pathname)) return 'oauth-callback';
  return 'app-other';
}

export function classifyPageError(error: unknown): EventCategory {
  const text = errorText(error);
  if (CHUNK_TEXT.test(text)) return 'chunk-load';
  if (/hydrat/i.test(text)) return 'hydration';
  if (NETWORK_TEXT.test(text)) return 'network';
  return 'other';
}

export function classifyRequestFailure(
  failure: { readonly resourceType: string; readonly errorText: string; readonly url: string },
  origin: string,
): EventCategory {
  const path = safely(() => {
    const url = new URL(failure.url);
    return url.origin === origin ? url.pathname : '';
  }, '');
  if (failure.resourceType === 'script' || NUXT_CHUNK_PATH.test(path)) return 'chunk-load';
  if (NETWORK_FAILURE.test(failure.errorText)) return 'network';
  return 'other';
}

export function classifyStatus(status: number): StatusClass {
  if (!Number.isInteger(status)) return 'none';
  if (status >= 200 && status < 300) return '2xx';
  if (status >= 300 && status < 400) return '3xx';
  if (status >= 400 && status < 500) return '4xx';
  if (status >= 500 && status < 600) return '5xx';
  return 'none';
}

export function closedStage(stage: unknown): string {
  if (typeof stage !== 'string' || stage === '' || stage.length > MAX_STAGE_LENGTH) {
    return UNAVAILABLE;
  }
  let reduced = stage;
  for (const family of OPEN_STAGE_FAMILIES) {
    const match = family.exec(stage);
    if (match !== null) {
      reduced = CLOSED_STAGE_SUFFIX.test(match[2]!) ? stage : match[1]!;
      break;
    }
  }
  return reduced.split('-').every((word) => /^[0-9]{1,6}$/.test(word) || STAGE_WORDS.has(word))
    ? reduced
    : UNAVAILABLE;
}

function fields(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function field(value: unknown, key: string): unknown {
  const source = fields(value);
  return Object.hasOwn(source, key) ? source[key] : undefined;
}

function oneOf<T extends string, F>(value: unknown, allowed: readonly T[], fallback: F): T | F {
  return allowed.find((item) => item === value) ?? fallback;
}

function closedCount(value: unknown): number | typeof UNAVAILABLE {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
    ? value
    : UNAVAILABLE;
}

function closedVisible(value: unknown): boolean | typeof UNAVAILABLE {
  return value === true || value === false ? value : UNAVAILABLE;
}

function closedPage(value: unknown): Record<string, unknown> {
  return {
    urlCategory: oneOf(field(value, 'urlCategory'), URL_CATEGORIES, UNAVAILABLE),
    readyState: oneOf(field(value, 'readyState'), READY_STATES, UNAVAILABLE),
    signInHeadingCount: closedCount(field(value, 'signInHeadingCount')),
    signInHeadingVisible: closedVisible(field(value, 'signInHeadingVisible')),
  };
}

function closedCounts(value: unknown): Record<EventCategory, number | typeof UNAVAILABLE> {
  return {
    'chunk-load': closedCount(field(value, 'chunk-load')),
    hydration: closedCount(field(value, 'hydration')),
    network: closedCount(field(value, 'network')),
    other: closedCount(field(value, 'other')),
  };
}

export function serializeEditorDiagnostic(input: unknown): string {
  const reauth = field(input, 'reauthPage');
  const elapsed = field(input, 'elapsedMs');
  const diagnostic = {
    schemaVersion: 1,
    stage: closedStage(field(input, 'stage')),
    failureCategory: oneOf(field(input, 'failureCategory'), FAILURE_CATEGORIES, 'other'),
    elapsedMs: {
      sinceTestStart: closedCount(field(elapsed, 'sinceTestStart')),
      sinceStageChange: closedCount(field(elapsed, 'sinceStageChange')),
    },
    mainPage: closedPage(field(input, 'mainPage')),
    reauthPage: {
      ...closedPage(reauth),
      mainFrameNavigations: closedCount(field(reauth, 'mainFrameNavigations')),
      lastNavigationStatusClass: oneOf(
        field(reauth, 'lastNavigationStatusClass'),
        STATUS_CLASSES,
        UNAVAILABLE,
      ),
    },
    pageErrors: closedCounts(field(input, 'pageErrors')),
    requestFailures: closedCounts(field(input, 'requestFailures')),
  };
  const serialized = `${JSON.stringify(diagnostic, null, 2)}\n`;
  if (Buffer.byteLength(serialized) > MAX_BYTES) {
    throw new Error('editor diagnostic exceeds its size bound');
  }
  return serialized;
}

function trackStageChanges(state: { editorDiagnosticStage: string }, now: () => number): () => number {
  let stage = state.editorDiagnosticStage;
  let changedAt = now();
  Object.defineProperty(state, 'editorDiagnosticStage', {
    configurable: true,
    enumerable: true,
    get: (): string => stage,
    set: (next: string): void => {
      if (next !== stage) changedAt = now();
      stage = next;
    },
  });
  return () => changedAt;
}

function newEventCounts(): EventCounts {
  return { 'chunk-load': 0, hydration: 0, network: 0, other: 0 };
}

async function collectPage(
  target: Page | undefined,
  origin: string,
  deadline: Promise<typeof UNAVAILABLE>,
): Promise<Record<string, unknown>> {
  if (target === undefined || safely(() => target.isClosed(), true)) {
    return {
      urlCategory: UNAVAILABLE,
      readyState: UNAVAILABLE,
      signInHeadingCount: UNAVAILABLE,
      signInHeadingVisible: UNAVAILABLE,
    };
  }
  const probe = <T>(work: () => Promise<T>): Promise<T | typeof UNAVAILABLE> => Promise.race([
    Promise.resolve().then(work).catch(() => UNAVAILABLE),
    deadline,
  ]);
  const heading = (): ReturnType<Page['getByRole']> => target.getByRole('heading', { name: 'Sign in' });
  const [readyState, signInHeadingCount, signInHeadingVisible] = await Promise.all([
    probe(async () => oneOf(
      await target.evaluate(() => document.readyState),
      READY_STATES,
      UNAVAILABLE,
    )),
    probe(() => heading().count()),
    probe(() => heading().first().isVisible()),
  ]);
  return {
    urlCategory: safely(() => classifyURL(target.url(), origin), UNAVAILABLE),
    readyState,
    signInHeadingCount,
    signInHeadingVisible,
  };
}

export function installEditorFailureDiagnostic(
  state: { editorDiagnosticStage: string },
  page: Page,
  options: EditorFailureDiagnosticOptions,
): EditorFailureDiagnostic {
  const now = options.now ?? Date.now;
  const budgetMs = options.budgetMs ?? COLLECTION_BUDGET_MS;
  const path = options.path ?? EDITOR_DIAGNOSTIC_PATH;
  const startedAt = now();
  const stageChangedAt = trackStageChanges(state, now);
  const pageErrors = newEventCounts();
  const requestFailures = newEventCounts();
  let reauthPage: Page | undefined;
  let countNavigations = false;
  let navigations = 0;
  let lastStatus: StatusClass = 'none';
  let failure: { readonly at: number; readonly stageChangedAt: number } | undefined;
  let written = false;

  const countErrors = (target: Page): void => {
    target.on('pageerror', (error: Error) => {
      pageErrors[classifyPageError(error)] += 1;
    });
    target.on('requestfailed', (request: Request) => {
      requestFailures[safely(() => classifyRequestFailure({
        resourceType: request.resourceType(),
        errorText: request.failure()?.errorText ?? '',
        url: request.url(),
      }, options.origin), 'other')] += 1;
    });
  };
  countErrors(page);

  return {
    markFailure: (): void => {
      failure ??= { at: now(), stageChangedAt: stageChangedAt() };
    },
    markReauthSignIn: (): void => {
      countNavigations = true;
      navigations = 0;
      lastStatus = 'none';
    },
    trackReauthPage: (target: Page): void => {
      if (reauthPage !== undefined) return;
      reauthPage = target;
      countErrors(target);
      target.on('framenavigated', (frame) => {
        if (countNavigations && safely(() => frame === target.mainFrame(), false)) navigations += 1;
      });
      target.on('response', (response: Response) => {
        if (!countNavigations) return;
        const status = safely(() => {
          const request = response.request();
          return request.isNavigationRequest() && request.frame() === target.mainFrame()
            ? classifyStatus(response.status())
            : undefined;
        }, undefined);
        if (status !== undefined) lastStatus = status;
      });
    },
    write: async (errors: readonly unknown[]): Promise<void> => {
      try {
        if (written) return;
        written = true;
        const at = failure ?? { at: now(), stageChangedAt: stageChangedAt() };
        let timer: ReturnType<typeof setTimeout> | undefined;
        const deadline = new Promise<typeof UNAVAILABLE>((resolve) => {
          timer = setTimeout(() => resolve(UNAVAILABLE), budgetMs);
        });
        let pages: Record<string, unknown>[];
        try {
          pages = await Promise.all([
            collectPage(page, options.origin, deadline),
            collectPage(reauthPage, options.origin, deadline),
          ]);
        } finally {
          clearTimeout(timer);
        }
        const serialized = serializeEditorDiagnostic({
          stage: state.editorDiagnosticStage,
          failureCategory: classifyFailure(errors),
          elapsedMs: {
            sinceTestStart: Math.max(0, Math.round(at.at - startedAt)),
            sinceStageChange: Math.max(0, Math.round(at.at - at.stageChangedAt)),
          },
          mainPage: pages[0],
          reauthPage: {
            ...pages[1],
            mainFrameNavigations: navigations,
            lastNavigationStatusClass: lastStatus,
          },
          pageErrors,
          requestFailures,
        });
        await writeFile(path, serialized, { flag: 'wx', mode: 0o600 });
      } catch {
        return;
      }
    },
  };
}
