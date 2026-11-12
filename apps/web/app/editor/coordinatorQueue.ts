import { toRaw } from 'vue';
import type { AtomicEditorCommand } from './commands';
import { reconcileCommand, reconcileTemplateGroup } from './reconcile';
import { freezeAttempt } from './resumeApi';
import { nextTemplateChild, type EditorQueueItem } from './templateGroup';
import {
  canDrain,
  DEBOUNCE_MS,
  dependenciesAcknowledged,
  dispatchToken,
  READ_RETRY_FIRST_MS,
  READ_RETRY_MAX_MS,
  readBarrierDue,
  stopForSessionLoss,
} from './coordinatorGuards';
import { dropChangedRefresh } from './coordinatorProject';
import { adoptRead } from './coordinatorReconcile';
import { resolveUnknown, settle } from './coordinatorSettle';
import type { CoordinatorContext } from './coordinatorTypes';

export function schedule(
  ctx: CoordinatorContext,
  resumeId: string,
  delayMs?: number,
): void {
  if (ctx.stopped.has(resumeId)) return;
  const prior = ctx.timers.get(resumeId);
  if (prior !== undefined) clearTimeout(prior);
  const requested
    = delayMs ?? (deliberateAction(ctx, resumeId) ? 0 : DEBOUNCE_MS);
  ctx.timers.set(
    resumeId,
    setTimeout(() => {
      ctx.timers.delete(resumeId);
      void flush(ctx, resumeId);
    }, Math.max(requested, retryWait(ctx, resumeId))),
  );
}

/** Time left before a failed completing read may run again. */
function retryWait(ctx: CoordinatorContext, resumeId: string): number {
  const retry = ctx.readRetries.get(resumeId);
  if (
    retry === undefined
    || ctx.deps.store.recordFor(resumeId)?.completeReadRequired !== true
  ) {
    return 0;
  }
  return Math.max(0, retry.at - Date.now());
}

/** A link or refresh is one chosen action, not typing, so it goes at once. */
function deliberateAction(ctx: CoordinatorContext, resumeId: string): boolean {
  const last = ctx.deps.store.recordFor(resumeId)?.pending.at(-1);
  return last?.kind === 'projectLink' || last?.kind === 'projectRefresh';
}

export function flush(
  ctx: CoordinatorContext,
  resumeId: string,
): Promise<void> {
  const active = ctx.drains.get(resumeId);
  if (active !== undefined) return active;
  const drain = drainResume(ctx, resumeId).finally(() =>
    ctx.drains.delete(resumeId),
  );
  ctx.drains.set(resumeId, drain);
  return drain;
}

async function drainResume(
  ctx: CoordinatorContext,
  resumeId: string,
): Promise<void> {
  for (;;) {
    if (ctx.stopped.has(resumeId)) return;
    const record = ctx.deps.store.recordFor(resumeId);
    const authState = ctx.deps.auth.authState.value;
    const ownerId = ctx.deps.auth.user.value?.id;
    if (record?.completeReadRequired === true) {
      // Nothing is sent over an unread revision: the queue reads first.
      if (
        !readBarrierDue(record, authState, ownerId)
        || !(await completeReadBarrier(ctx, resumeId))
      ) {
        return;
      }
      continue;
    }
    ctx.readRetries.delete(resumeId);
    if (!canDrain(record, authState, ownerId)) return;
    const queueItem = toRaw(record.pending[0]!);
    if (!dependenciesAcknowledged(record, queueItem)) return;
    if (queueItem.kind === 'templateGroup') {
      if (record.templateState === null) {
        ctx.deps.store.setTemplateState(resumeId, {
          kind: 'queued',
          nextChild: 0,
        });
        continue;
      }
      if (record.templateState.kind === 'partial') return;
      if (record.templateState.kind === 'complete') {
        ctx.deps.store.dropHead(resumeId, queueItem.id);
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
          ctx.deps.store.setTemplateState(resumeId, null);
          ctx.deps.store.dropHead(resumeId, queueItem.id);
          continue;
        }
        if (groupDecision.kind === 'conflict') {
          ctx.deps.store.markConflict(resumeId, groupDecision.conflict);
          return;
        }
      }
      const command = nextTemplateChild(queueItem, record.templateState);
      if (command === null) return;
      if (!(await dispatchCommand(ctx, resumeId, queueItem, command))) return;
      continue;
    }
    const decision = reconcileCommand(queueItem, record.accepted);
    if (decision.kind === 'satisfied') {
      ctx.deps.store.dropHead(resumeId, queueItem.id);
      continue;
    }
    if (decision.kind === 'conflict') {
      if (dropChangedRefresh(ctx, resumeId, queueItem, record.accepted)) {
        continue;
      }
      ctx.deps.store.markConflict(resumeId, decision.conflict);
      return;
    }
    if (!(await dispatchCommand(ctx, resumeId, queueItem, queueItem))) return;
  }
}

async function dispatchCommand(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
): Promise<boolean> {
  const record = ctx.deps.store.recordFor(resumeId);
  let csrfToken = dispatchToken(ctx, resumeId, queueItem.ownerId);
  if (record !== undefined && csrfToken === null && !record.sessionLost) {
    try {
      await ctx.deps.auth.refresh();
    } catch {
      return false;
    }
    csrfToken = dispatchToken(ctx, resumeId, queueItem.ownerId);
  }
  if (record === undefined || csrfToken === null) return false;
  const attempt = freezeAttempt(command, record.accepted, ctx.deps.runtime);
  ctx.deps.store.startAttempt(resumeId, queueItem, command, attempt);
  await settle(
    ctx,
    resumeId,
    queueItem,
    command,
    attempt,
    await ctx.deps.api.dispatch(attempt, csrfToken),
  );
  return true;
}

export async function retry(
  ctx: CoordinatorContext,
  resumeId: string,
  commandId: string,
): Promise<void> {
  const active = ctx.deps.store.recordFor(resumeId)?.attempt;
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
  if (ctx.deps.runtime.nowEpochMs() >= attempt.retryCutoff) {
    await resolveUnknown(
      ctx,
      resumeId,
      queueItem,
      command,
      attempt,
      'transport',
    );
  } else {
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
  // A retry settles outside the drain, so the drain resumes the queue and
  // completes any read barrier the settlement left.
  await flush(ctx, resumeId);
}

export async function resumeAfterAuth(
  ctx: CoordinatorContext,
  resumeId: string,
): Promise<void> {
  await ctx.deps.auth.refresh();
  const record = ctx.deps.store.recordFor(resumeId);
  const ownerId = ctx.deps.auth.user.value?.id;
  const retainedOwner
    = record?.attempt?.queueItem.ownerId ?? record?.pending[0]?.ownerId;
  if (
    record === undefined
    || ownerId === undefined
    || ctx.deps.auth.authState.value !== 'authenticated'
    || (retainedOwner !== undefined && retainedOwner !== ownerId)
  ) {
    return;
  }
  const result = await ctx.deps.api.read(resumeId);
  if (result.kind !== 'complete') return;
  adoptRead(ctx, resumeId, result.accepted);
  ctx.deps.store.clearSessionLost(resumeId);
  const active = ctx.deps.store.recordFor(resumeId)?.attempt;
  if (active !== null && active !== undefined) {
    await retry(ctx, resumeId, active.command.id);
  } else {
    schedule(ctx, resumeId);
  }
}

/**
 * Reads the complete resume to clear the read barrier, which marks a resume
 * the editor has not read in full: a 412 winner, a bodyless 204, or a
 * dropped project command. A read that leaves it set is retried on the
 * queue's own timer, 1 s after the first failure and doubling to 30 s, or
 * after a longer Retry-After; no schedule or flush runs it sooner. A lost
 * session waits for sign-in, whose read completes it; a missing resume ends
 * the retries.
 */
export async function completeReadBarrier(
  ctx: CoordinatorContext,
  resumeId: string,
): Promise<boolean> {
  if (ctx.stopped.has(resumeId)) return false;
  const wait = retryWait(ctx, resumeId);
  if (wait > 0) {
    schedule(ctx, resumeId, wait);
    return false;
  }
  const result = await ctx.deps.api.read(resumeId);
  if (result.kind === 'session-lost') {
    stopForSessionLoss(ctx, resumeId);
    return false;
  }
  if (result.kind === 'complete') {
    ctx.deps.store.adoptCompleteRead(resumeId, result.accepted);
  }
  if (!ctx.deps.store.recordFor(resumeId)?.completeReadRequired) {
    ctx.readRetries.delete(resumeId);
    return true;
  }
  if (
    ctx.stopped.has(resumeId)
    || (result.kind !== 'complete'
      && result.kind !== 'failed'
      && result.kind !== 'rate-limited')
  ) {
    return false;
  }
  const failures = ctx.readRetries.get(resumeId)?.failures ?? 0;
  const backoff = Math.min(
    READ_RETRY_FIRST_MS * 2 ** failures,
    READ_RETRY_MAX_MS,
  );
  const delay = result.kind === 'rate-limited'
    ? Math.max(result.retryAfterMs ?? 0, backoff)
    : backoff;
  ctx.readRetries.set(resumeId, {
    failures: failures + 1,
    at: Date.now() + delay,
  });
  schedule(ctx, resumeId, delay);
  return false;
}

/**
 * Ends this coordinator's work on a resume whose editor closed: no timer,
 * read or write starts for it again, though a request already sent
 * completes. Unsent edits stay behind until the resume is opened again,
 * which reads it afresh and drops them; leaving with them warns first.
 */
export function stop(ctx: CoordinatorContext, resumeId: string): void {
  ctx.stopped.add(resumeId);
  ctx.readRetries.delete(resumeId);
  const timer = ctx.timers.get(resumeId);
  if (timer !== undefined) clearTimeout(timer);
  ctx.timers.delete(resumeId);
}
