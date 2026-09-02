import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppChrome from '../app/components/ui/AppChrome.vue';

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
registerEndpoint('/api/v1/me', (event) => {
  if (meStatus !== 200) {
    setResponseStatus(event, meStatus);
    return { error: { code: 'session_required', message: 'Sign in.' } };
  }
  return me;
});

function links(
  wrapper: Awaited<ReturnType<typeof mountSuspended>>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const a of wrapper.findAll('a')) {
    out[a.text().trim()] = a.attributes('href') ?? '';
  }
  return out;
}

describe('AppChrome', () => {
  it('shows sign-in and registration while signed out', async () => {
    meStatus = 401;
    const wrapper = await mountSuspended(AppChrome);
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('.account-control').exists()).toBe(false);
  });

  it('shows the signed-out shell when /me returns a server error', async () => {
    meStatus = 500;
    const wrapper = await mountSuspended(AppChrome);
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('.account-control').exists()).toBe(false);
  });

  it(
    'shows app navigation and the account control when authenticated',
    async () => {
      meStatus = 200;
      const wrapper = await mountSuspended(AppChrome);
      await flushPromises();
      const found = links(wrapper);
      expect(found['Resumes']).toBe('/app/resumes');
      expect(found['Settings']).toBe('/app/settings/sessions');
      expect(found['Sign in']).toBeUndefined();
      expect(found['Create account']).toBeUndefined();
      expect(wrapper.get('.account-control').text()).toContain('Dev User');
    },
  );

  it('keeps the brand link and theme toggle in both states', async () => {
    meStatus = 401;
    const wrapper = await mountSuspended(AppChrome);
    await flushPromises();
    expect(links(wrapper)['aboutme']).toBe('/');
    expect(wrapper.find('button[aria-label^="Switch to"]').exists()).toBe(true);
  });
});
