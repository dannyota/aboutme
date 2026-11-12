import type { AtomicEditorCommand } from './commands';
import type { AttemptResult, FrozenAttempt } from './attempt';
import { createSupersededConflict, reconcileCommand } from './reconcile';
import { compareRevision } from './revision';
import { freezeAttempt } from './resumeApi';
import { reconcileTemplateChild, type EditorQueueItem } from './templateGroup';
import type { AcceptedResume } from './types';
import {
  assertNever,
  clearWriteRetry,
  holdFailed,
  owns,
  retryDelay,
  STALE_REBASE_LIMIT,
  stopForSessionLoss,
  UNKNOWN_REPLAY_DELAY_MS,
} from './coordinatorGuards';
import {
  askAgain,
  backOff,
  startWriteRetry,
  waitBriefly,
  waitForServer,
} from './coordinatorRetry';
import {
  adoptRead,
  refreshAndReconcile,
  settleTemplateComplete,
} from './coordinatorReconcile';
import type {
  CoordinatorContext,
  OperationContext,
} from './coordinatorTypes';
import {
  dropRefusedProjectCommand,
  settleStaleProject,
} from './coordinatorProject';

/**
 * Sends `attempt` and settles its answer; only the drain calls it. An answer
 * that comes back after the queue stopped holding that attempt is ignored.
 * Settling never sends: it leaves the write held, scheduled or gone, and the
 * drain decides what goes next.
 */
export async function send(
  ctx: OperationContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  csrfToken: string,
): Promise<void> {
  const result = await ctx.deps.api.dispatch(attempt, csrfToken);
  if (!owns(ctx, resumeId, attempt)) return;
  await settle(ctx, resumeId, queueItem, command, attempt, result);
}

async function settle(
  ctx: OperationContext,
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
      await refreshForSameAttempt(ctx, resumeId, queueItem, command, attempt);
      return;
    case 'session-lost':
      stopForSessionLoss(ctx, resumeId);
      return;
    case 'validation-rejected':
      if (dropRefusedProjectCommand(ctx, resumeId, queueItem)) return;
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
    case 'media-busy':
      holdForLater(
        ctx,
        resumeId,
        queueItem,
        command,
        attempt,
        result.kind,
        result.retryAfterMs,
      );
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
      if (dropRefusedProjectCommand(ctx, resumeId, queueItem, result.code)) {
        return;
      }
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

/**
 * A rate limit or busy media. A write whose outcome was already unknown
 * keeps recovering, unchanged, after the server's Retry-After; a write
 * refused before it ran waits there until the person retries it.
 */
function holdForLater(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  reason: 'rate-limited' | 'media-busy',
  retryAfterMs: number | null,
): void {
  if (attempt.automaticReplays > 0) {
    ctx.deps.store.holdAttempt(resumeId, {
      kind: 'unknown',
      queueItem,
      command,
      attempt,
      reason: 'server',
    });
    backOff(ctx, resumeId, queueItem.id, retryAfterMs ?? 0);
    return;
  }
  ctx.deps.store.holdAttempt(resumeId, {
    kind: 'retry-later',
    queueItem,
    command,
    attempt,
    reason,
    retryAfterMs,
  });
  waitForServer(ctx, resumeId, queueItem.id, retryAfterMs ?? 0);
}

/**
 * Refreshes the CSRF token once per attempt; the drain then sends the same
 * attempt again with the new token. A second rejection holds it.
 */
async function refreshForSameAttempt(
  ctx: OperationContext,
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
  // A write the person asked to send stays asked for.
  if (owns(ctx, resumeId, attempt)) askAgain(ctx, resumeId, queueItem.id);
}

/**
 * Holds a write whose outcome is unknown. Inside its retry window it is
 * replayed once after UNKNOWN_REPLAY_DELAY_MS, then backed off; past the
 * window the resume is read and the write reconciled against it.
 */
export async function resolveUnknown(
  ctx: OperationContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  reason: 'transport' | 'server',
): Promise<void> {
  if (ctx.deps.runtime.nowEpochMs() < attempt.retryCutoff) {
    const replay = attempt.automaticReplays === 0;
    ctx.deps.store.holdAttempt(resumeId, {
      kind: 'unknown',
      queueItem,
      command,
      attempt: replay ? { ...attempt, automaticReplays: 1 } : attempt,
      reason,
    });
    startWriteRetry(ctx, resumeId, queueItem.id);
    if (replay) {
      waitBriefly(ctx, resumeId, queueItem.id, UNKNOWN_REPLAY_DELAY_MS);
    } else {
      backOff(ctx, resumeId, queueItem.id);
    }
    return;
  }
  ctx.deps.store.holdAttempt(resumeId, {
    kind: 'unknown',
    queueItem,
    command,
    attempt,
    reason: 'cutoff',
  });
  clearWriteRetry(ctx, resumeId);
  if (command.kind === 'photoUpload') {
    await setOpaquePhoto(ctx, resumeId, queueItem, command, attempt);
    return;
  }
  await refreshAndReconcile(ctx, resumeId);
}

/**
 * Settles a 412 against the newer revision: a change it already holds
 * leaves the queue, a conflict waits for the person, and an unchanged base
 * is rebased with a new key, sent by the drain after 1, 2 and 4 s. Every
 * winner is reconciled; only a further rebase counts against the limit,
 * after which the rebased write waits for Try again.
 */
async function settleStale(
  ctx: OperationContext,
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
  if (record === undefined) return;
  let snapshot: AcceptedResume;
  if (command.kind === 'metadataField' || command.kind === 'resumeDelete') {
    const complete = await ctx.deps.api.read(resumeId);
    if (!owns(ctx, resumeId, attempt)) return;
    if (complete.kind === 'session-lost') {
      stopForSessionLoss(ctx, resumeId);
      return;
    }
    if (complete.kind !== 'complete') {
      // The write is held as unknown and sent again on the retry schedule;
      // its next 412 reads the resume again.
      ctx.deps.store.holdAttempt(resumeId, {
        kind: 'unknown',
        queueItem,
        command,
        attempt,
        reason: 'server',
      });
      backOff(
        ctx,
        resumeId,
        queueItem.id,
        complete.kind === 'rate-limited' ? complete.retryAfterMs ?? 0 : 0,
      );
      return;
    }
    const adoption = adoptRead(ctx, resumeId, complete.accepted);
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
  if (settleStaleProject(ctx, resumeId, queueItem, snapshot, decision)) {
    return;
  }
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
    staleRebases: attempt.staleRebases + 1,
  };
  if (attempt.staleRebases >= STALE_REBASE_LIMIT) {
    holdFailed(
      ctx.deps.store,
      resumeId,
      queueItem,
      command,
      { ...rebased, staleRebases: attempt.staleRebases },
      'stale-limit',
    );
    return;
  }
  ctx.deps.store.holdAttempt(resumeId, {
    kind: 'unknown',
    queueItem,
    command,
    attempt: rebased,
    reason: 'server',
  });
  // Rebases wait 1, 2 and 4 s, so revisions bumped in quick succession
  // settle before the write is sent over them again.
  waitBriefly(
    ctx,
    resumeId,
    queueItem.id,
    retryDelay(attempt.staleRebases),
    true,
  );
}

async function setOpaquePhoto(
  ctx: OperationContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: Extract<AtomicEditorCommand, { kind: 'photoUpload' }>,
  attempt: FrozenAttempt,
): Promise<void> {
  const before
    = ctx.deps.store.recordFor(resumeId)?.accepted.document.personalDetails
      .photo?.key;
  const result = await ctx.deps.api.read(resumeId);
  if (!owns(ctx, resumeId, attempt)) return;
  let observed: 'unchanged' | 'changed' | 'unavailable' = 'unavailable';
  if (result.kind === 'complete') {
    observed
      = result.accepted.document.personalDetails.photo?.key === before
        ? 'unchanged'
        : 'changed';
    adoptRead(ctx, resumeId, result.accepted);
  }
  ctx.deps.store.setOpaquePhotoOutcome(resumeId, {
    kind: 'photo-cutoff',
    command,
    attempt,
    observed,
  });
}
