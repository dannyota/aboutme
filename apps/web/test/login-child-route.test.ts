import { beforeEach, describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import { NuxtPage } from '#components';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

/**
 * `app/pages/login/second-factor.vue` makes `/login` the parent route of
 * `/login/second-factor`, so the pending page renders only if `login.vue`
 * renders a child outlet. Mounting either page component directly cannot see
 * that: these cases go through the router, exactly as a browser does.
 */

registerCapabilities();

// The pending row belongs to a real sign-in, which no test holds. The closed
// expired branch is the deterministic answer, and it proves the child page
// rendered its own content rather than an empty shell.
registerEndpoint('/api/v1/auth/second-factor', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'authentication_required', message: 'x' } };
});

const RouterOutlet = defineComponent({
  name: 'RouterOutlet',
  setup: () => () => h(NuxtPage),
});

describe('the login route tree', () => {
  beforeEach(() => setSiteLocale('en'));

  it('renders the pending page at /login/second-factor', async () => {
    const wrapper = await mountSuspended(RouterOutlet, {
      route: '/login/second-factor',
    });
    await flushPromises();

    expect(wrapper.find('[data-testid="second-factor-page"]').exists())
      .toBe(true);
    expect(wrapper.find('[data-testid="second-factor-expired"]').exists())
      .toBe(true);
    // The parent must step aside: a pending sign-in is not a sign-in form.
    expect(wrapper.find('[data-testid="login-form"]').exists()).toBe(false);
  });

  it('renders the sign-in form at /login', async () => {
    const wrapper = await mountSuspended(RouterOutlet, { route: '/login' });
    await flushPromises();

    expect(wrapper.find('[data-testid="login-form"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="second-factor-page"]').exists())
      .toBe(false);
  });
});
