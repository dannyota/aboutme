import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readRawBody, setResponseStatus } from 'h3';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

// Deliberately its own file with a single test, mirroring
// `useAuth-csrf-rotation.test.ts`: the assertion depends on the FIRST `/me`
// resolution returning a password user (`hasPassword: true`), and `useFetch`
// caches by URL within a shared Nuxt app instance. In a file that mounts
// `/me` more than once (or alongside `sessions.test.ts`), whether THIS
// mount actually invokes its own handler is not reliably predictable.

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

describe('sessions.vue password reauth', () => {
  it('reauthenticates a password user through POST with JSON and CSRF',
    async () => {
      registerEndpoint('/api/v1/me', () => ({
        data: {
          user: {
            id: 'user-1',
            email: 'demo@example.com',
            name: 'Demo User',
            avatarKey: null,
            hasPassword: true,
          },
          csrfToken: 'test-csrf-token',
          identities: [{ provider: 'google' }],
        },
      }));
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
        ],
      }));

      let receivedMethod: string | undefined;
      let receivedHeader: string | undefined;
      let receivedBody: string | undefined;
      registerEndpoint('/api/v1/auth/password/reauth', {
        method: 'POST',
        handler: async (event) => {
          receivedMethod = event.method;
          receivedHeader = requestHeader(event, 'x-csrf-token');
          receivedBody = await readRawBody(event);
          setResponseStatus(event, 204);
          return null;
        },
      });

      const wrapper = await mountSuspended(SessionsPage);
      await flushPromises();

      // A password user is offered "Change password", then reauth with the
      // current password only.
      expect(wrapper.get('[data-testid="password-status"]').text()).toBe(
        'You have a password.',
      );

      await wrapper.get('[data-testid="password-action"]').trigger('click');
      await flushPromises();
      await wrapper.get('#password-current').setValue('current-secret');
      await wrapper.get('form').trigger('submit');
      await flushPromises();

      expect(receivedMethod).toBe('POST');
      expect(receivedHeader).toBe('test-csrf-token');
      expect(receivedBody).toBe('{"password":"current-secret"}');
    });
});
