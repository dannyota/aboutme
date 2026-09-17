import { CURRENT_VERSION } from '@aboutme/schema/released';

import type {
  AttemptFailureCode,
  AttemptResult,
  FrozenAttempt,
  ObjectETag,
  OwnerPhotoReadResult,
  ResumeConditionalReadResult,
  ResumeListResult,
  ResumeReadResult,
} from './attempt';
import { UnknownDocumentVersionError } from './documentValidation';
import { compareRevision, parentETag, parseParentETag } from './revision';
import {
  dataOf,
  hasCurrentSchemaHeader,
  hasExactCachePolicy,
  parseAcceptedResponse,
  parseBodyless,
  parseError,
  parseObjectETag,
  parseStale,
  parseSummary,
  retryAfterMs,
  revisionFromParentETag,
  safeIssues,
} from './resumeApiParsing';
import { requestFromAttempt } from './resumeApiRequests';
import type { ParentETag } from './types';

export type {
  AttemptFailureCode,
  AttemptResult,
  FrozenAttempt,
  ObjectETag,
  OwnerPhotoReadResult,
  ResumeConditionalReadResult,
  ResumeListResult,
  ResumeReadResult,
  ResumeSummary,
  ServerValidationIssue,
  ValidatedStaleWinner,
} from './attempt';

export {
  freezeAttempt,
  freezeCreateAttempt,
  requestFromAttempt,
} from './resumeApiRequests';
export { parseAcceptedResponse, parseObjectETag } from './resumeApiParsing';

export interface ResumeApi {
  list(): Promise<ResumeListResult>;
  read(id: string): Promise<ResumeReadResult>;
  readConditional(
    id: string,
    etag?: ParentETag,
  ): Promise<ResumeConditionalReadResult>;
  dispatch(attempt: FrozenAttempt, csrfToken: string): Promise<AttemptResult>;
  readOwnerPhoto(id: string, etag?: ObjectETag): Promise<OwnerPhotoReadResult>;
}

const FAILURE_CODES = new Set<AttemptFailureCode>([
  'bad_request',
  'body_too_large',
  'customization_path_denied',
  'idempotency_key_invalid',
  'idempotency_key_required',
  'invalid_client_ip',
  'media_invalid',
  'media_not_found',
  'media_too_large',
  'media_type_unsupported',
  'method_not_allowed',
  'not_found',
  'precondition_malformed',
  'precondition_not_supported',
  'precondition_required',
  'request_invalid',
  'response_invalid',
  'resume_cap_exceeded',
  'resume_not_found',
  'unsupported_schema_version',
]);

export function createResumeApi(fetcher: typeof fetch = fetch): ResumeApi {
  return {
    async list(): Promise<ResumeListResult> {
      let response: Response;
      try {
        response = await fetcher(
          new Request('/api/v1/resumes', {
            cache: 'no-store',
            credentials: 'include',
            headers: { 'X-Resume-Schema-Version': String(CURRENT_VERSION) },
          }),
        );
      } catch {
        return { kind: 'failed', reason: 'network' };
      }
      if (!hasExactCachePolicy(response)) {
        return { kind: 'failed', reason: 'response-invalid' };
      }
      if (response.status === 401) return { kind: 'session-lost' };
      if (response.status === 429) {
        return { kind: 'rate-limited', retryAfterMs: retryAfterMs(response) };
      }
      if (response.status !== 200) {
        return { kind: 'failed', reason: 'response-invalid' };
      }
      try {
        const value = await response.json();
        const data = dataOf(value);
        if (!Array.isArray(data) || !hasCurrentSchemaHeader(response)) {
          throw new Error();
        }
        return { kind: 'ready', items: data.map(parseSummary) };
      } catch {
        return { kind: 'failed', reason: 'response-invalid' };
      }
    },

    async read(id: string): Promise<ResumeReadResult> {
      let response: Response;
      try {
        response = await fetcher(
          new Request(`/api/v1/resumes/${encodeURIComponent(id)}`, {
            cache: 'no-store',
            credentials: 'include',
            headers: { 'X-Resume-Schema-Version': String(CURRENT_VERSION) },
          }),
        );
      } catch {
        return { kind: 'failed', reason: 'network' };
      }
      if (!hasExactCachePolicy(response)) {
        return { kind: 'failed', reason: 'response-invalid' };
      }
      if (response.status === 401) return { kind: 'session-lost' };
      if (response.status === 404) return { kind: 'unavailable' };
      if (response.status === 429) {
        return { kind: 'rate-limited', retryAfterMs: retryAfterMs(response) };
      }
      if (response.status !== 200) {
        return { kind: 'failed', reason: 'response-invalid' };
      }
      try {
        return {
          kind: 'complete',
          accepted: await parseAcceptedResponse(response),
        };
      } catch (error) {
        if (error instanceof UnknownDocumentVersionError) {
          return { kind: 'unknown-version' };
        }
        return { kind: 'failed', reason: 'response-invalid' };
      }
    },

    async readConditional(
      id: string,
      etag?: ParentETag,
    ): Promise<ResumeConditionalReadResult> {
      const headers = new Headers({
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      });
      if (etag !== undefined) headers.set('If-None-Match', etag);
      let response: Response;
      try {
        const request = new Request(
          `/api/v1/resumes/${encodeURIComponent(id)}`,
          { cache: 'no-store', credentials: 'include' },
        );
        for (const [name, value] of headers) request.headers.set(name, value);
        response = await fetcher(request);
      } catch {
        return { kind: 'failed', reason: 'network' };
      }
      if (!hasExactCachePolicy(response)) {
        return { kind: 'failed', reason: 'response-invalid' };
      }
      if (response.status === 401) return { kind: 'session-lost' };
      if (response.status === 404) return { kind: 'unavailable' };
      if (response.status === 429) {
        return { kind: 'rate-limited', retryAfterMs: retryAfterMs(response) };
      }
      try {
        if (response.status === 304) {
          if (
            response.headers.get('Content-Type') !== null
            || (await response.arrayBuffer()).byteLength !== 0
          ) {
            throw new Error('unexpected 304 body');
          }
          const responseETag = parseParentETag(response.headers.get('ETag'));
          if (etag === undefined || responseETag !== etag) {
            throw new Error('unexpected 304 validator');
          }
          return {
            kind: 'not-modified',
            etag: responseETag,
          };
        }
        if (response.status !== 200) {
          return { kind: 'failed', reason: 'response-invalid' };
        }
        const accepted = await parseAcceptedResponse(response);
        return {
          kind: 'complete',
          accepted,
          etag: parentETag(accepted.revision),
        };
      } catch (error) {
        if (error instanceof UnknownDocumentVersionError) {
          return { kind: 'unknown-version' };
        }
        return { kind: 'failed', reason: 'response-invalid' };
      }
    },

    async dispatch(attempt, csrfToken): Promise<AttemptResult> {
      let response: Response;
      try {
        response = await fetcher(requestFromAttempt(attempt, csrfToken));
      } catch {
        return { kind: 'unknown', reason: 'transport' };
      }
      if (!hasExactCachePolicy(response)) {
        return { kind: 'unknown', reason: 'server' };
      }
      try {
        if (response.status === 200 || response.status === 201) {
          const accepted = await parseAcceptedResponse(response);
          if (
            attempt.ifMatch !== undefined
            && compareRevision(
              accepted.revision,
              revisionFromParentETag(attempt.ifMatch),
            ) === -1
          ) {
            throw new Error('revision regression');
          }
          return {
            kind: 'complete',
            status: response.status,
            accepted,
          };
        }
        if (response.status === 204) {
          return await parseBodyless(response, attempt);
        }
        if (response.status === 412) return await parseStale(response);
        const error = await parseError(response);
        if (response.status === 401) return { kind: 'session-lost' };
        if (response.status === 403 && error.code === 'csrf_rejected') {
          return { kind: 'csrf-rejected' };
        }
        if (response.status === 409 && error.code === 'idempotency_key_reuse') {
          return { kind: 'idempotency-reuse' };
        }
        if (response.status === 422 && Array.isArray(error.issues)) {
          return {
            kind: 'validation-rejected',
            issues: safeIssues(error.issues),
          };
        }
        if (response.status === 429 && error.code === 'rate_limited') {
          return { kind: 'rate-limited', retryAfterMs: retryAfterMs(response) };
        }
        if (response.status === 503 && error.code === 'media_busy') {
          return { kind: 'media-busy', retryAfterMs: retryAfterMs(response) };
        }
        if (response.status >= 500) {
          return { kind: 'unknown', reason: 'server' };
        }
        if (FAILURE_CODES.has(error.code as AttemptFailureCode)) {
          return { kind: 'rejected', code: error.code as AttemptFailureCode };
        }
      } catch {
        return { kind: 'unknown', reason: 'server' };
      }
      return { kind: 'unknown', reason: 'server' };
    },

    async readOwnerPhoto(id, etag): Promise<OwnerPhotoReadResult> {
      const headers = new Headers();
      if (etag !== undefined) headers.set('If-None-Match', etag);
      let response: Response;
      try {
        const request = new Request(
          `/api/v1/resumes/${encodeURIComponent(id)}/photo`,
          {
            cache: 'no-store',
            credentials: 'include',
          },
        );
        for (const [name, value] of headers) request.headers.set(name, value);
        response = await fetcher(request);
      } catch {
        return { kind: 'unavailable', reason: 'network' };
      }
      if (!hasExactCachePolicy(response)) {
        return { kind: 'unavailable', reason: 'invalid' };
      }
      if (response.status === 401) {
        return { kind: 'unavailable', reason: 'session-lost' };
      }
      if (response.status === 404) {
        return { kind: 'unavailable', reason: 'not-found' };
      }
      try {
        const objectETag = parseObjectETag(response.headers.get('ETag'));
        if (response.status === 304) {
          if (
            response.headers.get('Content-Type') !== null
            || (await response.arrayBuffer()).byteLength !== 0
          ) {
            throw new Error();
          }
          return { kind: 'not-modified', etag: objectETag };
        }
        const mime = response.headers.get('Content-Type');
        if (
          response.status !== 200
          || (mime !== 'image/jpeg' && mime !== 'image/png')
        ) {
          throw new Error();
        }
        const bytes = new Uint8Array(await response.arrayBuffer());
        return { kind: 'bytes', mime, etag: objectETag, bytes: bytes.slice() };
      } catch {
        return { kind: 'unavailable', reason: 'invalid' };
      }
    },
  };
}
