import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

mockNuxtImport('navigateTo', () => vi.fn());

const now = new Date('2026-09-04T12:00:00Z');
let linked: { provider: string }[] = [];

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
    identities: linked,
  },
}));
registerEndpoint('/api/v1/sessions', () => ({ data: [] }));

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

async function mountSettings(
  providers: readonly string[],
  route = '/app/settings/sessions',
): Promise<Wrapper> {
  registerCapabilities({
    providerLogin: providers.length > 0,
    agentAccess: false,
    providers,
  });
  const wrapper = await mountSuspended(SessionsPage, {
    props: { now },
    route,
  });
  await flushPromises();
  return wrapper;
}

async function linkButtons(wrapper: Wrapper): Promise<string[]> {
  await wrapper.get('[data-testid="add-provider-button"]').trigger('click');
  await flushPromises();
  return wrapper
    .findAll('button')
    .map((button) => button.text())
    .filter((text) => text.startsWith('Link '));
}

beforeEach(() => {
  linked = [];
  clearNuxtData();
});

describe('settings sign-in providers (ADR 0039)', () => {
  it('offers only Google when only Google is enabled', async () => {
    const wrapper = await mountSettings(['google']);
    expect(await linkButtons(wrapper)).toEqual(['Link Google']);
  });

  it('offers every provider when all three are enabled', async () => {
    const wrapper = await mountSettings(['google', 'github', 'linkedin']);
    expect(await linkButtons(wrapper)).toEqual([
      'Link Google',
      'Link GitHub',
      'Link LinkedIn',
    ]);
  });

  it('hides the provider block with no identity and nothing to link',
    async () => {
      const wrapper = await mountSettings([]);
      expect(
        wrapper.find('[aria-labelledby="providers-title"]').exists(),
      ).toBe(false);
    });

  it('lists a linked provider that is enabled', async () => {
    linked = [{ provider: 'google' }];
    const wrapper = await mountSettings(['google']);
    const row = wrapper.get('[data-testid="linked-provider-google"]');
    expect(row.text()).toContain('Google');
    expect(row.text()).toContain('Linked');
    expect(row.text()).not.toContain('not available');
  });

  it('lists a linked provider that is no longer enabled', async () => {
    linked = [{ provider: 'github' }];
    const wrapper = await mountSettings([]);
    expect(
      wrapper.find('[aria-labelledby="providers-title"]').exists(),
    ).toBe(true);
    const row = wrapper.get('[data-testid="linked-provider-github"]');
    expect(row.get('span').text()).toBe('GitHub');
    expect(row.text()).toContain('Linked, not available for sign-in');
    expect(wrapper.find('[data-testid="add-provider-button"]').exists()).toBe(
      false,
    );
  });

  it('skips a linked provider and offers nothing once all are linked',
    async () => {
      linked = [{ provider: 'google' }];
      const wrapper = await mountSettings(['google']);
      expect(
        wrapper.find('[aria-labelledby="providers-title"]').exists(),
      ).toBe(true);
      expect(
        wrapper.find('[data-testid="add-provider-button"]').exists(),
      ).toBe(false);
    });

  it('asks for reauthentication only through an enabled provider',
    async () => {
      linked = [{ provider: 'github' }, { provider: 'google' }];
      const wrapper = await mountSettings(
        ['google'],
        '/app/settings/sessions?error=reauth_required',
      );
      expect(wrapper.get('[data-testid="reauth-prompt"]').text()).toContain(
        'Sign in again with Google',
      );

      linked = [{ provider: 'github' }];
      clearNuxtData();
      const disabledOnly = await mountSettings(
        ['google'],
        '/app/settings/sessions?error=reauth_required',
      );
      expect(
        disabledOnly.find('[data-testid="reauth-prompt"]').exists(),
      ).toBe(false);
    });
});
