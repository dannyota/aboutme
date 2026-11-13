import type { AttemptState } from '../stores/resumeRecord';
import {
  clearWriteRetry,
  HELD_WRITE_ALERT_MS,
  retryDelay,
} from './coordinatorGuards';
import { schedule } from './coordinatorQueue';
import type {
  CoordinatorContext,
  OperationContext,
  WriteRetry,
} from './coordinatorTypes';

function writeRetryFor(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
): WriteRetry {
  const known = ctx.writeRetries.get(resumeId);
  if (known?.itemId === itemId) return known;
  clearWriteRetry(ctx, resumeId);
  const retry: WriteRetry = {
    itemId,
    failures: 0,
    at: 0,
    notBefore: 0,
    wait: 0,
    rebase: false,
    requested: false,
  };
  ctx.writeRetries.set(resumeId, retry);
  return retry;
}

function heldRetry(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
): WriteRetry | undefined {
  const retry = ctx.writeRetries.get(resumeId);
  return retry?.itemId === itemId ? retry : undefined;
}

export function startWriteRetry(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
): void {
  const retry = writeRetryFor(ctx, resumeId, itemId);
  retry.alert ??= setTimeout(() => {
    ctx.deps.store.markHeldAlert(resumeId, itemId);
  }, HELD_WRITE_ALERT_MS);
}

export function backOff(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
  retryAfterMs = 0,
): void {
  startWriteRetry(ctx, resumeId, itemId);
  const retry = writeRetryFor(ctx, resumeId, itemId);
  retry.at = Date.now() + retryDelay(retry.failures);
  retry.failures += 1;
  waitForServer(ctx, resumeId, itemId, retryAfterMs);
}

export function waitForServer(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
  retryAfterMs: number,
): void {
  const retry = writeRetryFor(ctx, resumeId, itemId);
  retry.notBefore = Math.max(retry.notBefore, Date.now() + retryAfterMs);
}

export function waitBriefly(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
  waitMs: number,
  rebase = false,
): void {
  const retry = writeRetryFor(ctx, resumeId, itemId);
  retry.wait = waitMs;
  retry.rebase = rebase;
  if (rebase) {
    retry.failures = 0;
    retry.at = 0;
  }
}

export function rebaseHeld(
  ctx: CoordinatorContext,
  resumeId: string,
  held: AttemptState | null,
): boolean {
  return held !== null
    && heldRetry(ctx, resumeId, held.queueItem.id)?.rebase === true;
}

export function requestRetry(
  ctx: CoordinatorContext,
  resumeId: string,
  held: AttemptState,
): void {
  if (held.kind === 'dispatching' || held.sending === true) return;
  askAgain(ctx, resumeId, held.queueItem.id);
  writeRetryFor(ctx, resumeId, held.queueItem.id).at = 0;
  if (held.kind === 'failed' && held.reason === 'stale-limit') {
    ctx.deps.store.holdAttempt(resumeId, {
      ...held,
      attempt: { ...held.attempt, staleRebases: 0 },
    });
  }
}

export function askAgain(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
): void {
  writeRetryFor(ctx, resumeId, itemId).requested = true;
}

export function startResend(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
): void {
  const retry = heldRetry(ctx, resumeId, itemId);
  if (retry === undefined) return;
  retry.requested = false;
  retry.rebase = false;
}

export function waitsForPerson(
  ctx: CoordinatorContext,
  resumeId: string,
  held: AttemptState,
): boolean {
  return (
    held.kind === 'failed'
    || held.kind === 'retry-later'
    || (held.kind === 'unknown' && held.reason === 'cutoff')
  ) && heldRetry(ctx, resumeId, held.queueItem.id)?.requested !== true;
}

export type HeldNext
  = | { readonly kind: 'send' }
    | { readonly kind: 'wait' }
    | { readonly kind: 'sleep'; readonly ms: number }
    | { readonly kind: 'cutoff'; readonly reason: 'transport' | 'server' };

export function heldNext(
  ctx: OperationContext,
  resumeId: string,
  held: AttemptState,
): HeldNext {
  const retry = heldRetry(ctx, resumeId, held.queueItem.id);
  if (ctx.deps.runtime.nowEpochMs() >= held.attempt.retryCutoff) {
    clearWriteRetry(ctx, resumeId);
    return {
      kind: 'cutoff',
      reason: held.kind === 'unknown' && held.reason !== 'cutoff'
        ? held.reason
        : 'transport',
    };
  }
  if (retry !== undefined && retry.wait > 0) {
    const ms = retry.wait;
    retry.wait = 0;
    return { kind: 'sleep', ms };
  }
  const due = Math.max(retry?.at ?? 0, retry?.notBefore ?? 0) - Date.now();
  if (due > 0) {
    schedule(ctx, resumeId, due);
    return { kind: 'wait' };
  }
  return { kind: 'send' };
}
