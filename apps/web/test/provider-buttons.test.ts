import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import LoginPage from '../app/pages/login.vue';
import RegisterPage from '../app/pages/register.vue';
import VerifyEmailPage from '../app/pages/verify-email.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

mockNuxtImport('navigateTo', () => vi.fn());

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

interface Queryable {
  findAll(selector: string): {
    attributes(name: string): string | undefined;
    text(): string;
  }[];
}

function providerLinks(wrapper: Queryable): Record<string, string> {
  const out: Record<string, string> = {};
  for (const link of wrapper.findAll('a[data-provider]')) {
    out[link.attributes('data-provider') ?? ''] = link.text();
  }
  return out;
}

async function mountAfterCapabilities(
  page: typeof LoginPage,
): Promise<Wrapper> {
  const wrapper = await mountSuspended(page);
  await flushPromises();
  return wrapper;
}

async function registerSuccessfully(): Promise<Wrapper> {
  registerEndpoint('/api/v1/auth/password/register', {
    method: 'POST',
    handler: (event) => {
      setResponseStatus(event, 202);
      return { data: { accepted: true } };
    },
  });
  const wrapper = await mountAfterCapabilities(RegisterPage);
  await wrapper.get('#register-name').setValue('Ada Lovelace');
  await wrapper.get('#register-email').setValue('ada@example.com');
  await wrapper.get('#register-password').setValue('correct horse battery');
  await wrapper.get('#register-password-confirm')
    .setValue('correct horse battery');
  await wrapper.get('[data-testid="register-form"]').trigger('submit');
  await flushPromises();
  expect(wrapper.find('[data-testid="register-success"]').exists()).toBe(true);
  return wrapper;
}

async function mountExpiredVerification(): Promise<Wrapper> {
  window.location.hash = '#token=expired-token';
  registerEndpoint('/api/v1/auth/password/verify', {
    method: 'POST',
    handler: (event) => {
      setResponseStatus(event, 400);
      return { error: { code: 'credential_token_invalid', message: 'x' } };
    },
  });
  return mountAfterCapabilities(VerifyEmailPage);
}

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
  window.location.hash = '';
});

describe('provider buttons', () => {
  it('draws the Google button with its inline G mark and no request',
    async () => {
      registerCapabilities({ providerLogin: true, agentAccess: false });
      const wrapper = await mountAfterCapabilities(LoginPage);
      const google = wrapper.get('a[data-provider="google"]');

      expect(google.attributes('href')).toBe('/api/v1/auth/google/start');
      expect(google.text()).toBe('Tiếp tục với Google');
      const mark = google.get('svg');
      expect(mark.attributes('aria-hidden')).toBe('true');
      expect(mark.findAll('path').map((p) => p.attributes('fill'))).toEqual(
        ['#EA4335', '#4285F4', '#FBBC05', '#34A853'],
      );
      expect(google.find('img, image, use').exists()).toBe(false);
      expect(google.classes()).toContain('font-[family-name:Roboto]');
    });

  it('renders every enabled provider on login and register', async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    for (const page of [LoginPage, RegisterPage]) {
      const wrapper = await mountAfterCapabilities(page);
      expect(providerLinks(wrapper)).toEqual({
        google: 'Tiếp tục với Google',
        github: 'Tiếp tục với GitHub',
        linkedin: 'Tiếp tục với LinkedIn',
      });
    }
    setSiteLocale('en');
    const english = await mountAfterCapabilities(RegisterPage);
    expect(providerLinks(english).google).toBe('Continue with Google');
    expect(
      english.get('a[data-provider="google"]').attributes('href'),
    ).toBe('/api/v1/auth/google/start');
  });

  it('renders only the listed providers, in the fixed order', async () => {
    registerCapabilities({
      providerLogin: true,
      agentAccess: false,
      providers: ['linkedin', 'google', 'unknown'],
    });
    const wrapper = await mountAfterCapabilities(LoginPage);
    expect(Object.keys(providerLinks(wrapper))).toEqual(['google', 'linkedin']);

    registerCapabilities({
      providerLogin: true,
      agentAccess: false,
      providers: ['google'],
    });
    clearNuxtData();
    const googleOnly = await mountAfterCapabilities(RegisterPage);
    expect(providerLinks(googleOnly)).toEqual({
      google: 'Tiếp tục với Google',
    });
  });

  it('renders no provider option for an empty list', async () => {
    registerCapabilities({
      providerLogin: false,
      agentAccess: false,
      providers: [],
    });
    const wrapper = await mountAfterCapabilities(LoginPage);
    expect(wrapper.find('[data-testid="provider-buttons"]').exists()).toBe(
      false,
    );
  });

  it('renders no provider option when the list is missing', async () => {
    registerEndpoint('/api/v1/capabilities', () => ({
      data: { providerLogin: true, agentAccess: false },
    }));
    const wrapper = await mountAfterCapabilities(LoginPage);
    expect(wrapper.find('[data-testid="provider-buttons"]').exists()).toBe(
      false,
    );
  });

  it('renders no provider option when none is enabled', async () => {
    registerCapabilities({ providerLogin: false, agentAccess: false });
    for (const page of [LoginPage, RegisterPage]) {
      const wrapper = await mountAfterCapabilities(page);
      expect(wrapper.find('[data-testid="provider-buttons"]').exists()).toBe(
        false,
      );
      // The brand panel beside the form lists unrelated facts; check the
      // form column only (DESIGN.md, auth pages).
      const form = wrapper.find('[data-auth-form]');
      expect((form.exists() ? form : wrapper).text())
        .not.toMatch(/Google|hoặc/u);
    }
  });
});

describe('provider space before the capabilities read', () => {
  it('holds one button of space with no link until it resolves', async () => {
    let release!: () => void;
    registerEndpoint('/api/v1/capabilities', () => new Promise((resolve) => {
      release = () => resolve({
        data: {
          providerLogin: true,
          agentAccess: false,
          providers: ['google'],
        },
      });
    }));
    for (const page of [LoginPage, RegisterPage]) {
      clearNuxtData();
      const wrapper = await mountSuspended(page);
      const placeholder = wrapper.get('[data-testid="provider-placeholder"]');
      expect(placeholder.attributes('aria-hidden')).toBe('true');
      expect(placeholder.classes()).toContain('invisible');
      expect(wrapper.find('a[data-provider]').exists()).toBe(false);

      release();
      await flushPromises();
      await flushPromises();
      expect(wrapper.find('[data-testid="provider-placeholder"]').exists())
        .toBe(false);
      expect(wrapper.find('a[data-provider="google"]').exists()).toBe(true);
    }
  });
});

describe('missing verification email', () => {
  it('offers spam advice and Google after registration', async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    const wrapper = await registerSuccessfully();
    const notice = wrapper.get('[data-testid="register-no-email"]');

    expect(notice.text()).toContain(
      'Nếu sau vài phút vẫn chưa thấy email, hãy kiểm tra thư mục thư rác.',
    );
    expect(notice.get('[data-testid="register-no-email-google"]').text())
      .toBe('Hoặc đăng nhập bằng tài khoản Google của bạn:');
    expect(providerLinks(notice)).toEqual({
      google: 'Tiếp tục với Google',
    });
    expect(notice.text()).not.toMatch(/không gửi được|thất bại/u);
  });

  it('gives the English notice without Google when it is off', async () => {
    registerCapabilities({ providerLogin: false, agentAccess: false });
    setSiteLocale('en');
    const wrapper = await registerSuccessfully();
    const notice = wrapper.get('[data-testid="register-no-email"]');

    expect(notice.text()).toBe(
      'If the email has not arrived within a few minutes, check your spam '
      + 'folder.',
    );
    expect(
      notice.find('[data-testid="register-no-email-google"]').exists(),
    ).toBe(false);
    expect(notice.find('a[data-provider]').exists()).toBe(false);
  });

  it('offers Google on an expired verification link', async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    const wrapper = await mountExpiredVerification();

    expect(wrapper.get('[data-testid="verify-error"]').text()).toBe(
      'Đường dẫn xác minh này không hợp lệ hoặc đã hết hạn.',
    );
    const google = wrapper.get('[data-testid="verify-google"]');
    expect(google.text()).toContain(
      'Hoặc đăng nhập bằng tài khoản Google của bạn:',
    );
    expect(google.get('a[data-provider="google"]').attributes('href')).toBe(
      '/api/v1/auth/google/start',
    );
  });

  it('offers Google on an incomplete link in English', async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    setSiteLocale('en');
    const wrapper = await mountAfterCapabilities(VerifyEmailPage);

    expect(wrapper.get('[data-testid="verify-google"]').text()).toContain(
      'Or sign in with your Google account instead:',
    );
  });

  it('offers nothing on a bad link when Google is off', async () => {
    registerCapabilities({ providerLogin: false, agentAccess: false });
    const wrapper = await mountExpiredVerification();

    expect(wrapper.find('[data-testid="verify-error"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="verify-google"]').exists()).toBe(false);
  });

  it('offers nothing when verification is only rate limited', async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    window.location.hash = '#token=some-token';
    registerEndpoint('/api/v1/auth/password/verify', {
      method: 'POST',
      handler: (event) => {
        setResponseStatus(event, 429);
        return { error: { code: 'rate_limited', message: 'x' } };
      },
    });
    const wrapper = await mountAfterCapabilities(VerifyEmailPage);

    expect(wrapper.find('[data-testid="verify-error"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="verify-google"]').exists()).toBe(false);
  });
});
