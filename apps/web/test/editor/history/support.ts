import { ref } from 'vue';
import { vi } from 'vitest';

import type {
  CheckpointSummary,
  CheckpointVersion,
  HistoryApi,
} from '../../../app/editor/historyApi';
import type {
  HistoryControllerDeps,
} from '../../../app/editor/historyController';
import { acceptedFixture } from '../fixture';

export function item(
  id: string,
  over: Partial<CheckpointSummary> = {},
): CheckpointSummary {
  return {
    id,
    startedAt: new Date(new Date().getFullYear(), 0, 5, 14, 5).toISOString(),
    cause: 'edit',
    actor: { kind: 'owner' },
    title: 'CV',
    byteSize: 10,
    ...over,
  };
}

export function version(
  id: string,
  over: Partial<CheckpointVersion> = {},
): CheckpointVersion {
  const base = acceptedFixture();
  return { ...item(id), lng: 'en', document: base.document, ...over };
}

export function fakeApi(items: CheckpointSummary[] = []) {
  const api = {
    list: vi.fn(async () => ({ kind: 'ok' as const, value: items })),
    read: vi.fn(async (_resume: string, id: string) => ({
      kind: 'ok' as const,
      value: version(id),
    })),
    write: vi.fn(async () => ({ kind: 'done' as const })),
  };
  return api satisfies HistoryApi;
}

export function fakeDeps(api: HistoryApi, record = fakeRecord()) {
  let n = 0;
  const store = {
    recordFor: vi.fn(() => record),
    adoptComplete: vi.fn(),
    adoptStaleWinner: vi.fn(),
    setTemplateState: vi.fn(),
    markSessionLost: vi.fn(),
  };
  const auth = {
    user: ref({ id: 'owner-1' }),
    csrfToken: ref<string | null>('csrf-1'),
    authState: ref('authenticated'),
    refresh: vi.fn(async () => {}),
  };
  const deps = {
    resumeId: 'resume-1',
    store,
    auth,
    api,
    runtime: {
      uuid: () => `key-${++n}`,
      nowEpochMs: () => 1000,
      delay: async () => {},
    },
    refresh: vi.fn(async () => {}),
  } as unknown as HistoryControllerDeps;
  return { deps, store, auth, record };
}

export function fakeRecord() {
  const accepted = acceptedFixture();
  return {
    accepted,
    current: { document: accepted.document, metadata: accepted.metadata },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' },
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}
