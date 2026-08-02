import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { defineComponent, h } from 'vue';

// The registerEndpoint mock's event doesn't normalize header casing the way
// a real Node request does, and h3's own `getRequestHeader` disagrees with
// the mocked event shape across the versions pinned here — so read the
// mocked request headers directly, case-insensitively.
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
    const { user, csrfToken, identities, logout, refresh } = useAuth();
    return { user, csrfToken, identities, logout, refresh };
  },
  render() {
    return h('div', [
      h('span', { 'data-testid': 'user' }, JSON.stringify(this.user)),
      h('span', { 'data-testid': 'csrf' }, JSON.stringify(this.csrfToken)),
      h(
        'span',
        { 'data-testid': 'identities' },
        JSON.stringify(this.identities),
      ),
      h(
        'button',
        { 'data-testid': 'logout-button', 'onClick': () => this.logout() },
        'logout',
      ),
      h(
        'button',
        { 'data-testid': 'refresh-button', 'onClick': () => this.refresh() },
        'refresh',
      ),
    ]);
  },
});

describe('useAuth', () => {
  it('logout() posts to /api/v1/auth/logout with the CSRF header and '
    + 'a JSON content type', async () => {
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

    let receivedMethod: string | undefined;
    let receivedHeader: string | undefined;
    let receivedContentType: string | undefined;
    registerEndpoint('/api/v1/auth/logout', {
      method: 'POST',
      handler: (event) => {
        receivedMethod = event.method;
        receivedHeader = requestHeader(event, 'x-csrf-token');
        receivedContentType = requestHeader(event, 'content-type');
        return { data: { ok: true } };
      },
    });

    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    await wrapper.get('[data-testid="logout-button"]').trigger('click');
    await flushPromises();

    expect(receivedMethod).toBe('POST');
    expect(receivedHeader).toBe('test-csrf-token');
    expect(receivedContentType).toBe('application/json');
  });

  it('renders a logged-out state instead of throwing on an unexpected '
    + '/me response shape', async () => {
    // Simulates contract drift (e.g. a missing `data` envelope key) —
    // `data.value?.data.user` would throw reading `.user` off
    // `undefined`; `data.value?.data?.user` must degrade to null instead.
    registerEndpoint('/api/v1/me', { method: 'GET', handler: () => ({}) });

    const wrapper = await mountSuspended(Probe);
    await flushPromises();
    // Force a genuine refetch against the malformed handler above —
    // `useFetch`'s automatic initial fetch is keyed by URL and can reuse
    // an already-`success` result from an earlier test's mount in this
    // same file, so asserting against it alone isn't reliable; `refresh()`
    // always re-fetches.
    await wrapper.get('[data-testid="refresh-button"]').trigger('click');
    await flushPromises();

    expect(wrapper.get('[data-testid="user"]').text()).toBe('null');
    expect(wrapper.get('[data-testid="csrf"]').text()).toBe('null');
    expect(wrapper.get('[data-testid="identities"]').text()).toBe('[]');
  });
});
