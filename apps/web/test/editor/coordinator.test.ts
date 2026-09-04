import { TEMPLATES } from '@aboutme/schema/templates';
import { createPinia, setActivePinia } from 'pinia';
import { computed, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import { captureCommand } from '../../app/editor/commands';
import { applyIntent } from '../../app/editor/reducer';
import { parseRevision } from '../../app/editor/revision';
import {
  advanceTemplateGroup,
  captureTemplateGroup,
} from '../../app/editor/templateGroup';
import { useResumeStore } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const preset = TEMPLATES.find(
  ({ customization }) => customization.layout.placement === 'byType',
)!;

describe('resume editor actions', () => {
  it('captures atomic and template IDs from the injected runtime', () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture({
      document: {
        ...acceptedFixture().document,
        content: { skill: { sectionType: 'skill', entries: [] } },
        customization: {
          ...acceptedFixture().document.customization,
          layout: {
            ...acceptedFixture().document.customization.layout,
            sections: { main: ['skill'], sidebar: [] },
          },
        },
      },
    });
    const store = useResumeStore();
    store.initialize(accepted);
    const coordinator = {
      schedule: vi.fn(),
    } as never;
    const ids = ['command-1', 'group-1', 'structure-1', 'customization-1'];
    const actions = createResumeEditorActions({
      resumeId: accepted.metadata.id,
      store,
      coordinator,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
      } as never,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => ids.shift()!,
        delay: async () => {},
      },
    });

    expect(
      actions.edit({ kind: 'metadataField', field: 'title', value: 'Ada' }),
    ).toMatchObject({
      kind: 'enqueued',
      command: { id: 'command-1', ownerId: 'owner-1', sequence: 1 },
    });
    expect(actions.applyTemplate(preset)).toMatchObject({
      kind: 'enqueued',
      group: {
        id: 'group-1',
        children: [{ id: 'structure-1' }, { id: 'customization-1' }],
      },
    });
    expect(
      store.recordFor(accepted.metadata.id)!.pending.map(({ kind }) => kind),
    ).toEqual(['metadataField', 'templateGroup']);
    expect(
      (coordinator as { schedule: ReturnType<typeof vi.fn> }).schedule,
    ).toHaveBeenCalledTimes(2);
  });

  it('creates entity IDs only through the injected runtime', () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const store = useResumeStore();
    store.initialize(accepted);
    const uuid = vi.fn(() => 'entity-1');
    const actions = createResumeEditorActions({
      resumeId: accepted.metadata.id,
      store,
      coordinator: { schedule: vi.fn() } as never,
      auth: { user: computed(() => ({ id: 'owner-1' })) } as never,
      runtime: { nowEpochMs: () => 0, uuid, delay: async () => {} },
    });

    expect(actions.createEntityId()).toBe('entity-1');
    expect(uuid).toHaveBeenCalledOnce();
  });
});

describe('mutation coordinator', () => {
  // eslint-disable-next-line max-len -- exact regression name.
  it('conflicts a second crop when the accepted photo is replaced', async () => {
    setActivePinia(createPinia());
    const fixture = acceptedFixture();
    const accepted = {
      ...fixture,
      document: {
        ...fixture.document,
        personalDetails: {
          ...fixture.document.personalDetails,
          photo: { key: 'photo-a' },
        },
      },
    };
    const first = {
      ...accepted,
      document: {
        ...accepted.document,
        personalDetails: {
          ...accepted.document.personalDetails,
          photo: {
            key: 'photo-a',
            crop: { x: 0, y: 0, width: 0.75, height: 1 },
          },
        },
      },
      revision: parseRevision('2'),
    };
    const winner = {
      ...first,
      document: {
        ...first.document,
        personalDetails: {
          ...first.document.personalDetails,
          photo: { key: 'photo-b' },
        },
      },
      revision: parseRevision('3'),
    };
    const store = useResumeStore();
    store.initialize(accepted);
    const api = {
      dispatch: vi.fn()
        .mockResolvedValueOnce({
          kind: 'complete', status: 200, accepted: first,
        })
        .mockResolvedValueOnce({
          kind: 'stale', status: 412,
          winner: { document: winner.document, revision: winner.revision },
        }),
    } as never;
    const ids = ['crop-1', 'attempt-1', 'crop-2', 'attempt-2'];
    const runtime = {
      nowEpochMs: () => 0,
      uuid: () => ids.shift()!,
      delay: async () => {},
    };
    const auth = {
      user: computed(() => ({ id: 'owner-1' })),
      csrfToken: computed(() => 'csrf-1'),
      authState: computed(() => 'authenticated'),
    } as never;
    const coordinator = createMutationCoordinator({
      api, store, auth, runtime,
    });
    const actions = createResumeEditorActions({
      resumeId: accepted.metadata.id,
      store,
      coordinator,
      auth,
      runtime,
    });

    expect(actions.edit({
      kind: 'photoCrop',
      crop: { x: 0, y: 0, width: 0.75, height: 1 },
    })).toMatchObject({ kind: 'enqueued' });
    await coordinator.flush(accepted.metadata.id);
    expect(actions.edit({
      kind: 'photoCrop',
      crop: { x: 0, y: 0, width: 0.5, height: 1 },
    })).toMatchObject({ kind: 'enqueued' });
    expect(store.saveStateFor(accepted.metadata.id)).toBe('dirty');
    await coordinator.flush(accepted.metadata.id);

    expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
      .toHaveBeenCalledTimes(2);
    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      accepted: {
        document: { personalDetails: { photo: { key: 'photo-b' } } },
        revision: parseRevision('3'),
      },
      conflicts: [{ kind: 'photo-changed', command: { kind: 'photoCrop' } }],
    });
  });

  it('marks a template partial when a later child is definitively rejected',
    async () => {
      setActivePinia(createPinia());
      const fixture = acceptedFixture();
      const accepted = {
        ...fixture,
        document: {
          ...fixture.document,
          content: { skill: { sectionType: 'skill' as const, entries: [] } },
          customization: {
            ...fixture.document.customization,
            layout: {
              ...fixture.document.customization.layout,
              sections: { main: ['skill'], sidebar: [] },
            },
          },
        },
      };
      const ids = ['group-1', 'structure-1', 'customization-1'];
      const runtime = {
        nowEpochMs: () => 0,
        uuid: () => ids.shift()!,
        delay: async () => {},
      };
      const group = captureTemplateGroup({
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        current: accepted,
        preset,
        dependencyIds: [],
        runtime,
      })!;
      expect(group.children).toHaveLength(2);
      const intermediate = {
        ...applyIntent(accepted, group.children[0]!),
        revision: parseRevision('2'),
        metadataFreshness: 'complete' as const,
      };
      expect(
        advanceTemplateGroup(
          group,
          { kind: 'queued', nextChild: 0 },
          intermediate,
        ),
      ).toMatchObject({ kind: 'running', nextChild: 1 });
      const store = useResumeStore();
      store.initialize(accepted);
      store.enqueue(accepted.metadata.id, group);
      const api = {
        dispatch: vi.fn()
          .mockResolvedValueOnce({
            kind: 'complete', status: 200, accepted: intermediate,
          })
          .mockResolvedValueOnce({
            kind: 'validation-rejected',
            issues: [{ path: 'customization', code: 'invalid' }],
          }),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'csrf-1'),
          authState: computed(() => 'authenticated'),
        } as never,
        runtime,
      });

      await coordinator.flush(accepted.metadata.id);

      expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
        .toHaveBeenCalledTimes(2);
      const record = store.recordFor(accepted.metadata.id)!;
      expect(record.templateState).toEqual({
        kind: 'partial',
        accepted: intermediate,
        nextChild: 1,
        reason: 'child-failed',
      });
      expect(record.attempt).toBeNull();
      expect(record.pending[0]?.id).toBe(group.id);
      expect(record.issues[group.id]).toEqual([
        { path: 'customization', code: 'invalid' },
      ]);
      const actions = createResumeEditorActions({
        resumeId: accepted.metadata.id,
        store,
        coordinator,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
        } as never,
        runtime,
      });
      expect(actions.recoverTemplate('keep-partial')).toEqual({
        kind: 'keep-partial',
      });
      expect(store.recordFor(accepted.metadata.id)!.issues).toEqual({});
    },
  );

  it('dispatches once one second after the last local edit', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    try {
      setActivePinia(createPinia());
      const accepted = acceptedFixture();
      const store = useResumeStore();
      store.initialize(accepted);
      const command = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        {
          nowEpochMs: () => Date.now(),
          uuid: () => 'command-1',
          delay: async () => {},
        },
      );
      store.enqueue(accepted.metadata.id, command);
      const api = {
        dispatch: vi.fn().mockResolvedValue({
          kind: 'validation-rejected',
          issues: [],
        }),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'csrf-1'),
          authState: computed(() => 'authenticated'),
          refresh: vi.fn(),
        } as never,
        runtime: {
          nowEpochMs: () => Date.now(),
          uuid: () => 'attempt-1',
          delay: async () => {},
        },
      });

      coordinator.schedule(accepted.metadata.id);
      await vi.advanceTimersByTimeAsync(700);
      coordinator.schedule(accepted.metadata.id);
      await vi.advanceTimersByTimeAsync(999);
      expect(
        (api as { dispatch: ReturnType<typeof vi.fn> }).dispatch,
      ).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      expect(
        (api as { dispatch: ReturnType<typeof vi.fn> }).dispatch,
      ).toHaveBeenCalledOnce();
      expect(
        (api as { dispatch: ReturnType<typeof vi.fn> }).dispatch.mock
          .calls[0]![0],
      ).toMatchObject({ firstDispatchAt: 1700 });
    } finally {
      vi.useRealTimers();
    }
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('refreshes an authenticated missing csrf token before dispatch', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const saved = {
      ...accepted,
      metadata: { ...accepted.metadata, title: 'Ada' },
      revision: parseRevision('2'),
    };
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(
      accepted.metadata.id,
      captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      ),
    );
    const token = ref<string | null>(null);
    const refresh = vi.fn(async () => {
      token.value = 'csrf-new';
    });
    const dispatch = vi.fn().mockResolvedValue({
      kind: 'complete',
      status: 200,
      accepted: saved,
    });
    const coordinator = createMutationCoordinator({
      api: { dispatch } as never,
      store,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => token.value),
        authState: computed(() => 'authenticated'),
        refresh,
      } as never,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'attempt-1',
        delay: async () => {},
      },
    });

    await coordinator.flush(accepted.metadata.id);

    expect(refresh).toHaveBeenCalledOnce();
    expect(dispatch).toHaveBeenCalledWith(expect.any(Object), 'csrf-new');
    expect(store.recordFor(accepted.metadata.id)!.pending).toEqual([]);
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('leaves work queued when one csrf refresh still has no token', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(
      accepted.metadata.id,
      captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      ),
    );
    const refresh = vi.fn().mockResolvedValue(undefined);
    const dispatch = vi.fn();
    const coordinator = createMutationCoordinator({
      api: { dispatch } as never,
      store,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => null),
        authState: computed(() => 'authenticated'),
        refresh,
      } as never,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'attempt-1',
        delay: async () => {},
      },
    });

    await coordinator.flush(accepted.metadata.id);

    expect(refresh).toHaveBeenCalledOnce();
    expect(dispatch).not.toHaveBeenCalled();
    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      attempt: null,
      pending: [{ id: 'command-1' }],
    });
  });

  // eslint-disable-next-line max-len
  it('retries csrf with the same frozen attempt and only a fresh token', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const saved = {
      ...accepted,
      metadata: { ...accepted.metadata, title: 'Ada' },
      revision: parseRevision('2'),
    };
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(
      accepted.metadata.id,
      captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      ),
    );
    const token = ref('csrf-old');
    const api = {
      dispatch: vi
        .fn()
        .mockResolvedValueOnce({ kind: 'csrf-rejected' })
        .mockResolvedValueOnce({
          kind: 'complete',
          status: 200,
          accepted: saved,
        }),
    } as never;
    const coordinator = createMutationCoordinator({
      api,
      store,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => token.value),
        authState: computed(() => 'authenticated'),
        refresh: vi.fn(async () => {
          token.value = 'csrf-new';
        }),
      } as never,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'attempt-1',
        delay: async () => {},
      },
    });

    await coordinator.flush(accepted.metadata.id);

    const calls = (api as { dispatch: ReturnType<typeof vi.fn> }).dispatch.mock
      .calls;
    expect(calls).toHaveLength(2);
    expect(calls[0]![0]).toEqual(calls[1]![0]);
    expect(calls.map(([, csrf]) => csrf)).toEqual(['csrf-old', 'csrf-new']);
    expect(store.recordFor(accepted.metadata.id)!.pending).toEqual([]);
  });

  it('returns an explicit opaque create after the 23-hour cutoff', async () => {
    let now = 0;
    const api = {
      dispatch: vi.fn(async () => {
        now = 23 * 60 * 60 * 1_000;
        return { kind: 'unknown', reason: 'transport' };
      }),
      list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
    } as never;
    const coordinator = createMutationCoordinator({
      api,
      store: { recordFor: () => undefined } as never,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => 'csrf-1'),
        authState: computed(() => 'authenticated'),
      } as never,
      runtime: {
        nowEpochMs: () => now,
        uuid: () => 'create-key',
        delay: async () => {},
      },
    });
    const intent = {
      kind: 'resumeCreate' as const,
      id: 'create-1',
      ownerId: 'owner-1',
      sequence: 1,
      title: 'Ada',
    };

    await expect(coordinator.createResume(intent)).resolves.toMatchObject({
      kind: 'opaque-create',
      outcome: { kind: 'create-cutoff', intent, refreshedItems: [] },
    });
    expect(
      (api as { dispatch: ReturnType<typeof vi.fn> }).dispatch,
    ).toHaveBeenCalledOnce();
  });

  it('holds a frozen create after its one replay until cutoff', async () => {
    let now = 0;
    const api = {
      // eslint-disable-next-line max-len
      dispatch: vi.fn().mockResolvedValue({ kind: 'unknown', reason: 'server' }),
      list: vi.fn().mockResolvedValue({ kind: 'ready', items: [] }),
    } as never;
    const coordinator = createMutationCoordinator({
      api,
      store: { recordFor: () => undefined } as never,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => 'csrf-1'),
        authState: computed(() => 'authenticated'),
      } as never,
      runtime: {
        nowEpochMs: () => now,
        uuid: () => 'create-key',
        delay: async () => {},
      },
    });
    const intent = {
      kind: 'resumeCreate' as const,
      id: 'create-1',
      ownerId: 'owner-1',
      sequence: 1,
      title: 'Ada',
    };

    await expect(coordinator.createResume(intent)).resolves.toEqual({
      kind: 'blocked', intentId: intent.id, reason: 'unknown',
    });
    expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
      .toHaveBeenCalledTimes(2);
    await expect(coordinator.retryCreate(intent.id)).resolves.toEqual({
      kind: 'blocked', intentId: intent.id, reason: 'unknown',
    });
    // eslint-disable-next-line max-len
    expect((api as { list: ReturnType<typeof vi.fn> }).list).not.toHaveBeenCalled();

    now = 23 * 60 * 60 * 1_000;
    await expect(coordinator.retryCreate(intent.id)).resolves.toMatchObject({
      kind: 'opaque-create', outcome: { kind: 'create-cutoff', intent },
    });
    expect((api as { list: ReturnType<typeof vi.fn> }).list)
      .toHaveBeenCalledOnce();
  });

  it.each(['anonymous', 'error'] as const)(
    'retains queued work without a write when auth is %s',
    async (authState) => {
      setActivePinia(createPinia());
      const accepted = acceptedFixture();
      const store = useResumeStore();
      store.initialize(accepted);
      store.enqueue(
        accepted.metadata.id,
        captureCommand(
          accepted,
          {
            resumeId: accepted.metadata.id,
            ownerId: 'owner-1',
            sequence: 1,
            dependencyIds: [],
            intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
          },
          // eslint-disable-next-line max-len
          { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
        ),
      );
      const api = { dispatch: vi.fn() } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'stale-csrf'),
          authState: computed(() => authState),
        } as never,
        // eslint-disable-next-line max-len
        runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
      });

      await coordinator.flush(accepted.metadata.id);

      expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
        .not.toHaveBeenCalled();
      expect(store.recordFor(accepted.metadata.id)!.pending).toHaveLength(1);
    },
  );

  // eslint-disable-next-line max-len
  it('retains queued work when another account owns the stale token', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(
      accepted.metadata.id,
      captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      ),
    );
    const api = { dispatch: vi.fn() } as never;
    const coordinator = createMutationCoordinator({
      api,
      store,
      auth: {
        user: computed(() => ({ id: 'owner-2' })),
        csrfToken: computed(() => 'stale-csrf'),
        authState: computed(() => 'authenticated'),
      } as never,
      // eslint-disable-next-line max-len
      runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
    });

    await coordinator.flush(accepted.metadata.id);

    expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
      .not.toHaveBeenCalled();
    expect(store.recordFor(accepted.metadata.id)!.pending).toHaveLength(1);
  });

  it('reads a complete winner before dispatching after a bodyless child ack',
    async () => {
      setActivePinia(createPinia());
      const base = acceptedFixture();
      const accepted = {
        ...base,
        document: {
          ...base.document,
          content: {
            profile: {
              sectionType: 'profile' as const,
              entries: [{ id: 'entry-1', text: 'Existing' }],
            },
          },
          customization: {
            ...base.document.customization,
            layout: {
              ...base.document.customization.layout,
              sections: { main: ['profile'], sidebar: [] },
            },
          },
        },
      };
      const complete = {
        ...accepted,
        document: {
          ...accepted.document,
          content: {
            profile: { sectionType: 'profile' as const, entries: [] },
          },
        },
        revision: parseRevision('2'),
      };
      const store = useResumeStore();
      store.initialize(accepted);
      const deleted = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          // eslint-disable-next-line max-len
          intent: { kind: 'entryDelete', sectionKey: 'profile', entryId: 'entry-1' },
        },
        { nowEpochMs: () => 0, uuid: () => 'delete-1', delay: async () => {} },
      );
      const later = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 2,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'later-1', delay: async () => {} },
      );
      store.enqueue(accepted.metadata.id, deleted);
      store.enqueue(accepted.metadata.id, later);
      const api = {
        dispatch: vi.fn()
          .mockResolvedValueOnce({
            kind: 'child-ack', status: 204, scope: 'entry', etag: '"r2"',
          })
          .mockResolvedValueOnce({ kind: 'validation-rejected', issues: [] }),
        // eslint-disable-next-line max-len
        read: vi.fn().mockResolvedValue({ kind: 'complete', accepted: complete }),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'csrf-1'),
          authState: computed(() => 'authenticated'),
        } as never,
        // eslint-disable-next-line max-len
        runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
      });

      await coordinator.flush(accepted.metadata.id);

      const dispatch = (api as { dispatch: ReturnType<typeof vi.fn> }).dispatch;
      const read = (api as { read: ReturnType<typeof vi.fn> }).read;
      expect(read).toHaveBeenCalledOnce();
      expect(read.mock.invocationCallOrder[0])
        .toBeLessThan(dispatch.mock.invocationCallOrder[1]!);
    },
  );

  it('treats revision 10 as newer than revision 9 during stale rebase',
    async () => {
      setActivePinia(createPinia());
      const accepted = acceptedFixture({ revision: parseRevision('9') });
      const store = useResumeStore();
      store.initialize(accepted);
      const command = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: {
            kind: 'personalField', path: 'headline',
            value: { present: true, value: 'Ada' },
          },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      );
      store.enqueue(accepted.metadata.id, command);
      const api = {
        dispatch: vi.fn()
          .mockResolvedValueOnce({
            kind: 'stale', status: 412,
            // eslint-disable-next-line max-len
            winner: { document: accepted.document, revision: parseRevision('10') },
          })
          .mockResolvedValueOnce({ kind: 'validation-rejected', issues: [] }),
        read: vi.fn(),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'csrf-1'),
          authState: computed(() => 'authenticated'),
        } as never,
        // eslint-disable-next-line max-len
        runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
      });

      await coordinator.flush(accepted.metadata.id);

      expect((api as { read: ReturnType<typeof vi.fn> }).read)
        .not.toHaveBeenCalled();
      expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
        .toHaveBeenCalledTimes(2);
    },
  );

  it('completes a stale-winner read before continuing after accept latest',
    async () => {
      setActivePinia(createPinia());
      const base = acceptedFixture();
      const accepted = {
        ...base,
        document: {
          ...base.document,
          content: {
            work: {
              sectionType: 'work' as const,
              entries: [{ id: 'entry-1', jobTitle: 'Original' }],
            },
          },
          customization: {
            ...base.document.customization,
            layout: {
              ...base.document.customization.layout,
              sections: { main: ['work'], sidebar: [] },
            },
          },
        },
      };
      const winner = {
        ...accepted,
        document: {
          ...accepted.document,
          content: {
            work: { sectionType: 'work' as const, entries: [] },
          },
        },
        revision: parseRevision('2'),
      };
      const store = useResumeStore();
      store.initialize(accepted);
      store.enqueue(
        accepted.metadata.id,
        captureCommand(
          accepted,
          {
            resumeId: accepted.metadata.id,
            ownerId: 'owner-1',
            sequence: 1,
            dependencyIds: [],
            intent: {
              kind: 'entryField',
              sectionKey: 'work',
              entryId: 'entry-1',
              path: 'jobTitle',
              value: { present: true, value: 'Local' },
            },
          },
          // eslint-disable-next-line max-len
          { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
        ),
      );
      const api = {
        dispatch: vi.fn()
          .mockResolvedValueOnce({
            kind: 'stale',
            status: 412,
            winner: { document: winner.document, revision: winner.revision },
          })
          .mockResolvedValueOnce({
            kind: 'complete',
            status: 200,
            accepted: {
              ...winner,
              document: {
                ...winner.document,
                content: {
                  work: {
                    sectionType: 'work' as const,
                    entries: [{ id: 'entry-2' }],
                  },
                },
              },
              revision: parseRevision('3'),
            },
          }),
        read: vi.fn().mockResolvedValue({ kind: 'complete', accepted: winner }),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'csrf-1'),
          authState: computed(() => 'authenticated'),
        } as never,
        // eslint-disable-next-line max-len
        runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
      });

      await coordinator.flush(accepted.metadata.id);

      expect(store.recordFor(accepted.metadata.id)?.conflicts).toMatchObject([
        { kind: 'membership-changed', command: { kind: 'entryField' } },
      ]);
      expect(store.saveStateFor(accepted.metadata.id)).toBe('conflict');
      const conflictId
        = store.recordFor(accepted.metadata.id)!.conflicts[0]!.id;
      const accepting = coordinator.acceptLatest(
        accepted.metadata.id,
        conflictId,
      );
      store.enqueue(
        accepted.metadata.id,
        captureCommand(
          store.recordFor(accepted.metadata.id)!.current,
          {
            resumeId: accepted.metadata.id,
            ownerId: 'owner-1',
            sequence: 2,
            dependencyIds: [],
            intent: {
              kind: 'entryUpsert',
              sectionKey: 'work',
              entry: { id: 'entry-2' },
            },
          },
          // eslint-disable-next-line max-len
          { nowEpochMs: () => 0, uuid: () => 'command-2', delay: async () => {} },
        ),
      );

      await accepting;

      expect((api as { read: ReturnType<typeof vi.fn> }).read)
        .toHaveBeenCalledOnce();
      expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
        .toHaveBeenCalledTimes(2);
      expect(store.recordFor(accepted.metadata.id)).toMatchObject({
        completeReadRequired: false,
        conflicts: [],
        pending: [],
        attempt: null,
        accepted: { revision: parseRevision('3') },
      });
    },
  );

  it('clears the stale-winner read barrier before applying mine', async () => {
    setActivePinia(createPinia());
    const base = acceptedFixture();
    const accepted = {
      ...base,
      document: {
        ...base.document,
        content: {
          work: {
            sectionType: 'work' as const,
            entries: [{ id: 'entry-1' }, { id: 'entry-2' }],
          },
        },
        customization: {
          ...base.document.customization,
          layout: {
            ...base.document.customization.layout,
            sections: { main: ['work'], sidebar: [] },
          },
        },
      },
    };
    const winner = {
      ...accepted,
      document: {
        ...accepted.document,
        content: {
          work: {
            sectionType: 'work' as const,
            entries: [{ id: 'entry-1' }],
          },
        },
      },
      revision: parseRevision('2'),
    };
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(
      accepted.metadata.id,
      captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: {
            kind: 'entryReorder',
            sectionKey: 'work',
            entryIds: ['entry-2', 'entry-1'],
          },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      ),
    );
    const api = {
      dispatch: vi.fn().mockResolvedValue({
        kind: 'stale',
        status: 412,
        winner: { document: winner.document, revision: winner.revision },
      }),
      read: vi.fn().mockResolvedValue({ kind: 'complete', accepted: winner }),
    } as never;
    const coordinator = createMutationCoordinator({
      api,
      store,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => 'csrf-1'),
        authState: computed(() => 'authenticated'),
      } as never,
      // eslint-disable-next-line max-len
      runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
    });

    await coordinator.flush(accepted.metadata.id);
    const conflictId
      = store.recordFor(accepted.metadata.id)!.conflicts[0]!.id;
    await coordinator.applyMine(accepted.metadata.id, conflictId, {
      kind: 'reorder',
      members: ['entry-1'],
    });
    await coordinator.flush(accepted.metadata.id);

    expect((api as { read: ReturnType<typeof vi.fn> }).read)
      .toHaveBeenCalledOnce();
    expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
      .toHaveBeenCalledOnce();
    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      completeReadRequired: false,
      conflicts: [],
      pending: [],
      attempt: null,
      accepted: { revision: parseRevision('2') },
    });
  });

  it.each([
    [
      'same target',
      {
        kind: 'personalField' as const,
        path: 'headline' as const,
        value: { present: true as const, value: 'Newer local value' },
      },
    ],
    [
      'different target',
      {
        kind: 'metadataField' as const,
        field: 'title' as const,
        value: 'Newer title',
      },
    ],
  ])('keeps a %s edit made during the apply-mine read',
    async (_case, newerIntent) => {
      setActivePinia(createPinia());
      const accepted = acceptedFixture();
      const localIntent = {
        kind: 'personalField' as const,
        path: 'headline' as const,
        value: { present: true as const, value: 'Local conflict value' },
      };
      const winnerSnapshot = applyIntent(accepted, {
        ...localIntent,
        value: { present: true, value: 'Remote winner' },
      });
      const winner = {
        ...accepted,
        ...winnerSnapshot,
        revision: parseRevision('2'),
      };
      const mineSnapshot = applyIntent(winner, localIntent);
      const mine = {
        ...winner,
        ...mineSnapshot,
        revision: parseRevision('3'),
      };
      const newestSnapshot = applyIntent(mine, newerIntent);
      const newest = {
        ...mine,
        ...newestSnapshot,
        revision: parseRevision('4'),
      };
      const store = useResumeStore();
      store.initialize(accepted);
      const conflictCommand = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: localIntent,
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      );
      store.enqueue(accepted.metadata.id, conflictCommand);
      let releaseRead = (): void => {};
      const readGate = new Promise<void>((resolve) => {
        releaseRead = resolve;
      });
      const api = {
        dispatch: vi.fn()
          .mockResolvedValueOnce({
            kind: 'stale',
            status: 412,
            winner: { document: winner.document, revision: winner.revision },
          })
          .mockResolvedValueOnce({
            kind: 'complete', status: 200, accepted: mine,
          })
          .mockResolvedValueOnce({
            kind: 'complete', status: 200, accepted: newest,
          }),
        read: vi.fn(async () => {
          await readGate;
          return { kind: 'complete', accepted: winner };
        }),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => ({ id: 'owner-1' })),
          csrfToken: computed(() => 'csrf-1'),
          authState: computed(() => 'authenticated'),
        } as never,
        runtime: {
          nowEpochMs: () => 0,
          uuid: () => 'attempt-1',
          delay: async () => {},
        },
      });

      await coordinator.flush(accepted.metadata.id);
      const applying = coordinator.applyMine(
        accepted.metadata.id,
        conflictCommand.id,
        { kind: 'field' },
      );
      await vi.waitFor(() => {
        expect((api as { read: ReturnType<typeof vi.fn> }).read)
          .toHaveBeenCalledOnce();
      });
      store.enqueue(
        accepted.metadata.id,
        captureCommand(
          store.recordFor(accepted.metadata.id)!.current,
          {
            resumeId: accepted.metadata.id,
            ownerId: 'owner-1',
            sequence: 2,
            dependencyIds: [conflictCommand.id],
            intent: newerIntent,
          },
          // eslint-disable-next-line max-len
          { nowEpochMs: () => 1, uuid: () => 'command-2', delay: async () => {} },
        ),
      );
      releaseRead();

      await applying;
      await coordinator.flush(accepted.metadata.id);

      expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
        .toHaveBeenCalledTimes(3);
      expect(store.recordFor(accepted.metadata.id)).toMatchObject({
        accepted: newest,
        attempt: null,
        conflicts: [],
        pending: [],
      });
    },
  );

  it.each([
    ['anonymous', 'owner-1', true],
    ['authenticated', 'owner-2', true],
    ['error', undefined, false],
  ] as const)(
    'does not re-dispatch a retained attempt as %s user %s',
    async (authState, userId, sessionLost) => {
      setActivePinia(createPinia());
      const accepted = acceptedFixture();
      const store = useResumeStore();
      store.initialize(accepted);
      const command = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      );
      store.enqueue(accepted.metadata.id, command);
      const currentState = ref<'authenticated' | 'anonymous' | 'error'>(
        'authenticated',
      );
      const currentUser = ref<string | undefined>('owner-1');
      const api = {
        dispatch: vi.fn().mockResolvedValue({
          kind: 'rate-limited', retryAfterMs: null,
        }),
      } as never;
      const coordinator = createMutationCoordinator({
        api,
        store,
        auth: {
          user: computed(() => {
            if (currentUser.value === undefined) return null;
            return { id: currentUser.value };
          }),
          csrfToken: computed(() => 'stale-csrf'),
          authState: computed(() => currentState.value),
        } as never,
        // eslint-disable-next-line max-len
        runtime: { nowEpochMs: () => 0, uuid: () => 'attempt-1', delay: async () => {} },
      });

      await coordinator.flush(accepted.metadata.id);
      currentState.value = authState;
      currentUser.value = userId;
      await coordinator.retry(accepted.metadata.id, command.id);

      expect((api as { dispatch: ReturnType<typeof vi.fn> }).dispatch)
        .toHaveBeenCalledOnce();
      expect(store.recordFor(accepted.metadata.id)).toMatchObject({
        sessionLost,
        attempt: { command },
      });
    },
  );

  // eslint-disable-next-line max-len
  it('retains a session-lost attempt until authenticated complete-read recovery', async () => {
    setActivePinia(createPinia());
    const accepted = acceptedFixture();
    const saved = {
      ...accepted,
      metadata: { ...accepted.metadata, title: 'Ada' },
      revision: parseRevision('2'),
    };
    const store = useResumeStore();
    store.initialize(accepted);
    store.enqueue(
      accepted.metadata.id,
      captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 1,
          dependencyIds: [],
          intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
        },
        { nowEpochMs: () => 0, uuid: () => 'command-1', delay: async () => {} },
      ),
    );
    const api = {
      dispatch: vi
        .fn()
        .mockResolvedValueOnce({ kind: 'session-lost' })
        .mockResolvedValueOnce({
          kind: 'complete',
          status: 200,
          accepted: saved,
        }),
      read: vi.fn().mockResolvedValue({ kind: 'complete', accepted: saved }),
    } as never;
    const coordinator = createMutationCoordinator({
      api,
      store,
      auth: {
        user: computed(() => ({ id: 'owner-1' })),
        csrfToken: computed(() => 'csrf-1'),
        authState: computed(() => 'authenticated'),
        refresh: vi.fn(),
      } as never,
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'attempt-1',
        delay: async () => {},
      },
    });

    await coordinator.flush(accepted.metadata.id);
    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      sessionLost: true,
      pending: [],
      attempt: { attempt: { idempotencyKey: 'attempt-1' } },
    });
    await coordinator.resumeAfterAuth(accepted.metadata.id);

    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      sessionLost: false,
      pending: [],
      attempt: null,
    });
  });
});
