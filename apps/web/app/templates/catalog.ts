import { SAMPLES, type SampleLanguage } from '@aboutme/schema/samples';
import { TEMPLATES, type TemplatePreset } from '@aboutme/schema/templates';
import type { LocationQuery } from 'vue-router';

import fontCatalog from '../assets/fonts/catalog.json';
import type { Locale } from '../i18n/locale';
import { SAMPLE_PAGES } from './samplePages';

export const FILTERS = [
  'sample',
  'ats',
  'one-page',
  'photo',
  'first-job',
  'technical',
  'management',
] as const;

export type GalleryFilter = (typeof FILTERS)[number];

export const ROLES = [
  'backend',
  'frontend',
  'mobile',
  'devops',
  'data-ai',
  'qa',
  'fresher',
  'brse',
  'security',
] as const;

export type GalleryRole = (typeof ROLES)[number];

export const ROLE_MEMBERS: Readonly<Record<GalleryRole, readonly string[]>> = {
  'backend': ['one-page-tight'],
  'frontend': ['engineer-compact'],
  'mobile': ['creative-accent'],
  'devops': ['nordic-muted'],
  'data-ai': ['elegant-serif-two'],
  'qa': ['mono-print'],
  'fresher': ['minimal-air'],
  'brse': ['international-lang'],
  'security': ['consulting-formal'],
};

export const MEMBERS: Readonly<
  Record<Exclude<GalleryFilter, 'sample'>, readonly string[]>
> = {
  'ats': [
    'ats-plain',
    'mono-print',
    'high-contrast',
    'government-formal',
    'minimal-air',
    'classic-serif',
    'academic-dense',
  ],
  'one-page': [
    'one-page-tight',
    'engineer-compact',
    'ats-plain',
    'graduate-friendly',
    'minimal-air',
  ],
  'photo': [
    'modern-sidebar',
    'executive-band',
    'graduate-friendly',
    'creative-accent',
    'designer-tag',
    'nordic-muted',
    'engineer-compact',
    'international-lang',
    'startup-bold',
    'elegant-serif-two',
    'classic-serif',
    'consulting-formal',
    'government-formal',
    'academic-dense',
  ],
  'first-job': [
    'graduate-friendly',
    'minimal-air',
    'startup-bold',
    'ats-plain',
  ],
  'technical': [
    'engineer-compact',
    'one-page-tight',
    'nordic-muted',
    'international-lang',
  ],
  'management': [
    'executive-band',
    'consulting-formal',
    'classic-serif',
    'elegant-serif-two',
  ],
};

const SAMPLE_ORDER = [
  ...ROLES.flatMap((role) => ROLE_MEMBERS[role]),
  'ats-plain',
  'graduate-friendly',
  'executive-band',
  'modern-sidebar',
];

interface CatalogEntry {
  readonly purpose: Readonly<Record<Locale, string>>;
  readonly sampleTags?: Readonly<
    Record<SampleLanguage, Readonly<Record<Locale, string>>>
  >;
}

const ENTRIES: Readonly<Record<string, CatalogEntry>> = {
  'academic-dense': {
    purpose: {
      vi: 'CV học thuật nhiều trang: công bố, giảng dạy, đề tài',
      en: 'Multi-page academic CV for publications, teaching and grants',
    },
  },
  'ats-plain': {
    purpose: {
      vi: 'Nộp qua cổng tuyển dụng: một cột, máy đọc đúng từng chữ',
      en: 'For job portals: one column software reads word for word',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Trưởng nhóm vận hành kho',
        en: 'Sample: Warehouse operations lead',
      },
      en: {
        vi: 'CV mẫu: Chuyên viên phân tích FP&A',
        en: 'Sample: FP&A analyst',
      },
    },
  },
  'classic-serif': {
    purpose: {
      vi: 'Trang trọng, truyền thống: ngân hàng, luật, hành chính',
      en: 'Traditional and formal: banking, law, administration',
    },
  },
  'consulting-formal': {
    purpose: {
      vi: 'Gọn, trang trọng cho tư vấn, tài chính, quản lý',
      en: 'Tight and formal for consulting, finance and management',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Security Engineer',
        en: 'Sample: Security Engineer',
      },
      en: {
        vi: 'CV mẫu: Security Engineer',
        en: 'Sample: Security Engineer',
      },
    },
  },
  'creative-accent': {
    purpose: {
      vi: 'Một màu nhấn nổi bật cho marketing, truyền thông',
      en: 'One bold accent color for marketing and media roles',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Senior Mobile Engineer',
        en: 'Sample: Senior Mobile Engineer',
      },
      en: {
        vi: 'CV mẫu: Senior Mobile Engineer',
        en: 'Sample: Senior Mobile Engineer',
      },
    },
  },
  'designer-tag': {
    purpose: {
      vi: 'Trang kiểu portfolio cho thiết kế và sáng tạo',
      en: 'A portfolio-style sheet for design and creative work',
    },
  },
  'editorial-wide': {
    purpose: {
      vi: 'Chữ có chân như sách, cho CV được đọc kỹ',
      en: 'Book-like serif for a CV that gets read, not scanned',
    },
  },
  'elegant-serif-two': {
    purpose: {
      vi: 'Hai cột chữ có chân, chứng chỉ nổi bật ở cột bên',
      en: 'Two serif columns, credentials first in the sidebar',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Machine Learning Engineer',
        en: 'Sample: Machine Learning Engineer',
      },
      en: {
        vi: 'CV mẫu: Machine Learning Engineer',
        en: 'Sample: Machine Learning Engineer',
      },
    },
  },
  'engineer-compact': {
    purpose: {
      vi: 'Kỹ sư, dữ liệu: hai cột dày, kỹ năng ở cột bên',
      en: 'For engineers and data roles: dense, skills in the sidebar',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Senior Frontend Engineer',
        en: 'Sample: Senior Frontend Engineer',
      },
      en: {
        vi: 'CV mẫu: Senior Frontend Engineer',
        en: 'Sample: Senior Frontend Engineer',
      },
    },
  },
  'executive-band': {
    purpose: {
      vi: 'Quản lý cấp cao: tên và chức danh trên dải màu',
      en: 'For senior leaders: name and title on a bold band',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Giám đốc công nghệ',
        en: 'Sample: Chief technology officer',
      },
      en: {
        vi: 'CV mẫu: Giám đốc vận hành',
        en: 'Sample: Chief operating officer',
      },
    },
  },
  'government-formal': {
    purpose: {
      vi: 'Hồ sơ trang trọng một màu mực cho khu vực công',
      en: 'One-ink formal record for public-sector applications',
    },
  },
  'graduate-friendly': {
    purpose: {
      vi: 'Mới tốt nghiệp: chữ lớn, thoáng, ít kinh nghiệm vẫn đầy trang',
      en: 'For graduates: airy type, a short history still fills the page',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Cử nhân kế toán',
        en: 'Sample: Accounting graduate',
      },
      en: {
        vi: 'CV mẫu: Cử nhân quản trị kinh doanh',
        en: 'Sample: Business graduate',
      },
    },
  },
  'high-contrast': {
    purpose: {
      vi: 'Dễ đọc nhất: tương phản cao, chữ lớn',
      en: 'Easiest to read: high contrast, large type',
    },
  },
  'international-lang': {
    purpose: {
      vi: 'Ứng tuyển nước ngoài: ngoại ngữ đứng đầu cột bên',
      en: 'For cross-border roles: languages lead the sidebar',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: BrSE',
        en: 'Sample: Bridge Software Engineer (BrSE)',
      },
      en: {
        vi: 'CV mẫu: BrSE',
        en: 'Sample: Bridge Software Engineer (BrSE)',
      },
    },
  },
  'minimal-air': {
    purpose: {
      vi: 'Tối giản: không kẻ, không nền, chỉ khoảng trắng',
      en: 'Minimal: no rules, no fills, only white space',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Fresher Backend Developer',
        en: 'Sample: Entry-level Backend Developer',
      },
      en: {
        vi: 'CV mẫu: Fresher Backend Developer',
        en: 'Sample: Entry-level Backend Developer',
      },
    },
  },
  'modern-sidebar': {
    purpose: {
      vi: 'Hiện đại, cột bên có màu cho kỹ năng, ngoại ngữ',
      en: 'Modern, a tinted sidebar for skills and languages',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Trưởng nhóm digital marketing',
        en: 'Sample: Digital marketing lead',
      },
      en: {
        vi: 'CV mẫu: Quản lý sản phẩm',
        en: 'Sample: Product manager',
      },
    },
  },
  'mono-print': {
    purpose: {
      vi: 'Đen trắng, photocopy vẫn rõ',
      en: 'Pure black and white, clear even when photocopied',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: QA Lead',
        en: 'Sample: QA Lead',
      },
      en: {
        vi: 'CV mẫu: QA Lead',
        en: 'Sample: QA Lead',
      },
    },
  },
  'nordic-muted': {
    purpose: {
      vi: 'Hai cột xanh xám dịu, gọn và nhẹ nhàng',
      en: 'Calm blue-grey two columns, compact and quiet',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Senior DevOps Engineer',
        en: 'Sample: Senior DevOps Engineer',
      },
      en: {
        vi: 'CV mẫu: Senior DevOps Engineer',
        en: 'Sample: Senior DevOps Engineer',
      },
    },
  },
  'one-page-tight': {
    purpose: {
      vi: 'Nhiều kinh nghiệm, gói gọn trong một trang A4',
      en: 'A long career on one A4 page',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Senior Backend Engineer',
        en: 'Sample: Senior Backend Engineer',
      },
      en: {
        vi: 'CV mẫu: Senior Backend Engineer',
        en: 'Sample: Senior Backend Engineer',
      },
    },
  },
  'startup-bold': {
    purpose: {
      vi: 'Chữ lớn, mạnh cho sản phẩm, tăng trưởng, startup',
      en: 'Large, bold type for product, growth and startup roles',
    },
  },
};

export interface GalleryTemplate extends CatalogEntry {
  readonly id: string;
  readonly name: string;
  readonly preset: Readonly<TemplatePreset>;
  readonly columns: 1 | 2;
  readonly pageFormat: 'a4' | 'letter';
  readonly fontName: string;
  readonly sampleLanguages: readonly SampleLanguage[];
  readonly suitsPhoto: boolean;
  readonly samplePages?: number;
}

const fontNames = new Map(
  fontCatalog.entries.map((entry) => [entry.id, entry.displayName]),
);

export const GALLERY: readonly GalleryTemplate[] = Object.freeze(
  TEMPLATES.map((preset): GalleryTemplate => {
    const entry = ENTRIES[preset.id];
    if (entry === undefined) {
      throw new Error(`template ${preset.id} has no gallery entry`);
    }
    return Object.freeze({
      ...entry,
      id: preset.id,
      name: preset.name,
      preset,
      columns: preset.customization.layout.columns,
      pageFormat: preset.customization.pageFormat,
      fontName: fontNames.get(preset.customization.font.family)
        ?? preset.customization.font.family,
      sampleLanguages: SAMPLES
        .filter((sample) => sample.templateId === preset.id)
        .map((sample) => sample.lng),
      suitsPhoto: MEMBERS.photo.includes(preset.id),
      ...(SAMPLE_PAGES[preset.id] === undefined
        ? {}
        : { samplePages: SAMPLE_PAGES[preset.id] }),
    });
  }).sort(byGalleryOrder),
);

function byGalleryOrder(left: GalleryTemplate, right: GalleryTemplate): number {
  const leftRank = SAMPLE_ORDER.indexOf(left.id);
  const rightRank = SAMPLE_ORDER.indexOf(right.id);
  if (leftRank !== -1 || rightRank !== -1) {
    if (leftRank === -1) return 1;
    if (rightRank === -1) return -1;
    return leftRank - rightRank;
  }
  return left.name.localeCompare(right.name, 'en');
}

export function galleryTemplate(id: string): GalleryTemplate | undefined {
  return GALLERY.find((template) => template.id === id);
}

export function sampleRole(
  template: GalleryTemplate,
  sampleLanguage: SampleLanguage,
  inLanguage: Locale,
): string | undefined {
  return template.sampleTags?.[sampleLanguage][inLanguage]
    .replace(/^(?:CV mẫu|Sample): /u, '');
}

export function matchesFilter(
  template: GalleryTemplate,
  filter: GalleryFilter,
): boolean {
  return filter === 'sample'
    ? template.sampleLanguages.length > 0
    : MEMBERS[filter].includes(template.id);
}

export function parseFilter(value: unknown): GalleryFilter | undefined {
  return FILTERS.find((filter) => filter === value);
}

export function filterMembers(): Readonly<Record<string, readonly string[]>> {
  return MEMBERS;
}

export function matchesRole(
  template: GalleryTemplate,
  role: GalleryRole,
): boolean {
  return ROLE_MEMBERS[role].includes(template.id);
}

export function parseRole(value: unknown): GalleryRole | undefined {
  return ROLES.find((role) => role === value);
}

export function withRole(
  query: LocationQuery,
  role: GalleryRole | undefined,
): LocationQuery {
  const next = { ...query };
  if (role === undefined) {
    delete next.role;
  } else {
    next.role = role;
  }
  return next;
}

export function withGalleryFilters(
  query: LocationQuery,
  role: GalleryRole | undefined,
  filter: GalleryFilter | undefined,
): LocationQuery {
  const next = { ...query };
  delete next.role;
  delete next.filter;
  if (role !== undefined) next.role = role;
  if (filter !== undefined) next.filter = filter;
  return next;
}
