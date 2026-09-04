import { createPinia, setActivePinia } from 'pinia';
import { mount } from '@vue/test-utils';
import { computed, nextTick, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';

// eslint-disable-next-line max-len -- dialog component import.
import CreateResumeDialog from '../../app/components/editor/list/CreateResumeDialog.vue';
// eslint-disable-next-line max-len -- dialog component import.
import DeleteResumeDialog from '../../app/components/editor/list/DeleteResumeDialog.vue';
// eslint-disable-next-line max-len -- dialog component import.
import RenameResumeDialog from '../../app/components/editor/list/RenameResumeDialog.vue';

import ResumeList from '../../app/components/editor/list/ResumeList.vue';
// eslint-disable-next-line max-len -- composable action type import.
import type { ResumeEditorActions } from '../../app/composables/useResumeEditor';
import type { CreateResumeIntent } from '../../app/editor/commands';
import type {
  OpaqueCreateOutcome,
  ResumeMutationCoordinator,
} from '../../app/editor/coordinator';
import type { ResumeApi, ResumeSummary } from '../../app/editor/resumeApi';
import { parseRevision } from '../../app/editor/revision';
import type { EditorRuntime } from '../../app/editor/types';
import { useResumeStore } from '../../app/stores/resumes';
import {
  createStatusMessage,
  useResumeList,
} from '../../app/composables/useResumeList';
import { acceptedFixture } from './fixture';

mockNuxtImport('navigateTo', () => vi.fn());

function summary(overrides: Partial<ResumeSummary> = {}): ResumeSummary {
  const fixture = acceptedFixture();
  return {
    ...fixture.metadata,
    revision: fixture.revision,
    ...overrides,
  };
}

function opaqueOutcome(title = 'First'): OpaqueCreateOutcome {
  return {
    kind: 'create-cutoff',
    intent: {
      kind: 'resumeCreate',
      id: 'intent-opaque',
      ownerId: 'owner-1',
      sequence: 0,
      title,
    },
    attempt: {} as never,
    refreshedItems: [],
  };
}

describe('useResumeList', () => {
  it('waits for auth and preserves the server list order', async () => {
    const older = {
      ...acceptedFixture().metadata,
      revision: parseRevision('1'),
    };
    const newer = {
      ...acceptedFixture().metadata,
      id: 'resume-2',
      revision: parseRevision('2'),
    };
    const api = {
      list: vi.fn().mockResolvedValue({ kind: 'ready', items: [older, newer] }),
    } as unknown as ResumeApi & { list: ReturnType<typeof vi.fn> };
    const authState = ref<'loading' | 'authenticated'>('loading');

    const list = useResumeList({ api, authState: authState as never });
    expect(api.list).not.toHaveBeenCalled();

    authState.value = 'authenticated';
    await nextTick();
    await list.settled();

    expect(list.view.value).toEqual({ kind: 'ready', items: [older, newer] });
    expect(list.items.value).toEqual([older, newer]);
    expect(api.list).toHaveBeenCalledTimes(1);
  });

  it('redirects only after auth resolves anonymous', async () => {
    const authState = ref<'loading' | 'anonymous'>('loading');
    useResumeList({
      api: { list: vi.fn() } as never,
      authState: authState as never,
    });
    expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();

    authState.value = 'anonymous';
    await nextTick();

    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
  });

  it.each([
    { kind: 'session-lost' },
    { kind: 'rate-limited', retryAfterMs: null },
    { kind: 'failed', reason: 'network' },
    { kind: 'failed', reason: 'response-invalid' },
  ] as const)(
    'maps list $kind failures to safe unavailable state',
    async (result) => {
      const list = useResumeList({
        api: { list: vi.fn().mockResolvedValue(result) } as never,
        authState: computed(() => 'authenticated') as never,
      });
      await list.settled();
      expect(list.view.value).toEqual({ kind: 'unavailable' });
    },
  );

  it('keeps one opaque create intent until the owner abandons it', async () => {
    const intent = {
      kind: 'resumeCreate',
      id: 'intent-1',
      ownerId: 'owner-1',
      sequence: 0,
      title: 'First',
    } satisfies CreateResumeIntent;
    const outcome = {
      kind: 'create-cutoff',
      intent,
      attempt: {} as never,
      refreshedItems: [],
    } satisfies OpaqueCreateOutcome;
    const coordinator = {
      createResume: vi
        .fn()
        .mockResolvedValueOnce({ kind: 'opaque-create', outcome })
        .mockResolvedValueOnce({ kind: 'created', resume: acceptedFixture() }),
      abandonOpaqueCreate: vi.fn(),
    } as unknown as ResumeMutationCoordinator & {
      createResume: ReturnType<typeof vi.fn>;
      abandonOpaqueCreate: ReturnType<typeof vi.fn>;
    };
    const ids = ['intent-1', 'intent-2'];
    const runtime: EditorRuntime = {
      nowEpochMs: () => 0,
      uuid: () => ids.shift()!,
      delay: async () => {},
    };
    const list = useResumeList({
      coordinator,
      runtime,
      ownerId: ref('owner-1'),
      authState: computed(() => 'authenticated') as never,
    });

    await expect(list.create('First', undefined)).resolves.toMatchObject({
      kind: 'opaque-create',
      outcome,
    });
    await expect(list.create('Second', undefined)).resolves.toMatchObject({
      kind: 'blocked',
      intentId: 'intent-1',
    });
    list.abandonCreate('intent-1');
    await list.create('Second', null);

    expect(coordinator.abandonOpaqueCreate).toHaveBeenCalledWith('intent-1');
    expect(coordinator.createResume.mock.calls.map(([value]) => value)).toEqual(
      [
        intent,
        expect.objectContaining({
          id: 'intent-2',
          title: 'Second',
          lng: null,
        }),
      ],
    );
  });

  it('captures exact create language presence without a document', async () => {
    const coordinator = {
      createResume: vi.fn().mockResolvedValue({
        kind: 'rejected',
        code: 'request_invalid',
      }),
    } as unknown as ResumeMutationCoordinator & {
      createResume: ReturnType<typeof vi.fn>;
    };
    const ids = ['omit', 'null', 'empty'];
    const list = useResumeList({
      api: {
        list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
      } as never,
      coordinator,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => ids.shift()!,
        delay: async () => {},
      },
      ownerId: ref('owner-1'),
      authState: computed(() => 'authenticated') as never,
    });

    await list.create('One', undefined);
    await list.create('Two', null);
    await list.create('Three', '');

    const captured = coordinator.createResume.mock.calls.map(
      ([intent]) => intent,
    );
    expect(captured).toEqual(
      [
        expect.not.objectContaining({
          lng: expect.anything(),
          document: expect.anything(),
        }),
        expect.objectContaining({ id: 'null', lng: null }),
        expect.objectContaining({ id: 'empty', lng: '' }),
      ],
    );
  });

  it('navigates only to the validated created response ID', async () => {
    const created = acceptedFixture({
      metadata: { ...acceptedFixture().metadata, id: 'server-id' },
    });
    const list = useResumeList({
      api: {
        list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
      } as never,
      coordinator: {
        createResume: vi.fn().mockResolvedValue({
          kind: 'created',
          resume: created,
        }),
      } as never,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'client-id',
        delay: async () => {},
      },
      ownerId: ref('owner-1'),
      authState: computed(() => 'authenticated') as never,
    });

    await list.create('Title', undefined);

    expect(vi.mocked(navigateTo)).toHaveBeenLastCalledWith(
      '/app/resumes/server-id',
    );
  });

  it('refreshes opaque summaries without claiming a create', async () => {
    const intent = {
      kind: 'resumeCreate',
      id: 'intent-1',
      ownerId: 'owner-1',
      sequence: 0,
      title: 'One',
    } satisfies CreateResumeIntent;
    const outcome = {
      kind: 'create-cutoff',
      intent,
      attempt: {} as never,
      refreshedItems: [],
    } satisfies OpaqueCreateOutcome;
    const refreshed = [{
      ...acceptedFixture().metadata,
      revision: parseRevision('1'),
    }];
    const coordinator = {
      createResume: vi.fn().mockResolvedValue({
        kind: 'opaque-create',
        outcome,
      }),
      refreshOpaqueCreate: vi.fn().mockResolvedValue({
        ...outcome,
        refreshedItems: refreshed,
      }),
    } as never;
    const list = useResumeList({
      api: {
        list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
      } as never,
      coordinator,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'intent-1',
        delay: async () => {},
      },
      ownerId: ref('owner-1'),
      authState: computed(() => 'authenticated') as never,
    });

    await list.create('One', undefined);
    await list.refreshCreate(intent.id);

    expect(list.items.value).toEqual(refreshed);
    expect(vi.mocked(navigateTo)).not.toHaveBeenLastCalledWith(
      `/app/resumes/${refreshed[0]!.id}`,
    );
  });

  it('maps resume-cap rejection to fixed safe copy', () => {
    expect(createStatusMessage({
      kind: 'rejected',
      code: 'resume_cap_exceeded',
    })).toBe(
      'You have reached the resume limit.',
    );
  });

  it(
    'shares one in-flight create request across duplicate submit events',
    async () => {
      let resolveCreate: ((value: unknown) => void) | undefined;
      const coordinator = {
        createResume: vi.fn(() => new Promise((resolve) => {
          resolveCreate = resolve;
        })),
      } as unknown as ResumeMutationCoordinator & {
        createResume: ReturnType<typeof vi.fn>;
      };
      const list = useResumeList({
        api: {
          list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
        } as never,
        coordinator,
        runtime: {
          nowEpochMs: () => 0,
          uuid: () => 'intent-1',
          delay: async () => {},
        },
        ownerId: ref('owner-1'),
        authState: computed(() => 'authenticated') as never,
      });

      const first = list.create('First', undefined);
      const second = list.create('First', undefined);
      expect(coordinator.createResume).toHaveBeenCalledTimes(1);
      resolveCreate?.({ kind: 'rejected', code: 'request_invalid' });

      await expect(second).resolves.toEqual(await first);
    },
  );

  it('reads a complete owner snapshot before rename and delete', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const api = {
      list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
      read: vi.fn().mockResolvedValue({ kind: 'complete', accepted }),
    } as unknown as ResumeApi & { read: ReturnType<typeof vi.fn> };
    const store = useResumeStore();
    const edit = vi.fn();
    const actions = { edit } as unknown as ResumeEditorActions;
    const list = useResumeList({
      api,
      store,
      actionsFor: () => actions,
      authState: computed(() => 'authenticated') as never,
    });

    await list.rename(accepted.metadata.id, 'New title');
    await list.remove(accepted.metadata.id, accepted.metadata.title);

    expect(api.read).toHaveBeenCalledTimes(2);
    expect(edit.mock.calls.map(([command]) => command)).toEqual([
      { kind: 'metadataField', field: 'title', value: 'New title' },
      { kind: 'resumeDelete', confirmedTitle: 'Fixture' },
    ]);
  });

  it('adopts a fresh read without discarding queued local work', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(accepted.metadata.id, {
      id: 'pending-1',
      kind: 'metadataField',
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      targetKey: 'metadata:title',
      base: {} as never,
      intended: null,
      dependencyIds: [],
      field: 'title',
      value: 'Queued',
    });
    const api = {
      list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
      read: vi.fn().mockResolvedValue({ kind: 'complete', accepted }),
    } as unknown as ResumeApi;
    const list = useResumeList({
      api,
      store,
      actionsFor: () => ({ edit: vi.fn() }) as never,
      authState: computed(() => 'authenticated') as never,
    });

    await list.rename(accepted.metadata.id, 'Later');

    expect(store.recordFor(accepted.metadata.id)?.pending).toHaveLength(1);
    expect(store.recordFor(accepted.metadata.id)?.pending[0]?.id).toBe(
      'pending-1',
    );
  });

  it('requires the fresh title before enqueuing resume deletion', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture({
      metadata: { ...acceptedFixture().metadata, title: 'Fresh title' },
    });
    const edit = vi.fn();
    const list = useResumeList({
      api: {
        list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
        read: vi.fn().mockResolvedValue({ kind: 'complete', accepted }),
      } as never,
      store: useResumeStore(),
      actionsFor: () => ({ edit }) as never,
      authState: computed(() => 'authenticated') as never,
    });

    await list.remove(accepted.metadata.id, 'Old summary title');

    expect(edit).not.toHaveBeenCalled();
    expect(list.actionMessage.value).toBe(
      'This resume changed. Reopen deletion and confirm its current title.',
    );
  });

  it('keeps a deleting row until a definitive store deletion', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const api = {
      list: vi.fn().mockResolvedValue({
        kind: 'ready',
        items: [{ ...accepted.metadata, revision: accepted.revision }],
      }),
      read: vi.fn().mockResolvedValue({ kind: 'complete', accepted }),
    } as unknown as ResumeApi;
    const store = useResumeStore();
    const list = useResumeList({
      api,
      store,
      actionsFor: () => ({ edit: () => ({ kind: 'enqueued' }) }) as never,
      authState: computed(() => 'authenticated') as never,
    });
    await nextTick();
    await list.settled();

    await list.remove(accepted.metadata.id, accepted.metadata.title);
    expect(list.items.value).toHaveLength(1);
    store.removeResume(accepted.metadata.id);
    await nextTick();

    expect(list.items.value).toEqual([]);
  });

  it(
    'flushes a confirmed delete and waits for definitive removal',
    async () => {
      setActivePinia(createPinia());
      const accepted = acceptedFixture();
      const store = useResumeStore();
      const flush = vi.fn(async (id: string) => store.removeResume(id));
      const refreshAuth = vi.fn().mockResolvedValue(undefined);
      const list = useResumeList({
        api: {
          list: vi.fn().mockResolvedValue({
            kind: 'ready',
            items: [{ ...accepted.metadata, revision: accepted.revision }],
          }),
          read: vi.fn().mockResolvedValue({ kind: 'complete', accepted }),
        } as never,
        store,
        coordinator: { flush } as unknown as ResumeMutationCoordinator,
        refreshAuth,
        actionsFor: () => ({
          edit: () => ({ kind: 'enqueued' }),
        }) as never,
        authState: computed(() => 'authenticated') as never,
      });
      await nextTick();
      await list.settled();

      await list.remove(accepted.metadata.id, accepted.metadata.title);
      await nextTick();

      expect(flush).toHaveBeenCalledWith(accepted.metadata.id);
      expect(refreshAuth).toHaveBeenCalledOnce();
      expect(list.items.value).toEqual([]);
    },
  );

  it('isolates busy state to the active resume row', () => {
    const first = {
      ...acceptedFixture().metadata,
      revision: parseRevision('1'),
    };
    const second = {
      ...first,
      id: 'resume-2',
      revision: parseRevision('2'),
    };
    const wrapper = mount(ResumeList, {
      props: {
        items: [first, second],
        busyIds: [first.id],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
      global: { stubs: { NuxtLink: true } },
    });

    const firstButton = wrapper.get(
      `[data-testid="resume-row-${first.id}"]`
      + ` [aria-label="More actions for ${first.title}"]`,
    );
    const secondButton = wrapper.get(
      `[data-testid="resume-row-${second.id}"]`
      + ` [aria-label="More actions for ${second.title}"]`,
    );
    expect(firstButton.attributes('disabled')).toBeDefined();
    expect(secondButton.attributes('disabled')).toBeUndefined();
  });

  it.each([
    ['resume-1', 'resume-2'],
    ['resume-2', 'resume-3'],
    ['resume-3', null],
  ])(
    'focuses deleted %s on %s or Create',
    async (removed, nextId) => {
      const first = {
        ...acceptedFixture().metadata,
        revision: parseRevision('1'),
      };
      const second = {
        ...first,
        id: 'resume-2',
        revision: parseRevision('2'),
      };
      const third = {
        ...first,
        id: 'resume-3',
        revision: parseRevision('3'),
      };
      const wrapper = mount(ResumeList, {
        attachTo: document.body,
        props: {
          items: [first, second, third],
          busyIds: [],
          removalFocusId: null,
          removalFocusVersion: 0,
        },
        global: { stubs: { NuxtLink: true } },
      });
      await wrapper.setProps({
        items: [first, second, third].filter((item) => item.id !== removed),
        removalFocusId: nextId,
        removalFocusVersion: 1,
      });
      await nextTick();
      await nextTick();
      const target = nextId === null
        ? wrapper.get('[data-testid="create-resume"]').element
        : wrapper.get(
          `[data-testid="resume-row-${nextId}"]`
          + ' [aria-label^="More actions for "]',
        ).element;
      expect(document.activeElement).toBe(target);
      wrapper.unmount();
    },
  );

  it(
    'renders three sheets without empty slots and disables Create resume',
    () => {
      const wrapper = mount(ResumeList, {
        props: {
          items: [1, 2, 3].map((id) => summary({ id: `r${id}` })),
          busyIds: [],
          removalFocusId: null,
          removalFocusVersion: 0,
        },
        global: { stubs: { NuxtLink: true } },
      });
      const list = wrapper.get('[data-testid="resume-list"]');
      expect(list.classes()).toEqual(expect.arrayContaining([
        'mx-auto',
        'w-full',
        'max-w-7xl',
        'px-6',
        'py-8',
        'space-y-8',
      ]));
      const sheets = wrapper.findAll('[data-testid^="resume-row-"]');
      expect(sheets).toHaveLength(3);
      expect(sheets[0]!.classes()).toEqual(
        expect.arrayContaining([
          'rounded-[var(--radius-dialog)]',
          'shadow-[var(--shadow-paper)]',
        ]),
      );
      expect(wrapper.findAll('[data-testid^="resume-slot-"]')).toHaveLength(0);
      expect(
        wrapper.get('[data-testid="create-resume"]').attributes('disabled'),
      ).toBeDefined();
      wrapper.unmount();
    },
  );

  it('renders two dashed slots for one resume', () => {
    const wrapper = mount(ResumeList, {
      props: {
        items: [summary({ id: 'r1', title: 'First' })],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
      global: { stubs: { NuxtLink: true } },
    });
    expect(wrapper.findAll('[data-testid^="resume-row-"]')).toHaveLength(1);
    expect(wrapper.findAll('[data-testid^="resume-slot-"]')).toHaveLength(2);
    wrapper.unmount();
  });

  it('renders the empty status in the first of three slots', () => {
    const wrapper = mount(ResumeList, {
      props: {
        items: [],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
    });
    expect(wrapper.findAll('[data-testid^="resume-slot-"]')).toHaveLength(3);
    expect(wrapper.get('[role="status"]').text()).toBe('No resumes yet.');
    expect(
      wrapper.get('[data-action="create-resume-slot"]')
        .attributes('data-slot'),
    ).toBe('button');
    expect(wrapper.text()).toContain(
      'Create your first resume. You can keep up to three.',
    );
    wrapper.unmount();
  });

  it('shows public and draft marks from the summary fields', () => {
    const publicItem = summary({
      id: 'public',
      title: 'Public resume',
      live: true,
      slug: 'ada-lovelace',
    });
    const draftItem = summary({
      id: 'draft',
      title: 'Draft resume',
      live: false,
      slug: 'stale-slug',
    });
    const wrapper = mount(ResumeList, {
      props: {
        items: [publicItem, draftItem],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
      global: { stubs: { NuxtLink: true } },
    });
    const publicRow = wrapper.get('[data-testid="resume-row-public"]');
    const draftRow = wrapper.get('[data-testid="resume-row-draft"]');
    expect(publicRow.text()).toContain('aboutme.vn/ada-lovelace');
    expect(publicRow.text()).not.toContain('stale-slug');
    const sheetLink = publicRow.get('[data-sheet-link]').element;
    const publicLink = publicRow.get('[data-public-link]').element;
    expect(sheetLink.contains(publicLink)).toBe(false);
    expect(draftRow.text()).toContain('Draft');
    expect(draftRow.text()).not.toContain('aboutme.vn');
    wrapper.unmount();
  });

  it('opens the editor from the sheet link with Enter', async () => {
    const wrapper = mount(ResumeList, {
      props: {
        items: [summary({ id: 'resume/one' })],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
        now: new Date('2026-09-04T12:00:00Z'),
      },
      global: {
        stubs: {
          NuxtLink: {
            props: ['to'],
            template: '<a :href="to"><slot /></a>',
          },
        },
      },
    });
    const link = wrapper.get(
      '[data-testid="resume-row-resume/one"] [data-sheet-link]',
    );
    expect(link.attributes('href')).toBe('/app/resumes/resume%2Fone');
    await link.trigger('keydown.enter');
    expect(wrapper.emitted('rename')).toBeUndefined();
    expect(wrapper.emitted('remove')).toBeUndefined();
    wrapper.unmount();
  });

  it('emits Rename from the overflow menu without navigating', async () => {
    const item = summary({ id: 'r1', title: 'First' });
    const wrapper = mount(ResumeList, {
      attachTo: document.body,
      props: {
        items: [item],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
      global: { stubs: { NuxtLink: true } },
    });
    const trigger = wrapper.get('[aria-label="More actions for First"]');
    await trigger.trigger('click');
    const rename = document.body.querySelector<HTMLElement>(
      '[aria-label="Rename First"]',
    );
    expect(rename).not.toBeNull();
    rename?.click();
    await nextTick();
    expect(wrapper.emitted('rename')).toEqual([[item]]);
    expect(wrapper.emitted('remove')).toBeUndefined();
    wrapper.unmount();
  });

  it(
    'returns focus to the overflow trigger when the menu closes with Escape',
    async () => {
      const wrapper = mount(ResumeList, {
        attachTo: document.body,
        props: {
          items: [summary({ id: 'r1', title: 'First' })],
          busyIds: [],
          removalFocusId: null,
          removalFocusVersion: 0,
        },
      });
      const trigger = wrapper.get('[aria-label="More actions for First"]');
      await trigger.trigger('click');
      const menu = document.body.querySelector<HTMLElement>('[role="menu"]');
      expect(menu).not.toBeNull();
      menu?.focus();
      menu?.dispatchEvent(
        new KeyboardEvent('keydown', {
          bubbles: true,
          cancelable: true,
          key: 'Escape',
        }),
      );
      await nextTick();
      await nextTick();
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(document.activeElement).toBe(trigger.element);
      wrapper.unmount();
    },
  );

  it('gates deletion on the exact current title', async () => {
    const wrapper = mount(DeleteResumeDialog, {
      attachTo: document.body,
      props: { item: { id: 'r1', title: 'First' }, busy: false },
    });
    await nextTick();
    const confirm = document.body.querySelector<HTMLButtonElement>(
      '[data-action="confirm-delete"]',
    )!;
    expect(confirm).not.toBeNull();
    if (confirm === null) {
      wrapper.unmount();
      return;
    }
    expect(confirm.disabled).toBe(true);
    const input = document.body.querySelector<HTMLInputElement>(
      '[role="alertdialog"] [data-slot="input"]',
    )!;
    expect(input).not.toBeNull();
    if (input === null) {
      wrapper.unmount();
      return;
    }
    const label = document.body.querySelector(
      `[data-slot="label"][for="${input.id}"]`,
    );
    expect(label).not.toBeNull();
    if (label === null) {
      wrapper.unmount();
      return;
    }
    expect(label.textContent).toContain('Current title');
    input.value = 'First';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await nextTick();
    expect(confirm.disabled).toBe(false);
    confirm.click();
    expect(wrapper.emitted('submit')).toEqual([['r1', 'First']]);
    wrapper.unmount();
  });

  it.each(['first', 'First '])(
    'keeps deletion disabled for a non-exact title %s', async (value) => {
      const wrapper = mount(DeleteResumeDialog, {
        attachTo: document.body,
        props: { item: { id: 'r1', title: 'First' }, busy: false },
      });
      await nextTick();
      const input = document.body.querySelector<HTMLInputElement>(
        '[role="alertdialog"] [data-slot="input"]',
      )!;
      expect(input).not.toBeNull();
      if (input === null) {
        wrapper.unmount();
        return;
      }
      input.value = value;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      await nextTick();
      const confirm = document.body.querySelector<HTMLButtonElement>(
        '[data-action="confirm-delete"]',
      );
      expect(confirm).not.toBeNull();
      if (confirm === null) {
        wrapper.unmount();
        return;
      }
      expect(confirm.disabled).toBe(true);
      expect(wrapper.emitted('submit')).toBeUndefined();
      wrapper.unmount();
    },
  );

  it('does not submit rename when the title is unchanged', async () => {
    const wrapper = mount(RenameResumeDialog, {
      attachTo: document.body,
      props: { item: summary({ id: 'r1', title: 'First' }), busy: false },
    });
    await nextTick();
    const form = document.body.querySelector<HTMLFormElement>(
      '[role="dialog"] [novalidate]',
    );
    expect(form).not.toBeNull();
    if (form === null) {
      wrapper.unmount();
      return;
    }
    form.dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    await nextTick();
    expect(wrapper.emitted('submit')).toBeUndefined();
    wrapper.unmount();
  });

  it(
    'shows recovery actions without the form for an opaque create',
    async () => {
      const wrapper = mount(CreateResumeDialog, {
        attachTo: document.body,
        props: { open: true, busy: false, retained: opaqueOutcome() },
      });
      await nextTick();
      expect(document.body.textContent).toContain(
        'We could not confirm whether this resume was created.',
      );
      expect(document.body.textContent).toContain('Refresh list');
      expect(document.body.textContent).toContain('Abandon');
      expect(document.body.querySelector('[name="title"]')).toBeNull();
      wrapper.unmount();
    });

  it('renders hostile titles as text in the row and dialogs', async () => {
    const hostile = '<img src=x onerror=alert(1)>';
    const item = summary({ id: 'hostile', title: hostile });
    const rowWrapper = mount(ResumeList, {
      props: {
        items: [item],
        busyIds: [],
        removalFocusId: null,
        removalFocusVersion: 0,
      },
    });
    const row = rowWrapper.get(`[data-testid="resume-row-${item.id}"]`);
    expect(descendantNames(row.element)).not.toContain('img');
    expect(row.text()).toContain(hostile);
    rowWrapper.unmount();

    const renameWrapper = mount(RenameResumeDialog, {
      attachTo: document.body,
      props: { item, busy: false },
    });
    await nextTick();
    const renameInput = document.body.querySelector<HTMLInputElement>(
      '[role="dialog"] [data-slot="input"]',
    )!;
    expect(renameInput).not.toBeNull();
    if (renameInput === null) {
      renameWrapper.unmount();
      return;
    }
    expect(renameInput.value).toBe(hostile);
    expect(
      document.body.querySelector(
        '[role="dialog"] [onerror]',
      ),
    ).toBeNull();
    renameWrapper.unmount();

    const deleteWrapper = mount(DeleteResumeDialog, {
      attachTo: document.body,
      props: { item, busy: false },
    });
    await nextTick();
    const alertDialog = document.body.querySelector(
      '[role="alertdialog"]',
    );
    expect(alertDialog).not.toBeNull();
    if (alertDialog === null) {
      deleteWrapper.unmount();
      return;
    }
    expect(alertDialog.querySelector('[onerror]')).toBeNull();
    expect(alertDialog.textContent).toContain(hostile);
    deleteWrapper.unmount();
  });

  it('returns focus after create cancel with dialog semantics', async () => {
    const trigger = document.createElement('button');
    document.body.append(trigger);
    trigger.focus();
    const wrapper = mount(CreateResumeDialog, {
      attachTo: document.body,
      props: { open: true, busy: false, retained: null },
    });
    await nextTick();
    const dialog = document.body.querySelector(
      '[role="dialog"]',
    );
    expect(dialog).not.toBeNull();
    if (dialog === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    expect(dialog.getAttribute('aria-labelledby')).toBeTruthy();
    expect(dialog.getAttribute('aria-describedby')).toBeTruthy();
    const form = document.body.querySelector<HTMLFormElement>(
      '[role="dialog"] [novalidate]',
    );
    expect(form).not.toBeNull();
    if (form === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    form.dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    await nextTick();
    expect(wrapper.emitted('submit')).toEqual([['', undefined]]);
    const cancel = document.body.querySelector<HTMLButtonElement>(
      '[role="dialog"] [data-slot="button"][type="button"]',
    );
    expect(cancel).not.toBeNull();
    if (cancel === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    cancel.click();
    await wrapper.setProps({ open: false });
    await nextTick();
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount();
    trigger.remove();
  });

  it('returns focus after rename Escape and keyboard submit', async () => {
    const trigger = document.createElement('button');
    document.body.append(trigger);
    trigger.focus();
    const item = {
      ...acceptedFixture().metadata,
      revision: parseRevision('1'),
    };
    const wrapper = mount(RenameResumeDialog, {
      attachTo: document.body,
      props: { item, busy: false },
    });
    await nextTick();
    const form = document.body.querySelector<HTMLFormElement>(
      '[role="dialog"] [novalidate]',
    );
    expect(form).not.toBeNull();
    if (form === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    const input = document.body.querySelector<HTMLInputElement>(
      '[role="dialog"] [data-slot="input"]',
    );
    expect(input).not.toBeNull();
    if (input === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    input.value = 'Renamed';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await nextTick();
    form.dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    await nextTick();
    expect(wrapper.emitted('submit')).toEqual([[item.id, 'Renamed']]);
    const dialog = document.body.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    if (dialog === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    dialog.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    await nextTick();
    expect(wrapper.emitted('close')).toHaveLength(1);
    await wrapper.setProps({ item: null });
    await nextTick();
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount();
    trigger.remove();
  });

  it('returns focus after a successful delete close', async () => {
    const trigger = document.createElement('button');
    document.body.append(trigger);
    trigger.focus();
    const item = {
      ...acceptedFixture().metadata,
      revision: parseRevision('1'),
    };
    const wrapper = mount(DeleteResumeDialog, {
      attachTo: document.body,
      props: { item, busy: false },
    });
    await nextTick();
    const input = document.body.querySelector<HTMLInputElement>(
      '[role="alertdialog"] [data-slot="input"]',
    )!;
    expect(input).not.toBeNull();
    if (input === null) {
      wrapper.unmount();
      trigger.remove();
      return;
    }
    input.value = item.title;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await nextTick();
    document.body.querySelector<HTMLButtonElement>(
      '[data-action="confirm-delete"]',
    )!.click();
    await nextTick();
    expect(wrapper.emitted('submit')).toEqual([[item.id, item.title]]);
    await wrapper.setProps({ item: null });
    await nextTick();
    await nextTick();
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount();
    trigger.remove();
  });
});

function descendantNames(root: Element): string[] {
  const names: string[] = [];
  const pending = [...root.children];
  while (pending.length > 0) {
    const element = pending.pop()!;
    names.push(element.localName);
    pending.push(...element.children);
  }
  return names;
}
