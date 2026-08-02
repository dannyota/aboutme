import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';

// Deliberately its own file, not a case inside `useAuth.test.ts`: this
// test needs to control PRECISELY which csrfToken value `/me` returns on
// its very first resolution versus a later one, and `useFetch`'s
// automatic initial fetch is keyed by URL — in a file shared with other
// tests that also resolve `/api/v1/me` successfully, whether THIS test's
// own mount actually invokes its own handler (versus reusing an
// already-cached "success" result from an earlier test) is not
// reliably predictable. A fresh, empty Nuxt app instance (one per test
// file) makes the first real call to the registered handler
// deterministic.
mockNuxtImport('navigateTo', () => vi.fn());

interface MockEvent {
  node?: { req?: { headers?: Record<string, string> } };
}

function requestHeader(event: MockEvent, name: string): string | undefined {
  const headers = event.node?.req?.headers ?? {};
  const key = Object.keys(headers).find(
    (k) => k.toLowerCase() === name.toLowerCase(),
  );
  return key ? headers[key] : undefined;
}

const Probe = defineComponent({
  setup() {
    const { logout } = useAuth();
    return { logout };
  },
  render() {
    return h(
      'button',
      {
        'data-testid': 'logout-button',
        'onClick': () => {
          this.logout().catch(() => {});
        },
      },
      'logout',
    );
  },
});

describe('useAuth mutate() CSRF x rotation self-heal', () => {
  it(
    'retries with the token /me returns AFTER refetching, not the one '
    + 'captured before the 403',
    async () => {
      let meGetCalls = 0;
      registerEndpoint('/api/v1/me', {
        method: 'GET',
        handler: () => {
          meGetCalls += 1;
          return {
            data: {
              user: {
                id: 'user-1',
                email: 'demo@example.com',
                name: 'Demo User',
                avatarKey: null,
              },
              // First resolution (the mount) hands out the token that
              // will go stale; the refetch mid-retry hands out the one
              // a real session rotation would mint.
              csrfToken: meGetCalls === 1
                ? 'test-csrf-token'
                : 'rotated-csrf-token',
              identities: [{ provider: 'google' }],
            },
          };
        },
      });

      const receivedHeaders: (string | undefined)[] = [];
      let logoutCalls = 0;
      registerEndpoint('/api/v1/auth/logout', {
        method: 'POST',
        handler: (event) => {
          logoutCalls += 1;
          receivedHeaders.push(requestHeader(event, 'x-csrf-token'));
          if (logoutCalls === 1) {
            setResponseStatus(event, 403);
            return { error: { code: 'csrf_rejected', message: 'x' } };
          }
          setResponseStatus(event, 204);
          return null;
        },
      });

      const wrapper = await mountSuspended(Probe);
      await flushPromises();
      // The automatic initial fetch is this test's very first (and only
      // prior) chance to resolve /me in this fresh app instance, so it
      // is guaranteed to have actually invoked the handler above once.
      expect(meGetCalls).toBe(1);

      await wrapper.get('[data-testid="logout-button"]').trigger('click');
      await flushPromises();
      await flushPromises();

      expect(logoutCalls).toBe(2);
      expect(meGetCalls).toBe(2);
      // Load-bearing: the retry must carry the REFRESHED token, not the
      // stale one captured once and reused for both attempts — that's
      // the exact defect this feature exists to prevent (hoisting
      // `csrfHeaders(csrfToken.value)` out of the retry closure would
      // pass a "did it retry" test while still shipping this bug).
      expect(receivedHeaders[0]).toBe('test-csrf-token');
      expect(receivedHeaders[1]).toBe('rotated-csrf-token');
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
    },
  );
});
