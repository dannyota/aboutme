import { toRaw } from 'vue';

import { replayCommand } from '../editor/commands';
import { equalProjection } from '../editor/projections';
import { compareRevision, parseRevision } from '../editor/revision';
import type { AtomicEditorCommand } from '../editor/commands';
import type { EditorQueueItem } from '../editor/templateGroup';
import type { AcceptedResume, ParentETag } from '../editor/types';
import {
  cloneAccepted,
  copy,
  isAtomic,
  replay,
  snapshotFromAccepted,
  type AttemptState,
  type CompletionAdoption,
  type ResumeRecord,
} from './resumeRecord';

export function adoptComplete(
  record: ResumeRecord | undefined,
  accepted: AcceptedResume,
): CompletionAdoption {
  if (record === undefined) {
    return { kind: 'older', winner: cloneAccepted(accepted) };
  }
  if (compareRevision(accepted.revision, record.accepted.revision) <= 0) {
    return { kind: 'older', winner: record.accepted };
  }
  record.accepted = cloneAccepted(accepted);
  record.completeReadRequired = false;
  replay(record);
  return { kind: 'adopted', accepted: cloneAccepted(record.accepted) };
}

export function adoptCompleteRead(
  record: ResumeRecord | undefined,
  accepted: AcceptedResume,
): CompletionAdoption {
  if (record === undefined) {
    return { kind: 'older', winner: cloneAccepted(accepted) };
  }
  if (compareRevision(accepted.revision, record.accepted.revision) < 0) {
    return { kind: 'older', winner: record.accepted };
  }
  record.accepted = cloneAccepted(accepted);
  record.completeReadRequired = false;
  replay(record);
  return { kind: 'adopted', accepted: cloneAccepted(record.accepted) };
}

export function adoptStaleWinner(
  record: ResumeRecord,
  accepted: AcceptedResume,
): void {
  if (compareRevision(accepted.revision, record.accepted.revision) <= 0) {
    return;
  }
  record.accepted = {
    ...copy(record.accepted),
    document: copy(accepted.document),
    revision: accepted.revision,
    metadataFreshness: 'stale',
  };
  record.completeReadRequired = true;
  replay(record);
}

export function requireCompleteRead(record: ResumeRecord): void {
  record.completeReadRequired = true;
}

export function acknowledgeChild(
  record: ResumeRecord,
  itemId: string,
  etag: ParentETag,
): void {
  if (record.attempt === null) return;
  const active = record.attempt as AttemptState;
  if (active.queueItem.id !== itemId || !isBodylessChild(active.command)) {
    return;
  }
  const revision = revisionFromParentETag(etag);
  if (compareRevision(revision, record.accepted.revision) <= 0) return;
  const reduced = replayCommand(
    snapshotFromAccepted(record.accepted),
    toRaw(active.command) as AtomicEditorCommand,
  );
  record.accepted = {
    ...copy(record.accepted),
    document: copy(reduced.document),
    revision,
    metadataFreshness: 'stale',
  };
  record.completeReadRequired = true;
  replay(record);
}

export function replaceConflictedAfterCompleteRead(
  record: ResumeRecord,
  conflicted: AtomicEditorCommand,
  accepted: AcceptedResume,
  replacement: EditorQueueItem,
): boolean {
  if (compareRevision(accepted.revision, record.accepted.revision) < 0) {
    return false;
  }
  const active = record.attempt?.queueItem.id === conflicted.id;
  if (!active && !unchangedHead(record, conflicted)) return false;
  record.accepted = cloneAccepted(accepted);
  record.completeReadRequired = false;
  record.attempt = null;
  record.pending = [
    replacement,
    ...(active ? record.pending : record.pending.slice(1)),
  ];
  replay(record);
  return true;
}

function unchangedHead(record: ResumeRecord, command: AtomicEditorCommand) {
  const head = record.pending[0];
  if (record.attempt !== null || head?.id !== command.id) return false;
  const intended = isAtomic(head) ? head.intended : null;
  return intended === null || command.intended === null
    ? intended === command.intended
    : equalProjection(intended, command.intended);
}

function isBodylessChild(command: AtomicEditorCommand): boolean {
  return command.kind === 'entryDelete' || command.kind === 'photoDelete';
}

function revisionFromParentETag(etag: ParentETag) {
  return parseRevision(etag.slice(2, -1));
}
