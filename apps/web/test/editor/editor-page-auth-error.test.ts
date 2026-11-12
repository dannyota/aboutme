import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { computed, ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import type { ResumeEditorActions } from '../../app/composables/useResumeEditor';
import ResumeEditorPage from '../../app/pages/app/resumes/[id].vue';
import { useResumeStore } from '../../app/stores/resumes';
import {
  fakeServer,
  headline,
  startingResume,
  type FakeResumeServer,
} from './fake-resume-server';

// The editor page over the real editor, coordinator and store, with only the
// API and the session check faked: the check fails with something other than
// a 401 while an edit is queued. The editor stays open with the edit, checks
// again on the retry schedule, and saves the edit once the check recovers.

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
    useResumeEditor: (id: string) => {
      const actions = real.useResumeEditor(id);
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

type AuthState = 'authenticated' | 'anonymous' | 'error' | 'loading';

/**
 * A session whose checks answer from `answers` in turn, keeping the last
 * state once they run out, and record when each ran.
 */
function session() {
  const state = ref<AuthState>('authenticated');
  const answers: AuthState[] = [];
  const checkedAt: number[] = [];
  mocks.auth = {
    authState: state,
    user: computed(() => ({ id: 'owner-1', hasPassword: true })),
    csrfToken: computed(() => 'csrf-1'),
    identities: computed(() => []),
    refresh: vi.fn(async () => {
      checkedAt.push(Date.now());
      state.value = answers.shift() ?? state.value;
    }),
    mutate: vi.fn(),
  };
  return { state, answers, checkedAt };
}

// The shell's banner calls `sessionCheck.retry`, as this stand-in does.
const shellStub = {
  props: ['sessionCheck'],
  template: '<div data-editor-shell><button v-if="sessionCheck?.failing.value"'
    + ' data-action="retry-session-check" @click="sessionCheck.retry()" />'
    + '</div>',
};

async function openPage() {
  return mountSuspended(ResumeEditorPage, {
    global: {
      stubs: {
        EditorShell: shellStub,
        EmptyState: {
          template: '<div data-empty-state><slot name="action" /></div>',
        },
        LoadingState: { template: '<div />' },
      },
    },
  });
}

/**
 * Opens the editor page, queues a headline edit, then fails the session
 * check before the edit is sent.
 */
async function queuedThenAuthError() {
  const server = fakeServer(startingResume());
  mocks.server = server;
  const auth = session();
  const wrapper = await openPage();
  await vi.waitFor(() => {
    expect(useResumeStore().recordFor(ID)).toBeDefined();
  });
  vi.useFakeTimers();
  vi.setSystemTime(0);
  const actions = mocks.editors.at(-1) as ResumeEditorActions;
  expect(actions.edit(headline('Staff engineer')).kind).toBe('enqueued');
  auth.state.value = 'error';
  await vi.advanceTimersByTimeAsync(5_000);
  expect(server.sent).toEqual([]);
  expect(useResumeStore().recordFor(ID)!.pending).toMatchObject([
    { path: 'headline' },
  ]);
  expect(wrapper.find('[data-editor-shell]').exists()).toBe(true);
  return { auth, server, wrapper };
}

/**
 * Opens the editor page and sends a headline edit whose response and quick
 * replay are lost, then fails the session check while the write is held.
 */
async function heldThenAuthError() {
  const server = fakeServer(startingResume());
  mocks.server = server;
  const auth = session();
  const wrapper = await openPage();
  await vi.waitFor(() => {
    expect(useResumeStore().recordFor(ID)).toBeDefined();
  });
  vi.useFakeTimers();
  vi.setSystemTime(0);
  const actions = mocks.editors.at(-1) as ResumeEditorActions;
  server.loseEveryResponse({ applied: false });
  expect(actions.edit(headline('Staff engineer')).kind).toBe('enqueued');
  await vi.advanceTimersByTimeAsync(1_250);
  expect(server.sent.map(({ result }) => result)).toEqual([
    'unknown', 'unknown',
  ]);
  auth.state.value = 'error';
  await vi.advanceTimersByTimeAsync(0);
  return { auth, server, wrapper };
}

afterEach(() => {
  vi.useRealTimers();
  mocks.editors.length = 0;
  useResumeStore().removeResume(ID);
});

describe('a failed session check with an edit queued', () => {
  it('keeps the editor and the edit through Try again and saves it',
    async () => {
      const { auth, server, wrapper } = await queuedThenAuthError();
      auth.answers.push('authenticated');

      await wrapper.get('[data-action="retry-session-check"]')
        .trigger('click');
      await vi.advanceTimersByTimeAsync(5_000);

      // The text the person typed is still in the editor after Try again.
      expect(useResumeStore().recordFor(ID)!.current.document.personalDetails
        .headline).toBe('Staff engineer');
      expect(server.stored.document.personalDetails.headline)
        .toBe('Staff engineer');
      expect(wrapper.find('[data-action="retry-session-check"]').exists())
        .toBe(false);
      wrapper.unmount();
    });

  it('keeps the editor and saves the edit once the check recovers on its '
    + 'own', async () => {
    const { auth, server, wrapper } = await queuedThenAuthError();
    auth.answers.push('authenticated');

    await vi.advanceTimersByTimeAsync(10_000);

    expect(wrapper.find('[data-editor-shell]').exists()).toBe(true);
    expect(server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    wrapper.unmount();
  });

  it('checks again on the retry schedule without a busy loop', async () => {
    const { auth, wrapper } = await queuedThenAuthError();

    await vi.advanceTimersByTimeAsync(116_000);

    const gaps = auth.checkedAt.slice(1)
      .map((time, index) => time - auth.checkedAt[index]!);
    expect(auth.checkedAt[0]).toBe(1_000);
    expect(gaps).toEqual([2_000, 4_000, 8_000, 16_000, 30_000, 30_000, 30_000]);
    wrapper.unmount();
  });

  it('checks only on its own schedule while a held write waits, and sends '
    + 'the write once the check recovers', async () => {
    const { auth, server, wrapper } = await heldThenAuthError();

    await vi.advanceTimersByTimeAsync(120_000);

    // No extra check from the held write's own retries in between.
    expect(auth.checkedAt).toEqual([
      2_250, 4_250, 8_250, 16_250, 32_250, 62_250, 92_250,
    ]);
    expect(server.sent).toHaveLength(2);

    server.loseEveryResponse(undefined);
    auth.answers.push('authenticated');
    await wrapper.get('[data-action="retry-session-check"]').trigger('click');
    await vi.advanceTimersByTimeAsync(5_000);

    expect(server.sent.map(({ result }) => result).at(-1)).toBe('complete');
    expect(new Set(server.sent.map(({ attempt }) => attempt.idempotencyKey)))
      .toHaveProperty('size', 1);
    expect(server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    wrapper.unmount();
  });

  it('stops checking when the editor closes or is opened again', async () => {
    const { auth, wrapper } = await queuedThenAuthError();
    const checks = auth.checkedAt.length;

    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(auth.checkedAt).toHaveLength(checks);

    vi.useRealTimers();
    auth.state.value = 'authenticated';
    const reopened = await openPage();
    await vi.waitFor(() => {
      expect(reopened.find('[data-editor-shell]').exists()).toBe(true);
    });
    expect(reopened.find('[data-action="retry-session-check"]').exists())
      .toBe(false);
    expect(auth.checkedAt).toHaveLength(checks);
    reopened.unmount();
  });

  it('stops checking and asks to sign in when the session is gone',
    async () => {
      const { auth, wrapper } = await queuedThenAuthError();
      auth.answers.push('anonymous');

      await vi.advanceTimersByTimeAsync(120_000);

      expect(useResumeStore().recordFor(ID)).toMatchObject({
        sessionLost: true,
        pending: [{ path: 'headline' }],
      });
      expect(auth.checkedAt).toHaveLength(3);
      wrapper.unmount();
    });
});

describe('a failed session check before the editor opened', () => {
  it('shows Editor unavailable instead of the editor', async () => {
    mocks.server = fakeServer(startingResume());
    const auth = session();
    auth.state.value = 'error';

    const wrapper = await openPage();

    expect(wrapper.find('[data-empty-state]').exists()).toBe(true);
    expect(wrapper.find('[data-editor-shell]').exists()).toBe(false);
    wrapper.unmount();
  });
});
