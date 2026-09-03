import { createPinia, setActivePinia } from 'pinia';
import { computed, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import { createPublishController } from '../../app/editor/publishController';
import type { PublishCommand } from '../../app/editor/publishApi';
import { useResumeStore } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';
import { parseRevision } from '../../app/editor/revision';

function setup() {
  setActivePinia(createPinia());
  const accepted = acceptedFixture();
  const store = useResumeStore();
  store.initialize(accepted);
  const user = ref<{ id: string; hasPassword: boolean }>({
    id: 'owner-1',
    hasPassword: true,
  });
  const providerLogin = ref(false);
  const csrfToken = ref<string | null>('csrf-1');
  const authState = ref<'authenticated' | 'anonymous'>('authenticated');
  const identities = ref([{ provider: 'google' as const }]);
  const auth = {
    user: computed(() => user.value),
    csrfToken: computed(() => csrfToken.value),
    authState: computed(() => authState.value),
    refresh: vi.fn().mockResolvedValue(undefined),
    mutate: vi.fn(),
    identities,
  } as never;
  const coordinator = { flush: vi.fn().mockResolvedValue(undefined) } as never;
  const api = { dispatch: vi.fn() } as never;
  const controller = createPublishController({
    resumeId: accepted.metadata.id,
    store,
    coordinator,
    api,
    auth,
    runtime: {
      nowEpochMs: () => 0,
      uuid: () => 'publish-key',
      delay: async () => {},
    },
    providerLogin,
  });
  return {
    accepted,
    store,
    auth,
    coordinator,
    api,
    controller,
    user,
    providerLogin,
    csrfToken,
    authState,
    identities,
  };
}

const command: PublishCommand = {
  slug: 'ada-lovelace',
  live: true,
  downloadEnabled: true,
  seoGeoEnabled: false,
};

describe('publish controller', () => {
  it('flushes first and blocks when a command remains pending', async () => {
    const context = setup();
    context.store.enqueue(context.accepted.metadata.id, {
      kind: 'metadataField',
      id: 'cmd-1',
      ownerId: 'owner-1',
      resumeId: 'resume-1',
      sequence: 1,
      dependencyIds: [],
      field: 'title',
      value: 'Changed',
    });
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'blocked',
      reason: 'saving',
    });
    expect(context.api.dispatch).not.toHaveBeenCalled();
  });

  it('adopts accepted publish responses in the store', async () => {
    const context = setup();
    const published = {
      ...context.accepted,
      revision: parseRevision('2'),
      metadata: {
        ...context.accepted.metadata,
        live: true,
        slug: 'ada-lovelace',
      },
    };
    vi.mocked(context.api.dispatch).mockResolvedValue({
      kind: 'accepted',
      resume: published,
    });
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'accepted',
    });
    expect(context.store.recordFor('resume-1')?.accepted.metadata.live).toBe(
      true,
    );
  });

  it('adopts stale winner and never republishes automatically', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch).mockResolvedValue({
      kind: 'stale',
      winner: {
        document: context.accepted.document,
        revision: parseRevision('2'),
      },
    });
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'stale',
    });
    expect(context.api.dispatch).toHaveBeenCalledOnce();
  });

  it('keeps an uncertain attempt for explicit retry', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch)
      .mockResolvedValueOnce({ kind: 'unknown', reason: 'transport' })
      .mockResolvedValueOnce({ kind: 'failed', code: 'request_invalid' });
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'unknown',
    });
    await context.controller.retryUncertain();
    expect(context.api.dispatch).toHaveBeenCalledTimes(2);
  });

  it('rejects an obviously invalid slug before dispatch', async () => {
    const context = setup();
    await expect(
      context.controller.submit({ ...command, slug: 'Bad slug' }),
    ).resolves.toEqual({
      kind: 'invalid',
      issues: [{ path: 'slug', code: 'invalid_format' }],
    });
    expect(context.api.dispatch).not.toHaveBeenCalled();
  });

  it('replays the same attempt after password reauthentication', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch)
      .mockResolvedValueOnce({ kind: 'reauth-required' })
      .mockResolvedValueOnce({
        kind: 'accepted',
        resume: {
          ...context.accepted,
          revision: parseRevision('2'),
        },
      });
    vi.mocked(context.auth.mutate).mockResolvedValue(undefined);
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'reauth-required',
      method: 'password',
    });
    await expect(
      context.controller.reauthPassword('password'),
    ).resolves.toMatchObject({ kind: 'accepted' });
    expect(context.auth.mutate).toHaveBeenCalledWith(
      '/api/v1/auth/password/reauth',
      { method: 'POST', body: { password: 'password' } },
    );
    expect(context.api.dispatch).toHaveBeenCalledTimes(2);
  });

  it('keeps wrong password and rate-limit states distinguishable', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch).mockResolvedValue({
      kind: 'reauth-required',
    });
    await context.controller.submit(command);
    vi.mocked(context.auth.mutate).mockRejectedValueOnce({
      statusCode: 401,
      data: { error: { code: 'reauth_failed' } },
    });
    await expect(
      context.controller.reauthPassword('bad'),
    ).resolves.toMatchObject({ kind: 'reauth-wrong-password' });
    vi.mocked(context.auth.mutate).mockRejectedValueOnce({
      statusCode: 429,
      data: { error: { code: 'rate_limited' } },
    });
    await expect(
      context.controller.reauthPassword('bad'),
    ).resolves.toMatchObject({ kind: 'reauth-rate-limited' });
  });

  it('fails closed on mismatched reauth status and code pairs', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch).mockResolvedValue({
      kind: 'reauth-required',
    });
    await context.controller.submit(command);
    vi.mocked(context.auth.mutate).mockRejectedValueOnce({
      statusCode: 500,
      data: { error: { code: 'reauth_failed' } },
    });
    await expect(
      context.controller.reauthPassword('bad'),
    ).resolves.toMatchObject({ kind: 'reauth-unavailable' });

    vi.mocked(context.auth.mutate).mockRejectedValueOnce({
      statusCode: 400,
      data: { error: { code: 'rate_limited' } },
    });
    await expect(
      context.controller.reauthPassword('bad'),
    ).resolves.toMatchObject({ kind: 'reauth-unavailable' });
  });

  it('uses the first linked provider for explicit reauth', async () => {
    const context = setup();
    context.user.value = { id: 'owner-1', hasPassword: false };
    context.providerLogin.value = true;
    context.auth.identities.value = [
      { provider: 'github' },
      { provider: 'google' },
    ];
    vi.mocked(context.api.dispatch).mockResolvedValueOnce({
      kind: 'reauth-required',
    });
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'reauth-required',
      method: 'provider',
    });
    vi.mocked(context.auth.mutate).mockResolvedValue({
      data: {
        authorizeUrl: 'https://github.com/login/oauth/authorize?state=x',
      },
    });
    await expect(
      context.controller.startProviderReauth(),
    ).resolves.toMatchObject({
      kind: 'provider-started',
      authorizeUrl: 'https://github.com/login/oauth/authorize?state=x',
    });
    expect(context.auth.mutate).toHaveBeenCalledWith(
      '/api/v1/auth/github/start',
      { method: 'POST', query: { purpose: 'reauth' } },
    );
  });

  it('clears the attempt when the authenticated owner changes', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch).mockResolvedValue({
      kind: 'unknown',
      reason: 'transport',
    });
    await context.controller.submit(command);
    context.user.value = { id: 'other-owner', hasPassword: true };
    await expect(context.controller.retryUncertain()).resolves.toEqual({
      kind: 'session-lost',
    });
    expect(context.api.dispatch).toHaveBeenCalledOnce();
  });

  it('refuses cancellation while a publish request is in flight', async () => {
    const context = setup();
    let resolve!: (value: unknown) => void;
    vi.mocked(context.api.dispatch).mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }) as never,
    );
    const pending = context.controller.submit(command);
    await vi.waitFor(() => expect(context.api.dispatch).toHaveBeenCalledOnce());
    context.controller.cancel();
    expect(context.controller.state.value.kind).toBe('dispatching');
    resolve({ kind: 'accepted', resume: context.accepted });
    await pending;
    expect(context.controller.state.value.kind).toBe('accepted');
  });

  it('maps every unresolved editor condition before dispatch', async () => {
    const cases: Array<[string, (context: ReturnType<typeof setup>) => void]>
      = [
        [
          'conflict',
          ({ store, accepted }) => {
            store.recordFor(accepted.metadata.id)!.conflicts = [{}] as never;
          },
        ],
        [
          'session-lost',
          ({ store, accepted }) => {
            store.markSessionLost(accepted.metadata.id);
          },
        ],
        [
          'issue',
          ({ store, accepted }) => {
            store.setIssues(accepted.metadata.id, 'command-1', [
              { path: 'personalDetails.fullName', code: 'required' },
            ]);
          },
        ],
        [
          'partial-template',
          ({ store, accepted }) => {
            store.recordFor(accepted.metadata.id)!.templateState = {
              kind: 'partial',
            } as never;
          },
        ],
        [
          'opaque-photo',
          ({ store, accepted }) => {
            store.recordFor(accepted.metadata.id)!.opaquePhotoOutcome
              = {} as never;
          },
        ],
        [
          'read-required',
          ({ store, accepted }) => {
            store.recordFor(accepted.metadata.id)!.completeReadRequired = true;
          },
        ],
      ];
    for (const [reason, arrange] of cases) {
      const context = setup();
      arrange(context);
      await expect(context.controller.submit(command)).resolves.toMatchObject({
        kind: 'blocked',
        reason,
      });
      expect(context.api.dispatch).not.toHaveBeenCalled();
    }

    const missing = setup();
    missing.store.removeResume(missing.accepted.metadata.id);
    await expect(missing.controller.submit(command)).resolves.toMatchObject({
      kind: 'blocked',
      reason: 'not-loaded',
    });
  });

  it('fails closed when the editor flush rejects', async () => {
    const context = setup();
    vi.mocked(context.coordinator.flush).mockRejectedValue(new Error('flush'));
    await expect(context.controller.submit(command)).resolves.toEqual({
      kind: 'failed',
      code: 'save_failed',
    });
    expect(context.api.dispatch).not.toHaveBeenCalled();
  });

  it('refreshes CSRF once with the same attempt and token', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch)
      .mockResolvedValueOnce({ kind: 'csrf-rejected' })
      .mockResolvedValueOnce({
        kind: 'accepted',
        resume: {
          ...context.accepted,
          revision: parseRevision('2'),
        },
      });
    vi.mocked(context.auth.refresh).mockImplementation(async () => {
      context.csrfToken.value = 'csrf-2';
    });
    await expect(context.controller.submit(command)).resolves.toMatchObject({
      kind: 'accepted',
    });
    expect(context.api.dispatch).toHaveBeenCalledTimes(2);
    expect(context.api.dispatch.mock.calls[0]![0]).toBe(
      context.api.dispatch.mock.calls[1]![0],
    );
    expect(context.api.dispatch.mock.calls[0]![1]).toBe('csrf-1');
    expect(context.api.dispatch.mock.calls[1]![1]).toBe('csrf-2');
  });

  it('stops after a second CSRF rejection or a failed refresh', async () => {
    const twice = setup();
    vi.mocked(twice.api.dispatch).mockResolvedValue({ kind: 'csrf-rejected' });
    await expect(twice.controller.submit(command)).resolves.toEqual({
      kind: 'failed',
      code: 'csrf_rejected',
    });
    expect(twice.api.dispatch).toHaveBeenCalledTimes(2);
    expect(twice.auth.refresh).toHaveBeenCalledOnce();

    const failed = setup();
    vi.mocked(failed.api.dispatch).mockResolvedValue({ kind: 'csrf-rejected' });
    vi.mocked(failed.auth.refresh).mockRejectedValue(new Error('offline'));
    await expect(failed.controller.submit(command)).resolves.toEqual({
      kind: 'failed',
      code: 'csrf_rejected',
    });
    expect(failed.api.dispatch).toHaveBeenCalledOnce();
  });

  it('stops a CSRF replay when refresh changes the owner', async () => {
    const context = setup();
    vi.mocked(context.api.dispatch).mockResolvedValue({
      kind: 'csrf-rejected',
    });
    vi.mocked(context.auth.refresh).mockImplementation(async () => {
      context.user.value = { id: 'other-owner', hasPassword: true };
    });
    await expect(context.controller.submit(command)).resolves.toEqual({
      kind: 'session-lost',
    });
    expect(context.api.dispatch).toHaveBeenCalledOnce();
  });

  it(
    'marks the editor session lost after publish authentication loss',
    async () => {
      const context = setup();
      vi.mocked(context.api.dispatch).mockResolvedValue({
        kind: 'session-lost',
      });
      await expect(context.controller.submit(command)).resolves.toEqual({
        kind: 'session-lost',
      });
      expect(
        context.store.recordFor(context.accepted.metadata.id)?.sessionLost,
      ).toBe(true);
    },
  );

  it(
    'suppresses duplicate submit without replacing dispatch state',
    async () => {
      const context = setup();
      let resolve!: (value: unknown) => void;
      vi.mocked(context.api.dispatch).mockReturnValue(
        new Promise((done) => {
          resolve = done;
        }) as never,
      );
      const first = context.controller.submit(command);
      await vi.waitFor(() =>
        expect(context.api.dispatch).toHaveBeenCalledOnce(),
      );
      await expect(context.controller.submit(command)).resolves.toMatchObject({
        kind: 'dispatching',
      });
      expect(context.controller.state.value.kind).toBe('dispatching');
      resolve({
        kind: 'accepted',
        resume: {
          ...context.accepted,
          revision: parseRevision('2'),
        },
      });
      await first;
      expect(context.api.dispatch).toHaveBeenCalledOnce();
    },
  );

  it(
    'fails closed without an enabled provider for passwordless users',
    async () => {
      const disabled = setup();
      disabled.user.value = { id: 'owner-1', hasPassword: false };
      vi.mocked(disabled.api.dispatch).mockResolvedValue({
        kind: 'reauth-required',
      });
      await expect(disabled.controller.submit(command)).resolves.toEqual({
        kind: 'failed',
        code: 'provider_disabled',
      });

      const missing = setup();
      missing.user.value = { id: 'owner-1', hasPassword: false };
      missing.providerLogin.value = true;
      missing.identities.value = [];
      vi.mocked(missing.api.dispatch).mockResolvedValue({
        kind: 'reauth-required',
      });
      await expect(missing.controller.submit(command)).resolves.toEqual({
        kind: 'failed',
        code: 'provider_unavailable',
      });
    },
  );

  it(
    'keeps provider start failures closed without automatic publish',
    async () => {
      const context = setup();
      context.user.value = { id: 'owner-1', hasPassword: false };
      context.providerLogin.value = true;
      vi.mocked(context.api.dispatch).mockResolvedValue({
        kind: 'reauth-required',
      });
      await context.controller.submit(command);

      vi.mocked(context.auth.mutate).mockResolvedValueOnce({
        data: { authorizeUrl: 'https://evil.example/authorize' },
      });
      await expect(
        context.controller.startProviderReauth(),
      ).resolves.toMatchObject({ kind: 'provider-start-invalid' });
      expect(context.api.dispatch).toHaveBeenCalledOnce();

      context.controller.cancel();
      await context.controller.submit(command);
      vi.mocked(context.auth.mutate).mockRejectedValueOnce({
        statusCode: 429,
        data: { error: { code: 'rate_limited' } },
      });
      await expect(
        context.controller.startProviderReauth(),
      ).resolves.toMatchObject({ kind: 'provider-started-rate-limited' });
    },
  );

  it('explicitly retries the same attempt after provider reauth', async () => {
    const context = setup();
    context.user.value = { id: 'owner-1', hasPassword: false };
    context.providerLogin.value = true;
    vi.mocked(context.api.dispatch)
      .mockResolvedValueOnce({ kind: 'reauth-required' })
      .mockResolvedValueOnce({
        kind: 'accepted',
        resume: {
          ...context.accepted,
          revision: parseRevision('2'),
        },
      });
    await context.controller.submit(command);
    vi.mocked(context.auth.mutate).mockResolvedValue({
      data: {
        authorizeUrl: 'https://accounts.google.com/o/oauth2/v2/auth?state=x',
      },
    });
    await context.controller.startProviderReauth();
    expect(context.api.dispatch).toHaveBeenCalledOnce();
    await context.controller.retryAfterProviderReauth();
    expect(context.api.dispatch).toHaveBeenCalledTimes(2);
    expect(context.api.dispatch.mock.calls[0]![0]).toBe(
      context.api.dispatch.mock.calls[1]![0],
    );
  });

  it(
    'cancels retained attempts but refuses in-flight cancellation',
    async () => {
      const context = setup();
      vi.mocked(context.api.dispatch).mockResolvedValue({
        kind: 'unknown',
        reason: 'transport',
      });
      await context.controller.submit(command);
      context.controller.cancel();
      expect(context.controller.state.value).toEqual({ kind: 'idle' });
      await context.controller.retryUncertain();
      expect(context.api.dispatch).toHaveBeenCalledOnce();
    },
  );
});
