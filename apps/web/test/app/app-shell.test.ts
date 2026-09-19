import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppShell from '../../app/components/app/AppShell.vue';
import { setSiteLocale } from '../support/locale';

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
// The homepage and account pages localize the shell, so default to an
// English-only route.
function mountShell(route = '/app/resumes') {
  return mountSuspended(AppShell, { route });
}
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
    setSiteLocale(undefined);
    clearNuxtData();
    vi.mocked(navigateTo).mockClear();
  });

  it('shows sign-in and registration while signed out', async () => {
    meStatus = 401;
    const wrapper = await mountShell();
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Templates']).toBe('/templates');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(false);
  });
  it('shows the signed-out shell when /me returns a server error', async () => {
    meStatus = 500;
    const wrapper = await mountShell();
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
    const wrapper = await mountShell();
    await flushPromises();
    const found = links(wrapper);
    expect(found['Resumes']).toBe('/app/resumes');
    expect(found['Settings']).toBe('/app/settings/sessions');
    expect(found['Templates']).toBe('/templates');
    expect(found['Sign in']).toBeUndefined();
    expect(found['Create account']).toBeUndefined();
    expect(wrapper.get('[aria-label="Account menu"]').exists()).toBe(true);
    expect(
      document.body.querySelector('[data-testid="account-menu"] [onerror]'),
    ).toBeNull();
    me.data.user.name = originalName;
    wrapper.unmount();
  });
  it('hides Settings but keeps Resumes on phones when signed in', async () => {
    meStatus = 200;
    const wrapper = await mountShell();
    await flushPromises();
    const resumes = wrapper.findAll('a')
      .find((a) => a.attributes('href') === '/app/resumes');
    const settings = wrapper.findAll('a')
      .find((a) => a.attributes('href') === '/app/settings/sessions');
    expect(resumes?.classes()).not.toContain('max-sm:hidden');
    expect(settings?.classes()).toContain('max-sm:hidden');
    // The account menu (tested below) keeps Settings one tap away on phones.
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it(
    'hides Templates on phones when signed in, keeps it when signed out',
    async () => {
      meStatus = 200;
      const signedInWrapper = await mountShell();
      await flushPromises();
      const signedInTemplates = signedInWrapper.findAll('a')
        .find((a) => a.attributes('href') === '/templates');
      expect(signedInTemplates?.classes()).toContain('max-sm:hidden');
      signedInWrapper.unmount();

      meStatus = 401;
      clearNuxtData();
      const signedOutWrapper = await mountShell();
      await flushPromises();
      const signedOutTemplates = signedOutWrapper.findAll('a')
        .find((a) => a.attributes('href') === '/templates');
      expect(signedOutTemplates?.classes()).not.toContain('max-sm:hidden');
    },
  );

  it('navigates from account menu and logs out', async () => {
    meStatus = 200;
    const wrapper = await mountShell();
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
    const wrapper = await mountShell();
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
    const wrapper = await mountShell();
    await flushPromises();
    expect(links(wrapper)['aboutme']).toBe('/');
    expect(wrapper.find('[aria-label^="Switch to"]').exists()).toBe(true);
  });

  it('speaks the homepage language in the signed-out shell on /', async () => {
    meStatus = 401;
    const wrapper = await mountShell('/');
    await flushPromises();
    const found = links(wrapper);
    expect(found['Đăng nhập']).toBe('/login');
    expect(found['Tạo tài khoản']).toBe('/register');
    const theme = wrapper.get('[aria-label^="Chuyển sang chế độ"]');
    expect(theme.text()).toMatch(/Chế độ (sáng|tối)/);

    setSiteLocale('en');
    const english = await mountShell('/');
    await flushPromises();
    expect(links(english)['Sign in']).toBe('/login');
    expect(english.find('[aria-label^="Switch to"]').exists()).toBe(true);
  });
  it('keeps English and no language toggle elsewhere', async () => {
    meStatus = 401;
    setSiteLocale('vi');
    const wrapper = await mountShell('/app/resumes');
    await flushPromises();
    expect(links(wrapper)['Sign in']).toBe('/login');
    expect(wrapper.find('[data-testid="landing-locale"]').exists()).toBe(false);
  });
  it('offers the language toggle on / when signed in', async () => {
    meStatus = 200;
    const wrapper = await mountShell('/');
    await flushPromises();
    expect(wrapper.find('[data-testid="landing-locale"]').exists()).toBe(true);
    expect(wrapper.find('[aria-label="Account menu"]').exists()).toBe(true);
    wrapper.unmount();
  });
  it('localizes the shell on the account pages', async () => {
    meStatus = 401;
    for (const route of ['/login', '/register', '/forgot-password']) {
      setSiteLocale(undefined);
      const wrapper = await mountShell(route);
      await flushPromises();
      expect(links(wrapper)['Tạo tài khoản']).toBe('/register');
      expect(wrapper.find('[data-testid="landing-locale"]').exists()).toBe(
        true,
      );
    }
  });

  it(
    'carries a valid next from /login onto the header register link',
    async () => {
      meStatus = 401;
      setSiteLocale('en');
      const next = '/app/new?sample=ats-plain&lng=vi';
      const wrapper = await mountShell(
        `/login?next=${encodeURIComponent(next)}`,
      );
      await flushPromises();
      const found = links(wrapper);
      expect(found['Create account']).toBe(
        `/register?next=${encodeURIComponent(next)}`,
      );
      expect(found['Sign in']).toBe(`/login?next=${encodeURIComponent(next)}`);
    },
  );

  it(
    'carries a valid next from /register onto the header sign-in link',
    async () => {
      meStatus = 401;
      setSiteLocale('en');
      const next = '/app/resumes';
      const wrapper = await mountShell(
        `/register?next=${encodeURIComponent(next)}`,
      );
      await flushPromises();
      const found = links(wrapper);
      expect(found['Sign in']).toBe(`/login?next=${encodeURIComponent(next)}`);
      expect(found['Create account']).toBe(
        `/register?next=${encodeURIComponent(next)}`,
      );
    },
  );

  it('drops a hostile next from the header links', async () => {
    meStatus = 401;
    setSiteLocale('en');
    const wrapper = await mountShell(
      `/login?next=${encodeURIComponent('//evil.example')}`,
    );
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
  });

  it(
    'marks the Templates link current on the gallery and its pages',
    async () => {
      meStatus = 401;
      for (const route of ['/templates', '/templates/engineer-compact']) {
        const wrapper = await mountShell(route);
        await flushPromises();
        const templatesLink = wrapper.findAll('a')
          .find((a) => a.attributes('href') === '/templates');
        expect(templatesLink?.attributes('aria-current')).toBe('page');
      }
      const elsewhere = await mountShell('/app/resumes');
      await flushPromises();
      const templatesLink = elsewhere.findAll('a')
        .find((a) => a.attributes('href') === '/templates');
      expect(templatesLink?.attributes('aria-current')).toBeUndefined();
    },
  );

  it('speaks Vietnamese for Templates on the localized gallery', async () => {
    meStatus = 401;
    setSiteLocale('vi');
    const wrapper = await mountShell('/templates');
    await flushPromises();
    expect(links(wrapper)['Mẫu']).toBe('/templates');
  });

  it(
    'speaks Vietnamese for Resumes and Settings on a localized page',
    async () => {
      meStatus = 200;
      setSiteLocale('vi');
      const wrapper = await mountShell('/templates');
      await flushPromises();
      const found = links(wrapper);
      expect(found['CV']).toBe('/app/resumes');
      expect(found['Cài đặt']).toBe('/app/settings/sessions');
      expect(found['Resumes']).toBeUndefined();
      expect(found['Settings']).toBeUndefined();
      wrapper.unmount();
    },
  );

  it(
    'keeps Resumes and Settings in English on the unlocalized app pages',
    async () => {
      meStatus = 200;
      setSiteLocale('vi');
      const wrapper = await mountShell('/app/resumes');
      await flushPromises();
      const found = links(wrapper);
      expect(found['Resumes']).toBe('/app/resumes');
      expect(found['Settings']).toBe('/app/settings/sessions');
      wrapper.unmount();
    },
  );

  it(
    'shortens locale labels on phones without changing the accessible name',
    async () => {
      meStatus = 401;
      const wrapper = await mountShell('/');
      await flushPromises();
      const vi = wrapper.get('[data-testid="landing-locale-vi"]');
      const en = wrapper.get('[data-testid="landing-locale-en"]');
      expect(vi.attributes('aria-label')).toBe('Tiếng Việt');
      expect(en.attributes('aria-label')).toBe('English');
      expect(vi.text()).toContain('VI');
      expect(vi.text()).toContain('Tiếng Việt');
      expect(en.text()).toContain('EN');
      expect(en.text()).toContain('English');
    },
  );

  it('ignores next outside /login and /register', async () => {
    meStatus = 401;
    setSiteLocale('en');
    const wrapper = await mountShell(
      `/templates?next=${encodeURIComponent('/app/resumes')}`,
    );
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
  });
});
