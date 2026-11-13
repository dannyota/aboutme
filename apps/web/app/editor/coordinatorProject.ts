import type { AtomicEditorCommand } from './commands';
import type { AttemptFailureCode } from './attempt';
import { linkedEntry } from './projectLinks';
import { noteProjectOutcome, type ProjectOutcome } from './projectOutcomes';
import type { ReconcileDecision } from './reconcile';
import type { EditorQueueItem } from './templateGroup';
import type { ResumeSnapshot } from './types';
import type { CoordinatorContext } from './coordinatorTypes';

type ProjectCommand = Extract<
  AtomicEditorCommand,
  { kind: 'projectLink' | 'projectLinkEntry' | 'projectRefresh' }
>;

const REFUSALS: ReadonlySet<AttemptFailureCode> = new Set([
  'project_not_found',
  'project_already_linked',
  'entry_not_found',
]);

function isProjectCommand(item: EditorQueueItem): item is ProjectCommand {
  return item.kind === 'projectLink'
    || item.kind === 'projectLinkEntry'
    || item.kind === 'projectRefresh';
}

function drop(
  ctx: CoordinatorContext,
  resumeId: string,
  command: ProjectCommand,
  outcome?: ProjectOutcome,
): void {
  if (outcome !== undefined) noteProjectOutcome(command.id, outcome);
  ctx.deps.store.resolveConflict(resumeId, command.id);
  ctx.deps.store.dropHead(resumeId, command.id);
  ctx.deps.store.requireCompleteRead(resumeId);
}

export function dropRefusedProjectCommand(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  code?: AttemptFailureCode,
): boolean {
  if (!isProjectCommand(queueItem)) return false;
  if (code !== undefined && !REFUSALS.has(code)) return false;
  drop(ctx, resumeId, queueItem);
  return true;
}

function refreshChange(
  command: Extract<ProjectCommand, { kind: 'projectRefresh' }>,
  latest: ResumeSnapshot,
): ProjectOutcome | undefined {
  const base = command.base.target;
  const projectId = base.present
    ? (base.value as { projectId?: string } | undefined)?.projectId
    : undefined;
  return projectId !== undefined
    && linkedEntry(latest.document, projectId)?.entryId === command.entryId
    ? 'refresh-changed'
    : undefined;
}

export function dropConflictingProject(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  latest: ResumeSnapshot,
): boolean {
  if (queueItem.kind === 'projectLinkEntry') {
    drop(ctx, resumeId, queueItem);
    return true;
  }
  if (queueItem.kind !== 'projectRefresh') return false;
  drop(ctx, resumeId, queueItem, refreshChange(queueItem, latest));
  return true;
}

export function settleStaleProject(
  ctx: CoordinatorContext,
  resumeId: string,
  queueItem: EditorQueueItem,
  latest: ResumeSnapshot,
  decision: ReconcileDecision,
): boolean {
  if (queueItem.kind === 'projectLink') {
    if (decision.kind !== 'satisfied') return false;
    drop(ctx, resumeId, queueItem);
    return true;
  }
  if (decision.kind !== 'conflict') return false;
  return dropConflictingProject(ctx, resumeId, queueItem, latest);
}
