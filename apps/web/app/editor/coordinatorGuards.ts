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
export const STALE_REBASE_LIMIT = 3;
export const HELD_WRITE_ALERT_MS = 60_000;

export function retryDelay(failures: number): number {
  return Math.min(RETRY_FIRST_MS * 2 ** failures, RETRY_MAX_MS);
}

export function begin(
  ctx: CoordinatorContext,
  resumeId: string,
): OperationContext {
  return { ...ctx, epoch: { record: rawRecord(ctx, resumeId) } };
}

export function current(ctx: OperationContext, resumeId: string): boolean {
  return (
    !ctx.stopped.has(resumeId)
    && rawRecord(ctx, resumeId) === ctx.epoch.record
  );
}

export function owns(
  ctx: OperationContext,
  resumeId: string,
  attempt: FrozenAttempt,
): boolean {
  return (
    current(ctx, resumeId)
    && ctx.deps.store.recordFor(resumeId)?.attempt?.attempt.idempotencyKey
    === attempt.idempotencyKey
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

export function clearWriteRetry(
  ctx: CoordinatorContext,
  resumeId: string,
): void {
  const retry = ctx.writeRetries.get(resumeId);
  if (retry === undefined) return;
  clearTimeout(retry.alert);
  ctx.writeRetries.delete(resumeId);
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

export function readBarrierDue(
  record: ResumeRecord | undefined,
  authState: ReturnType<typeof useAuth>['authState']['value'],
  ownerId: string | undefined,
): boolean {
  const head = record?.attempt?.queueItem ?? record?.pending[0];
  return (
    record !== undefined
    && record.completeReadRequired
    && !record.sessionLost
    && authState === 'authenticated'
    && ownerId !== undefined
    && (head?.ownerId ?? ownerId) === ownerId
  );
}

export function heldConflict(record: ResumeRecord): boolean {
  const held = record.attempt;
  return held !== null && record.conflicts.some(
    ({ id }) => id === held.queueItem.id || id === held.command.id,
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
