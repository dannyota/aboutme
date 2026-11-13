import { createApiClient, type ApiClientOptions } from '../../api/client';
import {
  sendWithCsrf,
  type ProjectAuth,
} from '../../composables/projectsApi';
import {
  createResumeApi,
  type ResumeApi,
  type ResumeListResult,
} from '../../editor/resumeApi';
import { canonicalJSON } from './projectDocument';

export type LinkFailure
  = | 'session-lost'
    | 'csrf-rejected'
    | 'stale'
    | 'already-linked'
    | 'document-invalid'
    | 'busy'
    | 'rate-limited'
    | 'unavailable'
    | 'project-not-found'
    | 'resume-not-found';

export type LinkResult
  = | { readonly kind: 'ok' }
    | { readonly kind: 'failed'; readonly failure: LinkFailure };

export interface ProjectLinksApi {
  link(
    resumeId: string,
    revision: string,
    projectId: string,
    key: string,
    csrfToken: string,
  ): Promise<LinkResult>;
}

function failed(failure: LinkFailure): LinkResult {
  return { kind: 'failed', failure };
}

function codeOf(error: unknown): unknown {
  if (typeof error !== 'object' || error === null) return null;
  const body = (error as { error?: unknown }).error;
  return typeof body === 'object' && body !== null
    ? (body as { code?: unknown }).code
    : null;
}

function failureOf(status: number, code: unknown): LinkResult {
  if (status === 401) return failed('session-lost');
  if (status === 403 && code === 'csrf_rejected') {
    return failed('csrf-rejected');
  }
  if (status === 404 && code === 'project_not_found') {
    return failed('project-not-found');
  }
  if (status === 404 && code === 'resume_not_found') {
    return failed('resume-not-found');
  }
  if (status === 409 && code === 'project_already_linked') {
    return failed('already-linked');
  }
  if (status === 412) return failed('stale');
  if (status === 422) return failed('document-invalid');
  if (status === 429) return failed('rate-limited');
  if (status === 503 && code === 'public_state_busy') return failed('busy');
  return failed('unavailable');
}

export function createProjectLinksApi(
  options: ApiClientOptions = {},
): ProjectLinksApi {
  const client = createApiClient(options);
  return {
    async link(resumeId, revision, projectId, key, csrfToken) {
      try {
        const result = await client.POST('/resumes/{id}/project-links', {
          params: {
            path: { id: resumeId },
            header: { 'If-Match': `"r${revision}"`, 'Idempotency-Key': key },
          },
          headers: { 'X-CSRF-Token': csrfToken },
          body: { projectId },
          cache: 'no-store',
        });
        return result.response.ok
          ? { kind: 'ok' }
          : failureOf(result.response.status, codeOf(result.error));
      } catch {
        return failed('unavailable');
      }
    },
  };
}

export interface ProjectLinker {
  resumes(): Promise<ResumeListResult>;
  link(
    ownerId: string,
    resumeId: string,
    projectId: string,
  ): Promise<LinkResult>;
}

export interface ProjectLinkerDeps {
  readonly links: ProjectLinksApi;
  readonly resumes: Pick<ResumeApi, 'list'>;
  readonly auth: ProjectAuth;
  readonly uuid: () => string;
}

const RETRYABLE = new Set<string>([
  'session-lost', 'csrf-rejected', 'busy', 'rate-limited', 'unavailable',
]);

export function createProjectLinker(
  deps: Partial<ProjectLinkerDeps> & Pick<ProjectLinkerDeps, 'auth'>,
): ProjectLinker {
  const links = deps.links ?? createProjectLinksApi();
  const resumes = deps.resumes ?? createResumeApi();
  const uuid = deps.uuid ?? (() => crypto.randomUUID());
  const keys = new Map<string, string>();

  async function revisionOf(
    resumeId: string,
  ): Promise<{ revision: string } | LinkResult> {
    const result = await resumes.list();
    if (result.kind === 'session-lost') return failed('session-lost');
    if (result.kind === 'rate-limited') return failed('rate-limited');
    if (result.kind !== 'ready') return failed('unavailable');
    const resume = result.items.find((item) => item.id === resumeId);
    return resume === undefined
      ? failed('resume-not-found')
      : { revision: resume.revision };
  }

  async function attempt(
    ownerId: string,
    resumeId: string,
    projectId: string,
  ): Promise<LinkResult> {
    const read = await revisionOf(resumeId);
    if ('kind' in read) return read;
    const { revision } = read;
    const fingerprint = canonicalJSON({
      ownerId, resumeId, projectId, revision,
    });
    const key = keys.get(fingerprint) ?? uuid();
    keys.set(fingerprint, key);
    const result = await sendWithCsrf(deps.auth, ownerId, (csrfToken) =>
      links.link(resumeId, revision, projectId, key, csrfToken));
    if (result.kind === 'ok' || !RETRYABLE.has(result.failure)) {
      keys.delete(fingerprint);
    }
    return result;
  }

  return {
    resumes: () => resumes.list(),
    async link(ownerId, resumeId, projectId) {
      if (
        deps.auth.authState.value !== 'authenticated'
        || deps.auth.user.value?.id !== ownerId
      ) {
        return failed('session-lost');
      }
      const first = await attempt(ownerId, resumeId, projectId);
      if (first.kind === 'failed' && first.failure === 'stale') {
        return attempt(ownerId, resumeId, projectId);
      }
      return first;
    },
  };
}

const unlinked = new Map<string, readonly string[]>();

export function rememberUnlinked(
  projectId: string,
  titles: readonly string[],
): void {
  if (titles.length > 0) unlinked.set(projectId, [...titles]);
}

export function takeUnlinked(projectId: string): readonly string[] {
  const titles = unlinked.get(projectId) ?? [];
  unlinked.delete(projectId);
  return titles;
}
