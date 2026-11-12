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
}
