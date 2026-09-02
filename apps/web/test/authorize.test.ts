import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AuthorizePage from '../app/pages/authorize.vue';
import LoginPage from '../app/pages/login.vue';
import { OAuthConsentFailure } from '../app/composables/useOAuthConsent';
import { registerCapabilities } from './support/capabilities';

mockNuxtImport('navigateTo', () => vi.fn());
registerCapabilities();

const consentMocks = vi.hoisted(() => ({
  get: vi.fn(),
  decide: vi.fn(),
}));
vi.mock('../app/composables/useOAuthConsent', () => ({
  OAuthConsentFailure: class OAuthConsentFailure extends Error {
    readonly kind: string;
    constructor(kind: string) {
      super(kind);
      this.kind = kind;
    }
  },
  useOAuthConsent: () => consentMocks,
}));

const query = {
  client_id: '018f5b6a-9a3e-7c21-8b1e-000000000001',
  redirect_uri: 'https://agent.example/callback',
  response_type: 'code',
  scope: 'resumes:read resumes:write',
  state: 'opaque-state',
  code_challenge: 'abcdefghijklmnopqrstuvwxyz1234567890123456789',
  code_challenge_method: 'S256',
};

function route(path = '/authorize') {
  const params = new URLSearchParams(query);
  return `${path}${path.includes('?') ? '&' : '?'}${params}`;
}

beforeEach(() => {
  vi.mocked(navigateTo).mockClear();
  consentMocks.get.mockReset();
  consentMocks.decide.mockReset();
  consentMocks.get.mockResolvedValue({
    clientName: 'Resume agent',
    scopes: ['resumes:read'],
  });
  consentMocks.decide.mockResolvedValue({
    redirectTo: 'https://agent.example/callback?code=x',
  });
});

describe('/authorize', () => {
  it('composes the auth card and shared banner', async () => {
    const wrapper = await mountSuspended(AuthorizePage, {
      route: '/authorize?client_id=only-one-field',
    });
    await flushPromises();

    expect(wrapper.find('[data-slot="card"]').exists()).toBe(true);
    expect(
      wrapper.get('[data-testid="consent-error"]').attributes('role'),
    ).toBe(
      'alert',
    );
  });

  it('renders hostile client names as text and displays scopes', async () => {
    consentMocks.get.mockResolvedValue({
      clientName: '<img src=x onerror=alert(1)>',
      scopes: ['resumes:read', 'resumes:write'],
    });
    const wrapper = await mountSuspended(AuthorizePage, { route: route() });
    await flushPromises();
    expect(wrapper.get('[data-testid="consent-client-name"]').text()).toBe(
      '<img src=x onerror=alert(1)>',
    );
    expect(wrapper.find('[src]').exists()).toBe(false);
    expect(wrapper.text()).toContain('Read resumes');
    expect(wrapper.text()).toContain('Write resumes');
  });

  it.each(['approve', 'deny'] as const)(
    'posts exact %s decision body',
    async (decision) => {
      let body: unknown;
      consentMocks.decide.mockImplementation(
        async (request: unknown, selected: string) => {
          body = { ...(request as object), decision: selected };
          return { redirectTo: 'https://agent.example/callback?code=x' };
        },
      );
      const wrapper = await mountSuspended(AuthorizePage, { route: route() });
      await flushPromises();
      if (decision === 'approve') {
        await wrapper.get('[data-testid="consent-form"]').trigger('submit');
      } else {
        await wrapper.get('[data-decision="deny"]').trigger('click');
      }
      await flushPromises();
      expect(body).toEqual({ ...query, decision });
    },
  );

  it(
    'navigates to returned redirectTo verbatim as an external page',
    async () => {
      const redirectTo = 'https://agent.example/callback?code=a%2Fb&state=x';
      consentMocks.decide.mockResolvedValue({ redirectTo });
      const wrapper = await mountSuspended(AuthorizePage, { route: route() });
      await flushPromises();
      await wrapper.get('[data-testid="consent-form"]').trigger('submit');
      await flushPromises();
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(redirectTo, {
        external: true,
      });
    },
  );

  it('does not call the API for an incomplete local query', async () => {
    const calls = consentMocks.get;
    const wrapper = await mountSuspended(AuthorizePage, {
      route: '/authorize?client_id=only-one-field',
    });
    await flushPromises();
    expect(wrapper.get('[data-testid="consent-error"]').text()).toContain(
      'invalid',
    );
    expect(calls).not.toHaveBeenCalled();
  });

  it.each([
    [400, 'request_invalid', 'invalid'],
    [503, 'server_error', 'unable'],
  ])(
    'renders closed %s error copy without server details',
    async (status, code, word) => {
      consentMocks.get.mockRejectedValue(
        new OAuthConsentFailure(code === 'request_invalid'
          ? 'invalid-request'
          : 'unavailable'),
      );
      const wrapper = await mountSuspended(AuthorizePage, { route: route() });
      await flushPromises();
      const error = wrapper.get('[data-testid="consent-error"]');
      expect(error.text().toLowerCase()).toContain(word);
      expect(error.text()).not.toContain('do not show this');
    },
  );

  it(
    'redirects session-required failures to login with the exact full path',
    async () => {
      consentMocks.get.mockRejectedValue(
        new OAuthConsentFailure('session-required'),
      );
      await mountSuspended(AuthorizePage, {
        route: route('/authorize?x=1'),
      });
      await flushPromises();
      const currentPath = useRoute().fullPath;
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        `/login?next=${encodeURIComponent(currentPath)}`,
      );
    },
  );

  it.each([
    ['approve', 'approve'],
    ['approve', 'deny'],
  ] as const)(
    'guards rapid %s/%s submissions with one request',
    async (first, second) => {
      let calls = 0;
      let release: () => void = () => {};
      consentMocks.decide.mockImplementation(() => new Promise((resolve) => {
        calls += 1;
        release = () => resolve({ redirectTo: 'https://agent.example' });
      }));
      const wrapper = await mountSuspended(AuthorizePage, { route: route() });
      await flushPromises();
      const firstSubmit = first === 'approve'
        ? wrapper.get('[data-testid="consent-form"]').trigger('submit')
        : wrapper.get('[data-decision="deny"]').trigger('click');
      const secondSubmit = second === 'approve'
        ? wrapper.get('[data-testid="consent-form"]').trigger('submit')
        : wrapper.get('[data-decision="deny"]').trigger('click');
      await Promise.all([firstSubmit, secondSubmit]);
      expect(calls).toBe(1);
      release();
      await flushPromises();
    });

  it(
    'redirects to login when the session expires during a decision',
    async () => {
      consentMocks.decide.mockRejectedValue(
        new OAuthConsentFailure('session-required'),
      );
      const path = route('/authorize?x=1');
      const wrapper = await mountSuspended(AuthorizePage, { route: path });
      await flushPromises();
      await wrapper.get('[data-testid="consent-form"]').trigger('submit');
      await flushPromises();
      const target = vi.mocked(navigateTo).mock.calls[0]?.[0] as string;
      const currentPath = useRoute().fullPath;
      expect(target).toBe(`/login?next=${encodeURIComponent(currentPath)}`);
    });
});
describe('login next preservation', () => {
  it('keeps provider links bare when next is absent or invalid', async () => {
    const invalid = [
      '//evil',
      'https://evil',
      'javascript:alert(1)',
      `/ok?value=${'é'.repeat(1025)}`,
      'not-relative',
      '/\\evil',
    ];
    for (const next of [undefined, ...invalid]) {
      const path = next === undefined
        ? '/login'
        : `/login?next=${encodeURIComponent(next)}`;
      const wrapper = await mountSuspended(LoginPage, { route: path });
      await flushPromises();
      expect(wrapper.get('[href="/api/v1/auth/google/start"]').exists())
        .toBe(true);
      expect(wrapper.get('[href="/api/v1/auth/github/start"]').exists())
        .toBe(true);
      expect(wrapper.get('[href="/api/v1/auth/linkedin/start"]').exists())
        .toBe(true);
    }
    const arrayWrapper = await mountSuspended(LoginPage, {
      route: '/login?next=%2Fone&next=%2Ftwo',
    });
    await flushPromises();
    expect(arrayWrapper.get('[href="/api/v1/auth/google/start"]').exists())
      .toBe(true);
  });

  it.each([
    ['2048 bytes', `/${'a'.repeat(2047)}`, true],
    ['2049 bytes', `/${'a'.repeat(2048)}`, false],
    ['carriage return', '/authorize\rnext', false],
    ['line feed', '/authorize\nnext', false],
    ['malformed percent escape', '/authorize/%ZZ', false],
  ] as const)(
    'enforces the exact next boundary: %s',
    async (_name, next, valid) => {
      const wrapper = await mountSuspended(LoginPage, {
        route: `/login?next=${encodeURIComponent(next)}`,
      });
      await flushPromises();
      const hrefs = wrapper
        .findAll('[href]')
        .map((link) => link.attributes('href'));
      for (const provider of ['google', 'github', 'linkedin']) {
        const prefix = `/api/v1/auth/${provider}/start`;
        if (valid) {
          expect(hrefs).toContain(`${prefix}?next=${encodeURIComponent(next)}`);
        } else {
          expect(hrefs).toContain(prefix);
        }
      }
    },
  );

  it(
    'preserves a valid query next on password and provider paths',
    async () => {
      const next = '/authorize?x=1&y=two';
      const wrapper = await mountSuspended(LoginPage, {
        route: `/login?next=${encodeURIComponent(next)}`,
      });
      await flushPromises();
      const hrefs = wrapper
        .findAll('[href]')
        .map((link) => link.attributes('href'));
      for (const provider of ['google', 'github', 'linkedin']) {
        expect(hrefs).toContain(
          `/api/v1/auth/${provider}/start?next=${encodeURIComponent(next)}`,
        );
      }
    });

  it('returns a successful password login to valid next verbatim', async () => {
    registerEndpoint('/api/v1/auth/password/login', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 204);
        return null;
      },
    });
    const next = '/authorize?x=1';
    const wrapper = await mountSuspended(LoginPage, {
      route: `/login?next=${encodeURIComponent(next)}`,
    });
    await wrapper.get('[autocomplete="email"]').setValue(
      'ada@example.com',
    );
    await wrapper.get('[autocomplete="current-password"]')
      .setValue('correct horse battery staple');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');
    await flushPromises();
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(next);
  });

  it('round-trips an unauthenticated authorize request through password login',
    async () => {
      consentMocks.get.mockRejectedValue(
        new OAuthConsentFailure('session-required'),
      );
      const authorizePath = route('/authorize?x=1');
      await mountSuspended(AuthorizePage, { route: authorizePath });
      await flushPromises();
      const loginTarget = vi.mocked(navigateTo).mock.calls[0]?.[0] as string;
      expect(loginTarget).toMatch(/^\/login\?next=/);

      registerEndpoint('/api/v1/auth/password/login', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 204);
          return null;
        },
      });
      const login = await mountSuspended(LoginPage, { route: loginTarget });
      await login.get('[autocomplete="email"]').setValue(
        'ada@example.com',
      );
      await login.get('[autocomplete="current-password"]')
        .setValue('correct horse battery staple');
      await login.get('[data-testid="login-form"]').trigger('submit');
      await flushPromises();
      const expectedPath = new URL(
        loginTarget,
        'http://localhost',
      ).searchParams.get('next');
      expect(vi.mocked(navigateTo)).toHaveBeenLastCalledWith(expectedPath);
    });
});
