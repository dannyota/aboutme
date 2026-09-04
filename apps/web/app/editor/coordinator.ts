import type { useAuth } from '../composables/useAuth';
import { toRaw } from 'vue';
import {
  dependencyIdsForNewCommand,
  nextSequence,
} from '../stores/resumes';
import type { ResumeRecord, useResumeStore } from '../stores/resumes';
import {
  captureCommand,
  type AtomicEditorCommand,
  type CreateResumeIntent,
} from './commands';
import type {
  AttemptFailureCode,
  AttemptResult,
  FrozenAttempt,
  ResumeSummary,
} from './attempt';
import { freezeAttempt, freezeCreateAttempt } from './resumeApi';
import {
  createReplacementCommand,
  createSupersededConflict,
  reconcileCommand,
  reconcileTemplateGroup,
  type ConflictConfirmation,
} from './reconcile';
import { compareRevision } from './revision';
import type { ResumeApi } from './resumeApi';
import {
  advanceTemplateGroup,
  nextTemplateChild,
  type EditorQueueItem,
  type TemplateGroupCommand,
} from './templateGroup';
import type { AcceptedResume, EditorRuntime } from './types';

const DEBOUNCE_MS = 1_000;
const UNKNOWN_REPLAY_DELAY_MS = 250;

export type CreateResumeResult
  = | { readonly kind: 'created'; readonly resume: AcceptedResume }
    | {
      readonly kind: 'blocked';
      readonly intentId: string;
      readonly reason: 'unknown' | 'session-lost';
    }
    | {
      readonly kind: 'retry-later';
      readonly intentId: string;
      readonly retryAfterMs: number | null;
    }
    | { readonly kind: 'opaque-create'; readonly outcome: OpaqueCreateOutcome }
    | { readonly kind: 'rejected'; readonly code: AttemptFailureCode };

export interface OpaqueCreateOutcome {
  readonly kind: 'create-cutoff';
  readonly intent: CreateResumeIntent;
  readonly attempt: FrozenAttempt;
  readonly refreshedItems: readonly ResumeSummary[] | null;
}

export type OpaquePhotoDecision
  = | { readonly kind: 'keep-observed' }
    | { readonly kind: 'replace'; readonly file: File };

export interface ResumeMutationCoordinator {
  createResume(intent: CreateResumeIntent): Promise<CreateResumeResult>;
  retryCreate(intentId: string): Promise<CreateResumeResult>;
  refreshOpaqueCreate(intentId: string): Promise<OpaqueCreateOutcome>;
  abandonOpaqueCreate(intentId: string): void;
  schedule(resumeId: string): void;
  flush(resumeId: string): Promise<void>;
  completeRead(resumeId: string): Promise<boolean>;
  retry(resumeId: string, commandId: string): Promise<void>;
  refreshAndReconcile(resumeId: string): Promise<void>;
  acceptLatest(resumeId: string, conflictId: string): Promise<void>;
  applyMine(
    resumeId: string,
    conflictId: string,
    confirmation: ConflictConfirmation,
  ): Promise<void>;
  resumeAfterAuth(resumeId: string): Promise<void>;
  resolveOpaquePhoto(
    resumeId: string,
    commandId: string,
    decision: OpaquePhotoDecision,
  ): Promise<void>;
  discard(resumeId: string): void;
}

interface RetainedCreate {
  readonly intent: CreateResumeIntent;
  attempt: FrozenAttempt;
  outcome?: OpaqueCreateOutcome;
}

export function createMutationCoordinator(deps: {
  api: ResumeApi;
  store: ReturnType<typeof useResumeStore>;
  auth: ReturnType<typeof useAuth>;
  runtime: EditorRuntime;
}): ResumeMutationCoordinator {
  const timers = new Map<string, ReturnType<typeof setTimeout>>();
  const drains = new Map<string, Promise<void>>();
  const retainedCreates = new Map<string, RetainedCreate>();
  const csrfRetries = new Set<string>();

  const schedule = (resumeId: string): void => {
    const prior = timers.get(resumeId);
    if (prior !== undefined) clearTimeout(prior);
    timers.set(
      resumeId,
      setTimeout(() => {
        timers.delete(resumeId);
        void flush(resumeId);
      }, DEBOUNCE_MS),
    );
  };

  const flush = (resumeId: string): Promise<void> => {
    const active = drains.get(resumeId);
    if (active !== undefined) return active;
    const drain = drainResume(resumeId).finally(() => drains.delete(resumeId));
    drains.set(resumeId, drain);
    return drain;
  };

  async function drainResume(resumeId: string): Promise<void> {
    for (;;) {
      const record = deps.store.recordFor(resumeId);
      if (
        !canDrain(
          record,
          deps.auth.authState.value,
          deps.auth.user.value?.id,
        )
      ) return;
      if (record.completeReadRequired) return;
      const queueItem = toRaw(record.pending[0]!);
      if (!dependenciesAcknowledged(record, queueItem)) return;
      if (queueItem.kind === 'templateGroup') {
        if (record.templateState === null) {
          deps.store.setTemplateState(resumeId, {
            kind: 'queued',
            nextChild: 0,
          });
          continue;
        }
        if (record.templateState.kind === 'partial') return;
        if (record.templateState.kind === 'complete') {
          deps.store.dropHead(resumeId, queueItem.id);
          continue;
        }
        if (
          record.templateState.kind === 'queued'
          && record.templateState.nextChild === 0
        ) {
          const groupDecision = reconcileTemplateGroup(
            queueItem,
            record.accepted,
          );
          if (groupDecision.kind === 'satisfied') {
            deps.store.setTemplateState(resumeId, null);
            deps.store.dropHead(resumeId, queueItem.id);
            continue;
          }
          if (groupDecision.kind === 'conflict') {
            deps.store.markConflict(resumeId, groupDecision.conflict);
            return;
          }
        }
        const command = nextTemplateChild(queueItem, record.templateState);
        if (command === null) return;
        if (!await dispatchCommand(resumeId, queueItem, command)) return;
        continue;
      }
      const decision = reconcileCommand(queueItem, record.accepted);
      if (decision.kind === 'satisfied') {
        deps.store.dropHead(resumeId, queueItem.id);
        continue;
      }
      if (decision.kind === 'conflict') {
        deps.store.markConflict(resumeId, decision.conflict);
        return;
      }
      if (!await dispatchCommand(resumeId, queueItem, queueItem)) return;
    }
  }

  async function dispatchCommand(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
  ): Promise<boolean> {
    const record = deps.store.recordFor(resumeId);
    let csrfToken = dispatchToken(resumeId, queueItem.ownerId);
    if (
      record !== undefined
      && csrfToken === null
      && !record.sessionLost
    ) {
      try {
        await deps.auth.refresh();
      } catch {
        return false;
      }
      csrfToken = dispatchToken(resumeId, queueItem.ownerId);
    }
    if (record === undefined || csrfToken === null) return false;
    const attempt = freezeAttempt(command, record.accepted, deps.runtime);
    deps.store.startAttempt(resumeId, queueItem, command, attempt);
    await settle(
      resumeId,
      queueItem,
      command,
      attempt,
      await deps.api.dispatch(attempt, csrfToken),
    );
    return true;
  }

  async function settle(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
    attempt: FrozenAttempt,
    result: AttemptResult,
  ): Promise<void> {
    switch (result.kind) {
      case 'complete':
        if (queueItem.kind === 'templateGroup') {
          settleTemplateComplete(resumeId, queueItem, result.accepted);
        } else {
          settleAtomicComplete(resumeId, command, result.accepted);
        }
        return;
      case 'child-ack':
        deps.store.acknowledgeChild(resumeId, queueItem.id, result.etag);
        deps.store.dropHead(resumeId, queueItem.id);
        await completeReadBarrier(resumeId);
        return;
      case 'resume-deleted':
        deps.store.acknowledgeResumeDelete(resumeId, queueItem.id);
        return;
      case 'stale':
        await settleStale(resumeId, queueItem, command, attempt, result.winner);
        return;
      case 'csrf-rejected':
        await refreshThenRetrySameAttemptOnce(
          resumeId,
          queueItem,
          command,
          attempt,
        );
        return;
      case 'session-lost':
        stopForSessionLoss(resumeId);
        return;
      case 'validation-rejected':
        deps.store.setIssues(resumeId, queueItem.id, result.issues);
        if (markTemplateChildFailed(resumeId, queueItem)) return;
        holdFailed(
          deps.store,
          resumeId,
          queueItem,
          command,
          attempt,
          'request_invalid',
        );
        return;
      case 'rate-limited':
        deps.store.holdAttempt(resumeId, {
          kind: 'retry-later',
          queueItem,
          command,
          attempt,
          reason: 'rate-limited',
          retryAfterMs: result.retryAfterMs,
        });
        return;
      case 'media-busy':
        deps.store.holdAttempt(resumeId, {
          kind: 'retry-later',
          queueItem,
          command,
          attempt,
          reason: 'media-busy',
          retryAfterMs: result.retryAfterMs,
        });
        return;
      case 'idempotency-reuse':
        holdFailed(
          deps.store,
          resumeId,
          queueItem,
          command,
          attempt,
          'idempotency-reuse',
        );
        await refreshAndReconcile(resumeId);
        return;
      case 'rejected':
        if (markTemplateChildFailed(resumeId, queueItem)) return;
        holdFailed(
          deps.store,
          resumeId,
          queueItem,
          command,
          attempt,
          result.code,
        );
        return;
      case 'unknown':
        await resolveUnknown(
          resumeId,
          queueItem,
          command,
          attempt,
          result.reason,
        );
        return;
      default:
        return assertNever(result);
    }
  }

  function settleAtomicComplete(
    resumeId: string,
    command: AtomicEditorCommand,
    accepted: AcceptedResume,
  ): void {
    const adoption = deps.store.adoptComplete(resumeId, accepted);
    if (adoption.kind === 'adopted') {
      deps.store.dropHead(resumeId, command.id);
      return;
    }
    const decision = reconcileCommand(command, adoption.winner);
    if (decision.kind === 'satisfied') {
      deps.store.dropHead(resumeId, command.id);
      return;
    }
    deps.store.markConflict(
      resumeId,
      createSupersededConflict(command, accepted, adoption.winner),
    );
    deps.store.dropHead(resumeId, command.id);
  }

  function settleTemplateComplete(
    resumeId: string,
    group: TemplateGroupCommand,
    accepted: AcceptedResume,
  ): void {
    const record = deps.store.recordFor(resumeId);
    if (record === undefined || record.templateState === null) return;
    const adoption = deps.store.adoptComplete(resumeId, accepted);
    if (adoption.kind === 'older') {
      const decision = reconcileTemplateGroup(group, adoption.winner);
      if (decision.kind === 'satisfied') {
        deps.store.setTemplateState(resumeId, null);
        deps.store.dropHead(resumeId, group.id);
        return;
      }
      deps.store.setTemplateState(resumeId, {
        kind: 'partial',
        accepted: adoption.winner,
        nextChild:
          record.templateState.kind === 'queued'
          || record.templateState.kind === 'running'
            ? record.templateState.nextChild
            : 0,
        reason: 'superseded-after-success',
      });
      deps.store.continueTemplateGroup(resumeId, group.id);
      return;
    }
    const next = advanceTemplateGroup(
      group,
      record.templateState,
      adoption.accepted,
    );
    deps.store.setTemplateState(resumeId, next);
    if (next.kind === 'complete') {
      deps.store.dropHead(resumeId, group.id);
      return;
    }
    deps.store.continueTemplateGroup(resumeId, group.id);
  }

  function markTemplateChildFailed(
    resumeId: string,
    queueItem: EditorQueueItem,
  ): boolean {
    if (queueItem.kind !== 'templateGroup') return false;
    const record = deps.store.recordFor(resumeId);
    const state = record?.templateState;
    if (
      record === undefined
      || state === undefined
      || state === null
      || state.kind === 'complete'
      || state.kind === 'partial'
    ) {
      return false;
    }
    deps.store.setTemplateState(resumeId, {
      kind: 'partial',
      accepted: record.accepted,
      nextChild: state.nextChild,
      reason: 'child-failed',
    });
    deps.store.continueTemplateGroup(resumeId, queueItem.id);
    return true;
  }

  async function refreshThenRetrySameAttemptOnce(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
    attempt: FrozenAttempt,
  ): Promise<void> {
    const key = `${resumeId}:${attempt.idempotencyKey}`;
    if (csrfRetries.has(key)) {
      holdFailed(
        deps.store,
        resumeId,
        queueItem,
        command,
        attempt,
        'csrf-rejected',
      );
      return;
    }
    csrfRetries.add(key);
    await deps.auth.refresh();
    const csrfToken = dispatchToken(resumeId, queueItem.ownerId);
    if (csrfToken === null) return;
    await settle(
      resumeId,
      queueItem,
      command,
      attempt,
      await deps.api.dispatch(attempt, csrfToken),
    );
  }

  async function resolveUnknown(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
    attempt: FrozenAttempt,
    reason: 'transport' | 'server',
  ): Promise<void> {
    if (
      attempt.automaticReplays === 0
      && deps.runtime.nowEpochMs() < attempt.retryCutoff
    ) {
      const replay = { ...attempt, automaticReplays: 1 as const };
      deps.store.holdAttempt(resumeId, {
        kind: 'unknown',
        queueItem,
        command,
        attempt: replay,
        reason,
      });
      await deps.runtime.delay(UNKNOWN_REPLAY_DELAY_MS);
      const csrfToken = dispatchToken(resumeId, queueItem.ownerId);
      if (csrfToken === null) return;
      if (deps.runtime.nowEpochMs() >= replay.retryCutoff) {
        await resolveUnknown(resumeId, queueItem, command, replay, reason);
        return;
      }
      await settle(
        resumeId,
        queueItem,
        command,
        replay,
        await deps.api.dispatch(replay, csrfToken),
      );
      return;
    }
    deps.store.holdAttempt(resumeId, {
      kind: 'unknown',
      queueItem,
      command,
      attempt,
      reason:
        deps.runtime.nowEpochMs() >= attempt.retryCutoff ? 'cutoff' : reason,
    });
    if (deps.runtime.nowEpochMs() < attempt.retryCutoff) return;
    if (command.kind === 'photoUpload') {
      await setOpaquePhoto(resumeId, command, attempt);
      return;
    }
    await refreshAndReconcile(resumeId);
  }

  async function settleStale(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
    attempt: FrozenAttempt,
    winner: {
      readonly document: AcceptedResume['document'];
      readonly revision: AcceptedResume['revision'];
    },
  ): Promise<void> {
    const record = deps.store.recordFor(resumeId);
    if (record === undefined || attempt.staleRebases === 1) {
      holdFailed(
        deps.store,
        resumeId,
        queueItem,
        command,
        attempt,
        'second-stale',
      );
      return;
    }
    if (
      command.kind === 'metadataField'
      || command.kind === 'resumeDelete'
      || compareRevision(winner.revision, record.accepted.revision) <= 0
    ) {
      await refreshAndReconcile(resumeId);
      return;
    }
    const snapshot: AcceptedResume = {
      document: winner.document,
      revision: winner.revision,
      metadata: record.accepted.metadata,
      metadataFreshness: 'stale',
    };
    deps.store.adoptStaleWinner(resumeId, snapshot);
    const decision = reconcileCommand(command, snapshot);
    if (decision.kind === 'satisfied') {
      deps.store.dropHead(resumeId, queueItem.id);
      return;
    }
    if (decision.kind === 'conflict') {
      deps.store.markConflict(resumeId, decision.conflict);
      return;
    }
    const rebased = {
      ...freezeAttempt(command, snapshot, deps.runtime),
      staleRebases: 1 as const,
    };
    deps.store.holdAttempt(resumeId, {
      kind: 'unknown',
      queueItem,
      command,
      attempt: rebased,
      reason: 'server',
    });
    const csrfToken = dispatchToken(resumeId, queueItem.ownerId);
    if (csrfToken === null) return;
    await settle(
      resumeId,
      queueItem,
      command,
      rebased,
      await deps.api.dispatch(rebased, csrfToken),
    );
  }

  async function refreshAndReconcile(resumeId: string): Promise<void> {
    const result = await deps.api.read(resumeId);
    if (result.kind === 'session-lost') {
      stopForSessionLoss(resumeId);
      return;
    }
    if (result.kind !== 'complete') return;
    const record = deps.store.recordFor(resumeId);
    if (record === undefined) return;
    deps.store.adoptComplete(resumeId, result.accepted);
    const active = deps.store.recordFor(resumeId)?.attempt;
    if (active === null || active === undefined) return;
    const decision = reconcileCommand(toRaw(active.command), result.accepted);
    if (decision.kind === 'satisfied') {
      deps.store.dropHead(resumeId, active.queueItem.id);
    } else if (decision.kind === 'conflict') {
      deps.store.markConflict(resumeId, decision.conflict);
    }
  }

  async function completeReadBarrier(resumeId: string): Promise<boolean> {
    const result = await deps.api.read(resumeId);
    if (result.kind === 'session-lost') {
      stopForSessionLoss(resumeId);
      return false;
    }
    if (result.kind !== 'complete') return false;
    deps.store.adoptCompleteRead(resumeId, result.accepted);
    return !deps.store.recordFor(resumeId)?.completeReadRequired;
  }

  async function retry(resumeId: string, commandId: string): Promise<void> {
    const active = deps.store.recordFor(resumeId)?.attempt;
    if (
      active === null
      || active === undefined
      || active.command.id !== commandId
    ) {
      return;
    }
    const queueItem = toRaw(active.queueItem);
    const command = toRaw(active.command);
    const attempt = toRaw(active.attempt);
    if (deps.runtime.nowEpochMs() >= attempt.retryCutoff) {
      await resolveUnknown(resumeId, queueItem, command, attempt, 'transport');
      return;
    }
    const csrfToken = dispatchToken(resumeId, queueItem.ownerId);
    if (csrfToken === null) return;
    await settle(
      resumeId,
      queueItem,
      command,
      attempt,
      await deps.api.dispatch(attempt, csrfToken),
    );
  }

  async function createResume(
    intent: CreateResumeIntent,
  ): Promise<CreateResumeResult> {
    const retained = retainedCreates.get(intent.id) ?? {
      intent,
      attempt: freezeCreateAttempt(intent, deps.runtime),
    };
    retainedCreates.set(intent.id, retained);
    return dispatchCreate(retained);
  }

  async function retryCreate(intentId: string): Promise<CreateResumeResult> {
    const retained = retainedCreates.get(intentId);
    if (retained === undefined) {
      return { kind: 'blocked', intentId, reason: 'unknown' };
    }
    if (retained.outcome !== undefined) {
      return { kind: 'opaque-create', outcome: retained.outcome };
    }
    return dispatchCreate(retained);
  }

  async function dispatchCreate(
    retained: RetainedCreate,
  ): Promise<CreateResumeResult> {
    const csrfToken = dispatchToken(undefined, retained.intent.ownerId);
    if (csrfToken === null) {
      return {
        kind: 'blocked',
        intentId: retained.intent.id,
        reason: 'session-lost',
      };
    }
    const result = await deps.api.dispatch(retained.attempt, csrfToken);
    switch (result.kind) {
      case 'complete':
        if (result.status !== 201) {
          return {
            kind: 'blocked',
            intentId: retained.intent.id,
            reason: 'unknown',
          };
        }
        retainedCreates.delete(retained.intent.id);
        return { kind: 'created', resume: result.accepted };
      case 'csrf-rejected': {
        await deps.auth.refresh();
        const freshToken = dispatchToken(undefined, retained.intent.ownerId);
        if (freshToken === null) {
          return {
            kind: 'blocked',
            intentId: retained.intent.id,
            reason: 'session-lost',
          };
        }
        return settleCreate(
          retained,
          await deps.api.dispatch(retained.attempt, freshToken),
        );
      }
      default:
        return settleCreate(retained, result);
    }
  }

  async function settleCreate(
    retained: RetainedCreate,
    result: AttemptResult,
  ): Promise<CreateResumeResult> {
    switch (result.kind) {
      case 'complete':
        if (result.status === 201) {
          retainedCreates.delete(retained.intent.id);
          return { kind: 'created', resume: result.accepted };
        }
        return {
          kind: 'blocked',
          intentId: retained.intent.id,
          reason: 'unknown',
        };
      case 'session-lost':
        return {
          kind: 'blocked',
          intentId: retained.intent.id,
          reason: 'session-lost',
        };
      case 'rate-limited':
      case 'media-busy':
        return {
          kind: 'retry-later',
          intentId: retained.intent.id,
          retryAfterMs: result.retryAfterMs,
        };
      case 'rejected':
        retainedCreates.delete(retained.intent.id);
        return { kind: 'rejected', code: result.code };
      case 'unknown':
        if (
          retained.attempt.automaticReplays === 0
          && deps.runtime.nowEpochMs() < retained.attempt.retryCutoff
        ) {
          retained.attempt = { ...retained.attempt, automaticReplays: 1 };
          await deps.runtime.delay(UNKNOWN_REPLAY_DELAY_MS);
          return dispatchCreate(retained);
        }
        if (deps.runtime.nowEpochMs() < retained.attempt.retryCutoff) {
          return {
            kind: 'blocked',
            intentId: retained.intent.id,
            reason: 'unknown',
          };
        }
        return opaqueCreate(retained);
      default:
        return {
          kind: 'blocked',
          intentId: retained.intent.id,
          reason: 'unknown',
        };
    }
  }

  async function opaqueCreate(
    retained: RetainedCreate,
  ): Promise<CreateResumeResult> {
    const listed = await deps.api.list();
    const outcome: OpaqueCreateOutcome = {
      kind: 'create-cutoff',
      intent: retained.intent,
      attempt: retained.attempt,
      refreshedItems: listed.kind === 'ready' ? listed.items : null,
    };
    retained.outcome = outcome;
    return { kind: 'opaque-create', outcome };
  }

  async function refreshOpaqueCreate(
    intentId: string,
  ): Promise<OpaqueCreateOutcome> {
    const retained = retainedCreates.get(intentId);
    if (retained?.outcome === undefined) {
      throw new Error('opaque create not found');
    }
    const listed = await deps.api.list();
    retained.outcome = {
      ...retained.outcome,
      refreshedItems: listed.kind === 'ready' ? listed.items : null,
    };
    return retained.outcome;
  }

  function abandonOpaqueCreate(intentId: string): void {
    retainedCreates.delete(intentId);
  }

  async function setOpaquePhoto(
    resumeId: string,
    command: Extract<AtomicEditorCommand, { kind: 'photoUpload' }>,
    attempt: FrozenAttempt,
  ): Promise<void> {
    const before
      = deps.store.recordFor(resumeId)?.accepted.document.personalDetails.photo
        ?.key;
    const result = await deps.api.read(resumeId);
    let observed: 'unchanged' | 'changed' | 'unavailable' = 'unavailable';
    if (result.kind === 'complete') {
      observed
        = result.accepted.document.personalDetails.photo?.key === before
          ? 'unchanged'
          : 'changed';
      deps.store.adoptComplete(resumeId, result.accepted);
    }
    deps.store.setOpaquePhotoOutcome(resumeId, {
      kind: 'photo-cutoff',
      command,
      attempt,
      observed,
    });
  }

  async function resolveOpaquePhoto(
    resumeId: string,
    commandId: string,
    decision: OpaquePhotoDecision,
  ): Promise<void> {
    const record = deps.store.recordFor(resumeId);
    const opaque = record?.opaquePhotoOutcome;
    if (
      record === undefined
      || opaque === undefined
      || opaque === null
      || opaque.command.id !== commandId
    ) {
      return;
    }
    deps.store.dropHead(resumeId, commandId);
    deps.store.setOpaquePhotoOutcome(resumeId, null);
    if (decision.kind === 'keep-observed') return;
    const replacement = captureCommand(
      record.current,
      {
        resumeId,
        ownerId: opaque.command.ownerId,
        sequence: nextSequence(record),
        dependencyIds: dependencyIdsForNewCommand(record),
        intent: { kind: 'photoUpload', file: decision.file },
      },
      deps.runtime,
    );
    deps.store.enqueue(resumeId, replacement);
    schedule(resumeId);
  }

  async function acceptLatest(
    resumeId: string,
    conflictId: string,
  ): Promise<void> {
    const conflict = deps.store
      .recordFor(resumeId)
      ?.conflicts.find((candidate) => candidate.id === conflictId);
    if (conflict === undefined) return;
    deps.store.dropHead(resumeId, conflict.id);
    deps.store.resolveConflict(resumeId, conflictId);
    if (
      deps.store.recordFor(resumeId)?.completeReadRequired === true
      && !await completeReadBarrier(resumeId)
    ) {
      return;
    }
    await flush(resumeId);
  }

  async function applyMine(
    resumeId: string,
    conflictId: string,
    confirmation: ConflictConfirmation,
  ): Promise<void> {
    const conflict = deps.store
      .recordFor(resumeId)
      ?.conflicts.find((candidate) => candidate.id === conflictId);
    if (conflict?.subject !== 'atomic') return;
    const latest = await deps.api.read(resumeId);
    if (latest.kind !== 'complete') return;
    const replacement = createReplacementCommand(
      conflict,
      latest.accepted,
      confirmation,
    );
    if (replacement === null) return;
    if (!deps.store.replaceActiveAfterCompleteRead(
      resumeId,
      conflict.command.id,
      latest.accepted,
      replacement,
    )) return;
    deps.store.resolveConflict(resumeId, conflictId);
    schedule(resumeId);
  }

  async function resumeAfterAuth(resumeId: string): Promise<void> {
    await deps.auth.refresh();
    const record = deps.store.recordFor(resumeId);
    const ownerId = deps.auth.user.value?.id;
    const retainedOwner
      = record?.attempt?.queueItem.ownerId ?? record?.pending[0]?.ownerId;
    if (
      record === undefined
      || ownerId === undefined
      || deps.auth.authState.value !== 'authenticated'
      || (retainedOwner !== undefined && retainedOwner !== ownerId)
    ) {
      return;
    }
    const result = await deps.api.read(resumeId);
    if (result.kind !== 'complete') return;
    deps.store.adoptComplete(resumeId, result.accepted);
    deps.store.clearSessionLost(resumeId);
    const active = deps.store.recordFor(resumeId)?.attempt;
    if (active !== null && active !== undefined) {
      await retry(resumeId, active.command.id);
    } else {
      schedule(resumeId);
    }
  }

  function discard(resumeId: string): void {
    const timer = timers.get(resumeId);
    if (timer !== undefined) clearTimeout(timer);
    timers.delete(resumeId);
    deps.store.discardLocal(resumeId);
  }

  function dispatchToken(
    resumeId: string | undefined,
    ownerId: string,
  ): string | null {
    const token = deps.auth.csrfToken.value;
    if (
      token !== null
      && deps.auth.authState.value === 'authenticated'
      && deps.auth.user.value?.id === ownerId
    ) return token;
    if (
      resumeId !== undefined
      && (deps.auth.authState.value === 'anonymous'
        || (deps.auth.authState.value === 'authenticated'
          && deps.auth.user.value?.id !== ownerId))
    ) {
      stopForSessionLoss(resumeId);
    }
    return null;
  }

  function stopForSessionLoss(resumeId: string): void {
    const timer = timers.get(resumeId);
    if (timer !== undefined) clearTimeout(timer);
    timers.delete(resumeId);
    deps.store.markSessionLost(resumeId);
  }

  return {
    createResume,
    retryCreate,
    refreshOpaqueCreate,
    abandonOpaqueCreate,
    schedule,
    flush,
    completeRead: completeReadBarrier,
    retry,
    refreshAndReconcile,
    acceptLatest,
    applyMine,
    resumeAfterAuth,
    resolveOpaquePhoto,
    discard,
  };
}

function canDrain(
  record: ResumeRecord | undefined,
  authState: ReturnType<typeof useAuth>['authState']['value'],
  ownerId: string | undefined,
): record is ResumeRecord {
  return (
    record !== undefined
    && record.pending.length > 0
    && record.attempt === null
    && !record.sessionLost
    && record.opaquePhotoOutcome === null
    && authState === 'authenticated'
    && ownerId !== undefined
    && record.pending[0]!.ownerId === ownerId
  );
}

function dependenciesAcknowledged(
  record: ResumeRecord,
  item: EditorQueueItem,
): boolean {
  return !item.dependencyIds.some(
    (id) =>
      record.attempt?.queueItem.id === id
      || record.pending.some((candidate) => candidate.id === id),
  );
}

function holdFailed(
  store: ReturnType<typeof useResumeStore>,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  reason:
    AttemptFailureCode | 'csrf-rejected' | 'idempotency-reuse' | 'second-stale',
): void {
  // `request_invalid` is the stable holder for a validation issue; individual
  // field issue codes remain on the record for the UI.
  const failure = reason === 'request_invalid' ? 'request_invalid' : reason;
  store.holdAttempt(resumeId, {
    kind: 'failed',
    queueItem,
    command,
    attempt,
    reason: failure,
  });
}

function assertNever(value: never): never {
  throw new Error(`unhandled attempt result: ${String(value)}`);
}
