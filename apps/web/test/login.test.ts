import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { nextTick } from 'vue';
import { setResponseStatus } from 'h3';
import LoginPage from '../app/pages/login.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

// login() navigates to /app/resumes on success — stub it so tests can assert
// the target without a real page transition tearing the mounted wrapper down.
mockNuxtImport('navigateTo', () => vi.fn());

describe('login.vue', () => {
  it('renders a real top-level link for each OAuth provider', async () => {
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();

    const google = wrapper.get('[href="/api/v1/auth/google/start"]');
    const github = wrapper.get('[href="/api/v1/auth/github/start"]');
    const linkedin = wrapper.get(
      '[href="/api/v1/auth/linkedin/start"]',
    );

    // Plain <a> tags, never JS-driven navigation: the start endpoint sets
    // a cookie and redirects, which needs a real top-level navigation.
    expect(google.element.tagName).toBe('A');
    expect(github.element.tagName).toBe('A');
    expect(linkedin.element.tagName).toBe('A');
  });

  it('shows no error banner when there is no ?error= query param', async () => {
    const wrapper = await mountSuspended(LoginPage);

    expect(wrapper.find('[data-testid="login-error"]').exists()).toBe(
      false,
    );
  });

  it('shows a generic message for auth_failed', async () => {
    const wrapper = await mountSuspended(LoginPage, {
      route: '/login?error=auth_failed',
    });

    const banner = wrapper.get('[data-testid="login-error"]');
    expect(banner.text()).toContain('Something went wrong');
  });

  it('shows a specific message for email_not_verified', async () => {
    const wrapper = await mountSuspended(LoginPage, {
      route: '/login?error=email_not_verified',
    });

    const banner = wrapper.get('[data-testid="login-error"]');
    expect(banner.text()).toContain('verified');
  });

  it('shows a neutral message for cancelled', async () => {
    const wrapper = await mountSuspended(LoginPage, {
      route: '/login?error=cancelled',
    });

    const banner = wrapper.get('[data-testid="login-error"]');
    expect(banner.text()).toContain('cancelled');
  });

  it('shows a provider-neutral message for email_already_registered',
    async () => {
      const wrapper = await mountSuspended(LoginPage, {
        route: '/login?error=email_already_registered',
      });

      const banner = wrapper.get('[data-testid="login-error"]');
      expect(banner.text()).toContain('already exists');
      // Must not name the existing provider (spec: targeted-phishing hint).
      expect(banner.text().toLowerCase()).not.toMatch(
        /google|github|linkedin/,
      );
    });

  it('falls back to the generic message for an unknown error code',
    async () => {
      const wrapper = await mountSuspended(LoginPage, {
        route: '/login?error=some_unrecognized_code',
      });

      const banner = wrapper.get('[data-testid="login-error"]');
      expect(banner.text()).toContain('Something went wrong');
    });

  it('falls back for the prototype object error code', async () => {
    const wrapper = await mountSuspended(LoginPage, {
      route: '/login?error=__proto__',
    });

    expect(wrapper.get('[data-testid="login-error"]').text()).toContain(
      'Something went wrong',
    );
  });

  it('composes the auth card, shared banner, and generated inputs',
    async () => {
      const wrapper = await mountSuspended(LoginPage, {
        route: '/login?error=cancelled',
      });
      await flushPromises();

      expect(wrapper.find('[data-slot="card"]').exists()).toBe(true);
      expect(
        wrapper.get('[data-testid="login-error"]').attributes('role'),
      ).toBe(
        'alert',
      );
      expect(
        wrapper
          .findAll('[data-slot="input"]')
          .every((input) => input.attributes('id') !== undefined),
      ).toBe(true);
    });

  it('does not resolve a prototype property as a valid error code',
    async () => {
      // A plain `errorMessages[code]` lookup resolves inherited
      // properties too — `?error=constructor` would otherwise render
      // `Object`'s constructor function instead of falling back.
      const wrapper = await mountSuspended(LoginPage, {
        route: '/login?error=constructor',
      });

      const banner = wrapper.get('[data-testid="login-error"]');
      expect(banner.text()).toContain('Something went wrong');
    });
});
describe('login.vue password form', () => {
  beforeEach(() => {
    vi.mocked(navigateTo).mockClear();
  });

  it('adds email and current-password fields alongside the provider anchors',
    async () => {
      const wrapper = await mountSuspended(LoginPage);
      await flushPromises();
      expect(wrapper.get('[autocomplete="email"]').exists()).toBe(true);
      expect(wrapper.get('[autocomplete="current-password"]').exists())
        .toBe(true);
      // Provider anchors remain real top-level navigation links.
      expect(wrapper.get('[href="/api/v1/auth/google/start"]').exists())
        .toBe(true);
      expect(wrapper.get('[href="/api/v1/auth/github/start"]').exists())
        .toBe(true);
      expect(wrapper.get('[href="/api/v1/auth/linkedin/start"]').exists())
        .toBe(true);
    });

  it('links to the forgot-password and register pages', async () => {
    const wrapper = await mountSuspended(LoginPage);
    const hrefs = wrapper.findAll('[href]').map((a) => a.attributes('href'));
    expect(hrefs).toContain('/forgot-password');
    expect(hrefs).toContain('/register');
  });

  it('navigates to /app/resumes after a successful login', async () => {
    registerEndpoint('/api/v1/auth/password/login', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 204);
        return null;
      },
    });
    const wrapper = await mountSuspended(LoginPage);
    await wrapper.get('[autocomplete="email"]')
      .setValue('ada@example.com');
    await wrapper.get('[autocomplete="current-password"]')
      .setValue('correct horse battery staple');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');
    await flushPromises();
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/app/resumes');
  });

  it('shows closed copy and does not navigate on authentication-failed',
    async () => {
      registerEndpoint('/api/v1/auth/password/login', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 401);
          return { error: { code: 'authentication_failed', message: 'x' } };
        },
      });
      const wrapper = await mountSuspended(LoginPage);
      await wrapper.get('[autocomplete="email"]')
        .setValue('ada@example.com');
      await wrapper.get('[autocomplete="current-password"]')
        .setValue('wrong');
      await wrapper.get('[data-testid="login-form"]').trigger('submit');
      await flushPromises();
      expect(wrapper.get('[data-testid="login-form-error"]').text())
        .toContain('Invalid email or password');
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    });

  it('shows closed copy when the service is unavailable', async () => {
    registerEndpoint('/api/v1/auth/password/login', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 503);
        return {
          error: { code: 'authentication_unavailable', message: 'x' },
        };
      },
    });
    const wrapper = await mountSuspended(LoginPage);
    await wrapper.get('[autocomplete="email"]')
      .setValue('ada@example.com');
    await wrapper.get('[autocomplete="current-password"]')
      .setValue('x');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');
    await flushPromises();
    expect(wrapper.get('[data-testid="login-form-error"]').text())
      .toContain('Something went wrong');
  });

  it('disables submit and shows pending text while the request is in flight',
    async () => {
      let resolveRequest: () => void = () => {};
      registerEndpoint('/api/v1/auth/password/login', {
        method: 'POST',
        handler: (event) => new Promise<void>((resolve) => {
          resolveRequest = () => {
            setResponseStatus(event, 204);
            resolve();
          };
        }),
      });
      const wrapper = await mountSuspended(LoginPage);
      await wrapper.get('[autocomplete="email"]')
        .setValue('ada@example.com');
      await wrapper.get('[autocomplete="current-password"]')
        .setValue('correct horse battery staple');
      await wrapper.get('[data-testid="login-form"]').trigger('submit');
      await flushPromises();
      const button = wrapper.get('[data-slot="button"][type="submit"]');
      expect(button.attributes('disabled')).toBeDefined();
      expect(button.text()).toContain('Signing in');
      resolveRequest();
      await flushPromises();
    });

  it('does not retain the password after a successful login', async () => {
    registerEndpoint('/api/v1/auth/password/login', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 204);
        return null;
      },
    });
    const wrapper = await mountSuspended(LoginPage);
    const passwordInput = wrapper.get(
      '[autocomplete="current-password"]',
    );
    await wrapper.get('[autocomplete="email"]')
      .setValue('ada@example.com');
    await passwordInput.setValue('correct horse battery staple');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');
    await flushPromises();
    await nextTick();
    expect((passwordInput.element as HTMLInputElement).value).toBe('');
    expect(wrapper.html()).not.toContain('correct horse battery staple');
  });
});

describe('login.vue provider gating', () => {
  beforeEach(() => {
    clearNuxtData();
  });

  it('renders no provider link or divider when providerLogin is false',
    async () => {
      registerCapabilities({ providerLogin: false, agentAccess: false });
      const wrapper = await mountSuspended(LoginPage);
      await flushPromises();
      expect(wrapper.find('[href^="/api/v1/auth/"]').exists()).toBe(false);
      expect(
        wrapper.find('[data-testid="login-divider"]').exists(),
      ).toBe(false);
      // The password form is unconditional.
      expect(wrapper.find('[data-testid="login-form"]').exists()).toBe(true);
    });

  it('renders no provider link when the capabilities read fails', async () => {
    registerCapabilities(null);
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(wrapper.find('[href^="/api/v1/auth/"]').exists()).toBe(false);
  });

  it('renders no provider link while the capabilities read is pending',
    async () => {
      let release!: (body: unknown) => void;
      registerEndpoint('/api/v1/capabilities', () => new Promise((resolve) => {
        release = () => resolve({
          data: { providerLogin: true, agentAccess: true },
        });
      }));
      const wrapper = await mountSuspended(LoginPage);
      expect(wrapper.find('[href^="/api/v1/auth/"]').exists()).toBe(false);
      expect(
        wrapper.find('[data-testid="login-divider"]').exists(),
      ).toBe(false);
      release({
        data: { providerLogin: true, agentAccess: true },
      });
      await flushPromises();
      await flushPromises();
      expect(wrapper.findAll('[href^="/api/v1/auth/"]')).toHaveLength(3);
    });

  it(
    'renders the provider links after providerLogin resolves true',
    async () => {
      registerCapabilities({ providerLogin: true, agentAccess: false });
      const wrapper = await mountSuspended(LoginPage);
      await flushPromises();
      expect(wrapper.findAll('[href^="/api/v1/auth/"]')).toHaveLength(3);
    },
  );
});
