import { replayCommand } from '../editor/commands';
import { cloneReactiveSafe } from '../editor/clone';
import type {
  AttemptFailureCode,
  FrozenAttempt,
  ObjectETag,
  ServerValidationIssue,
} from '../editor/attempt';
import type { AtomicEditorCommand } from '../editor/commands';
import type { ConflictRecord } from '../editor/reconcile';
import type {
  EditorQueueItem,
  TemplateGroupState,
} from '../editor/templateGroup';
import { templateUndoAvailable } from '../editor/templateGroup';
import type {
  AcceptedResume,
  ResumeSnapshot,
  SaveState,
} from '../editor/types';

// The editor's record of one resume: what the server last accepted, the
// local queue replayed over it, and the state the editor shows for both.

export type PhotoReadState
  = | { readonly kind: 'none' }
    | {
      readonly kind: 'loading';
      readonly binding: string;
      readonly generation: number;
    }
    | {
      readonly kind: 'ready';
      readonly binding: string;
      readonly generation: number;
      readonly etag: ObjectETag;
      readonly dataUrl: string;
    }
    | {
      readonly kind: 'suspended';
      readonly binding: string;
      readonly generation: number;
      readonly reason: 'read-failed' | 'binding-mismatch' | 'session-lost';
    };

export type AttemptState
  = | {
    readonly kind: 'dispatching';
    readonly queueItem: EditorQueueItem;
    readonly command: AtomicEditorCommand;
    readonly attempt: FrozenAttempt;
  }
  | {
    readonly kind: 'unknown';
    readonly queueItem: EditorQueueItem;
    readonly command: AtomicEditorCommand;
    readonly attempt: FrozenAttempt;
    readonly reason: 'transport' | 'server' | 'cutoff';
    /** The write has been failing long enough to tell the person. */
    readonly alert?: true;
  }
  | {
    readonly kind: 'retry-later';
    readonly queueItem: EditorQueueItem;
    readonly command: AtomicEditorCommand;
    readonly attempt: FrozenAttempt;
    readonly reason: 'rate-limited' | 'media-busy';
    readonly retryAfterMs: number | null;
  }
  | {
    readonly kind: 'failed';
    readonly queueItem: EditorQueueItem;
    readonly command: AtomicEditorCommand;
    readonly attempt: FrozenAttempt;
    readonly reason:
      | AttemptFailureCode
      | 'csrf-rejected'
      | 'idempotency-reuse'
      | 'stale-limit';
  };

export type OpaquePhotoOutcome = {
  readonly kind: 'photo-cutoff';
  readonly command: Extract<AtomicEditorCommand, { kind: 'photoUpload' }>;
  readonly attempt: FrozenAttempt;
  readonly observed: 'unchanged' | 'changed' | 'unavailable';
};

export type CompletionAdoption
  = | { readonly kind: 'adopted'; readonly accepted: AcceptedResume }
    | { readonly kind: 'older'; readonly winner: AcceptedResume };

export interface ResumeRecord {
  accepted: AcceptedResume;
  current: ResumeSnapshot;
  pending: readonly EditorQueueItem[];
  attempt: AttemptState | null;
  conflicts: readonly ConflictRecord[];
  issues: Readonly<Record<string, readonly ServerValidationIssue[]>>;
  templateState: TemplateGroupState | null;
  photoRead: PhotoReadState;
  completeReadRequired: boolean;
  /** Set by an explicit discard and cleared by the next queued edit. */
  discarded: boolean;
  sessionLost: boolean;
  opaquePhotoOutcome: OpaquePhotoOutcome | null;
}

export function createRecord(accepted: AcceptedResume): ResumeRecord {
  const copied = cloneAccepted(accepted);
  return {
    accepted: copied,
    current: snapshotFromAccepted(copied),
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' },
    completeReadRequired: false,
    discarded: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}

export function nextSequence(record: ResumeRecord): number {
  const sequences = [
    ...(record.attempt === null ? [] : [record.attempt.queueItem.sequence]),
    ...record.pending.map((item) => item.sequence),
  ];
  return (sequences.length === 0 ? 0 : Math.max(...sequences)) + 1;
}

export function dependencyIdsForNewCommand(
  record: ResumeRecord,
): readonly string[] {
  return [
    ...(record.attempt === null ? [] : [record.attempt.queueItem.id]),
    ...record.pending.map((item) => item.id),
  ];
}

export function replay(value: unknown): void {
  const record = value as ResumeRecord;
  const active = record.attempt === null ? [] : [record.attempt.queueItem];
  record.current = [...active, ...record.pending].reduce(
    replayQueueItem,
    snapshotFromAccepted(record.accepted),
  );
  if (
    record.templateState?.kind === 'complete'
    && !templateUndoAvailable(record.templateState.undo, record.current)
  ) {
    record.templateState = null;
  }
  clearPhotoWhenUnbound(record);
}

function replayQueueItem(
  snapshot: ResumeSnapshot,
  item: EditorQueueItem,
): ResumeSnapshot {
  return isAtomic(item)
    ? copy(replayCommand(snapshot, item))
    : copy(item.intendedFinal);
}

export function saveState(record: ResumeRecord): SaveState {
  if (record.sessionLost) return 'session-lost';
  if (record.conflicts.length > 0) return 'conflict';
  if (record.attempt?.kind === 'unknown') {
    // A held write is retried on its own; it is only reported as failed
    // once the alert is up or the retry window has closed.
    if (record.attempt.reason !== 'cutoff' && !record.attempt.alert) {
      return 'retrying';
    }
    return record.attempt.reason === 'transport' ? 'offline' : 'error';
  }
  if (
    record.attempt?.kind === 'failed'
    || record.attempt?.kind === 'retry-later'
    || record.templateState?.kind === 'partial'
    || record.opaquePhotoOutcome !== null
    || Object.keys(record.issues).length > 0
  ) {
    return 'error';
  }
  if (record.attempt?.kind === 'dispatching') return 'saving';
  if (record.pending.length > 0 || record.completeReadRequired) return 'dirty';
  return 'saved';
}

export function snapshotFromAccepted(
  accepted: AcceptedResume,
): ResumeSnapshot {
  const copied = copy(accepted);
  return { document: copied.document, metadata: copied.metadata };
}

export function cloneAccepted(accepted: AcceptedResume): AcceptedResume {
  return copy(accepted);
}

export function copy<T>(value: T): T {
  return cloneReactiveSafe(value);
}

export function isAtomic(item: EditorQueueItem): item is AtomicEditorCommand {
  return item.kind !== 'templateGroup';
}

export function photoBinding(snapshot: ResumeSnapshot): string | undefined {
  return snapshot.document.personalDetails.photo?.key;
}

function clearPhotoWhenUnbound(record: ResumeRecord): void {
  if (
    record.photoRead.kind === 'ready'
    && record.photoRead.binding !== photoBinding(record.current)
  ) {
    record.photoRead = { kind: 'none' };
  }
}
