import type { Resume } from '@aboutme/schema';
import type { CURRENT_VERSION } from '@aboutme/schema/released';

import type { components } from '../api/generated/openapi';
import type {
  AcceptedResume,
  ParentETag,
  Revision,
  ResumeMetadata,
} from './types';

export type ResumeOperation
  = | 'createResume'
    | 'updateResumeMetadata'
    | 'upsertResumeEntry'
    | 'deleteResumeEntry'
    | 'updateResumeSection'
    | 'updateResumeStructure'
    | 'updateResumePersonalDetails'
    | 'updateResumeCustomization'
    | 'uploadResumePhoto'
    | 'updateResumePhotoCrop'
    | 'deleteResumePhoto'
    | 'deleteResume';

export type AttemptPayload
  = | { readonly kind: 'json'; readonly utf8: string }
    | { readonly kind: 'empty' }
    | { readonly kind: 'photo'; readonly file: File };

export interface FrozenAttempt {
  readonly id: string;
  readonly operation: ResumeOperation;
  readonly url: string;
  readonly method: 'POST' | 'PATCH' | 'DELETE';
  readonly schemaVersion: typeof CURRENT_VERSION;
  readonly ifMatch?: ParentETag;
  readonly idempotencyKey: string;
  readonly payload: AttemptPayload;
  readonly firstDispatchAt: number;
  readonly retryCutoff: number;
  readonly automaticReplays: 0 | 1;
  readonly staleRebases: 0 | 1;
}

export interface ValidatedStaleWinner {
  readonly document: Resume;
  readonly revision: Revision;
}

export type AttemptFailureCode
  = | 'bad_request'
    | 'body_too_large'
    | 'customization_path_denied'
    | 'idempotency_key_invalid'
    | 'idempotency_key_required'
    | 'invalid_client_ip'
    | 'media_invalid'
    | 'media_not_found'
    | 'media_too_large'
    | 'media_type_unsupported'
    | 'method_not_allowed'
    | 'not_found'
    | 'precondition_malformed'
    | 'precondition_not_supported'
    | 'precondition_required'
    | 'request_invalid'
    | 'response_invalid'
    | 'resume_cap_exceeded'
    | 'resume_not_found'
    | 'unsupported_schema_version';

type GeneratedServerValidationIssue = NonNullable<
  NonNullable<components['schemas']['Error']['error']['details']>['issues']
>[number];

export type ServerValidationIssue = Readonly<
  Pick<GeneratedServerValidationIssue, 'path' | 'code'>
>;

export type AttemptResult
  = | {
    readonly kind: 'complete';
    readonly status: 200 | 201;
    readonly accepted: AcceptedResume;
  }
  | {
    readonly kind: 'child-ack';
    readonly status: 204;
    readonly scope: 'entry' | 'photo';
    readonly etag: ParentETag;
  }
  | { readonly kind: 'resume-deleted'; readonly status: 204 }
  | {
    readonly kind: 'stale';
    readonly status: 412;
    readonly winner: ValidatedStaleWinner;
  }
  | { readonly kind: 'csrf-rejected' }
  | { readonly kind: 'session-lost' }
  | {
    readonly kind: 'validation-rejected';
    readonly issues: readonly ServerValidationIssue[];
  }
  | { readonly kind: 'rate-limited'; readonly retryAfterMs: number | null }
  | { readonly kind: 'media-busy'; readonly retryAfterMs: number | null }
  | { readonly kind: 'idempotency-reuse' }
  | { readonly kind: 'rejected'; readonly code: AttemptFailureCode }
  | { readonly kind: 'unknown'; readonly reason: 'transport' | 'server' };

export interface ResumeSummary extends ResumeMetadata {
  readonly revision: Revision;
}

export type ResumeListResult
  = | { readonly kind: 'ready'; readonly items: readonly ResumeSummary[] }
    | { readonly kind: 'session-lost' }
    | { readonly kind: 'rate-limited'; readonly retryAfterMs: number | null }
    | {
      readonly kind: 'failed';
      readonly reason: 'network' | 'response-invalid';
    };

export type ResumeReadResult
  = | { readonly kind: 'complete'; readonly accepted: AcceptedResume }
    | { readonly kind: 'unavailable' }
    | { readonly kind: 'session-lost' }
    | { readonly kind: 'rate-limited'; readonly retryAfterMs: number | null }
    | {
      readonly kind: 'failed';
      readonly reason: 'network' | 'response-invalid';
    };

export type ObjectETag = string & { readonly __objectETag: unique symbol };

export type OwnerPhotoReadResult
  = | {
    readonly kind: 'bytes';
    readonly mime: 'image/jpeg' | 'image/png';
    readonly etag: ObjectETag;
    readonly bytes: Uint8Array;
  }
  | { readonly kind: 'not-modified'; readonly etag: ObjectETag }
  | {
    readonly kind: 'unavailable';
    readonly reason: 'not-found' | 'session-lost' | 'invalid' | 'network';
  };
