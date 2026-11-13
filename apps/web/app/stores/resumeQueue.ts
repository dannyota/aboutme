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
  const head = record.pending[0];
  if (
    record.attempt !== null
    || head === undefined
    || toRaw(head) !== toRaw(queueItem)
  ) {
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
  const held = record.attempt;
  if (held?.queueItem.id !== state.queueItem.id) return;
  const alert = held.kind === 'unknown' && held.alert === true
    && state.kind === 'unknown' && state.reason !== 'cutoff';
  record.attempt = copy(alert ? { ...state, alert: true as const } : state);
  replay(record);
}

export function markSending(
  record: ResumeRecord,
  idempotencyKey: string,
  sending: boolean,
): void {
  const held = record.attempt;
  if (
    held === null
    || held.kind === 'dispatching'
    || held.attempt.idempotencyKey !== idempotencyKey
    || (held.sending === true) === sending
  ) {
    return;
  }
  const { sending: _sending, ...rest } = copy(held);
  record.attempt = sending ? { ...rest, sending: true } : rest;
}

export function markHeldAlert(record: ResumeRecord, itemId: string): void {
  const held = record.attempt;
  if (held?.kind !== 'unknown' || held.queueItem.id !== itemId) return;
  if (held.reason === 'cutoff' || held.alert === true) return;
  record.attempt = { ...copy(held), alert: true };
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

export function dropCommands(
  record: ResumeRecord,
  commandIds: ReadonlySet<string>,
): void {
  const pending = (toRaw(record.pending) as readonly EditorQueueItem[])
    .filter((item) => !commandIds.has(item.id));
  const held = record.attempt !== null
    && commandIds.has(record.attempt.queueItem.id);
  if (pending.length === record.pending.length && !held) return;
  record.pending = pending as typeof record.pending;
  if (held) record.attempt = null;
  record.conflicts = record.conflicts.filter(
    (conflict) => !commandIds.has(conflict.id),
  );
  record.issues = Object.fromEntries(
    Object.entries(record.issues).filter(([id]) => !commandIds.has(id)),
  );
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
