import { ref, type InjectionKey, type Ref } from 'vue';

import type { AuthProvider } from './useAuth';

export const MAX_ACCOUNT_EXPORT_BYTES = 12_582_912;

export type AccountExportState
  = | { readonly kind: 'idle' }
    | { readonly kind: 'pending' }
    | { readonly kind: 'error'; readonly message: string };

export interface AccountExportController {
  readonly state: Readonly<Ref<AccountExportState>>;
  download(): Promise<AccountExportState>;
  dispose(): void;
}

export interface AccountExportControllerDeps {
  readonly fetcher?: typeof fetch;
  readonly createObjectURL?: (blob: Blob) => string;
  readonly revokeObjectURL?: (url: string) => void;
  readonly download?: (url: string, filename: string) => void;
}

export type PrivacySettingsErrorKind
  = | 'reauth-required'
    | 'reauth-failed'
    | 'session-required'
    | 'account-changed'
    | 'rate-limited'
    | 'unavailable';

export class PrivacySettingsFailure extends Error {
  readonly kind: PrivacySettingsErrorKind;

  constructor(kind: PrivacySettingsErrorKind) {
    super(`privacy settings failed: ${kind}`);
    this.name = 'PrivacySettingsFailure';
    this.kind = kind;
  }
}

export interface PrivacySettingsActions {
  exportAccount(): Promise<AccountExportState>;
  deleteAccount(): Promise<void>;
  reauthenticate(password: string): Promise<void>;
  startProviderReauth(provider: AuthProvider): Promise<void>;
}

export const PrivacySettingsActionsKey: InjectionKey<PrivacySettingsActions>
  = Symbol('aboutme-privacy-settings-actions');

/** Maps the closed account-deletion HTTP error vocabulary to UI failures. */
export function mapAccountDeletionError(
  error: unknown,
): PrivacySettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new PrivacySettingsFailure('reauth-required');
  }
  if (status === 401 && code === 'session_required') {
    return new PrivacySettingsFailure('session-required');
  }
  if (status === 409 && code === 'account_changed') {
    return new PrivacySettingsFailure('account-changed');
  }
  if (status === 429 && code === 'rate_limited') {
    return new PrivacySettingsFailure('rate-limited');
  }
  return new PrivacySettingsFailure('unavailable');
}

export function createAccountExportController(
  deps: AccountExportControllerDeps = {},
): AccountExportController {
  const state = ref<AccountExportState>({ kind: 'idle' });
  const fetcher = deps.fetcher ?? fetch;
  const createObjectURL = deps.createObjectURL ?? URL.createObjectURL;
  const revokeObjectURL = deps.revokeObjectURL ?? URL.revokeObjectURL;
  const triggerDownload = deps.download ?? browserDownload;
  let active: AbortController | null = null;

  async function download(): Promise<AccountExportState> {
    if (active !== null) return state.value;
    const request = new AbortController();
    active = request;
    state.value = { kind: 'pending' };
    try {
      const response = await fetcher('/api/v1/me/export', {
        credentials: 'same-origin',
        method: 'GET',
        signal: request.signal,
      });
      if (!isCurrent(request)) {
        await discardResponse(response);
        return state.value;
      }
      if (!validExportResponse(response)) {
        await discardResponse(response);
        if (!isCurrent(request)) return state.value;
        return fail(response.status);
      }
      const blob = await readExport(response, request.signal);
      if (!isCurrent(request)) return state.value;
      const url = createObjectURL(blob);
      try {
        if (!isCurrent(request)) return state.value;
        triggerDownload(url, 'aboutme-export.json');
      } finally {
        revokeObjectURL(url);
      }
      state.value = { kind: 'idle' };
      return state.value;
    } catch (error) {
      if (!isCurrent(request) || isAbort(error)) return state.value;
      return fail();
    } finally {
      if (active === request) {
        request.abort();
        active = null;
      }
    }
  }

  function fail(status?: number): AccountExportState {
    state.value = {
      kind: 'error',
      message:
        status === 401
          ? 'Your session ended. Sign in again.'
          : status === 429 || status === 503
            ? 'Your export is temporarily unavailable. Try again.'
            : 'Could not export your data. Try again.',
    };
    return state.value;
  }

  function dispose(): void {
    const request = active;
    active = null;
    request?.abort();
    if (state.value.kind === 'pending') state.value = { kind: 'idle' };
  }

  return { state, download, dispose };

  function isCurrent(request: AbortController): boolean {
    return active === request && !request.signal.aborted;
  }
}

function failureStatus(error: unknown): number | null {
  const candidate = error as { statusCode?: unknown; status?: unknown } | null;
  for (const key of ['statusCode', 'status'] as const) {
    const value = candidate?.[key];
    if (typeof value === 'number') return value;
  }
  return null;
}

function failureCode(error: unknown): unknown {
  return (error as { data?: { error?: { code?: unknown } } } | null)?.data
    ?.error?.code;
}

function validExportResponse(response: Response): boolean {
  const contentLength = response.headers.get('Content-Length');
  return (
    response.status === 200
    && /^application\/json(?:\s*;|$)/i.test(
      response.headers.get('Content-Type') ?? '',
    )
    && response.headers.get('Cache-Control') === 'no-store, no-transform'
    && response.body !== null
    && (contentLength === null
      || (/^(?:0|[1-9][0-9]*)$/.test(contentLength)
        && Number(contentLength) <= MAX_ACCOUNT_EXPORT_BYTES))
  );
}

async function readExport(
  response: Response,
  signal: AbortSignal,
): Promise<Blob> {
  const reader = response.body!.getReader();
  const chunks: ArrayBuffer[] = [];
  let size = 0;
  const cancelReader = () => {
    void reader.cancel().catch(() => undefined);
  };
  signal.addEventListener('abort', cancelReader, { once: true });
  try {
    for (;;) {
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
      const { done, value } = await reader.read();
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
      if (done) break;
      size += value.byteLength;
      if (size > MAX_ACCOUNT_EXPORT_BYTES) {
        await reader.cancel().catch(() => undefined);
        throw new Error('account export exceeds bound');
      }
      const copy = new Uint8Array(value.byteLength);
      copy.set(value);
      chunks.push(copy.buffer);
    }
  } finally {
    signal.removeEventListener('abort', cancelReader);
    reader.releaseLock();
  }
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
  if (size === 0) throw new Error('account export is empty');
  return new Blob(chunks, { type: 'application/json' });
}

async function discardResponse(response: Response): Promise<void> {
  await response.body?.cancel().catch(() => undefined);
}

function browserDownload(url: string, filename: string): void {
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}
