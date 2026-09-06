import { ref, watch, type ComputedRef, type Ref } from 'vue';

import type { useAuth } from '../composables/useAuth';
import type { ResumeRecord } from '../stores/resumes';

export const MAX_PDF_DOWNLOAD_BYTES = 16_777_216;

export type PdfDownloadState
  = | { readonly kind: 'idle' }
    | { readonly kind: 'pending' }
    | {
      readonly kind: 'error';
      readonly message:
        | 'Save changes before downloading PDF.'
        | 'Your session ended. Sign in again.'
        | 'PDF download failed. Try again.'
        | 'PDF is temporarily unavailable. Try again.';
    };

export interface PdfDownloadController {
  readonly state: Readonly<Ref<PdfDownloadState>>;
  download(): Promise<PdfDownloadState>;
  dispose(): void;
}

export interface PdfDownloadControllerDeps {
  readonly resumeId: string;
  readonly record: ComputedRef<ResumeRecord | undefined>;
  readonly flush: () => Promise<void>;
  readonly auth: Pick<ReturnType<typeof useAuth>, 'authState' | 'user'>;
  readonly fetcher?: typeof fetch;
  readonly createObjectURL?: (blob: Blob) => string;
  readonly revokeObjectURL?: (url: string) => void;
  readonly download?: (url: string, filename: string) => void;
}

export function createPdfDownloadController(
  deps: PdfDownloadControllerDeps,
): PdfDownloadController {
  const state = ref<PdfDownloadState>({ kind: 'idle' });
  const fetcher = deps.fetcher ?? fetch;
  const createObjectURL = deps.createObjectURL ?? URL.createObjectURL;
  const revokeObjectURL = deps.revokeObjectURL ?? URL.revokeObjectURL;
  const triggerDownload = deps.download ?? browserDownload;
  let active: AbortController | null = null;
  let generation = 0;
  let ownerId: string | null = null;

  const stopWatching = watch(
    () => [deps.auth.authState.value, deps.auth.user.value?.id] as const,
    () => {
      if (active !== null && (ownerId === null || !sameOwner(ownerId, deps))) {
        cancel();
      }
    },
  );

  async function download(): Promise<PdfDownloadState> {
    if (active !== null) return state.value;
    const beforeFlush = preFlushRecord(deps.resumeId, deps.record.value, deps);
    if (beforeFlush.kind === 'session') {
      return setState({
        kind: 'error',
        message: 'Your session ended. Sign in again.',
      });
    }
    if (beforeFlush.kind !== 'ready') return blocked();

    const runGeneration = generation;
    ownerId = beforeFlush.ownerId;
    active = new AbortController();
    setState({ kind: 'pending' });
    try {
      await deps.flush();
      if (!activeRun(runGeneration)) return state.value;
      const afterFlush = acceptedRecord(deps.resumeId, deps.record.value, deps);
      if (afterFlush.kind === 'session') {
        return setState({
          kind: 'error',
          message: 'Your session ended. Sign in again.',
        });
      }
      if (afterFlush.kind !== 'ready') return blocked();

      const response = await fetcher(`/api/v1/resumes/${deps.resumeId}/pdf`, {
        credentials: 'same-origin',
        method: 'GET',
        signal: active.signal,
      });
      if (!activeRun(runGeneration)) return state.value;
      if (!validResponse(response)) {
        await discardResponse(response);
        return responseError(response.status);
      }
      const blob = await readPDF(response, active.signal);
      if (!activeRun(runGeneration)) return state.value;
      const afterRead = acceptedRecord(deps.resumeId, deps.record.value, deps);
      if (afterRead.kind === 'session') {
        return setState({
          kind: 'error',
          message: 'Your session ended. Sign in again.',
        });
      }
      if (afterRead.kind !== 'ready') return blocked();

      const url = createObjectURL(blob);
      try {
        if (!activeRun(runGeneration)) return state.value;
        triggerDownload(url, 'resume.pdf');
      } finally {
        revokeObjectURL(url);
      }
      return setState({ kind: 'idle' });
    } catch (error) {
      if (!activeRun(runGeneration) || isAbort(error)) return state.value;
      return setState({
        kind: 'error',
        message: 'PDF download failed. Try again.',
      });
    } finally {
      if (runGeneration === generation) {
        active?.abort();
        active = null;
        ownerId = null;
      }
    }
  }

  function blocked(): PdfDownloadState {
    return setState({
      kind: 'error',
      message: 'Save changes before downloading PDF.',
    });
  }

  function responseError(status: number): PdfDownloadState {
    return setState({
      kind: 'error',
      message:
        status === 429 || status === 503
          ? 'PDF is temporarily unavailable. Try again.'
          : 'PDF download failed. Try again.',
    });
  }

  function activeRun(runGeneration: number): boolean {
    return (
      active !== null
      && runGeneration === generation
      && ownerId !== null
      && sameOwner(ownerId, deps)
    );
  }

  function cancel(): void {
    generation += 1;
    active?.abort();
    active = null;
    ownerId = null;
    if (state.value.kind === 'pending') setState({ kind: 'idle' });
  }

  function dispose(): void {
    cancel();
    stopWatching();
  }

  function setState(next: PdfDownloadState): PdfDownloadState {
    state.value = next;
    return next;
  }

  return { state, download, dispose };
}

function preFlushRecord(
  resumeId: string,
  record: ResumeRecord | undefined,
  deps: Pick<PdfDownloadControllerDeps, 'auth'>,
):
  | { readonly kind: 'ready'; readonly ownerId: string }
  | { readonly kind: 'blocked' }
  | { readonly kind: 'session' } {
  const ownerId = deps.auth.user.value?.id;
  if (
    deps.auth.authState.value !== 'authenticated'
    || ownerId === undefined
    || record?.sessionLost
  ) {
    return { kind: 'session' };
  }
  if (
    record === undefined
    || record.accepted.metadata.id !== resumeId
    || !canonicalUUID(resumeId)
    || record.conflicts.length > 0
    || (record.attempt !== null && record.attempt.kind !== 'dispatching')
    || record.templateState?.kind === 'partial'
    || record.opaquePhotoOutcome !== null
    || record.completeReadRequired
    || Object.keys(record.issues).length > 0
  ) {
    return { kind: 'blocked' };
  }
  return { kind: 'ready', ownerId };
}

function acceptedRecord(
  resumeId: string,
  record: ResumeRecord | undefined,
  deps: Pick<PdfDownloadControllerDeps, 'auth'>,
):
  | { readonly kind: 'ready'; readonly ownerId: string }
  | { readonly kind: 'blocked' }
  | { readonly kind: 'session' } {
  if (record === undefined) return { kind: 'blocked' };
  const result = preFlushRecord(resumeId, record, deps);
  if (result.kind !== 'ready') return result;
  return record.pending.length > 0 || record.attempt !== null
    ? { kind: 'blocked' }
    : result;
}

function validResponse(response: Response): boolean {
  const contentLength = response.headers.get('Content-Length');
  return (
    response.status === 200
    && response.headers.get('Content-Type') === 'application/pdf'
    && response.headers.get('Cache-Control') === 'no-store, no-transform'
    && response.body !== null
    && (contentLength === null
      || (/^(?:0|[1-9][0-9]*)$/.test(contentLength)
        && Number(contentLength) <= MAX_PDF_DOWNLOAD_BYTES))
  );
}

async function readPDF(response: Response, signal: AbortSignal): Promise<Blob> {
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
      if (done) break;
      size += value.byteLength;
      if (size > MAX_PDF_DOWNLOAD_BYTES) {
        await reader.cancel().catch(() => undefined);
        throw new Error('PDF response exceeds bound');
      }
      const copy = new Uint8Array(value.byteLength);
      copy.set(value);
      chunks.push(copy.buffer);
    }
  } finally {
    signal.removeEventListener('abort', cancelReader);
    reader.releaseLock();
  }
  if (size === 0) throw new Error('PDF response is empty');
  return new Blob(chunks, { type: 'application/pdf' });
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

function canonicalUUID(value: string): boolean {
  const pattern
    = new RegExp(
      '^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}'
      + '-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
    );
  return pattern.test(value);
}

function sameOwner(
  ownerId: string,
  deps: Pick<PdfDownloadControllerDeps, 'auth'>,
): boolean {
  return (
    deps.auth.authState.value === 'authenticated'
    && deps.auth.user.value?.id === ownerId
  );
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}
