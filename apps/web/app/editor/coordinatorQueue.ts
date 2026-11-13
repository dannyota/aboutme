import { toRaw } from 'vue';
import type { ResumeRecord } from '../stores/resumes';
import type { AttemptState } from '../stores/resumeRecord';
import type { AtomicEditorCommand } from './commands';
import { reconcileCommand, reconcileTemplateGroup } from './reconcile';
import { freezeAttempt } from './resumeApi';
import { nextTemplateChild } from './templateGroup';
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
import { dropConflictingProject } from './coordinatorProject';
import { reconcileHeld } from './coordinatorReconcile';
import {
  heldNext,
  rebaseHeld,
  requestRetry,
  startResend,
  waitsForPerson,
} from './coordinatorRetry';
import { resolveUnknown, send } from './coordinatorSettle';
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

function deliberateAction(ctx: CoordinatorContext, resumeId: string): boolean {
  const last = ctx.deps.store.recordFor(resumeId)?.pending.at(-1);
  return last?.kind === 'projectLink'
    || last?.kind === 'projectLinkEntry'
    || last?.kind === 'projectRefresh';
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

async function drainResume(
  ctx: OperationContext,
  resumeId: string,
): Promise<void> {
  let refreshed = false;
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
      const next = heldNext(ctx, resumeId, held);
      if (next.kind === 'wait') return;
      if (next.kind === 'sleep') {
        await ctx.deps.runtime.delay(next.ms);
        continue;
      }
      if (next.kind === 'cutoff') {
        await resolveUnknown(
          ctx,
          resumeId,
          held.queueItem,
          held.command,
          held.attempt,
          next.reason,
        );
        continue;
      }
    } else {
      clearWriteRetry(ctx, resumeId);
      if (
        !canDrain(
          record,
          ctx.deps.auth.authState.value,
          ctx.deps.auth.user.value?.id,
        )
      ) {
        return;
      }
    }
    const ownerId = held?.queueItem.ownerId ?? record.pending[0]!.ownerId;
    const csrfToken = dispatchToken(ctx, resumeId, ownerId);
    if (csrfToken === null) {
      if (
        refreshed
        || record.sessionLost
        || ctx.deps.auth.authState.value !== 'authenticated'
        || !(await refreshCsrf(ctx))
      ) {
        return;
      }
      refreshed = true;
      continue;
    }
    const sent = held === null
      ? await sendHead(ctx, resumeId, record, csrfToken)
      : await resend(ctx, resumeId, held, csrfToken);
    if (sent === 'stop') return;
    if (sent === 'sent') refreshed = false;
  }
}

function queuedConflict(record: ResumeRecord): boolean {
  const head = record.pending[0];
  return head !== undefined
    && record.conflicts.some(({ id }) => id === head.id);
}

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
    if (dropConflictingProject(ctx, resumeId, queueItem, record.accepted)) {
      return 'again';
    }
    ctx.deps.store.markConflict(resumeId, decision.conflict);
    return 'stop';
  }
  return 'send';
}

async function sendHead(
  ctx: OperationContext,
  resumeId: string,
  record: ResumeRecord,
  csrfToken: string,
): Promise<'sent' | 'again' | 'stop'> {
  const queueItem = toRaw(record.pending[0]!);
  if (!dependenciesAcknowledged(record, queueItem)) return 'stop';
  const next = reconcileHead(ctx, resumeId, record);
  if (next !== 'send') return next;
  const state = record.templateState;
  let command: AtomicEditorCommand | null = null;
  if (queueItem.kind !== 'templateGroup') {
    command = queueItem;
  } else if (state?.kind === 'queued' || state?.kind === 'running') {
    command = nextTemplateChild(queueItem, state);
  }
  if (command === null) return 'stop';
  const attempt = freezeAttempt(command, record.accepted, ctx.deps.runtime);
  ctx.deps.store.startAttempt(resumeId, queueItem, command, attempt);
  if (!owns(ctx, resumeId, attempt)) return 'again';
  await send(ctx, resumeId, queueItem, command, attempt, csrfToken);
  return 'sent';
}

async function refreshCsrf(ctx: OperationContext): Promise<boolean> {
  try {
    await ctx.deps.auth.refresh();
    return true;
  } catch {
    return false;
  }
}

async function resend(
  ctx: OperationContext,
  resumeId: string,
  held: AttemptState,
  csrfToken: string,
): Promise<'sent' | 'again'> {
  if (!owns(ctx, resumeId, held.attempt)) return 'again';
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
  return 'sent';
}

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
  await wake(ctx, resumeId, true);
}

export function wake(
  ctx: OperationContext,
  resumeId: string,
  recovered: boolean,
): Promise<void> {
  const record = ctx.deps.store.recordFor(resumeId);
  const ownerId = ctx.deps.auth.user.value?.id;
  const retainedOwner
    = record?.attempt?.queueItem.ownerId ?? record?.pending[0]?.ownerId;
  if (
    !current(ctx, resumeId)
    || record === undefined
    || ownerId === undefined
    || ctx.deps.auth.authState.value !== 'authenticated'
    || (retainedOwner !== undefined && retainedOwner !== ownerId)
  ) {
    return Promise.resolve();
  }
  if (recovered) {
    ctx.deps.store.clearSessionLost(resumeId);
    ctx.deps.store.requireCompleteRead(resumeId);
    const held = record.attempt;
    if (held !== null) requestRetry(ctx, resumeId, toRaw(held));
  }
  return flush(ctx, resumeId);
}

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

export function stop(ctx: CoordinatorContext, resumeId: string): void {
  ctx.stopped.add(resumeId);
  ctx.readRetries.delete(resumeId);
  clearWriteRetry(ctx, resumeId);
  const timer = ctx.timers.get(resumeId);
  if (timer !== undefined) clearTimeout(timer);
  ctx.timers.delete(resumeId);
}
