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

beforeEach(() => {
  meStatus = 401;
  meRequests = 0;
  mocks.fetchMock.mockClear();
});

describe('index.vue', () => {
  it('renders the approved stamped-document hero without a card', async () => {
    const wrapper = await mountSuspended(LandingPage);
    expect(wrapper.get('[data-testid="landing"]').element.tagName).toBe('MAIN');
    const heading = wrapper.get('[data-testid="landing-title"]');

    expect(heading.element.tagName).toBe('H1');
    expect(heading.text()).toBe('The resume is public. You are not.');
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

  it('renders the approved copy', async () => {
    const wrapper = await mountSuspended(LandingPage);
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
      const wrapper = await mountSuspended(LandingPage);
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
    const wrapper = await mountSuspended(LandingPage);
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
    const wrapper = await mountSuspended(LandingPage);
    expect(wrapper.text().toLowerCase()).not.toMatch(/realtime|real-time/);
  });

  it('links the license line to the repository', async () => {
    const wrapper = await mountSuspended(LandingPage);
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
