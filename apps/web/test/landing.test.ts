import { describe, expect, it, vi } from 'vitest';
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
registerEndpoint('/api/v1/me', (event) => {
  meRequests += 1;
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});
function apiPaths(): string[] {
  return mocks.fetchMock.mock.calls
    .map(([url]) => new URL(String(url), 'http://localhost').pathname)
    .filter((path) => path.startsWith('/api/'));
}

describe('index.vue', () => {
  it('composes a type-led heading without a card', async () => {
    const wrapper = await mountSuspended(LandingPage);
    const heading = wrapper.get('#landing-title');

    expect(heading.element.tagName).toBe('H1');
    expect(heading.classes()).toContain('text-4xl');
    expect(wrapper.find('[data-slot="card"]').exists()).toBe(false);
  });

  it('renders the approved copy', async () => {
    const wrapper = await mountSuspended(LandingPage);
    expect(wrapper.get('#landing-title').text()).toBe(
      'Build your resume. Publish it at its own link.',
    );
    expect(wrapper.text()).toContain(
      'aboutme is an open-source resume builder.',
    );
    const points = wrapper
      .findAll('[data-testid="landing-point"]')
      .map((p) => p.get('[data-testid="landing-point-title"]').text());
    expect(points).toEqual([
      'Yours to keep.',
      'One link per resume.',
      'Bring your own agent.',
    ]);
  });

  it('offers sign-in and registration and nothing into the app', async () => {
    const wrapper = await mountSuspended(LandingPage);
    const hrefs = wrapper.findAll('[href]').map((a) => a.attributes('href'));
    expect(hrefs).toContain('/login');
    expect(hrefs).toContain('/register');
    expect(hrefs.some((h) => h?.startsWith('/app'))).toBe(false);
  });

  it('names no unshipped feature', async () => {
    const wrapper = await mountSuspended(LandingPage);
    expect(wrapper.text().toLowerCase()).not.toMatch(/pdf|realtime|real-time/);
  });

  it('links the license line to the repository', async () => {
    const wrapper = await mountSuspended(LandingPage);
    const license = wrapper.get('[data-testid="landing-license"] [href]');
    expect(license.text()).toContain('AGPL-3.0');
    expect(license.attributes('href')).toBe(
      'https://github.com/dannyota/aboutme',
    );
    expect(license.attributes('rel')).toBe('noopener noreferrer');
  });

  it('keeps the shell read to one /me request and no other API calls',
    async () => {
      mocks.fetchMock.mockClear();
      meRequests = 0;
      const wrapper = await mountSuspended(AppRoot, { route: '/' });
      await flushPromises();

      expect(meRequests).toBeLessThanOrEqual(1);
      expect(apiPaths().every((path) => path === '/api/v1/me')).toBe(true);
      expect(
        wrapper.get('[data-testid="landing-license"]').exists(),
      ).toBe(true);
    });

  it('does not request API data from the landing page', async () => {
    mocks.fetchMock.mockClear();
    await mountSuspended(LandingPage);
    await flushPromises();

    expect(apiPaths()).toEqual([]);
  });
});
