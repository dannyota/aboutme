import { describe, expect, it } from 'vitest';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import LoginPage from '../app/pages/login.vue';

describe('login.vue', () => {
  it('renders a real top-level link for each OAuth provider', async () => {
    const wrapper = await mountSuspended(LoginPage);

    const google = wrapper.get('a[href="/api/v1/auth/google/start"]');
    const github = wrapper.get('a[href="/api/v1/auth/github/start"]');
    const linkedin = wrapper.get(
      'a[href="/api/v1/auth/linkedin/start"]',
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
