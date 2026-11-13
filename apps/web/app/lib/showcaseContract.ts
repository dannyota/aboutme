import type { components } from '../api/generated/openapi';

type Schemas = components['schemas'];

export const SHOWCASE_ROLES = [
  'backend',
  'frontend',
  'mobile',
  'devops',
  'data-ai',
  'qa',
  'fresher',
  'brse',
  'security',
  'other',
] as const satisfies readonly ShowcaseRole[];
export type ShowcaseRole = Schemas['ShowcaseRole'];

export const SHOWCASE_FILTER_LANGUAGES = ['vi', 'en'] as const;
export type ShowcaseFilterLanguage = (typeof SHOWCASE_FILTER_LANGUAGES)[number];
export type ShowcaseItemLanguage = ShowcaseFilterLanguage | 'other';

const SHOWCASE_PAGE_SIZE = 12;
export const SHOWCASE_MAX_PAGE = 100;
export const SHOWCASE_CUSTOM_TEMPLATE = 'custom';

export const SHOWCASE_LISTING_PATH = '/api/v1/public/showcase';

export type ShowcaseItem = Readonly<Schemas['ShowcaseItem']>;
export type ShowcaseListing = Readonly<
  Omit<Schemas['ShowcaseListing'], 'items'>
  & { items: readonly ShowcaseItem[] }
>;

export type OwnerShowcase = Readonly<Schemas['ResumeShowcase']>;

export type PublishShowcaseFields = Pick<
  Schemas['PublishResumeRequest'],
  'showcaseEnabled' | 'showcaseRole'
>;

export function isShowcaseRole(value: unknown): value is ShowcaseRole {
  return SHOWCASE_ROLES.some((role) => role === value);
}

export function showcaseCardUrl(item: ShowcaseItem): string {
  return `/api/v1/public/resumes/${encodeURIComponent(item.slug)}/og/`
    + `${encodeURIComponent(item.cardVersion)}.png`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseItem(value: unknown): ShowcaseItem {
  if (
    !isRecord(value)
    || typeof value.slug !== 'string'
    || typeof value.cardVersion !== 'string'
    || typeof value.imageText !== 'string'
    || (value.language !== 'vi' && value.language !== 'en'
      && value.language !== 'other')
    || (value.templateId !== null && typeof value.templateId !== 'string')
    || (value.role !== null && typeof value.role !== 'string')
  ) {
    throw new Error('invalid showcase item');
  }
  return Object.freeze({
    slug: value.slug,
    cardVersion: value.cardVersion,
    imageText: value.imageText,
    language: value.language,
    templateId: value.templateId,
    role: isShowcaseRole(value.role) ? value.role : null,
  });
}

export function parseShowcaseListing(value: unknown): ShowcaseListing {
  if (
    !isRecord(value)
    || !Array.isArray(value.items)
    || !Number.isSafeInteger(value.page)
    || !Number.isSafeInteger(value.pageCount)
    || !Number.isSafeInteger(value.total)
    || value.items.length > SHOWCASE_PAGE_SIZE
  ) {
    throw new Error('invalid showcase listing');
  }
  return Object.freeze({
    items: Object.freeze(value.items.map(parseItem)),
    page: value.page as number,
    pageCount: value.pageCount as number,
    total: value.total as number,
  });
}

export function parseOwnerShowcase(value: unknown): OwnerShowcase | null {
  if (
    !isRecord(value)
    || value.state !== 'listed'
    || (value.role !== null && value.role !== undefined
      && typeof value.role !== 'string')
  ) {
    return null;
  }
  return Object.freeze({
    state: value.state,
    role: isShowcaseRole(value.role) ? value.role : null,
  });
}
