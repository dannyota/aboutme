import { CURRENT_VERSION } from '@aboutme/schema/released';

import type { AcceptedResume, EditorRuntime, Revision } from './types';
import type { ServerValidationIssue, ValidatedStaleWinner } from './attempt';
import { parseAcceptedResponse } from './resumeApi';
import { parseCurrentDocument } from './documentValidation';
import { compareRevision, parentETag, parseRevision } from './revision';

export interface PublishCommand {
  readonly slug?: string;
  readonly live: boolean;
  readonly downloadEnabled: boolean;
  readonly seoGeoEnabled: boolean;
}

export interface FrozenPublishAttempt {
  readonly resumeId: string;
  readonly ownerId: string;
  readonly revision: Revision;
  readonly schemaVersion: typeof CURRENT_VERSION;
  readonly idempotencyKey: string;
  readonly command: Readonly<PublishCommand>;
}

export type PublishFailureCode
  = | 'body_too_large'
    | 'invalid_client_ip'
    | 'idempotency_key_invalid'
    | 'idempotency_key_required'
    | 'idempotency_key_reuse'
    | 'method_not_allowed'
    | 'precondition_malformed'
    | 'precondition_required'
    | 'request_invalid'
    | 'resume_not_found'
    | 'unsupported_schema_version';

export type PublishResult
  = | { readonly kind: 'accepted'; readonly resume: AcceptedResume }
    | {
      readonly kind: 'invalid';
      readonly issues: readonly ServerValidationIssue[];
    }
    | { readonly kind: 'reauth-required' }
    | { readonly kind: 'slug-taken' }
    | { readonly kind: 'stale'; readonly winner: ValidatedStaleWinner }
    | { readonly kind: 'rate-limited'; readonly retryAfterMs: number | null }
    | {
      readonly kind: 'public-state-busy';
      readonly retryAfterMs: number | null;
    }
    | { readonly kind: 'session-lost' }
    | { readonly kind: 'failed'; readonly code: PublishFailureCode }
    | { readonly kind: 'unknown'; readonly reason: 'transport' | 'server' };

export type PublishTransportResult
  = PublishResult | { readonly kind: 'csrf-rejected' };

export interface PublishApi {
  dispatch(
    attempt: FrozenPublishAttempt,
    csrfToken: string,
  ): Promise<PublishTransportResult>;
}

export function freezePublishAttempt(
  resumeId: string,
  revision: Revision,
  command: PublishCommand,
  runtime: Pick<EditorRuntime, 'uuid'>,
  ownerId: string,
): FrozenPublishAttempt {
  const frozen = Object.freeze({
    resumeId,
    ownerId,
    revision,
    schemaVersion: CURRENT_VERSION,
    idempotencyKey: runtime.uuid(),
    command: Object.freeze({ ...command }),
  });
  return frozen;
}

export function publishRequest(
  attempt: FrozenPublishAttempt,
  csrfToken: string,
): Request {
  const url = `/api/v1/resumes/${encodeURIComponent(attempt.resumeId)}/publish`;
  const request = new Request(url, {
    method: 'POST',
    body: JSON.stringify(attempt.command),
    cache: 'no-store',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      'If-Match': parentETag(attempt.revision),
      'Idempotency-Key': attempt.idempotencyKey,
      'X-CSRF-Token': csrfToken,
      'X-Resume-Schema-Version': String(attempt.schemaVersion),
    },
  });
  return request;
}

export function createPublishApi(fetcher: typeof fetch = fetch): PublishApi {
  return {
    async dispatch(attempt, csrfToken): Promise<PublishTransportResult> {
      let response: Response;
      try {
        response = await fetcher(publishRequest(attempt, csrfToken));
      } catch {
        return { kind: 'unknown', reason: 'transport' };
      }
      if (response.headers.get('Cache-Control') !== 'no-store, no-transform') {
        return { kind: 'unknown', reason: 'server' };
      }
      if (response.status === 200) {
        try {
          const accepted = await parseAcceptedResponse(response);
          if (
            accepted.metadata.id !== attempt.resumeId
            || compareRevision(accepted.revision, attempt.revision) <= 0
          ) {
            throw new Error('publish response mismatch');
          }
          return {
            kind: 'accepted',
            resume: accepted,
          };
        } catch {
          return { kind: 'unknown', reason: 'server' };
        }
      }
      if (response.status === 412) {
        try {
          const error = await parseError(response);
          if (error.code !== 'revision_mismatch' || !isRecord(error.details)) {
            throw new Error();
          }
          const revision = parseRevision(error.details.revision);
          const document = parseCurrentDocument(error.details.document);
          return {
            kind: 'stale',
            winner: Object.freeze({ revision, document }),
          };
        } catch {
          return { kind: 'unknown', reason: 'server' };
        }
      }
      let error: ParsedError;
      try {
        error = await parseError(response);
      } catch {
        return { kind: 'unknown', reason: 'server' };
      }
      if (response.status === 500) {
        return { kind: 'unknown', reason: 'server' };
      }
      if (response.status === 401 && error.code === 'session_required') {
        return { kind: 'session-lost' };
      }
      if (response.status === 403 && error.code === 'csrf_rejected') {
        return { kind: 'csrf-rejected' };
      }
      if (response.status === 403 && error.code === 'reauth_required') {
        return { kind: 'reauth-required' };
      }
      if (response.status === 409 && error.code === 'slug_taken') {
        return { kind: 'slug-taken' };
      }
      if (response.status === 422 && error.code === 'publish_invalid') {
        try {
          return { kind: 'invalid', issues: validateIssues(error.details) };
        } catch {
          return { kind: 'unknown', reason: 'server' };
        }
      }
      if (response.status === 429 && error.code === 'rate_limited') {
        return { kind: 'rate-limited', retryAfterMs: retryAfterMs(response) };
      }
      if (response.status === 503 && error.code === 'public_state_busy') {
        return {
          kind: 'public-state-busy',
          retryAfterMs: retryAfterMs(response),
        };
      }
      if (
        response.status === 405
        && error.code === 'method_not_allowed'
        && response.headers.get('Allow') === 'POST'
      ) {
        return { kind: 'failed', code: 'method_not_allowed' };
      }
      if (KNOWN_FAILURE_CODES.has(`${response.status}:${error.code}`)) {
        return { kind: 'failed', code: error.code as PublishFailureCode };
      }
      if (response.status >= 500) return { kind: 'unknown', reason: 'server' };
      return { kind: 'unknown', reason: 'server' };
    },
  };
}

const KNOWN_FAILURE_CODES = new Set<string>([
  '400:invalid_client_ip',
  '400:idempotency_key_invalid',
  '400:idempotency_key_required',
  '400:precondition_malformed',
  '400:request_invalid',
  '400:unsupported_schema_version',
  '413:body_too_large',
  '409:idempotency_key_reuse',
  '428:precondition_required',
  '404:resume_not_found',
]);

const ISSUE_CODES = new Set([
  'required_for_live',
  'requires_live',
  'invalid_format',
  'reserved',
  'required',
  'visible_entry_required',
]);

interface ParsedError {
  code: string;
  details?: unknown;
}

async function parseError(response: Response): Promise<ParsedError> {
  const value: unknown = await response.json();
  if (
    !isRecord(value)
    || !isRecord(value.error)
    || typeof value.error.code !== 'string'
    || typeof value.error.message !== 'string'
  ) {
    throw new Error('invalid error');
  }
  return { code: value.error.code, details: value.error.details };
}

function validateIssues(details: unknown): readonly ServerValidationIssue[] {
  if (!isRecord(details) || !Array.isArray(details.issues)) {
    throw new Error('invalid issues');
  }
  if (
    !details.issues.every(
      (issue) =>
        isRecord(issue)
        && typeof issue.path === 'string'
        && typeof issue.code === 'string'
        && ISSUE_CODES.has(issue.code),
    )
  ) {
    throw new Error('invalid issue');
  }
  return details.issues.map((issue) =>
    Object.freeze({
      path: (issue as { path: string }).path,
      code: (issue as { code: string }).code,
    }),
  );
}

function retryAfterMs(response: Response): number | null {
  const value = response.headers.get('Retry-After');
  if (value === null || !/^[0-9]+$/.test(value)) return null;
  const seconds = Number(value);
  const milliseconds = seconds * 1000;
  return Number.isSafeInteger(seconds)
    && seconds > 0
    && Number.isSafeInteger(milliseconds)
    ? milliseconds
    : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
