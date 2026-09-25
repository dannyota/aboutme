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
import { landingCopy } from '../app/landing/copy';
import LandingPage from '../app/pages/index.vue';
import { setSiteLocale } from './support/locale';

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

async function mountLanding(locale?: 'vi' | 'en') {
  setSiteLocale(locale);
  return mountSuspended(LandingPage);
}

beforeEach(() => {
  meStatus = 401;
  meRequests = 0;
  mocks.fetchMock.mockClear();
  setSiteLocale(undefined);
  clearNuxtData();
});

describe('index.vue', () => {
  it('renders the approved stamped-document hero without a card', async () => {
    const wrapper = await mountLanding('en');
    expect(wrapper.get('[data-testid="landing"]').element.tagName).toBe('MAIN');
    const heading = wrapper.get('[data-testid="landing-title"]');

    expect(heading.element.tagName).toBe('H1');
    expect(heading.text()).toBe('Your resume. Your link. Your control.');
    expect(heading.classes()).toContain('text-4xl');
    expect(wrapper.find('[data-slot="card"]').exists()).toBe(false);
    const sample = wrapper.get('[data-testid="landing-sample"]');
    expect(sample.get('[data-testid="landing-sheet"]').classes()).toEqual(
      expect.arrayContaining([
        'rounded-[var(--radius-sheet)]',
        'shadow-[var(--shadow-paper)]',
      ]),
    );
    // The page's only h1 is the hero title; the embedded sample resume's
    // name is a p, so the document keeps a single h1.
    expect(wrapper.findAll('h1')).toHaveLength(1);
  });

  it(
    'never stamps the hero sample so it cannot read as an already-public '
    + 'resume (DESIGN.md seal rules)',
    async () => {
      const wrapper = await mountLanding('en');
      const sample = wrapper.get('[data-testid="landing-sample"]');
      expect(sample.find('[data-app-seal]').exists()).toBe(false);
      expect(wrapper.find('[data-testid="landing-seal"]').exists()).toBe(
        false,
      );
    },
  );

  it('sets the emphasized suffix apart from the rest of the headline',
    async () => {
      for (const locale of ['en', 'vi'] as const) {
        const wrapper = await mountLanding(locale);
        const emphasis = wrapper.get(
          '[data-testid="landing-title-emphasis"]',
        );
        expect(emphasis.text()).toBe(landingCopy[locale].titleEmphasis);
        expect(landingCopy[locale].title[1].endsWith(
          landingCopy[locale].titleEmphasis,
        )).toBe(true);
      }
    });

  it('renders the approved English copy', async () => {
    const wrapper = await mountLanding('en');
    expect(wrapper.text()).toContain(
      'Free and open source. Write your resume, see exactly how each page '
      + 'will look, and publish it at its own link only when you’re ready.',
    );
    const points = wrapper
      .findAll('[data-testid="landing-point"]')
      .map((p) => p.get('[data-testid="landing-point-title"]').text());
    expect(points).toEqual([
      'Private by default',
      'One link per resume',
      'Bring your own AI',
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
        wrapper.get('[data-testid="landing-create-account"]').text(),
      ).toBe('Create your resume');
      expect(
        wrapper.get('[data-testid="landing-sign-in"]').attributes('href'),
      ).toBe('/login');
      expect(
        wrapper.find('[data-testid="landing-open-resumes"]').exists(),
      ).toBe(false);
      expect(
        wrapper.get('[data-testid="landing-browse-templates"]')
          .attributes('href'),
      ).toBe('/templates');
      const hero = wrapper.get('[data-testid="landing-hero"]');
      expect(hero.text().indexOf('Create your resume')).toBeLessThan(
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
    expect(
      wrapper.get('[data-testid="landing-browse-templates"]')
        .attributes('href'),
    ).toBe('/templates');
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

  it('never claims the assistant is "AI ready"', async () => {
    for (const locale of ['en', 'vi'] as const) {
      const wrapper = await mountLanding(locale);
      expect(wrapper.text().toLowerCase()).not.toContain('ai ready');
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
      expect.arrayContaining(['text-link', 'underline']),
    );
  });

  it('offers a direct source link beside the license text', async () => {
    const wrapper = await mountLanding('en');
    const source = wrapper.get('[data-testid="landing-source-link"]');
    expect(source.text()).toBe('View the code on GitHub');
    expect(source.attributes('href')).toBe(
      'https://github.com/dannyota/aboutme',
    );
    expect(source.attributes('rel')).toBe('noopener noreferrer');
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

  it('uses only design tokens and utilities, never a page hex color', () => {
    const source = readFileSync('app/pages/index.vue', 'utf8');
    expect(source).not.toMatch(/#[0-9a-fA-F]{3,8}\b/u);
  });
});

describe('index.vue language', () => {
  it('renders Vietnamese by default', async () => {
    const wrapper = await mountLanding();
    expect(wrapper.get('[data-testid="landing-title"]').text()).toBe(
      'CV của bạn. Chia sẻ theo cách của bạn.',
    );
    expect(wrapper.get('[data-testid="landing-create-account"]').text()).toBe(
      'Tạo CV của bạn',
    );
    expect(wrapper.get('[data-testid="landing-sign-in"]').text()).toBe(
      'Đăng nhập',
    );
    const points = wrapper
      .findAll('[data-testid="landing-point-title"]')
      .map((p) => p.text());
    expect(points).toEqual([
      'Riêng tư theo mặc định',
      'Mỗi CV một đường dẫn',
      'Dùng trợ lý AI của bạn',
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
    expect(
      wrapper.get('[data-testid="landing-browse-templates"]')
        .attributes('href'),
    ).toBe('/templates');
    expect(
      wrapper.get('[data-testid="landing-browse-templates"]').text(),
    ).toBe('Xem thư viện');
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
      'Your resume. Your link. Your control.',
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
          ? ['CV của bạn.', 'Chia sẻ theo cách của bạn.']
          : ['Your resume.', 'Your link. Your control.'],
      );
    }
  });

  it('falls back to Vietnamese for an unknown cookie value', async () => {
    const wrapper = await mountLanding('fr' as 'vi');
    expect(wrapper.get('[data-testid="landing-title"]').text()).toBe(
      'CV của bạn. Chia sẻ theo cách của bạn.',
    );
  });
});
