import type { AttemptState } from '../stores/resumeRecord';
import {
  clearWriteRetry,
  HELD_WRITE_ALERT_MS,
  retryDelay,
} from './coordinatorGuards';
import { schedule } from './coordinatorQueue';
import { resolveUnknown } from './coordinatorSettle';
import type {
  CoordinatorContext,
  OperationContext,
  WriteRetry,
} from './coordinatorTypes';

// When the write a resume's queue holds goes out again. Settlement and the
// person's buttons only change this schedule and wake the drain; the drain
// alone sends.
//
// A write whose outcome is unknown is sent again unchanged: the same
// Idempotency-Key, If-Match and body, so a write that already landed replays
// its stored answer instead of running twice (ADR 0006). After one quick
// replay it is retried on the schedule in docs/design/budgets.md, never
// sooner than the server's Retry-After, until the attempt's retry cutoff,
// where the read-and-reconcile path takes over. The person is told once the
// outcome has stayed unknown for HELD_WRITE_ALERT_MS.

/** The schedule of the held write `itemId`, started afresh if needed. */
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

/** Starts the alert clock at a write's first unknown outcome. */
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

/**
 * Backs off after another unknown outcome: the next retry waits the next
 * step of the schedule, and nothing goes before `retryAfterMs`.
 */
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

/** Nothing for the held write goes before the server's Retry-After. */
export function waitForServer(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
  retryAfterMs: number,
): void {
  const retry = writeRetryFor(ctx, resumeId, itemId);
  retry.notBefore = Math.max(retry.notBefore, Date.now() + retryAfterMs);
}

/**
 * A short wait the drain sleeps itself before the next send: the quick
 * replay of an unknown write, or a rebase onto a newer revision. A rebase
 * also restarts the backoff, since the server answered.
 */
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

/** The held write is a rebase built from a 412 winner's document. */
export function rebaseHeld(
  ctx: CoordinatorContext,
  resumeId: string,
  held: AttemptState | null,
): boolean {
  return held !== null
    && heldRetry(ctx, resumeId, held.queueItem.id)?.rebase === true;
}

/**
 * Asks for the held write to go out again, for Retry now, Try again or a
 * session signed in again. It ends the backoff but keeps the server's
 * Retry-After, and Try again also starts a fresh cycle of rebases. Nothing
 * changes while the write is on its way.
 */
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

/** The held write is to go out again even if it waits for the person. */
export function askAgain(
  ctx: CoordinatorContext,
  resumeId: string,
  itemId: string,
): void {
  writeRetryFor(ctx, resumeId, itemId).requested = true;
}

/** The held write goes out now: a request and a rebase are used up. */
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

/**
 * Part of step 2 of the drain: a write refused for a reason, held for later
 * or past its retry window goes out again only when asked, so until then
 * the drain neither reads nor sends for it.
 */
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

/**
 * Step 4 of the drain: whether the held write is sent now, waits, or the
 * drain looks again after a wait or a read. Past its retry window a write
 * is read and reconciled instead of sent.
 */
export async function nextForHeld(
  ctx: OperationContext,
  resumeId: string,
  held: AttemptState,
): Promise<'send' | 'again' | 'wait'> {
  const retry = heldRetry(ctx, resumeId, held.queueItem.id);
  if (ctx.deps.runtime.nowEpochMs() >= held.attempt.retryCutoff) {
    clearWriteRetry(ctx, resumeId);
    await resolveUnknown(
      ctx,
      resumeId,
      held.queueItem,
      held.command,
      held.attempt,
      held.kind === 'unknown' && held.reason !== 'cutoff'
        ? held.reason
        : 'transport',
    );
    return 'again';
  }
  if (retry !== undefined && retry.wait > 0) {
    const wait = retry.wait;
    retry.wait = 0;
    await ctx.deps.runtime.delay(wait);
    return 'again';
  }
  const due = Math.max(retry?.at ?? 0, retry?.notBefore ?? 0) - Date.now();
  if (due > 0) {
    schedule(ctx, resumeId, due);
    return 'wait';
  }
  return 'send';
}
