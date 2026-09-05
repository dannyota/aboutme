import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { computed, nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import ResumeEditorPage from '../../app/pages/app/resumes/[id].vue';
import { acceptedFixture } from './fixture';

const mocks = vi.hoisted(() => ({
  auth: undefined as unknown,
  api: undefined as unknown,
  actions: undefined as unknown,
  controllerOptions: undefined as unknown,
  stop: vi.fn(),
}));

mockNuxtImport('useAuth', () => () => mocks.auth);
mockNuxtImport('useRoute', () => () => ({ params: { id: 'resume-1' } }));
mockNuxtImport('navigateTo', () => vi.fn());

vi.mock('../../app/editor/resumeApi', () => ({
  createResumeApi: () => mocks.api,
}));
vi.mock('../../app/composables/useResumeEditor', () => ({
  useResumeEditor: () => mocks.actions,
}));
vi.mock('../../app/composables/useUnsavedNavigationGuard', () => ({
  shouldRetainEditorOnSessionLoss: () => true,
  useUnsavedNavigationGuard: vi.fn(),
}));
vi.mock('../../app/editor/photoController', () => ({
  createPhotoController: () => ({ sync: vi.fn(), clear: vi.fn() }),
}));
vi.mock('../../app/realtime/controller', () => ({
  createRealtimeController: (options: unknown) => {
    mocks.controllerOptions = options;
    return { start: vi.fn(), stop: mocks.stop };
  },
}));

describe('resume editor realtime session expiry', () => {
  beforeEach(() => {
    const accepted = acceptedFixture();
    const record = {
      accepted,
      current: {
        document: structuredClone(accepted.document),
        metadata: structuredClone(accepted.metadata),
      },
      pending: [{ id: 'pending-1' }],
      attempt: null,
      conflicts: [],
      issues: {},
      templateState: null,
      photoRead: { kind: 'none' },
      completeReadRequired: false,
      sessionLost: false,
      opaquePhotoOutcome: null,
    };
    mocks.auth = {
      authState: ref('authenticated'),
      user: computed(() => ({ id: 'owner-1' })),
    };
    mocks.api = {
      read: vi.fn().mockResolvedValue({ kind: 'complete', accepted }),
    };
    mocks.actions = {
      record: computed(() => record),
      refresh: vi.fn(async () => {
        record.sessionLost = true;
        return { kind: 'session-lost' };
      }),
      refreshConditional: vi.fn(async () => {
        record.sessionLost = true;
        return { kind: 'session-lost' };
      }),
    };
    mocks.controllerOptions = undefined;
    mocks.stop.mockReset();
  });

  it.each(['unconditional', 'conditional'] as const)(
    'stops realtime after a %s refresh loses the session',
    async (mode) => {
      const wrapper = await mountSuspended(ResumeEditorPage, {
        global: {
          stubs: {
            EditorShell: { template: '<div />' },
            EmptyState: { template: '<div />' },
            LoadingState: { template: '<div />' },
          },
        },
      });
      await nextTick();
      const options = mocks.controllerOptions as {
        refetch: (value: typeof mode) => Promise<unknown>;
      };

      await expect(options.refetch(mode)).resolves.toEqual({ kind: 'failed' });

      expect(mocks.stop).toHaveBeenCalledOnce();
      expect(
        (mocks.actions as { record: { value: { pending: unknown[] } } })
          .record.value.pending,
      ).toEqual([{ id: 'pending-1' }]);
      wrapper.unmount();
    },
  );

  it('stops realtime when authentication enters an error state', async () => {
    const wrapper = await mountSuspended(ResumeEditorPage, {
      global: { stubs: { EditorShell: { template: '<div />' } } },
    });
    await nextTick();

    (mocks.auth as { authState: { value: string } }).authState.value = 'error';
    await nextTick();

    expect(mocks.stop).toHaveBeenCalledOnce();
    wrapper.unmount();
  });
});
