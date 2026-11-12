import type { useAuth } from '../composables/useAuth';
import type { ResumeRecord, useResumeStore } from '../stores/resumes';
import type { AtomicEditorCommand } from './commands';
import type { AttemptFailureCode, FrozenAttempt } from './attempt';
import type { EditorQueueItem } from './templateGroup';
import type { CoordinatorContext } from './coordinatorTypes';

export const DEBOUNCE_MS = 1_000;
export const UNKNOWN_REPLAY_DELAY_MS = 250;
export const READ_RETRY_FIRST_MS = 1_000;
export const READ_RETRY_MAX_MS = 30_000;

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
