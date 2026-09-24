// The template gallery (/templates) and template pages (/templates/<id>) in
// both site languages. Purpose lines and sample tags per template live in
// app/templates/catalog.ts.
import type { GalleryFilter } from '../templates/catalog';
import type { Locale } from './locale';

export interface GalleryCopy {
  readonly title: string;
  readonly seoTitle: string;
  readonly lead: string;
  readonly filtersLabel: string;
  readonly all: (count: number) => string;
  readonly filters: Readonly<Record<GalleryFilter, string>>;
  readonly illustrative: string;
  readonly pageImageAlt: (name: string) => string;
  readonly noMatch: string;
  readonly detail: {
    readonly breadcrumb: string;
    readonly seoTitle: (name: string) => string;
    readonly sampleToggle: string;
    readonly sampleLanguages: Readonly<Record<Locale, string>>;
    readonly tabsLabel: string;
    readonly pageTab: string;
    readonly pdfTab: string;
    readonly pdfHint: string;
    readonly pdfPageAlt: (number: number, total: number, name: string)
      => string;
    readonly pdfPageCaption: (number: number, total: number) => string;
    readonly atsTab: string;
    readonly atsHint: string;
    readonly sample: string;
    readonly fictional: string;
    readonly pages: string;
    readonly pageCount: (count: number) => string;
    readonly layout: string;
    readonly oneColumn: string;
    readonly twoColumns: string;
    readonly paper: string;
    readonly photo: string;
    readonly suitsPhoto: string;
    readonly photoNotRecommended: string;
    readonly twoColumnNote: readonly [string, string];
    readonly fillerNote: string;
    readonly useSample: string;
    readonly useBlank: string;
    readonly privateCopy: string;
    readonly preview: string;
  };
}

export const galleryCopy: Readonly<Record<Locale, GalleryCopy>> = {
  vi: {
    title: 'Thư viện',
    seoTitle: 'Thư viện mẫu CV miễn phí',
    lead: '20 mẫu miễn phí. Trang bạn xem ở đây chính là trang nhà tuyển dụng '
      + 'mở và bản PDF họ tải về.',
    filtersLabel: 'Lọc thư viện',
    all: (count) => `Tất cả ${count}`,
    filters: {
      'sample': 'Có CV mẫu',
      'ats': 'Chuẩn ATS',
      'one-page': 'Một trang',
      'photo': 'Hợp với ảnh',
      'first-job': 'Mới tốt nghiệp',
      'technical': 'Kỹ thuật',
      'management': 'Quản lý',
    },
    illustrative: 'Nội dung minh họa',
    pageImageAlt: (name) => `Trang 1 của CV mẫu ${name}`,
    noMatch: 'Không có mẫu nào khớp với bộ lọc này.',
    detail: {
      breadcrumb: 'Thư viện',
      seoTitle: (name) => `Mẫu CV ${name}`,
      sampleToggle: 'Ngôn ngữ của CV mẫu',
      sampleLanguages: { vi: 'Tiếng Việt', en: 'Tiếng Anh' },
      tabsLabel: 'Cách xem CV mẫu',
      pageTab: 'Trang CV',
      pdfTab: 'PDF',
      pdfHint: 'Bản PDF tải về của CV mẫu, từng trang.',
      pdfPageAlt: (number, total, name) =>
        `Trang ${number}/${total} của CV mẫu ${name}`,
      pdfPageCaption: (number, total) => `Trang ${number}/${total}`,
      atsTab: 'ATS đọc được gì',
      atsHint: 'Nội dung CV theo đúng thứ tự phần mềm lọc hồ sơ (ATS) đọc.',
      sample: 'CV mẫu',
      fictional: '(nhân vật hư cấu)',
      pages: 'Số trang',
      pageCount: (count) => `${count} trang`,
      layout: 'Bố cục',
      oneColumn: 'Một cột',
      twoColumns: 'Hai cột',
      paper: 'Khổ giấy',
      photo: 'Ảnh',
      suitsPhoto: 'Hợp với ảnh (đặt trên, bên trái hoặc bên phải tên)',
      photoNotRecommended: 'Không nên dùng ảnh',
      twoColumnNote: [
        'Cổng tuyển dụng có thể đọc cột bên sau cột chính. Khi nộp qua cổng '
        + 'trực tuyến, hãy dùng ',
        '.',
      ],
      fillerNote: 'Mẫu này chưa có CV mẫu riêng. Nội dung bên dưới chỉ để '
        + 'minh họa.',
      useSample: 'Dùng CV mẫu này',
      useBlank: 'Dùng mẫu này với CV trống',
      privateCopy: 'CV mẫu tạo một bản sao riêng tư trong tài khoản của bạn. '
        + 'Bạn thay nội dung, rồi tự quyết định có công khai hay không.',
      preview: 'Bản xem trước CV',
    },
  },
  en: {
    title: 'Library',
    seoTitle: 'Free resume library',
    lead: '20 free templates. What you see here is exactly the page a '
      + 'recruiter opens and the PDF they download.',
    filtersLabel: 'Filter the library',
    all: (count) => `All ${count}`,
    filters: {
      'sample': 'With a sample',
      'ats': 'ATS-friendly',
      'one-page': 'One page',
      'photo': 'Suits a photo',
      'first-job': 'First job',
      'technical': 'Technical',
      'management': 'Management',
    },
    illustrative: 'Illustrative content',
    pageImageAlt: (name) => `Page 1 of the ${name} sample resume`,
    noMatch: 'No template matches this filter.',
    detail: {
      breadcrumb: 'Library',
      seoTitle: (name) => `${name} resume template`,
      sampleToggle: 'Sample language',
      sampleLanguages: { vi: 'Vietnamese sample', en: 'English sample' },
      tabsLabel: 'How to view the sample',
      pageTab: 'Page',
      pdfTab: 'PDF',
      pdfHint: 'The sample’s PDF download, page by page.',
      pdfPageAlt: (number, total, name) =>
        `Page ${number} of ${total} of the ${name} sample resume`,
      pdfPageCaption: (number, total) => `Page ${number} of ${total}`,
      atsTab: 'What an ATS reads',
      atsHint: 'The resume’s text, in the order an applicant tracking system '
        + 'reads it.',
      sample: 'Sample',
      fictional: '(fictional person)',
      pages: 'Pages',
      pageCount: (count) => `${count} ${count === 1 ? 'page' : 'pages'}`,
      layout: 'Layout',
      oneColumn: 'One column',
      twoColumns: 'Two columns',
      paper: 'Paper',
      photo: 'Photo',
      suitsPhoto: 'Suits a photo: above, left, or right of the name',
      photoNotRecommended: 'Photo not recommended',
      twoColumnNote: [
        'Job portals may read the sidebar after the main column. For online '
        + 'portals, use ',
        '.',
      ],
      fillerNote: 'This template has no sample of its own yet. The content '
        + 'below is illustrative.',
      useSample: 'Use this sample',
      useBlank: 'Use this template with a blank resume',
      privateCopy: 'A sample creates a private copy in your account. You '
        + 'replace the content, then decide whether to publish it.',
      preview: 'Resume preview',
    },
  },
};
