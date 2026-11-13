import type { useAuth } from '../composables/useAuth';
import type { useResumeStore } from '../stores/resumes';
import type { CreateResumeIntent } from './commands';
import type {
  AttemptFailureCode,
  FrozenAttempt,
  ResumeReadResult,
  ResumeSummary,
} from './attempt';
import type { ConflictConfirmation } from './reconcile';
import type { ResumeApi, ResumeConditionalReadResult } from './resumeApi';
import type { AcceptedResume, EditorRuntime, ParentETag } from './types';

export type CreateResumeResult
  = | { readonly kind: 'created'; readonly resume: AcceptedResume }
    | {
      readonly kind: 'blocked';
      readonly intentId: string;
      readonly reason: 'unknown' | 'session-lost';
    }
    | {
      readonly kind: 'retry-later';
      readonly intentId: string;
      readonly retryAfterMs: number | null;
    }
    | { readonly kind: 'opaque-create'; readonly outcome: OpaqueCreateOutcome }
    | { readonly kind: 'rejected'; readonly code: AttemptFailureCode };

export interface OpaqueCreateOutcome {
  readonly kind: 'create-cutoff';
  readonly intent: CreateResumeIntent;
  readonly attempt: FrozenAttempt;
  readonly refreshedItems: readonly ResumeSummary[] | null;
}

export type OpaquePhotoDecision
  = | { readonly kind: 'keep-observed' }
    | { readonly kind: 'replace'; readonly file: File };

export interface ResumeMutationCoordinator {
  createResume(intent: CreateResumeIntent): Promise<CreateResumeResult>;
  retryCreate(intentId: string): Promise<CreateResumeResult>;
  refreshOpaqueCreate(intentId: string): Promise<OpaqueCreateOutcome>;
  abandonOpaqueCreate(intentId: string): void;
  schedule(resumeId: string): void;
  flush(resumeId: string): Promise<void>;
  completeRead(resumeId: string): Promise<boolean>;
  retry(resumeId: string, commandId: string): Promise<void>;
  refreshAndReconcile(resumeId: string): Promise<ResumeReadResult>;
  refreshConditionalAndReconcile?: (
    resumeId: string,
    etag?: ParentETag,
  ) => Promise<ResumeConditionalReadResult>;
  acceptLatest(resumeId: string, conflictId: string): Promise<void>;
  applyMine(
    resumeId: string,
    conflictId: string,
    confirmation: ConflictConfirmation,
  ): Promise<void>;
  resumeAfterAuth(resumeId: string): Promise<void>;
  /** Goes on with the queue once the session is the owner's again. */
  wake(resumeId: string, recovered: boolean): Promise<void>;
  /** Ends all work on a resume whose editor closed. */
  stop(resumeId: string): void;
  resolveOpaquePhoto(
    resumeId: string,
    commandId: string,
    decision: OpaquePhotoDecision,
  ): Promise<void>;
  discard(resumeId: string): void;
}

/** A create intent retained until the server accepts it or the caller
 * abandons it, so retries reuse the same frozen attempt. */
export interface RetainedCreate {
  readonly intent: CreateResumeIntent;
  attempt: FrozenAttempt;
  outcome?: OpaqueCreateOutcome;
}

/** The coordinator's fixed collaborators: the transport, the local store,
 * auth state, and the runtime clock/uuid/delay hooks. */
export interface CoordinatorDeps {
  api: ResumeApi;
  store: ReturnType<typeof useResumeStore>;
  auth: ReturnType<typeof useAuth>;
  runtime: EditorRuntime;
}

/** Mutable state shared across the coordinator's split modules: one
 * instance per `createMutationCoordinator` call. */
export interface CoordinatorContext {
  deps: CoordinatorDeps;
  timers: Map<string, ReturnType<typeof setTimeout>>;
  drains: Map<string, Promise<void>>;
  retainedCreates: Map<string, RetainedCreate>;
  csrfRetries: Set<string>;
  /**
   * Failed completing reads in a row and the epoch time before which the
   * read may not run again.
   */
  readRetries: Map<string, { failures: number; at: number }>;
  /** Resumes whose editor closed; nothing more is scheduled for them. */
  stopped: Set<string>;
  /** When and how the write the queue holds may go out again. */
  writeRetries: Map<string, WriteRetry>;
}

/**
 * The schedule of the write a resume's queue holds. Only the drain reads it
 * to send; everything else only changes it and wakes the drain.
 */
export interface WriteRetry {
  /** The queue item of the held write; a schedule for another is stale. */
  readonly itemId: string;
  /** Unknown outcomes after the quick replay; they set the backoff. */
  failures: number;
  /** Epoch ms before which the next automatic retry may not go. */
  at: number;
  /** Epoch ms before which nothing may be sent: the server's Retry-After. */
  notBefore: number;
  /** A wait the drain sleeps itself first: the quick replay or a rebase. */
  wait: number;
  /** The write is a rebase built from a 412 winner, so it needs no read. */
  rebase: boolean;
  /** The person, or a session signed in again, asked to send it again. */
  requested: boolean;
  /** Raises the alert once the outcome has stayed unknown long enough. */
  alert?: ReturnType<typeof setTimeout>;
}

/**
 * The context of one operation: a drain, a retry, a read, or a conflict
 * resolution. It carries the editor record the operation started on, so a
 * continuation can tell whether that record is still the one being edited.
 */
export interface OperationContext extends CoordinatorContext {
  readonly epoch: { readonly record: object | undefined };
}
