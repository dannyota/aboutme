import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import { localeCookie } from '../app/i18n/locale';
import { setSiteLocale } from './support/locale';

/**
 * Covers `restoreLocaleCookie` in `useAuth.ts`'s `mutate`: logout,
 * logout-everywhere, revoking the caller's own session, and account
 * deletion all send `Clear-Site-Data` (docs/design/security.md, session
 * lifecycle), which wipes `aboutme-locale` along with every other cookie.
 * The test endpoint plays the browser's part: it clears the cookie itself
 * and never exposes the header, as Chromium hides `Clear-Site-Data` from
 * page scripts. `logout-state.test.ts` covers the full logout-then-reload
 * path; this file isolates the hook's outcomes directly.
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

/**
 * The locale cookie's value, or `undefined` when none is set. Deleting a
 * cookie the jar never held can leave an empty entry in the test DOM, which
 * a browser would not send, so an empty value counts as none.
 */
function localeCookieValue(): string | undefined {
  const prefix = `${localeCookie}=`;
  const value = document.cookie
    .split('; ')
    .find((entry) => entry.startsWith(prefix))
    ?.slice(prefix.length);
  return value === '' ? undefined : value;
}

/**
 * Registers the mutation endpoint. `clearedBy` is the cookie line the
 * endpoint applies on the browser's behalf while answering: the
 * Clear-Site-Data wipe, another tab's write, or nothing.
 */
function registerMutation(clearedBy: string | undefined): void {
  registerEndpoint('/api/v1/test-clear-site-data', {
    method: 'POST',
    handler: (event) => {
      if (clearedBy !== undefined) document.cookie = clearedBy;
      setResponseStatus(event, 204);
      return null;
    },
  });
}

const clearSiteData = `${localeCookie}=; Max-Age=0; Path=/`;

async function triggerMutation(): Promise<void> {
  const wrapper = await mountSuspended(Probe);
  await flushPromises();
  await wrapper.get('[data-testid="mutate-button"]').trigger('click');
  await flushPromises();
}

describe('useAuth restores the locale cookie after Clear-Site-Data', () => {
  it('rewrites a cleared choice without seeing the header', async () => {
    setSiteLocale('en');
    registerMutation(clearSiteData);
    useState(localeCookie).value = 'en';

    await triggerMutation();

    expect(localeCookieValue()).toBe('en');
  });

  it('writes the latest state over a different cleared value', async () => {
    setSiteLocale('en');
    registerMutation(clearSiteData);
    useState(localeCookie).value = 'vi';

    await triggerMutation();

    expect(localeCookieValue()).toBe('vi');
  });

  it('falls back to the cleared cookie when the state holds none', async () => {
    setSiteLocale('en');
    registerMutation(clearSiteData);
    useState(localeCookie).value = undefined;

    await triggerMutation();

    expect(localeCookieValue()).toBe('en');
  });

  it('writes nothing for a browser that never chose a language', async () => {
    setSiteLocale(undefined);
    registerMutation(clearSiteData);
    useState(localeCookie).value = 'vi';

    await triggerMutation();

    expect(localeCookieValue()).toBeUndefined();
  });

  it('leaves a cookie the response did not clear alone', async () => {
    setSiteLocale('en');
    registerMutation(`${localeCookie}=vi; Path=/`);
    useState(localeCookie).value = 'en';

    await triggerMutation();

    expect(localeCookieValue()).toBe('vi');
  });
});
