import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readRawBody, setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';

// mockNuxtImport's factory is hoisted like vi.mock, so it creates the mock.
mockNuxtImport('navigateTo', () => vi.fn());

// registerEndpoint's mocked event does not normalize header casing.
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
    hasPassword: true,
  },
  csrfToken: 'test-csrf-token',
  identities: [{ provider: 'google' }],
};

registerEndpoint('/api/v1/me', () => ({ data: meData }));

const Probe = defineComponent({
  setup() {
    const { user, csrfToken, identities, authState, logout, mutate, refresh }
      = useAuth();
    return { user, csrfToken, identities, authState, logout, mutate, refresh };
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
      h('span', { 'data-testid': 'auth-state' }, this.authState),
      h(
        'button',
        {
          'data-testid': 'logout-button',
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
      h(
        'button',
        {
          'data-testid': 'json-mutation-button',
          'onClick': () => {
            this.mutate('/api/v1/test-json-mutation', {
              method: 'POST',
              body: { enabled: true },
            }).catch(() => {});
          },
        },
        'mutate JSON',
      ),
      h(
        'button',
        {
          'data-testid': 'password-put-button',
          'onClick': () => {
            this.mutate('/api/v1/me/password', {
              method: 'PUT',
              body: { password: 'secret' },
            }).catch(() => {});
          },
        },
        'mutate PUT password',
      ),
    ]);
  },
});

describe('useAuth', () => {
  beforeEach(() => {
    vi.mocked(navigateTo).mockClear();
  });

  // eslint-disable-next-line max-len
  it('logout() posts to /api/v1/auth/logout as a bodiless mutation', async () => {
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
    expect(receivedContentType).toBeUndefined();
    clearNuxtData();
  });

  it('keeps application/json on mutations that have a JSON body', async () => {
    let receivedContentType: string | undefined;
    let receivedBody: string | undefined;
    registerEndpoint('/api/v1/test-json-mutation', {
      method: 'POST',
      handler: async (event) => {
        receivedContentType = requestHeader(event, 'content-type');
        receivedBody = await readRawBody(event);
        setResponseStatus(event, 204);
        return null;
      },
    });

    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    await wrapper.get('[data-testid="json-mutation-button"]').trigger('click');
    await flushPromises();

    expect(receivedContentType).toBe('application/json');
    expect(receivedBody).toBe('{"enabled":true}');
  });

  it('exposes hasPassword from the /me user', async () => {
    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    expect(wrapper.get('[data-testid="user"]').text()).toContain(
      '"hasPassword":true',
    );
  });

  it('sends an authenticated PUT with JSON and CSRF', async () => {
    let receivedMethod: string | undefined;
    let receivedHeader: string | undefined;
    let receivedContentType: string | undefined;
    let receivedBody: string | undefined;
    registerEndpoint('/api/v1/me/password', {
      method: 'PUT',
      handler: async (event) => {
        receivedMethod = event.method;
        receivedHeader = requestHeader(event, 'x-csrf-token');
        receivedContentType = requestHeader(event, 'content-type');
        receivedBody = await readRawBody(event);
        setResponseStatus(event, 204);
        return null;
      },
    });

    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    await wrapper.get('[data-testid="password-put-button"]').trigger('click');
    await flushPromises();

    expect(receivedMethod).toBe('PUT');
    expect(receivedHeader).toBe('test-csrf-token');
    expect(receivedContentType).toBe('application/json');
    expect(receivedBody).toBe('{"password":"secret"}');
  });

  it('PUT retries once on csrf_rejected, then surfaces', async () => {
    let putCalls = 0;
    registerEndpoint('/api/v1/me/password', {
      method: 'PUT',
      handler: (event) => {
        putCalls += 1;
        setResponseStatus(event, 403);
        return { error: { code: 'csrf_rejected', message: 'x' } };
      },
    });

    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    await wrapper.get('[data-testid="password-put-button"]').trigger('click');
    await flushPromises();
    await flushPromises();

    expect(putCalls).toBe(2);
  });

  it('PUT does not retry a non-csrf 403 (e.g. reauth_required)', async () => {
    let putCalls = 0;
    registerEndpoint('/api/v1/me/password', {
      method: 'PUT',
      handler: (event) => {
        putCalls += 1;
        setResponseStatus(event, 403);
        return { error: { code: 'reauth_required', message: 'x' } };
      },
    });

    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    await wrapper.get('[data-testid="password-put-button"]').trigger('click');
    await flushPromises();
    await flushPromises();

    expect(putCalls).toBe(1);
  });

  it('logout() navigates to /login once the session is destroyed', async () => {
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

  it(
    'renders a logged-out state instead of throwing on an unexpected '
    + '/me response shape',
    async () => {
      registerEndpoint('/api/v1/me', { method: 'GET', handler: () => ({}) });

      const wrapper = await mountSuspended(Probe);
      await flushPromises();
      // Nuxt's useFetch can reuse a URL's cached result; refresh() refetches.
      await wrapper.get('[data-testid="refresh-button"]').trigger('click');
      await flushPromises();

      expect(wrapper.get('[data-testid="user"]').text()).toBe('null');
      expect(wrapper.get('[data-testid="csrf"]').text()).toBe('null');
      expect(wrapper.get('[data-testid="identities"]').text()).toBe('[]');
      expect(wrapper.get('[data-testid="auth-state"]').text()).toBe('error');
    },
  );

  it('resolves refresh after identity, state, and CSRF update', async () => {
    const refreshed = {
      ...meData,
      user: { ...meData.user, id: 'owner-1' },
      csrfToken: 'csrf-new',
    };
    registerEndpoint('/api/v1/me', {
      method: 'GET',
      handler: () => ({ data: refreshed }),
    });

    const wrapper = await mountSuspended(Probe);
    await wrapper.vm.refresh();

    expect(wrapper.get('[data-testid="auth-state"]').text()).toBe(
      'authenticated',
    );
    expect(wrapper.get('[data-testid="user"]').text()).toContain('owner-1');
    expect(wrapper.get('[data-testid="csrf"]').text()).toBe('"csrf-new"');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('recovers a missing csrf token from a fresh authenticated read', async () => {
    let reads = 0;
    let measureRecovery = false;
    registerEndpoint('/api/v1/me', {
      method: 'GET',
      handler: () => {
        if (measureRecovery) reads += 1;
        return {
          data: {
            ...meData,
            csrfToken: measureRecovery && reads === 2
              ? 'csrf-recovered'
              : null,
          },
        };
      },
    });
    const wrapper = await mountSuspended(Probe);
    await flushPromises();

    measureRecovery = true;
    await wrapper.vm.refresh();

    expect(reads).toBe(2);
    expect(wrapper.get('[data-testid="auth-state"]').text()).toBe(
      'authenticated',
    );
    expect(wrapper.get('[data-testid="csrf"]').text()).toBe('"csrf-recovered"');
  });

  describe('CSRF x rotation self-heal (mutate())', () => {
    // eslint-disable-next-line max-len
    it('surfaces a second csrf_rejected instead of retrying forever', async () => {
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

      await expect(wrapper.vm.logout()).rejects.toBeDefined();

      expect(logoutCalls).toBe(2);
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

    it(
      'does not retry a 403 that is not csrf_rejected (e.g. '
      + 'reauth_required) — a mutation the server already refused must '
      + 'not be double-fired',
      async () => {
        let logoutCalls = 0;
        registerEndpoint('/api/v1/auth/logout', {
          method: 'POST',
          handler: (event) => {
            logoutCalls += 1;
            setResponseStatus(event, 403);
            return { error: { code: 'reauth_required', message: 'x' } };
          },
        });

        const wrapper = await mountSuspended(Probe);
        await flushPromises();

        await expect(wrapper.vm.logout()).rejects.toBeDefined();

        expect(logoutCalls).toBe(1);
        expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
      },
    );
  });
});
