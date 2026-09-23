import { beforeEach, describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import { NuxtPage } from '#components';
import LoginPage from '../app/pages/login.vue';
import SecondFactorPage from '../app/pages/login/second-factor.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';
import {
  clearNavigationInFlight,
  landedOn,
  markNavigationInFlight,
} from './support/routing';

/**
 * Where a finished sign-in actually lands, read from the router rather than
 * from the helper it was asked with.
 *
 * Nuxt's `navigateTo` declines to move the client router while another
 * navigation is still settling: it returns the path it was given and does
 * nothing, so the page stays put with no error to show. Asserting which path
 * `navigateTo` was called with passes either way, so these cases drive the
 * real router and read where it stopped.
 *
 * A destination outside this app's own pages still goes through `navigateTo`
 * with `external: true`, which that rule does not touch; its coverage stays
 * in test/login.test.ts and test/second-factor-login.test.ts, where the
 * helper is stubbed and no real browser navigation is attempted.
 */

const CSRF_TOKEN = 'A'.repeat(43);

registerCapabilities();

let enrolled = false;

registerEndpoint('/api/v1/auth/password/login', {
  method: 'POST',
  handler: (event) => {
    if (!enrolled) {
      setResponseStatus(event, 204);
      return null;
    }
    setResponseStatus(event, 202);
    return { data: { secondFactorRequired: true } };
  },
});

registerEndpoint('/api/v1/auth/second-factor', {
  method: 'GET',
  handler: () => ({
    data: {
      purpose: 'login',
      methods: ['recovery'],
      expiresAt: '2026-09-20T09:05:00Z',
      returnPath: '/app/resumes',
      csrfToken: CSRF_TOKEN,
    },
  }),
});

registerEndpoint('/api/v1/auth/second-factor/recovery/verify', {
  method: 'POST',
  handler: (event) => {
    setResponseStatus(event, 204);
    return null;
  },
});

/** Renders whatever the current route resolves to, as `app.vue` does. */
const RouterOutlet = defineComponent({
  name: 'RouterOutlet',
  setup: () => () => h(NuxtPage),
});

describe('password sign-in landing', () => {
  beforeEach(() => {
    setSiteLocale('en');
    enrolled = false;
    clearNavigationInFlight();
  });

  it('leaves the login page for the app after a 204', async () => {
    const wrapper = await mountSuspended(LoginPage, { route: '/login' });
    await wrapper.get('[autocomplete="email"]').setValue('ada@example.com');
    await wrapper.get('[autocomplete="current-password"]')
      .setValue('correct horse battery staple');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');

    expect(await landedOn('/app/resumes')).toBe('/app/resumes');
  });

  it('leaves the login page for the app after a 204 while another '
    + 'navigation is still settling', async () => {
    const wrapper = await mountSuspended(LoginPage, { route: '/login' });
    markNavigationInFlight();
    await wrapper.get('[autocomplete="email"]').setValue('ada@example.com');
    await wrapper.get('[autocomplete="current-password"]')
      .setValue('correct horse battery staple');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');

    expect(await landedOn('/app/resumes')).toBe('/app/resumes');
  });

  it('reaches the pending page after a 202 while another navigation is '
    + 'still settling', async () => {
    enrolled = true;
    const wrapper = await mountSuspended(RouterOutlet, { route: '/login' });
    markNavigationInFlight();
    await wrapper.get('[autocomplete="email"]').setValue('ada@example.com');
    await wrapper.get('[autocomplete="current-password"]')
      .setValue('correct horse battery staple');
    await wrapper.get('[data-testid="login-form"]').trigger('submit');

    expect(await landedOn('/login/second-factor')).toBe('/login/second-factor');
    await flushPromises();
    expect(wrapper.find('[data-testid="second-factor-page"]').exists())
      .toBe(true);
  });
});

describe('second-factor completion landing', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNavigationInFlight();
  });

  it('leaves the pending page for the return path while another '
    + 'navigation is still settling', async () => {
    const wrapper = await mountSuspended(SecondFactorPage, {
      route: '/login/second-factor',
    });
    await flushPromises();
    markNavigationInFlight();
    await wrapper.get('#second-factor-recovery-code')
      .setValue('amr_00000-00000-00000-00000-00000-0');
    await wrapper.get('[data-testid="second-factor-recovery-form"]')
      .trigger('submit');

    expect(await landedOn('/app/resumes')).toBe('/app/resumes');
  });
});
