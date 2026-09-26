// Homepage copy in both site languages (see app/i18n/locale.ts).
import type { Locale } from '@/i18n/locale';

type Point = { readonly title: string; readonly text: string };

export type LandingCopy = {
  readonly description: string;
  /** The headline, rendered as two lines. */
  readonly title: readonly [string, string];
  /** A trailing suffix of title[1], set apart with the brand gradient. */
  readonly titleEmphasis: string;
  readonly lead: string;
  readonly createResume: string;
  readonly signIn: string;
  readonly openResumes: string;
  readonly browseTemplates: string;
  readonly sampleLabel: string;
  readonly sealLabel: string;
  readonly heroChips: { readonly private: string; readonly pdf: string };
  readonly principlesTitle: string;
  readonly points: readonly [Point, Point, Point];
  readonly templatesTitle: string;
  readonly templatesLead: string;
  readonly templateCategoriesLabel: string;
  readonly browseAllTemplates: string;
  readonly publishTitle: string;
  readonly publishLead: string;
  readonly publishExample: string;
  readonly publishChoices: readonly [Point, Point, Point];
  readonly stateOn: string;
  readonly stateOff: string;
  readonly openSourceTitle: string;
  readonly openSourceText: string;
  readonly licensePrefix: string;
  readonly viewSource: string;
};

export const landingCopy: Record<Locale, LandingCopy> = {
  vi: {
    description:
      'Công cụ tạo CV mã nguồn mở. Viết một lần, xem trước đúng bố cục '
      + 'trang, và đăng từng CV tại một đường dẫn gọn gàng do bạn kiểm soát.',
    title: ['CV của bạn.', 'Chia sẻ theo cách của bạn.'],
    titleEmphasis: 'theo cách của bạn.',
    lead:
      'Miễn phí và mã nguồn mở. Viết CV, xem trước đúng từng trang và chỉ '
      + 'đăng tại đường dẫn riêng khi bạn sẵn sàng.',
    createResume: 'Tạo CV của bạn',
    signIn: 'Đăng nhập',
    openResumes: 'Mở CV của bạn',
    browseTemplates: 'Xem thư viện',
    sampleLabel: 'CV mẫu đăng tại aboutme.vn/danny',
    sealLabel: 'Công khai tại aboutme.vn/danny',
    heroChips: {
      private: 'Riêng tư theo mặc định',
      pdf: 'PDF',
    },
    principlesTitle: 'Vì sao dùng aboutme.vn',
    points: [
      {
        title: 'Riêng tư theo mặc định',
        text:
          'CV của bạn luôn riêng tư cho đến khi bạn đăng. Mỗi tài khoản có '
          + 'tối đa ba CV.',
      },
      {
        title: 'Mỗi CV một đường dẫn',
        text:
          'Mỗi CV có đường dẫn aboutme.vn gọn gàng của riêng nó. Đăng, gỡ '
          + 'đăng và cho phép lập chỉ mục tìm kiếm riêng cho từng CV.',
      },
      {
        title: 'Dùng trợ lý AI của bạn',
        text:
          'Kết nối trợ lý hỗ trợ MCP với các quyền do bạn cấp và có thể thu '
          + 'hồi.',
      },
    ],
    templatesTitle: 'Chọn phong cách cho CV',
    templatesLead:
      'Mỗi bản xem trước là mẫu thật: đúng trang nhà tuyển dụng mở và bản '
      + 'PDF họ tải về.',
    templateCategoriesLabel: 'Nhóm trong thư viện',
    browseAllTemplates: 'Mở thư viện',
    publishTitle: 'Đăng CV gồm ba lựa chọn',
    publishLead:
      'Không gì được công khai cho đến khi bạn đăng. Bạn chọn từng thiết '
      + 'lập cho từng CV và có thể đổi bất cứ lúc nào.',
    publishExample: 'Thiết lập ví dụ',
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
    stateOn: 'Bật',
    stateOff: 'Tắt',
    openSourceTitle: 'Miễn phí và mã nguồn mở',
    openSourceText:
      'Mã nguồn công khai trên GitHub. Bạn có thể đọc mã, báo lỗi hoặc tự '
      + 'chạy bản của riêng mình.',
    licensePrefix: 'Phát hành theo giấy phép',
    viewSource: 'Xem mã nguồn trên GitHub',
  },
  en: {
    description:
      'Open-source resume builder. Write once, preview the exact page '
      + 'layout, and publish each resume at a clean URL you control.',
    title: ['Your resume.', 'Your link. Your control.'],
    titleEmphasis: 'Your control.',
    lead:
      'Free and open source. Write your resume, see exactly how each page '
      + 'will look, and publish it at its own link only when you’re ready.',
    createResume: 'Create your resume',
    signIn: 'Sign in',
    openResumes: 'Open your resumes',
    browseTemplates: 'Browse the library',
    sampleLabel: 'Sample resume published at aboutme.vn/danny',
    sealLabel: 'Public at aboutme.vn/danny',
    heroChips: {
      private: 'Private by default',
      pdf: 'PDF',
    },
    principlesTitle: 'Why aboutme.vn',
    points: [
      {
        title: 'Private by default',
        text:
          'Your resume stays private until you publish it. Keep up to '
          + 'three resumes per account.',
      },
      {
        title: 'One link per resume',
        text:
          'Each resume gets its own clean aboutme.vn link. Publish, '
          + 'unpublish, and control search indexing for each one.',
      },
      {
        title: 'Bring your own AI',
        text:
          'Connect an MCP-capable assistant with scopes you grant and can '
          + 'revoke.',
      },
    ],
    templatesTitle: 'Choose your style',
    templatesLead:
      'Each preview is the real template: the same page a recruiter opens '
      + 'and the PDF they download.',
    templateCategoriesLabel: 'Library categories',
    browseAllTemplates: 'Open the library',
    publishTitle: 'Publishing is three choices',
    publishLead:
      'Nothing is public until you publish it. You choose each setting for '
      + 'each resume, and you can change it any time.',
    publishExample: 'Example settings',
    publishChoices: [
      { title: 'Public resume', text: 'Whether any public page exists.' },
      {
        title: 'PDF download',
        text:
          'Whether visitors can download the PDF. You can always export '
          + 'your own.',
      },
      {
        title: 'SEO and GEO',
        text:
          'Whether search engines and AI answer engines may index it. Off '
          + 'by default.',
      },
    ],
    stateOn: 'On',
    stateOff: 'Off',
    openSourceTitle: 'Free and open source',
    openSourceText:
      'The code is public on GitHub. Read it, report issues, or run your '
      + 'own copy.',
    licensePrefix: 'Licensed under',
    viewSource: 'View the code on GitHub',
  },
};
