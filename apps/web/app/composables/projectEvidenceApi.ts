import { createApiClient, type ApiClientOptions } from '../api/client';
import type { components } from '../api/generated/openapi';
import {
  isEvidenceKind,
  isEvidenceList,
  isIsoDate,
  isProject,
  isRecord,
  isRevision,
  type EvidenceKind,
  type Project,
  type ProjectEvidence,
} from '../components/projects/projectDocument';

export type EvidenceCandidate
  = components['schemas']['ProjectEvidenceCandidate'];

export interface CandidatePage {
  readonly items: EvidenceCandidate[];
  readonly nextPage: number | null;
  readonly incomplete: boolean;
}

export interface CandidateQuery {
  readonly kind: EvidenceKind;
  readonly page: number;
  readonly repository?: number;
}

export type AttachBody
  = | { readonly kind: EvidenceKind; readonly candidateId: string }
    | { readonly kind: EvidenceKind; readonly link: string };

export interface EvidenceWritten {
  readonly evidence: ProjectEvidence[];
  readonly projectRevision: string;
}

export type EvidenceFailure
  = | 'evidence-unavailable'
    | 'project-not-found'
    | 'evidence-not-found'
    | 'session-required'
    | 'duplicate'
    | 'key-reuse'
    | 'not-confirmed'
    | 'limit'
    | 'unsupported-link'
    | 'size'
    | 'upstream'
    | 'rate-limited'
    | 'busy'
    | 'invalid'
    | 'session-lost'
    | 'csrf-rejected'
    | 'unavailable';

export type EvidenceResult<T>
  = | { readonly kind: 'ok'; readonly value: T }
    | { readonly kind: 'stale'; readonly project: Project }
    | {
      readonly kind: 'failed';
      readonly failure: EvidenceFailure;
      readonly retryAfter: number | null;
    };

export interface ProjectEvidenceApi {
  candidates(
    projectId: string,
    query: CandidateQuery,
    csrfToken: string,
  ): Promise<EvidenceResult<CandidatePage>>;
  attach(
    projectId: string,
    revision: string,
    body: AttachBody,
    key: string,
    csrfToken: string,
  ): Promise<EvidenceResult<EvidenceWritten>>;
  recheck(
    projectId: string,
    evidenceId: string,
    revision: string,
    key: string,
    csrfToken: string,
  ): Promise<EvidenceResult<EvidenceWritten>>;
  reorder(
    projectId: string,
    revision: string,
    order: readonly string[],
    key: string,
    csrfToken: string,
  ): Promise<EvidenceResult<EvidenceWritten>>;
  remove(
    projectId: string,
    evidenceId: string,
    revision: string,
    key: string,
    csrfToken: string,
  ): Promise<EvidenceResult<EvidenceWritten>>;
}

interface RawResult {
  readonly data?: unknown;
  readonly error?: unknown;
  readonly response: Response;
}

const CODES: Readonly<Record<string, EvidenceFailure>> = {
  '404 evidence_unavailable': 'evidence-unavailable',
  '404 project_not_found': 'project-not-found',
  '404 evidence_not_found': 'evidence-not-found',
  '409 evidence_session_required': 'session-required',
  '409 evidence_duplicate': 'duplicate',
  '409 idempotency_key_reuse': 'key-reuse',
  '422 evidence_not_confirmed': 'not-confirmed',
  '422 evidence_limit': 'limit',
  '422 evidence_unsupported_link': 'unsupported-link',
  '422 document_invalid': 'size',
  '502 evidence_upstream': 'upstream',
  '429 rate_limited': 'rate-limited',
  '503 public_state_busy': 'busy',
  '403 csrf_rejected': 'csrf-rejected',
};

export function parseRetryAfter(value: string | null): number | null {
  const trimmed = value?.trim() ?? '';
  if (!/^[0-9]+$/u.test(trimmed)) return null;
  const seconds = Number(trimmed);
  return Number.isSafeInteger(seconds) && seconds > 0 ? seconds : null;
}

function failed(
  failure: EvidenceFailure,
  retryAfter: number | null = null,
): EvidenceResult<never> {
  return { kind: 'failed', failure, retryAfter };
}

function failureOf(response: Response, error: unknown): EvidenceResult<never> {
  const body = isRecord(error) && isRecord(error.error) ? error.error : {};
  const code = typeof body.code === 'string' ? body.code : '';
  const status = response.status;
  if (status === 412 && code === 'revision_mismatch') {
    const project = isRecord(body.details) ? body.details.project : undefined;
    return isProject(project)
      ? { kind: 'stale', project }
      : failed('unavailable');
  }
  if (status === 401) return failed('session-lost');
  if (status === 400) return failed('invalid');
  const failure = CODES[`${status} ${code}`] ?? 'unavailable';
  return failure === 'rate-limited'
    ? failed(failure, parseRetryAfter(response.headers.get('Retry-After')))
    : failed(failure);
}

async function settle<T>(
  run: () => Promise<RawResult>,
  parse: (data: unknown) => T | undefined,
): Promise<EvidenceResult<T>> {
  let result: RawResult;
  try {
    result = await run();
  } catch {
    return failed('unavailable');
  }
  if (!result.response.ok) return failureOf(result.response, result.error);
  const value = parse(result.data);
  return value === undefined ? failed('unavailable') : { kind: 'ok', value };
}

function isCandidate(value: unknown): value is EvidenceCandidate {
  if (!isRecord(value) || !isRecord(value.repository)) return false;
  const { id, fullName } = value.repository;
  return typeof value.candidateId === 'string'
    && /^[A-Za-z0-9_-]{22}$/u.test(value.candidateId)
    && isEvidenceKind(value.kind)
    && typeof value.title === 'string'
    && (id === null || (Number.isSafeInteger(id) && (id as number) > 0))
    && typeof fullName === 'string'
    && isIsoDate(value.date)
    && typeof value.attached === 'boolean';
}

function candidatePage(data: unknown): CandidatePage | undefined {
  const page = isRecord(data) && isRecord(data.data) ? data.data : undefined;
  if (page === undefined) return undefined;
  const { items, nextPage, incomplete } = page;
  const pageNumber = nextPage === null
    || (Number.isSafeInteger(nextPage) && (nextPage as number) > 1);
  return Array.isArray(items)
    && items.every(isCandidate)
    && pageNumber
    && typeof incomplete === 'boolean'
    ? { items, nextPage: nextPage as number | null, incomplete }
    : undefined;
}

function evidenceWritten(data: unknown): EvidenceWritten | undefined {
  const body = isRecord(data) && isRecord(data.data) ? data.data : undefined;
  return body !== undefined
    && isEvidenceList(body.evidence)
    && isRevision(body.projectRevision)
    ? { evidence: body.evidence, projectRevision: body.projectRevision }
    : undefined;
}

function guard(revision: string, key: string) {
  return { 'If-Match': `"r${revision}"`, 'Idempotency-Key': key };
}

export function createProjectEvidenceApi(
  options: ApiClientOptions = {},
): ProjectEvidenceApi {
  const client = createApiClient(options);
  return {
    candidates: (projectId, query, csrfToken) => settle(
      () => client.GET('/projects/{id}/evidence/candidates', {
        params: {
          path: { id: projectId },
          query: {
            kind: query.kind,
            page: query.page,
            ...(query.kind === 'release'
              ? { repository: query.repository }
              : {}),
          },
        },
        headers: { 'X-CSRF-Token': csrfToken },
        cache: 'no-store',
      }) as Promise<RawResult>,
      candidatePage,
    ),
    attach: (projectId, revision, body, key, csrfToken) => settle(
      () => client.POST('/projects/{id}/evidence', {
        params: { path: { id: projectId }, header: guard(revision, key) },
        headers: { 'X-CSRF-Token': csrfToken },
        body,
        cache: 'no-store',
      }) as Promise<RawResult>,
      evidenceWritten,
    ),
    recheck: (projectId, evidenceId, revision, key, csrfToken) => settle(
      () => client.POST('/projects/{id}/evidence/{evidenceId}/recheck', {
        params: {
          path: { id: projectId, evidenceId },
          header: guard(revision, key),
        },
        headers: { 'X-CSRF-Token': csrfToken },
        cache: 'no-store',
      }) as Promise<RawResult>,
      evidenceWritten,
    ),
    reorder: (projectId, revision, order, key, csrfToken) => settle(
      () => client.PUT('/projects/{id}/evidence/order', {
        params: { path: { id: projectId }, header: guard(revision, key) },
        headers: { 'X-CSRF-Token': csrfToken },
        body: { order: [...order] },
        cache: 'no-store',
      }) as Promise<RawResult>,
      evidenceWritten,
    ),
    remove: (projectId, evidenceId, revision, key, csrfToken) => settle(
      () => client.DELETE('/projects/{id}/evidence/{evidenceId}', {
        params: {
          path: { id: projectId, evidenceId },
          header: guard(revision, key),
        },
        headers: { 'X-CSRF-Token': csrfToken },
        cache: 'no-store',
      }) as Promise<RawResult>,
      evidenceWritten,
    ),
  };
}
