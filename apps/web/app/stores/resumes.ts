import { defineStore } from 'pinia';
import { toRaw } from 'vue';

import { coalescePending, replayCommand } from '../editor/commands';
import { compareRevision, parseRevision } from '../editor/revision';
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
  ParentETag,
  ResumeSnapshot,
  SaveState,
} from '../editor/types';

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
      | 'second-stale';
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
  sessionLost: boolean;
  opaquePhotoOutcome: OpaquePhotoOutcome | null;
}

export interface ResumeStoreState {
  records: Map<string, ResumeRecord>;
}

export interface ResumeStoreActions {
  initialize(accepted: AcceptedResume): void;
  enqueue(resumeId: string, item: EditorQueueItem): void;
  startAttempt(
    resumeId: string,
    queueItem: EditorQueueItem,
    command: AtomicEditorCommand,
    attempt: FrozenAttempt,
  ): void;
  holdAttempt(
    resumeId: string,
    state: Exclude<AttemptState, { kind: 'dispatching' }>,
  ): void;
  adoptComplete(resumeId: string, accepted: AcceptedResume): CompletionAdoption;
  adoptCompleteRead(
    resumeId: string,
    accepted: AcceptedResume,
  ): CompletionAdoption;
  adoptStaleWinner(resumeId: string, accepted: AcceptedResume): void;
  acknowledgeChild(resumeId: string, itemId: string, etag: ParentETag): void;
  acknowledgeResumeDelete(resumeId: string, itemId: string): void;
  replaceHead(resumeId: string, item: EditorQueueItem): void;
  dropHead(resumeId: string, itemId: string): void;
  markConflict(resumeId: string, conflict: ConflictRecord): void;
  resolveConflict(resumeId: string, conflictId: string): void;
  continueTemplateGroup(resumeId: string, groupId: string): void;
  setIssues(
    resumeId: string,
    itemId: string,
    issues: readonly ServerValidationIssue[],
  ): void;
  setTemplateState(resumeId: string, state: TemplateGroupState | null): void;
  setPhotoRead(resumeId: string, state: PhotoReadState): void;
  markSessionLost(resumeId: string): void;
  clearSessionLost(resumeId: string): void;
  setOpaquePhotoOutcome(
    resumeId: string,
    state: OpaquePhotoOutcome | null,
  ): void;
  discardLocal(resumeId: string): void;
  removeResume(resumeId: string): void;
}

interface ResumeStore extends ResumeStoreState, ResumeStoreActions {
  recordFor(resumeId: string): ResumeRecord | undefined;
  saveStateFor(resumeId: string): SaveState;
}

const resumeStore = defineStore('resumes', {
  state: () => ({ records: new Map<string, unknown>() }),

  getters: {
    recordFor:
      (state) =>
        (resumeId: string): ResumeRecord | undefined =>
          state.records.get(resumeId) as ResumeRecord | undefined,
    saveStateFor:
      (state) =>
        (resumeId: string): SaveState => {
          const record = state.records.get(resumeId) as
            | ResumeRecord
            | undefined;
          return record === undefined ? 'idle' : saveState(record);
        },
  },

  actions: {
    initialize(accepted: AcceptedResume) {
      const copied = cloneAccepted(accepted);
      this.records.set(copied.metadata.id, {
        accepted: copied,
        current: snapshotFromAccepted(copied),
        pending: [],
        attempt: null,
        conflicts: [],
        issues: {},
        templateState: null,
        photoRead: { kind: 'none' },
        completeReadRequired: false,
        sessionLost: false,
        opaquePhotoOutcome: null,
      });
    },

    enqueue(resumeId: string, item: EditorQueueItem) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      const pending = toRaw(record.pending) as readonly EditorQueueItem[];
      record.pending = (isAtomic(item)
        ? coalesceAtomicPending(pending, item)
        : [...pending, item]) as unknown as typeof record.pending;
      replay(record);
    },

    startAttempt(
      resumeId: string,
      queueItem: EditorQueueItem,
      command: AtomicEditorCommand,
      attempt: FrozenAttempt,
    ) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (
        record === undefined
        || record.attempt !== null
        || record.pending[0]?.id !== queueItem.id
      ) {
        return;
      }
      record.pending = record.pending.slice(1);
      record.attempt = { kind: 'dispatching', queueItem, command, attempt };
      replay(record);
    },

    holdAttempt(
      resumeId: string,
      state: Exclude<AttemptState, { kind: 'dispatching' }>,
    ) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (
        record === undefined
        || record.attempt?.queueItem.id !== state.queueItem.id
      ) {
        return;
      }
      record.attempt = copy(state);
      replay(record);
    },

    adoptComplete(
      resumeId: string,
      accepted: AcceptedResume,
    ): CompletionAdoption {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) {
        return { kind: 'older', winner: cloneAccepted(accepted) };
      }
      if (compareRevision(accepted.revision, record.accepted.revision) <= 0) {
        return { kind: 'older', winner: record.accepted };
      }
      record.accepted = cloneAccepted(accepted);
      record.completeReadRequired = false;
      replay(record);
      return { kind: 'adopted', accepted: record.accepted };
    },

    adoptCompleteRead(
      resumeId: string,
      accepted: AcceptedResume,
    ): CompletionAdoption {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) {
        return { kind: 'older', winner: cloneAccepted(accepted) };
      }
      if (compareRevision(accepted.revision, record.accepted.revision) < 0) {
        return { kind: 'older', winner: record.accepted };
      }
      record.accepted = cloneAccepted(accepted);
      record.completeReadRequired = false;
      replay(record);
      return { kind: 'adopted', accepted: record.accepted };
    },

    adoptStaleWinner(resumeId: string, accepted: AcceptedResume) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (
        record === undefined
        || compareRevision(accepted.revision, record.accepted.revision) <= 0
      ) {
        return;
      }
      record.accepted = {
        ...copy(record.accepted),
        document: copy(accepted.document),
        revision: accepted.revision,
        metadataFreshness: 'stale',
      };
      replay(record);
    },

    acknowledgeChild(resumeId: string, itemId: string, etag: ParentETag) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined || record.attempt === null) return;
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
    },

    acknowledgeResumeDelete(resumeId: string, itemId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (
        record?.attempt?.queueItem.id !== itemId
        || record.attempt.command.kind !== 'resumeDelete'
      ) {
        return;
      }
      this.records.delete(resumeId);
    },

    replaceHead(resumeId: string, item: EditorQueueItem) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined || record.attempt !== null) return;
      if (record.pending[0] === undefined) return;
      record.pending = [item, ...record.pending.slice(1)];
      replay(record);
    },

    dropHead(resumeId: string, itemId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      if (record.attempt?.queueItem.id === itemId) {
        record.attempt = null;
      } else if (record.pending[0]?.id === itemId) {
        record.pending = record.pending.slice(1);
      } else {
        return;
      }
      replay(record);
    },

    markConflict(resumeId: string, conflict: ConflictRecord) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      record.conflicts = [...record.conflicts, copy(conflict)];
      replay(record);
    },

    resolveConflict(resumeId: string, conflictId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      const conflicts = record.conflicts.filter(
        (conflict) => conflict.id !== conflictId,
      );
      if (conflicts.length === record.conflicts.length) return;
      record.conflicts = conflicts;
      replay(record);
    },

    continueTemplateGroup(resumeId: string, groupId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
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
    },

    setIssues(
      resumeId: string,
      itemId: string,
      issues: readonly ServerValidationIssue[],
    ) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      const next = issues.length === 0
        ? Object.fromEntries(
            Object.entries(record.issues).filter(([id]) => id !== itemId),
          )
        : { ...record.issues, [itemId]: copy(issues) };
      record.issues = next;
    },

    setTemplateState(resumeId: string, state: TemplateGroupState | null) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      record.templateState = state === null ? null : copy(state);
      replay(record);
    },

    setPhotoRead(resumeId: string, state: PhotoReadState) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      if (state.kind === 'none') {
        record.photoRead = { kind: 'none' };
        return;
      }
      if (
        record.photoRead.kind !== 'none'
        && state.generation < record.photoRead.generation
      ) {
        return;
      }
      if (state.binding !== photoBinding(record.current)) {
        record.photoRead = {
          kind: 'suspended',
          binding: state.binding,
          generation: state.generation,
          reason: 'binding-mismatch',
        };
        return;
      }
      record.photoRead = copy(state);
    },

    markSessionLost(resumeId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      record.sessionLost = true;
      if (record.photoRead.kind !== 'none') {
        record.photoRead = {
          kind: 'suspended',
          binding: record.photoRead.binding,
          generation: record.photoRead.generation,
          reason: 'session-lost',
        };
      }
      replay(record);
    },

    clearSessionLost(resumeId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      record.sessionLost = false;
      replay(record);
    },

    setOpaquePhotoOutcome(resumeId: string, state: OpaquePhotoOutcome | null) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      record.opaquePhotoOutcome = state === null ? null : copy(state);
    },

    discardLocal(resumeId: string) {
      const record = this.records.get(resumeId) as unknown as
        | ResumeRecord
        | undefined;
      if (record === undefined) return;
      record.pending = [];
      record.attempt = null;
      record.conflicts = [];
      record.issues = {};
      record.templateState = null;
      record.sessionLost = false;
      record.opaquePhotoOutcome = null;
      replay(record);
    },

    removeResume(resumeId: string) {
      this.records.delete(resumeId);
    },
  },
});

export const useResumeStore = resumeStore as unknown as () => ResumeStore;

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

function replay(value: unknown): void {
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

function coalesceAtomicPending(
  pending: readonly EditorQueueItem[],
  item: AtomicEditorCommand,
): readonly EditorQueueItem[] {
  const last = pending.at(-1);
  if (last === undefined || !isAtomic(last)) return [...pending, item];
  return [...pending.slice(0, -1), ...coalescePending([last], item)];
}

function saveState(record: ResumeRecord): SaveState {
  if (record.sessionLost) return 'session-lost';
  if (record.conflicts.length > 0) return 'conflict';
  if (record.attempt?.kind === 'unknown') {
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

function snapshotFromAccepted(accepted: AcceptedResume): ResumeSnapshot {
  const copied = copy(accepted);
  return { document: copied.document, metadata: copied.metadata };
}

function cloneAccepted(accepted: AcceptedResume): AcceptedResume {
  return copy(accepted);
}

function copy<T>(value: T): T {
  return structuredClone(toRaw(value)) as T;
}

function isAtomic(item: EditorQueueItem): item is AtomicEditorCommand {
  return item.kind !== 'templateGroup';
}

function isBodylessChild(command: AtomicEditorCommand): boolean {
  return command.kind === 'entryDelete' || command.kind === 'photoDelete';
}

function revisionFromParentETag(etag: ParentETag) {
  return parseRevision(etag.slice(2, -1));
}

function photoBinding(snapshot: ResumeSnapshot): string | undefined {
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
