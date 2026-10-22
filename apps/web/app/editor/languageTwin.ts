import type {
  PersonalDetail,
  PersonalDetails,
  Resume,
  Section,
} from '@aboutme/schema';

/** Resume languages the Vietnam tech guide pairs. */
export type TwinLanguage = 'vi' | 'en';

/** Structural changes the editor checklist explains. */
export type TwinNote
  = | 'photo-removed'
    | 'birth-removed'
    | 'zalo-suggested'
    | 'zalo-removed'
    | 'headings-left';

export interface LanguageTwinInput {
  readonly title: string;
  readonly lng: string;
  readonly document: Resume;
}

export interface LanguageTwin {
  readonly lng: TwinLanguage;
  readonly title: string;
  readonly document: Resume;
  readonly notes: readonly TwinNote[];
}

interface StoredLanguageTwin {
  readonly id: string;
  readonly target: TwinLanguage;
  readonly notes: readonly TwinNote[];
}

const TITLE_LIMIT = 160;
/** In-tab handoff from create → editor; never browser storage. */
let rememberedTwin: StoredLanguageTwin | null = null;
const TITLE_SUFFIX: Readonly<Record<TwinLanguage, string>> = {
  vi: ' · Tiếng Việt',
  en: ' · English',
};

type Heading = Readonly<Record<TwinLanguage, string>>;

const DEFAULT_HEADING: Readonly<Record<Section['sectionType'], Heading>> = {
  profile: { vi: 'Giới thiệu', en: 'Summary' },
  work: { vi: 'Kinh nghiệm làm việc', en: 'Experience' },
  education: { vi: 'Học vấn', en: 'Education' },
  skill: { vi: 'Kỹ năng', en: 'Skills' },
  language: { vi: 'Ngoại ngữ', en: 'Languages' },
  certificate: { vi: 'Chứng chỉ', en: 'Certifications' },
  project: { vi: 'Dự án', en: 'Projects' },
  custom: { vi: 'Phần tùy chỉnh', en: 'Custom section' },
};

const HEADING_ALIASES: readonly {
  readonly names: readonly string[];
  readonly heading: Heading;
}[] = [
  {
    names: ['Giới thiệu', 'Tóm tắt', 'Hồ sơ', 'Summary', 'Profile'],
    heading: DEFAULT_HEADING.profile,
  },
  {
    names: ['Mục tiêu nghề nghiệp', 'Objective'],
    heading: { vi: 'Mục tiêu nghề nghiệp', en: 'Objective' },
  },
  {
    names: ['Kinh nghiệm làm việc', 'Experience', 'Work experience'],
    heading: DEFAULT_HEADING.work,
  },
  {
    names: ['Học vấn', 'Education'],
    heading: DEFAULT_HEADING.education,
  },
  {
    names: ['Kỹ năng', 'Skills'],
    heading: DEFAULT_HEADING.skill,
  },
  {
    names: ['Ngoại ngữ', 'Ngôn ngữ', 'Languages'],
    heading: DEFAULT_HEADING.language,
  },
  {
    names: ['Chứng chỉ', 'Certifications'],
    heading: DEFAULT_HEADING.certificate,
  },
  {
    names: ['Dự án', 'Projects'],
    heading: DEFAULT_HEADING.project,
  },
  {
    names: ['Giải thưởng', 'Awards'],
    heading: { vi: 'Giải thưởng', en: 'Awards' },
  },
  {
    names: ['Hoạt động', 'Activities'],
    heading: { vi: 'Hoạt động', en: 'Activities' },
  },
  {
    names: ['Phần tùy chỉnh', 'Custom section'],
    heading: DEFAULT_HEADING.custom,
  },
];

const BIRTH_LABELS = new Set([
  'ngày sinh',
  'năm sinh',
  'nam sinh',
  'date of birth',
  'birth date',
  'birth year',
  'dob',
  'birthday',
]);

/** The other Vietnam-guide language, or null when this resume is not vi/en. */
export function twinTarget(lng: string): TwinLanguage | null {
  if (lng === 'vi') return 'en';
  if (lng === 'en') return 'vi';
  return null;
}

/**
 * Copy a resume into the other language. Prose stays for a person to review.
 * The photo is dropped because its storage key belongs to the source resume.
 */
export function languageTwin(input: LanguageTwinInput): LanguageTwin | null {
  const target = twinTarget(input.lng);
  if (target === null) return null;
  const source = structuredClone(input.document);
  const notes: TwinNote[] = [];
  const personalDetails = translateDetails(source, target, notes);
  let headingsLeft = false;
  const content: Resume['content'] = {};
  for (const [key, section] of Object.entries(source.content)) {
    const next = translateHeading(section, target);
    content[key] = next.section;
    if (!next.known) headingsLeft = true;
  }
  if (headingsLeft) notes.push('headings-left');
  return {
    lng: target,
    title: retitle(input.title, target),
    document: {
      ...source,
      personalDetails,
      content,
      customization: {
        ...source.customization,
        dateFormat: target === 'vi' ? 'MM/YYYY' : 'Mon YYYY',
      },
    },
    notes,
  };
}

/** Remember a twin so the editor can show its review checklist once. */
export function rememberLanguageTwin(
  id: string,
  twin: LanguageTwin,
): void {
  rememberedTwin = {
    id,
    target: twin.lng,
    notes: twin.notes,
  };
}

/** The checklist for this resume, when it was just created as a twin. */
export function readLanguageTwin(id: string): StoredLanguageTwin | null {
  if (rememberedTwin === null || rememberedTwin.id !== id) return null;
  return rememberedTwin;
}

/** Drop the checklist after the person finishes reviewing. */
export function dismissLanguageTwin(id: string): void {
  if (rememberedTwin?.id === id) rememberedTwin = null;
}

/** Clear any pending checklist (tests). */
export function forgetLanguageTwin(): void {
  rememberedTwin = null;
}

function retitle(title: string, target: TwinLanguage): string {
  const suffix = TITLE_SUFFIX[target];
  let base = title.trim();
  for (const ending of Object.values(TITLE_SUFFIX)) {
    if (base.endsWith(ending)) {
      base = base.slice(0, -ending.length).trimEnd();
    }
  }
  const room = TITLE_LIMIT - Array.from(suffix).length;
  const trimmed = Array.from(base).slice(0, Math.max(0, room)).join('');
  return `${trimmed.trimEnd()}${suffix}`;
}

function translateDetails(
  source: Resume,
  target: TwinLanguage,
  notes: TwinNote[],
): PersonalDetails {
  const personal = source.personalDetails;
  const personalDetails: PersonalDetails = { ...personal };
  delete personalDetails.photo;
  if (personal.photo !== undefined) notes.push('photo-removed');
  const original = personal.details ?? [];
  personalDetails.details = target === 'en'
    ? englishDetails(original, notes)
    : vietnameseDetails(original, notes);
  return personalDetails;
}

function englishDetails(
  details: readonly PersonalDetail[],
  notes: TwinNote[],
): PersonalDetail[] {
  const withoutBirth = details.filter((detail) => !isBirth(detail));
  if (withoutBirth.length !== details.length) notes.push('birth-removed');
  const withoutZalo = withoutBirth.filter((detail) => !isZalo(detail));
  if (withoutZalo.length !== withoutBirth.length) notes.push('zalo-removed');
  return withoutZalo;
}

function vietnameseDetails(
  details: readonly PersonalDetail[],
  notes: TwinNote[],
): PersonalDetail[] {
  notes.push('zalo-suggested');
  return [...details];
}

function translateHeading(
  section: Section,
  target: TwinLanguage,
): { readonly section: Section; readonly known: boolean } {
  const current = section.displayName?.trim() ?? '';
  if (current === '') {
    if (section.sectionType === 'custom') return { section, known: true };
    return {
      section: {
        ...section,
        displayName: DEFAULT_HEADING[section.sectionType][target],
      },
      known: true,
    };
  }
  const heading = HEADING_ALIASES.find(
    (alias) => alias.names.includes(current),
  )?.heading;
  if (heading === undefined) return { section, known: false };
  return { section: { ...section, displayName: heading[target] }, known: true };
}

function fold(value: string): string {
  return value.normalize('NFC').trim().toLowerCase();
}

function isBirth(detail: PersonalDetail): boolean {
  return detail.type === 'custom'
    && BIRTH_LABELS.has(fold(detail.label ?? ''));
}

function isZalo(detail: PersonalDetail): boolean {
  return detail.type === 'custom' && fold(detail.label ?? '') === 'zalo';
}
