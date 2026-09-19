import { SAMPLES, type SampleLanguage } from '@aboutme/schema/samples';
import { TEMPLATES, type TemplatePreset } from '@aboutme/schema/templates';

import fontCatalog from '../assets/fonts/catalog.json';
import type { Locale } from '../i18n/locale';
import { SAMPLE_PAGES } from './samplePages';

/**
 * The template gallery's catalog: what each of the 20 templates is for, in
 * both site languages, and the facts the gallery filters on. Presets and
 * samples come from @aboutme/schema; this file holds only the gallery copy
 * and judgments.
 */

/** The gallery's single-choice filters, in chip order (SPEC §2). */
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

/**
 * Which templates each filter shows, besides "sample", which follows the
 * samples. Membership is an editorial judgment, kept here, not in the schema.
 */
const MEMBERS: Readonly<
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
  // Every template can show a photo; these suit one.
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
    'high-contrast',
  ],
  'management': [
    'executive-band',
    'consulting-formal',
    'classic-serif',
    'elegant-serif-two',
  ],
};

/** The templates with a sample lead the gallery, in this order. */
const SAMPLE_ORDER = [
  'ats-plain',
  'engineer-compact',
  'graduate-friendly',
  'executive-band',
  'modern-sidebar',
];

interface CatalogEntry {
  /** One line on what the template is for, per site language. */
  readonly purpose: Readonly<Record<Locale, string>>;
  /**
   * The tag naming each sample, by the sample's language, in each site
   * language ("CV mẫu: Kỹ sư backend" on the Vietnamese site).
   */
  readonly sampleTags?: Readonly<
    Record<SampleLanguage, Readonly<Record<Locale, string>>>
  >;
}

const ENTRIES: Readonly<Record<string, CatalogEntry>> = {
  'academic-dense': {
    purpose: {
      vi: 'Một cột dài cho công bố và giảng dạy',
      en: 'One long column for publications and teaching',
    },
  },
  'ats-plain': {
    purpose: {
      vi: 'Một cột, không biểu tượng: máy đọc được từng chữ',
      en: 'One column, no icons: every word machine-readable',
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
      vi: 'Chữ có chân, tiêu đề căn giữa',
      en: 'Serif type under a centered heading',
    },
  },
  'consulting-formal': {
    purpose: {
      vi: 'Trang Letter, xanh navy, trang trọng',
      en: 'A formal Letter page in navy',
    },
  },
  'creative-accent': {
    purpose: {
      vi: 'Một màu nhấn trên dải đầu trang',
      en: 'One accent color on a tinted header',
    },
  },
  'designer-tag': {
    purpose: {
      vi: 'Nền giấy, kỹ năng dạng nhãn',
      en: 'Paper tone, skills as tags',
    },
  },
  'editorial-wide': {
    purpose: {
      vi: 'Lề rộng, chữ có chân như sách',
      en: 'Wide margins and book-like serif type',
    },
  },
  'elegant-serif-two': {
    purpose: {
      vi: 'Hai cột chữ có chân, cột bên màu kem',
      en: 'Two serif columns with a cream sidebar',
    },
  },
  'engineer-compact': {
    purpose: {
      vi: 'Hai cột dày, kỹ năng dạng thanh ở cột bên',
      en: 'Dense two columns, skill bars in the sidebar',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Kỹ sư frontend',
        en: 'Sample: Frontend engineer',
      },
      en: {
        vi: 'CV mẫu: Kỹ sư backend',
        en: 'Sample: Backend engineer',
      },
    },
  },
  'executive-band': {
    purpose: {
      vi: 'Tên và chức danh trên dải màu đầu trang',
      en: 'Name and title on a colored band at the top',
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
      vi: 'Một màu mực, khối liên hệ xếp dọc',
      en: 'One ink, contacts stacked in a block',
    },
  },
  'graduate-friendly': {
    purpose: {
      vi: 'Chữ lớn, thoáng: ít kinh nghiệm vẫn đầy trang',
      en: 'Large, airy type: a short history still fills the page',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Cử nhân kế toán',
        en: 'Sample: Accounting graduate',
      },
      en: {
        vi: 'CV mẫu: Cử nhân kinh doanh',
        en: 'Sample: Business graduate',
      },
    },
  },
  'high-contrast': {
    purpose: {
      vi: 'Tương phản tối đa, chữ lớn',
      en: 'Maximum contrast, large type',
    },
  },
  'international-lang': {
    purpose: {
      vi: 'Ngôn ngữ dẫn đầu cột bên',
      en: 'Languages lead the sidebar',
    },
  },
  'minimal-air': {
    purpose: {
      vi: 'Không kẻ, không nền: chỉ khoảng trắng',
      en: 'No rules, no fills: only white space',
    },
  },
  'modern-sidebar': {
    purpose: {
      vi: 'Cột bên có màu cho kỹ năng và ngôn ngữ',
      en: 'A tinted sidebar for skills and languages',
    },
    sampleTags: {
      vi: {
        vi: 'CV mẫu: Trưởng nhóm marketing',
        en: 'Sample: Marketing lead',
      },
      en: {
        vi: 'CV mẫu: Quản lý sản phẩm',
        en: 'Sample: Product manager',
      },
    },
  },
  'mono-print': {
    purpose: {
      vi: 'Đen trắng thuần, in photocopy vẫn rõ',
      en: 'Pure black and white, clear even when photocopied',
    },
  },
  'nordic-muted': {
    purpose: {
      vi: 'Xanh xám dịu, cột bên nhạt',
      en: 'Muted blue-grey with a faint sidebar',
    },
  },
  'one-page-tight': {
    purpose: {
      vi: 'Cả sự nghiệp gọn trong một trang A4',
      en: 'A whole career on one A4 page',
    },
  },
  'startup-bold': {
    purpose: {
      vi: 'Chữ lớn, mạnh cho vai trò sản phẩm',
      en: 'Large, bold type for product roles',
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
  /** Sample languages this template has, in SAMPLES order. */
  readonly sampleLanguages: readonly SampleLanguage[];
  /** Suits a header photo; every template can show one. */
  readonly suitsPhoto: boolean;
  /** The sample's page count, when the template has a sample. */
  readonly samplePages?: number;
}

const fontNames = new Map(
  fontCatalog.entries.map((entry) => [entry.id, entry.displayName]),
);

/** The 20 templates in gallery order: those with a sample first. */
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

/**
 * The role a sample shows, from its tag ("Sample: Backend engineer" gives
 * "Backend engineer"), in a given language.
 */
export function sampleRole(
  template: GalleryTemplate,
  sampleLanguage: SampleLanguage,
  inLanguage: Locale,
): string | undefined {
  return template.sampleTags?.[sampleLanguage][inLanguage]
    .replace(/^(?:CV mẫu|Sample): /u, '');
}

/** Whether a template belongs under a gallery filter. */
export function matchesFilter(
  template: GalleryTemplate,
  filter: GalleryFilter,
): boolean {
  return filter === 'sample'
    ? template.sampleLanguages.length > 0
    : MEMBERS[filter].includes(template.id);
}

/** The filter named in a query value, or undefined for "all". */
export function parseFilter(value: unknown): GalleryFilter | undefined {
  return FILTERS.find((filter) => filter === value);
}

/** Every template id each filter names, for the catalog checks. */
export function filterMembers(): Readonly<Record<string, readonly string[]>> {
  return MEMBERS;
}
