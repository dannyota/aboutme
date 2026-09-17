import type { AtomicEditorCommand } from './commands';
import type { AttemptResult, FrozenAttempt } from './attempt';
import { createSupersededConflict, reconcileCommand } from './reconcile';
import { compareRevision } from './revision';
import { freezeAttempt } from './resumeApi';
import { reconcileTemplateChild, type EditorQueueItem } from './templateGroup';
import type { AcceptedResume } from './types';
import {
  assertNever,
  dispatchToken,
  holdFailed,
  stopForSessionLoss,
  UNKNOWN_REPLAY_DELAY_MS,
} from './coordinatorGuards';
import {
  completeReadBarrier,
  refreshAndReconcile,
  settleTemplateComplete,
} from './coordinatorReconcile';
import type { CoordinatorContext } from './coordinatorTypes';

export async function settle(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  result: AttemptResult,
): Promise<void> {
  switch (result.kind) {
    case 'complete':
      if (queueItem.kind === 'templateGroup') {
        settleTemplateComplete(ctx, resumeId, queueItem, result.accepted);
      } else {
        settleAtomicComplete(ctx, resumeId, command, result.accepted);
      }
      return;
    case 'child-ack':
      ctx.deps.store.acknowledgeChild(resumeId, queueItem.id, result.etag);
      ctx.deps.store.dropHead(resumeId, queueItem.id);
      await completeReadBarrier(ctx, resumeId);
      return;
    case 'resume-deleted':
      ctx.deps.store.acknowledgeResumeDelete(resumeId, queueItem.id);
      return;
    case 'stale':
      await settleStale(
        ctx,
        resumeId,
        queueItem,
        command,
        attempt,
        result.winner,
      );
      return;
    case 'csrf-rejected':
      await refreshThenRetrySameAttemptOnce(
        ctx,
        resumeId,
        queueItem,
        command,
        attempt,
      );
      return;
    case 'session-lost':
      stopForSessionLoss(ctx, resumeId);
      return;
    case 'validation-rejected':
      ctx.deps.store.setIssues(resumeId, queueItem.id, result.issues);
      if (markTemplateChildFailed(ctx, resumeId, queueItem)) return;
      holdFailed(
        ctx.deps.store,
        resumeId,
        queueItem,
        command,
        attempt,
        'request_invalid',
      );
      return;
    case 'rate-limited':
      ctx.deps.store.holdAttempt(resumeId, {
        kind: 'retry-later',
        queueItem,
        command,
        attempt,
        reason: 'rate-limited',
        retryAfterMs: result.retryAfterMs,
      });
      return;
    case 'media-busy':
      ctx.deps.store.holdAttempt(resumeId, {
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
        ctx.deps.store,
        resumeId,
        queueItem,
        command,
        attempt,
        'idempotency-reuse',
      );
      await refreshAndReconcile(ctx, resumeId);
      return;
    case 'rejected':
      if (markTemplateChildFailed(ctx, resumeId, queueItem)) return;
      holdFailed(
        ctx.deps.store,
        resumeId,
        queueItem,
        command,
        attempt,
        result.code,
      );
      return;
    case 'unknown':
      await resolveUnknown(
        ctx,
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
  ctx: CoordinatorContext,
  resumeId: string,
  command: AtomicEditorCommand,
  accepted: AcceptedResume,
): void {
  const adoption = ctx.deps.store.adoptComplete(resumeId, accepted);
  if (adoption.kind === 'adopted') {
    ctx.deps.store.dropHead(resumeId, command.id);
    return;
  }
  if (compareRevision(accepted.revision, adoption.winner.revision) === 0) {
    ctx.deps.store.resolveConflict(resumeId, command.id);
    ctx.deps.store.dropHead(resumeId, command.id);
    return;
  }
  const decision = reconcileCommand(command, adoption.winner);
  if (decision.kind === 'satisfied') {
    ctx.deps.store.dropHead(resumeId, command.id);
    return;
  }
  ctx.deps.store.markConflict(
    resumeId,
    createSupersededConflict(command, accepted, adoption.winner),
  );
  ctx.deps.store.dropHead(resumeId, command.id);
}

function markTemplateChildFailed(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
): boolean {
  if (queueItem.kind !== 'templateGroup') return false;
  const record = ctx.deps.store.recordFor(resumeId);
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
  ctx.deps.store.setTemplateState(resumeId, {
    kind: 'partial',
    accepted: record.accepted,
    nextChild: state.nextChild,
    reason: 'child-failed',
  });
  ctx.deps.store.continueTemplateGroup(resumeId, queueItem.id);
  return true;
}

async function refreshThenRetrySameAttemptOnce(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
): Promise<void> {
  const key = `${resumeId}:${attempt.idempotencyKey}`;
  if (ctx.csrfRetries.has(key)) {
    holdFailed(
      ctx.deps.store,
      resumeId,
      queueItem,
      command,
      attempt,
      'csrf-rejected',
    );
    return;
  }
  ctx.csrfRetries.add(key);
  await ctx.deps.auth.refresh();
  const csrfToken = dispatchToken(ctx, resumeId, queueItem.ownerId);
  if (csrfToken === null) return;
  await settle(
    ctx,
    resumeId,
    queueItem,
    command,
    attempt,
    await ctx.deps.api.dispatch(attempt, csrfToken),
  );
}

export async function resolveUnknown(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  reason: 'transport' | 'server',
): Promise<void> {
  if (
    attempt.automaticReplays === 0
    && ctx.deps.runtime.nowEpochMs() < attempt.retryCutoff
  ) {
    const replay = { ...attempt, automaticReplays: 1 as const };
    ctx.deps.store.holdAttempt(resumeId, {
      kind: 'unknown',
      queueItem,
      command,
      attempt: replay,
      reason,
    });
    await ctx.deps.runtime.delay(UNKNOWN_REPLAY_DELAY_MS);
    const csrfToken = dispatchToken(ctx, resumeId, queueItem.ownerId);
    if (csrfToken === null) return;
    if (ctx.deps.runtime.nowEpochMs() >= replay.retryCutoff) {
      await resolveUnknown(ctx, resumeId, queueItem, command, replay, reason);
      return;
    }
    await settle(
      ctx,
      resumeId,
      queueItem,
      command,
      replay,
      await ctx.deps.api.dispatch(replay, csrfToken),
    );
    return;
  }
  ctx.deps.store.holdAttempt(resumeId, {
    kind: 'unknown',
    queueItem,
    command,
    attempt,
    reason:
      ctx.deps.runtime.nowEpochMs() >= attempt.retryCutoff ? 'cutoff' : reason,
  });
  if (ctx.deps.runtime.nowEpochMs() < attempt.retryCutoff) return;
  if (command.kind === 'photoUpload') {
    await setOpaquePhoto(ctx, resumeId, command, attempt);
    return;
  }
  await refreshAndReconcile(ctx, resumeId);
}

async function settleStale(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  winner: {
    readonly document: AcceptedResume['document'];
    readonly revision: AcceptedResume['revision'];
  },
): Promise<void> {
  const record = ctx.deps.store.recordFor(resumeId);
  if (record === undefined || attempt.staleRebases === 1) {
    holdFailed(
      ctx.deps.store,
      resumeId,
      queueItem,
      command,
      attempt,
      'second-stale',
    );
    return;
  }
  let snapshot: AcceptedResume;
  if (command.kind === 'metadataField' || command.kind === 'resumeDelete') {
    const complete = await ctx.deps.api.read(resumeId);
    if (complete.kind === 'session-lost') {
      stopForSessionLoss(ctx, resumeId);
      return;
    }
    if (complete.kind === 'rate-limited') {
      ctx.deps.store.holdAttempt(resumeId, {
        kind: 'retry-later',
        queueItem,
        command,
        attempt,
        reason: 'rate-limited',
        retryAfterMs: complete.retryAfterMs,
      });
      return;
    }
    if (complete.kind !== 'complete') {
      ctx.deps.store.holdAttempt(resumeId, {
        kind: 'unknown',
        queueItem,
        command,
        attempt,
        reason: 'server',
      });
      return;
    }
    const adoption = ctx.deps.store.adoptComplete(resumeId, complete.accepted);
    snapshot = adoption.kind === 'adopted'
      ? adoption.accepted
      : adoption.winner;
  } else {
    snapshot = compareRevision(winner.revision, record.accepted.revision) <= 0
      ? record.accepted
      : {
          document: winner.document,
          revision: winner.revision,
          metadata: record.accepted.metadata,
          metadataFreshness: 'stale',
        };
    if (snapshot !== record.accepted) {
      ctx.deps.store.adoptStaleWinner(resumeId, snapshot);
    }
  }
  const decision = queueItem.kind === 'templateGroup'
    && (command.kind === 'structure' || command.kind === 'customization')
    ? reconcileTemplateChild(command, snapshot)
    : reconcileCommand(command, snapshot);
  if (decision.kind === 'satisfied') {
    if (queueItem.kind === 'templateGroup') {
      settleTemplateComplete(ctx, resumeId, queueItem, snapshot);
      return;
    }
    ctx.deps.store.dropHead(resumeId, queueItem.id);
    return;
  }
  if (decision.kind === 'conflict') {
    ctx.deps.store.markConflict(resumeId, decision.conflict);
    return;
  }
  const rebased = {
    ...freezeAttempt(command, snapshot, ctx.deps.runtime),
    staleRebases: 1 as const,
  };
  ctx.deps.store.holdAttempt(resumeId, {
    kind: 'unknown',
    queueItem,
    command,
    attempt: rebased,
    reason: 'server',
  });
  const csrfToken = dispatchToken(ctx, resumeId, queueItem.ownerId);
  if (csrfToken === null) return;
  await settle(
    ctx,
    resumeId,
    queueItem,
    command,
    rebased,
    await ctx.deps.api.dispatch(rebased, csrfToken),
  );
}

async function setOpaquePhoto(
  ctx: CoordinatorContext,
  resumeId: string,
  command: Extract<AtomicEditorCommand, { kind: 'photoUpload' }>,
  attempt: FrozenAttempt,
): Promise<void> {
  const before
    = ctx.deps.store.recordFor(resumeId)?.accepted.document.personalDetails
      .photo?.key;
  const result = await ctx.deps.api.read(resumeId);
  let observed: 'unchanged' | 'changed' | 'unavailable' = 'unavailable';
  if (result.kind === 'complete') {
    observed
      = result.accepted.document.personalDetails.photo?.key === before
        ? 'unchanged'
        : 'changed';
    ctx.deps.store.adoptComplete(resumeId, result.accepted);
  }
  ctx.deps.store.setOpaquePhotoOutcome(resumeId, {
    kind: 'photo-cutoff',
    command,
    attempt,
    observed,
  });
}
