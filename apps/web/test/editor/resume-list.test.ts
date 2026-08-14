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
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { CreateResumeIntent } from '../../app/editor/commands';
import type {
  OpaqueCreateOutcome,
  ResumeMutationCoordinator,
} from '../../app/editor/coordinator';
import type { ResumeApi } from '../../app/editor/resumeApi';
import { parseRevision } from '../../app/editor/revision';
import type { EditorRuntime } from '../../app/editor/types';
import { useResumeStore } from '../../app/stores/resumes';
import {
  createStatusMessage,
  useResumeList,
} from '../../app/composables/useResumeList';
import { acceptedFixture } from './fixture';

mockNuxtImport('navigateTo', () => vi.fn());

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
      `[data-testid="resume-row-${first.id}"] button`,
    );
    const secondButton = wrapper.get(
      `[data-testid="resume-row-${second.id}"] button`,
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
        : wrapper.get(`[data-testid="resume-row-${nextId}"] button`).element;
      expect(document.activeElement).toBe(target);
      wrapper.unmount();
    },
  );

  it('returns focus after create cancel with dialog semantics', async () => {
    const trigger = document.createElement('button');
    document.body.append(trigger);
    trigger.focus();
    const wrapper = mount(CreateResumeDialog, {
      attachTo: document.body,
      props: { open: true, busy: false, retained: null },
    });
    await nextTick();
    const dialog = wrapper.get('[role="dialog"]');
    expect(dialog.attributes()).toMatchObject({
      'aria-labelledby': 'create-resume-title',
      'aria-describedby': 'create-resume-description',
    });
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')).toEqual([['', undefined]]);
    await wrapper.get('button[type="button"]').trigger('click');
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
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')).toEqual([[item.id, item.title]]);
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
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
    const input = wrapper.get('input');
    await input.setValue(item.title);
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')).toEqual([[item.id, item.title]]);
    await wrapper.setProps({ item: null });
    await nextTick();
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount();
    trigger.remove();
  });
});
