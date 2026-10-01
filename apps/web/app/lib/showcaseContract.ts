// The community showcase's wire shapes: the public listing and the owner's
// publish fields (docs/design/showcase.md, Delivery and Data and contract).
// Everything here mirrors the OpenAPI contract by hand. When the generated
// client carries these shapes, replace the types below with aliases of the
// generated ones and keep the constants.

/**
 * The ten roles: the nine Library role chips in chip order, then Other. A
 * test keeps this list equal to the Library's.
 */
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
] as const;
export type ShowcaseRole = (typeof SHOWCASE_ROLES)[number];

/** The listing's language filter; an item may also be `other`. */
export const SHOWCASE_FILTER_LANGUAGES = ['vi', 'en'] as const;
export type ShowcaseFilterLanguage = (typeof SHOWCASE_FILTER_LANGUAGES)[number];
export type ShowcaseItemLanguage = ShowcaseFilterLanguage | 'other';

export const SHOWCASE_PAGE_SIZE = 12;
export const SHOWCASE_MAX_PAGE = 100;
/** The template filter value for resumes that match no preset. */
export const SHOWCASE_CUSTOM_TEMPLATE = 'custom';

export const SHOWCASE_LISTING_PATH = '/api/v1/public/showcase';

export interface ShowcaseItem {
  readonly slug: string;
  readonly cardVersion: string;
  readonly imageText: string;
  readonly language: ShowcaseItemLanguage;
  readonly templateId: string | null;
  readonly role: string | null;
}

export interface ShowcaseListing {
  readonly items: readonly ShowcaseItem[];
  readonly page: number;
  readonly pageCount: number;
  readonly total: number;
}

/** The owner resume resource's `showcase` field: null when off. */
export type OwnerShowcaseState = 'pending' | 'listed' | 'declined';
export interface OwnerShowcase {
  readonly state: OwnerShowcaseState;
  readonly role: string | null;
}

/** The publish request's optional showcase fields; omitted keeps them. */
export interface PublishShowcaseFields {
  readonly showcaseEnabled?: boolean;
  /** Empty clears the role. */
  readonly showcaseRole?: string;
}

export function isShowcaseRole(value: unknown): value is ShowcaseRole {
  return SHOWCASE_ROLES.some((role) => role === value);
}

/** The versioned card image of a listing, always on the page's own origin. */
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
  // Only the closed fields survive, so nothing else reaches the DOM.
  return Object.freeze({
    slug: value.slug,
    cardVersion: value.cardVersion,
    imageText: value.imageText,
    language: value.language,
    templateId: value.templateId,
    role: value.role,
  });
}

/** Validates a listing response; throws on any shape violation. */
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

/** The owner resource's `showcase` field, or null when absent or unknown. */
export function parseOwnerShowcase(value: unknown): OwnerShowcase | null {
  if (
    !isRecord(value)
    || (value.state !== 'pending' && value.state !== 'listed'
      && value.state !== 'declined')
    || (value.role !== null && value.role !== undefined
      && typeof value.role !== 'string')
  ) {
    return null;
  }
  return Object.freeze({ state: value.state, role: value.role ?? null });
}
