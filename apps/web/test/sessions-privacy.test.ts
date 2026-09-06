import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readRawBody, setResponseStatus } from 'h3';

import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

interface MockEvent {
  method?: string;
  node?: { req?: { headers?: Record<string, string> } };
}

function requestHeader(event: MockEvent, name: string): string | undefined {
  const headers = event.node?.req?.headers ?? {};
  const key = Object.keys(headers).find(
    (candidate) => candidate.toLowerCase() === name.toLowerCase(),
  );
  return key ? headers[key] : undefined;
}

describe('sessions.vue privacy settings', () => {
  it('sends one bodyless CSRF-protected account deletion request', async () => {
    registerEndpoint('/api/v1/me', () => ({
      data: {
        user: {
          id: 'user-1',
          email: 'demo@example.com',
          name: 'Demo User',
          avatarKey: null,
          hasPassword: true,
        },
        csrfToken: 'privacy-csrf-token',
        identities: [{ provider: 'google' }],
      },
    }));
    registerEndpoint('/api/v1/sessions', () => ({ data: [] }));

    let method: string | undefined;
    let csrf: string | undefined;
    let contentType: string | undefined;
    let body: string | undefined;
    registerEndpoint('/api/v1/me', {
      method: 'DELETE',
      handler: async (event) => {
        method = event.method;
        csrf = requestHeader(event, 'x-csrf-token');
        contentType = requestHeader(event, 'content-type');
        body = await readRawBody(event);
        setResponseStatus(event, 204);
        return null;
      },
    });

    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    await wrapper.get('[data-testid="account-delete-action"]').trigger('click');
    await flushPromises();
    const input = document.body.querySelector<HTMLInputElement>(
      '[role="alertdialog"] input',
    );
    input!.value = 'DELETE';
    input!.dispatchEvent(new Event('input', { bubbles: true }));
    await flushPromises();
    (
      document.body.querySelector(
        '[data-action="confirm-account-delete"]',
      ) as HTMLButtonElement
    ).click();
    await flushPromises();

    expect(method).toBe('DELETE');
    expect(csrf).toBe('privacy-csrf-token');
    expect(contentType).toBeUndefined();
    expect(body).toBeUndefined();
    wrapper.unmount();
  });
});
