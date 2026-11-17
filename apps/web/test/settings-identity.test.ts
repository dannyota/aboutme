import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

registerCapabilities();
mockNuxtImport('navigateTo', () => vi.fn());

registerEndpoint('/api/v1/me', () => ({
  data: {
    user: {
      id: 'user-1',
      email: 'ada@example.com',
      name: 'Ada',
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: 'csrf',
    identities: [],
  },
}));
registerEndpoint('/api/v1/sessions', () => ({
  data: [
    {
      id: 'here',
      createdAt: '2026-11-01T00:00:00Z',
      lastSeenAt: '2026-11-16T00:00:00Z',
      ua: 'Chrome on macOS',
      ip: null,
      current: true,
    },
    {
      id: 'there',
      createdAt: '2026-10-01T00:00:00Z',
      lastSeenAt: '2026-11-10T00:00:00Z',
      ua: 'Firefox on Linux',
      ip: null,
      current: false,
    },
  ],
}));
registerEndpoint('/api/v1/me/second-factor', () => ({
  data: { enabled: false, passkeys: [], recoveryCodesRemaining: 0 },
}));
registerEndpoint('/api/v1/me/agents', () => ({
  data: {
    grants: [{
      id: 'grant-1',
      clientName: 'Agent',
      clientKind: 'cimd',
      clientHost: 'agent.example',
      scopes: ['resumes:read'],
      createdAt: '2026-11-01T00:00:00Z',
      lastUsedAt: null,
    }],
  },
}));

const SECTION_TITLES = [
  'devices-title',
  'password-title',
  'second-factor-title',
  'privacy-title',
  'agents-title',
  'providers-title',
];

async function mountSettings() {
  const wrapper = await mountSuspended(SessionsPage, {
    route: '/app/settings/sessions',
  });
  await flushPromises();
  return wrapper;
}

describe('settings page identity', () => {
  beforeEach(() => {
    setSiteLocale('en');
  });

  it('sets the page heading at 32 px, weight 800', async () => {
    const wrapper = await mountSettings();
    const title = wrapper.get('[data-testid="settings-page"] h1');
    for (const name of ['text-2xl', 'font-extrabold', 'tracking-[-0.02em]']) {
      expect(title.classes(), name).toContain(name);
    }
    expect(title.classes()).not.toContain('font-semibold');
  });

  it('sets every section title at 20 px, weight 700', async () => {
    const wrapper = await mountSettings();
    for (const id of SECTION_TITLES) {
      const title = wrapper.get(`#${id}`);
      expect(title.element.tagName, id).toBe('H2');
      expect(title.classes(), id).toEqual(
        expect.arrayContaining(['text-lg', 'font-bold']),
      );
      expect(title.classes(), id).not.toContain('font-semibold');
    }
  });

  it('divides the sections with plain top rules', async () => {
    const wrapper = await mountSettings();
    const sections = wrapper.findAll('[data-testid="settings-page"] > section');
    expect(sections.length).toBeGreaterThanOrEqual(SECTION_TITLES.length);
    for (const section of sections) {
      const label = section.attributes('aria-labelledby');
      expect(section.classes(), label).toContain('border-t');
      expect(section.classes().filter((name) =>
        /^border-(?!t$)/u.test(name)), label).toEqual([]);
    }
  });

  it('makes Revoke and Log out of one device destructive outline pills',
    async () => {
      const wrapper = await mountSettings();
      const logOut = wrapper
        .get('[data-testid="session-row-here"] [data-slot="button"]');
      expect(logOut.text()).toBe('Log out');
      expect(logOut.attributes('data-variant')).toBe('destructive');
      expect(wrapper.get('[data-testid="revoke-button"]')
        .attributes('data-variant')).toBe('destructive');
      expect(wrapper.get('[data-testid="agent-revoke"]')
        .attributes('data-variant')).toBe('destructive');
    });

  it('makes Delete account a destructive outline pill', async () => {
    const wrapper = await mountSettings();
    expect(wrapper.get('[data-testid="account-delete-action"]')
      .attributes('data-variant')).toBe('destructive');
  });

  it('makes Log out everywhere a plain outline pill', async () => {
    const wrapper = await mountSettings();
    const everywhere = wrapper.get('[data-testid="revoke-all-button"]');
    expect(everywhere.attributes('data-variant')).toBe('outline');
    expect(everywhere.classes()).not.toContain('text-destructive');
  });
});
