import { createApiClient, type ApiClientOptions } from '../../api/client';
import {
  createProjectsApi,
  sendWithCsrf,
  type ProjectsApi,
  type ProjectAuth,
} from '../../composables/projectsApi';
import {
  createResumeApi,
  type ResumeApi,
  type ResumeListResult,
} from '../../editor/resumeApi';
import { projectPrivacyRequired } from '../../editor/projectLinks';
import { canonicalJSON } from './projectDocument';

// Linking a library project to a resume from the project screens
// (docs/design/ui/projects.md "Showing a project on resumes"). A link is a
// resume write, so it carries the resume revision, not the project's.

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
    | 'resume-not-found'
    | 'privacy-required';

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

/** The link route through the generated client. */
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
  /** The account's resumes, with the revision each one has now. */
  resumes(): Promise<ResumeListResult>;
  link(
    ownerId: string,
    resumeId: string,
    projectId: string,
    privacyConfirmed?: boolean,
  ): Promise<LinkResult>;
}

export interface ProjectLinkerDeps {
  readonly links: ProjectLinksApi;
  readonly resumes: Pick<ResumeApi, 'list'>;
  readonly projects: Pick<ProjectsApi, 'read'>;
  readonly auth: ProjectAuth;
  readonly uuid: () => string;
}

// Nothing was written, or the outcome is unknown, so the same request may be
// sent again with its key and the server answers it at most once.
const RETRYABLE = new Set<string>([
  'session-lost', 'csrf-rejected', 'busy', 'rate-limited', 'unavailable',
]);

/**
 * Links a project to one resume of `ownerId`. The resume's current revision
 * is read when the action starts; a revision mismatch reads it again and
 * repeats the link once. A key is kept only to retry the identical request.
 */
export function createProjectLinker(
  deps: Partial<ProjectLinkerDeps> & Pick<ProjectLinkerDeps, 'auth'>,
): ProjectLinker {
  const links = deps.links ?? createProjectLinksApi();
  const resumes = deps.resumes ?? createResumeApi();
  const projects = deps.projects ?? createProjectsApi();
  const uuid = deps.uuid ?? (() => crypto.randomUUID());
  const keys = new Map<string, string>();

  async function revisionOf(
    resumeId: string,
    projectId: string,
    privacyConfirmed: boolean,
  ): Promise<{ revision: string } | LinkResult> {
    const [result, project] = await Promise.all([
      resumes.list(), projects.read(projectId),
    ]);
    const required = projectPrivacyRequired(
      resumeId,
      project.kind === 'ok' ? project.value.linkedResumeIds ?? [] : null,
      result.kind === 'ready' ? result.items : null,
    );
    if (required && !privacyConfirmed) return failed('privacy-required');
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
    privacyConfirmed: boolean,
  ): Promise<LinkResult> {
    const read = await revisionOf(resumeId, projectId, privacyConfirmed);
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
    async link(ownerId, resumeId, projectId, privacyConfirmed = false) {
      if (
        deps.auth.authState.value !== 'authenticated'
        || deps.auth.user.value?.id !== ownerId
      ) {
        return failed('session-lost');
      }
      const first = await attempt(
        ownerId, resumeId, projectId, privacyConfirmed,
      );
      if (first.kind === 'failed' && first.failure === 'stale') {
        return attempt(ownerId, resumeId, projectId, privacyConfirmed);
      }
      return first;
    },
  };
}

// The titles of resumes a new project could not join, kept in memory until
// its editor opens and shows them once.
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
