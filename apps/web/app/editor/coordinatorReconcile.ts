import { toRaw } from 'vue';
import { reconcileCommand, reconcileTemplateGroup } from './reconcile';
import { compareRevision, parentETag } from './revision';
import type { ResumeConditionalReadResult } from './resumeApi';
import {
  advanceTemplateGroup,
  reconcileTemplateChild,
  type TemplateGroupCommand,
} from './templateGroup';
import type { AcceptedResume, ParentETag } from './types';
import type { ResumeReadResult } from './attempt';
import { current, stopForSessionLoss } from './coordinatorGuards';
import { flush } from './coordinatorQueue';
import type {
  CoordinatorContext,
  OperationContext,
} from './coordinatorTypes';

export function settleTemplateComplete(
  ctx: CoordinatorContext,
  resumeId: string,
  group: TemplateGroupCommand,
  accepted: AcceptedResume,
): void {
  const record = ctx.deps.store.recordFor(resumeId);
  if (record === undefined || record.templateState === null) return;
  const adoption = ctx.deps.store.adoptComplete(resumeId, accepted);
  if (adoption.kind === 'older') {
    if (compareRevision(accepted.revision, adoption.winner.revision) === 0) {
      const next = advanceTemplateGroup(
        group,
        record.templateState,
        adoption.winner,
      );
      ctx.deps.store.setTemplateState(resumeId, next);
      if (next.kind === 'complete') {
        ctx.deps.store.dropHead(resumeId, group.id);
        return;
      }
      ctx.deps.store.continueTemplateGroup(resumeId, group.id);
      return;
    }
    const decision = reconcileTemplateGroup(group, adoption.winner);
    if (decision.kind === 'satisfied') {
      ctx.deps.store.setTemplateState(resumeId, null);
      ctx.deps.store.dropHead(resumeId, group.id);
      return;
    }
    ctx.deps.store.setTemplateState(resumeId, {
      kind: 'partial',
      accepted: adoption.winner,
      nextChild:
        record.templateState.kind === 'queued'
        || record.templateState.kind === 'running'
          ? record.templateState.nextChild
          : 0,
      reason: 'superseded-after-success',
    });
    ctx.deps.store.continueTemplateGroup(resumeId, group.id);
    return;
  }
  const next = advanceTemplateGroup(
    group,
    record.templateState,
    adoption.accepted,
  );
  ctx.deps.store.setTemplateState(resumeId, next);
  if (next.kind === 'complete') {
    ctx.deps.store.dropHead(resumeId, group.id);
    return;
  }
  ctx.deps.store.continueTemplateGroup(resumeId, group.id);
}

export async function refreshAndReconcile(
  ctx: OperationContext,
  resumeId: string,
): Promise<ResumeReadResult> {
  const result = await ctx.deps.api.read(resumeId);
  if (!current(ctx, resumeId)) return result;
  if (result.kind === 'session-lost') {
    stopForSessionLoss(ctx, resumeId);
    return result;
  }
  if (result.kind !== 'complete') return result;
  const record = ctx.deps.store.recordFor(resumeId);
  if (record === undefined) return result;
  const latest = adoptAndReconcile(ctx, resumeId, result.accepted);
  return latest === undefined
    ? result
    : { kind: 'complete', accepted: latest };
}

function adoptAndReconcile(
  ctx: CoordinatorContext,
  resumeId: string,
  accepted: AcceptedResume,
): AcceptedResume | undefined {
  if (ctx.deps.store.recordFor(resumeId) === undefined) return undefined;
  const adoption = adoptRead(ctx, resumeId, accepted);
  const latest
    = adoption.kind === 'adopted' ? adoption.accepted : adoption.winner;
  const active = ctx.deps.store.recordFor(resumeId)?.attempt;
  // A held write the read shows landed no longer blocks the queue.
  if (reconcileHeld(ctx, resumeId, latest) && active?.kind !== 'dispatching') {
    void flush(ctx, resumeId);
  }
  return latest;
}

/**
 * Reconciles the write the queue holds against `latest`, a complete read:
 * one the resume already holds leaves the queue and a conflict waits for the
 * person. A template child is settled here only past its retry window.
 * Returns whether the write left the queue.
 */
export function reconcileHeld(
  ctx: CoordinatorContext,
  resumeId: string,
  latest: AcceptedResume,
): boolean {
  const record = ctx.deps.store.recordFor(resumeId);
  const active = record?.attempt;
  if (record === undefined || active === null || active === undefined) {
    return false;
  }
  if (active.queueItem.kind === 'templateGroup') {
    if (
      active.command.kind !== 'structure'
      && active.command.kind !== 'customization'
    ) {
      return false;
    }
    const decision = reconcileTemplateChild(toRaw(active.command), latest);
    if (
      decision.kind === 'satisfied'
      && active.kind === 'unknown'
      && active.reason === 'cutoff'
    ) {
      settleTemplateComplete(ctx, resumeId, active.queueItem, latest);
    } else if (
      decision.kind === 'safe-base'
      && active.kind === 'unknown'
      && active.reason === 'cutoff'
      && record.templateState !== null
      && record.templateState.kind !== 'complete'
      && record.templateState.kind !== 'partial'
    ) {
      ctx.deps.store.setTemplateState(resumeId, {
        kind: 'partial',
        accepted: latest,
        nextChild: record.templateState.nextChild,
        reason: 'unknown-outcome',
      });
      ctx.deps.store.continueTemplateGroup(resumeId, active.queueItem.id);
    } else if (decision.kind === 'conflict') {
      ctx.deps.store.markConflict(resumeId, decision.conflict);
    }
    return false;
  }
  const decision = reconcileCommand(toRaw(active.command), latest);
  if (decision.kind === 'satisfied') {
    ctx.deps.store.dropHead(resumeId, active.queueItem.id);
    return true;
  }
  if (decision.kind === 'conflict') {
    ctx.deps.store.markConflict(resumeId, decision.conflict);
  }
  return false;
}

export async function refreshConditionalAndReconcile(
  ctx: OperationContext,
  resumeId: string,
  etag?: ParentETag,
): Promise<ResumeConditionalReadResult> {
  const result = await ctx.deps.api.readConditional(resumeId, etag);
  if (!current(ctx, resumeId)) return result;
  if (result.kind === 'session-lost') {
    stopForSessionLoss(ctx, resumeId);
    return result;
  }
  if (result.kind !== 'complete') return result;
  const record = ctx.deps.store.recordFor(resumeId);
  if (record === undefined) return result;
  const latest = adoptAndReconcile(ctx, resumeId, result.accepted);
  return latest === undefined
    ? result
    : {
        kind: 'complete',
        accepted: latest,
        etag: parentETag(latest.revision),
      };
}

/**
 * Adopts a complete read of the resume. Any complete read at the held
 * revision or newer completes the read barrier; without the barrier only a
 * newer revision replaces what the editor holds.
 */
export function adoptRead(
  ctx: CoordinatorContext,
  resumeId: string,
  accepted: AcceptedResume,
): ReturnType<CoordinatorContext['deps']['store']['adoptComplete']> {
  return ctx.deps.store.recordFor(resumeId)?.completeReadRequired === true
    ? ctx.deps.store.adoptCompleteRead(resumeId, accepted)
    : ctx.deps.store.adoptComplete(resumeId, accepted);
}
