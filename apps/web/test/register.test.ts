import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readRawBody, setResponseStatus } from 'h3';
import RegisterPage from '../app/pages/register.vue';
import {
  type PasswordAuthFailure,
  type PasswordAuthError,
  type PasswordIssue,
  usePasswordAuth,
} from '../app/composables/usePasswordAuth';

// The credentials test needs to observe the `credentials` option, which the
// test runtime's `$fetch` swallows before the h3 mock server sees it. Route
// every auto-imported `$fetch` call through a delegating spy so the init is
// observable while real requests still reach `registerEndpoint`.
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const real: any = (globalThis as { $fetch: unknown }).$fetch;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return { fetchMock: vi.fn((...args: any[]) => real(...args)) };
});
mockNuxtImport('$fetch', () => mocks.fetchMock);

// Async body reads (readRawBody) and deferred responses resolve a macrotask
// after `flushPromises` finishes — settle past those.
const settle = async (): Promise<void> => {
  await flushPromises();
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
  await flushPromises();
};

const validInput = {
  name: 'Ada Lovelace',
  email: 'ada@example.com',
  password: 'correct horse battery staple',
};

function failureMatcher(kind: PasswordAuthError, issue?: PasswordIssue) {
  return issue === undefined ? { kind } : { kind, issue };
}

describe('usePasswordAuth.register', () => {
  it('resolves when the server accepts the registration (202)', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 202);
        return { data: { accepted: true } };
      },
    });
    await expect(usePasswordAuth().register(validInput))
      .resolves.toBeUndefined();
  });

  it('maps 400 request_invalid to invalid-request', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 400);
        return { error: { code: 'request_invalid', message: 'x' } };
      },
    });
    await expect(usePasswordAuth().register(validInput)).rejects.toMatchObject(
      failureMatcher('invalid-request'),
    );
  });

  it('maps 422 password_invalid to password-invalid with each closed issue',
    async () => {
      for (const issue of ['length', 'common', 'breached'] as const) {
        registerEndpoint('/api/v1/auth/password/register', {
          method: 'POST',
          handler: (event) => {
            setResponseStatus(event, 422);
            return {
              error: {
                code: 'password_invalid',
                message: 'x',
                details: { issue },
              },
            };
          },
        });
        await expect(
          usePasswordAuth().register(validInput),
        ).rejects.toMatchObject(failureMatcher('password-invalid', issue));
      }
    });

  it('drops an unknown or hostile details.issue value', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 422);
        return {
          error: {
            code: 'password_invalid',
            message: 'x',
            details: { issue: '__proto__' },
          },
        };
      },
    });
    const failure = await usePasswordAuth().register(validInput)
      .catch((error) => error as PasswordAuthFailure);
    expect(failure.kind).toBe('password-invalid');
    expect(failure.issue).toBeUndefined();
  });

  it('maps 429 rate_limited to rate-limited', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 429);
        return { error: { code: 'rate_limited', message: 'x' } };
      },
    });
    await expect(usePasswordAuth().register(validInput)).rejects.toMatchObject(
      failureMatcher('rate-limited'),
    );
  });

  it('maps 503 authentication_unavailable to unavailable', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 503);
        return {
          error: { code: 'authentication_unavailable', message: 'x' },
        };
      },
    });
    await expect(usePasswordAuth().register(validInput)).rejects.toMatchObject(
      failureMatcher('unavailable'),
    );
  });

  it('falls back to unavailable for an unknown status/code pair', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 500);
        return { error: { code: 'internal_error', message: 'x' } };
      },
    });
    await expect(usePasswordAuth().register(validInput)).rejects.toMatchObject(
      failureMatcher('unavailable'),
    );
  });

  it('falls back to unavailable for prototype-pollution error codes',
    async () => {
      registerEndpoint('/api/v1/auth/password/register', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 400);
          return { error: { code: 'constructor', message: 'x' } };
        },
      });
      await expect(usePasswordAuth().register(validInput))
        .rejects.toMatchObject(
          failureMatcher('unavailable'),
        );
    });

  it('posts the register body and sends credentials', async () => {
    mocks.fetchMock.mockClear();
    let receivedBody: string | null = null;
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: async (event) => {
        receivedBody = await readRawBody(event);
        setResponseStatus(event, 202);
        return { data: { accepted: true } };
      },
    });
    await usePasswordAuth().register(validInput);
    expect(JSON.parse(receivedBody ?? '{}')).toEqual(validInput);
    const init = mocks.fetchMock.mock.calls[0]?.[1] as { credentials?: string }
      | undefined;
    expect(init?.credentials).toBe('include');
  });
});

describe('register.vue', () => {
  it('composes the auth card and generated fields', async () => {
    const wrapper = await mountSuspended(RegisterPage);

    expect(wrapper.find('[data-slot="card"]').exists()).toBe(true);
    expect(wrapper.findAll('[data-slot="input"]').length).toBeGreaterThan(0);
    expect(
      wrapper
        .findAll('[data-slot="input"]')
        .every((input) => input.attributes('id') !== undefined),
    ).toBe(true);
  });

  it('keeps password toggle semantics and native autofill', async () => {
    const wrapper = await mountSuspended(RegisterPage);
    const password = wrapper.get('#register-password');
    const toggle = wrapper.get('[aria-label="Show Password"]');

    expect(password.attributes('autocomplete')).toBe('new-password');
    expect(toggle.attributes('aria-pressed')).toBe('false');
    expect(toggle.text()).toBe('Show');
    expect(wrapper.html()).not.toContain('@paste');

    await toggle.trigger('click');
    expect(password.attributes('type')).toBe('text');
    expect(toggle.attributes('aria-pressed')).toBe('true');
    expect(toggle.attributes('aria-label')).toBe('Hide Password');
    expect(toggle.text()).toBe('Hide');
  });

  it('renders name/email/password/confirm with correct autocomplete',
    async () => {
      const wrapper = await mountSuspended(RegisterPage);
      expect(wrapper.get('#register-name').attributes('autocomplete'))
        .toBe('name');
      expect(wrapper.get('#register-email').attributes('autocomplete'))
        .toBe('email');
      expect(wrapper.get('#register-password').attributes('autocomplete'))
        .toBe('new-password');
      expect(
        wrapper.get('#register-password-confirm').attributes('autocomplete'),
      ).toBe('new-password');
      expect(wrapper.get('#register-password').attributes('type'))
        .toBe('password');
      // Root carries the Nova/Zinc/Emerald theme token wrapper.
      expect(wrapper.get('[data-slot="card"]').exists()).toBe(true);
    });

  it('blocks submit with a local error when the passwords do not match',
    async () => {
      let calls = 0;
      registerEndpoint('/api/v1/auth/password/register', {
        method: 'POST',
        handler: (event) => {
          calls += 1;
          setResponseStatus(event, 202);
          return { data: { accepted: true } };
        },
      });
      const wrapper = await mountSuspended(RegisterPage);
      await wrapper.get('#register-name').setValue(validInput.name);
      await wrapper.get('#register-email').setValue(validInput.email);
      await wrapper.get('#register-password').setValue(validInput.password);
      await wrapper.get('#register-password-confirm')
        .setValue('a different password');
      await wrapper.get('[data-testid="register-form"]').trigger('submit');
      await flushPromises();
      expect(wrapper.get('[data-testid="register-error"]').text())
        .toContain('Passwords do not match');
      expect(calls).toBe(0);
    });

  it('shows the fixed generic 202 copy and never submits the confirmation',
    async () => {
      let receivedBody: string | null = null;
      registerEndpoint('/api/v1/auth/password/register', {
        method: 'POST',
        handler: async (event) => {
          receivedBody = await readRawBody(event);
          setResponseStatus(event, 202);
          return { data: { accepted: true } };
        },
      });
      const wrapper = await mountSuspended(RegisterPage);
      await wrapper.get('#register-name').setValue(validInput.name);
      await wrapper.get('#register-email').setValue(validInput.email);
      await wrapper.get('#register-password').setValue(validInput.password);
      await wrapper.get('#register-password-confirm')
        .setValue(validInput.password);
      await wrapper.get('[data-testid="register-form"]').trigger('submit');
      await settle();
      expect(wrapper.get('[data-testid="register-success"]').text()).toBe(
        'Check your email to verify your address.',
      );
      expect(JSON.parse(receivedBody ?? '{}')).toEqual(validInput);
    });

  it('shows closed copy for each password policy issue', async () => {
    const issueCopy: Record<string, string> = {
      length: 'at least 12',
      common: 'too common',
      breached: 'data breach',
    };
    for (const [issue, fragment] of Object.entries(issueCopy)) {
      registerEndpoint('/api/v1/auth/password/register', {
        method: 'POST',
        handler: (event) => {
          setResponseStatus(event, 422);
          return {
            error: {
              code: 'password_invalid',
              message: 'x',
              details: { issue },
            },
          };
        },
      });
      const wrapper = await mountSuspended(RegisterPage);
      await wrapper.get('#register-name').setValue(validInput.name);
      await wrapper.get('#register-email').setValue(validInput.email);
      await wrapper.get('#register-password').setValue('weak');
      await wrapper.get('#register-password-confirm').setValue('weak');
      await wrapper.get('[data-testid="register-form"]').trigger('submit');
      await flushPromises();
      expect(wrapper.get('[data-testid="register-error"]').text())
        .toContain(fragment);
    }
  });

  it('submits a weak password without client-side strength validation',
    async () => {
      let calls = 0;
      registerEndpoint('/api/v1/auth/password/register', {
        method: 'POST',
        handler: (event) => {
          calls += 1;
          setResponseStatus(event, 202);
          return { data: { accepted: true } };
        },
      });
      const wrapper = await mountSuspended(RegisterPage);
      await wrapper.get('#register-name').setValue(validInput.name);
      await wrapper.get('#register-email').setValue(validInput.email);
      await wrapper.get('#register-password').setValue('a');
      await wrapper.get('#register-password-confirm').setValue('a');
      await wrapper.get('[data-testid="register-form"]').trigger('submit');
      await flushPromises();
      // No character-class or strength hint is ever rendered.
      expect(wrapper.html()).not.toMatch(
        /uppercase|lowercase|number|symbol|special|strength/i,
      );
      expect(calls).toBe(1);
    });

  it('disables the submit button and shows pending text while in flight',
    async () => {
      let resolveRequest: () => void = () => {};
      registerEndpoint('/api/v1/auth/password/register', {
        method: 'POST',
        handler: (event) => new Promise<{ data: { accepted: boolean } }>(
          (resolve) => {
            resolveRequest = () => {
              setResponseStatus(event, 202);
              resolve({ data: { accepted: true } });
            };
          },
        ),
      });
      const wrapper = await mountSuspended(RegisterPage);
      await wrapper.get('#register-name').setValue(validInput.name);
      await wrapper.get('#register-email').setValue(validInput.email);
      await wrapper.get('#register-password').setValue(validInput.password);
      await wrapper.get('#register-password-confirm')
        .setValue(validInput.password);
      await wrapper.get('[data-testid="register-form"]').trigger('submit');
      await flushPromises();
      const button = wrapper.get('[data-slot="button"][type="submit"]');
      expect(button.attributes('disabled')).toBeDefined();
      expect(button.text()).toContain('Creating account');
      resolveRequest();
      await settle();
      expect(wrapper.get('[data-testid="register-success"]').exists())
        .toBe(true);
    });

  it('moves focus to the error summary on a server rejection', async () => {
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 422);
        return {
          error: {
            code: 'password_invalid',
            message: 'x',
            details: { issue: 'length' },
          },
        };
      },
    });
    // Attach to the live document: happy-dom's `focus()` is a no-op on a
    // detached tree, and this test asserts the error summary receives focus.
    const wrapper = await mountSuspended(RegisterPage, {
      attachTo: document.body,
    });
    await wrapper.get('#register-name').setValue(validInput.name);
    await wrapper.get('#register-email').setValue(validInput.email);
    await wrapper.get('#register-password').setValue('weak');
    await wrapper.get('#register-password-confirm').setValue('weak');
    await wrapper.get('[data-testid="register-form"]').trigger('submit');
    await settle();
    const summary = wrapper.get('[data-testid="register-error"]');
    expect(summary.attributes('tabindex')).toBe('-1');
    expect(document.activeElement).toBe(summary.element);
  });
});
