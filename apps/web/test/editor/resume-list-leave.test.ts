import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { computed, defineComponent, h, nextTick, ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import ResumeList from '../../app/components/editor/list/ResumeList.vue';
import {
  useResumeList,
  type ResumeListController,
} from '../../app/composables/useResumeList';
import ResumeListPage from '../../app/pages/app/resumes/index.vue';
import { useResumeStore } from '../../app/stores/resumes';
import { setSiteLocale } from '../support/locale';
import {
  fakeServer,
  startingResume,
  type FakeResumeServer,
} from './fake-resume-server';

// The resume list page renames and deletes through its own save queue: the
// card says when that is being saved or held, Try again goes through the
// queue, leaving warns while it is unsettled, and nothing is read or written
// once the page has gone.

const mocks = vi.hoisted(() => ({
  auth: undefined as unknown,
  server: undefined as unknown,
}));

mockNuxtImport('useAuth', () => () => mocks.auth);
mockNuxtImport('navigateTo', () => vi.fn());

vi.mock('../../app/editor/resumeApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/editor/resumeApi')>()),
  createResumeApi: () => (mocks.server as FakeResumeServer).api,
}));

const ID = 'resume-1';
const ROW = `[data-testid="resume-row-${ID}"]`;

function signedIn(): void {
  mocks.auth = {
    authState: ref('authenticated'),
    user: computed(() => ({ id: 'owner-1' })),
    csrfToken: computed(() => 'csrf-1'),
    refresh: vi.fn(async () => {}),
  };
}

/** Whether closing or reloading the tab would ask first. */
function leaveWarns(): boolean {
  const event = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

/**
 * The real list page over the fake server. The rename and delete dialogs
 * are stand-ins that submit at once, as the real ones do on Save.
 */
async function openList() {
  const server = fakeServer(startingResume());
  mocks.server = server;
  signedIn();
  const wrapper = await mountSuspended(ResumeListPage, {
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
  await vi.waitFor(() => {
    expect(wrapper.find(ROW).exists()).toBe(true);
  });
  vi.useFakeTimers();
  vi.setSystemTime(0);
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
    /** Renames the card to "Ada resume" from its menu's dialog. */
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

afterEach(() => {
  vi.useRealTimers();
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

      // The queue retries on its own, without a busy loop.
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
    // Leaving drops the unsent rename, as closing the editor does.
    expect(useResumeStore().recordFor(ID)).toMatchObject({
      attempt: null,
      pending: [],
    });
  });
});
