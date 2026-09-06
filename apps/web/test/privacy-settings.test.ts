import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';

import PrivacySettings from '../app/components/settings/PrivacySettings.vue';
import {
  createAccountExportController,
  MAX_ACCOUNT_EXPORT_BYTES,
  mapAccountDeletionError,
  PrivacySettingsActionsKey,
  PrivacySettingsFailure,
  type PrivacySettingsActions,
} from '../app/composables/privacySettings';
import { PasswordSettingsFailure } from '../app/composables/passwordSettings';

function actionsFor(
  overrides: Partial<PrivacySettingsActions> = {},
): PrivacySettingsActions {
  return {
    exportAccount: vi.fn(async () => ({ kind: 'idle' })),
    deleteAccount: vi.fn(async () => {}),
    reauthenticate: vi.fn(async () => {}),
    startProviderReauth: vi.fn(async () => {}),
    ...overrides,
  };
}

function mountSettings(actions: PrivacySettingsActions) {
  return mount(PrivacySettings, {
    attachTo: document.body,
    props: { hasPassword: true, providers: ['google'] },
    global: { provide: { [PrivacySettingsActionsKey]: actions } },
  });
}

async function openDelete(wrapper: ReturnType<typeof mount>): Promise<void> {
  await wrapper.get('[data-testid="account-delete-action"]').trigger('click');
  await flushPromises();
  const input = document.body.querySelector<HTMLInputElement>(
    '[role="alertdialog"] input',
  );
  expect(input).not.toBeNull();
  input!.value = 'DELETE';
  input!.dispatchEvent(new Event('input', { bubbles: true }));
  await flushPromises();
}

function confirmDelete(): void {
  const button = document.body.querySelector<HTMLButtonElement>(
    '[data-action="confirm-account-delete"]',
  );
  expect(button).not.toBeNull();
  button!.click();
}

describe('PrivacySettings', () => {
  it(
    'discloses the deletion schedule and cancellation makes no request',
    async () => {
      const actions = actionsFor();
      const wrapper = mountSettings(actions);

      expect(wrapper.text()).toContain('Download your data');
      await wrapper
        .get('[data-testid="account-delete-action"]')
        .trigger('click');
      await flushPromises();
      expect(document.body.textContent).toContain('Access ends immediately.');
      expect(document.body.textContent).toContain(
        'Private-media removal targets 24 hours.',
      );
      expect(document.body.textContent).toContain(
        'Backup copies expire on the 30-day schedule.',
      );

      (
        document.body.querySelector(
          '[data-action="cancel-account-delete"]',
        ) as HTMLButtonElement
      ).click();
      await flushPromises();
      expect(actions.deleteAccount).not.toHaveBeenCalled();
      wrapper.unmount();
    },
  );

  it(
    'disables duplicate deletion while the confirmed request is pending',
    async () => {
      let release!: () => void;
      const waiting = new Promise<void>((resolve) => {
        release = resolve;
      });
      const deleteAccount = vi.fn(async () => waiting);
      const wrapper = mountSettings(actionsFor({ deleteAccount }));

      await openDelete(wrapper);
      confirmDelete();
      await flushPromises();
      confirmDelete();
      await flushPromises();

      expect(deleteAccount).toHaveBeenCalledOnce();
      expect(
        document.body
          .querySelector('[role="alertdialog"]')
          ?.getAttribute('aria-busy'),
      ).toBe('true');
      release();
      await flushPromises();
      wrapper.unmount();
    },
  );

  it(
    'requires fresh explicit confirmation after password reauthentication',
    async () => {
      const deleteAccount = vi
        .fn()
        .mockRejectedValueOnce(new PrivacySettingsFailure('reauth-required'))
        .mockResolvedValueOnce(undefined);
      const reauthenticate = vi.fn(async () => {});
      const wrapper = mountSettings(
        actionsFor({ deleteAccount, reauthenticate }),
      );

      await openDelete(wrapper);
      confirmDelete();
      await flushPromises();
      expect(
        wrapper.get('[data-testid="account-delete-reauth-password"]').exists(),
      ).toBe(true);
      expect(deleteAccount).toHaveBeenCalledOnce();

      await wrapper
        .get('#account-delete-current-password')
        .setValue('current-secret');
      await wrapper
        .get('[data-testid="account-delete-reauth-password"]')
        .trigger('submit');
      await flushPromises();
      expect(reauthenticate).toHaveBeenCalledWith('current-secret');
      expect(deleteAccount).toHaveBeenCalledOnce();

      const repeatInput = document.body.querySelector<HTMLInputElement>(
        '[role="alertdialog"] input',
      );
      repeatInput!.value = 'DELETE';
      repeatInput!.dispatchEvent(new Event('input', { bubbles: true }));
      await flushPromises();
      confirmDelete();
      await flushPromises();
      expect(deleteAccount).toHaveBeenCalledTimes(2);
      expect(wrapper.emitted('deleted')).toHaveLength(1);
      wrapper.unmount();
    },
  );

  it('does not retry deletion after failed reauthentication', async () => {
    const deleteAccount = vi.fn(async () => {
      throw new PrivacySettingsFailure('reauth-required');
    });
    const wrapper = mountSettings(
      actionsFor({
        deleteAccount,
        reauthenticate: vi.fn(async () => {
          throw new PasswordSettingsFailure('reauth-failed');
        }),
      }),
    );

    await openDelete(wrapper);
    confirmDelete();
    await flushPromises();
    await wrapper.get('#account-delete-current-password').setValue('wrong');
    await wrapper
      .get('[data-testid="account-delete-reauth-password"]')
      .trigger('submit');
    await flushPromises();

    expect(wrapper.text()).toContain('Incorrect password.');
    expect(deleteAccount).toHaveBeenCalledOnce();
    wrapper.unmount();
  });

  it.each([
    ['session-required', 'Your session ended. Sign in again.'],
    ['account-changed', 'Your account changed. Review it and try again.'],
    ['rate-limited', 'Too many attempts. Try again later.'],
    ['unavailable', 'Account deletion is temporarily unavailable. Try again.'],
  ] as const)('shows fixed %s deletion error copy', async (kind, copy) => {
    const wrapper = mountSettings(
      actionsFor({
        deleteAccount: vi.fn(async () => {
          throw new PrivacySettingsFailure(kind);
        }),
      }),
    );
    await openDelete(wrapper);
    confirmDelete();
    await flushPromises();
    expect(wrapper.get('[data-testid="account-delete-error"]').text()).toBe(
      copy,
    );
    wrapper.unmount();
  });
});

describe('account export download', () => {
  it(
    'downloads bounded JSON as aboutme-export.json and always revokes its URL',
    async () => {
      const createObjectURL = vi.fn(() => 'blob:account-export');
      const revokeObjectURL = vi.fn();
      const download = vi.fn();
      const fetcher = vi.fn(async () =>
        jsonResponse([new Uint8Array([123, 125])]),
      );
      const controller = createAccountExportController({
        fetcher,
        createObjectURL,
        revokeObjectURL,
        download,
      });

      await controller.download();

      expect(download).toHaveBeenCalledWith(
        'blob:account-export',
        'aboutme-export.json',
      );
      expect(revokeObjectURL).toHaveBeenCalledWith('blob:account-export');
      expect(fetcher).toHaveBeenCalledWith('/api/v1/me/export', {
        credentials: 'same-origin',
        method: 'GET',
        signal: expect.any(AbortSignal),
      });
      expect(controller.state.value).toEqual({ kind: 'idle' });
    },
  );

  it.each([
    () => new Response('x', { status: 500 }),
    () =>
      new Response('x', {
        status: 200,
        headers: { 'Content-Type': 'text/plain' },
      }),
    () =>
      new Response('x', {
        status: 200,
        headers: jsonHeaders({
          'Content-Length': String(MAX_ACCOUNT_EXPORT_BYTES + 1),
        }),
      }),
  ])(
    'rejects invalid export responses without diagnostic data',
    async (response) => {
      const download = vi.fn();
      const controller = createAccountExportController({
        fetcher: async () => response(),
        createObjectURL: vi.fn(() => 'blob:account-export'),
        revokeObjectURL: vi.fn(),
        download,
      });

      await controller.download();

      expect(download).not.toHaveBeenCalled();
      expect(controller.state.value).toEqual({
        kind: 'error',
        message: 'Could not export your data. Try again.',
      });
    },
  );

  it('aborts and remains idle when disposed during an export', async () => {
    let signal: AbortSignal | undefined;
    const controller = createAccountExportController({
      fetcher: (_input, init) => {
        signal = init?.signal;
        return Promise.reject(new DOMException('Aborted', 'AbortError'));
      },
      createObjectURL: vi.fn(() => 'blob:account-export'),
      revokeObjectURL: vi.fn(),
      download: vi.fn(),
    });

    const pending = controller.download();
    await Promise.resolve();
    controller.dispose();
    await pending;
    expect(signal?.aborted).toBe(true);
    expect(controller.state.value).toEqual({ kind: 'idle' });
  });

  it(
    'discards a late response after disposal without creating a download',
    async () => {
      let resolveResponse!: (response: Response) => void;
      const createObjectURL = vi.fn(() => 'blob:account-export');
      const download = vi.fn();
      const controller = createAccountExportController({
        fetcher: () => new Promise<Response>((resolve) => {
          resolveResponse = resolve;
        }),
        createObjectURL,
        revokeObjectURL: vi.fn(),
        download,
      });

      const pending = controller.download();
      controller.dispose();
      resolveResponse(jsonResponse([new Uint8Array([123, 125])]));
      await pending;

      expect(createObjectURL).not.toHaveBeenCalled();
      expect(download).not.toHaveBeenCalled();
      expect(controller.state.value).toEqual({ kind: 'idle' });
    },
  );

  it(
    'does not save partial bytes when disposal cancels a pending reader read',
    async () => {
      let resolvePull!: () => void;
      let pullStarted!: () => void;
      const started = new Promise<void>((resolve) => {
        pullStarted = resolve;
      });
      const response = new Response(new ReadableStream<Uint8Array>({
        start(stream) {
          stream.enqueue(new Uint8Array([123]));
        },
        pull() {
          pullStarted();
          return new Promise<void>((resolve) => {
            resolvePull = resolve;
          });
        },
        cancel() {
          resolvePull();
        },
      }), { status: 200, headers: jsonHeaders() });
      const createObjectURL = vi.fn(() => 'blob:account-export');
      const download = vi.fn();
      const controller = createAccountExportController({
        fetcher: async () => response,
        createObjectURL,
        revokeObjectURL: vi.fn(),
        download,
      });

      const pending = controller.download();
      await started;
      controller.dispose();
      await pending;

      expect(createObjectURL).not.toHaveBeenCalled();
      expect(download).not.toHaveBeenCalled();
      expect(controller.state.value).toEqual({ kind: 'idle' });
    },
  );

  it.each([
    [429, 'Your export is temporarily unavailable. Try again.'],
    [503, 'Your export is temporarily unavailable. Try again.'],
    [401, 'Your session ended. Sign in again.'],
  ])('maps export status %i to fixed copy', async (status, message) => {
    const controller = createAccountExportController({
      fetcher: async () => new Response('server-secret-value', { status }),
      createObjectURL: vi.fn(() => 'blob:account-export'),
      revokeObjectURL: vi.fn(),
      download: vi.fn(),
    });

    await controller.download();

    expect(controller.state.value).toEqual({ kind: 'error', message });
    expect(JSON.stringify(controller.state.value)).not.toContain(
      'server-secret-value',
    );
  });
});

describe('account deletion error mapping', () => {
  it.each([
    [401, 'session_required', 'session-required'],
    [409, 'account_changed', 'account-changed'],
    [429, 'rate_limited', 'rate-limited'],
    [503, 'account_unavailable', 'unavailable'],
  ] as const)(
    'maps HTTP %i %s without exposing the server message',
    (status, code, kind) => {
      const failure = mapAccountDeletionError({
        statusCode: status,
        data: { error: { code, message: 'database host secret' } },
      });
      expect(failure.kind).toBe(kind);
      expect(failure.message).not.toContain('database host secret');
    },
  );
});

function jsonResponse(chunks: readonly Uint8Array[]): Response {
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(chunk);
        controller.close();
      },
    }),
    { status: 200, headers: jsonHeaders() },
  );
}

function jsonHeaders(extra: Record<string, string> = {}): Headers {
  return new Headers({
    'Cache-Control': 'no-store, no-transform',
    'Content-Type': 'application/json',
    ...extra,
  });
}
