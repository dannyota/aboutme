import { acceptLatest, applyMine } from './coordinatorConflicts';
import {
  abandonOpaqueCreate,
  createResume,
  refreshOpaqueCreate,
  retryCreate,
} from './coordinatorCreate';
import { begin, discard } from './coordinatorGuards';
import { resolveOpaquePhoto } from './coordinatorPhoto';
import {
  completeReadBarrier,
  flush,
  resumeAfterAuth,
  retry,
  schedule,
  stop,
  wake,
} from './coordinatorQueue';
import {
  refreshAndReconcile,
  refreshConditionalAndReconcile,
} from './coordinatorReconcile';
import type {
  CoordinatorContext,
  CoordinatorDeps,
  ResumeMutationCoordinator,
} from './coordinatorTypes';

export type {
  CreateResumeResult,
  OpaqueCreateOutcome,
  OpaquePhotoDecision,
  ResumeMutationCoordinator,
} from './coordinatorTypes';

export function createMutationCoordinator(
  deps: CoordinatorDeps,
): ResumeMutationCoordinator {
  const ctx: CoordinatorContext = {
    deps,
    timers: new Map(),
    drains: new Map(),
    retainedCreates: new Map(),
    csrfRetries: new Set(),
    readRetries: new Map(),
    stopped: new Set(),
    writeRetries: new Map(),
  };

  return {
    createResume: (intent) => createResume(ctx, intent),
    retryCreate: (intentId) => retryCreate(ctx, intentId),
    refreshOpaqueCreate: (intentId) => refreshOpaqueCreate(ctx, intentId),
    abandonOpaqueCreate: (intentId) => abandonOpaqueCreate(ctx, intentId),
    schedule: (resumeId) => schedule(ctx, resumeId),
    flush: (resumeId) => flush(ctx, resumeId),
    completeRead: (resumeId) =>
      completeReadBarrier(begin(ctx, resumeId), resumeId),
    retry: (resumeId, commandId) =>
      retry(begin(ctx, resumeId), resumeId, commandId),
    refreshAndReconcile: (resumeId) =>
      refreshAndReconcile(begin(ctx, resumeId), resumeId),
    refreshConditionalAndReconcile: (resumeId, etag) =>
      refreshConditionalAndReconcile(begin(ctx, resumeId), resumeId, etag),
    acceptLatest: (resumeId, conflictId) =>
      acceptLatest(ctx, resumeId, conflictId),
    applyMine: (resumeId, conflictId, confirmation) =>
      applyMine(begin(ctx, resumeId), resumeId, conflictId, confirmation),
    resumeAfterAuth: (resumeId) =>
      resumeAfterAuth(begin(ctx, resumeId), resumeId),
    wake: (resumeId, recovered) =>
      wake(begin(ctx, resumeId), resumeId, recovered),
    stop: (resumeId) => stop(ctx, resumeId),
    resolveOpaquePhoto: (resumeId, commandId, decision) =>
      resolveOpaquePhoto(ctx, resumeId, commandId, decision),
    discard: (resumeId) => discard(ctx, resumeId),
  };
}
