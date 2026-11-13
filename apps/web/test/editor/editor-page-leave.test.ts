import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { computed, ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import type { ResumeEditorActions } from '../../app/composables/useResumeEditor';
import ResumeEditorPage from '../../app/pages/app/resumes/[id].vue';
import { useResumeStore } from '../../app/stores/resumes';
import {
  fakeServer,
  fullName,
  headline,
  startingResume,
  type FakeResumeServer,
} from './fake-resume-server';

const mocks = vi.hoisted(() => ({
  server: undefined as unknown,
  auth: undefined as unknown,
  editors: [] as unknown[],
}));

mockNuxtImport('useAuth', () => () => mocks.auth);
mockNuxtImport('useRoute', () => () => ({
  params: { id: 'resume-1' },
  query: {},
}));
mockNuxtImport('navigateTo', () => vi.fn());
mockNuxtImport('useCapabilities', () => () => ({
  providerLogin: computed(() => false),
}));

vi.mock('../../app/editor/resumeApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/editor/resumeApi')>()),
  createResumeApi: () => (mocks.server as FakeResumeServer).api,
}));
vi.mock('../../app/composables/useResumeEditor', async (importOriginal) => {
  const real = await importOriginal<
    typeof import('../../app/composables/useResumeEditor')
  >();
  return {
    ...real,
    useResumeEditor: (...args: Parameters<typeof real.useResumeEditor>) => {
      const actions = real.useResumeEditor(...args);
      mocks.editors.push(actions);
      return actions;
    },
  };
});
vi.mock('../../app/editor/photoController', () => ({
  createPhotoController: () => ({ sync: vi.fn(), clear: vi.fn() }),
}));
vi.mock('../../app/realtime/controller', () => ({
  createRealtimeController: () => ({ start: vi.fn(), stop: vi.fn() }),
}));

const ID = 'resume-1';

async function openPage(server: FakeResumeServer) {
  const reads = server.reads;
  const wrapper = await mountSuspended(ResumeEditorPage, {
    global: {
      stubs: {
        EditorShell: { template: '<div />' },
        EmptyState: { template: '<div />' },
        LoadingState: { template: '<div />' },
      },
    },
  });
  await vi.waitFor(() => {
    expect(server.reads).toBe(reads + 1);
    expect(useResumeStore().recordFor(ID)).toMatchObject({
      pending: [],
      completeReadRequired: false,
    });
  });
  return {
    wrapper,
    actions: mocks.editors.at(-1) as ResumeEditorActions,
  };
}

function start(): FakeResumeServer {
  const server = fakeServer(startingResume());
  mocks.server = server;
  mocks.auth = {
    authState: ref('authenticated'),
    user: computed(() => ({ id: 'owner-1', hasPassword: true })),
    csrfToken: computed(() => 'csrf-1'),
    identities: computed(() => []),
    refresh: vi.fn(async () => {}),
    retryAfterMs: computed(() => null),
    mutate: vi.fn(),
  };
  return server;
}

function leaveWarns(): boolean {
  const event = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

function sameHeadlineElsewhere(server: FakeResumeServer): void {
  server.otherWriter((document) => ({
    ...document,
    personalDetails: {
      ...document.personalDetails,
      headline: 'Staff engineer',
    },
  }));
}

afterEach(() => {
  vi.useRealTimers();
  mocks.editors.length = 0;
  useResumeStore().removeResume(ID);
});

describe('leaving the editor with unsent work', () => {
  it('warns, then discards text queued behind the read barrier and stops '
    + 'its retries; reopening starts from a fresh read', async () => {
    const server = start();
    const first = await openPage(server);
    vi.useFakeTimers();
    sameHeadlineElsewhere(server);
    server.failNextRead({ kind: 'failed', reason: 'network' });
    server.failNextRead({ kind: 'failed', reason: 'network' });
    first.actions.edit(headline('Staff engineer'));
    await vi.advanceTimersByTimeAsync(1_000);
    first.actions.edit(fullName('Ada King'));
    await vi.advanceTimersByTimeAsync(1_000);
    const store = useResumeStore();
    expect(store.recordFor(ID)).toMatchObject({
      completeReadRequired: true,
      pending: [{ kind: 'personalField', path: 'fullName' }],
    });
    expect(leaveWarns()).toBe(true);
    const reads = server.reads;

    first.wrapper.unmount();
    await vi.advanceTimersByTimeAsync(120_000);

    expect(server.reads).toBe(reads);
    expect(server.sent).toHaveLength(1);
    vi.useRealTimers();
    const second = await openPage(server);
    expect(server.reads).toBe(reads + 1);
    expect(store.recordFor(ID)).toMatchObject({
      pending: [],
      attempt: null,
      completeReadRequired: false,
      accepted: { revision: server.stored.revision },
    });
    expect(store.recordFor(ID)!.current.document.personalDetails)
      .toMatchObject({ fullName: 'Ada Lovelace', headline: 'Staff engineer' });
    expect(server.sent).toHaveLength(1);
    second.wrapper.unmount();
  });

  it('warns, then sends nothing it had not sent before the page closed',
    async () => {
      const server = start();
      const first = await openPage(server);
      vi.useFakeTimers();
      first.actions.edit(headline('Staff engineer'));
      expect(leaveWarns()).toBe(true);

      first.wrapper.unmount();
      await vi.advanceTimersByTimeAsync(120_000);

      expect(server.sent).toEqual([]);
      expect(server.reads).toBe(1);
      vi.useRealTimers();
      const second = await openPage(server);
      expect(server.reads).toBe(2);
      second.wrapper.unmount();
    });
});

describe('discarding work after the session is lost', () => {
  it('lets the page go while the read barrier stays for the queue',
    async () => {
      const server = start();
      const page = await openPage(server);
      vi.useFakeTimers();
      sameHeadlineElsewhere(server);
      server.failNextRead({ kind: 'session-lost' });
      page.actions.edit(headline('Staff engineer'));
      page.actions.edit(fullName('Ada King'));
      await vi.advanceTimersByTimeAsync(1_000);
      const store = useResumeStore();
      expect(store.recordFor(ID)).toMatchObject({
        sessionLost: true,
        completeReadRequired: true,
        pending: [{ path: 'fullName' }],
      });
      expect(leaveWarns()).toBe(true);

      page.actions.discard();

      expect(store.recordFor(ID)).toMatchObject({
        pending: [],
        sessionLost: false,
        completeReadRequired: true,
      });
      expect(leaveWarns()).toBe(false);
      page.wrapper.unmount();
    });
});
