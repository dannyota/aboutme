import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { defineComponent, h } from 'vue';
import { localeCookie } from '../app/i18n/locale';
import { setSiteLocale } from './support/locale';

// Chromium hides Clear-Site-Data from scripts; the endpoint clears the cookie.

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

// Deleting an unset cookie can leave an empty entry in the test DOM.
function localeCookieValue(): string | undefined {
  const prefix = `${localeCookie}=`;
  const value = document.cookie
    .split('; ')
    .find((entry) => entry.startsWith(prefix))
    ?.slice(prefix.length);
  return value === '' ? undefined : value;
}

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
