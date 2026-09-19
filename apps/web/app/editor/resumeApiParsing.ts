import { CURRENT_VERSION } from '@aboutme/schema/released';

import type {
  AttemptResult,
  FrozenAttempt,
  ObjectETag,
  ResumeSummary,
  ServerValidationIssue,
  ValidatedStaleWinner,
} from './attempt';
import { parseCurrentDocument } from './documentValidation';
import { parentETag, parseParentETag, parseRevision } from './revision';
import type { AcceptedResume, ParentETag } from './types';

const CACHE_CONTROL = 'no-store, no-transform';

export function parseObjectETag(value: string | null): ObjectETag {
  // eslint-disable-next-line no-control-regex -- ETags forbid C0 and DEL.
  if (value === null || !/^"[^"\\\s,\x00-\x1F\x7F]+"$/.test(value)) {
    throw new Error('invalid object ETag');
  }
  return value as ObjectETag;
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

export async function parseBodyless(
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

export async function parseStale(response: Response): Promise<AttemptResult> {
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

export function parseSummary(value: unknown): ResumeSummary {
  if (
    !isRecord(value)
    || typeof value.id !== 'string'
    || typeof value.title !== 'string'
    || typeof value.lng !== 'string'
    || typeof value.live !== 'boolean'
    || typeof value.downloadEnabled !== 'boolean'
    || typeof value.seoGeoEnabled !== 'boolean'
    || (value.slug !== null && typeof value.slug !== 'string')
    || !optionalText(value.publicTitle)
    || !optionalText(value.faviconEmoji)
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
    publicTitle: (value.publicTitle as string | null | undefined) ?? null,
    faviconEmoji: (value.faviconEmoji as string | null | undefined) ?? null,
    schemaVersion: CURRENT_VERSION,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
    revision: parseRevision(value.revision),
  });
}

// An owner resource from before the public page fields has neither key;
// absence reads as the default, like null.
function optionalText(value: unknown): boolean {
  return value === undefined || value === null || typeof value === 'string';
}

export function dataOf(value: unknown): unknown {
  if (!isRecord(value) || !('data' in value)) {
    throw new Error('invalid envelope');
  }
  return value.data;
}

export async function parseError(
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

export function safeIssues(
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

export function hasExactCachePolicy(response: Response): boolean {
  return response.headers.get('Cache-Control') === CACHE_CONTROL;
}

export function hasCurrentSchemaHeader(response: Response): boolean {
  return (
    response.headers.get('X-Resume-Schema-Version') === String(CURRENT_VERSION)
  );
}

export function revisionFromParentETag(etag: ParentETag) {
  parseParentETag(etag);
  return parseRevision(etag.slice(2, -1));
}

export function retryAfterMs(response: Response): number | null {
  const value = response.headers.get('Retry-After');
  if (value === null || !/^[0-9]+$/.test(value)) return null;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) ? seconds * 1000 : null;
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
