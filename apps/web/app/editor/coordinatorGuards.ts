import { toRaw } from 'vue';

import type { useAuth } from '../composables/useAuth';
import type { ResumeRecord, useResumeStore } from '../stores/resumes';
import type { AtomicEditorCommand } from './commands';
import type { AttemptFailureCode, FrozenAttempt } from './attempt';
import type { EditorQueueItem } from './templateGroup';
import type {
  CoordinatorContext,
  OperationContext,
} from './coordinatorTypes';

export const DEBOUNCE_MS = 1_000;
export const UNKNOWN_REPLAY_DELAY_MS = 250;
export const RETRY_FIRST_MS = 1_000;
export const RETRY_MAX_MS = 30_000;
/** Rebases one write may have onto newer revisions before it is held. */
export const STALE_REBASE_LIMIT = 3;
/** How long a write may stay unknown before the person is told. */
export const HELD_WRITE_ALERT_MS = 60_000;

/**
 * The wait before retry number `failures + 1` of a failed read or of a write
 * whose outcome is unknown: 1 s, doubling to 30 s (docs/design/budgets.md).
 */
export function retryDelay(failures: number): number {
  return Math.min(RETRY_FIRST_MS * 2 ** failures, RETRY_MAX_MS);
}

/**
 * Starts an operation on the record the editor holds for `resumeId` now.
 * Closing the editor stops the coordinator for the resume, and opening the
 * resume again replaces the record; either makes the operation stale.
 */
export function begin(
  ctx: CoordinatorContext,
  resumeId: string,
): OperationContext {
  return { ...ctx, epoch: { record: rawRecord(ctx, resumeId) } };
}

/**
 * Whether an operation may still act after an await. A stale one returns
 * without sending, scheduling or changing the store; a request it already
 * sent may complete on the server, and its answer is ignored.
 */
export function current(ctx: OperationContext, resumeId: string): boolean {
  return (
    !ctx.stopped.has(resumeId)
    && rawRecord(ctx, resumeId) === ctx.epoch.record
  );
}

/** The operation is current and `queueItem` is still the write in flight. */
export function owns(
  ctx: OperationContext,
  resumeId: string,
  queueItem: EditorQueueItem,
): boolean {
  return (
    current(ctx, resumeId)
    && ctx.deps.store.recordFor(resumeId)?.attempt?.queueItem.id
    === queueItem.id
  );
}

function rawRecord(
  ctx: CoordinatorContext,
  resumeId: string,
): object | undefined {
  const record = ctx.deps.store.recordFor(resumeId);
  return record === undefined ? undefined : toRaw(record);
}

export function dispatchToken(
  ctx: CoordinatorContext,
  resumeId: string | undefined,
  ownerId: string,
): string | null {
  const token = ctx.deps.auth.csrfToken.value;
  if (
    token !== null
    && ctx.deps.auth.authState.value === 'authenticated'
    && ctx.deps.auth.user.value?.id === ownerId
  ) {
    return token;
  }
  if (
    resumeId !== undefined
    && (ctx.deps.auth.authState.value === 'anonymous'
      || (ctx.deps.auth.authState.value === 'authenticated'
        && ctx.deps.auth.user.value?.id !== ownerId))
  ) {
    stopForSessionLoss(ctx, resumeId);
  }
  return null;
}

export function stopForSessionLoss(
  ctx: CoordinatorContext,
  resumeId: string,
): void {
  const timer = ctx.timers.get(resumeId);
  if (timer !== undefined) clearTimeout(timer);
  ctx.timers.delete(resumeId);
  ctx.deps.store.markSessionLost(resumeId);
}

export function discard(ctx: CoordinatorContext, resumeId: string): void {
  const timer = ctx.timers.get(resumeId);
  if (timer !== undefined) clearTimeout(timer);
  ctx.timers.delete(resumeId);
  clearWriteRetry(ctx, resumeId);
  ctx.deps.store.discardLocal(resumeId);
}

/** Forgets the retry schedule and alert timer of a held write. */
export function clearWriteRetry(
  ctx: CoordinatorContext,
  resumeId: string,
): void {
  const retry = ctx.writeRetries.get(resumeId);
  if (retry === undefined) return;
  clearTimeout(retry.alert);
  ctx.writeRetries.delete(resumeId);
}

/**
 * A write held with an unknown outcome is sent again, unchanged, while the
 * session is the owner's and its retry window is open.
 */
export function heldWriteDue(
  record: ResumeRecord | undefined,
  authState: ReturnType<typeof useAuth>['authState']['value'],
  ownerId: string | undefined,
): boolean {
  const held = record?.attempt;
  return (
    held?.kind === 'unknown'
    && held.reason !== 'cutoff'
    && !record!.sessionLost
    && record!.opaquePhotoOutcome === null
    && authState === 'authenticated'
    && held.queueItem.ownerId === ownerId
  );
}

export function canDrain(
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

/**
 * The read barrier is completed before any queued edit, once no write is in
 * flight. A lost session waits for sign-in instead.
 */
export function readBarrierDue(
  record: ResumeRecord | undefined,
  authState: ReturnType<typeof useAuth>['authState']['value'],
  ownerId: string | undefined,
): boolean {
  return (
    record !== undefined
    && record.completeReadRequired
    && record.attempt === null
    && !record.sessionLost
    && authState === 'authenticated'
    && ownerId !== undefined
    && (record.pending[0]?.ownerId ?? ownerId) === ownerId
  );
}

export function dependenciesAcknowledged(
  record: ResumeRecord,
  item: EditorQueueItem,
): boolean {
  return !item.dependencyIds.some(
    (id) =>
      record.attempt?.queueItem.id === id
      || record.pending.some((candidate) => candidate.id === id),
  );
}

export function holdFailed(
  store: ReturnType<typeof useResumeStore>,
  resumeId: string,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
  reason:
    AttemptFailureCode | 'csrf-rejected' | 'idempotency-reuse' | 'stale-limit',
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

export function assertNever(value: never): never {
  throw new Error(`unhandled attempt result: ${String(value)}`);
}
