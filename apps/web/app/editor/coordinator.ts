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
    stop: (resumeId) => stop(ctx, resumeId),
    resolveOpaquePhoto: (resumeId, commandId, decision) =>
      resolveOpaquePhoto(ctx, resumeId, commandId, decision),
    discard: (resumeId) => discard(ctx, resumeId),
  };
}
