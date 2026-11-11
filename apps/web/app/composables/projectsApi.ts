import { ref, type Ref } from 'vue';

import { createApiClient, type ApiClientOptions } from '../api/client';
import type {
  CreateProjectRequest,
  Project,
  ProjectRequest,
} from '../components/projects/projectDocument';
import { requestFromProject } from '../components/projects/projectDocument';
import { mapReauthError, mapReauthStartError } from './passwordSettings';
import { validateAuthorizeUrl } from './providerAuthorization';
import type { UseAuthReturn } from './useAuth';

export type ProjectFailure
  = | 'session-lost'
    | 'csrf-rejected'
    | 'not-found'
    | 'limit'
    | 'slug-taken'
    | 'slug-held'
    | 'reauth-required'
    | 'invalid'
    | 'rate-limited'
    | 'unavailable';

export type ProjectResult<T>
  = | { readonly kind: 'ok'; readonly value: T }
    | { readonly kind: 'stale'; readonly project: Project }
    | { readonly kind: 'failed'; readonly failure: ProjectFailure };

export interface ProjectsApi {
  list(): Promise<ProjectResult<Project[]>>;
  read(id: string): Promise<ProjectResult<Project>>;
  create(
    body: CreateProjectRequest,
    key: string,
    csrfToken: string,
  ): Promise<ProjectResult<Project>>;
  replace(
    id: string,
    revision: string,
    body: ProjectRequest,
    key: string,
    csrfToken: string,
  ): Promise<ProjectResult<Project>>;
  remove(
    id: string,
    revision: string,
    key: string,
    csrfToken: string,
  ): Promise<ProjectResult<null>>;
}

export type ProjectAuth = Pick<
  UseAuthReturn,
  'user' | 'csrfToken' | 'authState' | 'identities' | 'refresh' | 'mutate'
>;

interface RawResult {
  readonly data?: unknown;
  readonly error?: unknown;
  readonly response: Response;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isProject(value: unknown): value is Project {
  if (!isRecord(value) || !isRecord(value.text)) return false;
  const technologies = value.technologies;
  return typeof value.id === 'string'
    && value.id !== ''
    && typeof value.slug === 'string'
    && typeof value.revision === 'string'
    && /^[0-9]+$/u.test(value.revision)
    && typeof value.updatedAt === 'string'
    && typeof value.defaultLanguage === 'string'
    && isRecord(value.text[value.defaultLanguage])
    && Object.values(value.text).every(isRecord)
    && (technologies === undefined
      || (Array.isArray(technologies)
        && technologies.every((item) => typeof item === 'string')));
}

function codeOf(error: unknown): unknown {
  return isRecord(error) && isRecord(error.error) ? error.error.code : null;
}

function failureOf(status: number, error: unknown): ProjectResult<never> {
  const code = codeOf(error);
  const failed = (failure: ProjectFailure) =>
    ({ kind: 'failed', failure }) as const;
  if (status === 412 && code === 'revision_mismatch') {
    const details = isRecord(error) && isRecord(error.error)
      ? error.error.details
      : undefined;
    const project = isRecord(details) ? details.project : undefined;
    return isProject(project)
      ? { kind: 'stale', project }
      : failed('unavailable');
  }
  if (status === 401) return failed('session-lost');
  if (status === 403 && code === 'csrf_rejected') {
    return failed('csrf-rejected');
  }
  if (status === 403 && code === 'reauth_required') {
    return failed('reauth-required');
  }
  if (status === 404 && code === 'project_not_found') {
    return failed('not-found');
  }
  if (status === 409 && code === 'project_limit') return failed('limit');
  if (status === 409 && code === 'project_slug_taken') {
    return failed('slug-taken');
  }
  if (status === 409 && code === 'project_slug_held') {
    return failed('slug-held');
  }
  if (status === 422) return failed('invalid');
  if (status === 429) return failed('rate-limited');
  return failed('unavailable');
}

async function settle<T>(
  run: () => Promise<RawResult>,
  parse: (data: unknown) => T | undefined,
): Promise<ProjectResult<T>> {
  let result: RawResult;
  try {
    result = await run();
  } catch {
    return { kind: 'failed', failure: 'unavailable' };
  }
  if (!result.response.ok) {
    return failureOf(result.response.status, result.error);
  }
  const value = parse(result.data);
  return value === undefined
    ? { kind: 'failed', failure: 'unavailable' }
    : { kind: 'ok', value };
}

function projectData(data: unknown): Project | undefined {
  return isRecord(data) && isProject(data.data) ? data.data : undefined;
}

function etag(revision: string): string {
  return `"r${revision}"`;
}

/** The owner project routes through the generated client. */
export function createProjectsApi(
  options: ApiClientOptions = {},
): ProjectsApi {
  const client = createApiClient(options);
  return {
    list: () => settle(
      () => client.GET('/projects', {
        cache: 'no-store',
      }) as Promise<RawResult>,
      (data) => {
        const projects = isRecord(data) && isRecord(data.data)
          ? data.data.projects
          : undefined;
        return Array.isArray(projects) && projects.every(isProject)
          ? projects
          : undefined;
      },
    ),
    read: (id) => settle(
      () => client.GET('/projects/{id}', {
        params: { path: { id } },
        cache: 'no-store',
      }) as Promise<RawResult>,
      projectData,
    ),
    create: (body, key, csrfToken) => settle(
      () => client.POST('/projects', {
        params: { header: { 'Idempotency-Key': key } },
        headers: { 'X-CSRF-Token': csrfToken },
        body,
        cache: 'no-store',
      }) as Promise<RawResult>,
      projectData,
    ),
    replace: (id, revision, body, key, csrfToken) => settle(
      () => client.PUT('/projects/{id}', {
        params: {
          path: { id },
          header: { 'If-Match': etag(revision), 'Idempotency-Key': key },
        },
        headers: { 'X-CSRF-Token': csrfToken },
        body,
        cache: 'no-store',
      }) as Promise<RawResult>,
      (data) => {
        const project = isRecord(data) && isRecord(data.data)
          ? data.data.project
          : undefined;
        return isProject(project) ? project : undefined;
      },
    ),
    remove: (id, revision, key, csrfToken) => settle(
      () => client.DELETE('/projects/{id}', {
        params: {
          path: { id },
          header: { 'If-Match': etag(revision), 'Idempotency-Key': key },
        },
        headers: { 'X-CSRF-Token': csrfToken },
        cache: 'no-store',
      }) as Promise<RawResult>,
      () => null,
    ),
  };
}

/** The CSRF token while the session still belongs to `ownerId`. */
function tokenFor(auth: ProjectAuth, ownerId: string): string | null {
  const token = auth.csrfToken.value;
  return token !== null
    && auth.authState.value === 'authenticated'
    && auth.user.value?.id === ownerId
    ? token
    : null;
}

const SESSION_LOST = { kind: 'failed', failure: 'session-lost' } as const;

/**
 * Sends one write of `ownerId`'s intent. A rotated token is refreshed and
 * the same request, with the same key, is sent once more. Another tab can
 * switch the session to a different account meanwhile: then nothing more
 * is sent and no result is handed back for the new account to adopt.
 */
export async function sendWithCsrf<T>(
  auth: ProjectAuth,
  ownerId: string,
  send: (csrfToken: string) => Promise<ProjectResult<T>>,
): Promise<ProjectResult<T>> {
  const first = tokenFor(auth, ownerId);
  if (first === null) return SESSION_LOST;
  const result = await send(first);
  if (tokenFor(auth, ownerId) === null) return SESSION_LOST;
  if (result.kind !== 'failed' || result.failure !== 'csrf-rejected') {
    return result;
  }
  try {
    await auth.refresh();
  } catch {
    return result;
  }
  const second = tokenFor(auth, ownerId);
  if (second === null) return SESSION_LOST;
  const retried = await send(second);
  return tokenFor(auth, ownerId) === null ? SESSION_LOST : retried;
}

export type ReleaseKind = 'address' | 'delete';

export type ReleaseState
  = | { readonly kind: 'idle' }
    | { readonly kind: 'busy' }
    | { readonly kind: 'done' }
    | { readonly kind: 'stale' }
    | { readonly kind: 'slug-taken' }
    | { readonly kind: 'slug-held' }
    | { readonly kind: 'not-found' }
    | { readonly kind: 'invalid' }
    | { readonly kind: 'try-again' }
    | { readonly kind: 'session-lost' }
    | { readonly kind: 'no-reauth-method' }
    | {
      readonly kind:
        | 'reauth-required'
        | 'reauth-rate-limited'
        | 'reauth-unavailable';
      readonly method: 'password' | 'provider';
    }
    | {
      readonly kind:
        | 'reauth-wrong-password'
        | 'reauth-second-factor-incomplete';
      readonly method: 'password';
    }
    | { readonly kind: 'reauth-second-factor' }
    | {
      readonly kind: 'provider-start-invalid' | 'provider-started-rate-limited';
      readonly method: 'provider';
    }
    | { readonly kind: 'provider-started'; readonly authorizeUrl: string };

interface PasswordReauthEnvelope {
  readonly data?: { readonly secondFactorRequired?: unknown };
}

interface ReleaseAttempt {
  readonly kind: ReleaseKind;
  readonly project: Project;
  readonly slug: string;
  readonly key: string;
  readonly ownerId: string;
}

export interface ReleaseActionDeps {
  readonly api: ProjectsApi;
  readonly auth: ProjectAuth;
  readonly uuid: () => string;
  readonly providerLogin: Readonly<Ref<boolean>>;
  /** The released project: the renamed one, or null after a deletion. */
  readonly onDone: (kind: ReleaseKind, project: Project | null) =>
    void | Promise<void>;
  readonly onStale: (project: Project) => void;
}

export interface ReleaseAction {
  readonly state: Readonly<Ref<ReleaseState>>;
  start(kind: ReleaseKind, project: Project, slug?: string): Promise<void>;
  reauthPassword(password: string): Promise<void>;
  startProviderReauth(): Promise<void>;
  retryAfterProviderReauth(): Promise<void>;
  continueAfterSecondFactor(): Promise<void>;
  reset(): void;
}

const PASSWORD_STATES = new Set<ReleaseState['kind']>([
  'reauth-required', 'reauth-wrong-password', 'reauth-rate-limited',
  'reauth-unavailable', 'reauth-second-factor-incomplete',
]);
const PROVIDER_START_STATES = new Set<ReleaseState['kind']>([
  'reauth-required', 'reauth-rate-limited', 'reauth-unavailable',
  'provider-start-invalid', 'provider-started-rate-limited',
]);

function methodOf(state: ReleaseState): 'password' | 'provider' | null {
  return 'method' in state ? state.method : null;
}

/**
 * Change of address and deletion release a slug, so each may need the
 * reauthentication step the publish dialog uses. After that step, and after
 * a second factor the owner completes in another tab, the same request,
 * with the same idempotency key, is sent once more.
 */
export function createReleaseAction(deps: ReleaseActionDeps): ReleaseAction {
  const state = ref<ReleaseState>({ kind: 'idle' });
  let attempt: ReleaseAttempt | null = null;
  let running = false;

  const sameOwner = (current: ReleaseAttempt): boolean =>
    deps.auth.authState.value === 'authenticated'
    && deps.auth.user.value?.id === current.ownerId;

  const provider = () => deps.auth.identities.value[0]?.provider ?? null;

  function send(current: ReleaseAttempt): Promise<ProjectResult<unknown>> {
    const { project } = current;
    return sendWithCsrf<unknown>(deps.auth, current.ownerId, (csrfToken) =>
      current.kind === 'delete'
        ? deps.api.remove(project.id, project.revision, current.key, csrfToken)
        : deps.api.replace(
            project.id,
            project.revision,
            requestFromProject(project, current.slug),
            current.key,
            csrfToken,
          ));
  }

  async function dispatch(
    current: ReleaseAttempt,
    afterSecondFactor = false,
  ): Promise<void> {
    if (!sameOwner(current)) {
      attempt = null;
      state.value = { kind: 'session-lost' };
      return;
    }
    state.value = { kind: 'busy' };
    const result = await send(current);
    if (attempt !== current) return;
    if (result.kind === 'ok') {
      attempt = null;
      state.value = { kind: 'done' };
      await deps.onDone(current.kind, current.kind === 'delete'
        ? null
        : result.value as Project);
      return;
    }
    if (result.kind === 'stale') {
      attempt = null;
      state.value = { kind: 'stale' };
      deps.onStale(result.project);
      return;
    }
    const failure = result.failure;
    if (failure === 'reauth-required') {
      const hasPassword = deps.auth.user.value?.hasPassword !== false;
      if (afterSecondFactor && hasPassword) {
        state.value = {
          kind: 'reauth-second-factor-incomplete', method: 'password',
        };
        return;
      }
      state.value = hasPassword
        ? { kind: 'reauth-required', method: 'password' }
        : deps.providerLogin.value && provider() !== null
          ? { kind: 'reauth-required', method: 'provider' }
          : { kind: 'no-reauth-method' };
      return;
    }
    if (
      failure === 'rate-limited'
      || failure === 'unavailable'
      || failure === 'csrf-rejected'
    ) {
      // The outcome is unknown or nothing ran, so a retry reuses this key.
      state.value = { kind: 'try-again' };
      return;
    }
    attempt = null;
    state.value = failure === 'slug-taken' || failure === 'slug-held'
      || failure === 'not-found' || failure === 'invalid'
      || failure === 'session-lost'
      ? { kind: failure }
      : { kind: 'try-again' };
  }

  async function guarded(run: () => Promise<void>): Promise<void> {
    if (running) return;
    running = true;
    try {
      await run();
    } finally {
      running = false;
    }
  }

  /** Resends once after a step the owner finished in another tab. */
  function resendAfterOtherTab(
    from: 'provider-started' | 'reauth-second-factor',
  ): Promise<void> {
    return guarded(async () => {
      const current = attempt;
      if (current === null || state.value.kind !== from) return;
      state.value = { kind: 'busy' };
      try {
        await deps.auth.refresh();
      } catch {
        attempt = null;
        state.value = { kind: 'session-lost' };
        return;
      }
      if (attempt === current) {
        await dispatch(current, from === 'reauth-second-factor');
      }
    });
  }

  return {
    state,
    start: (kind, project, slug = project.slug) => guarded(async () => {
      const ownerId = deps.auth.user.value?.id;
      if (
        ownerId === undefined
        || deps.auth.authState.value !== 'authenticated'
      ) {
        state.value = { kind: 'session-lost' };
        return;
      }
      const reuse = attempt !== null
        && attempt.kind === kind
        && attempt.project.id === project.id
        && attempt.project.revision === project.revision
        && attempt.slug === slug
        && attempt.ownerId === ownerId;
      if (!reuse) {
        attempt = { kind, project, slug, key: deps.uuid(), ownerId };
      }
      await dispatch(attempt!);
    }),
    reauthPassword: (password) => guarded(async () => {
      const current = attempt;
      if (
        current === null
        || !PASSWORD_STATES.has(state.value.kind)
        || methodOf(state.value) !== 'password'
      ) return;
      state.value = { kind: 'busy' };
      let response: PasswordReauthEnvelope | undefined;
      try {
        response = await deps.auth.mutate<PasswordReauthEnvelope | undefined>(
          '/api/v1/auth/password/reauth',
          { method: 'POST', body: { password } },
        );
      } catch (error) {
        if (attempt !== current) return;
        const reason = mapReauthError(error).kind;
        state.value = reason === 'reauth-failed'
          ? { kind: 'reauth-wrong-password', method: 'password' }
          : reason === 'rate-limited'
            ? { kind: 'reauth-rate-limited', method: 'password' }
            : { kind: 'reauth-unavailable', method: 'password' };
        return;
      }
      if (attempt !== current) return;
      if (!sameOwner(current)) {
        attempt = null;
        state.value = { kind: 'session-lost' };
        return;
      }
      // An enrolled account's password only opens a pending factor; the
      // server still refuses the action until that factor completes, so the
      // attempt waits for an explicit Continue instead of resending now.
      if (response?.data?.secondFactorRequired === true) {
        state.value = { kind: 'reauth-second-factor' };
        return;
      }
      await dispatch(current);
    }),
    startProviderReauth: () => guarded(async () => {
      const current = attempt;
      const name = provider();
      if (
        current === null
        || !PROVIDER_START_STATES.has(state.value.kind)
        || methodOf(state.value) !== 'provider'
        || name === null
      ) return;
      state.value = { kind: 'busy' };
      try {
        const response = await deps.auth.mutate<{
          data?: { authorizeUrl?: unknown };
        }>(`/api/v1/auth/${name}/start`, {
          method: 'POST',
          query: { purpose: 'reauth' },
        });
        if (attempt !== current) return;
        const authorizeUrl = validateAuthorizeUrl(
          name, response?.data?.authorizeUrl,
        );
        state.value = authorizeUrl === null
          ? { kind: 'provider-start-invalid', method: 'provider' }
          : { kind: 'provider-started', authorizeUrl };
      } catch (error) {
        if (attempt !== current) return;
        state.value = mapReauthStartError(error).kind === 'rate-limited'
          ? { kind: 'provider-started-rate-limited', method: 'provider' }
          : { kind: 'reauth-unavailable', method: 'provider' };
      }
    }),
    retryAfterProviderReauth: () => resendAfterOtherTab('provider-started'),
    continueAfterSecondFactor: () =>
      resendAfterOtherTab('reauth-second-factor'),
    reset: () => {
      if (running) return;
      attempt = null;
      state.value = { kind: 'idle' };
    },
  };
}
