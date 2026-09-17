import { dependencyIdsForNewCommand, nextSequence } from '../stores/resumes';
import { captureCommand } from './commands';
import { schedule } from './coordinatorQueue';
import type {
  CoordinatorContext,
  OpaquePhotoDecision,
} from './coordinatorTypes';

export async function resolveOpaquePhoto(
  ctx: CoordinatorContext,
  resumeId: string,
  commandId: string,
  decision: OpaquePhotoDecision,
): Promise<void> {
  const record = ctx.deps.store.recordFor(resumeId);
  const opaque = record?.opaquePhotoOutcome;
  if (
    record === undefined
    || opaque === undefined
    || opaque === null
    || opaque.command.id !== commandId
  ) {
    return;
  }
  ctx.deps.store.dropHead(resumeId, commandId);
  ctx.deps.store.setOpaquePhotoOutcome(resumeId, null);
  if (decision.kind === 'keep-observed') return;
  const replacement = captureCommand(
    record.current,
    {
      resumeId,
      ownerId: opaque.command.ownerId,
      sequence: nextSequence(record),
      dependencyIds: dependencyIdsForNewCommand(record),
      intent: { kind: 'photoUpload', file: decision.file },
    },
    ctx.deps.runtime,
  );
  ctx.deps.store.enqueue(resumeId, replacement);
  schedule(ctx, resumeId);
}
