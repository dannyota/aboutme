import { createApiClient, type ApiClientOptions } from '../api/client';
import { isIsoDate, isRecord } from '../components/projects/projectDocument';

export interface GitHubEvidenceStatus {
  readonly generation: number;
  readonly hasEvidence: boolean;
  readonly connection: {
    readonly login: string;
    readonly connectedAt: string;
  } | null;
  readonly expiresAt: string | null;
}

export type GitHubEvidenceFailure
  = | 'session-lost'
    | 'csrf-rejected'
    | 'unavailable'
    | 'session-ended'
    | 'upstream'
    | 'rate-limited'
    | 'connection-changed'
    | 'failed';

export type GitHubEvidenceOutcome<T>
  = | { readonly kind: 'ok'; readonly value: T }
    | { readonly kind: 'failed'; readonly failure: GitHubEvidenceFailure };

export interface GitHubEvidenceApi {
  status(): Promise<GitHubEvidenceOutcome<GitHubEvidenceStatus>>;
  start(
    csrfToken: string,
    returnTo?: string,
  ): Promise<GitHubEvidenceOutcome<unknown>>;
  endSession(csrfToken: string): Promise<GitHubEvidenceOutcome<null>>;
  disconnect(
    keepLinks: boolean,
    generation: number,
    key: string,
    csrfToken: string,
  ): Promise<GitHubEvidenceOutcome<null>>;
}

interface RawResult {
  readonly data?: unknown;
  readonly error?: unknown;
  readonly response: Response;
}

function connectionFrom(
  value: unknown,
): GitHubEvidenceStatus['connection'] | undefined {
  if (value === null) return null;
  if (!isRecord(value)) return undefined;
  const { githubUserId, login, connectedAt } = value;
  return typeof githubUserId === 'number'
    && typeof login === 'string'
    && login !== ''
    && isIsoDate(connectedAt)
    ? { login, connectedAt }
    : undefined;
}

function expiryFrom(value: unknown): string | null | undefined {
  if (value === null) return null;
  if (!isRecord(value)) return undefined;
  return isIsoDate(value.expiresAt) ? value.expiresAt : undefined;
}

function statusFrom(data: unknown): GitHubEvidenceStatus | undefined {
  if (!isRecord(data) || !isRecord(data.data)) return undefined;
  const { generation, hasEvidence } = data.data;
  if (
    typeof generation !== 'number'
    || !Number.isSafeInteger(generation)
    || generation < 0
    || typeof hasEvidence !== 'boolean'
  ) {
    return undefined;
  }
  const connection = connectionFrom(data.data.connection);
  const expiresAt = expiryFrom(data.data.session);
  return connection === undefined || expiresAt === undefined
    ? undefined
    : { generation, hasEvidence, connection, expiresAt };
}

function authorizeUrlFrom(data: unknown): unknown {
  return isRecord(data) && isRecord(data.data) ? data.data.authorizeUrl : null;
}

function failureOf(status: number, error: unknown): GitHubEvidenceFailure {
  const code = isRecord(error) && isRecord(error.error)
    ? error.error.code
    : undefined;
  if (status === 401) return 'session-lost';
  if (status === 403 && code === 'csrf_rejected') return 'csrf-rejected';
  if (code === 'evidence_unavailable') return 'unavailable';
  if (code === 'evidence_session_required') return 'session-ended';
  if (code === 'rate_limited') return 'rate-limited';
  if (code === 'evidence_upstream') return 'upstream';
  if (code === 'evidence_connection_changed') return 'connection-changed';
  return 'failed';
}

async function settle<T>(
  run: () => Promise<RawResult>,
  parse: (data: unknown) => T | undefined,
): Promise<GitHubEvidenceOutcome<T>> {
  let result: RawResult;
  try {
    result = await run();
  } catch {
    return { kind: 'failed', failure: 'failed' };
  }
  if (!result.response.ok) {
    return {
      kind: 'failed',
      failure: failureOf(result.response.status, result.error),
    };
  }
  const value = parse(result.data);
  return value === undefined
    ? { kind: 'failed', failure: 'failed' }
    : { kind: 'ok', value };
}

export function createGitHubEvidenceApi(
  options: ApiClientOptions = {},
): GitHubEvidenceApi {
  const client = createApiClient(options);
  return {
    status: () => settle(
      () => client.GET('/github-evidence', {
        cache: 'no-store',
      }) as Promise<RawResult>,
      statusFrom,
    ),
    start: (csrfToken, returnTo) => settle(
      () => client.POST('/github-evidence/start', {
        headers: { 'X-CSRF-Token': csrfToken },
        ...(returnTo === undefined ? {} : { body: { returnTo } }),
        cache: 'no-store',
      }) as Promise<RawResult>,
      authorizeUrlFrom,
    ),
    endSession: (csrfToken) => settle(
      () => client.POST('/github-evidence/session/end', {
        headers: { 'X-CSRF-Token': csrfToken },
        cache: 'no-store',
      }) as Promise<RawResult>,
      () => null,
    ),
    disconnect: (keepLinks, generation, key, csrfToken) => settle(
      () => client.DELETE('/github-evidence', {
        params: {
          query: { keepLinks: keepLinks ? 'true' : 'false', generation },
          header: { 'Idempotency-Key': key },
        },
        headers: { 'X-CSRF-Token': csrfToken },
        cache: 'no-store',
      }) as Promise<RawResult>,
      () => null,
    ),
  };
}
