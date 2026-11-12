import {
  createReplacementCommand,
  type ConflictConfirmation,
} from './reconcile';
import { current, stopForSessionLoss } from './coordinatorGuards';
import { flush, schedule } from './coordinatorQueue';
import type {
  CoordinatorContext,
  OperationContext,
} from './coordinatorTypes';

export async function acceptLatest(
  ctx: CoordinatorContext,
  resumeId: string,
  conflictId: string,
): Promise<void> {
  const conflict = ctx.deps.store
    .recordFor(resumeId)
    ?.conflicts.find((candidate) => candidate.id === conflictId);
  if (conflict === undefined) return;
  ctx.deps.store.dropHead(resumeId, conflict.id);
  ctx.deps.store.resolveConflict(resumeId, conflictId);
  await flush(ctx, resumeId);
}

export async function applyMine(
  ctx: OperationContext,
  resumeId: string,
  conflictId: string,
  confirmation: ConflictConfirmation,
): Promise<void> {
  const conflict = ctx.deps.store
    .recordFor(resumeId)
    ?.conflicts.find((candidate) => candidate.id === conflictId);
  if (conflict?.subject !== 'atomic') return;
  const latest = await ctx.deps.api.read(resumeId);
  if (!current(ctx, resumeId)) return;
  if (latest.kind === 'session-lost') stopForSessionLoss(ctx, resumeId);
  if (latest.kind !== 'complete') return;
  const replacement = createReplacementCommand(
    conflict,
    latest.accepted,
    confirmation,
  );
  if (replacement === null) return;
  if (
    !ctx.deps.store.replaceConflictedAfterCompleteRead(
      resumeId,
      conflict.command,
      latest.accepted,
      replacement,
    )
  ) {
    return;
  }
  ctx.deps.store.resolveConflict(resumeId, conflictId);
  schedule(ctx, resumeId);
}
