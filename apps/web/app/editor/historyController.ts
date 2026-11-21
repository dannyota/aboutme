import { ref, type Ref } from 'vue';

import type { useAuth } from '../composables/useAuth';
import type { useResumeStore } from '../stores/resumes';
import {
  createHistoryApi,
  freezeHistoryWrite,
  type CheckpointSummary,
  type CheckpointVersion,
  type HistoryApi,
  type HistoryWrite,
  type HistoryWriteResult,
} from './historyApi';
import type { EditorRuntime } from './types';

export type HistoryListState
  = | { readonly kind: 'loading' }
    | { readonly kind: 'failed' }
    | { readonly kind: 'ready'; readonly items: readonly CheckpointSummary[] };

export type HistoryPreviewState
  = | { readonly kind: 'idle' }
    | { readonly kind: 'loading' }
    | { readonly kind: 'failed' }
    | { readonly kind: 'gone' }
    | { readonly kind: 'ready'; readonly version: CheckpointVersion };

export type RestorePhase
  = | 'confirm'
    | 'busy'
    | 'changed'
    | 'invalid'
    | 'gone'
    | 'network'
    | 'later'
    | 'retry'
    | 'failed';

export interface RestoreDialog {
  readonly id: string;
  readonly phase: RestorePhase;
  readonly version: CheckpointVersion | null;
}

export interface ConfirmDialog {
  readonly id: string | null;
  readonly phase: 'confirm' | 'busy' | 'failed';
}

export interface HistoryBanner {
  readonly id: string;
  readonly startedAt: string;
  readonly undoId: string | null;
}

export type HistoryStatus
  = | { readonly kind: 'showing'; readonly startedAt: string }
    | { readonly kind: 'deleted'; readonly startedAt: string }
    | { readonly kind: 'cleared' };

export type HistoryFocusRequest
  = | { readonly kind: 'option'; readonly id: string }
    | { readonly kind: 'current' }
    | { readonly kind: 'empty' };

export interface HistoryControllerDeps {
  readonly resumeId: string;
  readonly store: Pick<
    ReturnType<typeof useResumeStore>,
    | 'recordFor'
    | 'adoptComplete'
    | 'adoptStaleWinner'
    | 'setTemplateState'
    | 'markSessionLost'
  >;
  readonly auth: Pick<
    ReturnType<typeof useAuth>,
    'user' | 'csrfToken' | 'authState' | 'refresh'
  >;
  readonly runtime: Pick<EditorRuntime, 'uuid' | 'nowEpochMs' | 'delay'>;
  readonly api?: HistoryApi;
  readonly refresh: () => Promise<unknown>;
}

export interface HistoryController {
  readonly list: Ref<HistoryListState>;
  readonly selected: Ref<string>;
  readonly preview: Ref<HistoryPreviewState>;
  readonly restore: Ref<RestoreDialog | null>;
  readonly remove: Ref<ConfirmDialog | null>;
  readonly clear: Ref<ConfirmDialog | null>;
  readonly banner: Ref<HistoryBanner | null>;
  readonly status: Ref<HistoryStatus | null>;
  readonly focusRequest: Ref<HistoryFocusRequest | null>;
  open(): void;
  close(): void;
  reload(): Promise<void>;
  select(id: string): Promise<void>;
  retryPreview(): Promise<void>;
  openRestore(id: string): void;
  confirmRestore(): Promise<void>;
  closeRestore(): void;
  openDelete(): void;
  confirmDelete(): Promise<void>;
  closeDelete(): void;
  openClear(): void;
  confirmClear(): Promise<void>;
  closeClear(): void;
  dismissBanner(): void;
  ackFocus(): void;
}

export const CURRENT_VERSION_ID = 'current';

export function createHistoryController(
  deps: HistoryControllerDeps,
): HistoryController {
  const api = deps.api ?? createHistoryApi();
  const list = ref<HistoryListState>({ kind: 'loading' });
  const selected = ref<string>(CURRENT_VERSION_ID);
  const preview = ref<HistoryPreviewState>({ kind: 'idle' });
  const restore = ref<RestoreDialog | null>(null);
  const remove = ref<ConfirmDialog | null>(null);
  const clear = ref<ConfirmDialog | null>(null);
  const banner = ref<HistoryBanner | null>(null);
  const status = ref<HistoryStatus | null>(null);
  const focusRequest = ref<HistoryFocusRequest | null>(null);

  let reading = false;
  let queued = false;
  let epoch = 0;
  let previewToken = 0;
  let restoreWrite: HistoryWrite | null = null;
  let deleteWrite: HistoryWrite | null = null;
  let clearWrite: HistoryWrite | null = null;

  const items = (): readonly CheckpointSummary[] =>
    list.value.kind === 'ready' ? list.value.items : [];

  function reset(): void {
    epoch += 1;
    previewToken += 1;
    list.value = { kind: 'loading' };
    selected.value = CURRENT_VERSION_ID;
    preview.value = { kind: 'idle' };
    restore.value = null;
    remove.value = null;
    clear.value = null;
    banner.value = null;
    status.value = null;
    focusRequest.value = null;
    restoreWrite = null;
    deleteWrite = null;
    clearWrite = null;
  }

  async function readOnce(): Promise<void> {
    const mine = epoch;
    const result = await api.list(deps.resumeId);
    if (mine !== epoch) return;
    if (result.kind === 'ok') {
      list.value = { kind: 'ready', items: result.value };
      if (
        selected.value !== CURRENT_VERSION_ID
        && !result.value.some((entry) => entry.id === selected.value)
      ) {
        selected.value = CURRENT_VERSION_ID;
        if (preview.value.kind !== 'gone') preview.value = { kind: 'idle' };
      }
      return;
    }
    if (result.kind === 'session-lost') {
      deps.store.markSessionLost(deps.resumeId);
      return;
    }
    list.value = { kind: 'failed' };
  }

  async function reload(): Promise<void> {
    if (reading) {
      queued = true;
      return;
    }
    reading = true;
    try {
      do {
        queued = false;
        await readOnce();
      } while (queued);
    } finally {
      reading = false;
    }
  }

  function open(): void {
    reset();
    void reload();
  }

  function close(): void {
    reset();
  }

  async function select(id: string): Promise<void> {
    previewToken += 1;
    const token = previewToken;
    banner.value = null;
    if (id === CURRENT_VERSION_ID) {
      selected.value = CURRENT_VERSION_ID;
      preview.value = { kind: 'idle' };
      status.value = null;
      return;
    }
    selected.value = id;
    preview.value = { kind: 'loading' };
    const result = await api.read(deps.resumeId, id);
    if (token !== previewToken) return;
    if (result.kind === 'ok') {
      preview.value = { kind: 'ready', version: result.value };
      status.value = { kind: 'showing', startedAt: result.value.startedAt };
      return;
    }
    if (result.kind === 'not-found') {
      preview.value = { kind: 'gone' };
      await reload();
      if (token === previewToken) selected.value = CURRENT_VERSION_ID;
      return;
    }
    if (result.kind === 'session-lost') {
      deps.store.markSessionLost(deps.resumeId);
    }
    preview.value = { kind: 'failed' };
  }

  async function retryPreview(): Promise<void> {
    if (selected.value !== CURRENT_VERSION_ID) await select(selected.value);
  }

  async function dispatch(write: HistoryWrite): Promise<HistoryWriteResult> {
    let refreshed = false;
    for (;;) {
      const csrf = deps.auth.csrfToken.value;
      if (
        csrf === null
        || deps.auth.authState.value !== 'authenticated'
        || deps.auth.user.value?.id !== write.ownerId
      ) {
        return { kind: 'session-lost' };
      }
      const result = await api.write(write, csrf);
      if (result.kind !== 'csrf-rejected') return result;
      if (refreshed) return { kind: 'failed' };
      refreshed = true;
      try {
        await deps.auth.refresh();
      } catch {
        return { kind: 'failed' };
      }
    }
  }

  function owner(): string | undefined {
    return deps.auth.user.value?.id;
  }

  function openRestore(id: string): void {
    restoreWrite = null;
    const shown = preview.value;
    const version
      = shown.kind === 'ready' && shown.version.id === id
        ? shown.version
        : null;
    restore.value = { id, phase: 'confirm', version };
    if (version === null) {
      void api.read(deps.resumeId, id).then((result) => {
        if (result.kind === 'ok' && restore.value?.id === id) {
          restore.value = { ...restore.value, version: result.value };
        }
      });
    }
  }

  function setPhase(phase: RestorePhase): void {
    if (restore.value !== null) {
      restore.value = { ...restore.value, phase };
    }
  }

  async function confirmRestore(): Promise<void> {
    const dialog = restore.value;
    if (
      dialog === null
      || !['confirm', 'changed', 'network', 'retry', 'failed'].includes(
        dialog.phase,
      )
    ) {
      return;
    }
    const record = deps.store.recordFor(deps.resumeId);
    const ownerId = owner();
    if (record === undefined || ownerId === undefined) return;
    if (restoreWrite === null || restoreWrite.checkpointId !== dialog.id) {
      restoreWrite = freezeHistoryWrite(
        {
          operation: 'restore',
          resumeId: deps.resumeId,
          checkpointId: dialog.id,
          revision: record.accepted.revision,
          ownerId,
        },
        deps.runtime,
      );
    }
    setPhase('busy');
    const result = await dispatch(restoreWrite);
    if (restore.value?.id !== dialog.id) return;
    switch (result.kind) {
      case 'accepted': {
        deps.store.adoptComplete(deps.resumeId, result.resume);
        deps.store.setTemplateState(deps.resumeId, null);
        const restored = items().find((entry) => entry.id === dialog.id);
        restoreWrite = null;
        restore.value = null;
        selected.value = CURRENT_VERSION_ID;
        preview.value = { kind: 'idle' };
        status.value = null;
        banner.value = {
          id: dialog.id,
          startedAt: restored?.startedAt ?? dialog.version?.startedAt ?? '',
          undoId: null,
        };
        await reload();
        if (banner.value?.id === dialog.id) {
          banner.value = { ...banner.value, undoId: items()[0]?.id ?? null };
        }
        return;
      }
      case 'stale':
        deps.store.adoptStaleWinner(deps.resumeId, {
          ...result.winner,
          metadata: record.accepted.metadata,
          metadataFreshness: 'stale',
        });
        restoreWrite = null;
        await deps.refresh().catch(() => undefined);
        setPhase('changed');
        return;
      case 'invalid':
        restoreWrite = null;
        setPhase('invalid');
        return;
      case 'not-found':
        restoreWrite = null;
        setPhase('gone');
        return;
      case 'network':
        setPhase('network');
        return;
      case 'busy':
        if (result.retryAfterMs === null) {
          setPhase('retry');
          return;
        }
        setPhase('later');
        void deps.runtime.delay(result.retryAfterMs).then(() => {
          if (
            restore.value?.id === dialog.id
            && restore.value.phase === 'later'
          ) {
            setPhase('retry');
          }
        });
        return;
      case 'session-lost':
        restoreWrite = null;
        restore.value = null;
        deps.store.markSessionLost(deps.resumeId);
        return;
      default:
        restoreWrite = null;
        setPhase('failed');
    }
  }

  function closeRestore(): void {
    const dialog = restore.value;
    if (dialog === null || dialog.phase === 'busy') return;
    restoreWrite = null;
    restore.value = null;
    if (dialog.phase === 'gone') void reload();
  }

  function openDelete(): void {
    if (selected.value === CURRENT_VERSION_ID) return;
    deleteWrite = null;
    banner.value = null;
    remove.value = { id: selected.value, phase: 'confirm' };
  }

  async function confirmDelete(): Promise<void> {
    const dialog = remove.value;
    const ownerId = owner();
    if (dialog === null || dialog.id === null) return;
    if (dialog.phase === 'busy' || ownerId === undefined) return;
    if (deleteWrite === null || deleteWrite.checkpointId !== dialog.id) {
      deleteWrite = freezeHistoryWrite(
        {
          operation: 'delete',
          resumeId: deps.resumeId,
          checkpointId: dialog.id,
          ownerId,
        },
        deps.runtime,
      );
    }
    remove.value = { ...dialog, phase: 'busy' };
    const result = await dispatch(deleteWrite);
    if (remove.value?.id !== dialog.id) return;
    if (result.kind === 'session-lost') {
      deleteWrite = null;
      remove.value = null;
      deps.store.markSessionLost(deps.resumeId);
      return;
    }
    if (result.kind !== 'done' && result.kind !== 'not-found') {
      remove.value = { ...dialog, phase: 'failed' };
      return;
    }
    const before = items();
    const index = before.findIndex((entry) => entry.id === dialog.id);
    const removed = before[index];
    const next = before[index + 1] ?? before[index - 1];
    deleteWrite = null;
    remove.value = null;
    previewToken += 1;
    selected.value = CURRENT_VERSION_ID;
    preview.value = { kind: 'idle' };
    status.value = { kind: 'deleted', startedAt: removed?.startedAt ?? '' };
    await reload();
    focusRequest.value = items().length === 0
      ? { kind: 'empty' }
      : next !== undefined && items().some((entry) => entry.id === next.id)
        ? { kind: 'option', id: next.id }
        : { kind: 'current' };
  }

  function closeDelete(): void {
    if (remove.value?.phase === 'busy') return;
    deleteWrite = null;
    remove.value = null;
  }

  function openClear(): void {
    clearWrite = null;
    banner.value = null;
    clear.value = { id: null, phase: 'confirm' };
  }

  async function confirmClear(): Promise<void> {
    const dialog = clear.value;
    const ownerId = owner();
    if (dialog === null || dialog.phase === 'busy' || ownerId === undefined) {
      return;
    }
    clearWrite ??= freezeHistoryWrite(
      { operation: 'clear', resumeId: deps.resumeId, ownerId },
      deps.runtime,
    );
    clear.value = { ...dialog, phase: 'busy' };
    const result = await dispatch(clearWrite);
    if (clear.value === null) return;
    if (result.kind === 'session-lost') {
      clearWrite = null;
      clear.value = null;
      deps.store.markSessionLost(deps.resumeId);
      return;
    }
    if (result.kind !== 'done' && result.kind !== 'not-found') {
      clear.value = { ...dialog, phase: 'failed' };
      return;
    }
    clearWrite = null;
    clear.value = null;
    previewToken += 1;
    selected.value = CURRENT_VERSION_ID;
    preview.value = { kind: 'idle' };
    status.value = { kind: 'cleared' };
    await reload();
    focusRequest.value = items().length === 0
      ? { kind: 'empty' }
      : { kind: 'current' };
  }

  function closeClear(): void {
    if (clear.value?.phase === 'busy') return;
    clearWrite = null;
    clear.value = null;
  }

  return {
    list,
    selected,
    preview,
    restore,
    remove,
    clear,
    banner,
    status,
    focusRequest,
    open,
    close,
    reload,
    select,
    retryPreview,
    openRestore,
    confirmRestore,
    closeRestore,
    openDelete,
    confirmDelete,
    closeDelete,
    openClear,
    confirmClear,
    closeClear,
    dismissBanner: () => {
      banner.value = null;
    },
    ackFocus: () => {
      focusRequest.value = null;
    },
  };
}
