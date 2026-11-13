import {
  computed, getCurrentScope, onScopeDispose, ref, watch,
  type ComputedRef, type Ref,
} from 'vue';
import type { Resume } from '@aboutme/schema';

import type { CreateResumeIntent } from '../editor/commands';
import {
  createMutationCoordinator,
  type CreateResumeResult,
  type OpaqueCreateOutcome,
  type ResumeMutationCoordinator,
} from '../editor/coordinator';
import {
  createResumeApi,
  type ResumeApi,
  type ResumeSummary,
} from '../editor/resumeApi';
import type { EditorRuntime } from '../editor/types';
import { useResumeStore, type ResumeRecord } from '../stores/resumes';
import {
  browserEditorRuntime,
  createResumeEditorActions,
  type ResumeEditorActions,
} from './useResumeEditor';
import { useAuth, type AuthState } from './useAuth';
import { hasUnsafeWork } from './useUnsavedNavigationGuard';

type ResumeListView
  = | { readonly kind: 'waiting-auth' }
    | { readonly kind: 'loading' }
    | { readonly kind: 'ready'; readonly items: readonly ResumeSummary[] }
    | { readonly kind: 'unavailable' };

export type CreateNotice
  = | 'resume-cap'
    | 'create-failed'
    | 'retry-later'
    | 'session-lost'
    | null;

type ResumeListNotice = 'resume-changed' | null;

/**
 * What a card shows while the list's own rename or delete of it is not
 * settled: being saved, or a rename the queue holds after a failure.
 */
export type ResumeCardSaveState = 'saving' | 'rename-held';

interface CreateOpenOptions {
  readonly isCurrent?: () => boolean;
  readonly beforeOpen?: (resumeId: string) => void;
}

export interface ResumeListController {
  readonly view: Ref<ResumeListView>;
  readonly items: ComputedRef<readonly ResumeSummary[]>;
  readonly actionNotice: Ref<ResumeListNotice>;
  readonly removalFocusId: Ref<string | null>;
  readonly removalFocusVersion: Ref<number>;
  readonly saveStates: ComputedRef<
    Readonly<Record<string, ResumeCardSaveState>>
  >;
  settled(): Promise<void>;
  /** A rename or delete this list made has not settled yet. */
  unsettled(): boolean;
  /** Sends a held rename or delete again through the queue. */
  retry(id: string): Promise<void>;
  create(
    title: string,
    lng: string | null | undefined,
    document?: Resume,
    options?: CreateOpenOptions,
  ): Promise<CreateResumeResult>;
  refreshCreate(intentId: string): Promise<OpaqueCreateOutcome>;
  abandonCreate(intentId: string): void;
  rename(id: string, title: string): Promise<void>;
  remove(id: string, confirmedTitle: string): Promise<void>;
}

export interface ResumeListDeps {
  api?: ResumeApi;
  authState?: Ref<AuthState>;
  ownerId?: Ref<string | null>;
  runtime?: EditorRuntime;
  coordinator?: ResumeMutationCoordinator;
  store?: ReturnType<typeof useResumeStore>;
  actionsFor?: (resumeId: string) => ResumeEditorActions;
  refreshAuth?: () => Promise<void>;
}

/** Resumes an account may hold; the fourth create is refused (409). */
export const RESUME_CAP = 3;

export function createNotice(result: CreateResumeResult): CreateNotice {
  if (result.kind === 'rejected' && result.code === 'resume_cap_exceeded') {
    return 'resume-cap';
  }
  if (result.kind === 'rejected') {
    return 'create-failed';
  }
  if (result.kind === 'retry-later') return 'retry-later';
  if (result.kind === 'blocked' && result.reason === 'session-lost') {
    return 'session-lost';
  }
  return null;
}

/** Browser-owned list and lifecycle boundary. Network and command capture stay
 * in the existing API/coordinator/action layers. */
export function useResumeList(deps: ResumeListDeps = {}): ResumeListController {
  const auth = useAuth();
  const api = deps.api ?? createResumeApi();
  const store = deps.store ?? useResumeStore();
  const runtime = deps.runtime ?? browserEditorRuntime;
  const authState = deps.authState ?? auth.authState;
  const ownerId = deps.ownerId ?? computed(() => auth.user.value?.id ?? null);
  const coordinator = deps.coordinator ?? createMutationCoordinator({
    api,
    store,
    auth,
    runtime,
  });
  const actionsFor = deps.actionsFor ?? ((resumeId: string) =>
    createResumeEditorActions({ resumeId, store, coordinator, auth, runtime }));
  const refreshAuth = deps.refreshAuth
    ?? (deps.authState === undefined ? auth.refresh : async () => {});
  const view = ref<ResumeListView>({ kind: 'waiting-auth' });
  const actionNotice = ref<ResumeListNotice>(null);
  const removalFocusId = ref<string | null>(null);
  const removalFocusVersion = ref(0);
  const retainedCreate = ref<OpaqueCreateOutcome | null>(null);
  const deleting = new Set<string>();
  let creating: {
    intent: CreateResumeIntent;
    options: CreateOpenOptions | undefined;
    promise: Promise<CreateResumeResult>;
  } | null = null;
  let settling: Promise<void> = Promise.resolve();
  let wasAuthenticated = false;
  const disposed = ref(false);
  /** Resumes this list renamed or deleted, or is about to. */
  const touched = ref(new Set<string>());
  /** Renames and deletes still reading the resume before they are queued. */
  const starting = ref(0);
  /** The commands this list queued; leaving drops these and no others. */
  const ownCommands = new Set<string>();
  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      disposed.value = true;
      // Leaving the list ends its queue's reads and writes and drops the
      // renames and deletes it had not saved, as the leave warning said and
      // as closing the editor does; the resume opens afresh. A record an
      // editor has open keeps everything, the editor's work included.
      for (const id of touched.value) {
        coordinator.stop(id);
        // An open editor takes the record over, with what this list held.
        if (store.openInEditor(id)) store.handOver(id);
        else store.dropCommands(id, ownCommands);
      }
    });
  }

  const waitForAuth = async (): Promise<void> => {
    if (
      disposed.value
      || (authState.value !== 'loading' && authState.value !== 'error')
    ) return;
    await new Promise<void>((resolve) => {
      const stop = watch([authState, disposed], ([state, isDisposed]) => {
        if (!isDisposed && (state === 'loading' || state === 'error')) return;
        stop();
        resolve();
      }, { flush: 'sync' });
    });
  };

  const items = computed<readonly ResumeSummary[]>(() =>
    view.value.kind === 'ready' ? view.value.items : [],
  );

  const saveStates = computed(() => {
    const states: Record<string, ResumeCardSaveState> = {};
    for (const id of touched.value) {
      const record = store.recordFor(id);
      if (!hasUnsafeWork(record)) continue;
      const held = record!.attempt;
      const rename = held?.command.kind === 'metadataField'
        && held.command.field === 'title';
      states[id] = rename && held.kind !== 'dispatching' && !held.sending
        ? 'rename-held'
        : 'saving';
    }
    return states;
  });

  watch(
    () => [...store.records.keys()],
    () => {
      if (view.value.kind !== 'ready') return;
      const removed = new Set(
        [...deleting].filter((id) => store.recordFor(id) === undefined),
      );
      if (removed.size === 0) return;
      for (const id of removed) {
        const index = view.value.items.findIndex((item) => item.id === id);
        removalFocusId.value = view.value.items[index + 1]?.id ?? null;
        removalFocusVersion.value++;
        deleting.delete(id);
      }
      view.value = {
        kind: 'ready',
        items: view.value.items.filter((item) => !removed.has(item.id)),
      };
    },
  );

  const load = (): Promise<void> => {
    const pending = api.list().then((result) => {
      if (result.kind === 'ready') {
        view.value = { kind: 'ready', items: result.items };
        return;
      }
      view.value = { kind: 'unavailable' };
    }).catch(() => {
      view.value = { kind: 'unavailable' };
    });
    settling = pending;
    return pending;
  };

  watch(authState, (state) => {
    if (state === 'loading') {
      view.value = { kind: 'waiting-auth' };
      return;
    }
    if (state === 'authenticated') {
      wasAuthenticated = true;
      view.value = { kind: 'loading' };
      void load();
      return;
    }
    if (state === 'anonymous') {
      // A first-visit anonymous state is the shared route guard's job
      // (middleware/signed-in.global.ts); only a session lost after this
      // page had already read a resume list is this page's own to send on.
      if (wasAuthenticated) void navigateTo('/login');
      return;
    }
    view.value = { kind: 'unavailable' };
  }, { immediate: true });

  const create = async (
    title: string,
    lng: string | null | undefined,
    document?: Resume,
    options?: CreateOpenOptions,
  ): Promise<CreateResumeResult> => {
    if (options?.isCurrent?.() === false) {
      return { kind: 'blocked', intentId: '', reason: 'session-lost' };
    }
    const currentOwner = ownerId.value;
    if (authState.value !== 'authenticated' || currentOwner === null) {
      return { kind: 'blocked', intentId: '', reason: 'session-lost' };
    }
    if (creating !== null) {
      if (
        creating.intent.ownerId === currentOwner
        && creating.intent.title === title
        && creating.intent.lng === lng
        && creating.intent.document === document
        && creating.options === options
        && creating.options?.isCurrent?.() !== false
      ) return creating.promise;
      return { kind: 'blocked', intentId: '', reason: 'unknown' };
    }
    actionNotice.value = null;
    if (retainedCreate.value !== null) {
      return {
        kind: 'blocked',
        intentId: retainedCreate.value.intent.id,
        reason: 'unknown',
      };
    }
    const intent: CreateResumeIntent = {
      kind: 'resumeCreate',
      id: runtime.uuid(),
      ownerId: currentOwner,
      sequence: 0,
      title,
      ...(lng === undefined ? {} : { lng }),
      ...(document === undefined ? {} : { document }),
    };
    const promise = coordinator.createResume(intent).then(async (result) => {
      while (
        !disposed.value
        && (authState.value === 'loading' || authState.value === 'error')
      ) {
        await waitForAuth();
      }
      if (
        disposed.value
        || ownerId.value !== intent.ownerId
        || authState.value !== 'authenticated'
        || options?.isCurrent?.() === false
      ) {
        return { kind: 'blocked', intentId: '', reason: 'unknown' } as const;
      }
      if (result.kind === 'created') {
        options?.beforeOpen?.(result.resume.metadata.id);
        await navigateTo(
          `/app/resumes/${encodeURIComponent(result.resume.metadata.id)}`,
        );
      } else if (result.kind === 'opaque-create') {
        retainedCreate.value = result.outcome;
      }
      return result;
    }).finally(() => {
      creating = null;
    });
    creating = { intent, options, promise };
    return promise;
  };

  const refreshCreate = async (
    intentId: string,
  ): Promise<OpaqueCreateOutcome> => {
    const outcome = await coordinator.refreshOpaqueCreate(intentId);
    retainedCreate.value = outcome;
    if (outcome.refreshedItems !== null) {
      view.value = { kind: 'ready', items: outcome.refreshedItems };
    }
    return outcome;
  };

  const abandonCreate = (intentId: string): void => {
    coordinator.abandonOpaqueCreate(intentId);
    if (retainedCreate.value?.intent.id === intentId) {
      retainedCreate.value = null;
    }
  };

  const initializeForAction = async (
    id: string,
    confirmedTitle?: string,
  ): Promise<ResumeEditorActions | null> => {
    actionNotice.value = null;
    // The resume is this list's from the first await, so leaving meanwhile
    // stops its queue and warns; after each await a list that has gone
    // neither reads, queues nor sends.
    touched.value = new Set([...touched.value, id]);
    starting.value += 1;
    try {
      await refreshAuth();
      if (disposed.value) return null;
      const read = await api.read(id);
      if (disposed.value) return null;
      if (read.kind !== 'complete') {
        view.value = { kind: 'unavailable' };
        return null;
      }
      if (
        confirmedTitle !== undefined
        && read.accepted.metadata.title !== confirmedTitle
      ) {
        actionNotice.value = 'resume-changed';
        return null;
      }
      // Nothing re-initializes a record that holds unsent or in-flight work.
      if (store.recordFor(id) === undefined) store.initialize(read.accepted);
      else store.adoptCompleteRead(id, read.accepted);
      return actionsFor(id);
    } finally {
      starting.value -= 1;
    }
  };

  const updateItem = (id: string, patch: Partial<ResumeSummary>): void => {
    if (view.value.kind !== 'ready') return;
    view.value = {
      kind: 'ready',
      items: view.value.items.map((item) =>
        item.id === id ? { ...item, ...patch } : item),
    };
  };

  /**
   * The card shows what the record holds: the title while it is queued, on
   * its way, unknown or held to be sent again; otherwise, after it saved or
   * was refused for good, the title the server kept.
   */
  const showRecord = (id: string): void => {
    const record = store.recordFor(id);
    if (record === undefined) return;
    updateItem(id, {
      title: titleStillSending(record)
        ? record.current.metadata.title
        : record.accepted.metadata.title,
      updatedAt: record.accepted.metadata.updatedAt,
      revision: record.accepted.revision,
    });
  };

  // A rename the queue settles later, on its own retries, shows its result.
  watch(
    () => [...touched.value].map((id) => {
      const record = store.recordFor(id);
      return record === undefined
        ? id
        : [
            id,
            record.accepted.revision,
            record.accepted.metadata.title,
            record.current.metadata.title,
            titleStillSending(record),
          ].join(':');
    }).join('|'),
    () => {
      for (const id of touched.value) showRecord(id);
    },
  );

  const rename = async (id: string, title: string): Promise<void> => {
    const actions = await initializeForAction(id);
    if (disposed.value) return;
    const result = actions?.edit({
      kind: 'metadataField',
      field: 'title',
      value: title,
    });
    if (result?.kind !== 'enqueued') return;
    ownCommands.add(result.command.id);
    // The card shows the new title at once, then whatever the server kept.
    updateItem(id, { title });
    await coordinator.flush(id);
    if (!disposed.value) showRecord(id);
  };

  const retry = (id: string): Promise<void> => {
    const held = store.recordFor(id)?.attempt;
    if (held === null || held === undefined) return Promise.resolve();
    // Only the queue sends: this asks it to send the held write again.
    return coordinator.retry(id, held.command.id);
  };

  const unsettled = (): boolean => starting.value > 0
    || [...touched.value].some((id) => hasUnsafeWork(store.recordFor(id)));

  const remove = async (id: string, confirmedTitle: string): Promise<void> => {
    const actions = await initializeForAction(id, confirmedTitle);
    if (disposed.value) return;
    const result = actions?.edit({ kind: 'resumeDelete', confirmedTitle });
    if (result?.kind === 'enqueued') {
      ownCommands.add(result.command.id);
      deleting.add(id);
      await coordinator.flush(id);
    }
  };

  return {
    view,
    items,
    actionNotice,
    removalFocusId,
    removalFocusVersion,
    saveStates,
    settled: () => settling,
    unsettled,
    retry,
    create,
    refreshCreate,
    abandonCreate,
    rename,
    remove,
  };
}

/**
 * Whether the record's title is still on its way: queued, in flight, of
 * unknown outcome, or held to be sent again. A write refused for good is
 * not, until Try again sends it; it stays held while the card shows the
 * saved title.
 */
function titleStillSending(record: ResumeRecord): boolean {
  if (record.pending.length > 0) return true;
  const held = record.attempt;
  return held !== null
    && (held.kind !== 'failed'
      || held.reason === 'stale-limit'
      || held.sending === true);
}
