import { beforeEach, describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

registerCapabilities();
beforeEach(() => setSiteLocale('en'));

// Own file: Nuxt reuses a URL's cached useFetch result within a test file.
let meResponse: unknown = {};

registerEndpoint('/api/v1/me', {
  method: 'GET',
  handler: () => meResponse,
});
registerEndpoint('/api/v1/sessions', () => ({
  data: [
    {
      id: 'sess-1',
      createdAt: '2026-07-01T00:00:00Z',
      lastSeenAt: '2026-08-01T00:00:00Z',
      ua: 'Chrome on macOS',
      ip: '203.0.113.10',
      current: true,
    },
    {
      id: 'sess-2',
      createdAt: '2026-06-01T00:00:00Z',
      lastSeenAt: '2026-07-15T00:00:00Z',
      ua: 'Firefox on Linux',
      ip: '203.0.113.20',
      current: false,
    },
  ],
}));
registerEndpoint('/api/v1/me/second-factor', () => ({
  data: { enabled: false, passkeys: [], recoveryCodesRemaining: 0 },
}));

describe('sessions.vue CSRF gating', () => {
  it(
    'uses the first returned identity as the reauthentication ' + 'provider',
    async () => {
      meResponse = {
        data: {
          user: {
            id: 'user-1',
            email: 'demo@example.com',
            name: 'Demo User',
            avatarKey: null,
          },
          csrfToken: 'test-csrf-token',
          identities: [{ provider: 'linkedin' }, { provider: 'google' }],
        },
      };

      const wrapper = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=reauth_required',
      });
      await flushPromises();

      const prompt = wrapper.get('[data-testid="reauth-prompt"]');
      expect(prompt.get('[data-slot="button"]').text()).toBe(
        'Sign in again with LinkedIn',
      );

      meResponse = {};
      await refreshNuxtData();
      await flushPromises();
    },
  );

  it('disables mutating controls until csrfToken is available', async () => {
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    const currentRow = wrapper.get('[data-testid="session-row-sess-1"]');
    const logoutButton = currentRow
      .findAll('[data-slot="button"]')
      .find((b) => b.text() === 'Log out');
    expect(logoutButton?.element.disabled).toBe(true);

    const revokeButton = wrapper.get(
      '[data-testid="session-row-sess-2"] [data-testid="revoke-button"]',
    );
    expect(revokeButton.element.disabled).toBe(true);

    const revokeAllButton = wrapper.get('[data-testid="revoke-all-button"]');
    expect(revokeAllButton.element.disabled).toBe(true);
  });

  it(
    'mounts the authenticator-app section with no usable csrfToken',
    async () => {
      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      expect(wrapper.get('[data-testid="totp-status"]').text())
        .toBe('Not set up.');
    },
  );
});
