import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import ForgotPasswordPage from '../app/pages/forgot-password.vue';
import LoginPage from '../app/pages/login.vue';
import RegisterPage from '../app/pages/register.vue';
import ResetPasswordPage from '../app/pages/reset-password.vue';
import VerifyEmailPage from '../app/pages/verify-email.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

registerCapabilities();
mockNuxtImport('navigateTo', () => vi.fn());
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

function title(wrapper: Awaited<ReturnType<typeof mountSuspended>>): string {
  return wrapper.get('[data-page-title]').text();
}

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
});

describe('account pages in Vietnamese by default', () => {
  it('renders the sign-in page in Vietnamese', async () => {
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(title(wrapper)).toBe('Đăng nhập');
    expect(wrapper.text()).toContain('Dùng email và mật khẩu của tài khoản.');
    expect(wrapper.get('label[for="login-password"]').text()).toBe(
      'Mật khẩu',
    );
    expect(wrapper.find('[aria-label="Hiện mật khẩu"]').exists()).toBe(true);
    expect(wrapper.get('a[href="/forgot-password"]').text()).toBe(
      'Quên mật khẩu?',
    );
    expect(wrapper.get('[href="/api/v1/auth/google/start"]').text()).toBe(
      'Tiếp tục với Google',
    );

    await wrapper.get('[data-testid="login-form"]').trigger('submit');
    expect(wrapper.get('[data-testid="login-form-error"]').text()).toBe(
      'Nhập email và mật khẩu.',
    );
  });

  it('maps provider callback errors to Vietnamese copy', async () => {
    const wrapper = await mountSuspended(LoginPage, {
      route: '/login?error=cancelled',
    });
    expect(wrapper.get('[data-testid="login-error"]').text()).toBe(
      'Đã hủy đăng nhập.',
    );
  });

  it('renders registration in Vietnamese', async () => {
    const wrapper = await mountSuspended(RegisterPage);
    expect(title(wrapper)).toBe('Tạo tài khoản');
    expect(wrapper.get('label[for="register-name"]').text()).toBe('Họ và tên');
    expect(wrapper.get('label[for="register-password-confirm"]').text()).toBe(
      'Nhập lại mật khẩu',
    );
    expect(wrapper.text()).toContain('Đã có tài khoản?');

    await wrapper.get('[data-testid="register-form"]').trigger('submit');
    await flushPromises();
    expect(wrapper.get('[data-testid="register-error"]').text()).toBe(
      'Hãy điền đủ các trường.',
    );
  });

  it('renders password recovery in Vietnamese', async () => {
    const wrapper = await mountSuspended(ForgotPasswordPage);
    expect(title(wrapper)).toBe('Quên mật khẩu');
    expect(wrapper.get('button[type="submit"]').text()).toBe(
      'Gửi đường dẫn đặt lại',
    );
    expect(wrapper.get('a[href="/login"]').text()).toBe('Quay lại đăng nhập');
  });

  it('renders a missing reset link in Vietnamese', async () => {
    const wrapper = await mountSuspended(ResetPasswordPage);
    expect(title(wrapper)).toBe('Đặt lại mật khẩu');
    expect(wrapper.get('[data-testid="reset-error"]').text()).toBe(
      'Đường dẫn đặt lại này không hợp lệ hoặc không đầy đủ.',
    );
  });

  it('renders a missing verification link in Vietnamese', async () => {
    const wrapper = await mountSuspended(VerifyEmailPage);
    expect(title(wrapper)).toBe('Xác minh email');
    expect(wrapper.get('[data-testid="verify-error"]').text()).toBe(
      'Đường dẫn xác minh này không hợp lệ hoặc không đầy đủ.',
    );
  });
});

describe('account page language toggle', () => {
  it('switches a shown error and the page copy to English', async () => {
    const wrapper = await mountSuspended(AppRoot, { route: '/login' });
    await flushPromises();
    // The head manager writes html attributes after the render settles.
    await vi.waitFor(() => expect(document.documentElement.lang).toBe('vi'));
    await wrapper.get('[data-testid="login-form"]').trigger('submit');
    expect(wrapper.get('[data-testid="login-form-error"]').text()).toBe(
      'Nhập email và mật khẩu.',
    );

    await wrapper
      .get('[data-testid="app-shell"] [data-testid="landing-locale-en"]')
      .trigger('click');
    await flushPromises();

    expect(title(wrapper)).toBe('Sign in');
    expect(wrapper.get('[data-testid="login-form-error"]').text()).toBe(
      'Enter your email and password.',
    );
    await vi.waitFor(() => expect(document.documentElement.lang).toBe('en'));
    expect(document.cookie).toContain('aboutme-locale=en');
    wrapper.unmount();
  });
});
