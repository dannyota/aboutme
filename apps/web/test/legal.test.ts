import { readFileSync } from 'node:fs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import LandingPage from '../app/pages/index.vue';
import LoginPage from '../app/pages/login.vue';
import PrivacyPage from '../app/pages/privacy.vue';
import RegisterPage from '../app/pages/register.vue';
import TermsPage from '../app/pages/terms.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

registerCapabilities({ providerLogin: false, agentAccess: false });
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

function title(wrapper: Wrapper): string {
  return wrapper.get('[data-page-title]').text();
}

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
});

describe('privacy and terms pages', () => {
  it('render the Privacy Policy in Vietnamese by default', async () => {
    const wrapper = await mountSuspended(PrivacyPage);
    expect(title(wrapper)).toBe('Chính sách quyền riêng tư');
    expect(wrapper.get('[data-testid="legal-updated"]').text()).toBe(
      'Cập nhật lần cuối ngày 26/09/2026',
    );
    expect(wrapper.text()).toContain('mã băm Argon2id');
    expect(wrapper.text()).toContain('Singapore (ap-southeast-1)');
    expect(wrapper.text()).toContain('Dữ liệu cá nhân chúng tôi thu thập');
    expect(wrapper.text()).toContain('Mục đích và cơ sở xử lý');
    expect(wrapper.text()).toContain('Quyền của bạn');
    expect(wrapper.text()).toContain(
      'Bản ghi về việc xoá tài khoản và gỡ liên kết nhà cung cấp',
    );
    expect(wrapper.text()).toContain(
      'Việc duy trì phiên đăng nhập không kéo dài thời hạn này.',
    );
    expect(wrapper.text()).toContain(
      'chúng tôi giữ chỗ đường dẫn cũ trong 180 ngày',
    );
    const operator = wrapper.get('[data-testid="legal-operator"]');
    expect(operator.text()).toBe(
      'aboutme do Danny, một cá nhân, vận hành phi thương mại tại Việt Nam. '
      + 'Liên hệ: danny@aboutme.vn.',
    );
    expect(operator.get('a').attributes('href')).toBe(
      'mailto:danny@aboutme.vn',
    );
    expect(wrapper.get('[data-testid="legal-contact"]').attributes('href'))
      .toBe('mailto:danny@aboutme.vn');
  });

  it('render the Privacy Policy in English', async () => {
    setSiteLocale('en');
    const wrapper = await mountSuspended(PrivacyPage);
    expect(title(wrapper)).toBe('Privacy Policy');
    expect(wrapper.get('[data-testid="legal-updated"]').text()).toBe(
      'Last updated September 26, 2026',
    );
    expect(wrapper.text()).toContain(
      'We delete the IP address and browser (user agent) recorded for a '
      + 'sign-in no later than 90 days after that sign-in.',
    );
    expect(wrapper.text()).toContain('It cannot publish or unpublish');
    expect(wrapper.text()).toContain('which does not store copies of them');
    expect(wrapper.text()).toContain(
      'Cookies are used only to keep you signed in, to complete Google '
      + 'sign-in, to hold a pending second-factor sign-in for five '
      + 'minutes, and to remember your theme and language.',
    );
    expect(wrapper.text()).not.toContain('CSRF');
    expect(wrapper.text()).toContain('Have I Been Pwned');
    expect(wrapper.text()).toContain(
      'transferred abroad: mainly to Singapore',
    );
    expect(wrapper.text()).toContain(
      'we keep its old web address reserved for 180 days',
    );
    expect(wrapper.text()).toContain('Your rights');
    expect(wrapper.text()).toContain('Why we use your data');
    expect(wrapper.get('[data-testid="legal-operator"]').text()).toBe(
      'aboutme is operated by Danny, an individual, on a non-commercial '
      + 'basis in Vietnam. Contact: danny@aboutme.vn.',
    );
  });

  it('render the Terms in Vietnamese and English', async () => {
    const vietnamese = await mountSuspended(TermsPage);
    expect(vietnamese.find('[data-testid="legal-operator"]').exists()).toBe(
      false,
    );
    expect(title(vietnamese)).toBe('Điều khoản sử dụng');
    expect(vietnamese.text()).toContain('Bạn phải từ 16 tuổi trở lên.');
    expect(vietnamese.text()).toContain('pháp luật Việt Nam');

    setSiteLocale('en');
    const en = await mountSuspended(TermsPage);
    expect(title(en)).toBe('Terms of Service');
    expect(en.text()).toContain('You must be at least 16 years old.');
    expect(en.text()).toContain(
      'If we learn an account belongs to someone under 16, we will delete it.',
    );
    expect(en.text()).toContain('except for backup copies until they expire');
    expect(en.text()).toContain('governed by the laws of Vietnam');
    expect(
      en.get('a[href="https://github.com/dannyota/aboutme"]').text(),
    ).toBe('Source code on GitHub');
  });

  it('set the html language on both routes', async () => {
    for (const route of ['/privacy', '/terms']) {
      setSiteLocale(undefined);
      const wrapper = await mountSuspended(AppRoot, { route });
      await flushPromises();
      await vi.waitFor(() =>
        expect(document.documentElement.lang).toBe('vi'));
      expect(wrapper.find('[data-testid="landing-locale"]').exists()).toBe(
        true,
      );
      wrapper.unmount();

      setSiteLocale('en');
      const english = await mountSuspended(AppRoot, { route });
      await flushPromises();
      await vi.waitFor(() =>
        expect(document.documentElement.lang).toBe('en'));
      english.unmount();
    }
  });

  it('load nothing from another origin and fetch no data', async () => {
    for (const page of [PrivacyPage, TermsPage]) {
      const wrapper = await mountSuspended(page);
      expect(wrapper.find('img, script, iframe, link, video').exists()).toBe(
        false,
      );
      const hrefs = wrapper.findAll('[href]').map((a) => a.attributes('href'));
      expect(
        hrefs.every((href) =>
          href === 'mailto:danny@aboutme.vn'
          || href === 'https://github.com/dannyota/aboutme'),
      ).toBe(true);
    }
    for (const file of [
      'app/pages/privacy.vue',
      'app/pages/terms.vue',
      'app/components/legal/LegalDocument.vue',
    ]) {
      expect(readFileSync(file, 'utf8')).not.toMatch(
        /useFetch|useAsyncData|\$fetch/u,
      );
    }
  });
});

describe('links to the legal pages', () => {
  it('shows the agreement line under the registration button', async () => {
    const wrapper = await mountSuspended(RegisterPage);
    const line = wrapper.get('[data-testid="register-agreement"]');
    expect(line.text()).toBe(
      'Khi tạo tài khoản, bạn xác nhận đủ 16 tuổi và đồng ý với Điều khoản '
      + 'sử dụng và Chính sách quyền riêng tư.',
    );
    expect(line.find('a[href="/terms"]').exists()).toBe(true);
    expect(line.find('a[href="/privacy"]').exists()).toBe(true);
    expect(line.find('input[type="checkbox"]').exists()).toBe(false);

    setSiteLocale('en');
    const english = await mountSuspended(RegisterPage);
    expect(english.get('[data-testid="register-agreement"]').text()).toBe(
      'By creating an account you confirm you are at least 16 and agree '
      + 'to the Terms of Service and the Privacy Policy.',
    );
  });

  it('shows the agreement under the sign-in provider buttons', async () => {
    registerCapabilities({
      providerLogin: true,
      agentAccess: false,
      providers: ['google'],
    });
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    const line = wrapper.get('[data-testid="login-agreement"]');
    expect(line.text()).toBe(
      'Khi tạo tài khoản, bạn xác nhận đủ 16 tuổi và đồng ý với Điều khoản '
      + 'sử dụng và Chính sách quyền riêng tư.',
    );
    expect(line.find('a[href="/terms"]').exists()).toBe(true);

    registerCapabilities({ providerLogin: false, agentAccess: false });
    clearNuxtData();
    const passwordOnly = await mountSuspended(LoginPage);
    await flushPromises();
    expect(passwordOnly.find('[data-testid="login-agreement"]').exists())
      .toBe(false);
  });

  it('links both pages from the homepage footer', async () => {
    const wrapper = await mountSuspended(LandingPage);
    expect(
      wrapper.get('[data-testid="landing-terms-link"]').attributes('href'),
    ).toBe('/terms');
    expect(
      wrapper.get('[data-testid="landing-privacy-link"]').attributes('href'),
    ).toBe('/privacy');
    expect(wrapper.get('[data-testid="landing-privacy-link"]').text()).toBe(
      'Chính sách quyền riêng tư',
    );
  });
});
