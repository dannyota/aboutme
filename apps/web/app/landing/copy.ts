// Homepage copy. The homepage defaults to Vietnamese because the initial
// community is Vietnamese (docs/design/product.md); English is one toggle away.

export const landingLocales = ['vi', 'en'] as const;

export type LandingLocale = (typeof landingLocales)[number];

export const defaultLandingLocale: LandingLocale = 'vi';

export function isLandingLocale(value: unknown): value is LandingLocale {
  return landingLocales.includes(value as LandingLocale);
}

type Point = { readonly title: string; readonly text: string };

export type LandingCopy = {
  readonly description: string;
  /** The headline, rendered as two lines. */
  readonly title: readonly [string, string];
  readonly lead: string;
  readonly createAccount: string;
  readonly signIn: string;
  readonly openResumes: string;
  readonly sampleLabel: string;
  readonly points: readonly [Point, Point, Point];
  readonly publishTitle: string;
  readonly publishChoices: readonly [Point, Point, Point];
  readonly licensePrefix: string;
  readonly localeLabel: string;
  readonly lightMode: string;
  readonly darkMode: string;
  readonly switchToLight: string;
  readonly switchToDark: string;
};

export const localeNames: Record<LandingLocale, string> = {
  vi: 'Tiếng Việt',
  en: 'English',
};

export const landingCopy: Record<LandingLocale, LandingCopy> = {
  vi: {
    description:
      'Công cụ tạo CV mã nguồn mở. Viết một lần, xem trước đúng bố cục '
      + 'trang, và đăng từng CV tại một đường dẫn gọn gàng do bạn kiểm soát.',
    title: ['CV của bạn. Miễn phí.', 'Không ai thấy nếu bạn không muốn.'],
    lead:
      'aboutme là công cụ tạo CV mã nguồn mở. Viết tối đa ba CV, xem trước '
      + 'đúng từng trang, và đăng mỗi CV tại một đường dẫn riêng. Tìm kiếm '
      + 'và khám phá bằng AI luôn tắt cho đến khi bạn bật.',
    createAccount: 'Tạo tài khoản',
    signIn: 'Đăng nhập',
    openResumes: 'Mở CV của bạn',
    sampleLabel: 'CV mẫu đăng tại aboutme.vn/ada-lovelace',
    points: [
      {
        title: 'Của bạn, do bạn giữ.',
        text: 'Tối đa ba CV mỗi tài khoản, riêng tư cho đến khi bạn đăng.',
      },
      {
        title: 'Mỗi CV một đường dẫn.',
        text:
          'Đăng, gỡ đăng và cho phép lập chỉ mục tìm kiếm riêng cho từng CV.',
      },
      {
        title: 'Dùng trợ lý AI của bạn.',
        text:
          'Kết nối trợ lý hỗ trợ MCP với các quyền do bạn cấp và có thể thu '
          + 'hồi.',
      },
    ],
    publishTitle: 'Đăng CV gồm ba lựa chọn',
    publishChoices: [
      { title: 'CV công khai', text: 'Có trang công khai hay không.' },
      {
        title: 'Tải PDF',
        text:
          'Người xem có được tải PDF hay không. Bạn luôn có thể tự xuất bản '
          + 'của mình.',
      },
      {
        title: 'SEO và GEO',
        text:
          'Công cụ tìm kiếm và công cụ trả lời AI có được lập chỉ mục hay '
          + 'không. Mặc định tắt.',
      },
    ],
    licensePrefix: 'Mã nguồn mở theo giấy phép',
    localeLabel: 'Ngôn ngữ',
    lightMode: 'Chế độ sáng',
    darkMode: 'Chế độ tối',
    switchToLight: 'Chuyển sang chế độ sáng',
    switchToDark: 'Chuyển sang chế độ tối',
  },
  en: {
    description:
      'Open-source resume builder. Write once, preview the exact page '
      + 'layout, and publish each resume at a clean URL you control.',
    title: ['Your resume. Free.', 'No one sees it unless you want them to.'],
    lead:
      'aboutme is an open-source resume builder. Write up to three resumes, '
      + 'preview the exact page, and publish each one at its own link. Search '
      + 'and AI discovery stay off until you turn them on.',
    createAccount: 'Create account',
    signIn: 'Sign in',
    openResumes: 'Open your resumes',
    sampleLabel: 'Sample resume published at aboutme.vn/ada-lovelace',
    points: [
      {
        title: 'Yours to keep.',
        text: 'Up to three resumes per account, private until you publish.',
      },
      {
        title: 'One link per resume.',
        text:
          'Publish, unpublish, and control search indexing for each resume '
          + 'on its own.',
      },
      {
        title: 'Bring your own agent.',
        text:
          'Connect an MCP-capable assistant with scopes you grant and can '
          + 'revoke.',
      },
    ],
    publishTitle: 'Publishing is three choices',
    publishChoices: [
      { title: 'Public resume', text: 'Whether any public page exists.' },
      {
        title: 'PDF download',
        text:
          'Whether visitors can download the PDF. You can always export your '
          + 'own.',
      },
      {
        title: 'SEO and GEO',
        text:
          'Whether search engines and AI answer engines may index it. Off by '
          + 'default.',
      },
    ],
    licensePrefix: 'Open source under',
    localeLabel: 'Language',
    lightMode: 'Light mode',
    darkMode: 'Dark mode',
    switchToLight: 'Switch to light theme',
    switchToDark: 'Switch to dark theme',
  },
};
