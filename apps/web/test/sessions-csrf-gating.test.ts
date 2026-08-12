import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';

// Deliberately its own file, not a case inside sessions.test.ts:
// `useFetch('/api/v1/me')`'s automatic initial fetch is keyed by URL and,
// once resolved "success" in a given test file's shared Nuxt app instance
// (even with a malformed body — the HTTP call itself still succeeded), a
// later mount in the SAME file reuses that cached status rather than
// re-hitting a newly-registered handler, regardless of ordering.
// sessions.vue has no on-page trigger to force a fresh /me fetch the way
// `useAuth.test.ts`'s probe component does — but each Vitest test *file*
// gets its own fresh Nuxt app instance, so isolating this case in its own
// file sidesteps the whole problem instead of fighting it.
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

describe('sessions.vue CSRF gating', () => {
  it('uses the first returned identity as the reauthentication provider',
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
          identities: [
            { provider: 'linkedin' },
            { provider: 'google' },
          ],
        },
      };

      const wrapper = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=reauth_required',
      });
      await flushPromises();

      const prompt = wrapper.get('[data-testid="reauth-prompt"]');
      expect(prompt.get('button').text()).toBe(
        'Sign in again with linkedin',
      );

      meResponse = {};
      await refreshNuxtData();
      await flushPromises();
    });

  it('disables mutating controls until csrfToken is available', async () => {
    // Contract drift / a proxy error page — /me resolves with no usable
    // csrfToken, so nothing that would send X-CSRF-Token is clickable.
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    const currentRow = wrapper.get('[data-testid="session-row-sess-1"]');
    const logoutButton = currentRow
      .findAll('button')
      .find((b) => b.text() === 'Log out');
    expect(logoutButton?.element.disabled).toBe(true);

    const revokeButton = wrapper.get(
      '[data-testid="session-row-sess-2"] [data-testid="revoke-button"]',
    );
    expect(revokeButton.element.disabled).toBe(true);

    const revokeAllButton = wrapper.get('[data-testid="revoke-all-button"]');
    expect(revokeAllButton.element.disabled).toBe(true);
  });
});
