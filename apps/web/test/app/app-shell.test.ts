import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppShell from '../../app/components/app/AppShell.vue';

const me = {
  data: {
    user: {
      id: 'user-1',
      email: 'dev@aboutme.invalid',
      name: 'Dev User',
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: 'csrf',
    identities: [],
  },
};
let meStatus = 401;
mockNuxtImport('navigateTo', () => vi.fn());
registerEndpoint('/api/v1/me', (event) => {
  if (meStatus !== 200) {
    setResponseStatus(event, meStatus);
    return { error: { code: 'session_required', message: 'Sign in.' } };
  }
  return me;
});
registerEndpoint('/api/v1/auth/logout', {
  method: 'POST',
  handler: (event) => {
    setResponseStatus(event, 204);
    return null;
  },
});
function links(
  wrapper: Awaited<ReturnType<typeof mountSuspended>>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const a of wrapper.findAll('[href]')) {
    out[a.text().trim()] = a.attributes('href') ?? '';
  }
  return out;
}

describe('AppShell', () => {
  beforeEach(() => {
    clearNuxtData();
    vi.mocked(navigateTo).mockClear();
  });

  it('shows sign-in and registration while signed out', async () => {
    meStatus = 401;
    const wrapper = await mountSuspended(AppShell);
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(false);
  });
  it('shows the signed-out shell when /me returns a server error', async () => {
    meStatus = 500;
    const wrapper = await mountSuspended(AppShell);
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(false);
  });
  it('shows app navigation and account menu when authenticated', async () => {
    meStatus = 200;
    const originalName = me.data.user.name;
    me.data.user.name = '<img src=x onerror=alert(1)>';
    const wrapper = await mountSuspended(AppShell);
    await flushPromises();
    const found = links(wrapper);
    expect(found['Resumes']).toBe('/app/resumes');
    expect(found['Settings']).toBe('/app/settings/sessions');
    expect(found['Sign in']).toBeUndefined();
    expect(found['Create account']).toBeUndefined();
    expect(wrapper.get('[aria-label="Account menu"]').exists()).toBe(true);
    expect(
      document.body.querySelector('[data-testid="account-menu"] [onerror]'),
    ).toBeNull();
    me.data.user.name = originalName;
    wrapper.unmount();
  });
  it('navigates from account menu and logs out', async () => {
    meStatus = 200;
    const wrapper = await mountSuspended(AppShell);
    await flushPromises();
    await wrapper.get('[data-testid="account-menu"]').trigger('click');
    await flushPromises();
    document.body
      .querySelector<HTMLElement>('[data-testid="account-menu-settings"]')
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await flushPromises();
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
      '/app/settings/sessions',
    );

    await wrapper.get('[data-testid="account-menu"]').trigger('click');
    await flushPromises();
    document.body
      .querySelector<HTMLElement>('[data-testid="account-menu-logout"]')
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await flushPromises();
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/login');
  });
  it('moves signed-in theme control into the account menu', async () => {
    meStatus = 200;
    const wrapper = await mountSuspended(AppShell);
    await flushPromises();
    expect(wrapper.find('[aria-label^="Switch to"]').exists()).toBe(false);

    await wrapper.get('[aria-label="Account menu"]').trigger('click');
    await flushPromises();
    const toggle = document.body.querySelector<HTMLElement>(
      '[data-testid="theme-toggle"]',
    );
    expect(toggle).not.toBeNull();
    expect(toggle?.textContent).toMatch(/Dark theme|Light theme/);
    toggle?.click();
    await flushPromises();
    expect(document.documentElement.dataset.theme).toMatch(/dark|light/);
    wrapper.unmount();
  });
  it('keeps brand link and theme toggle in both states', async () => {
    meStatus = 401;
    const wrapper = await mountSuspended(AppShell);
    await flushPromises();
    expect(links(wrapper)['aboutme']).toBe('/');
    expect(wrapper.find('[aria-label^="Switch to"]').exists()).toBe(true);
  });
});
