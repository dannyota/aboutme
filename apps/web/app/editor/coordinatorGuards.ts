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
export const READ_RETRY_FIRST_MS = 1_000;
export const READ_RETRY_MAX_MS = 30_000;

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
  ctx.deps.store.discardLocal(resumeId);
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
    AttemptFailureCode | 'csrf-rejected' | 'idempotency-reuse' | 'second-stale',
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
