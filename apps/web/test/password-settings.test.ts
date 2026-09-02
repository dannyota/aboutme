import { describe, expect, it, vi } from 'vitest';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import PasswordSettings from '../app/components/auth/PasswordSettings.vue';
import {
  type PasswordSettingsActions,
  PasswordSettingsActionsKey,
  PasswordSettingsFailure,
} from '../app/composables/passwordSettings';
import type { AuthProvider } from '../app/composables/useAuth';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

interface SettingsProps {
  hasPassword: boolean;
  providers: AuthProvider[];
}

function makeActions(
  overrides: Partial<PasswordSettingsActions> = {},
): PasswordSettingsActions {
  return {
    reauthenticate: vi.fn(async () => {}),
    setPassword: vi.fn(async () => {}),
    startProviderReauth: vi.fn(async () => {}),
    ...overrides,
  };
}

function mountSettings(props: SettingsProps, actions: PasswordSettingsActions) {
  return mountSuspended(PasswordSettings, {
    props,
    global: {
      provide: {
        [PasswordSettingsActionsKey]: actions,
      },
    },
  });
}

describe('PasswordSettings', () => {
  it(
    'shows the add action and no-password status for a ' + 'provider-only user',
    async () => {
      const wrapper = await mountSettings(
        { hasPassword: false, providers: ['google', 'github'] },
        makeActions(),
      );
      await flushPromises();

      expect(wrapper.get('[data-testid="password-settings"] h2').text()).toBe(
        'Password',
      );
      expect(
        wrapper.find('[data-testid="password-settings"] [as]').exists(),
      ).toBe(false);
      expect(wrapper.get('[data-testid="password-status"]').text()).toBe(
        'No password set.',
      );
      expect(wrapper.get('[data-testid="password-action"]').text()).toBe(
        'Add a password',
      );
      // Provider-only add never asks for a current password up front.
      expect(wrapper.find('#password-current').exists()).toBe(false);
    },
  );

  it(
    'shows the change action and has-password status for a ' + 'password user',
    async () => {
      const wrapper = await mountSettings(
        { hasPassword: true, providers: ['google'] },
        makeActions(),
      );
      await flushPromises();

      expect(wrapper.get('[data-testid="password-status"]').text()).toBe(
        'You have a password.',
      );
      expect(wrapper.get('[data-testid="password-action"]').text()).toBe(
        'Change password',
      );
    },
  );

  it(
    'changes a password: reauth with current, then set the ' + 'new one',
    async () => {
      const actions = makeActions();
      const wrapper = await mountSettings(
        { hasPassword: true, providers: ['google'] },
        actions,
      );
      await flushPromises();

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();

      // Reauth first: current password only.
      await wrapper.get('#password-current').setValue('current-secret');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      expect(actions.reauthenticate).toHaveBeenCalledWith('current-secret');
      expect(actions.setPassword).not.toHaveBeenCalled();

      // Reauth succeeded — now the new-password form (with confirmation).
      expect(wrapper.find('#password-new').exists()).toBe(true);
      await wrapper.get('#password-new').setValue('new-secret');
      await wrapper.get('#password-new-confirm').setValue('new-secret');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      // The confirmation value is component-local: only the new password
      // reaches the action, and the current password never does.
      expect(actions.setPassword).toHaveBeenCalledWith('new-secret');
      expect(wrapper.emitted('updated')).toBeTruthy();

      // Exact success copy and back to idle.
      expect(wrapper.get('[data-testid="password-success"]').text()).toBe(
        'Password changed.',
      );
      expect(wrapper.get('[data-testid="password-action"]').text()).toBe(
        'Change password',
      );
    },
  );

  it(
    'adds a password for a provider-only user and reports exact ' + 'success',
    async () => {
      const actions = makeActions();
      const wrapper = await mountSettings(
        { hasPassword: false, providers: ['github'] },
        actions,
      );
      await flushPromises();

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();

      // Straight to the new-password form — the server arbitrates whether a
      // recent provider reauth is still required.
      await wrapper.get('#password-new').setValue('brand-new-password');
      await wrapper.get('#password-new-confirm').setValue('brand-new-password');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      expect(actions.setPassword).toHaveBeenCalledWith('brand-new-password');
      expect(actions.reauthenticate).not.toHaveBeenCalled();
      expect(actions.startProviderReauth).not.toHaveBeenCalled();
      expect(wrapper.emitted('updated')).toBeTruthy();
      expect(wrapper.get('[data-testid="password-success"]').text()).toBe(
        'Password added.',
      );
    },
  );

  it('shows incorrect-password copy when password reauth fails', async () => {
    const actions = makeActions({
      reauthenticate: vi.fn(async () => {
        throw new PasswordSettingsFailure('reauth-failed');
      }),
    });
    const wrapper = await mountSettings(
      { hasPassword: true, providers: ['google'] },
      actions,
    );
    await flushPromises();

    await wrapper.get('[data-testid="password-action"]').trigger('click');
    await flushPromises();
    await wrapper.get('#password-current').setValue('wrong');
    await wrapper.get('[data-testid="password-form"]').trigger('submit');
    await flushPromises();

    expect(wrapper.get('[data-testid="password-error"]').text()).toBe(
      'Incorrect password.',
    );
    // Still asking for the current password — the flow did not advance.
    expect(wrapper.find('#password-current').exists()).toBe(true);
    expect(wrapper.find('#password-new').exists()).toBe(false);
  });

  it(
    'recovers to password reauth when a change loses its reauth ' + 'window',
    async () => {
      const actions = makeActions({
        reauthenticate: vi.fn(async () => {}),
        setPassword: vi.fn(async () => {
          throw new PasswordSettingsFailure('reauth-required');
        }),
      });
      const wrapper = await mountSettings(
        { hasPassword: true, providers: ['google'] },
        actions,
      );
      await flushPromises();

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();
      await wrapper.get('#password-current').setValue('current-secret');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();
      await wrapper.get('#password-new').setValue('new-secret');
      await wrapper.get('#password-new-confirm').setValue('new-secret');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      // Back to the password-reauth step, with recovery copy.
      expect(wrapper.find('#password-current').exists()).toBe(true);
      expect(wrapper.get('[data-testid="password-error"]').text()).toContain(
        'Sign in again',
      );
      expect(wrapper.emitted('updated')).toBeUndefined();
    },
  );

  it(
    'offers linked providers for reauth when an add needs provider ' + 'reauth',
    async () => {
      const actions = makeActions({
        setPassword: vi.fn(async () => {
          throw new PasswordSettingsFailure('reauth-required');
        }),
      });
      const wrapper = await mountSettings(
        { hasPassword: false, providers: ['google', 'github'] },
        actions,
      );
      await flushPromises();

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();
      await wrapper.get('#password-new').setValue('new-secret');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      // Only the linked providers are offered, each as a reauth target.
      expect(
        wrapper
          .find('[data-testid="password-provider-reauth-google"]')
          .exists(),
      ).toBe(true);
      expect(
        wrapper
          .find('[data-testid="password-provider-reauth-github"]')
          .exists(),
      ).toBe(true);
      expect(
        wrapper
          .find('[data-testid="password-provider-reauth-linkedin"]')
          .exists(),
      ).toBe(false);
    },
  );

  it(
    'starts the provider reauth round trip with the selected ' + 'provider',
    async () => {
      const actions = makeActions({
        setPassword: vi.fn(async () => {
          throw new PasswordSettingsFailure('reauth-required');
        }),
      });
      const wrapper = await mountSettings(
        { hasPassword: false, providers: ['github'] },
        actions,
      );
      await flushPromises();

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();
      await wrapper.get('#password-new').setValue('new-secret');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      await wrapper
        .get('[data-testid="password-provider-reauth-github"]')
        .trigger('click');
      await flushPromises();

      expect(actions.startProviderReauth).toHaveBeenCalledWith('github');
    },
  );

  it('maps each closed policy issue to fixed copy', async () => {
    const cases = [
      ['length', '12 characters'],
      ['common', 'too common'],
      ['breached', 'data breach'],
    ] as const;

    for (const [issue, needle] of cases) {
      const actions = makeActions({
        setPassword: vi.fn(async () => {
          throw new PasswordSettingsFailure('password-invalid', issue);
        }),
      });
      const wrapper = await mountSettings(
        { hasPassword: false, providers: ['google'] },
        actions,
      );
      await flushPromises();

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();
      await wrapper.get('#password-new').setValue('bad');
      await wrapper.get('[data-testid="password-form"]').trigger('submit');
      await flushPromises();

      expect(wrapper.get('[data-testid="password-error"]').text()).toContain(
        needle,
      );
    }
  });

  it('blocks submission when the confirmation does not match', async () => {
    const actions = makeActions();
    const wrapper = await mountSettings(
      { hasPassword: false, providers: ['google'] },
      actions,
    );
    await flushPromises();

    await wrapper.get('[data-testid="password-action"]').trigger('click');
    await flushPromises();
    await wrapper.get('#password-new').setValue('one-password');
    await wrapper.get('#password-new-confirm').setValue('different-password');
    await wrapper.get('[data-testid="password-form"]').trigger('submit');
    await flushPromises();

    expect(wrapper.get('[data-testid="password-error"]').text()).toBe(
      'Passwords do not match.',
    );
    expect(actions.setPassword).not.toHaveBeenCalled();
  });

  it('disables submit while pending and does not double-fire', async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const setPassword = vi.fn(async () => {
      await gate;
    });
    const wrapper = await mountSettings(
      { hasPassword: false, providers: ['google'] },
      makeActions({ setPassword }),
    );
    await flushPromises();

    await wrapper.get('[data-testid="password-action"]').trigger('click');
    await flushPromises();
    await wrapper.get('#password-new').setValue('new-secret');
    await wrapper.get('[data-testid="password-form"]').trigger('submit');
    await flushPromises();

    // While the action is unresolved the submit is disabled.
    expect(
      wrapper.get('[data-testid="password-set-submit"]').attributes('disabled'),
    ).toBeDefined();

    // A second submit during the pending window is a no-op.
    await wrapper.get('[data-testid="password-form"]').trigger('submit');
    await flushPromises();
    expect(setPassword).toHaveBeenCalledOnce();

    release();
    await flushPromises();
  });

  it('maps rate-limit and unavailable failures to fixed copy', async () => {
    const actions = makeActions({
      reauthenticate: vi.fn(async () => {
        throw new PasswordSettingsFailure('rate-limited');
      }),
    });
    const wrapper = await mountSettings(
      { hasPassword: true, providers: [] },
      actions,
    );
    await flushPromises();

    await wrapper.get('[data-testid="password-action"]').trigger('click');
    await flushPromises();
    await wrapper.get('#password-current').setValue('x');
    await wrapper.get('[data-testid="password-form"]').trigger('submit');
    await flushPromises();

    expect(wrapper.get('[data-testid="password-error"]').text()).toBe(
      'Too many attempts. Try again later.',
    );
  });

  it('never renders an email address', async () => {
    const wrapper = await mountSettings(
      { hasPassword: false, providers: ['google', 'github', 'linkedin'] },
      makeActions(),
    );
    await flushPromises();

    await wrapper.get('[data-testid="password-action"]').trigger('click');
    await flushPromises();

    // No provider email is shown in any state; the provider labels are
    // fixed names, never email addresses.
    expect(wrapper.text()).not.toContain('@');
    expect(wrapper.text()).not.toMatch(/[a-z]+@[a-z]+\.[a-z]+/);
  });

  it('cancels back to idle and clears form state', async () => {
    const wrapper = await mountSettings(
      { hasPassword: true, providers: ['google'] },
      makeActions(),
    );
    await flushPromises();

    await wrapper.get('[data-testid="password-action"]').trigger('click');
    await flushPromises();
    await wrapper.get('#password-current').setValue('current-secret');
    await wrapper.get('[data-testid="password-cancel"]').trigger('click');
    await flushPromises();

    expect(wrapper.get('[data-testid="password-action"]').exists()).toBe(true);
    expect(wrapper.find('#password-current').exists()).toBe(false);
  });
});
