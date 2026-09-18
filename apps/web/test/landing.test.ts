import { readFileSync } from 'node:fs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import LandingPage from '../app/pages/index.vue';

const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const real: any = (globalThis as { $fetch: unknown }).$fetch;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return { fetchMock: vi.fn((...args: any[]) => real(...args)) };
});
mockNuxtImport('$fetch', () => mocks.fetchMock);

let meRequests = 0;
let meStatus = 401;
registerEndpoint('/api/v1/me', (event) => {
  meRequests += 1;
  setResponseStatus(event, meStatus);
  if (meStatus === 200) {
    return {
      data: {
        user: {
          id: 'user-1',
          email: 'ada@example.com',
          name: 'Ada Lovelace',
          avatarKey: null,
          hasPassword: false,
        },
        csrfToken: 'csrf-token',
        identities: [],
      },
    };
  }
  return { error: { code: 'session_required', message: 'Sign in.' } };
});
function apiPaths(): string[] {
  return mocks.fetchMock.mock.calls
    .map(([url]) => new URL(String(url), 'http://localhost').pathname)
    .filter((path) => path.startsWith('/api/'));
}

function setLocaleCookie(value: string | undefined): void {
  document.cookie = value === undefined
    ? 'aboutme-locale=; max-age=0; path=/'
    : `aboutme-locale=${value}; path=/`;
}

async function mountLanding(locale?: 'vi' | 'en') {
  setLocaleCookie(locale);
  return mountSuspended(LandingPage);
}

beforeEach(() => {
  meStatus = 401;
  meRequests = 0;
  mocks.fetchMock.mockClear();
  setLocaleCookie(undefined);
  clearNuxtData();
});

describe('index.vue', () => {
  it('renders the approved stamped-document hero without a card', async () => {
    const wrapper = await mountLanding('en');
    expect(wrapper.get('[data-testid="landing"]').element.tagName).toBe('MAIN');
    const heading = wrapper.get('[data-testid="landing-title"]');

    expect(heading.element.tagName).toBe('H1');
    expect(heading.text()).toBe(
      'Your resume. Free. No one sees it unless you want them to.',
    );
    expect(heading.classes()).toContain('text-2xl');
    expect(wrapper.find('[data-slot="card"]').exists()).toBe(false);
    const sample = wrapper.get('[data-testid="landing-sample"]');
    expect(sample.get('[data-testid="landing-sheet"]').classes()).toEqual(
      expect.arrayContaining([
        'rounded-[var(--radius-sheet)]',
        'shadow-[var(--shadow-paper)]',
      ]),
    );
  });

  it('renders the approved English copy', async () => {
    const wrapper = await mountLanding('en');
    expect(wrapper.text()).toContain(
      'aboutme is an open-source resume builder. Write up to three resumes, '
      + 'preview the exact page, and publish each one at its own link.',
    );
    const points = wrapper
      .findAll('[data-testid="landing-point"]')
      .map((p) => p.get('[data-testid="landing-point-title"]').text());
    expect(points).toEqual([
      'Yours to keep.',
      'One link per resume.',
      'Bring your own agent.',
    ]);
    expect(wrapper.text()).toContain('Public resume');
    expect(wrapper.text()).toContain('Whether any public page exists.');
    expect(wrapper.text()).toContain('PDF download');
    expect(wrapper.text()).toContain('SEO and GEO');
    expect(wrapper.text()).toContain(
      'Whether search engines and AI answer engines may index it. Off by '
      + 'default.',
    );
  });

  it(
    'offers registration before sign-in and nothing into the app',
    async () => {
      const wrapper = await mountLanding('en');
      expect(
        wrapper
          .get('[data-testid="landing-create-account"]')
          .attributes('href'),
      ).toBe('/register');
      expect(
        wrapper.get('[data-testid="landing-sign-in"]').attributes('href'),
      ).toBe('/login');
      expect(
        wrapper.find('[data-testid="landing-open-resumes"]').exists(),
      ).toBe(false);
      const hero = wrapper.get('[aria-labelledby="landing-title"]');
      expect(hero.text().indexOf('Create account')).toBeLessThan(
        hero.text().indexOf('Sign in'),
      );
    },
  );

  it('shows only the resume entry action when authenticated', async () => {
    meStatus = 200;
    const wrapper = await mountLanding('en');
    await flushPromises();
    expect(wrapper.get('[data-testid="landing-open-resumes"]').text()).toBe(
      'Open your resumes',
    );
    expect(wrapper.find('[data-testid="landing-sign-in"]').exists()).toBe(
      false,
    );
    expect(
      wrapper.find('[data-testid="landing-create-account"]').exists(),
    ).toBe(false);
  });

  it('names no unshipped feature', async () => {
    for (const locale of ['en', 'vi'] as const) {
      const wrapper = await mountLanding(locale);
      expect(wrapper.text().toLowerCase()).not.toMatch(
        /realtime|real-time|thời gian thực/u,
      );
    }
  });

  it('links the license line to the repository', async () => {
    const wrapper = await mountLanding('en');
    const license = wrapper.get('[data-testid="landing-license-link"]');
    expect(license.text()).toContain('AGPL-3.0');
    expect(license.attributes('href')).toBe(
      'https://github.com/dannyota/aboutme',
    );
    expect(license.attributes('rel')).toBe('noopener noreferrer');
    expect(license.classes()).toEqual(
      expect.arrayContaining(['text-primary', 'underline']),
    );
  });

  it(
    'keeps the shell read to one /me request and no other API calls',
    async () => {
      mocks.fetchMock.mockClear();
      meRequests = 0;
      const wrapper = await mountSuspended(AppRoot, { route: '/' });
      await flushPromises();

      expect(meRequests).toBeLessThanOrEqual(2);
      expect(apiPaths().every((path) => path === '/api/v1/me')).toBe(true);
      expect(
        wrapper.get('[data-testid="landing-license"]').exists(),
      ).toBe(true);
    },
  );

  it('does not request API data from the landing page', async () => {
    mocks.fetchMock.mockClear();
    await mountSuspended(LandingPage);
    await flushPromises();

    expect(apiPaths().every((path) => path === '/api/v1/me')).toBe(true);
  });

  it(
    'does not include a client data fetch or alter the base CSP contract',
    () => {
      const source = readFileSync('app/pages/index.vue', 'utf8');
      expect(source).not.toMatch(/(?:useFetch|useAsyncData|\$fetch)/u);
      expect(source).not.toContain('Content-Security-Policy');
    },
  );
});

describe('index.vue language', () => {
  it('renders Vietnamese by default', async () => {
    const wrapper = await mountLanding();
    expect(wrapper.get('[data-testid="landing-title"]').text()).toBe(
      'CV của bạn. Miễn phí. Không ai thấy nếu bạn không muốn.',
    );
    expect(wrapper.get('[data-testid="landing-create-account"]').text()).toBe(
      'Tạo tài khoản',
    );
    expect(wrapper.get('[data-testid="landing-sign-in"]').text()).toBe(
      'Đăng nhập',
    );
    const points = wrapper
      .findAll('[data-testid="landing-point-title"]')
      .map((p) => p.text());
    expect(points).toEqual([
      'Của bạn, do bạn giữ.',
      'Mỗi CV một đường dẫn.',
      'Dùng trợ lý AI của bạn.',
    ]);
    expect(wrapper.text()).toContain('Đăng CV gồm ba lựa chọn');
    expect(wrapper.text()).not.toContain('Create account');
  });

  it('keeps the auth links on their routes in Vietnamese', async () => {
    const wrapper = await mountLanding('vi');
    expect(
      wrapper.get('[data-testid="landing-create-account"]').attributes('href'),
    ).toBe('/register');
    expect(
      wrapper.get('[data-testid="landing-sign-in"]').attributes('href'),
    ).toBe('/login');
  });

  it('marks the page language for the rendered copy', async () => {
    await mountLanding();
    await flushPromises();
    expect(document.documentElement.lang).toBe('vi');

    await mountLanding('en');
    await flushPromises();
    expect(document.documentElement.lang).toBe('en');
  });

  it('switches to English from the header and remembers it', async () => {
    const wrapper = await mountSuspended(AppRoot, { route: '/' });
    await flushPromises();
    const toggle = wrapper
      .get('[data-testid="app-shell"]')
      .get('[data-testid="landing-locale"]');
    expect(toggle.attributes('role')).toBe('group');
    expect(toggle.attributes('aria-label')).toBe('Ngôn ngữ');
    const vi = toggle.get('[data-testid="landing-locale-vi"]');
    const en = toggle.get('[data-testid="landing-locale-en"]');
    expect(vi.attributes('aria-pressed')).toBe('true');
    expect(en.attributes('aria-pressed')).toBe('false');
    expect(en.attributes('lang')).toBe('en');
    expect(vi.attributes('lang')).toBe('vi');

    await en.trigger('click');
    await flushPromises();
    expect(en.attributes('aria-pressed')).toBe('true');
    expect(document.cookie).toContain('aboutme-locale=en');
    wrapper.unmount();
    // Browsers sync same-name cookie refs through cookieStore or
    // BroadcastChannel; the test DOM has neither, so read the cookie afresh.
    const page = await mountSuspended(LandingPage);
    expect(page.get('[data-testid="landing-title"]').text()).toBe(
      'Your resume. Free. No one sees it unless you want them to.',
    );
  });

  it('renders the headline as two lines', async () => {
    for (const locale of ['vi', 'en'] as const) {
      const wrapper = await mountLanding(locale);
      const lines = wrapper
        .findAll('[data-testid="landing-title"] > span.block')
        .map((line) => line.text());
      expect(lines).toEqual(
        locale === 'vi'
          ? ['CV của bạn. Miễn phí.', 'Không ai thấy nếu bạn không muốn.']
          : ['Your resume. Free.', 'No one sees it unless you want them to.'],
      );
    }
  });

  it('falls back to Vietnamese for an unknown cookie value', async () => {
    const wrapper = await mountLanding('fr' as 'vi');
    expect(wrapper.get('[data-testid="landing-title"]').text()).toBe(
      'CV của bạn. Miễn phí. Không ai thấy nếu bạn không muốn.',
    );
  });
});
