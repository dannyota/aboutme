import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { setResponseStatus } from 'h3';
import { computed, defineComponent, h, nextTick } from 'vue';
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

const mocks = vi.hoisted(() => ({
  server: undefined as unknown,
  editors: [] as unknown[],
  realtimeStarts: 0,
  pages: [] as { unmount: () => void }[],
}));

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
  createRealtimeController: () => ({
    start: () => {
      mocks.realtimeStarts += 1;
    },
    stop: vi.fn(),
  }),
}));

const ID = 'resume-1';

type MeAnswer
  = | { readonly kind: 'user'; readonly id: string }
    | { readonly kind: 'error' }
    | { readonly kind: 'anonymous' };

const me = {
  answer: { kind: 'user', id: 'owner-1' } as MeAnswer,
  calls: [] as number[],
  held: undefined as Promise<void> | undefined,
};

registerEndpoint('/api/v1/me', {
  method: 'GET',
  handler: async (event) => {
    me.calls.push(Date.now());
    if (me.held !== undefined) await me.held;
    if (me.answer.kind === 'error') {
      setResponseStatus(event, 503);
      return { error: { code: 'unavailable', message: 'Try again.' } };
    }
    if (me.answer.kind === 'anonymous') {
      setResponseStatus(event, 401);
      return { error: { code: 'session_required', message: 'Sign in.' } };
    }
    return {
      data: {
        user: {
          id: me.answer.id,
          email: 'owner@example.com',
          name: null,
          avatarKey: null,
          hasPassword: true,
        },
        csrfToken: 'csrf-1',
        identities: [],
      },
    };
  },
});

const Session = defineComponent({
  setup: () => ({ auth: useAuth() }),
  render: () => h('div'),
});

const shellStub = {
  props: ['actions', 'record', 'sessionCheck'],
  template: '<div data-editor-shell>'
    + '<button v-if="sessionCheck?.failing.value"'
    + ' data-action="retry-session-check"'
    + ' :disabled="sessionCheck.busy?.value"'
    + ' @click="sessionCheck.retry()" />'
    + '<div v-if="record.sessionLost" data-session-lost>'
    + '<button data-action="resume-after-auth"'
    + ' @click="actions.resumeAfterAuth()" /></div>'
    + '</div>',
};

const FAKE_TIMERS: Parameters<typeof vi.useFakeTimers>[0] = {
  toFake: [
    'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date',
  ],
};

async function letNetworkAnswer(): Promise<void> {
  for (let turn = 0; turn < 10; turn += 1) {
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
  await nextTick();
}

async function run(ms: number): Promise<void> {
  await letNetworkAnswer();
  for (let passed = 0; passed < ms; passed += 50) {
    await vi.advanceTimersByTimeAsync(Math.min(50, ms - passed));
    await letNetworkAnswer();
  }
}

async function sessionAnswers(answer: MeAnswer) {
  me.answer = answer;
  const session = await mountSuspended(Session);
  const { auth } = session.vm as unknown as {
    auth: ReturnType<typeof useAuth>;
  };
  await auth.refresh();
  return auth;
}

async function mountPage() {
  const page = await mountSuspended(ResumeEditorPage, {
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
  mocks.pages.push(page);
  return page;
}

async function openEditor() {
  const server = fakeServer(startingResume());
  mocks.server = server;
  const auth = await sessionAnswers({ kind: 'user', id: 'owner-1' });
  const wrapper = await mountPage();
  await vi.waitFor(() => {
    expect(useResumeStore().recordFor(ID)).toBeDefined();
    expect(wrapper.find('[data-editor-shell]').exists()).toBe(true);
    expect(mocks.realtimeStarts).toBeGreaterThan(0);
  });
  vi.useFakeTimers(FAKE_TIMERS);
  vi.setSystemTime(0);
  const actions = mocks.editors.at(-1) as ResumeEditorActions;
  const settle = (ms = 0): Promise<void> => run(ms);
  return {
    actions,
    auth,
    server,
    settle,
    wrapper,
    record: () => useResumeStore().recordFor(ID)!,
    outage: async (): Promise<void> => {
      me.answer = { kind: 'error' };
      await auth.refresh();
      await settle();
      expect(auth.authState.value).toBe('error');
    },
  };
}

afterEach(() => {
  for (const page of mocks.pages.splice(0)) {
    try {
      page.unmount();
    } catch {
      // Already unmounted by the test.
    }
  }
  vi.useRealTimers();
  mocks.editors.length = 0;
  me.calls.length = 0;
  me.held = undefined;
  me.answer = { kind: 'user', id: 'owner-1' };
  useResumeStore().removeResume(ID);
});

describe('a failed session check while the editor is open', () => {
  it('queues text typed during the outage and saves it after recovery',
    async () => {
      const context = await openEditor();
      await context.outage();
      expect(context.auth.user.value).toBeNull();

      expect(context.actions.edit(headline('Staff engineer')).kind)
        .toBe('enqueued');
      me.answer = { kind: 'user', id: 'owner-1' };
      await context.settle(5_000);

      expect(context.server.stored.document.personalDetails.headline)
        .toBe('Staff engineer');
      expect(context.wrapper.find('[data-action="retry-session-check"]')
        .exists()).toBe(false);
      context.wrapper.unmount();
    });

  it('keeps the edit through Try again and saves it', async () => {
    const context = await openEditor();
    expect(context.actions.edit(headline('Staff engineer')).kind)
      .toBe('enqueued');
    await context.outage();
    await context.settle(5_000);
    expect(context.server.sent).toEqual([]);
    me.answer = { kind: 'user', id: 'owner-1' };

    await context.wrapper.get('[data-action="retry-session-check"]')
      .trigger('click');
    await context.settle(5_000);

    expect(context.record().current.document.personalDetails.headline)
      .toBe('Staff engineer');
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    context.wrapper.unmount();
  });

  it('retries the read recovery needs when it fails, then sends the edit',
    async () => {
      const context = await openEditor();
      expect(context.actions.edit(headline('Staff engineer')).kind)
        .toBe('enqueued');
      await context.outage();
      context.server.failNextRead({ kind: 'failed', reason: 'network' });
      me.answer = { kind: 'user', id: 'owner-1' };

      await context.settle(10_000);

      expect(context.server.stored.document.personalDetails.headline)
        .toBe('Staff engineer');
      context.wrapper.unmount();
    });

  it('checks again on the retry schedule without a busy loop', async () => {
    const context = await openEditor();
    await context.outage();
    const outageAt = me.calls.length;

    await context.settle(120_000);

    expect(me.calls.slice(outageAt)).toEqual([
      1_000, 3_000, 7_000, 15_000, 31_000, 61_000, 91_000,
    ]);
    context.wrapper.unmount();
  });

  it('checks only on its own schedule while a held write waits, and sends '
    + 'the write once the check recovers', async () => {
    const context = await openEditor();
    context.server.loseEveryResponse({ applied: false });
    expect(context.actions.edit(headline('Staff engineer')).kind)
      .toBe('enqueued');
    await context.settle(1_250);
    expect(context.server.sent.map(({ result }) => result)).toEqual([
      'unknown', 'unknown',
    ]);
    await context.outage();
    const outageAt = me.calls.length;

    await context.settle(120_000);

    expect(me.calls.slice(outageAt)).toEqual([
      2_250, 4_250, 8_250, 16_250, 32_250, 62_250, 92_250,
    ]);
    expect(context.server.sent).toHaveLength(2);

    context.server.loseEveryResponse(undefined);
    me.answer = { kind: 'user', id: 'owner-1' };
    await context.wrapper.get('[data-action="retry-session-check"]')
      .trigger('click');
    await context.settle(5_000);

    expect(context.server.sent.map(({ result }) => result).at(-1))
      .toBe('complete');
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    context.wrapper.unmount();
  });

  it('stops checking when the editor closes or is opened again', async () => {
    const context = await openEditor();
    await context.outage();
    const checks = me.calls.length;

    context.wrapper.unmount();
    await context.settle(120_000);
    expect(me.calls).toHaveLength(checks);

    vi.useRealTimers();
    me.answer = { kind: 'user', id: 'owner-1' };
    const reopened = await mountPage();
    await vi.waitFor(() => {
      expect(reopened.find('[data-editor-shell]').exists()).toBe(true);
    });
    expect(reopened.find('[data-action="retry-session-check"]').exists())
      .toBe(false);
    reopened.unmount();
  });

  it('asks to sign in and stops checking when the session is gone',
    async () => {
      const context = await openEditor();
      expect(context.actions.edit(headline('Staff engineer')).kind)
        .toBe('enqueued');
      await context.outage();
      me.answer = { kind: 'anonymous' };

      await context.settle(120_000);

      expect(context.record()).toMatchObject({
        sessionLost: true,
        pending: [{ path: 'headline' }],
      });
      expect(context.wrapper.find('[data-session-lost]').exists()).toBe(true);
      expect(context.server.sent).toEqual([]);
      context.wrapper.unmount();
    });
});

describe('a session that comes back as someone else', () => {
  it('sends none of the work, asks to sign in, and keeps the work until '
    + 'its owner signs back in', async () => {
    const context = await openEditor();
    expect(context.actions.edit(headline('Staff engineer')).kind)
      .toBe('enqueued');
    await context.outage();
    const starts = mocks.realtimeStarts;
    me.answer = { kind: 'user', id: 'owner-2' };

    await context.settle(5_000);

    expect(context.server.sent).toEqual([]);
    expect(context.record()).toMatchObject({
      sessionLost: true,
      pending: [{ path: 'headline' }],
    });
    expect(context.wrapper.find('[data-session-lost]').exists()).toBe(true);
    expect(mocks.realtimeStarts).toBe(starts);

    me.answer = { kind: 'user', id: 'owner-1' };
    await context.wrapper.get('[data-action="resume-after-auth"]')
      .trigger('click');
    await context.settle(5_000);

    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    expect(context.record().sessionLost).toBe(false);
    context.wrapper.unmount();
  });
});

describe('an ordinary session check', () => {
  it('lets a retry that came due while it was on its way go on', async () => {
    const context = await openEditor();
    context.server.loseNextResponse({ applied: false });
    context.server.loseNextResponse({ applied: false });
    expect(context.actions.edit(headline('Staff engineer')).kind)
      .toBe('enqueued');
    await context.settle(1_250);
    let answer = (): void => {};
    me.held = new Promise<void>((resolve) => {
      answer = resolve;
    });
    const checking = context.auth.refresh();
    await context.settle();
    expect(context.auth.authState.value).toBe('loading');

    await context.settle(1_000);
    expect(context.server.sent).toHaveLength(2);
    me.held = undefined;
    answer();
    await checking;
    await context.settle();

    expect(context.server.sent.map(({ result }) => result).at(-1))
      .toBe('complete');
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    context.wrapper.unmount();
  });
});

describe('an older page whose first read arrives late', () => {
  it('leaves the record the newer page is editing alone', async () => {
    const server = fakeServer(startingResume());
    mocks.server = server;
    await sessionAnswers({ kind: 'user', id: 'owner-1' });
    let arrive = (): void => {};
    const read = server.api.read.getMockImplementation()!;
    server.api.read.mockImplementationOnce(() => new Promise((resolve) => {
      arrive = () => void read().then(resolve);
    }));
    const first = await mountPage();
    await vi.waitFor(() => expect(server.api.read).toHaveBeenCalledOnce());
    first.unmount();

    const second = await mountPage();
    await vi.waitFor(() => {
      expect(second.find('[data-editor-shell]').exists()).toBe(true);
    });
    vi.useFakeTimers(FAKE_TIMERS);
    const actions = mocks.editors.at(-1) as ResumeEditorActions;
    expect(actions.edit(headline('Staff engineer')).kind).toBe('enqueued');
    arrive();
    await run(5_000);

    expect(server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    expect(useResumeStore().recordFor(ID)!.current.document.personalDetails
      .headline).toBe('Staff engineer');
    second.unmount();
  });
});

describe('a session answer while the first read is out', () => {
  async function openWithReadOut() {
    const server = fakeServer(startingResume());
    mocks.server = server;
    const auth = await sessionAnswers({ kind: 'user', id: 'owner-1' });
    let arrive = (): void => {};
    const read = server.api.read.getMockImplementation()!;
    server.api.read.mockImplementationOnce(() => new Promise((resolve) => {
      arrive = () => void read().then(resolve);
    }));
    mocks.realtimeStarts = 0;
    const wrapper = await mountPage();
    await vi.waitFor(() => expect(server.api.read).toHaveBeenCalledOnce());
    vi.useFakeTimers(FAKE_TIMERS);
    vi.setSystemTime(0);
    return { arrive: () => arrive(), auth, server, wrapper };
  }

  it('opens the editor with the banner and a scheduled check when a check '
    + 'fails meanwhile', async () => {
    const context = await openWithReadOut();
    me.answer = { kind: 'error' };
    await context.auth.refresh();
    await run(0);
    expect(context.wrapper.find('[data-editor-shell]').exists()).toBe(false);

    context.arrive();
    await run(0);

    expect(context.wrapper.find('[data-editor-shell]').exists()).toBe(true);
    expect(context.wrapper.find('[data-action="retry-session-check"]')
      .exists()).toBe(true);
    expect(mocks.realtimeStarts).toBe(0);
    const checks = me.calls.length;
    await run(1_000);
    expect(me.calls).toHaveLength(checks + 1);

    me.answer = { kind: 'user', id: 'owner-1' };
    const actions = mocks.editors.at(-1) as ResumeEditorActions;
    expect(actions.edit(headline('Staff engineer')).kind).toBe('enqueued');
    await run(5_000);
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
    expect(mocks.realtimeStarts).toBe(1);
  });

  it('starts realtime once when a failed check recovers meanwhile',
    async () => {
      const context = await openWithReadOut();
      me.answer = { kind: 'error' };
      await context.auth.refresh();
      me.answer = { kind: 'user', id: 'owner-1' };
      await context.auth.refresh();
      await run(0);

      context.arrive();
      await run(5_000);

      expect(context.wrapper.find('[data-editor-shell]').exists()).toBe(true);
      expect(context.wrapper.find('[data-action="retry-session-check"]')
        .exists()).toBe(false);
      expect(mocks.realtimeStarts).toBe(1);
    });
});

describe('a failed session check before the editor opened', () => {
  it('shows Editor unavailable, and Try again opens the editor once the '
    + 'session answers', async () => {
    mocks.server = fakeServer(startingResume());
    await sessionAnswers({ kind: 'error' });

    const wrapper = await mountPage();
    await vi.waitFor(() => {
      expect(wrapper.find('[data-empty-state]').exists()).toBe(true);
    });
    expect(wrapper.find('[data-editor-shell]').exists()).toBe(false);

    me.answer = { kind: 'user', id: 'owner-1' };
    await wrapper.get('[data-empty-state] button').trigger('click');
    await vi.waitFor(() => {
      expect(wrapper.find('[data-editor-shell]').exists()).toBe(true);
    });
    const actions = mocks.editors.at(-1) as ResumeEditorActions;
    expect(actions.edit(headline('Staff engineer')).kind).toBe('enqueued');
    await vi.waitFor(() => {
      expect((mocks.server as FakeResumeServer).stored.document
        .personalDetails.headline).toBe('Staff engineer');
    }, { timeout: 3_000 });
    wrapper.unmount();
  });
});
