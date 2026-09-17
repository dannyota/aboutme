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
} from './coordinatorGuards';
import { resolveUnknown, settle } from './coordinatorSettle';
import type { CoordinatorContext } from './coordinatorTypes';

export function schedule(ctx: CoordinatorContext, resumeId: string): void {
  const prior = ctx.timers.get(resumeId);
  if (prior !== undefined) clearTimeout(prior);
  ctx.timers.set(
    resumeId,
    setTimeout(() => {
      ctx.timers.delete(resumeId);
      void flush(ctx, resumeId);
    }, DEBOUNCE_MS),
  );
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
    const record = ctx.deps.store.recordFor(resumeId);
    if (
      !canDrain(
        record,
        ctx.deps.auth.authState.value,
        ctx.deps.auth.user.value?.id,
      )
    ) {
      return;
    }
    if (record.completeReadRequired) return;
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
    return;
  }
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
  ctx.deps.store.adoptComplete(resumeId, result.accepted);
  ctx.deps.store.clearSessionLost(resumeId);
  const active = ctx.deps.store.recordFor(resumeId)?.attempt;
  if (active !== null && active !== undefined) {
    await retry(ctx, resumeId, active.command.id);
  } else {
    schedule(ctx, resumeId);
  }
}
