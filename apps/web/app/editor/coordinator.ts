import { acceptLatest, applyMine } from './coordinatorConflicts';
import {
  abandonOpaqueCreate,
  createResume,
  refreshOpaqueCreate,
  retryCreate,
} from './coordinatorCreate';
import { discard } from './coordinatorGuards';
import { resolveOpaquePhoto } from './coordinatorPhoto';
import { flush, resumeAfterAuth, retry, schedule } from './coordinatorQueue';
import {
  completeReadBarrier,
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

/**
 * Builds the resume mutation coordinator: the debounced, at-most-one-in-flight
 * write queue that turns editor commands into server attempts, reconciles the
 * server's response against the local optimistic state, and recovers from
 * conflicts, opaque outcomes, and session loss.
 *
 * The coordinator's behavior lives in the sibling `coordinator*` modules; this
 * factory only builds the shared context and wires each module's functions
 * into the public interface.
 */
export function createMutationCoordinator(
  deps: CoordinatorDeps,
): ResumeMutationCoordinator {
  const ctx: CoordinatorContext = {
    deps,
    timers: new Map(),
    drains: new Map(),
    retainedCreates: new Map(),
    csrfRetries: new Set(),
  };

  return {
    createResume: (intent) => createResume(ctx, intent),
    retryCreate: (intentId) => retryCreate(ctx, intentId),
    refreshOpaqueCreate: (intentId) => refreshOpaqueCreate(ctx, intentId),
    abandonOpaqueCreate: (intentId) => abandonOpaqueCreate(ctx, intentId),
    schedule: (resumeId) => schedule(ctx, resumeId),
    flush: (resumeId) => flush(ctx, resumeId),
    completeRead: (resumeId) => completeReadBarrier(ctx, resumeId),
    retry: (resumeId, commandId) => retry(ctx, resumeId, commandId),
    refreshAndReconcile: (resumeId) => refreshAndReconcile(ctx, resumeId),
    refreshConditionalAndReconcile: (resumeId, etag) =>
      refreshConditionalAndReconcile(ctx, resumeId, etag),
    acceptLatest: (resumeId, conflictId) =>
      acceptLatest(ctx, resumeId, conflictId),
    applyMine: (resumeId, conflictId, confirmation) =>
      applyMine(ctx, resumeId, conflictId, confirmation),
    resumeAfterAuth: (resumeId) => resumeAfterAuth(ctx, resumeId),
    resolveOpaquePhoto: (resumeId, commandId, decision) =>
      resolveOpaquePhoto(ctx, resumeId, commandId, decision),
    discard: (resumeId) => discard(ctx, resumeId),
  };
}
