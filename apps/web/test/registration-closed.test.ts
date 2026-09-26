import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import LoginPage from '../app/pages/login.vue';
import RegisterPage from '../app/pages/register.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

mockNuxtImport('navigateTo', () => vi.fn());

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

async function mountResolved(page: typeof RegisterPage): Promise<Wrapper> {
  const wrapper = await mountSuspended(page);
  await flushPromises();
  return wrapper;
}

function closedWith(providers: readonly string[]): void {
  registerCapabilities({
    providerLogin: providers.length > 0,
    agentAccess: false,
    providers,
    passwordRegistration: false,
  });
}

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
});

describe('email sign-up closed', () => {
  it.each([
    [
      'vi',
      'Đăng ký bằng email đang tạm đóng. Hãy tiếp tục bằng một trong các '
      + 'tài khoản sau.',
      'Tiếp tục với Google',
    ],
    [
      'en',
      'Email sign-up is temporarily closed. Continue with one of these '
      + 'accounts instead.',
      'Continue with Google',
    ],
  ] as const)('offers Google instead of the form (%s)', async (
    locale,
    note,
    google,
  ) => {
    setSiteLocale(locale);
    closedWith(['google']);
    const wrapper = await mountResolved(RegisterPage);

    expect(wrapper.find('[data-testid="register-form"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="register-closed"]').text()).toBe(note);
    expect(wrapper.get('a[data-provider="google"]').text()).toBe(google);
    expect(wrapper.find('[data-testid="register-divider"]').exists()).toBe(
      false,
    );
    const agreement = wrapper.get('[data-testid="register-agreement"]');
    const provider = wrapper.get('[data-testid="provider-buttons"]');
    expect(
      provider.element.compareDocumentPosition(agreement.element)
      & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it('offers every listed provider, in capabilities order, when closed',
    async () => {
      closedWith(['linkedin', 'google']);
      const wrapper = await mountResolved(RegisterPage);

      const links = wrapper.findAll('a[data-provider]')
        .map((link) => link.attributes('data-provider'));
      expect(links).toEqual(['google', 'linkedin']);
    });

  it('shows only the note and Sign in when no provider is listed', async () => {
    closedWith([]);
    const wrapper = await mountResolved(RegisterPage);

    expect(wrapper.find('[data-testid="register-closed"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="provider-buttons"]').exists()).toBe(
      false,
    );
    expect(wrapper.find('[data-testid="register-agreement"]').exists()).toBe(
      false,
    );
    expect(wrapper.get('main nav a[href="/login"]').text()).toBe('Đăng nhập');
  });

  it('keeps password sign-in and the Create account link on /login',
    async () => {
      closedWith(['google']);
      const wrapper = await mountResolved(LoginPage);

      expect(wrapper.find('[data-testid="login-form"]').exists()).toBe(true);
      expect(wrapper.get('main nav a[href="/register"]').text()).toBe(
        'Tạo tài khoản',
      );
    });
});

describe('email sign-up closed after the page loaded', () => {
  it('switches to the closed note when sign-up answers 404', async () => {
    registerCapabilities({
      providerLogin: true,
      agentAccess: false,
      providers: ['google'],
      passwordRegistration: true,
    });
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: (event) => {
        event.node.res.statusCode = 404;
        return { error: { code: 'not_found', message: 'Not found.' } };
      },
    });
    const wrapper = await mountResolved(RegisterPage);
    await wrapper.get('#register-name').setValue('Ada Lovelace');
    await wrapper.get('#register-email').setValue('ada@example.com');
    await wrapper.get('#register-password').setValue('correct horse battery');
    await wrapper.get('#register-password-confirm')
      .setValue('correct horse battery');
    await wrapper.get('[data-testid="register-form"]').trigger('submit');
    await flushPromises();

    expect(wrapper.find('[data-testid="register-form"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="register-closed"]').text()).toBe(
      'Đăng ký bằng email đang tạm đóng. Hãy tiếp tục bằng một trong các '
      + 'tài khoản sau.',
    );
    expect(wrapper.find('[data-testid="register-error"]').exists()).toBe(
      false,
    );
  });
});

describe('email sign-up open', () => {
  it.each(['vi', 'en'] as const)('shows the form (%s)', async (locale) => {
    setSiteLocale(locale);
    registerCapabilities({
      providerLogin: true,
      agentAccess: false,
      providers: ['google'],
      passwordRegistration: true,
    });
    const wrapper = await mountResolved(RegisterPage);

    const form = wrapper.get('[data-testid="register-form"]');
    expect(form.classes()).not.toContain('invisible');
    expect(form.attributes('inert')).toBeUndefined();
    expect(wrapper.find('[data-testid="register-closed"]').exists()).toBe(
      false,
    );
    expect(wrapper.find('[data-testid="register-divider"]').exists()).toBe(
      true,
    );
  });

  it('keeps the form when an older server omits the field', async () => {
    registerEndpoint('/api/v1/capabilities', () => ({
      data: { providerLogin: false, agentAccess: false, providers: [] },
    }));
    const wrapper = await mountResolved(RegisterPage);

    expect(wrapper.find('[data-testid="register-form"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="register-closed"]').exists()).toBe(
      false,
    );
  });

  it('holds the form hidden and inert while the read is pending', async () => {
    registerEndpoint('/api/v1/capabilities', () => new Promise(() => {}));
    const wrapper = await mountSuspended(RegisterPage);

    const form = wrapper.get('[data-testid="register-form"]');
    expect(form.classes()).toContain('invisible');
    expect(form.attributes('inert')).toBeDefined();
    expect(form.attributes('aria-hidden')).toBe('true');
    expect(
      wrapper.get('[data-testid="register-agreement"]').classes(),
    ).toContain('invisible');
    expect(wrapper.find('[data-testid="register-closed"]').exists()).toBe(
      false,
    );
  });
});
