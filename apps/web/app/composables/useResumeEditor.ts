import type { TemplatePreset } from '@aboutme/schema/templates';
import { computed, toRaw, type ComputedRef } from 'vue';

import {
  createMutationCoordinator,
  type OpaquePhotoDecision,
  type ResumeMutationCoordinator,
} from '../editor/coordinator';
import {
  captureCommand,
  type AtomicCommandIntent,
  type AtomicEditorCommand,
} from '../editor/commands';
import {
  createResumeApi,
  type ResumeConditionalReadResult,
  type ResumeReadResult,
} from '../editor/resumeApi';
import type { ParentETag, EditorRuntime } from '../editor/types';
import { createPublishApi } from '../editor/publishApi';
import {
  createPdfDownloadController,
  type PdfDownloadController,
} from '../editor/pdfDownload';
import {
  createPublishController,
  type PublishController,
} from '../editor/publishController';
import {
  captureTemplateGroup,
  captureTemplateUndo,
  recoverTemplateGroup,
  type TemplateGroupCommand,
  type TemplateRecovery,
} from '../editor/templateGroup';
import type { ConflictConfirmation } from '../editor/reconcile';
import {
  dependencyIdsForNewCommand,
  nextSequence,
  type ResumeRecord,
  useResumeStore,
} from '../stores/resumes';
import { useAuth } from './useAuth';

export const browserEditorRuntime: EditorRuntime = {
  nowEpochMs: () => Date.now(),
  uuid: () => crypto.randomUUID(),
  delay: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
};

type EditorActionResult
  = | { readonly kind: 'enqueued'; readonly command: AtomicEditorCommand }
    | {
      readonly kind: 'blocked';
      readonly reason:
        | 'not-loaded'
        | 'session-lost'
        | 'owner-mismatch'
        | 'stopped';
    };

/** An action the person started in an editor, carried across its awaits. */
export interface EditorContinuation {
  /** The editor's verified owner when the action started. */
  readonly ownerId: string;
  /**
   * Whether the action may still act: the editor is open on the same
   * record and its owner is the one signed in.
   */
  current(): boolean;
}

type TemplateActionResult
  = | { readonly kind: 'enqueued'; readonly group: TemplateGroupCommand }
    | { readonly kind: 'no-change' }
    | Extract<EditorActionResult, { kind: 'blocked' }>;

export interface ResumeEditorActions {
  readonly record: ComputedRef<ResumeRecord | undefined>;
  createEntityId(): string;
  edit(intent: AtomicCommandIntent): EditorActionResult;
  /** Binds an action starting now to this editor; null when it cannot act. */
  continuation(): EditorContinuation | null;
  applyTemplate(preset: Readonly<TemplatePreset>): TemplateActionResult;
  undoTemplate(): TemplateRecovery;
  recoverTemplate(
    action: 'retry-remaining' | 'restore-pre-apply' | 'keep-partial',
  ): TemplateRecovery;
  resolveOpaquePhoto(
    commandId: string,
    decision: OpaquePhotoDecision,
  ): Promise<void>;
  retry(commandId: string): Promise<void>;
  acceptLatest(conflictId: string): Promise<void>;
  applyMine(
    conflictId: string,
    confirmation: ConflictConfirmation,
  ): Promise<void>;
  resumeAfterAuth(): Promise<void>;
  /** Goes on with the queue once the session is the owner's again. */
  wake(recovered: boolean): Promise<void>;
  refresh(): Promise<ResumeReadResult>;
  refreshConditional?: (
    etag?: ParentETag,
  ) => Promise<ResumeConditionalReadResult>;
  discard(): void;
  /**
   * Ends background reads and writes when the editor closes, and drops the
   * work it has not saved: the resume opens afresh next time.
   */
  stop(): void;
  readonly downloadPdf: PdfDownloadController;
  readonly publish: PublishController;
}

export interface ResumeEditorActionDeps {
  resumeId: string;
  store: ReturnType<typeof useResumeStore>;
  coordinator: ResumeMutationCoordinator;
  auth: ReturnType<typeof useAuth>;
  runtime: EditorRuntime;
  providerLogin?: ComputedRef<boolean>;
  /**
   * The verified owner of the editor's work, captured when it opened. Left
   * out, the first signed-in user the actions see is kept.
   */
  ownerId?: string;
  /** Checks the session again for Resume after sign-in, if the page does. */
  recheckSession?: () => Promise<void>;
}

export function createResumeEditorActions(
  deps: ResumeEditorActionDeps,
): ResumeEditorActions {
  const record = computed(() => deps.store.recordFor(deps.resumeId));
  const publish = createPublishController({
    resumeId: deps.resumeId,
    store: deps.store,
    coordinator: deps.coordinator,
    auth: deps.auth,
    runtime: deps.runtime,
    api: createPublishApi(),
    providerLogin: deps.providerLogin ?? computed(() => false),
  });
  const downloadPdf = createPdfDownloadController({
    resumeId: deps.resumeId,
    record,
    flush: () => deps.coordinator.flush(deps.resumeId),
    auth: deps.auth,
  });
  const blocked = (
    reason: Extract<EditorActionResult, { kind: 'blocked' }>['reason'],
  ): Extract<EditorActionResult, { kind: 'blocked' }> => ({
    kind: 'blocked',
    reason,
  });
  // Local edits are captured for the editor's own owner for its whole life,
  // so typing while the session cannot be checked is queued, not refused.
  // Sending still waits for that owner to be the one signed in.
  let retainedOwner = deps.ownerId;
  const owner = (): string | undefined =>
    (retainedOwner ??= deps.auth.user.value?.id);
  const editable = () => {
    const state = record.value;
    const ownerId = owner();
    if (state === undefined || ownerId === undefined) return null;
    if (state.sessionLost) return null;
    const pendingOwner
      = state.attempt?.queueItem.ownerId ?? state.pending[0]?.ownerId;
    if (pendingOwner !== undefined && pendingOwner !== ownerId) return null;
    return { state, ownerId };
  };
  // A closed editor queues nothing: work a late callback queued would stay
  // in the shared store and be sent by the next editor on the resume.
  let stopped = false;
  const edit = (intent: AtomicCommandIntent): EditorActionResult => {
    if (stopped) return blocked('stopped');
    const state = record.value;
    const ownerId = owner();
    if (state === undefined || ownerId === undefined) {
      return blocked('not-loaded');
    }
    if (state.sessionLost) return blocked('session-lost');
    const pendingOwner
      = state.attempt?.queueItem.ownerId ?? state.pending[0]?.ownerId;
    if (pendingOwner !== undefined && pendingOwner !== ownerId) {
      return blocked('owner-mismatch');
    }
    const command = captureCommand(
      state.current,
      {
        resumeId: deps.resumeId,
        ownerId,
        sequence: nextSequence(state),
        dependencyIds: dependencyIdsForNewCommand(state),
        intent,
      },
      deps.runtime,
    );
    deps.store.enqueue(deps.resumeId, command);
    deps.coordinator.schedule(deps.resumeId);
    return { kind: 'enqueued', command };
  };
  const continuation = (): EditorContinuation | null => {
    const state = record.value;
    const ownerId = owner();
    if (stopped || state === undefined || ownerId === undefined) return null;
    const epoch = toRaw(state);
    return {
      ownerId,
      current: () => !stopped
        && record.value !== undefined
        && toRaw(record.value) === epoch
        && deps.auth.authState.value === 'authenticated'
        && deps.auth.user.value?.id === ownerId,
    };
  };
  const applyTemplate = (
    preset: Readonly<TemplatePreset>,
  ): TemplateActionResult => {
    const current = editable();
    if (current === null) {
      const state = record.value;
      if (state === undefined || owner() === undefined) {
        return blocked('not-loaded');
      }
      return state.sessionLost
        ? blocked('session-lost')
        : blocked('owner-mismatch');
    }
    const group = captureTemplateGroup({
      resumeId: deps.resumeId,
      ownerId: current.ownerId,
      sequence: nextSequence(current.state),
      current: toRaw(current.state.current),
      preset,
      dependencyIds: dependencyIdsForNewCommand(current.state),
      runtime: deps.runtime,
    });
    if (group === null) return { kind: 'no-change' };
    deps.store.enqueue(deps.resumeId, group);
    deps.coordinator.schedule(deps.resumeId);
    return { kind: 'enqueued', group };
  };
  const recoverTemplate = (
    action: 'retry-remaining' | 'restore-pre-apply' | 'keep-partial',
  ): TemplateRecovery => {
    const state = record.value;
    const group = activeTemplateGroup(state);
    if (state?.templateState?.kind !== 'partial' || group === undefined) {
      return { kind: 'unavailable', reason: 'state-changed' };
    }
    if (state.completeReadRequired) {
      return { kind: 'unavailable', reason: 'read-required' };
    }
    const result = recoverTemplateGroup(
      group,
      state.templateState,
      toRaw(state.accepted),
      action,
      deps.runtime,
    );
    if (result.kind === 'keep-partial') {
      deps.store.setIssues(deps.resumeId, group.id, []);
      deps.store.dropHead(deps.resumeId, group.id);
      deps.store.setTemplateState(deps.resumeId, null);
      return result;
    }
    if (result.kind === 'enqueue') {
      deps.store.setIssues(deps.resumeId, group.id, []);
      deps.store.replaceHead(deps.resumeId, result.group);
      deps.store.setTemplateState(deps.resumeId, {
        kind: 'queued',
        nextChild:
          result.group.id === group.id ? state.templateState.nextChild : 0,
      });
      deps.coordinator.schedule(deps.resumeId);
    }
    return result;
  };
  const undoTemplate = (): TemplateRecovery => {
    const state = record.value;
    if (state?.templateState?.kind !== 'complete') {
      return { kind: 'unavailable', reason: 'state-changed' };
    }
    const result = captureTemplateUndo({
      undo: state.templateState.undo,
      current: toRaw(state.accepted),
      ownerId: owner() ?? '',
      sequence: nextSequence(state),
      dependencyIds: dependencyIdsForNewCommand(state),
      runtime: deps.runtime,
    });
    if (result.kind === 'enqueue') {
      deps.store.enqueue(deps.resumeId, result.group);
      deps.coordinator.schedule(deps.resumeId);
    }
    return result;
  };
  return {
    record,
    createEntityId: () => deps.runtime.uuid(),
    edit,
    continuation,
    applyTemplate,
    undoTemplate,
    recoverTemplate,
    resolveOpaquePhoto: (commandId, decision) =>
      deps.coordinator.resolveOpaquePhoto(deps.resumeId, commandId, decision),
    retry: (commandId) => deps.coordinator.retry(deps.resumeId, commandId),
    acceptLatest: (conflictId) =>
      deps.coordinator.acceptLatest(deps.resumeId, conflictId),
    applyMine: (conflictId, confirmation) =>
      deps.coordinator.applyMine(deps.resumeId, conflictId, confirmation),
    resumeAfterAuth: deps.recheckSession
      ?? (() => deps.coordinator.resumeAfterAuth(deps.resumeId)),
    wake: (recovered) => deps.coordinator.wake(deps.resumeId, recovered),
    refresh: () => deps.coordinator.refreshAndReconcile(deps.resumeId),
    refreshConditional:
      deps.coordinator.refreshConditionalAndReconcile === undefined
        ? undefined
        : (etag) =>
            deps.coordinator.refreshConditionalAndReconcile!(
              deps.resumeId,
              etag,
            ),
    discard: () => deps.coordinator.discard(deps.resumeId),
    stop: () => {
      stopped = true;
      deps.coordinator.stop(deps.resumeId);
      deps.coordinator.discard(deps.resumeId);
    },
    downloadPdf,
    publish,
  };
}

function activeTemplateGroup(
  state: ResumeRecord | undefined,
): TemplateGroupCommand | undefined {
  const active = state?.attempt?.queueItem;
  if (active?.kind === 'templateGroup') return toRaw(active);
  const pending = state?.pending.find((item) => item.kind === 'templateGroup');
  return pending === undefined ? undefined : toRaw(pending);
}

export function useResumeEditor(
  resumeId: string,
  runtime: EditorRuntime = browserEditorRuntime,
  session: Pick<ResumeEditorActionDeps, 'ownerId' | 'recheckSession'> = {},
): ResumeEditorActions {
  const store = useResumeStore();
  const auth = useAuth();
  const { providerLogin } = useCapabilities();
  const coordinator = createMutationCoordinator({
    api: createResumeApi(),
    store,
    auth,
    runtime,
  });
  return createResumeEditorActions({
    resumeId,
    store,
    coordinator,
    auth,
    runtime,
    providerLogin,
    ...session,
  });
}
