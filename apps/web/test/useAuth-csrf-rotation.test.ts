import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';

// Own file: Nuxt reuses a URL's cached useFetch result within a test file.
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
              csrfToken:
                meGetCalls === 1 ? 'test-csrf-token' : 'rotated-csrf-token',
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
      expect(meGetCalls).toBe(1);

      await wrapper.get('[data-testid="logout-button"]').trigger('click');
      await flushPromises();
      await flushPromises();

      expect(logoutCalls).toBe(2);
      expect(meGetCalls).toBe(2);
      expect(receivedHeaders[0]).toBe('test-csrf-token');
      expect(receivedHeaders[1]).toBe('rotated-csrf-token');
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
    },
  );
});
