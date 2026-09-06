import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';

import PrivacySettings from '../app/components/settings/PrivacySettings.vue';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities({ providerLogin: false, agentAccess: false });
registerEndpoint('/api/v1/me', () => ({
  data: {
    user: {
      id: 'user-1',
      email: 'demo@example.com',
      name: 'Demo User',
      avatarKey: null,
      hasPassword: false,
    },
    csrfToken: 'privacy-csrf-token',
    identities: [{ provider: 'google' }],
  },
}));
registerEndpoint('/api/v1/sessions', () => ({ data: [] }));

describe('sessions.vue privacy settings capability gate', () => {
  it(
    'does not pass linked providers to deletion reauthentication when disabled',
    async () => {
      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      expect(wrapper.getComponent(PrivacySettings).props('providers')).toEqual(
        [],
      );
    },
  );
});
