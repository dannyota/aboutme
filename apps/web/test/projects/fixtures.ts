import { CURRENT_VERSION } from '@aboutme/schema/released';
import { computed, ref } from 'vue';
import { vi } from 'vitest';

import type {
  Project,
  ProjectEvidence,
} from '../../app/components/projects/projectDocument';
import type {
  EvidenceCandidate,
  EvidenceFailure,
  EvidenceResult,
  ProjectEvidenceApi,
} from '../../app/composables/projectEvidenceApi';
import type {
  ProjectAuth,
  ProjectFailure,
  ProjectResult,
  ProjectsApi,
} from '../../app/composables/projectsApi';
import type { AuthState } from '../../app/composables/useAuth';
import type { ResumeSummary } from '../../app/editor/resumeApi';
import { parseRevision } from '../../app/editor/revision';

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

/** One resume of the account, as the resume list returns it. */
export function resumeSummary(
  id: string,
  revision = '1',
  title = id,
  overrides: Partial<ResumeSummary> = {},
): ResumeSummary {
  return {
    id,
    title,
    lng: 'en',
    live: false,
    downloadEnabled: false,
    seoGeoEnabled: false,
    signInToView: false,
    slug: null,
    publicTitle: null,
    faviconEmoji: null,
    schemaVersion: CURRENT_VERSION,
    createdAt: '2026-11-01T00:00:00Z',
    updatedAt: '2026-11-01T00:00:00Z',
    revision: parseRevision(revision),
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
    mutate: vi.fn(answered(204)),
  } satisfies ProjectAuth;
  return { auth, user, csrfToken, authState, identities };
}

/** A fake `mutate` answer: the status it reports, then its body. */
export function answered(status: number, body?: unknown) {
  return async (
    _url: string,
    options: { onStatus?: (value: number) => void },
  ): Promise<never> => {
    options.onStatus?.(status);
    return body as never;
  };
}

/** Keys k1, k2, … so a test can tell a fresh key from a reused one. */
export function sequence(prefix = 'k'): () => string {
  let next = 0;
  return () => {
    next += 1;
    return `${prefix}${next}`;
  };
}

export const EVIDENCE_IDS = [
  '0190f000-0000-7000-8000-000000000e01',
  '0190f000-0000-7000-8000-000000000e02',
  '0190f000-0000-7000-8000-000000000e03',
] as const;

export function evidenceItem(
  overrides: Partial<ProjectEvidence> = {},
): ProjectEvidence {
  return {
    id: EVIDENCE_IDS[0],
    kind: 'pullRequest',
    url: 'https://github.com/octocat/hello/pull/42',
    repositoryName: 'octocat/hello',
    title: 'Fix the thing',
    itemDate: '2026-01-02T03:04:05Z',
    checkedAt: '2026-11-17T08:00:00Z',
    confirmed: true,
    flagReason: null,
    ...overrides,
  };
}

export function candidate(
  overrides: Partial<EvidenceCandidate> = {},
): EvidenceCandidate {
  return {
    candidateId: 'Zm9vYmFyYmF6cXV4MDEyMw',
    kind: 'repository',
    title: 'octocat/hello',
    repository: { id: 101, fullName: 'octocat/hello' },
    date: '2025-03-04T05:06:07Z',
    attached: false,
    ...overrides,
  };
}

export function written(
  evidence: readonly ProjectEvidence[],
  projectRevision: string,
): EvidenceResult<{
  evidence: ProjectEvidence[];
  projectRevision: string;
}> {
  return { kind: 'ok', value: { evidence: [...evidence], projectRevision } };
}

export function evidenceFailed(
  failure: EvidenceFailure,
  retryAfter: number | null = null,
): EvidenceResult<never> {
  return { kind: 'failed', failure, retryAfter };
}

export function fakeEvidenceApi() {
  const unavailable = evidenceFailed('unavailable');
  return {
    candidates: vi.fn<ProjectEvidenceApi['candidates']>()
      .mockResolvedValue(unavailable),
    attach: vi.fn<ProjectEvidenceApi['attach']>()
      .mockResolvedValue(unavailable),
    recheck: vi.fn<ProjectEvidenceApi['recheck']>()
      .mockResolvedValue(unavailable),
    reorder: vi.fn<ProjectEvidenceApi['reorder']>()
      .mockResolvedValue(unavailable),
    remove: vi.fn<ProjectEvidenceApi['remove']>()
      .mockResolvedValue(unavailable),
  };
}
