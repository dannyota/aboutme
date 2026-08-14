import { computed, ref, watch, type ComputedRef, type Ref } from 'vue';

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
import { useResumeStore } from '../stores/resumes';
import {
  browserEditorRuntime,
  createResumeEditorActions,
  type ResumeEditorActions,
} from './useResumeEditor';
import { useAuth, type AuthState } from './useAuth';

export type ResumeListView
  = | { readonly kind: 'waiting-auth' }
    | { readonly kind: 'loading' }
    | { readonly kind: 'ready'; readonly items: readonly ResumeSummary[] }
    | { readonly kind: 'unavailable' };

export interface ResumeListController {
  readonly view: Ref<ResumeListView>;
  readonly items: ComputedRef<readonly ResumeSummary[]>;
  readonly actionMessage: Ref<string | null>;
  readonly removalFocusId: Ref<string | null>;
  readonly removalFocusVersion: Ref<number>;
  settled(): Promise<void>;
  create(
    title: string,
    lng: string | null | undefined,
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
}

export function createStatusMessage(result: CreateResumeResult): string | null {
  if (result.kind === 'rejected' && result.code === 'resume_cap_exceeded') {
    return 'You have reached the resume limit.';
  }
  if (result.kind === 'rejected') {
    return 'Could not create the resume. Try again.';
  }
  if (result.kind === 'retry-later') return 'Please wait, then try again.';
  if (result.kind === 'blocked' && result.reason === 'session-lost') {
    return 'Your session ended. Sign in again.';
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
  const view = ref<ResumeListView>({ kind: 'waiting-auth' });
  const actionMessage = ref<string | null>(null);
  const removalFocusId = ref<string | null>(null);
  const removalFocusVersion = ref(0);
  const retainedCreate = ref<OpaqueCreateOutcome | null>(null);
  const deleting = new Set<string>();
  let creating: Promise<CreateResumeResult> | null = null;
  let settling: Promise<void> = Promise.resolve();

  const items = computed<readonly ResumeSummary[]>(() =>
    view.value.kind === 'ready' ? view.value.items : [],
  );

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
      view.value = { kind: 'loading' };
      void load();
      return;
    }
    if (state === 'anonymous') {
      void navigateTo('/login');
      return;
    }
    view.value = { kind: 'unavailable' };
  }, { immediate: true });

  const create = async (
    title: string,
    lng: string | null | undefined,
  ): Promise<CreateResumeResult> => {
    if (creating !== null) return creating;
    actionMessage.value = null;
    if (retainedCreate.value !== null) {
      return {
        kind: 'blocked',
        intentId: retainedCreate.value.intent.id,
        reason: 'unknown',
      };
    }
    const currentOwner = ownerId.value;
    if (authState.value !== 'authenticated' || currentOwner === null) {
      return { kind: 'blocked', intentId: '', reason: 'session-lost' };
    }
    const intent: CreateResumeIntent = {
      kind: 'resumeCreate',
      id: runtime.uuid(),
      ownerId: currentOwner,
      sequence: 0,
      title,
      ...(lng === undefined ? {} : { lng }),
    };
    creating = coordinator.createResume(intent).then(async (result) => {
      if (result.kind === 'created') {
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
    return creating;
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
    actionMessage.value = null;
    const read = await api.read(id);
    if (read.kind !== 'complete') {
      view.value = { kind: 'unavailable' };
      return null;
    }
    if (
      confirmedTitle !== undefined
      && read.accepted.metadata.title !== confirmedTitle
    ) {
      actionMessage.value = 'This resume changed. Reopen deletion and confirm '
        + 'its current title.';
      return null;
    }
    if (store.recordFor(id) === undefined) store.initialize(read.accepted);
    else store.adoptCompleteRead(id, read.accepted);
    return actionsFor(id);
  };

  const rename = async (id: string, title: string): Promise<void> => {
    const actions = await initializeForAction(id);
    actions?.edit({ kind: 'metadataField', field: 'title', value: title });
  };

  const remove = async (id: string, confirmedTitle: string): Promise<void> => {
    const actions = await initializeForAction(id, confirmedTitle);
    const result = actions?.edit({ kind: 'resumeDelete', confirmedTitle });
    if (result?.kind === 'enqueued') deleting.add(id);
  };

  return {
    view,
    items,
    actionMessage,
    removalFocusId,
    removalFocusVersion,
    settled: () => settling,
    create,
    refreshCreate,
    abandonCreate,
    rename,
    remove,
  };
}
