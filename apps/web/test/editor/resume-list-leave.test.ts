import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import {
  computed,
  defineComponent,
  effectScope,
  h,
  nextTick,
  ref,
} from 'vue';
import { RouterView } from 'vue-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import ResumeList from '../../app/components/editor/list/ResumeList.vue';
import {
  createResumeEditorActions,
  type ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import {
  useResumeList,
  type ResumeListController,
} from '../../app/composables/useResumeList';
import ResumeEditorPage from '../../app/pages/app/resumes/[id].vue';
import ResumeListPage from '../../app/pages/app/resumes/index.vue';
import { useResumeStore } from '../../app/stores/resumes';
import { setSiteLocale } from '../support/locale';
import {
  fakeServer,
  headline,
  startingResume,
  type FakeResumeServer,
} from './fake-resume-server';

const mocks = vi.hoisted(() => ({
  auth: undefined as unknown,
  server: undefined as unknown,
  editors: [] as unknown[],
  mounted: [] as { unmount: () => void }[],
}));

mockNuxtImport('useAuth', () => () => mocks.auth);
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
const ROW = `[data-testid="resume-row-${ID}"]`;

function signedIn(): void {
  mocks.auth = {
    authState: ref('authenticated'),
    user: computed(() => ({ id: 'owner-1', hasPassword: true })),
    csrfToken: computed(() => 'csrf-1'),
    identities: computed(() => []),
    retryAfterMs: computed(() => null),
    refresh: vi.fn(async () => {}),
    mutate: vi.fn(),
  };
}

const RouterHost = defineComponent({ render: () => h(RouterView) });

function leaveWarns(): boolean {
  const event = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

interface ListOptions {
  readonly router?: boolean;
  readonly timers?: boolean;
}

async function openList({ router = false, timers = true }: ListOptions = {}) {
  const server = fakeServer(startingResume());
  mocks.server = server;
  signedIn();
  const wrapper = await mountSuspended(router ? RouterHost : ResumeListPage, {
    ...(router ? { route: '/app/resumes' } : {}),
    global: {
      stubs: {
        EditorListRenameResumeDialog: {
          props: ['item', 'busy'],
          emits: ['submit', 'close'],
          template: '<button v-if="item" data-submit-rename'
            + ' @click="$emit(\'submit\', item.id, \'Ada resume\')" />',
        },
        EditorListDeleteResumeDialog: {
          props: ['item', 'busy'],
          emits: ['submit', 'close'],
          template: '<button v-if="item" data-submit-delete'
            + ' @click="$emit(\'submit\', item.id, item.title)" />',
        },
      },
    },
  });
  mocks.mounted.push(wrapper);
  await vi.waitFor(() => {
    expect(wrapper.find(ROW).exists()).toBe(true);
  });
  if (timers) {
    vi.useFakeTimers();
    vi.setSystemTime(0);
  }
  const list = wrapper.findComponent(ResumeList);
  const item = () => list.props('items').find(({ id }) => id === ID)!;
  const settle = async (ms = 0): Promise<void> => {
    await vi.advanceTimersByTimeAsync(ms);
    await nextTick();
  };
  return {
    server,
    settle,
    wrapper,
    card: () => wrapper.find(ROW),
    rename: async (): Promise<void> => {
      list.vm.$emit('rename', item());
      await nextTick();
      await wrapper.get('[data-submit-rename]').trigger('click');
    },
    remove: async (): Promise<void> => {
      list.vm.$emit('remove', item());
      await nextTick();
      await wrapper.get('[data-submit-delete]').trigger('click');
    },
  };
}

async function openEditorPage() {
  const page = await mountSuspended(ResumeEditorPage, {
    route: `/app/resumes/${ID}`,
    global: {
      stubs: {
        EditorShell: { template: '<div data-editor-shell />' },
        EmptyState: { template: '<div />' },
        LoadingState: { template: '<div />' },
      },
    },
  });
  mocks.mounted.push(page);
  await vi.waitFor(() => {
    expect(page.find('[data-editor-shell]').exists()).toBe(true);
  });
  return { page, actions: mocks.editors.at(-1) as ResumeEditorActions };
}

afterEach(() => {
  for (const page of mocks.mounted.splice(0)) {
    try {
      page.unmount();
    } catch {
      // Already unmounted by the test.
    }
  }
  vi.useRealTimers();
  mocks.editors.length = 0;
  useResumeStore().removeResume(ID);
  setSiteLocale('en');
});

describe('the resume list page', () => {
  it('reads and writes nothing after it unmounts', async () => {
    const server = fakeServer(startingResume());
    signedIn();
    let list: ResumeListController | undefined;
    const Page = defineComponent({
      setup() {
        list = useResumeList({
          api: server.api as never,
          authState: computed(() => 'authenticated') as never,
          ownerId: ref('owner-1'),
          runtime: {
            nowEpochMs: () => Date.now(),
            uuid: () => crypto.randomUUID(),
            delay: (ms) =>
              new Promise<void>((resolve) => setTimeout(resolve, ms)),
          },
        });
        return () => h('div');
      },
    });
    const wrapper = await mountSuspended(Page);
    vi.useFakeTimers();
    server.loseNextResponse({ applied: false });

    void list!.rename(ID, 'Ada resume');
    await vi.advanceTimersByTimeAsync(0);
    expect(server.sent.map(({ result }) => result)).toEqual(['unknown']);
    const reads = server.reads;

    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(120_000);

    expect(server.sent.map(({ result }) => result)).toEqual(['unknown']);
    expect(server.reads).toBe(reads);
  });

  it.each([
    ['en', 'Saving…'],
    ['vi', 'Đang lưu…'],
  ] as const)(
    'shows %s Saving on the card and warns before leaving while a rename '
    + 'is on its way',
    async (locale, saving) => {
      setSiteLocale(locale);
      const context = await openList();
      const release = context.server.holdNextResponse();

      await context.rename();
      await context.settle();

      expect(context.card().get('[data-state-mark="saving"]').text())
        .toBe(saving);
      expect(context.card().text()).toContain('Ada resume');
      expect(leaveWarns()).toBe(true);

      release();
      await context.settle();

      expect(context.card().find('[data-state-mark="saving"]').exists())
        .toBe(false);
      expect(context.card().find('[data-state-mark="draft"]').exists())
        .toBe(true);
      expect(context.server.stored.metadata.title).toBe('Ada resume');
      expect(leaveWarns()).toBe(false);
      context.wrapper.unmount();
    },
  );

  it('shows Saving on the card while a delete is on its way, then drops '
    + 'the card', async () => {
    const context = await openList();
    const release = context.server.holdNextResponse();

    await context.remove();
    await context.settle();

    expect(context.card().find('[data-state-mark="saving"]').exists())
      .toBe(true);
    expect(leaveWarns()).toBe(true);

    release();
    await context.settle();

    expect(context.card().exists()).toBe(false);
    expect(leaveWarns()).toBe(false);
    context.wrapper.unmount();
  });

  it.each([
    ['en', 'Name not saved.', 'Try again'],
    ['vi', 'Chưa lưu được tên.', 'Thử lại'],
  ] as const)(
    'holds an unknown rename as %s, retries it on the schedule, and sends '
    + 'it once on two quick clicks of Try again',
    async (locale, held, tryAgain) => {
      setSiteLocale(locale);
      const context = await openList();
      context.server.loseEveryResponse({ applied: false });

      await context.rename();
      await context.settle(7_250);

      expect(context.server.sendGapsFor(
        context.server.sent[0]!.attempt.id,
      )).toEqual([250, 1_000, 2_000, 4_000]);
      const line = context.card().get('[data-rename-held]');
      expect(line.text()).toContain(held);
      expect(context.card().text()).toContain('Ada resume');
      expect(leaveWarns()).toBe(true);
      const sends = context.server.sent.length;

      context.server.loseEveryResponse(undefined);
      const release = context.server.holdNextResponse();
      const button = line.get('[data-action="retry-rename"]');
      expect(button.text()).toBe(tryAgain);
      await button.trigger('click');
      await button.trigger('click');
      await context.settle();

      expect(context.server.sent).toHaveLength(sends + 1);
      expect(context.card().find('[data-state-mark="saving"]').exists())
        .toBe(true);

      release();
      await context.settle();

      expect(context.server.sent).toHaveLength(sends + 1);
      expect(new Set(context.server.sent.map(
        ({ attempt }) => attempt.idempotencyKey,
      )).size).toBe(1);
      expect(context.server.stored.metadata.title).toBe('Ada resume');
      expect(context.card().find('[data-rename-held]').exists()).toBe(false);
      expect(leaveWarns()).toBe(false);
      context.wrapper.unmount();
    },
  );

  it('stops reading and writing for a held rename, and drops it, when the '
    + 'page unmounts', async () => {
    const context = await openList();
    context.server.loseEveryResponse({ applied: false });
    await context.rename();
    await context.settle(3_250);
    const sends = context.server.sent.length;
    const reads = context.server.reads;

    context.wrapper.unmount();
    await context.settle(120_000);

    expect(context.server.sent).toHaveLength(sends);
    expect(context.server.reads).toBe(reads);
    expect(useResumeStore().recordFor(ID)).toMatchObject({
      attempt: null,
      pending: [],
    });
  });
});

describe('a rename that has not reached the queue yet', () => {
  it('warns before leaving, and after the page goes neither reads, queues '
    + 'nor sends', async () => {
    const context = await openList();
    let arrive = (): void => {};
    const read = context.server.api.read.getMockImplementation()!;
    context.server.api.read.mockImplementationOnce(() => new Promise(
      (resolve) => {
        arrive = () => void read().then(resolve);
      },
    ));

    await context.rename();
    await context.settle();
    expect(leaveWarns()).toBe(true);
    context.wrapper.unmount();
    arrive();
    await context.settle(5_000);

    expect(context.server.sent).toEqual([]);
    expect(useResumeStore().recordFor(ID)).toBeUndefined();
  });
});

describe('the list and the editor open on the same resume', () => {
  it('leaves the record to an editor that opened it before the list went',
    async () => {
      const context = await openList({ timers: false });
      const release = context.server.holdNextResponse();
      await context.rename();
      await vi.waitFor(() => expect(context.server.sent).toHaveLength(1));
      const editor = await openEditorPage();
      expect(editor.actions.edit(headline('Staff engineer')).kind)
        .toBe('enqueued');

      context.wrapper.unmount();
      release();

      await vi.waitFor(() => {
        expect(context.server.stored.document.personalDetails.headline)
          .toBe('Staff engineer');
      }, { timeout: 4_000 });
      expect(context.server.stored.metadata.title).toBe('Ada resume');
      expect(useResumeStore().recordFor(ID)).toMatchObject({
        attempt: null,
        pending: [],
      });
    });

  it('drops only its own rename when the editor opens after the list went',
    async () => {
      const context = await openList({ timers: false });
      context.server.loseEveryResponse({ applied: false });
      await context.rename();
      await vi.waitFor(() => expect(context.server.sent).toHaveLength(1));

      context.wrapper.unmount();
      expect(useResumeStore().recordFor(ID)).toMatchObject({
        attempt: null,
        pending: [],
      });
      context.server.loseEveryResponse(undefined);
      const editor = await openEditorPage();
      expect(editor.actions.edit(headline('Staff engineer')).kind)
        .toBe('enqueued');

      await vi.waitFor(() => {
        expect(context.server.stored.document.personalDetails.headline)
          .toBe('Staff engineer');
      }, { timeout: 4_000 });
      expect(context.server.stored.metadata.title).toBe('Fixture');
    });
});

describe('leaving the list while a rename is unsettled', () => {
  async function navigateAway(answer: 'stay' | 'confirm') {
    const navigation = useRouter().push('/terms');
    let button: HTMLElement | null = null;
    await vi.waitFor(() => {
      button = document.body.querySelector<HTMLElement>(
        `[data-action="project-leave-${answer}"]`,
      );
      expect(button).not.toBeNull();
    });
    expect(document.body.querySelector('[role="alertdialog"]')?.textContent)
      .toContain('You have unsaved changes.');
    button!.click();
    await navigation;
  }

  it('stays with everything on Stay', async () => {
    const context = await openList({ router: true, timers: false });
    context.server.loseEveryResponse({ applied: false });
    await context.rename();
    await vi.waitFor(() => {
      expect(context.card().find('[data-rename-held]').exists()).toBe(true);
    });

    await navigateAway('stay');

    expect(useRouter().currentRoute.value.path).toBe('/app/resumes');
    expect(context.card().find('[data-rename-held]').exists()).toBe(true);
    expect(useResumeStore().recordFor(ID)?.attempt).toMatchObject({
      kind: 'unknown',
    });
  });

  it('goes once, dropping the rename, on Leave without saving', async () => {
    const context = await openList({ router: true, timers: false });
    context.server.loseEveryResponse({ applied: false });
    await context.rename();
    await vi.waitFor(() => {
      expect(context.card().find('[data-rename-held]').exists()).toBe(true);
    });

    await navigateAway('confirm');

    expect(useRouter().currentRoute.value.path).toBe('/terms');
    expect(context.wrapper.find(ROW).exists()).toBe(false);
    expect(useResumeStore().recordFor(ID)).toMatchObject({
      attempt: null,
      pending: [],
    });
    const sends = context.server.sent.length;
    await new Promise((resolve) => setTimeout(resolve, 1_500));
    expect(context.server.sent).toHaveLength(sends);
  });
});

describe('the title a card shows', () => {
  it('goes back to the saved title, with the held line, after a rename is '
    + 'refused for good', async () => {
    const context = await openList();
    context.server.failNextWrite({ kind: 'rejected', code: 'request_invalid' });

    await context.rename();
    await context.settle();

    expect(context.card().get('[data-rename-held]').text())
      .toContain('Name not saved.');
    expect(context.card().text()).toContain('Fixture');
    expect(context.card().text()).not.toContain('Ada resume');
  });
});

describe('a rename the server asks to wait for', () => {
  it('is sent by Try again only after its Retry-After', async () => {
    const context = await openList();
    context.server.failNextWrite({ kind: 'rate-limited', retryAfterMs: 5_000 });
    await context.rename();
    await context.settle();
    const sends = context.server.sent.length;

    await context.settle(1_000);
    await context.card().get('[data-action="retry-rename"]').trigger('click');
    await context.settle(3_900);
    expect(context.server.sent).toHaveLength(sends);
    await context.settle(100);

    expect(context.server.sent).toHaveLength(sends + 1);
    expect(context.server.stored.metadata.title).toBe('Ada resume');
    expect(context.card().find('[data-rename-held]').exists()).toBe(false);
  });
});

describe('a delete that landed but whose answer was lost', () => {
  it('does not bring the card back when the list is read again', async () => {
    const context = await openList();
    context.server.loseEveryResponse({ applied: true });
    await context.remove();
    await context.settle(300);
    expect(context.server.sent.map(({ result }) => result)).toEqual([
      'unknown', 'unknown',
    ]);

    const state = (mocks.auth as { authState: { value: string } }).authState;
    state.value = 'loading';
    await context.settle();
    state.value = 'authenticated';
    await context.settle();
    expect(context.server.api.list).toHaveBeenCalledTimes(2);
    expect(context.card().exists()).toBe(false);

    context.server.loseEveryResponse(undefined);
    await context.settle(5_000);
    expect(context.card().exists()).toBe(false);
    expect(useResumeStore().recordFor(ID)).toBeUndefined();
  });
});

describe('a rename or delete whose read finished just as the page went', () => {
  it.each(['rename', 'remove'] as const)(
    'queues and sends nothing for a %s',
    async (action) => {
      const server = fakeServer(startingResume());
      mocks.server = server;
      signedIn();
      const store = useResumeStore();
      const runtime = {
        nowEpochMs: () => Date.now(),
        uuid: () => crypto.randomUUID(),
        delay: (ms: number) =>
          new Promise<void>((resolve) => setTimeout(resolve, ms)),
      };
      const auth = mocks.auth as never;
      const coordinator = createMutationCoordinator({
        api: server.api as never, store, auth, runtime,
      });
      const scope = effectScope();
      const list = scope.run(() => useResumeList({
        api: server.api as never,
        store,
        coordinator,
        runtime,
        authState: computed(() => 'authenticated') as never,
        ownerId: ref('owner-1'),
        actionsFor: (resumeId) => {
          scope.stop();
          return createResumeEditorActions({
            resumeId, store, coordinator, auth, runtime,
          });
        },
      }))!;

      await (action === 'rename'
        ? list.rename(ID, 'Ada resume')
        : list.remove(ID, 'Fixture'));
      await new Promise((resolve) => setTimeout(resolve, 50));

      expect(store.recordFor(ID)).toMatchObject({ pending: [], attempt: null });
      expect(server.sent).toEqual([]);
    },
  );
});

describe('Try again of a rename refused for good', () => {
  it('shows the attempted title while it is on its way', async () => {
    const context = await openList();
    context.server.failNextWrite({ kind: 'rejected', code: 'request_invalid' });
    await context.rename();
    await context.settle();
    expect(context.card().text()).toContain('Fixture');

    const release = context.server.holdNextResponse();
    await context.card().get('[data-action="retry-rename"]').trigger('click');
    await context.settle();

    expect(context.card().text()).toContain('Ada resume');
    expect(context.card().find('[data-state-mark="saving"]').exists())
      .toBe(true);
    release();
    await context.settle();
    expect(context.server.stored.metadata.title).toBe('Ada resume');
    expect(context.card().text()).toContain('Ada resume');
  });
});

describe('an editor open while the list held a rename', () => {
  it('sends the held rename once after the list goes', async () => {
    const context = await openList({ timers: false });
    context.server.loseEveryResponse({ applied: false });
    await context.rename();
    await vi.waitFor(() => {
      expect(context.server.sent.length).toBeGreaterThanOrEqual(2);
    });
    await openEditorPage();

    context.server.loseEveryResponse(undefined);
    const sends = context.server.sent.length;
    context.wrapper.unmount();

    await vi.waitFor(() => {
      expect(context.server.stored.metadata.title).toBe('Ada resume');
    }, { timeout: 4_000 });
    await new Promise((resolve) => setTimeout(resolve, 1_500));
    expect(context.server.sent).toHaveLength(sends + 1);
    expect(useResumeStore().recordFor(ID)).toMatchObject({
      attempt: null,
      pending: [],
    });
  });
});
