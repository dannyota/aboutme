import { toRaw } from 'vue';
import type { ResumeRecord } from '../stores/resumes';
import type { AttemptState } from '../stores/resumeRecord';
import type { AtomicEditorCommand } from './commands';
import { reconcileCommand, reconcileTemplateGroup } from './reconcile';
import { freezeAttempt } from './resumeApi';
import { nextTemplateChild, type EditorQueueItem } from './templateGroup';
import {
  begin,
  canDrain,
  clearWriteRetry,
  current,
  DEBOUNCE_MS,
  dependenciesAcknowledged,
  dispatchToken,
  heldConflict,
  owns,
  readBarrierDue,
  retryDelay,
  stopForSessionLoss,
} from './coordinatorGuards';
import { dropChangedRefresh } from './coordinatorProject';
import { adoptRead, reconcileHeld } from './coordinatorReconcile';
import {
  nextForHeld,
  rebaseHeld,
  requestRetry,
  startResend,
  waitsForPerson,
} from './coordinatorRetry';
import { send } from './coordinatorSettle';
import type {
  CoordinatorContext,
  OperationContext,
} from './coordinatorTypes';

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
  const drain = drainResume(begin(ctx, resumeId), resumeId).finally(() =>
    ctx.drains.delete(resumeId),
  );
  ctx.drains.set(resumeId, drain);
  return drain;
}

/**
 * The resume's only sender. Each pass decides in one place, in this order:
 * 1. the epoch: a closed editor or a resume opened again ends the drain;
 * 2. the head waits for the person: a conflict, or a held write only the
 *    person may send again;
 * 3. the read barrier: nothing is sent over an unread revision, so the read
 *    completes first, and a held write is reconciled against it before it
 *    is sent again; only a rebase built from a 412 winner skips it;
 * 4. the held write's retry deadline, Retry-After included;
 * 5. the send: the held write unchanged, or the head of the queue.
 * Settlement, timers and buttons only change state and wake the drain.
 */
async function drainResume(
  ctx: OperationContext,
  resumeId: string,
): Promise<void> {
  for (;;) {
    if (!current(ctx, resumeId)) return;
    const record = ctx.deps.store.recordFor(resumeId);
    if (record === undefined || record.sessionLost) return;
    const held = record.attempt === null ? null : toRaw(record.attempt);
    if (held !== null && heldConflict(record)) {
      clearWriteRetry(ctx, resumeId);
      return;
    }
    if (held !== null && waitsForPerson(ctx, resumeId, held)) return;
    if (held === null && queuedConflict(record)) {
      const next = reconcileHead(ctx, resumeId, record);
      if (next === 'stop') return;
      if (next === 'again') continue;
    }
    if (record.completeReadRequired && !rebaseHeld(ctx, resumeId, held)) {
      if (
        !readBarrierDue(
          record,
          ctx.deps.auth.authState.value,
          ctx.deps.auth.user.value?.id,
        )
        || !(await completeReadBarrier(ctx, resumeId))
      ) {
        return;
      }
      reconcileHeld(ctx, resumeId, record.accepted);
      continue;
    }
    ctx.readRetries.delete(resumeId);
    if (record.opaquePhotoOutcome !== null) return;
    if (held !== null) {
      const next = await nextForHeld(ctx, resumeId, held);
      if (next === 'wait') return;
      if (next === 'again') continue;
      if (!(await resend(ctx, resumeId, held))) return;
      continue;
    }
    clearWriteRetry(ctx, resumeId);
    if (!(await sendHead(ctx, resumeId, record))) return;
  }
}

/** The queue's head, not sent yet, has a conflict recorded. */
function queuedConflict(record: ResumeRecord): boolean {
  const head = record.pending[0];
  return head !== undefined
    && record.conflicts.some(({ id }) => id === head.id);
}

/**
 * Settles the queue's head against what the editor last accepted, before it
 * is sent: one the resume already holds leaves the queue, a conflict stops
 * the queue for the person, and a refresh over changed text is dropped. A
 * recorded conflict is reconciled again, so it follows later typing.
 */
function reconcileHead(
  ctx: OperationContext,
  resumeId: string,
  record: ResumeRecord,
): 'send' | 'again' | 'stop' {
  const queueItem = toRaw(record.pending[0]!);
  if (queueItem.kind === 'templateGroup') {
    if (record.templateState === null) {
      ctx.deps.store.setTemplateState(resumeId, {
        kind: 'queued',
        nextChild: 0,
      });
      return 'again';
    }
    if (record.templateState.kind === 'partial') return 'stop';
    if (record.templateState.kind === 'complete') {
      ctx.deps.store.dropHead(resumeId, queueItem.id);
      return 'again';
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
        return 'again';
      }
      if (groupDecision.kind === 'conflict') {
        ctx.deps.store.markConflict(resumeId, groupDecision.conflict);
        return 'stop';
      }
    }
    return 'send';
  }
  const decision = reconcileCommand(queueItem, record.accepted);
  if (decision.kind === 'satisfied') {
    ctx.deps.store.dropHead(resumeId, queueItem.id);
    return 'again';
  }
  if (decision.kind === 'conflict') {
    if (dropChangedRefresh(ctx, resumeId, queueItem, record.accepted)) {
      return 'again';
    }
    ctx.deps.store.markConflict(resumeId, decision.conflict);
    return 'stop';
  }
  return 'send';
}

/** Step 5 for the queue's head. Returns whether the drain looks again. */
async function sendHead(
  ctx: OperationContext,
  resumeId: string,
  record: ResumeRecord,
): Promise<boolean> {
  if (
    !canDrain(
      record,
      ctx.deps.auth.authState.value,
      ctx.deps.auth.user.value?.id,
    )
  ) {
    return false;
  }
  const queueItem = toRaw(record.pending[0]!);
  if (!dependenciesAcknowledged(record, queueItem)) return false;
  const next = reconcileHead(ctx, resumeId, record);
  if (next !== 'send') return next === 'again';
  const state = record.templateState;
  let command: AtomicEditorCommand | null = null;
  if (queueItem.kind !== 'templateGroup') {
    command = queueItem;
  } else if (state?.kind === 'queued' || state?.kind === 'running') {
    command = nextTemplateChild(queueItem, state);
  }
  if (command === null) return false;
  return dispatchCommand(ctx, resumeId, queueItem, command);
}

/** A CSRF token for the owner's write, refreshed once if there is none. */
async function csrfFor(
  ctx: OperationContext,
  resumeId: string,
  ownerId: string,
): Promise<string | null> {
  const token = dispatchToken(ctx, resumeId, ownerId);
  const record = ctx.deps.store.recordFor(resumeId);
  if (token !== null || record === undefined || record.sessionLost) {
    return token;
  }
  try {
    await ctx.deps.auth.refresh();
  } catch {
    return null;
  }
  if (!current(ctx, resumeId)) return null;
  return dispatchToken(ctx, resumeId, ownerId);
}

async function dispatchCommand(
  ctx: OperationContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
): Promise<boolean> {
  const csrfToken = await csrfFor(ctx, resumeId, queueItem.ownerId);
  const record = ctx.deps.store.recordFor(resumeId);
  if (record === undefined || csrfToken === null) return false;
  const attempt = freezeAttempt(command, record.accepted, ctx.deps.runtime);
  ctx.deps.store.startAttempt(resumeId, queueItem, command, attempt);
  if (!owns(ctx, resumeId, attempt)) return false;
  await send(ctx, resumeId, queueItem, command, attempt, csrfToken);
  return true;
}

/**
 * Step 5 for the held write: sends it again unchanged, marked as on its way
 * so its retry button waits. Returns whether the drain looks again.
 */
async function resend(
  ctx: OperationContext,
  resumeId: string,
  held: AttemptState,
): Promise<boolean> {
  const csrfToken = await csrfFor(ctx, resumeId, held.queueItem.ownerId);
  if (csrfToken === null || !owns(ctx, resumeId, held.attempt)) return false;
  const key = held.attempt.idempotencyKey;
  startResend(ctx, resumeId, held.queueItem.id);
  ctx.deps.store.markSending(resumeId, key, true);
  try {
    await send(
      ctx,
      resumeId,
      held.queueItem,
      held.command,
      held.attempt,
      csrfToken,
    );
  } finally {
    if (current(ctx, resumeId)) {
      ctx.deps.store.markSending(resumeId, key, false);
    }
  }
  return true;
}

/**
 * Asks for the held write `commandId` to go out again: Retry now, Try
 * again, or a photo retry. It only changes the schedule; the drain sends.
 */
export function retry(
  ctx: OperationContext,
  resumeId: string,
  commandId: string,
): Promise<void> {
  const held = ctx.deps.store.recordFor(resumeId)?.attempt;
  if (held === null || held === undefined || held.command.id !== commandId) {
    return Promise.resolve();
  }
  requestRetry(ctx, resumeId, toRaw(held));
  return flush(ctx, resumeId);
}

export async function resumeAfterAuth(
  ctx: OperationContext,
  resumeId: string,
): Promise<void> {
  await ctx.deps.auth.refresh();
  if (!current(ctx, resumeId)) return;
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
  if (!current(ctx, resumeId) || result.kind !== 'complete') return;
  adoptRead(ctx, resumeId, result.accepted);
  ctx.deps.store.clearSessionLost(resumeId);
  const held = ctx.deps.store.recordFor(resumeId)?.attempt;
  if (held !== null && held !== undefined) {
    requestRetry(ctx, resumeId, toRaw(held));
    await flush(ctx, resumeId);
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
  ctx: OperationContext,
  resumeId: string,
): Promise<boolean> {
  if (!current(ctx, resumeId)) return false;
  const wait = retryWait(ctx, resumeId);
  if (wait > 0) {
    schedule(ctx, resumeId, wait);
    return false;
  }
  const result = await ctx.deps.api.read(resumeId);
  if (!current(ctx, resumeId)) return false;
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
    result.kind !== 'complete'
    && result.kind !== 'failed'
    && result.kind !== 'rate-limited'
  ) {
    return false;
  }
  const failures = ctx.readRetries.get(resumeId)?.failures ?? 0;
  const backoff = retryDelay(failures);
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
  clearWriteRetry(ctx, resumeId);
  const timer = ctx.timers.get(resumeId);
  if (timer !== undefined) clearTimeout(timer);
  ctx.timers.delete(resumeId);
}
