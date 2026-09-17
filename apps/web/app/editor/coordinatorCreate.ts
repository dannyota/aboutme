import type { AttemptResult } from './attempt';
import type { CreateResumeIntent } from './commands';
import { freezeCreateAttempt } from './resumeApi';
import { dispatchToken, UNKNOWN_REPLAY_DELAY_MS } from './coordinatorGuards';
import type {
  CoordinatorContext,
  CreateResumeResult,
  OpaqueCreateOutcome,
  RetainedCreate,
} from './coordinatorTypes';

export async function createResume(
  ctx: CoordinatorContext,
  intent: CreateResumeIntent,
): Promise<CreateResumeResult> {
  const retained = ctx.retainedCreates.get(intent.id) ?? {
    intent,
    attempt: freezeCreateAttempt(intent, ctx.deps.runtime),
  };
  ctx.retainedCreates.set(intent.id, retained);
  return dispatchCreate(ctx, retained);
}

export async function retryCreate(
  ctx: CoordinatorContext,
  intentId: string,
): Promise<CreateResumeResult> {
  const retained = ctx.retainedCreates.get(intentId);
  if (retained === undefined) {
    return { kind: 'blocked', intentId, reason: 'unknown' };
  }
  if (retained.outcome !== undefined) {
    return { kind: 'opaque-create', outcome: retained.outcome };
  }
  return dispatchCreate(ctx, retained);
}

async function dispatchCreate(
  ctx: CoordinatorContext,
  retained: RetainedCreate,
): Promise<CreateResumeResult> {
  const csrfToken = dispatchToken(ctx, undefined, retained.intent.ownerId);
  if (csrfToken === null) {
    return {
      kind: 'blocked',
      intentId: retained.intent.id,
      reason: 'session-lost',
    };
  }
  const result = await ctx.deps.api.dispatch(retained.attempt, csrfToken);
  switch (result.kind) {
    case 'complete':
      if (result.status !== 201) {
        return {
          kind: 'blocked',
          intentId: retained.intent.id,
          reason: 'unknown',
        };
      }
      ctx.retainedCreates.delete(retained.intent.id);
      return { kind: 'created', resume: result.accepted };
    case 'csrf-rejected': {
      await ctx.deps.auth.refresh();
      const freshToken = dispatchToken(
        ctx,
        undefined,
        retained.intent.ownerId,
      );
      if (freshToken === null) {
        return {
          kind: 'blocked',
          intentId: retained.intent.id,
          reason: 'session-lost',
        };
      }
      return settleCreate(
        ctx,
        retained,
        await ctx.deps.api.dispatch(retained.attempt, freshToken),
      );
    }
    default:
      return settleCreate(ctx, retained, result);
  }
}

async function settleCreate(
  ctx: CoordinatorContext,
  retained: RetainedCreate,
  result: AttemptResult,
): Promise<CreateResumeResult> {
  switch (result.kind) {
    case 'complete':
      if (result.status === 201) {
        ctx.retainedCreates.delete(retained.intent.id);
        return { kind: 'created', resume: result.accepted };
      }
      return {
        kind: 'blocked',
        intentId: retained.intent.id,
        reason: 'unknown',
      };
    case 'session-lost':
      return {
        kind: 'blocked',
        intentId: retained.intent.id,
        reason: 'session-lost',
      };
    case 'rate-limited':
    case 'media-busy':
      return {
        kind: 'retry-later',
        intentId: retained.intent.id,
        retryAfterMs: result.retryAfterMs,
      };
    case 'rejected':
      ctx.retainedCreates.delete(retained.intent.id);
      return { kind: 'rejected', code: result.code };
    case 'unknown':
      if (
        retained.attempt.automaticReplays === 0
        && ctx.deps.runtime.nowEpochMs() < retained.attempt.retryCutoff
      ) {
        retained.attempt = { ...retained.attempt, automaticReplays: 1 };
        await ctx.deps.runtime.delay(UNKNOWN_REPLAY_DELAY_MS);
        return dispatchCreate(ctx, retained);
      }
      if (ctx.deps.runtime.nowEpochMs() < retained.attempt.retryCutoff) {
        return {
          kind: 'blocked',
          intentId: retained.intent.id,
          reason: 'unknown',
        };
      }
      return opaqueCreate(ctx, retained);
    default:
      return {
        kind: 'blocked',
        intentId: retained.intent.id,
        reason: 'unknown',
      };
  }
}

async function opaqueCreate(
  ctx: CoordinatorContext,
  retained: RetainedCreate,
): Promise<CreateResumeResult> {
  const listed = await ctx.deps.api.list();
  const outcome: OpaqueCreateOutcome = {
    kind: 'create-cutoff',
    intent: retained.intent,
    attempt: retained.attempt,
    refreshedItems: listed.kind === 'ready' ? listed.items : null,
  };
  retained.outcome = outcome;
  return { kind: 'opaque-create', outcome };
}

export async function refreshOpaqueCreate(
  ctx: CoordinatorContext,
  intentId: string,
): Promise<OpaqueCreateOutcome> {
  const retained = ctx.retainedCreates.get(intentId);
  if (retained?.outcome === undefined) {
    throw new Error('opaque create not found');
  }
  const listed = await ctx.deps.api.list();
  retained.outcome = {
    ...retained.outcome,
    refreshedItems: listed.kind === 'ready' ? listed.items : null,
  };
  return retained.outcome;
}

export function abandonOpaqueCreate(
  ctx: CoordinatorContext,
  intentId: string,
): void {
  ctx.retainedCreates.delete(intentId);
}
