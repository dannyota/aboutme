import { beforeEach, describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

registerCapabilities();
beforeEach(() => setSiteLocale('en'));

// Own file: Nuxt reuses a URL's cached useFetch result within a test file.
registerEndpoint('/api/v1/me', () => ({
  data: {
    user: {
      id: 'user-1',
      email: 'demo@example.com',
      name: 'Demo User',
      avatarKey: null,
    },
    csrfToken: 'test-csrf-token',
    identities: [{ provider: 'google' }],
  },
}));

registerEndpoint('/api/v1/sessions', () => ({
  data: [
    {
      id: 'sess-3',
      createdAt: '2026-05-01T00:00:00Z',
      lastSeenAt: '2026-05-02T00:00:00Z',
      ua: null,
      ip: null,
      current: false,
    },
  ],
}));

describe('sessions.vue nullable fields', () => {
  it('renders a session row with null ua and ip without crashing', async () => {
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();

    expect(wrapper.find('[data-testid="session-row-sess-3"]').exists()).toBe(
      true,
    );
  });
});
