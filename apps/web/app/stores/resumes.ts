import { defineStore } from 'pinia';

import type { FrozenAttempt, ServerValidationIssue } from '../editor/attempt';
import type { AtomicEditorCommand } from '../editor/commands';
import type { ConflictRecord } from '../editor/reconcile';
import type {
  EditorQueueItem,
  TemplateGroupState,
} from '../editor/templateGroup';
import type { AcceptedResume, ParentETag, SaveState } from '../editor/types';
import * as adoption from './resumeAdoption';
import * as queue from './resumeQueue';
import {
  copy,
  createRecord,
  photoBinding,
  replay,
  saveState,
  type AttemptState,
  type CompletionAdoption,
  type OpaquePhotoOutcome,
  type PhotoReadState,
  type ResumeRecord,
} from './resumeRecord';

export type { PhotoReadState, ResumeRecord } from './resumeRecord';
export { dependencyIdsForNewCommand, nextSequence } from './resumeRecord';

interface ResumeStoreState {
  records: Map<string, ResumeRecord>;
  /** Editor pages open on each resume; other pages leave its record alone. */
  editors: Map<string, number>;
  /** Times another page left a resume's record to its open editor. */
  handovers: Map<string, number>;
}

interface ResumeStoreActions {
  /**
   * Starts a record from a complete read, unless the one held for the
   * resume has unsent or in-flight work, which nothing replaces. Returns
   * whether it started the record.
   */
  initialize(accepted: AcceptedResume): boolean;
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
  markHeldAlert(resumeId: string, itemId: string): void;
  markSending(resumeId: string, idempotencyKey: string, sending: boolean): void;
  adoptComplete(resumeId: string, accepted: AcceptedResume): CompletionAdoption;
  adoptCompleteRead(
    resumeId: string,
    accepted: AcceptedResume,
  ): CompletionAdoption;
  adoptStaleWinner(resumeId: string, accepted: AcceptedResume): void;
  requireCompleteRead(resumeId: string): void;
  acknowledgeChild(resumeId: string, itemId: string, etag: ParentETag): void;
  acknowledgeResumeDelete(resumeId: string, itemId: string): void;
  replaceConflictedAfterCompleteRead(
    resumeId: string,
    conflicted: AtomicEditorCommand,
    accepted: AcceptedResume,
    replacement: EditorQueueItem,
  ): boolean;
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
  /**
   * Removes the queued or held commands `commandIds`, and nothing else, from
   * the resume's record: the work a page queued and is leaving behind.
   */
  dropCommands(resumeId: string, commandIds: ReadonlySet<string>): void;
  /** An editor page opened on the resume, or closed. */
  markEditorOpen(resumeId: string, open: boolean): void;
  /** Leaves the record to the open editor, whose queue then looks at it. */
  handOver(resumeId: string): void;
  removeResume(resumeId: string): void;
}

interface ResumeStore extends ResumeStoreState, ResumeStoreActions {
  recordFor(resumeId: string): ResumeRecord | undefined;
  saveStateFor(resumeId: string): SaveState;
  /** An editor page has the resume's record open. */
  openInEditor(resumeId: string): boolean;
  handoversFor(resumeId: string): number;
}

/** The store's record for `resumeId`, as its reactive state holds it. */
function recordIn(
  store: { readonly records: Map<string, unknown> },
  resumeId: string,
): ResumeRecord | undefined {
  return store.records.get(resumeId) as ResumeRecord | undefined;
}

const resumeStore = defineStore('resumes', {
  state: () => ({
    records: new Map<string, unknown>(),
    editors: new Map<string, number>(),
    handovers: new Map<string, number>(),
  }),

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
    openInEditor:
      (state) =>
        (resumeId: string): boolean =>
          (state.editors.get(resumeId) ?? 0) > 0,
    handoversFor:
      (state) =>
        (resumeId: string): number =>
          state.handovers.get(resumeId) ?? 0,
  },

  actions: {
    initialize(accepted: AcceptedResume) {
      const held = recordIn(this, accepted.metadata.id);
      const working = held !== undefined
        && (held.pending.length > 0 || held.attempt !== null);
      if (working) return false;
      const record = createRecord(accepted);
      this.records.set(record.accepted.metadata.id, record);
      return true;
    },

    enqueue(resumeId: string, item: EditorQueueItem) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.enqueue(record, item);
    },

    startAttempt(
      resumeId: string,
      queueItem: EditorQueueItem,
      command: AtomicEditorCommand,
      attempt: FrozenAttempt,
    ) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) {
        queue.startAttempt(record, queueItem, command, attempt);
      }
    },

    holdAttempt(
      resumeId: string,
      state: Exclude<AttemptState, { kind: 'dispatching' }>,
    ) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.holdAttempt(record, state);
    },

    markHeldAlert(resumeId: string, itemId: string) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.markHeldAlert(record, itemId);
    },

    markSending(resumeId: string, idempotencyKey: string, sending: boolean) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) {
        queue.markSending(record, idempotencyKey, sending);
      }
    },

    adoptComplete(
      resumeId: string,
      accepted: AcceptedResume,
    ): CompletionAdoption {
      return adoption.adoptComplete(recordIn(this, resumeId), accepted);
    },

    adoptCompleteRead(
      resumeId: string,
      accepted: AcceptedResume,
    ): CompletionAdoption {
      return adoption.adoptCompleteRead(recordIn(this, resumeId), accepted);
    },

    adoptStaleWinner(resumeId: string, accepted: AcceptedResume) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) adoption.adoptStaleWinner(record, accepted);
    },

    requireCompleteRead(resumeId: string) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) adoption.requireCompleteRead(record);
    },

    acknowledgeChild(resumeId: string, itemId: string, etag: ParentETag) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) {
        adoption.acknowledgeChild(record, itemId, etag);
      }
    },

    acknowledgeResumeDelete(resumeId: string, itemId: string) {
      const record = recordIn(this, resumeId);
      if (
        record?.attempt?.queueItem.id !== itemId
        || record.attempt.command.kind !== 'resumeDelete'
      ) {
        return;
      }
      this.records.delete(resumeId);
    },

    replaceConflictedAfterCompleteRead(
      resumeId: string,
      conflicted: AtomicEditorCommand,
      accepted: AcceptedResume,
      replacement: EditorQueueItem,
    ) {
      const record = recordIn(this, resumeId);
      return record !== undefined
        && adoption.replaceConflictedAfterCompleteRead(
          record,
          conflicted,
          accepted,
          replacement,
        );
    },

    replaceHead(resumeId: string, item: EditorQueueItem) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.replaceHead(record, item);
    },

    dropHead(resumeId: string, itemId: string) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.dropHead(record, itemId);
    },

    markConflict(resumeId: string, conflict: ConflictRecord) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.markConflict(record, conflict);
    },

    resolveConflict(resumeId: string, conflictId: string) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.resolveConflict(record, conflictId);
    },

    continueTemplateGroup(resumeId: string, groupId: string) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.continueTemplateGroup(record, groupId);
    },

    setIssues(
      resumeId: string,
      itemId: string,
      issues: readonly ServerValidationIssue[],
    ) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.setIssues(record, itemId, issues);
    },

    setTemplateState(resumeId: string, state: TemplateGroupState | null) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.setTemplateState(record, state);
    },

    setPhotoRead(resumeId: string, state: PhotoReadState) {
      const record = recordIn(this, resumeId);
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
      const record = recordIn(this, resumeId);
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
      const record = recordIn(this, resumeId);
      if (record === undefined) return;
      record.sessionLost = false;
      replay(record);
    },

    setOpaquePhotoOutcome(resumeId: string, state: OpaquePhotoOutcome | null) {
      const record = recordIn(this, resumeId);
      if (record === undefined) return;
      record.opaquePhotoOutcome = state === null ? null : copy(state);
    },

    discardLocal(resumeId: string) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.discardLocal(record);
    },

    dropCommands(resumeId: string, commandIds: ReadonlySet<string>) {
      const record = recordIn(this, resumeId);
      if (record !== undefined) queue.dropCommands(record, commandIds);
    },

    markEditorOpen(resumeId: string, open: boolean) {
      const count = (this.editors.get(resumeId) ?? 0) + (open ? 1 : -1);
      if (count > 0) this.editors.set(resumeId, count);
      else this.editors.delete(resumeId);
    },

    handOver(resumeId: string) {
      this.handovers.set(resumeId, (this.handovers.get(resumeId) ?? 0) + 1);
    },

    removeResume(resumeId: string) {
      this.records.delete(resumeId);
    },
  },
});

export const useResumeStore = resumeStore as unknown as () => ResumeStore;
