import type { Locale } from '@/i18n/locale';

import type { ShowcaseFilter } from './showcase';

type Choice = { readonly title: string; readonly text: string };
type Field = { readonly label: string; readonly value: string };

export type LandingCopy = {
  readonly description: string;
  readonly sampleLabel: string;
  readonly sealLabel: string;
  readonly hero: {
    readonly title: string;
    readonly lead: string;
    readonly coin: string;
  };
  readonly createResume: string;
  readonly openResumes: string;
  readonly browseTemplates: string;
  readonly signIn: string;
  readonly editor: {
    readonly title: string;
    readonly lead: string;
    readonly facts: readonly [string, string, string, string];
    readonly illustrationLabel: string;
    readonly tabs: readonly [string, string, string, string];
    readonly fields: readonly [Field, Field, Field];
    readonly saved: string;
    readonly note: string;
  };
  readonly templates: {
    readonly title: string;
    readonly lead: string;
    readonly labels: Readonly<Record<ShowcaseFilter, string>>;
    readonly openLibrary: string;
  };
  readonly publish: {
    readonly title: string;
    readonly lead: string;
    readonly example: string;
    readonly choices: readonly [Choice, Choice, Choice];
    readonly stateOn: string;
    readonly stateOff: string;
  };
  readonly ai: {
    readonly title: string;
    readonly text: string;
    readonly tags: readonly [string, string];
    readonly ownerTag: string;
    readonly link: string;
  };
  readonly data: {
    readonly title: string;
    readonly text: string;
    readonly tag: string;
    readonly offTag: string;
    readonly link: string;
  };
  readonly maker: {
    readonly title: string;
    readonly text: string;
    readonly avatarAlt: string;
    readonly seeResume: string;
    readonly viewCode: string;
  };
  readonly footer: {
    readonly navLabel: string;
    readonly license: string;
  };
};

export const landingCopy: Record<Locale, LandingCopy> = {
  vi: {
    description:
      'Công cụ tạo CV mã nguồn mở. Viết một lần, xem trước đúng bố cục '
      + 'trang, và xuất bản từng CV tại một đường dẫn gọn gàng do bạn kiểm '
      + 'soát.',
    sampleLabel: 'CV mẫu xuất bản tại aboutme.vn/danny',
    sealLabel: 'Công khai tại aboutme.vn/danny',
    hero: {
      title: 'Làm CV ở đây, gửi đi bằng một đường link.',
      lead:
        'Chào bạn, mình là aboutme.vn, chỗ làm CV miễn phí và mã nguồn mở. '
        + 'Bạn viết, chọn mẫu, rồi xuất bản tại đường dẫn riêng khi nào bạn '
        + 'sẵn sàng.',
      coin: '0đ',
    },
    createResume: 'Tạo CV của bạn',
    openResumes: 'Mở CV của bạn',
    browseTemplates: 'Xem thư viện',
    signIn: 'Đăng nhập',
    editor: {
      title: 'Viết đến đâu, thấy đúng trang đến đó.',
      lead:
        'Bạn điền nội dung, bản xem trước cho thấy đúng từng trang: giống '
        + 'trang nhà tuyển dụng sẽ mở và bản PDF họ tải về.',
      facts: [
        'Miễn phí và mã nguồn mở.',
        'Tối đa ba CV cho mỗi tài khoản.',
        'Tự xuất PDF lúc nào cũng được.',
        'Giao diện tiếng Việt và tiếng Anh.',
      ],
      illustrationLabel:
        'Minh họa trình chỉnh sửa: biểu mẫu và bản xem trước trang CV',
      tabs: ['Thông tin', 'Kinh nghiệm', 'Kỹ năng', 'Học vấn'],
      fields: [
        { label: 'Họ và tên', value: 'Đinh Công Khánh' },
        { label: 'Tiêu đề', value: 'Senior Mobile Engineer' },
        {
          label: 'Giới thiệu',
          value:
            'Senior Mobile Engineer, 7 năm làm app Android và iOS cho tới 2 '
            + 'triệu người dùng.',
        },
      ],
      saved: 'Đã lưu',
      note:
        'Hình minh họa trình chỉnh sửa. Trang xem trước là mẫu Creative '
        + 'Accent với một CV mẫu.',
    },
    templates: {
      title: 'Chọn mẫu hợp với nghề của bạn.',
      lead:
        'Thư viện có 20 mẫu miễn phí. Mỗi bản xem trước là mẫu thật: đúng '
        + 'trang nhà tuyển dụng mở và bản PDF họ tải về.',
      labels: {
        'ats': 'Chuẩn ATS',
        'technical': 'Kỹ thuật',
        'first-job': 'Mới tốt nghiệp',
        'management': 'Quản lý',
      },
      openLibrary: 'Mở thư viện',
    },
    publish: {
      title: 'Không gì công khai cho đến khi bạn xuất bản.',
      lead:
        'Mỗi CV có ba lựa chọn riêng. Bạn đổi lúc nào cũng được, kể cả hủy '
        + 'xuất bản.',
      example: 'Thiết lập ví dụ',
      choices: [
        { title: 'CV công khai', text: 'Có trang công khai hay không.' },
        {
          title: 'Tải PDF',
          text:
            'Người xem có được tải PDF hay không. Bạn luôn có thể tự xuất '
            + 'bản của mình.',
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
    },
    ai: {
      title: 'Dùng trợ lý AI của chính bạn.',
      text:
        'Kết nối Claude hoặc trợ lý AI khác hỗ trợ MCP để nó đọc và sửa CV '
        + 'giúp bạn. Bạn duyệt từng kết nối, thu hồi lúc nào cũng được, và '
        + 'việc xuất bản vẫn là của bạn.',
      tags: ['Đọc CV', 'Chỉnh sửa CV'],
      ownerTag: 'Xuất bản: chỉ bạn',
      link: 'Xem cách kết nối',
    },
    data: {
      title: 'Dữ liệu của bạn lưu tại Việt Nam.',
      text:
        'Máy chủ đặt tại Việt Nam. CV riêng tư theo mặc định, và công cụ '
        + 'tìm kiếm chỉ thấy CV nào bạn cho phép.',
      tag: 'Riêng tư theo mặc định',
      offTag: 'Lập chỉ mục: mặc định tắt',
      link: 'Đọc chính sách quyền riêng tư',
    },
    maker: {
      title: 'Chào bạn, mình là Danny.',
      text:
        'Mình làm aboutme.vn cho người tìm việc ở Việt Nam, và CV của chính '
        + 'mình cũng nằm ở đây. Trang còn mới nên mình chưa có con số nào để '
        + 'khoe. Thứ mình khoe được là mã nguồn: nó công khai trên GitHub '
        + 'theo giấy phép AGPL-3.0, bạn cứ đọc, báo lỗi, hoặc tự chạy bản '
        + 'của riêng mình.',
      avatarAlt:
        'Tranh vẽ chân dung Danny, bên cạnh là vợ và con anh trong khung '
        + 'hình trái tim',
      seeResume: 'Xem CV của mình',
      viewCode: 'Xem mã nguồn trên GitHub',
    },
    footer: {
      navLabel: 'aboutme.vn',
      license:
        'aboutme.vn là phần mềm mã nguồn mở, phát hành theo giấy phép '
        + 'AGPL-3.0.',
    },
  },
  en: {
    description:
      'Open-source resume builder. Write once, preview the exact page '
      + 'layout, and publish each resume at a clean URL you control.',
    sampleLabel: 'Sample resume published at aboutme.vn/danny',
    sealLabel: 'Public at aboutme.vn/danny',
    hero: {
      title: 'Write your resume here. Send it as one link.',
      lead:
        'Hi, I\'m aboutme.vn, a free, open-source resume builder. Write it, '
        + 'pick a template, and publish it at its own link when you\'re '
        + 'ready.',
      coin: '0đ',
    },
    createResume: 'Create your resume',
    openResumes: 'Open your resumes',
    browseTemplates: 'Browse the library',
    signIn: 'Sign in',
    editor: {
      title: 'See the real page while you write.',
      lead:
        'Fill in your details and the preview shows every page as it will '
        + 'really look: the page a recruiter opens and the PDF they download.',
      facts: [
        'Free and open source.',
        'Up to three resumes per account.',
        'Export your own PDF any time.',
        'Vietnamese and English interface.',
      ],
      illustrationLabel:
        'Illustration of the editor: a form and the resume page preview',
      tabs: ['Details', 'Experience', 'Skills', 'Education'],
      fields: [
        { label: 'Full name', value: 'Đinh Công Khánh' },
        { label: 'Headline', value: 'Senior Mobile Engineer' },
        {
          label: 'Summary',
          value:
            'Senior Mobile Engineer with 7 years on Android and iOS apps for '
            + 'up to 2 million users.',
        },
      ],
      saved: 'Saved',
      note:
        'Illustration of the editor. The preview is the Creative Accent '
        + 'template with a sample resume.',
    },
    templates: {
      title: 'Pick a template that fits your work.',
      lead:
        'The library has 20 free templates. Each preview is the real thing: '
        + 'the page a recruiter opens and the PDF they download.',
      labels: {
        'ats': 'ATS-friendly',
        'technical': 'Technical',
        'first-job': 'First job',
        'management': 'Management',
      },
      openLibrary: 'Open the library',
    },
    publish: {
      title: 'Nothing is public until you publish it.',
      lead:
        'Each resume has its own three choices. Change them any time, or '
        + 'unpublish.',
      example: 'Example settings',
      choices: [
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
    },
    ai: {
      title: 'Bring your own AI assistant.',
      text:
        'Connect Claude or another MCP-capable assistant to read and edit '
        + 'your resumes. You approve each connection, you can revoke it any '
        + 'time, and publishing stays with you.',
      tags: ['Read resumes', 'Write resumes'],
      ownerTag: 'Publish: only you',
      link: 'See how to connect',
    },
    data: {
      title: 'Your data is hosted in Vietnam.',
      text:
        'The servers are in Vietnam. Your resume is private by default, and '
        + 'search engines only see the ones you allow.',
      tag: 'Private by default',
      offTag: 'Indexing: off by default',
      link: 'Read the privacy policy',
    },
    maker: {
      title: 'Hi, I\'m Danny.',
      text:
        'I built aboutme.vn for job seekers in Vietnam, and my own resume '
        + 'lives here too. The site is new, so I have no big numbers to show '
        + 'you. What I can show is the code: it is public on GitHub under the '
        + 'AGPL-3.0 license, so read it, report a bug, or run your own copy.',
      avatarAlt:
        'Drawn portrait of Danny, with his wife and child in a heart-shaped '
        + 'frame',
      seeResume: 'See my resume',
      viewCode: 'View the code on GitHub',
    },
    footer: {
      navLabel: 'aboutme.vn',
      license:
        'aboutme.vn is open-source software, released under the AGPL-3.0 '
        + 'license.',
    },
  },
};
