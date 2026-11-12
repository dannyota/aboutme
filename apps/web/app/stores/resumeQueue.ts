import { toRaw } from 'vue';

import { coalescePending } from '../editor/commands';
import type { FrozenAttempt, ServerValidationIssue } from '../editor/attempt';
import type { AtomicEditorCommand } from '../editor/commands';
import type { ConflictRecord } from '../editor/reconcile';
import type {
  EditorQueueItem,
  TemplateGroupState,
} from '../editor/templateGroup';
import {
  copy,
  isAtomic,
  replay,
  type AttemptState,
  type ResumeRecord,
} from './resumeRecord';

// The editor's own changes to a resume record: queued edits, the write in
// flight, conflicts and issues the person has to see, and discarding them.

export function enqueue(record: ResumeRecord, item: EditorQueueItem): void {
  const pending = toRaw(record.pending) as readonly EditorQueueItem[];
  record.pending = (isAtomic(item)
    ? coalesceAtomicPending(pending, item)
    : [...pending, item]) as unknown as typeof record.pending;
  record.discarded = false;
  replay(record);
}

function coalesceAtomicPending(
  pending: readonly EditorQueueItem[],
  item: AtomicEditorCommand,
): readonly EditorQueueItem[] {
  const last = pending.at(-1);
  if (last === undefined || !isAtomic(last)) return [...pending, item];
  return [...pending.slice(0, -1), ...coalescePending([last], item)];
}

export function startAttempt(
  record: ResumeRecord,
  queueItem: EditorQueueItem,
  command: AtomicEditorCommand,
  attempt: FrozenAttempt,
): void {
  if (record.attempt !== null || record.pending[0]?.id !== queueItem.id) {
    return;
  }
  record.pending = record.pending.slice(1);
  record.attempt = { kind: 'dispatching', queueItem, command, attempt };
  replay(record);
}

export function holdAttempt(
  record: ResumeRecord,
  state: Exclude<AttemptState, { kind: 'dispatching' }>,
): void {
  if (record.attempt?.queueItem.id !== state.queueItem.id) return;
  record.attempt = copy(state);
  replay(record);
}

export function replaceHead(record: ResumeRecord, item: EditorQueueItem): void {
  if (record.attempt !== null) return;
  if (record.pending[0] === undefined) return;
  record.pending = [item, ...record.pending.slice(1)];
  replay(record);
}

export function dropHead(record: ResumeRecord, itemId: string): void {
  if (record.attempt?.queueItem.id === itemId) {
    record.attempt = null;
  } else if (record.pending[0]?.id === itemId) {
    record.pending = record.pending.slice(1);
  } else {
    return;
  }
  replay(record);
}

export function markConflict(
  record: ResumeRecord,
  conflict: ConflictRecord,
): void {
  const existing = record.conflicts.findIndex(
    (candidate) => candidate.id === conflict.id,
  );
  record.conflicts = existing === -1
    ? [...record.conflicts, copy(conflict)]
    : record.conflicts.map((candidate, index) =>
        index === existing ? copy(conflict) : candidate,
      );
  replay(record);
}

export function resolveConflict(
  record: ResumeRecord,
  conflictId: string,
): void {
  const conflicts = record.conflicts.filter(
    (conflict) => conflict.id !== conflictId,
  );
  if (conflicts.length === record.conflicts.length) return;
  record.conflicts = conflicts;
  replay(record);
}

export function continueTemplateGroup(
  record: ResumeRecord,
  groupId: string,
): void {
  const active = record.attempt;
  if (
    active === null
    || active === undefined
    || active.queueItem.kind !== 'templateGroup'
    || active.queueItem.id !== groupId
  ) {
    return;
  }
  record.attempt = null;
  record.pending = [active.queueItem, ...record.pending];
  replay(record);
}

export function setIssues(
  record: ResumeRecord,
  itemId: string,
  issues: readonly ServerValidationIssue[],
): void {
  const next = issues.length === 0
    ? Object.fromEntries(
        Object.entries(record.issues).filter(([id]) => id !== itemId),
      )
    : { ...record.issues, [itemId]: copy(issues) };
  record.issues = next;
}

export function setTemplateState(
  record: ResumeRecord,
  state: TemplateGroupState | null,
): void {
  record.templateState = state === null ? null : copy(state);
  replay(record);
}

export function discardLocal(record: ResumeRecord): void {
  record.pending = [];
  record.attempt = null;
  record.conflicts = [];
  record.issues = {};
  record.templateState = null;
  record.sessionLost = false;
  record.opaquePhotoOutcome = null;
  record.discarded = true;
  replay(record);
}
