import { computed, ref } from 'vue';
import { vi } from 'vitest';

import type { Project } from '../../app/components/projects/projectDocument';
import type {
  ProjectAuth,
  ProjectFailure,
  ProjectResult,
  ProjectsApi,
} from '../../app/composables/projectsApi';
import type { AuthState } from '../../app/composables/useAuth';

export function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    slug: 'shop-site',
    revision: '3',
    createdAt: '2026-11-01T00:00:00Z',
    updatedAt: '2026-11-02T00:00:00Z',
    defaultLanguage: 'en',
    text: { en: { name: 'Shop site', role: 'Lead engineer' } },
    technologies: ['Go', 'Vue'],
    linkedResumeIds: [],
    ...overrides,
  };
}

export function ok<T>(value: T): ProjectResult<T> {
  return { kind: 'ok', value };
}

export function failed(failure: ProjectFailure): ProjectResult<never> {
  return { kind: 'failed', failure };
}

export function stale(winner: Project): ProjectResult<never> {
  return { kind: 'stale', project: winner };
}

/** Every call a test did not script answers as unavailable. */
export function fakeApi() {
  const unavailable = failed('unavailable');
  return {
    list: vi.fn<ProjectsApi['list']>().mockResolvedValue(unavailable),
    read: vi.fn<ProjectsApi['read']>().mockResolvedValue(unavailable),
    create: vi.fn<ProjectsApi['create']>().mockResolvedValue(unavailable),
    replace: vi.fn<ProjectsApi['replace']>().mockResolvedValue(unavailable),
    remove: vi.fn<ProjectsApi['remove']>().mockResolvedValue(unavailable),
  };
}

export function fakeAuth(options: { hasPassword?: boolean } = {}) {
  const user = ref({
    id: 'owner-1',
    email: 'owner@example.invalid',
    name: null,
    avatarKey: null,
    hasPassword: options.hasPassword ?? true,
  });
  const csrfToken = ref<string | null>('csrf-1');
  const authState = ref<AuthState>('authenticated');
  const identities = ref<{
    id: string;
    provider: 'google';
    createdAt: string;
  }[]>([]);
  const auth = {
    user: computed(() => user.value),
    csrfToken: computed(() => csrfToken.value),
    authState: computed(() => authState.value),
    identities: computed(() => identities.value),
    refresh: vi.fn(async () => {}),
    mutate: vi.fn(async () => undefined as never),
  } satisfies ProjectAuth;
  return { auth, user, csrfToken, authState, identities };
}

/** Keys k1, k2, … so a test can tell a fresh key from a reused one. */
export function sequence(prefix = 'k'): () => string {
  let next = 0;
  return () => {
    next += 1;
    return `${prefix}${next}`;
  };
}
