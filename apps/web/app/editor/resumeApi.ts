import { CURRENT_VERSION } from '@aboutme/schema/released';

import type { operations } from '../api/generated/openapi';
import type {
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
import { applyIntent } from './commands';
import {
  parseCurrentDocument,
  UnknownDocumentVersionError,
} from './documentValidation';
import {
  compareRevision,
  parentETag,
  parseParentETag,
  parseRevision,
} from './revision';
import type { AtomicEditorCommand, CreateResumeIntent } from './commands';
import type { AcceptedResume, EditorRuntime, ParentETag } from './types';

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

type JsonBody<Operation extends keyof operations>
  = operations[Operation] extends {
    requestBody: { content: { 'application/json': infer Body } };
  }
    ? Body
    : never;

type Wire = Readonly<{
  operation: FrozenAttempt['operation'];
  url: string;
  method: FrozenAttempt['method'];
  body?: unknown;
  file?: File;
}>;

const CACHE_CONTROL = 'no-store, no-transform';
const RETRY_WINDOW_MS = 23 * 60 * 60 * 1000;
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

export function freezeAttempt(
  command: AtomicEditorCommand,
  accepted: AcceptedResume,
  runtime: EditorRuntime,
): FrozenAttempt {
  const wire = wireForCommand(command, accepted);
  return freezeWire(command.id, wire, parentETag(accepted.revision), runtime);
}

export function freezeCreateAttempt(
  intent: CreateResumeIntent,
  runtime: EditorRuntime,
): FrozenAttempt {
  const body: JsonBody<'createResume'>
    = intent.lng === undefined
      ? { title: intent.title }
      : { title: intent.title, lng: intent.lng };
  return freezeWire(
    intent.id,
    {
      operation: 'createResume',
      url: '/api/v1/resumes',
      method: 'POST',
      body,
    },
    undefined,
    runtime,
  );
}

export function requestFromAttempt(
  attempt: FrozenAttempt,
  csrfToken: string,
): Request {
  const headers = new Headers({
    'Idempotency-Key': attempt.idempotencyKey,
    'X-CSRF-Token': csrfToken,
    'X-Resume-Schema-Version': String(attempt.schemaVersion),
  });
  if (attempt.ifMatch !== undefined) headers.set('If-Match', attempt.ifMatch);

  let body: BodyInit | undefined;
  if (attempt.payload.kind === 'json') {
    body = attempt.payload.utf8;
  } else if (attempt.payload.kind === 'photo') {
    const form = new FormData();
    form.append('file', attempt.payload.file);
    body = form;
  }

  const request = new Request(attempt.url, {
    method: attempt.method,
    body,
    cache: 'no-store',
    credentials: 'include',
  });
  for (const [name, value] of headers) request.headers.set(name, value);
  if (attempt.payload.kind === 'json') {
    request.headers.set('Content-Type', 'application/json');
  }
  return request;
}

export function parseObjectETag(value: string | null): ObjectETag {
  // eslint-disable-next-line no-control-regex -- ETags forbid C0 and DEL.
  if (value === null || !/^"[^"\\\s,\x00-\x1F\x7F]+"$/.test(value)) {
    throw new Error('invalid object ETag');
  }
  return value as ObjectETag;
}

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

function freezeWire(
  id: string,
  wire: Wire,
  ifMatch: ParentETag | undefined,
  runtime: EditorRuntime,
): FrozenAttempt {
  const firstDispatchAt = runtime.nowEpochMs();
  const payload
    = wire.file === undefined
      ? wire.body === undefined
        ? Object.freeze({ kind: 'empty' as const })
        : Object.freeze({
            kind: 'json' as const,
            utf8: JSON.stringify(wire.body),
          })
      : Object.freeze({ kind: 'photo' as const, file: wire.file });
  return Object.freeze({
    id,
    operation: wire.operation,
    url: wire.url,
    method: wire.method,
    schemaVersion: CURRENT_VERSION,
    ...(ifMatch === undefined ? {} : { ifMatch }),
    idempotencyKey: runtime.uuid(),
    payload,
    firstDispatchAt,
    retryCutoff: firstDispatchAt + RETRY_WINDOW_MS,
    automaticReplays: 0 as const,
    staleRebases: 0 as const,
  });
}

function wireForCommand(
  command: AtomicEditorCommand,
  accepted: AcceptedResume,
): Wire {
  const base = `/api/v1/resumes/${encodeURIComponent(command.resumeId)}`;
  const updated = applyIntent(accepted, command);
  switch (command.kind) {
    case 'metadataField':
      return {
        operation: 'updateResumeMetadata',
        url: base,
        method: 'PATCH',
        body: {
          [command.field]: command.value,
        } as JsonBody<'updateResumeMetadata'>,
      };
    case 'personalField': {
      const { photo: _photo, ...personalDetails }
        = updated.document.personalDetails;
      return {
        operation: 'updateResumePersonalDetails',
        url: `${base}/personal-details`,
        method: 'PATCH',
        body: personalDetails as JsonBody<'updateResumePersonalDetails'>,
      };
    }
    case 'entryField':
    case 'entryUpsert': {
      const section = updated.document.content[command.sectionKey];
      const entryId
        = command.kind === 'entryField' ? command.entryId : command.entry.id;
      const entry = section?.entries.find(
        (candidate) => candidate.id === entryId,
      );
      if (entry === undefined) throw new Error('entry missing after command');
      return {
        operation: 'upsertResumeEntry',
        url: `${base}/entries/${encodeURIComponent(command.sectionKey)}`,
        method: 'PATCH',
        body: { entry } as unknown as JsonBody<'upsertResumeEntry'>,
      };
    }
    case 'entryDelete': {
      const sectionKey = encodeURIComponent(command.sectionKey);
      const entryId = encodeURIComponent(command.entryId);
      return {
        operation: 'deleteResumeEntry',
        url: `${base}/entries/${sectionKey}/${entryId}`,
        method: 'DELETE',
      };
    }
    case 'entryReorder':
      return {
        operation: 'updateResumeSection',
        url: `${base}/sections/${encodeURIComponent(command.sectionKey)}`,
        method: 'PATCH',
        body: {
          entryOrder: command.entryIds,
        } as JsonBody<'updateResumeSection'>,
      };
    case 'sectionMetadata':
      return {
        operation: 'updateResumeSection',
        url: `${base}/sections/${encodeURIComponent(command.sectionKey)}`,
        method: 'PATCH',
        body: {
          [command.change.field]: command.change.value,
        } as JsonBody<'updateResumeSection'>,
      };
    case 'structure':
      return {
        operation: 'updateResumeStructure',
        url: `${base}/structure`,
        method: 'PATCH',
        body: {
          commands: command.commands,
        } as JsonBody<'updateResumeStructure'>,
      };
    case 'customization':
      return {
        operation: 'updateResumeCustomization',
        url: `${base}/customization`,
        method: 'PATCH',
        body: {
          deltas: command.deltas,
        } as JsonBody<'updateResumeCustomization'>,
      };
    case 'photoUpload':
      return {
        operation: 'uploadResumePhoto',
        url: `${base}/photo`,
        method: 'POST',
        file: command.file,
      };
    case 'photoCrop':
      return {
        operation: 'updateResumePhotoCrop',
        url: `${base}/photo`,
        method: 'PATCH',
        body: { crop: command.crop } as JsonBody<'updateResumePhotoCrop'>,
      };
    case 'photoDelete':
      return {
        operation: 'deleteResumePhoto',
        url: `${base}/photo`,
        method: 'DELETE',
      };
    case 'resumeDelete':
      return { operation: 'deleteResume', url: base, method: 'DELETE' };
    default:
      return assertNever(command);
  }
}

export async function parseAcceptedResponse(
  response: Response,
): Promise<AcceptedResume> {
  if (!hasCurrentSchemaHeader(response)) throw new Error('wrong schema header');
  const data = dataOf(await response.json());
  const summary = parseSummary(data);
  if (!isRecord(data)) throw new Error('invalid resume');
  const document = parseCurrentDocument(data.document);
  const etag = parseParentETag(response.headers.get('ETag'));
  if (etag !== parentETag(summary.revision)) {
    throw new Error('mismatched revision');
  }
  return {
    document,
    metadata: summary,
    revision: summary.revision,
    metadataFreshness: 'complete',
  };
}

async function parseBodyless(
  response: Response,
  attempt: FrozenAttempt,
): Promise<AttemptResult> {
  if (
    response.headers.get('Content-Type') !== null
    || (await response.arrayBuffer()).byteLength !== 0
  ) {
    throw new Error('unexpected 204 body');
  }
  if (attempt.operation === 'deleteResume') {
    if (
      response.headers.get('ETag') !== null
      || response.headers.get('X-Resume-Schema-Version') !== null
    ) {
      throw new Error('unexpected delete headers');
    }
    return { kind: 'resume-deleted', status: 204 };
  }
  const scope
    = attempt.operation === 'deleteResumeEntry'
      ? 'entry'
      : attempt.operation === 'deleteResumePhoto'
        ? 'photo'
        : null;
  if (scope === null || !hasCurrentSchemaHeader(response)) {
    throw new Error('invalid 204');
  }
  return {
    kind: 'child-ack',
    status: 204,
    scope,
    etag: parseParentETag(response.headers.get('ETag')),
  };
}

async function parseStale(response: Response): Promise<AttemptResult> {
  const error = await parseError(response);
  if (error.code !== 'revision_mismatch' || !isRecord(error.details)) {
    throw new Error('invalid stale response');
  }
  const revision = parseRevision(error.details.revision);
  return {
    kind: 'stale',
    status: 412,
    winner: Object.freeze({
      document: parseCurrentDocument(error.details.document),
      revision,
    }) as ValidatedStaleWinner,
  };
}

function parseSummary(value: unknown): ResumeSummary {
  if (
    !isRecord(value)
    || typeof value.id !== 'string'
    || typeof value.title !== 'string'
    || typeof value.lng !== 'string'
    || typeof value.live !== 'boolean'
    || typeof value.downloadEnabled !== 'boolean'
    || typeof value.seoGeoEnabled !== 'boolean'
    || (value.slug !== null && typeof value.slug !== 'string')
    || typeof value.createdAt !== 'string'
    || typeof value.updatedAt !== 'string'
    || value.schemaVersion !== CURRENT_VERSION
  ) {
    throw new Error('invalid summary');
  }
  return Object.freeze({
    id: value.id,
    title: value.title,
    lng: value.lng,
    live: value.live,
    downloadEnabled: value.downloadEnabled,
    seoGeoEnabled: value.seoGeoEnabled,
    slug: value.slug,
    schemaVersion: CURRENT_VERSION,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
    revision: parseRevision(value.revision),
  });
}

function dataOf(value: unknown): unknown {
  if (!isRecord(value) || !('data' in value)) {
    throw new Error('invalid envelope');
  }
  return value.data;
}

async function parseError(
  response: Response,
): Promise<{ code: string; details?: unknown; issues?: unknown[] }> {
  const value = await response.json();
  if (
    !isRecord(value)
    || !isRecord(value.error)
    || typeof value.error.code !== 'string'
    || typeof value.error.message !== 'string'
  ) {
    throw new Error('invalid error envelope');
  }
  const details = value.error.details;
  return {
    code: value.error.code,
    details,
    issues:
      isRecord(details) && Array.isArray(details.issues)
        ? details.issues
        : undefined,
  };
}

function safeIssues(
  issues: readonly unknown[],
): readonly ServerValidationIssue[] {
  if (
    !issues.every(
      (issue) =>
        isRecord(issue)
        && typeof issue.path === 'string'
        && typeof issue.code === 'string',
    )
  ) {
    throw new Error('invalid validation issues');
  }
  return issues.map((issue) => {
    const validated = issue as Record<string, unknown>;
    return Object.freeze({
      path: validated.path as string,
      code: validated.code as string,
    });
  });
}

function hasExactCachePolicy(response: Response): boolean {
  return response.headers.get('Cache-Control') === CACHE_CONTROL;
}

function hasCurrentSchemaHeader(response: Response): boolean {
  return (
    response.headers.get('X-Resume-Schema-Version') === String(CURRENT_VERSION)
  );
}

function revisionFromParentETag(etag: ParentETag) {
  parseParentETag(etag);
  return parseRevision(etag.slice(2, -1));
}

function retryAfterMs(response: Response): number | null {
  const value = response.headers.get('Retry-After');
  if (value === null || !/^[0-9]+$/.test(value)) return null;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) ? seconds * 1000 : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function assertNever(value: never): never {
  throw new Error(`unhandled command: ${String(value)}`);
}
