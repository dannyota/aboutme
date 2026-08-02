import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';

// Deliberately its own file, not a case inside sessions.test.ts: see
// `sessions-csrf-gating.test.ts`'s own doc comment for why —
// `useFetch('/api/v1/sessions')`'s automatic initial fetch is keyed by
// URL and, once resolved "success" in a given test file's shared Nuxt app
// instance, a later mount in the SAME file reuses that cached result
// rather than re-hitting a newly-registered handler, regardless of
// ordering. Isolating this in its own file sidesteps the whole problem.
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

// Go: `UA *string` / `IP *string`; OpenAPI: `type: [string, "null"]` — a
// request that legitimately carried neither header nullifies both, they
// are not always-present strings.
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
  it('renders a session row with null ua and ip without crashing',
    async () => {
      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      expect(
        wrapper.find('[data-testid="session-row-sess-3"]').exists(),
      ).toBe(true);
    });
});
