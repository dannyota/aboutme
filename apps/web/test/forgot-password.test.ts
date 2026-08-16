import { describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import ForgotPasswordPage from '../app/pages/forgot-password.vue';

const GENERIC_COPY
  = 'If an account exists for this email, we\'ve sent a password reset link.';

async function submit(wrapper: Awaited<ReturnType<typeof mountSuspended>>) {
  await wrapper.get('input[autocomplete="email"]').setValue('ada@example.com');
  await wrapper.get('form').trigger('submit');
  await flushPromises();
}

describe('forgot-password.vue', () => {
  it('shows the exact generic 202 copy after a successful submit',
    async () => {
      registerEndpoint('/api/v1/auth/password/forgot', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 202);
          return { data: { accepted: true } };
        },
      });
      const wrapper = await mountSuspended(ForgotPasswordPage);
      await submit(wrapper);
      const success = wrapper.get('[data-testid="forgot-success"]');
      expect(success.text()).toBe(GENERIC_COPY);
      expect(success.text()).not.toContain('ada@example.com');
      expect(success.text().toLowerCase()).not.toMatch(
        /google|github|linkedin/,
      );
    });

  it('keeps every server-outcome copy closed and account-neutral',
    async () => {
      const outcomes: Array<{ status: number; body: unknown }> = [
        {
          status: 400,
          body: { error: { code: 'request_invalid', message: 'x' } },
        },
        {
          status: 429,
          body: { error: { code: 'rate_limited', message: 'x' } },
        },
        {
          status: 503,
          body: {
            error: { code: 'authentication_unavailable', message: 'x' },
          },
        },
        {
          status: 500,
          body: { error: { code: 'internal_error', message: 'x' } },
        },
      ];
      for (const outcome of outcomes) {
        registerEndpoint('/api/v1/auth/password/forgot', {
          method: 'POST',
          handler: (event) => {
            setResponseStatus(event, outcome.status);
            return outcome.body;
          },
        });
        const wrapper = await mountSuspended(ForgotPasswordPage);
        await submit(wrapper);
        const error = wrapper.get('[data-testid="forgot-error"]');
        expect(error.text()).not.toContain('ada@example.com');
        expect(error.text().toLowerCase()).not.toMatch(
          /google|github|linkedin/,
        );
        expect(error.text()).not.toMatch(/not registered|no account/i);
      }
    });
});
