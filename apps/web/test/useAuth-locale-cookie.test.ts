import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseHeader, setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import { localeCookie } from '../app/i18n/locale';
import { setSiteLocale } from './support/locale';

/**
 * Covers `restoreLocaleCookie` in `useAuth.ts`'s `mutate`: logout,
 * logout-everywhere, revoking the caller's own session, and account
 * deletion all send `Clear-Site-Data` (docs/design/security.md, session
 * lifecycle), which wipes `aboutme-locale` along with every other cookie.
 * `logout-state.test.ts` covers the full logout-then-reload path; this file
 * isolates the hook's three outcomes directly.
 */

const meData = {
  user: {
    id: 'user-1',
    email: 'demo@example.com',
    name: 'Demo User',
    avatarKey: null,
    hasPassword: true,
  },
  csrfToken: 'test-csrf-token',
  identities: [],
};

registerEndpoint('/api/v1/me', () => ({ data: meData }));

const Probe = defineComponent({
  setup() {
    const { mutate } = useAuth();
    return { mutate };
  },
  render() {
    return h(
      'button',
      {
        'data-testid': 'mutate-button',
        'onClick': () => {
          this.mutate('/api/v1/test-clear-site-data', { method: 'POST' })
            .catch(() => {});
        },
      },
      'mutate',
    );
  },
});

function localeCookieEntry(): string | undefined {
  return document.cookie
    .split('; ')
    .find((entry) => entry.startsWith(`${localeCookie}=`));
}

function registerClearSiteData(withHeader: boolean): void {
  registerEndpoint('/api/v1/test-clear-site-data', {
    method: 'POST',
    handler: (event) => {
      if (withHeader) {
        setResponseHeader(event, 'Clear-Site-Data', '"cookies", "storage"');
      }
      setResponseStatus(event, 204);
      return null;
    },
  });
}

async function triggerMutation(): Promise<void> {
  const wrapper = await mountSuspended(Probe);
  await flushPromises();
  await wrapper.get('[data-testid="mutate-button"]').trigger('click');
  await flushPromises();
}

describe('useAuth restores the locale cookie after Clear-Site-Data', () => {
  it('rewrites the cookie from state when the header is present', async () => {
    setSiteLocale(undefined);
    registerClearSiteData(true);
    useState(localeCookie).value = 'en';

    await triggerMutation();

    expect(localeCookieEntry()).toBe(`${localeCookie}=en`);
  });

  it('writes nothing when the state holds no valid locale', async () => {
    setSiteLocale(undefined);
    registerClearSiteData(true);
    useState(localeCookie).value = undefined;

    await triggerMutation();

    expect(localeCookieEntry()).toBeUndefined();
  });

  it('writes nothing without the header, even with a valid state', async () => {
    setSiteLocale(undefined);
    registerClearSiteData(false);
    useState(localeCookie).value = 'vi';

    await triggerMutation();

    expect(localeCookieEntry()).toBeUndefined();
  });
});
