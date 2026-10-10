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
      'Cập nhật lần cuối ngày 11/10/2026',
    );
    expect(wrapper.text()).toContain('mã băm Argon2id');
    expect(wrapper.text()).toContain(
      'Tài khoản, CV, ảnh, bản sao lưu và nhật ký của bạn được lưu trên '
      + 'hạ tầng GreenNode tại Thành phố Hồ Chí Minh, Việt Nam.',
    );
    expect(wrapper.text()).toContain(
      'Bizfly Email Transaction gửi email tài khoản qua SMTP.',
    );
    expect(wrapper.text()).toContain(
      'dấu thời gian UTC, địa chỉ IP nguồn, phương thức HTTP, mã trạng '
      + 'thái phản hồi và nhóm đường dẫn chung',
    );
    expect(wrapper.text()).toContain(
      'không ghi đường dẫn hoặc URI cụ thể, chuỗi truy vấn, tiêu đề yêu '
      + 'cầu hay phản hồi, hoặc nội dung yêu cầu hay phản hồi',
    );
    expect(wrapper.text()).toContain(
      'Dữ liệu đầu vào chỉ nằm trong RAM. Cảnh báo và lệnh chặn của '
      + 'CrowdSec được lưu trên ổ dữ liệu được mã hóa tại Việt Nam.',
    );
    expect(wrapper.text()).toContain(
      'Dữ liệu đầu vào và toàn bộ bản ghi tấn công của CrowdSec, gồm cảnh '
      + 'báo và lệnh chặn HTTP và SSH, được giữ tối đa 24 giờ.',
    );
    expect(wrapper.text()).toContain(
      'Không dữ liệu nào trong số này được chia sẻ với cộng đồng CrowdSec.',
    );
    expect(wrapper.text()).toContain(
      'Nhật ký vận hành thông thường của máy chủ không ghi địa chỉ IP '
      + 'hoặc tiêu đề yêu cầu và được lưu tối đa 30 ngày.',
    );
    expect(wrapper.text()).not.toContain('Amazon Web Services ở Singapore');
    expect(wrapper.text()).not.toContain('Amazon SES');
    expect(wrapper.text()).not.toContain('Amazon CloudFront');
    expect(wrapper.text()).not.toContain('AWS WAF');
    expect(wrapper.text()).not.toContain('Singapore');
    expect(wrapper.text()).not.toContain('danh sách chặn gửi');
    expect(wrapper.text()).toContain('Dữ liệu cá nhân chúng tôi thu thập');
    expect(wrapper.text()).toContain('Mục đích và cơ sở xử lý');
    expect(wrapper.text()).toContain('Quyền của bạn');
    expect(wrapper.text()).toContain(
      'Bản ghi về việc xóa tài khoản và gỡ liên kết nhà cung cấp',
    );
    expect(wrapper.text()).toContain(
      'Việc duy trì phiên đăng nhập không kéo dài thời hạn này.',
    );
    expect(wrapper.text()).toContain(
      'chúng tôi giữ chỗ đường dẫn cũ trong 180 ngày',
    );
    expect(wrapper.text()).toContain(
      'Nội dung: các CV bạn viết, ảnh bạn tải lên, và, khi một CV đang công '
      + 'khai, ảnh xem trước chúng tôi tạo từ CV đó để hiển thị khi đường '
      + 'dẫn được chia sẻ.',
    );
    expect(wrapper.text()).toContain(
      'dịch vụ đó sẽ lấy tiêu đề trang, phần tóm tắt và ảnh xem trước của '
      + 'trang (họ tên, tiêu đề và ảnh của bạn), và có thể giữ bản sao riêng '
      + 'của họ sau khi bạn hủy xuất bản.',
    );
    expect(wrapper.text()).toContain('Amazon Route 53 cung cấp dịch vụ DNS.');
    expect(wrapper.text()).toContain(
      'Google (Hoa Kỳ) và LinkedIn (Hoa Kỳ) cung cấp lựa chọn đăng nhập.',
    );
    expect(wrapper.text()).toContain(
      'Nhập từ LinkedIn: trình duyệt của bạn đọc tệp PDF hồ sơ LinkedIn bạn '
      + 'chọn. Chúng tôi không nhận tệp này; chúng tôi chỉ lưu CV bạn tạo từ '
      + 'nó.',
    );
    expect(wrapper.text()).not.toContain('Cloudflare');
    expect(wrapper.text()).toContain(
      'Lượt xem CV công khai: chúng tôi đếm lượt xem và chỉ lưu tổng số '
      + 'theo ngày.',
    );
    expect(wrapper.text()).toContain(
      'Không dùng công cụ phân tích hay mã theo dõi quảng cáo của bên thứ '
      + 'ba. Chúng tôi chỉ đếm lượt xem CV công khai như mô tả ở trên.',
    );
    expect(wrapper.text()).toContain(
      'nên chỉ nhận một mã định danh tài khoản do nhà cung cấp cấp, và xóa '
      + 'mã đó ngay trong lần đăng nhập.',
    );
    expect(wrapper.text()).not.toContain('xóa ngay tên và email');
    expect(wrapper.text()).toContain(
      'Người xem CV công khai không cần tài khoản.',
    );
    expect(wrapper.text()).toContain(
      'mức thu phóng và độ rộng khung chỉnh sửa bạn chọn',
    );
    expect(wrapper.text()).toContain('sessionStorage');
    expect(wrapper.text()).toContain('Mục Cài đặt → Thiết bị đã đăng nhập');
    expect(wrapper.text()).not.toContain('Settings');
    expect(wrapper.text()).toContain('trong vòng 20 ngày');
    expect(wrapper.text()).toContain(
      'Nếu xảy ra sự cố làm lộ hoặc mất dữ liệu cá nhân của bạn',
    );
    const operator = wrapper.get('[data-testid="legal-operator"]');
    expect(operator.text()).toBe(
      'aboutme.vn do Danny, một cá nhân, vận hành phi thương mại tại Việt '
      + 'Nam. Liên hệ: danny@aboutme.vn.',
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
      'Last updated October 11, 2026',
    );
    expect(wrapper.text()).toContain(
      'Your account, resumes, photos, backups, and logs are stored on '
      + 'GreenNode infrastructure in Ho Chi Minh City, Vietnam.',
    );
    expect(wrapper.text()).toContain(
      'Bizfly Email Transaction sends account emails over SMTP.',
    );
    expect(wrapper.text()).toContain(
      'the UTC timestamp, source IP address, HTTP method, response status, '
      + 'and broad route class',
    );
    expect(wrapper.text()).toContain(
      'does not record the specific path or URI, query string, request or '
      + 'response headers, or request or response body',
    );
    expect(wrapper.text()).toContain(
      'The feed stays only in RAM. CrowdSec attack alerts and bans are '
      + 'stored on the encrypted Vietnam data volume.',
    );
    expect(wrapper.text()).toContain(
      'We keep the feed and all CrowdSec attack records, including HTTP '
      + 'and SSH alerts and bans, for at most 24 hours.',
    );
    expect(wrapper.text()).toContain(
      'None of this data is shared with the CrowdSec community.',
    );
    expect(wrapper.text()).toContain(
      'Ordinary server operational logs do not record request IP addresses '
      + 'or request headers and are kept for up to 30 days.',
    );
    expect(wrapper.text()).not.toContain(
      'Amazon Web Services in Singapore',
    );
    expect(wrapper.text()).not.toContain('Amazon SES');
    expect(wrapper.text()).not.toContain('Amazon CloudFront');
    expect(wrapper.text()).not.toContain('AWS WAF');
    expect(wrapper.text()).not.toContain('Singapore');
    expect(wrapper.text()).not.toContain('suppression list');
    expect(wrapper.text()).toContain(
      'We delete the IP address and browser (user agent) recorded for a '
      + 'sign-in no later than 90 days after that sign-in.',
    );
    expect(wrapper.text()).toContain('It cannot publish or unpublish');
    expect(wrapper.text()).toContain(
      'Public pages are delivered directly from our host in Vietnam.',
    );
    expect(wrapper.text()).toContain(
      'Cookies are used only to keep you signed in, to complete Google or '
      + 'LinkedIn sign-in, to hold a pending second-factor sign-in for '
      + 'five minutes, to remember your theme and language, and, when a '
      + 'resume owner requires sign-in to view, to let you view that '
      + 'resume for 7 days.',
    );
    expect(wrapper.text()).not.toContain('CSRF');
    expect(wrapper.text()).toContain('Have I Been Pwned');
    expect(wrapper.text()).toContain(
      'may involve processing outside Vietnam',
    );
    expect(wrapper.text()).toContain(
      'we keep its old web address reserved for 180 days',
    );
    expect(wrapper.text()).toContain(
      'Content: the resumes you write, the photos you upload, and, while a '
      + 'resume is public, the preview image we make from it for link '
      + 'previews.',
    );
    expect(wrapper.text()).toContain(
      'When anyone shares your public link in a chat app or social network, '
      + 'that service fetches the page\'s title, summary, and preview image '
      + '(your name, headline, and photo), and may keep its own copy after '
      + 'you unpublish.',
    );
    expect(wrapper.text()).toContain('Amazon Route 53 provides DNS.');
    expect(wrapper.text()).toContain(
      'Google (United States) and LinkedIn (United States) provide optional '
      + 'sign-in.',
    );
    expect(wrapper.text()).toContain(
      'LinkedIn import: your browser reads the LinkedIn profile PDF you '
      + 'pick. We never receive the file; we store only the resume you '
      + 'create from it.',
    );
    expect(wrapper.text()).not.toContain('Cloudflare');
    expect(wrapper.text()).toContain(
      'Views of public resumes: we count views and keep only daily totals.',
    );
    expect(wrapper.text()).toContain(
      'No third-party analytics or advertising trackers. We count views '
      + 'of public resumes only as described above.',
    );
    expect(wrapper.text()).toContain(
      'so we receive only an account ID from the provider, and we discard '
      + 'it during that sign-in.',
    );
    expect(wrapper.text()).not.toContain('discard your name and email');
    expect(wrapper.text()).toContain(
      'Viewers of public resumes need no account.',
    );
    expect(wrapper.text()).toContain(
      'preview mode (PDF or web), zoom, and editor panel width',
    );
    expect(wrapper.text()).toContain('(sessionStorage)');
    expect(wrapper.text()).toContain(
      'Settings → Signed-in devices lists your signed-in devices.',
    );
    expect(wrapper.text()).toContain('delete data within 20 days');
    expect(wrapper.text()).toContain(
      'If an incident exposes or loses your personal data',
    );
    expect(wrapper.text()).toContain('Your rights');
    expect(wrapper.text()).toContain('Why we use your data');
    expect(wrapper.get('[data-testid="legal-operator"]').text()).toBe(
      'aboutme.vn is operated by Danny, an individual, on a non-commercial '
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
    expect(vietnamese.text()).toContain(
      'việc tiếp tục sử dụng không được coi là đồng ý',
    );

    setSiteLocale('en');
    const en = await mountSuspended(TermsPage);
    expect(title(en)).toBe('Terms of Service');
    expect(en.text()).toContain('You must be at least 16 years old.');
    expect(en.text()).toContain(
      'If we learn an account belongs to someone under 16, we will delete it.',
    );
    expect(en.text()).toContain('except for backup copies until they expire');
    expect(en.text()).toContain('governed by the laws of Vietnam');
    expect(en.text()).toContain('continued use does not count as consent');
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

  it(
    'links Terms, Privacy, Verify, and the MCP guide from the homepage '
    + 'footer',
    async () => {
      const wrapper = await mountSuspended(LandingPage);
      expect(
        wrapper.get('[data-testid="landing-terms-link"]').attributes('href'),
      ).toBe('/terms');
      expect(
        wrapper.get('[data-testid="landing-privacy-link"]')
          .attributes('href'),
      ).toBe('/privacy');
      expect(wrapper.get('[data-testid="landing-privacy-link"]').text())
        .toBe('Chính sách quyền riêng tư');
      expect(
        wrapper.get('[data-testid="landing-verify-link"]').attributes('href'),
      ).toBe('/verify');
      expect(wrapper.get('[data-testid="landing-verify-link"]').text())
        .toBe('Kiểm chứng');
      expect(
        wrapper.get('[data-testid="landing-guide-link"]').attributes('href'),
      ).toBe('/guide/mcp');
      expect(wrapper.get('[data-testid="landing-guide-link"]').text())
        .toBe('Kết nối AI');
      const footer = wrapper.get('[data-testid="landing-footer"]');
      const order = footer.findAll('a')
        .map((a) => a.attributes('data-testid'));
      expect(order).toEqual([
        'landing-terms-link',
        'landing-privacy-link',
        'landing-verify-link',
        'landing-guide-link',
      ]);

      setSiteLocale('en');
      const english = await mountSuspended(LandingPage);
      expect(english.get('[data-testid="landing-verify-link"]').text())
        .toBe('Verify');
      expect(english.get('[data-testid="landing-guide-link"]').text())
        .toBe('Connect AI');
    },
  );
});
