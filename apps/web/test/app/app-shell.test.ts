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
    setSiteLocale('en');
    clearNuxtData();
    vi.mocked(navigateTo).mockClear();
  });

  it('shows sign-in and registration while signed out', async () => {
    meStatus = 401;
    // Not an authRequiredPath: /app/resumes (mountShell's default) redirects
    // an anonymous visitor away, so it never keeps a settled signed-out
    // header the way this generic route does.
    const wrapper = await mountShell('/forgot-password');
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Library']).toBe('/templates');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(false);
  });
  it('shows the signed-out shell when /me returns a server error', async () => {
    meStatus = 500;
    const wrapper = await mountShell('/forgot-password');
    // A 500 retries once (ofetch's default for GET), so settling takes an
    // extra round trip past a single flushPromises().
    await vi.waitFor(() => expect(links(wrapper)['Sign in']).toBe('/login'));
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create account']).toBe('/register');
    expect(found['Resumes']).toBeUndefined();
    expect(found['Settings']).toBeUndefined();
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(false);
  });
  it.each([
    '/app/settings/sessions',
    '/authorize',
    '/app/resumes',
    '/app/new',
  ])(
    'shows no signed-out account links while /me is still loading on %s',
    async (route) => {
      // These routes redirect an anonymous visitor to /login, so a
      // signed-out header is never their real state, only a transient one
      // while /me is in flight (useAuth.ts's 'loading' authState). A
      // never-resolving handler holds that transient state still so the
      // assertion below cannot race the mocked fetch.
      const unregisterMe = registerEndpoint(
        '/api/v1/me',
        () => new Promise(() => {}),
      );
      try {
        const wrapper = await mountShell(route);
        await flushPromises();
        const found = links(wrapper);
        expect(found['Sign in']).toBeUndefined();
        expect(found['Create account']).toBeUndefined();
        expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(
          false,
        );
        wrapper.unmount();
      } finally {
        // A failed assertion above must not leave this handler active: every
        // later test's own /me read would hang against it too.
        unregisterMe();
      }
    },
  );
  it.each([
    '/app/settings/sessions',
    '/authorize',
    '/app/resumes',
    '/app/new',
  ])(
    'never shows signed-out account links on %s once /me settles to a 401',
    async (route) => {
      // These routes redirect an anonymous visitor to /login, so a header
      // that shows the signed-out links only once /me has settled (rather
      // than just while it is loading) still renders them for the moment
      // between that settling and the redirect landing — the production
      // regression this guards.
      meStatus = 401;
      const wrapper = await mountShell(route);
      expect(links(wrapper)['Sign in']).toBeUndefined();
      expect(links(wrapper)['Create account']).toBeUndefined();
      await flushPromises();
      const found = links(wrapper);
      expect(found['Sign in']).toBeUndefined();
      expect(found['Create account']).toBeUndefined();
      expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(
        false,
      );
      wrapper.unmount();
    },
  );
  it('keeps signed-out account links immediate on / while /me is still '
    + 'loading', async () => {
    const unregisterMe = registerEndpoint(
      '/api/v1/me',
      () => new Promise(() => {}),
    );
    try {
      const wrapper = await mountShell('/');
      await flushPromises();
      const found = links(wrapper);
      expect(found['Sign in']).toBe('/login');
      // '/' is a marketing path (AppShell.vue's onMarketingPath), so the CTA
      // reads "Create your resume" there, not "Create account".
      expect(found['Create your resume']).toBe('/register');
      wrapper.unmount();
    } finally {
      unregisterMe();
    }
  });
  // /app/settings/sessions and /authorize once kept the signed-out links
  // unhidden on phones (hidePhoneAccountLinks), for a signed-out visitor who
  // had no other in-page account affordance there. Both are authRequiredPath
  // routes, so showSignedOutLinks now hides those links outright, and that
  // exception never renders.
  it.each(['/', '/login', '/privacy'])(
    'keeps signed-out account links compact on phones at %s',
    async (route) => {
      meStatus = 401;
      const wrapper = await mountShell(route);
      await flushPromises();
      const signIn = wrapper.findAll('a')
        .find((link) => link.attributes('href') === '/login');
      const createAccount = wrapper.findAll('a')
        .find((link) => link.attributes('href') === '/register');

      expect(signIn?.classes()).toContain('max-[44rem]:hidden');
      expect(createAccount?.classes()).toContain('max-[44rem]:hidden');
      wrapper.unmount();
    },
  );
  it('shows app navigation and account menu when authenticated', async () => {
    meStatus = 200;
    const originalName = me.data.user.name;
    me.data.user.name = '<img src=x onerror=alert(1)>';
    const wrapper = await mountShell();
    await flushPromises();
    const found = links(wrapper);
    expect(found['Resumes']).toBe('/app/resumes');
    expect(found['Settings']).toBe('/app/settings/sessions');
    expect(found['Library']).toBe('/templates');
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
    'hides the Library link on phones when signed in, keeps it when signed out',
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
    expect(wrapper.find('[aria-label^="Switch to"]').exists()).toBe(true);
  });

  it('links the header brand mark to / with an aboutme accessible name',
    async () => {
      meStatus = 401;
      const wrapper = await mountShell();
      await flushPromises();
      const brand = wrapper.findAll('a')
        .find((a) => a.attributes('href') === '/');
      expect(brand).toBeDefined();
      const svg = brand?.find('svg');
      expect(svg?.exists()).toBe(true);
      expect(svg?.attributes('role')).toBe('img');
      const name = svg?.attributes('aria-label')
        ?? svg?.element.querySelector(':scope > title')?.textContent?.trim();
      expect(name).toBe('aboutme');
    });

  it('speaks the homepage language in the signed-out shell on /', async () => {
    meStatus = 401;
    setSiteLocale('vi');
    const wrapper = await mountShell('/');
    await flushPromises();
    const found = links(wrapper);
    expect(found['Đăng nhập']).toBe('/login');
    expect(found['Tạo CV của bạn']).toBe('/register');
    const theme = wrapper.get('[aria-label^="Chuyển sang chế độ"]');
    expect(theme.text()).toMatch(/Chế độ (sáng|tối)/);

    setSiteLocale('en');
    const english = await mountShell('/');
    await flushPromises();
    expect(links(english)['Sign in']).toBe('/login');
    expect(english.find('[aria-label^="Switch to"]').exists()).toBe(true);
  });
  it('localizes the workspace shell and offers its language toggle',
    async () => {
      meStatus = 401;
      setSiteLocale('vi');
      const wrapper = await mountShell('/app/resumes');
      await flushPromises();
      // /app/resumes is an authRequiredPath, so its header keeps no
      // signed-out links once /me settles; Library is the localized text
      // this signed-out shell still shows there.
      expect(links(wrapper)['Thư viện']).toBe('/templates');
      expect(wrapper.find('[data-testid="landing-locale"]').exists()).toBe(
        true,
      );
    });
  it('preserves focus when the header locale control uses a pointer',
    async () => {
      meStatus = 401;
      setSiteLocale('en');
      const wrapper = await mountShell('/app/settings/sessions');
      await flushPromises();
      const field = document.createElement('input');
      document.body.append(field);
      field.focus();
      const localeButton = wrapper.get('[data-testid="landing-locale-vi"]');
      const pointerdown = new PointerEvent('pointerdown', {
        bubbles: true,
        cancelable: true,
      });

      localeButton.element.dispatchEvent(pointerdown);

      expect(pointerdown.defaultPrevented).toBe(true);
      expect(document.activeElement).toBe(field);
      field.remove();
      wrapper.unmount();
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
      setSiteLocale('vi');
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
    'marks the Library link current on the gallery and its pages',
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

  it('speaks Vietnamese for Library on the localized gallery', async () => {
    meStatus = 401;
    setSiteLocale('vi');
    const wrapper = await mountShell('/templates');
    await flushPromises();
    expect(links(wrapper)['Thư viện']).toBe('/templates');
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

  it('localizes Resumes and Settings on workspace routes', async () => {
    meStatus = 200;
    setSiteLocale('vi');
    const wrapper = await mountShell('/app/resumes');
    await flushPromises();
    const found = links(wrapper);
    expect(found['CV']).toBe('/app/resumes');
    expect(found['Cài đặt']).toBe('/app/settings/sessions');
    wrapper.unmount();
  });

  it.each([
    [undefined, '/app/settings/sessions', 'CV', 'Cài đặt'],
    ['fr', '/authorize', 'CV', 'Cài đặt'],
    ['en', '/app/settings/sessions', 'Resumes', 'Settings'],
    ['en', '/authorize', 'Resumes', 'Settings'],
  ])('localizes the shell with locale %s on %s', async (
    locale,
    route,
    resumes,
    settings,
  ) => {
    meStatus = 200;
    setSiteLocale(locale);
    const wrapper = await mountShell(route);
    await flushPromises();
    const found = links(wrapper);
    expect(found[resumes]).toBe('/app/resumes');
    expect(found[settings]).toBe('/app/settings/sessions');
    expect(wrapper.find('[data-testid="landing-locale"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="account-menu"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('localizes the account menu on workspace routes', async () => {
    meStatus = 200;
    setSiteLocale('vi');
    const wrapper = await mountShell('/app/new');
    await flushPromises();
    await wrapper.get('[data-testid="account-menu"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('[aria-label="Tài khoản"]').exists()).toBe(true);
    expect(document.body.querySelector('[data-testid="account-menu-settings"]')
      ?.textContent).toContain('Cài đặt');
    expect(document.body.querySelector('[data-testid="account-menu-logout"]')
      ?.textContent).toContain('Đăng xuất');
    wrapper.unmount();
  });

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

  it.each([
    ['/', 'Create your resume'],
    ['/templates', 'Create your resume'],
    ['/templates/engineer-compact', 'Create your resume'],
    ['/terms', 'Create your resume'],
    ['/privacy', 'Create your resume'],
    ['/login', 'Create account'],
    ['/register', 'Create account'],
    // /authorize is excluded: it is an authRequiredPath, so its header never
    // renders this CTA at all while signed out, settled or not.
  ])('labels the header CTA %s as %s', async (route, label) => {
    meStatus = 401;
    setSiteLocale('en');
    const wrapper = await mountShell(route);
    await flushPromises();
    const createAccount = wrapper.findAll('a')
      .find((link) => link.attributes('href')?.startsWith('/register'));
    expect(createAccount?.text()).toBe(label);
  });

  it('shows the open source link only when signed out', async () => {
    meStatus = 401;
    setSiteLocale('en');
    const signedOut = await mountShell('/');
    await flushPromises();
    const openSource = signedOut.get(
      '[data-testid="app-shell-open-source"]',
    );
    expect(openSource.attributes('href')).toBe(
      'https://github.com/dannyota/aboutme',
    );
    expect(openSource.attributes('rel')).toBe('noopener noreferrer');
    expect(openSource.classes()).toContain('max-[56rem]:hidden');
    signedOut.unmount();

    meStatus = 200;
    const signedIn = await mountShell('/');
    await flushPromises();
    expect(
      signedIn.find('[data-testid="app-shell-open-source"]').exists(),
    ).toBe(false);
  });

  it('ignores next outside /login and /register', async () => {
    meStatus = 401;
    setSiteLocale('en');
    const wrapper = await mountShell(
      `/templates?next=${encodeURIComponent('/app/resumes')}`,
    );
    await flushPromises();
    const found = links(wrapper);
    expect(found['Sign in']).toBe('/login');
    expect(found['Create your resume']).toBe('/register');
  });
});
