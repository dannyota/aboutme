// The community showcase page (/showcase) in both site languages. Every
// string quotes the Copy tables of docs/design/showcase.md.
import type {
  ShowcaseItemLanguage,
  ShowcaseRole,
} from '../lib/showcaseContract';
import type { Locale } from './locale';
import { galleryCopy } from './templates';

export interface ShowcaseCopy {
  readonly navLabel: string;
  readonly title: string;
  readonly description: string;
  readonly lead: string;
  readonly orderNote: string;
  readonly rolesLabel: string;
  readonly allRoles: string;
  readonly roles: Readonly<Record<ShowcaseRole, string>>;
  readonly languageLabel: string;
  readonly allLanguages: string;
  readonly languages: Readonly<Record<ShowcaseItemLanguage, string>>;
  readonly templateLabel: string;
  readonly allTemplates: string;
  readonly customDesign: string;
  readonly report: string;
  readonly reportLabel: (slug: string) => string;
  readonly reportSubject: (slug: string) => string;
  readonly empty: string;
  readonly emptyAction: string;
  readonly emptyActionSignedIn: string;
  readonly noMatch: string;
  readonly loadFailed: string;
  readonly retry: string;
  readonly previous: string;
  readonly next: string;
  readonly pageStatus: (page: number, pageCount: number) => string;
}

export const showcaseCopy: Record<Locale, ShowcaseCopy> = {
  vi: {
    navLabel: 'Cộng đồng',
    title: 'CV từ cộng đồng',
    description: 'CV thật do người dùng aboutme.vn chọn chia sẻ.',
    lead:
      'CV thật do người dùng aboutme.vn xuất bản và chọn hiện ở đây.',
    orderNote: 'CV thêm sớm nhất hiện trước.',
    rolesLabel: galleryCopy.vi.rolesLabel,
    allRoles: galleryCopy.vi.allRoles,
    roles: { ...galleryCopy.vi.roles, other: 'Khác' },
    languageLabel: 'Ngôn ngữ của CV',
    allLanguages: 'Mọi ngôn ngữ',
    languages: { vi: 'Tiếng Việt', en: 'Tiếng Anh', other: 'Ngôn ngữ khác' },
    templateLabel: 'Mẫu',
    allTemplates: 'Mọi mẫu',
    customDesign: 'Thiết kế riêng',
    report: 'Báo cáo',
    reportLabel: (slug) => `Báo cáo CV ${slug} qua email`,
    reportSubject: (slug) => `Báo cáo trang Cộng đồng: ${slug}`,
    empty:
      'Chưa có CV nào ở đây. Xuất bản CV của bạn và bật Hiện trong trang '
      + 'Cộng đồng để là người đầu tiên.',
    emptyAction: 'Tạo CV',
    emptyActionSignedIn: 'Mở CV của bạn',
    noMatch: 'Không có CV nào khớp với bộ lọc này.',
    loadFailed: 'Không tải được danh sách. Hãy thử lại.',
    retry: 'Thử lại',
    previous: 'Trang trước',
    next: 'Trang sau',
    pageStatus: (page, pageCount) => `Trang ${page}/${pageCount}`,
  },
  en: {
    navLabel: 'Community',
    title: 'Community resumes',
    description: 'Real resumes that aboutme.vn users chose to share.',
    lead:
      'Real resumes that aboutme.vn users published and chose to show here.',
    orderNote: 'Earliest added first.',
    rolesLabel: galleryCopy.en.rolesLabel,
    allRoles: galleryCopy.en.allRoles,
    roles: { ...galleryCopy.en.roles, other: 'Other' },
    languageLabel: 'Resume language',
    allLanguages: 'All languages',
    languages: { vi: 'Vietnamese', en: 'English', other: 'Other language' },
    templateLabel: 'Template',
    allTemplates: 'All templates',
    customDesign: 'Custom design',
    report: 'Report',
    reportLabel: (slug) => `Report resume ${slug} by email`,
    reportSubject: (slug) => `Report showcase: ${slug}`,
    empty:
      'No resumes here yet. Publish your resume and turn on Show in the '
      + 'community showcase to be the first.',
    emptyAction: 'Create your resume',
    emptyActionSignedIn: 'Open your resumes',
    noMatch: 'No resume matches these filters.',
    loadFailed: 'Could not load the list. Try again.',
    retry: 'Try again',
    previous: 'Previous',
    next: 'Next',
    pageStatus: (page, pageCount) => `Page ${page} of ${pageCount}`,
  },
};
