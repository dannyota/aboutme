import { toRaw } from 'vue';

import type { EditorQueueItem } from './templateGroup';
import {
  clearWriteRetry,
  dispatchToken,
  HELD_WRITE_ALERT_MS,
  retryDelay,
} from './coordinatorGuards';
import { schedule } from './coordinatorQueue';
import { resolveUnknown, send } from './coordinatorSettle';
import type {
  CoordinatorContext,
  OperationContext,
} from './coordinatorTypes';

// A write whose outcome is unknown is sent again unchanged: the same
// Idempotency-Key, If-Match and body, so a write that already landed replays
// its stored answer instead of running twice (ADR 0006). Retries follow the
// schedule in docs/design/budgets.md until the attempt's retry cutoff, where
// the read-and-reconcile path takes over. The person is told once the write
// has been failing for HELD_WRITE_ALERT_MS.

/** Starts the alert clock at a write's first unknown outcome. */
export function startWriteRetry(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
): void {
  if (ctx.writeRetries.has(resumeId)) return;
  ctx.writeRetries.set(resumeId, {
    failures: 0,
    at: 0,
    alert: setTimeout(() => {
      ctx.deps.store.markHeldAlert(resumeId, queueItem.id);
    }, HELD_WRITE_ALERT_MS),
  });
}

/** Arms the queue timer for the next retry of the held write. */
export function scheduleWriteRetry(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  atLeastMs = 0,
): void {
  startWriteRetry(ctx, resumeId, queueItem);
  const retry = ctx.writeRetries.get(resumeId)!;
  const delay = Math.max(retryDelay(retry.failures), atLeastMs);
  retry.failures += 1;
  retry.at = Date.now() + delay;
  schedule(ctx, resumeId, delay);
}

/**
 * Sends the held write again once its retry is due. Returns whether the
 * queue may look at the record again at once.
 */
export async function retryHeldWrite(
  ctx: OperationContext,
  resumeId: string,
): Promise<boolean> {
  const held = ctx.deps.store.recordFor(resumeId)?.attempt;
  if (held?.kind !== 'unknown' || held.reason === 'cutoff') return false;
  const wait = (ctx.writeRetries.get(resumeId)?.at ?? 0) - Date.now();
  if (wait > 0) {
    schedule(ctx, resumeId, wait);
    return false;
  }
  const queueItem = toRaw(held.queueItem);
  const command = toRaw(held.command);
  const attempt = toRaw(held.attempt);
  if (ctx.deps.runtime.nowEpochMs() >= attempt.retryCutoff) {
    clearWriteRetry(ctx, resumeId);
    await resolveUnknown(
      ctx,
      resumeId,
      queueItem,
      command,
      attempt,
      held.reason,
    );
    return true;
  }
  const csrfToken = dispatchToken(ctx, resumeId, queueItem.ownerId);
  if (csrfToken === null) return false;
  await send(ctx, resumeId, queueItem, command, attempt, csrfToken);
  return true;
}
