import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseHeader, setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import AppShell from '../app/components/app/AppShell.vue';
import LocaleToggle from '../app/components/app/LocaleToggle.vue';
import { localeCookie } from '../app/i18n/locale';
import { setSiteLocale } from './support/locale';

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
    setResponseHeader(event, 'Clear-Site-Data', '"cookies", "storage"');
    setResponseStatus(event, 204);
    // The real browser applies this header itself and wipes every cookie
    // for this origin, including `aboutme-locale`, before the client's
    // response handler runs (docs/design/security.md, session lifecycle).
    // Simulated here so the reload assertion below is a real regression
    // check: without useAuth's restore hook, it would fail.
    document.cookie = `${localeCookie}=; Max-Age=0; Path=/`;
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
    setSiteLocale('en');
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
    // /app/resumes is an authRequiredPath (AppShell.vue), and the real
    // navigateTo('/login') asserted below would already have left it: the
    // header shows neither the account menu nor the signed-out links while
    // this now-anonymous state is still rendered here.
    expect(wrapper.find('[href="/login"]').exists()).toBe(false);
    expect(wrapper.find('[href="/register"]').exists()).toBe(false);
    expect(meReads).toBe(readsBeforeLogout);
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');

    // A reload after logout starts a fresh app instance that reads only
    // the cookie the restore hook just rewrote, not the in-memory locale
    // state Clear-Site-Data cannot touch (docs/design/localization.md,
    // "Two language domains"). Without the hook, the handler above leaves
    // the cookie cleared and this would render Vietnamese by default.
    clearNuxtState(localeCookie);
    const reloaded = await mountSuspended(LocaleToggle, {
      props: { label: 'Language' },
    });
    expect(reloaded.get('[lang="en"]').attributes('aria-pressed')).toBe(
      'true',
    );
  });
});
