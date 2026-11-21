import type { Resume } from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';

import type { ValidatedStaleWinner } from './attempt';
import { parseCurrentDocument } from './documentValidation';
import {
  dataOf,
  parseAcceptedResponse,
  parseError,
  retryAfterMs,
} from './resumeApiParsing';
import { compareRevision, parentETag, parseRevision } from './revision';
import type { AcceptedResume, EditorRuntime, Revision } from './types';

export type CheckpointCause = 'edit' | 'assistant' | 'restore' | 'accept';

export type CheckpointActor
  = | { readonly kind: 'owner' }
    | { readonly kind: 'assistant'; readonly name: string };

export interface CheckpointSummary {
  readonly id: string;
  readonly startedAt: string;
  readonly cause: CheckpointCause;
  readonly actor: CheckpointActor;
  readonly title: string;
  readonly byteSize: number;
}

export interface CheckpointVersion extends CheckpointSummary {
  readonly lng: string | null;
  readonly document: Resume;
}

export type HistoryReadResult<T>
  = | { readonly kind: 'ok'; readonly value: T }
    | { readonly kind: 'not-found' }
    | { readonly kind: 'session-lost' }
    | { readonly kind: 'failed' };

export interface HistoryWrite {
  readonly operation: 'restore' | 'delete' | 'clear';
  readonly resumeId: string;
  readonly checkpointId?: string;
  readonly revision?: Revision;
  readonly ownerId: string;
  readonly idempotencyKey: string;
}

export type HistoryWriteResult
  = | { readonly kind: 'done' }
    | { readonly kind: 'accepted'; readonly resume: AcceptedResume }
    | { readonly kind: 'stale'; readonly winner: ValidatedStaleWinner }
    | { readonly kind: 'invalid' }
    | { readonly kind: 'not-found' }
    | { readonly kind: 'busy'; readonly retryAfterMs: number | null }
    | { readonly kind: 'session-lost' }
    | { readonly kind: 'csrf-rejected' }
    | { readonly kind: 'network' }
    | { readonly kind: 'failed' };

export interface HistoryApi {
  list(resumeId: string): Promise<HistoryReadResult<CheckpointSummary[]>>;
  read(
    resumeId: string,
    checkpointId: string,
  ): Promise<HistoryReadResult<CheckpointVersion>>;
  write(write: HistoryWrite, csrfToken: string): Promise<HistoryWriteResult>;
}

export function freezeHistoryWrite(
  input: Omit<HistoryWrite, 'idempotencyKey'>,
  runtime: Pick<EditorRuntime, 'uuid'>,
): HistoryWrite {
  return Object.freeze({ ...input, idempotencyKey: runtime.uuid() });
}

const BASE = '/api/v1/resumes';

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

const CAUSES = new Set<string>(['edit', 'assistant', 'restore', 'accept']);

function parseSummary(value: unknown): CheckpointSummary {
  if (
    !isRecord(value)
    || typeof value.id !== 'string'
    || value.id === ''
    || typeof value.startedAt !== 'string'
    || !Number.isFinite(Date.parse(value.startedAt))
    || typeof value.cause !== 'string'
    || !CAUSES.has(value.cause)
    || typeof value.title !== 'string'
    || typeof value.byteSize !== 'number'
    || !isRecord(value.actor)
  ) {
    throw new Error('invalid checkpoint');
  }
  const actor = value.actor;
  if (actor.kind === 'owner') {
    return {
      id: value.id,
      startedAt: value.startedAt,
      cause: value.cause as CheckpointCause,
      actor: { kind: 'owner' },
      title: value.title,
      byteSize: value.byteSize,
    };
  }
  if (actor.kind === 'assistant' && typeof actor.name === 'string') {
    return {
      id: value.id,
      startedAt: value.startedAt,
      cause: value.cause as CheckpointCause,
      actor: { kind: 'assistant', name: actor.name },
      title: value.title,
      byteSize: value.byteSize,
    };
  }
  throw new Error('invalid checkpoint actor');
}

function parseVersion(value: unknown): CheckpointVersion {
  const summary = parseSummary(value);
  const body = value as Record<string, unknown>;
  if (
    (body.lng !== null && typeof body.lng !== 'string')
    || body.schemaVersion !== CURRENT_VERSION
    || !isRecord(body.document)
  ) {
    throw new Error('invalid checkpoint version');
  }
  const document = parseCurrentDocument({
    schemaVersion: body.schemaVersion,
    ...body.document,
  });
  return { ...summary, lng: body.lng as string | null, document };
}

async function readJson<T>(
  fetcher: typeof fetch,
  path: string,
  parse: (data: unknown) => T,
): Promise<HistoryReadResult<T>> {
  let response: Response;
  try {
    response = await fetcher(
      new Request(path, {
        method: 'GET',
        cache: 'no-store',
        credentials: 'include',
        headers: { Accept: 'application/json' },
      }),
    );
  } catch {
    return { kind: 'failed' };
  }
  if (response.status === 401) return { kind: 'session-lost' };
  if (response.status === 404) return { kind: 'not-found' };
  if (response.status !== 200) return { kind: 'failed' };
  try {
    return { kind: 'ok', value: parse(dataOf(await response.json())) };
  } catch {
    return { kind: 'failed' };
  }
}

function writeRequest(write: HistoryWrite, csrfToken: string): Request {
  const base = `${BASE}/${encodeURIComponent(write.resumeId)}/checkpoints`;
  const headers: Record<string, string> = {
    'Idempotency-Key': write.idempotencyKey,
    'X-CSRF-Token': csrfToken,
  };
  if (write.operation === 'clear') {
    return new Request(base, {
      method: 'DELETE',
      cache: 'no-store',
      credentials: 'include',
      headers,
    });
  }
  const target = `${base}/${encodeURIComponent(write.checkpointId ?? '')}`;
  if (write.operation === 'delete') {
    return new Request(target, {
      method: 'DELETE',
      cache: 'no-store',
      credentials: 'include',
      headers,
    });
  }
  return new Request(`${target}/restore`, {
    method: 'POST',
    cache: 'no-store',
    credentials: 'include',
    headers: {
      ...headers,
      'If-Match': parentETag(write.revision as Revision),
      'X-Resume-Schema-Version': String(CURRENT_VERSION),
    },
  });
}

async function mapFailure(response: Response): Promise<HistoryWriteResult> {
  const code = await parseError(response).then(
    (error) => error.code,
    () => '',
  );
  switch (response.status) {
    case 401:
      return { kind: 'session-lost' };
    case 403:
      return code === 'csrf_rejected'
        ? { kind: 'csrf-rejected' }
        : { kind: 'failed' };
    case 404:
      return { kind: 'not-found' };
    case 422:
      return { kind: 'invalid' };
    case 429:
    case 503:
      return { kind: 'busy', retryAfterMs: retryAfterMs(response) };
    default:
      return { kind: 'failed' };
  }
}

async function mapStale(response: Response): Promise<HistoryWriteResult> {
  try {
    const error = await parseError(response);
    if (error.code !== 'revision_mismatch' || !isRecord(error.details)) {
      throw new Error('invalid stale response');
    }
    return {
      kind: 'stale',
      winner: Object.freeze({
        revision: parseRevision(error.details.revision),
        document: parseCurrentDocument(error.details.document),
      }) as ValidatedStaleWinner,
    };
  } catch {
    return { kind: 'failed' };
  }
}

export function createHistoryApi(fetcher: typeof fetch = fetch): HistoryApi {
  return {
    list: (resumeId) =>
      readJson(
        fetcher,
        `${BASE}/${encodeURIComponent(resumeId)}/checkpoints`,
        (data) => {
          if (!Array.isArray(data)) throw new Error('invalid list');
          return data.map(parseSummary);
        },
      ),
    read: (resumeId, checkpointId) =>
      readJson(
        fetcher,
        `${BASE}/${encodeURIComponent(resumeId)}/checkpoints/`
        + encodeURIComponent(checkpointId),
        parseVersion,
      ),
    async write(write, csrfToken): Promise<HistoryWriteResult> {
      let response: Response;
      try {
        response = await fetcher(writeRequest(write, csrfToken));
      } catch {
        return { kind: 'network' };
      }
      if (write.operation === 'restore') {
        if (response.status === 200) {
          try {
            const resume = await parseAcceptedResponse(response);
            if (
              resume.metadata.id !== write.resumeId
              || compareRevision(
                resume.revision,
                write.revision as Revision,
              ) <= 0
            ) {
              throw new Error('restore response mismatch');
            }
            return { kind: 'accepted', resume };
          } catch {
            return { kind: 'failed' };
          }
        }
        if (response.status === 412) return mapStale(response);
      } else if (response.status === 204) {
        return { kind: 'done' };
      }
      return mapFailure(response);
    },
  };
}
