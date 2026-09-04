import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import AppShell from '../app/components/app/AppShell.vue';

mockNuxtImport('navigateTo', () => vi.fn());

const meData = {
  user: {
    id: 'user-1',
    email: 'dev@aboutme.invalid',
    name: 'Dev User',
    avatarKey: null,
    hasPassword: true,
  },
  csrfToken: 'csrf',
  identities: [{ provider: 'google' as const }],
};

let meReads = 0;
registerEndpoint('/api/v1/me', {
  method: 'GET',
  handler: () => {
    meReads += 1;
    return { data: meData };
  },
});

registerEndpoint('/api/v1/auth/logout', {
  method: 'POST',
  handler: (event) => {
    setResponseStatus(event, 204);
    return null;
  },
});

const Harness = defineComponent({
  setup() {
    const { user, csrfToken, identities, authState, logout } = useAuth();
    return { user, csrfToken, identities, authState, logout };
  },
  render() {
    return h('div', [
      h(AppShell),
      h('span', { 'data-testid': 'user' }, JSON.stringify(this.user)),
      h(
        'span',
        {
          'data-testid': 'csrf',
        },
        JSON.stringify(this.csrfToken),
      ),
      h(
        'span',
        {
          'data-testid': 'identities',
        },
        JSON.stringify(this.identities),
      ),
      h('span', { 'data-testid': 'auth-state' }, this.authState),
      h(
        'button',
        {
          'data-testid': 'logout',
          'onClick': () => this.logout(),
        },
        'Log out',
      ),
    ]);
  },
});

describe('logout state transition', () => {
  it('clears shared auth state before navigating after logout', async () => {
    const wrapper = await mountSuspended(Harness, { route: '/app/resumes' });
    await flushPromises();
    const readsBeforeLogout = meReads;

    expect(wrapper.get('[data-testid="user"]').text()).toContain('Dev User');
    expect(wrapper.get('[data-testid="account-menu"]').attributes(
      'aria-label',
    )).toBe(
      'Account menu',
    );
    expect(wrapper.get('[data-testid="auth-state"]').text()).toBe(
      'authenticated',
    );

    await wrapper.get('[data-testid="logout"]').trigger('click');
    await flushPromises();
    await flushPromises();

    expect(wrapper.get('[data-testid="user"]').text()).toBe('null');
    expect(wrapper.get('[data-testid="csrf"]').text()).toBe('null');
    expect(wrapper.get('[data-testid="identities"]').text()).toBe('[]');
    expect(wrapper.get('[data-testid="auth-state"]').text()).toBe('anonymous');
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(false);
    expect(wrapper.get('[href="/login"]').text()).toContain('Sign in');
    expect(wrapper.get('[href="/register"]').text()).toContain(
      'Create account',
    );
    expect(meReads).toBe(readsBeforeLogout);
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
  });
});
