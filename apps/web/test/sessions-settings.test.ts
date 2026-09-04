import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();
mockNuxtImport('navigateTo', () => vi.fn());

const now = new Date('2026-09-04T12:00:00Z');
const sessions = [
  {
    id: 'current',
    createdAt: '2026-09-01T00:00:00Z',
    lastSeenAt: '2026-09-04T10:00:00Z',
    ua:
      'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 '
      + '(KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36',
    ip: null,
    current: true,
  },
  {
    id: 'other',
    createdAt: '2026-08-01T00:00:00Z',
    lastSeenAt: '2026-09-02T12:00:00Z',
    ua:
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:128.0) '
      + 'Gecko/20100101 Firefox/128.0',
    ip: null,
    current: false,
  },
];

registerEndpoint('/api/v1/me', () => ({
  data: {
    user: {
      id: 'user-1',
      email: 'demo@example.com',
      name: 'Demo User',
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: 'csrf',
    identities: [],
  },
}));
registerEndpoint('/api/v1/sessions', () => ({ data: sessions }));

describe('settings sessions page', () => {
  it(
    'describes devices, uses relative time, and hides raw UA from row text',
    async () => {
      const wrapper = await mountSuspended(SessionsPage, {
        props: { now },
        route: '/app/settings/sessions',
      });
      await flushPromises();

      const current = wrapper.get('[data-testid="session-row-current"]');
      const other = wrapper.get('[data-testid="session-row-other"]');
      expect(current.text()).toContain('Chrome 152 on Linux');
      expect(current.text()).toContain('Last seen 2 hours ago');
      expect(current.text()).toContain('This device');
      expect(other.text()).toContain('Firefox 128 on Mac OS X');
      expect(other.text()).toContain('Last seen 2 days ago');
      expect(other.text()).not.toContain(sessions[1].ua);
      expect(
        current.get('[data-testid="session-description"]').attributes('title'),
      ).toBe(sessions[0].ua);
    });

  it(
    'has one logout action and one revoke action for the other session',
    async () => {
      const wrapper = await mountSuspended(SessionsPage, { props: { now } });
      await flushPromises();

      const buttons = wrapper.findAll('[data-slot="button"]');
      expect(
        buttons.filter((button) => button.text() === 'Log out'),
      ).toHaveLength(1);
      expect(
        buttons.filter((button) => button.text() === 'Revoke'),
      ).toHaveLength(1);
      expect(
        wrapper.get('[data-testid="session-row-current"]').text(),
      ).toContain('Log out');
    });

  it(
    'gates connected agents and provider linking by capabilities',
    async () => {
      const wrapper = await mountSuspended(SessionsPage, { props: { now } });
      await flushPromises();
      expect(wrapper.find('[aria-labelledby="agents-title"]').exists()).toBe(
        true,
      );
      expect(wrapper.find('[aria-labelledby="providers-title"]').exists()).toBe(
        true,
      );
    },
  );

  it('keeps the theme cookie in app surface head metadata', async () => {
    document.cookie = 'aboutme-theme=dark; Path=/';
    const source = await import('node:fs/promises').then(({ readFile }) =>
      readFile('app/app.vue', 'utf8'),
    );
    expect(source).toMatch(/useCookie<Theme \| undefined>\(["']aboutme-theme/);
    expect(source).toMatch(/["']data-theme["']:\s*theme\.value/);
  });
});
