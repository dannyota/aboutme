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
import { createResumeApi } from '../editor/resumeApi';
import {
  captureTemplateGroup,
  captureTemplateUndo,
  recoverTemplateGroup,
  type TemplateGroupCommand,
  type TemplateRecovery,
} from '../editor/templateGroup';
import type { ConflictConfirmation } from '../editor/reconcile';
import type { EditorRuntime } from '../editor/types';
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

export type EditorActionResult
  = | { readonly kind: 'enqueued'; readonly command: AtomicEditorCommand }
    | {
      readonly kind: 'blocked';
      readonly reason: 'not-loaded' | 'session-lost' | 'owner-mismatch';
    };

export type TemplateActionResult
  = | { readonly kind: 'enqueued'; readonly group: TemplateGroupCommand }
    | { readonly kind: 'no-change' }
    | Extract<EditorActionResult, { kind: 'blocked' }>;

export interface ResumeEditorActions {
  readonly record: ComputedRef<ResumeRecord | undefined>;
  createEntityId(): string;
  edit(intent: AtomicCommandIntent): EditorActionResult;
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
  discard(): void;
}

export interface ResumeEditorActionDeps {
  resumeId: string;
  store: ReturnType<typeof useResumeStore>;
  coordinator: ResumeMutationCoordinator;
  auth: ReturnType<typeof useAuth>;
  runtime: EditorRuntime;
}

export function createResumeEditorActions(
  deps: ResumeEditorActionDeps,
): ResumeEditorActions {
  const record = computed(() => deps.store.recordFor(deps.resumeId));
  const blocked = (
    reason: Extract<EditorActionResult, { kind: 'blocked' }>['reason'],
  ): Extract<EditorActionResult, { kind: 'blocked' }> => ({
    kind: 'blocked',
    reason,
  });
  const editable = () => {
    const state = record.value;
    const ownerId = deps.auth.user.value?.id;
    if (state === undefined || ownerId === undefined) return null;
    if (state.sessionLost) return null;
    const pendingOwner
      = state.attempt?.queueItem.ownerId ?? state.pending[0]?.ownerId;
    if (pendingOwner !== undefined && pendingOwner !== ownerId) return null;
    return { state, ownerId };
  };
  const edit = (intent: AtomicCommandIntent): EditorActionResult => {
    const state = record.value;
    const ownerId = deps.auth.user.value?.id;
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
  const applyTemplate = (
    preset: Readonly<TemplatePreset>,
  ): TemplateActionResult => {
    const current = editable();
    if (current === null) {
      const state = record.value;
      if (state === undefined || deps.auth.user.value === null) {
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
      deps.store.dropHead(deps.resumeId, group.id);
      deps.store.setTemplateState(deps.resumeId, null);
      return result;
    }
    if (result.kind === 'enqueue') {
      deps.store.replaceHead(deps.resumeId, result.group);
      deps.store.setTemplateState(deps.resumeId, {
        kind: 'queued',
        nextChild: result.group.id === group.id
          ? state.templateState.nextChild
          : 0,
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
      ownerId: deps.auth.user.value?.id ?? '',
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
    resumeAfterAuth: () => deps.coordinator.resumeAfterAuth(deps.resumeId),
    discard: () => deps.coordinator.discard(deps.resumeId),
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
): ResumeEditorActions {
  const store = useResumeStore();
  const auth = useAuth();
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
  });
}
