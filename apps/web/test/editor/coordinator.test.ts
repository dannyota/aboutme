import { TEMPLATES } from '@aboutme/schema/templates';
import { createPinia, setActivePinia } from 'pinia';
import { computed, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import { captureCommand } from '../../app/editor/commands';
import { parseRevision } from '../../app/editor/revision';
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
