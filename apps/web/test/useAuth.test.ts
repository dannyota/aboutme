import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';

// logout() navigates away via `navigateTo` on success — stub it so tests
// can assert on the target without a real page transition tearing the
// mounted wrapper down mid-test. `mockNuxtImport`'s factory is hoisted
// above any top-level `const` (same as `vi.mock`), so the mock function
// has to be created inside it; `navigateTo` itself becomes accessible as
// an auto-import in this file afterward, via `vi.mocked(navigateTo)`.
mockNuxtImport('navigateTo', () => vi.fn());

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

const meData = {
  user: {
    id: 'user-1',
    email: 'demo@example.com',
    name: 'Demo User',
    avatarKey: null,
  },
  csrfToken: 'test-csrf-token',
  identities: [{ provider: 'google' }],
};

registerEndpoint('/api/v1/me', () => ({ data: meData }));

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
        {
          'data-testid': 'logout-button',
          // The CSRF self-heal retry (and its "surface a second failure"
          // counterpart below) means logout() can genuinely reject; the
          // tests assert on side effects (mock call counts, navigateTo),
          // not on catching this click handler's own promise.
          'onClick': () => {
            this.logout().catch(() => {});
          },
        },
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
  beforeEach(() => {
    vi.mocked(navigateTo).mockClear();
  });

  it('logout() posts to /api/v1/auth/logout with the CSRF header and '
    + 'a JSON content type', async () => {
    let receivedMethod: string | undefined;
    let receivedHeader: string | undefined;
    let receivedContentType: string | undefined;
    registerEndpoint('/api/v1/auth/logout', {
      method: 'POST',
      handler: (event) => {
        receivedMethod = event.method;
        receivedHeader = requestHeader(event, 'x-csrf-token');
        receivedContentType = requestHeader(event, 'content-type');
        setResponseStatus(event, 204);
        return null;
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

  it('logout() navigates to /login once the session is destroyed',
    async () => {
      registerEndpoint('/api/v1/auth/logout', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 204);
          return null;
        },
      });

      const wrapper = await mountSuspended(Probe);
      await flushPromises();

      await wrapper.get('[data-testid="logout-button"]').trigger('click');
      await flushPromises();

      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
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

  describe('CSRF x rotation self-heal (mutate())', () => {
    it('refetches /me once and retries after a single csrf_rejected',
      async () => {
        let logoutCalls = 0;
        registerEndpoint('/api/v1/auth/logout', {
          method: 'POST',
          handler: (event) => {
            logoutCalls += 1;
            if (logoutCalls === 1) {
              setResponseStatus(event, 403);
              return { error: { code: 'csrf_rejected', message: 'x' } };
            }
            setResponseStatus(event, 204);
            return null;
          },
        });

        let meGetCalls = 0;
        registerEndpoint('/api/v1/me', {
          method: 'GET',
          handler: () => {
            meGetCalls += 1;
            return { data: meData };
          },
        });

        const wrapper = await mountSuspended(Probe);
        await flushPromises();
        const meGetCallsAtMount = meGetCalls;

        await wrapper.get('[data-testid="logout-button"]').trigger('click');
        await flushPromises();
        await flushPromises();

        // One failed attempt, one refetch of /me to pick up the rotated
        // secret, one retry that succeeds — not a hard logout for what is
        // really just a same-day session rotation.
        expect(logoutCalls).toBe(2);
        expect(meGetCalls).toBe(meGetCallsAtMount + 1);
        expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
      });

    it('surfaces a second csrf_rejected instead of retrying forever',
      async () => {
        let logoutCalls = 0;
        registerEndpoint('/api/v1/auth/logout', {
          method: 'POST',
          handler: (event) => {
            logoutCalls += 1;
            setResponseStatus(event, 403);
            return { error: { code: 'csrf_rejected', message: 'x' } };
          },
        });

        const wrapper = await mountSuspended(Probe);
        await flushPromises();

        await wrapper.get('[data-testid="logout-button"]').trigger('click');
        await flushPromises();
        await flushPromises();

        // Exactly the original attempt plus one retry — a second genuine
        // rejection surfaces (no navigation), it does not loop.
        expect(logoutCalls).toBe(2);
        expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
      });
  });
});
