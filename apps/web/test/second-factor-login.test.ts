import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readBody, setResponseStatus } from 'h3';
import SecondFactorPage from '../app/pages/login/second-factor.vue';
import { setSiteLocale } from './support/locale';
import { landedOn } from './support/routing';

// Only a return path outside this app's own pages goes through `navigateTo`,
// which would be a real browser navigation; stub it so that case can assert
// the target. An in-app return path moves the client router instead, so these
// tests read the router itself: test/auth-landing.test.ts explains why
// asserting the helper cannot see whether the page actually left.
mockNuxtImport('navigateTo', () => vi.fn());

// h3's `getHeader` does not resolve against `registerEndpoint`'s mocked
// event in this Nuxt test environment; read the raw Node request headers
// instead, the same way test/useAuth-csrf-rotation.test.ts does.
interface MockEvent {
  node?: { req?: { headers?: Record<string, string> } };
}

function requestHeader(event: MockEvent, name: string): string | undefined {
  const headers = event.node?.req?.headers ?? {};
  const key = Object.keys(headers).find(
    (k) => k.toLowerCase() === name.toLowerCase(),
  );
  return key ? headers[key] : undefined;
}

const CSRF_TOKEN = 'A'.repeat(43);
const DEFAULT_EXPIRES_AT = '2026-09-20T09:05:00Z';

const VALID_ASSERTION_PUBLIC_KEY = {
  challenge: 'A'.repeat(43),
  timeout: 300000,
  rpId: 'aboutme.vn',
  allowCredentials: [{ type: 'public-key', id: 'B'.repeat(22) }],
  userVerification: 'required',
};

interface StatusOverrides {
  status?: number;
  errorCode?: string;
  purpose?: 'login' | 'reauth';
  methods?: string[];
  returnPath?: string;
  csrfToken?: string;
}

/** Registers `GET /api/v1/auth/second-factor` for one test. */
function registerStatus(overrides: StatusOverrides = {}): void {
  const {
    status = 200,
    errorCode = 'authentication_required',
    purpose = 'login',
    methods = ['passkey', 'recovery'],
    returnPath = '/app/resumes',
    csrfToken = CSRF_TOKEN,
  } = overrides;
  registerEndpoint('/api/v1/auth/second-factor', {
    method: 'GET',
    handler: (event) => {
      setResponseStatus(event, status);
      if (status !== 200) {
        return { error: { code: errorCode, message: 'x' } };
      }
      return {
        data: {
          purpose,
          methods,
          expiresAt: DEFAULT_EXPIRES_AT,
          returnPath,
          csrfToken,
        },
      };
    },
  });
}

/** Registers `POST /api/v1/auth/second-factor/passkey/options`. */
function registerPasskeyOptions(
  publicKey: unknown = VALID_ASSERTION_PUBLIC_KEY,
): void {
  registerEndpoint('/api/v1/auth/second-factor/passkey/options', {
    method: 'POST',
    handler: () => ({ data: { ceremonyId: 'C'.repeat(43), publicKey } }),
  });
}

interface VerifyResult {
  status: number;
  errorCode?: string;
}

/**
 * Registers a pending verify POST. When a test does not need the request
 * body or headers, the handler stays synchronous (no `await readBody`):
 * `registerEndpoint`'s mock handler hangs the client's `$fetch` forever for
 * an `async` handler that both awaits and answers with a non-2xx status —
 * reproduced directly against this exact combination, and never against a
 * synchronous handler (`registerStatus` below sets 401/503 the same way,
 * synchronously, and works) or an `async` handler that answers 2xx. Every
 * currently-passing inspection call already only checks the 2xx path, so
 * this keeps those on the working `async` shape and only the error-status
 * registrations move to the working synchronous shape.
 */
function registerVerify(
  path: string,
  result: VerifyResult,
  onRequest?: (headers: Record<string, unknown>, body: unknown) => void,
): void {
  const respond = (): unknown => {
    if (result.status >= 400) {
      return { error: { code: result.errorCode, message: 'x' } };
    }
    return null;
  };
  registerEndpoint(path, {
    method: 'POST',
    handler: onRequest
      ? async (event) => {
        const body = await readBody(event);
        onRequest({ csrfToken: requestHeader(event, 'x-csrf-token') }, body);
        setResponseStatus(event, result.status);
        return respond();
      }
      : (event) => {
          setResponseStatus(event, result.status);
          return respond();
        },
  });
}

/** Registers `POST /api/v1/auth/second-factor/passkey/verify`. */
function registerPasskeyVerify(
  result: VerifyResult,
  onRequest?: (headers: Record<string, unknown>, body: unknown) => void,
): void {
  registerVerify(
    '/api/v1/auth/second-factor/passkey/verify',
    result,
    onRequest,
  );
}

/** Registers `POST /api/v1/auth/second-factor/recovery/verify`. */
function registerRecoveryVerify(
  result: VerifyResult,
  onRequest?: (headers: Record<string, unknown>, body: unknown) => void,
): void {
  registerVerify(
    '/api/v1/auth/second-factor/recovery/verify',
    result,
    onRequest,
  );
}

function mockAssertionCredential(): unknown {
  return {
    rawId: new Uint8Array([1, 2, 3]).buffer,
    response: {
      clientDataJSON: new Uint8Array([4, 5]).buffer,
      authenticatorData: new Uint8Array([6, 7]).buffer,
      signature: new Uint8Array([8]).buffer,
      userHandle: null,
    },
  };
}

function stubWebAuthnSupport(
  get: (...args: unknown[]) => Promise<unknown> = () =>
    Promise.resolve(mockAssertionCredential()),
): void {
  Object.defineProperty(window, 'PublicKeyCredential', {
    value: function PublicKeyCredentialStub() {},
    configurable: true,
  });
  Object.defineProperty(navigator, 'credentials', {
    value: { get: vi.fn(get) },
    configurable: true,
  });
}

function clearWebAuthnSupport(): void {
  Reflect.deleteProperty(window, 'PublicKeyCredential');
  Reflect.deleteProperty(navigator, 'credentials');
}

beforeEach(() => {
  setSiteLocale('en');
  stubWebAuthnSupport();
});

afterEach(clearWebAuthnSupport);

describe('second-factor.vue loading and method rendering', () => {
  it('shows a loading state before the pending status resolves', async () => {
    let release: (body: unknown) => void = () => {};
    registerEndpoint('/api/v1/auth/second-factor', {
      method: 'GET',
      handler: () => new Promise((resolve) => {
        release = resolve;
      }),
    });
    const wrapper = await mountSuspended(SecondFactorPage);
    expect(wrapper.find('[data-testid="second-factor-loading"]').exists())
      .toBe(true);
    expect(wrapper.find('[data-testid="second-factor-passkey"]').exists())
      .toBe(false);

    release({
      data: {
        purpose: 'login',
        methods: ['passkey'],
        expiresAt: DEFAULT_EXPIRES_AT,
        returnPath: '/app/resumes',
        csrfToken: CSRF_TOKEN,
      },
    });
    // The mocked fetch resolves through a real round trip, so the pending
    // status's own resolution needs an extra macrotask tick beyond the
    // first flush — the same double-flush test/connected-agents.test.ts
    // uses for its own delayed mock response.
    await flushPromises();
    await flushPromises();
    expect(wrapper.find('[data-testid="second-factor-loading"]').exists())
      .toBe(false);
  });

  it('renders passkey before recovery when both methods are available',
    async () => {
      registerStatus({ methods: ['passkey', 'recovery'] });
      const wrapper = await mountSuspended(SecondFactorPage);
      await flushPromises();
      const passkey = wrapper.get('[data-testid="second-factor-passkey"]');
      const recovery = wrapper.get('[data-testid="second-factor-recovery"]');
      expect(
        (passkey.element.compareDocumentPosition(recovery.element)
          & Node.DOCUMENT_POSITION_FOLLOWING) !== 0,
      ).toBe(true);
    });

  it('renders only recovery when the account has no active passkey',
    async () => {
      registerStatus({ methods: ['recovery'] });
      const wrapper = await mountSuspended(SecondFactorPage);
      await flushPromises();
      expect(wrapper.find('[data-testid="second-factor-passkey"]').exists())
        .toBe(false);
      expect(wrapper.get('[data-testid="second-factor-recovery"]').exists())
        .toBe(true);
    });

  it('shows a refresh prompt alone for an unknown method', async () => {
    registerStatus({ methods: ['totp'] });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-refresh-prompt"]').text())
      .toContain('Refresh');
    expect(wrapper.find('[data-testid="second-factor-passkey"]').exists())
      .toBe(false);
    expect(wrapper.find('[data-testid="second-factor-recovery"]').exists())
      .toBe(false);
  });

  it('shows a refresh prompt beside recovery for an unknown method',
    async () => {
      registerStatus({ methods: ['totp', 'recovery'] });
      const wrapper = await mountSuspended(SecondFactorPage);
      await flushPromises();
      expect(
        wrapper.find('[data-testid="second-factor-refresh-prompt"]').exists(),
      ).toBe(true);
      expect(wrapper.get('[data-testid="second-factor-recovery"]').exists())
        .toBe(true);
      expect(wrapper.find('[data-testid="second-factor-passkey"]').exists())
        .toBe(false);
    });
});

describe('second-factor.vue ignores its own query string', () => {
  it('never turns query text into copy, markup, or a request target',
    async () => {
      let requestedUrl: string | null = null;
      registerEndpoint('/api/v1/auth/second-factor', {
        method: 'GET',
        handler: (event) => {
          requestedUrl = event.node.req.url ?? null;
          return {
            data: {
              purpose: 'login',
              methods: ['recovery'],
              expiresAt: DEFAULT_EXPIRES_AT,
              returnPath: '/app/resumes',
              csrfToken: CSRF_TOKEN,
            },
          };
        },
      });
      const wrapper = await mountSuspended(SecondFactorPage, {
        route: '/login/second-factor'
          + '?evil=%3Cscript%3Ealert(1)%3C%2Fscript%3E&next=/etc/passwd',
      });
      await flushPromises();
      expect(wrapper.html()).not.toContain('<script>alert(1)</script>');
      expect(wrapper.html()).not.toContain('/etc/passwd');
      // The pending read never carries this page's own query string onward.
      expect(requestedUrl).not.toContain('evil');
      expect(requestedUrl).not.toContain('etc%2Fpasswd');
    });
});

describe('second-factor.vue expiry and availability', () => {
  it('shows the expired state with a login link on a missing or expired '
    + 'pending row', async () => {
    registerStatus({ status: 401, errorCode: 'authentication_required' });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-expired"]').exists())
      .toBe(true);
    expect(
      wrapper.get('[data-testid="second-factor-sign-in-again"]')
        .attributes('href'),
    ).toBe('/login?error=authentication_required');
  });

  it('sends an already-known reauth pending failure back to settings, '
    + 'remembered from the earlier status read', async () => {
    registerStatus({ purpose: 'reauth', methods: ['recovery'] });
    registerRecoveryVerify({
      status: 401,
      errorCode: 'authentication_required',
    });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('#second-factor-recovery-code')
      .setValue('amr_00000-00000-00000-00000-00000-0');
    await wrapper.get('[data-testid="second-factor-recovery-form"]')
      .trigger('submit');
    await flushPromises();
    expect(
      wrapper.get('[data-testid="second-factor-sign-in-again"]')
        .attributes('href'),
    ).toBe('/app/settings/sessions?error=authentication_required');
  });

  it('shows an unavailable message on a dependency failure', async () => {
    registerStatus({ status: 503, errorCode: 'authentication_unavailable' });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-unavailable"]').text())
      .toContain('Something went wrong');
  });
});

describe('second-factor.vue passkey completion', () => {
  it('completes an assertion and navigates to the validated return path',
    async () => {
      registerStatus({ methods: ['passkey'], returnPath: '/app/resumes' });
      registerPasskeyOptions();
      let sentCsrfToken: unknown;
      registerPasskeyVerify({ status: 204 }, (headers) => {
        sentCsrfToken = headers.csrfToken;
      });
      const wrapper = await mountSuspended(SecondFactorPage, {
        route: '/login/second-factor',
      });
      await flushPromises();
      await wrapper.get('[data-testid="second-factor-passkey-button"]')
        .trigger('click');
      await flushPromises();
      expect(sentCsrfToken).toBe(CSRF_TOKEN);
      expect(await landedOn('/app/resumes')).toBe('/app/resumes');
    });

  it('navigates externally when the return path is a server-only route, '
    + 'not a client page', async () => {
    registerStatus({
      methods: ['passkey'],
      returnPath: '/oauth/authorize?client_id=abc',
    });
    registerPasskeyOptions();
    registerPasskeyVerify({ status: 204 });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    await flushPromises();
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
      '/oauth/authorize?client_id=abc',
      { external: true },
    );
  });

  it('falls back to the default return path when the pending row carries '
    + 'an invalid one', async () => {
    registerStatus({ methods: ['passkey'], returnPath: '//evil.example' });
    registerPasskeyOptions();
    registerPasskeyVerify({ status: 204 });
    const wrapper = await mountSuspended(SecondFactorPage, {
      route: '/login/second-factor',
    });
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    await flushPromises();
    expect(await landedOn('/app/resumes')).toBe('/app/resumes');
  });

  it('shows a retryable error and keeps the button enabled on a failed '
    + 'assertion', async () => {
    registerStatus({ methods: ['passkey'] });
    registerPasskeyOptions();
    registerPasskeyVerify({ status: 401, errorCode: 'verification_failed' });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    // The passkey ceremony chains an options fetch, the mocked credential
    // ceremony, and the verify fetch; a non-2xx verify response needs an
    // extra macrotask tick beyond the first flush to finish propagating.
    await flushPromises();
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-passkey-error"]').text())
      .toContain('Verification failed');
    const button = wrapper.get('[data-testid="second-factor-passkey-button"]');
    expect(button.attributes('disabled')).toBeUndefined();
    expect(wrapper.find('[data-testid="second-factor-expired"]').exists())
      .toBe(false);
  });

  it('shows a cancellation message and never calls verify when the '
    + 'browser cancels', async () => {
    registerStatus({ methods: ['passkey'] });
    registerPasskeyOptions();
    let verifyCalled = false;
    registerPasskeyVerify({ status: 204 }, () => {
      verifyCalled = true;
    });
    stubWebAuthnSupport(() =>
      Promise.reject(new DOMException('cancelled', 'NotAllowedError')));
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-passkey-error"]').text())
      .toContain('cancelled');
    expect(verifyCalled).toBe(false);
  });

  it('shows a generic error for a malformed options response', async () => {
    registerStatus({ methods: ['passkey'] });
    registerPasskeyOptions({
      ...VALID_ASSERTION_PUBLIC_KEY,
      challenge: undefined,
    });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-passkey-error"]').text())
      .toContain('did not work');
  });

  it('shows the unsupported message and never starts a ceremony when the '
    + 'browser has no WebAuthn support', async () => {
    clearWebAuthnSupport();
    registerStatus({ methods: ['passkey', 'recovery'] });
    let optionsCalled = false;
    registerEndpoint('/api/v1/auth/second-factor/passkey/options', {
      method: 'POST',
      handler: () => {
        optionsCalled = true;
        return {
          data: { ceremonyId: 'x', publicKey: VALID_ASSERTION_PUBLIC_KEY },
        };
      },
    });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    expect(
      wrapper.get('[data-testid="second-factor-passkey-unsupported"]').text(),
    ).toContain('does not support passkeys');
    expect(
      wrapper.find('[data-testid="second-factor-passkey-button"]').exists(),
    ).toBe(false);
    expect(optionsCalled).toBe(false);
    expect(wrapper.get('[data-testid="second-factor-recovery"]').exists())
      .toBe(true);
  });

  it('shows a rate-limited message', async () => {
    registerStatus({ methods: ['passkey'] });
    registerPasskeyOptions();
    registerPasskeyVerify({ status: 429, errorCode: 'rate_limited' });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    await flushPromises();
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-passkey-error"]').text())
      .toContain('Too many attempts');
  });
});

describe('second-factor.vue recovery completion', () => {
  it('completes a recovery code and navigates to the return path',
    async () => {
      registerStatus({ methods: ['recovery'], returnPath: '/app/resumes' });
      let sentBody: unknown;
      let sentCsrfToken: unknown;
      registerRecoveryVerify({ status: 204 }, (headers, body) => {
        sentCsrfToken = headers.csrfToken;
        sentBody = body;
      });
      const wrapper = await mountSuspended(SecondFactorPage, {
        route: '/login/second-factor',
      });
      await flushPromises();
      await wrapper.get('#second-factor-recovery-code')
        .setValue('amr_00000-00000-00000-00000-00000-0');
      await wrapper.get('[data-testid="second-factor-recovery-form"]')
        .trigger('submit');
      await flushPromises();
      expect(sentCsrfToken).toBe(CSRF_TOKEN);
      expect(sentBody).toEqual({ code: 'amr_00000-00000-00000-00000-00000-0' });
      expect(await landedOn('/app/resumes')).toBe('/app/resumes');
    });

  it('clears the recovery code input after a failed attempt', async () => {
    registerStatus({ methods: ['recovery'] });
    registerRecoveryVerify({ status: 401, errorCode: 'verification_failed' });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    const input = wrapper.get('#second-factor-recovery-code');
    await input.setValue('amr_00000-00000-00000-00000-00000-0');
    await wrapper.get('[data-testid="second-factor-recovery-form"]')
      .trigger('submit');
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-recovery-error"]').text())
      .toContain('Verification failed');
    expect((input.element as HTMLInputElement).value).toBe('');
  });

  it('requires a non-empty code before sending a request', async () => {
    registerStatus({ methods: ['recovery'] });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-recovery-form"]')
      .trigger('submit');
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-recovery-error"]').text())
      .toContain('Enter your recovery code');
  });
});

describe('second-factor.vue never treats the pending cookie as a session',
  () => {
    it('never calls /me across a full completion and an expired attempt',
      async () => {
        let meCalled = false;
        registerEndpoint('/api/v1/me', () => {
          meCalled = true;
          return { data: { user: null } };
        });
        registerStatus({ methods: ['passkey'] });
        registerPasskeyOptions();
        registerPasskeyVerify({
          status: 401,
          errorCode: 'authentication_required',
        });
        const wrapper = await mountSuspended(SecondFactorPage);
        await flushPromises();
        await wrapper.get('[data-testid="second-factor-passkey-button"]')
          .trigger('click');
        await flushPromises();
        await flushPromises();
        expect(wrapper.get('[data-testid="second-factor-expired"]').exists())
          .toBe(true);
        expect(meCalled).toBe(false);
      });
  });

describe('second-factor.vue locales', () => {
  it('renders the title and lead in Vietnamese', async () => {
    setSiteLocale('vi');
    registerStatus({ purpose: 'login', methods: ['recovery'] });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    expect(wrapper.get('[data-page-title]').text()).toBe('Xác thực hai bước');
    expect(wrapper.text()).toContain(
      'Hoàn tất đăng nhập bằng passkey hoặc mã khôi phục.',
    );
  });

  it('updates a shown error to the new language without losing the error '
    + 'state', async () => {
    registerStatus({ methods: ['passkey'] });
    registerPasskeyOptions();
    registerPasskeyVerify({ status: 401, errorCode: 'verification_failed' });
    const wrapper = await mountSuspended(SecondFactorPage);
    await flushPromises();
    await wrapper.get('[data-testid="second-factor-passkey-button"]')
      .trigger('click');
    await flushPromises();
    await flushPromises();
    expect(wrapper.get('[data-testid="second-factor-passkey-error"]').text())
      .toContain('Verification failed');

    useLocale().setLocale('vi');
    await flushPromises();

    expect(wrapper.get('[data-testid="second-factor-passkey-error"]').text())
      .toBe('Xác thực không thành công. Hãy thử lại.');
  });
});
