import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readRawBody, setResponseStatus } from 'h3';
import VerifyEmailPage from '../app/pages/verify-email.vue';

mockNuxtImport('useHead', () => vi.fn());

const TOKEN = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefg';

describe('verify-email.vue (fragment handling)', () => {
  it('composes the auth page and shared error banner', async () => {
    window.location.hash = '';
    const wrapper = await mountSuspended(VerifyEmailPage);

    const main = wrapper.get('[data-testid="verify-email-page"]');
    expect(main.find('[data-slot="card"]').exists()).toBe(false);
    expect(main.get('[data-page-title]').text()).toBe('Verify email');
    expect(wrapper.get('[data-testid="verify-error"]').attributes('role')).toBe(
      'alert',
    );
    expect(
      wrapper.get('[data-testid="verify-error"]').attributes('tabindex'),
    ).toBe(
      '-1',
    );
  });

  beforeEach(() => {
    vi.mocked(useHead).mockClear();
    window.location.hash = '';
  });

  it('verifies a single token, stripping it before the fetch', async () => {
    window.location.hash = `#token=${TOKEN}`;
    const replaceSpy = vi.spyOn(history, 'replaceState');
    let hashAtRequest = 'unset';
    let bodyAtRequest: string | null = null;
    let calls = 0;
    registerEndpoint('/api/v1/auth/password/verify', {
      method: 'POST',
      handler: async (event) => {
        calls += 1;
        hashAtRequest = window.location.hash;
        bodyAtRequest = await readRawBody(event);
        setResponseStatus(event, 204);
        return null;
      },
    });

    const wrapper = await mountSuspended(VerifyEmailPage);
    await flushPromises();

    expect(calls).toBe(1);
    expect(bodyAtRequest).toBe(JSON.stringify({ token: TOKEN }));
    expect(hashAtRequest).toBe('');
    expect(replaceSpy).toHaveBeenCalledTimes(1);
    expect(window.location.hash).toBe('');
    expect(wrapper.get('[data-testid="verify-success"]').text()).toContain(
      'Email verified. Sign in.',
    );
    expect(wrapper.html()).not.toContain(TOKEN);
  });

  it('treats a missing token key as a local failure with no request',
    async () => {
      window.location.hash = '#foo=bar';
      let calls = 0;
      registerEndpoint('/api/v1/auth/password/verify', {
        method: 'POST',
        handler: (event) => {
          calls += 1;
          setResponseStatus(event, 204);
          return null;
        },
      });
      const wrapper = await mountSuspended(VerifyEmailPage);
      await flushPromises();
      expect(calls).toBe(0);
      expect(wrapper.get('[data-testid="verify-error"]').text()).toBe(
        'This verification link is invalid or incomplete.',
      );
    });

  it('treats multiple token keys as malformed', async () => {
    window.location.hash = '#token=a&token=b';
    let calls = 0;
    registerEndpoint('/api/v1/auth/password/verify', {
      method: 'POST',
      handler: (event) => {
        calls += 1;
        setResponseStatus(event, 204);
        return null;
      },
    });
    const wrapper = await mountSuspended(VerifyEmailPage);
    await flushPromises();
    expect(calls).toBe(0);
    expect(wrapper.get('[data-testid="verify-error"]').exists()).toBe(true);
  });

  it('shows closed copy when the token is rejected as invalid', async () => {
    window.location.hash = `#token=${TOKEN}`;
    registerEndpoint('/api/v1/auth/password/verify', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 400);
        return {
          error: { code: 'credential_token_invalid', message: 'x' },
        };
      },
    });
    const wrapper = await mountSuspended(VerifyEmailPage);
    await flushPromises();
    expect(wrapper.get('[data-testid="verify-error"]').text()).toContain(
      'invalid or has expired',
    );
  });

  it('renders a no-referrer meta and loads no third-party resource',
    async () => {
      window.location.hash = `#token=${TOKEN}`;
      registerEndpoint('/api/v1/auth/password/verify', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 204);
          return null;
        },
      });
      const wrapper = await mountSuspended(VerifyEmailPage);
      await flushPromises();
      expect(vi.mocked(useHead)).toHaveBeenCalledWith(
        expect.objectContaining({
          meta: expect.arrayContaining([
            expect.objectContaining({
              name: 'referrer',
              content: 'no-referrer',
            }),
          ]),
        }),
      );
      expect(wrapper.html()).not.toMatch(/src=["']https?:/);
    });

  it('never re-submits after the token is stripped (refresh/replay)',
    async () => {
      window.location.hash = `#token=${TOKEN}`;
      let calls = 0;
      registerEndpoint('/api/v1/auth/password/verify', {
        method: 'POST',
        handler: (event) => {
          calls += 1;
          setResponseStatus(event, 204);
          return null;
        },
      });
      await mountSuspended(VerifyEmailPage);
      await flushPromises();
      expect(calls).toBe(1);
      // A fresh load with the hash already gone must not fire again.
      await mountSuspended(VerifyEmailPage);
      await flushPromises();
      expect(calls).toBe(1);
    });
});
