import type { AtomicEditorCommand } from './commands';
import type { AttemptFailureCode } from './attempt';
import { linkedEntry } from './projectLinks';
import { noteProjectOutcome, type ProjectOutcome } from './projectOutcomes';
import type { ReconcileDecision } from './reconcile';
import type { EditorQueueItem } from './templateGroup';
import type { ResumeSnapshot } from './types';
import { completeReadBarrier } from './coordinatorReconcile';
import type { CoordinatorContext } from './coordinatorTypes';

// How a link or refresh leaves the queue when it is not simply applied
// (docs/design/ui/projects.md "In the resume editor"). Each path completes
// the read barrier before the queue continues, so the edits behind it are
// sent; a stale winner adopted on the way leaves that barrier set.

type ProjectCommand = Extract<
  AtomicEditorCommand,
  { kind: 'projectLink' | 'projectRefresh' }
>;

const REFUSALS: ReadonlySet<AttemptFailureCode> = new Set([
  'project_not_found',
  'project_already_linked',
]);

function isProjectCommand(item: EditorQueueItem): item is ProjectCommand {
  return item.kind === 'projectLink' || item.kind === 'projectRefresh';
}

async function drop(
  ctx: CoordinatorContext,
  resumeId: string,
  command: ProjectCommand,
  outcome?: ProjectOutcome,
): Promise<void> {
  if (outcome !== undefined) noteProjectOutcome(command.id, outcome);
  // A conflict a realtime read recorded for this command goes with it.
  ctx.deps.store.resolveConflict(resumeId, command.id);
  ctx.deps.store.dropHead(resumeId, command.id);
  await completeReadBarrier(ctx, resumeId);
}

/**
 * A refused link or refresh wrote nothing, and its entry is built by the
 * server, so no edit can make it succeed. It leaves the queue rather than
 * holding the edits behind it, and the read shows what the resume holds:
 * a link another tab made, or an entry whose project was deleted.
 */
export async function dropRefusedProjectCommand(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  code?: AttemptFailureCode,
): Promise<boolean> {
  if (!isProjectCommand(queueItem)) return false;
  if (code !== undefined && !REFUSALS.has(code)) return false;
  await drop(ctx, resumeId, queueItem);
  return true;
}

/** Why a refresh does not go out over `latest`, or nothing to say. */
function refreshChange(
  command: Extract<ProjectCommand, { kind: 'projectRefresh' }>,
  latest: ResumeSnapshot,
): ProjectOutcome | undefined {
  const base = command.base.target;
  const projectId = base.present
    ? (base.value as { projectId?: string } | undefined)?.projectId
    : undefined;
  // An entry that lost its link loses its note too, so it needs no word.
  return projectId !== undefined
    && linkedEntry(latest.document, projectId)?.entryId === command.entryId
    ? 'refresh-changed'
    : undefined;
}

/**
 * A refresh whose entry changed after the person confirmed it is not sent
 * over that text; it leaves the queue so Refresh asks again.
 */
export async function dropChangedRefresh(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  latest: ResumeSnapshot,
): Promise<boolean> {
  if (queueItem.kind !== 'projectRefresh') return false;
  await drop(ctx, resumeId, queueItem, refreshChange(queueItem, latest));
  return true;
}

/**
 * Settles a project command after a revision mismatch. A link that already
 * landed leaves the queue; any other link is the same request, resent once
 * on the new revision by the caller. A refresh is resent only over
 * unchanged text.
 */
export async function settleStaleProject(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  latest: ResumeSnapshot,
  decision: ReconcileDecision,
): Promise<boolean> {
  if (queueItem.kind === 'projectLink') {
    if (decision.kind !== 'satisfied') return false;
    await drop(ctx, resumeId, queueItem);
    return true;
  }
  if (decision.kind !== 'conflict') return false;
  return dropChangedRefresh(ctx, resumeId, queueItem, latest);
}
