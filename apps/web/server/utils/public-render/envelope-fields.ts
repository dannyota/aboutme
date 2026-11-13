import type { PublicRenderPreview } from './envelope';

// Field checks shared by every public render envelope. Each one accepts only
// values the server computes; the decoders fail closed on anything else.

// The server percent-encodes every byte outside the URL-unreserved set, so a
// favicon href holds only these characters and cannot leave its attribute.
const FAVICON_HREF = /^data:image\/svg\+xml,[A-Za-z0-9._~%-]{1,2048}$/u;

export const validPageTitle = (value: unknown): value is string =>
  typeof value === 'string' && value.length > 0 && value.length <= 4096;

export const validFaviconHref = (value: unknown): value is string =>
  value === '' || (typeof value === 'string' && FAVICON_HREF.test(value));

const PREVIEW_LOCALE = /^[a-z]{2,3}_[A-Z]{2}$/u;
const PREVIEW_KEYS = ['description', 'imageAlt', 'locale', 'title'].join(',');
const PREVIEW_KEYS_WITH_IMAGE
  = ['description', 'imageAlt', 'imageUrl', 'locale', 'title'].join(',');

export const boundedText = (
  value: unknown,
  maxBytes: number,
): value is string =>
  typeof value === 'string'
  && value.length > 0
  && Buffer.byteLength(value, 'utf8') <= maxBytes;

// Same shape the public page's own slug validates against
// (app/public/public-resume.client.ts).
const SLUG = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/u;

export const validSlug = (value: unknown): value is string =>
  typeof value === 'string' && SLUG.test(value);

// The library's project slug grammar (apps/server/internal/project/slug.go).
const PROJECT_SLUG = /^(?=.{2,40}$)[a-z0-9]+(?:-[a-z0-9]+)*$/u;

export const validProjectSlug = (value: unknown): value is string =>
  typeof value === 'string' && PROJECT_SLUG.test(value);

/** The size limits of a page's preview title and description. */
export interface PreviewBounds {
  readonly titleBytes: number;
  /** Unicode code points; unlimited when absent. */
  readonly titleCodePoints?: number;
  readonly descriptionBytes: number;
  /** Whether the page may have no description. */
  readonly emptyDescription: boolean;
}

/** The resume page's and the gate's preview: always described. */
export const PAGE_PREVIEW: PreviewBounds = {
  titleBytes: 2240, descriptionBytes: 1024, emptyDescription: false,
};

/** The page head's preview text, within the page's bounds. */
export const validPreview = (
  value: unknown,
  bounds: PreviewBounds = PAGE_PREVIEW,
): value is PublicRenderPreview => {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return false;
  }
  const preview = value as Record<string, unknown>;
  const keys = Object.keys(preview).sort().join(',');
  return (keys === PREVIEW_KEYS || keys === PREVIEW_KEYS_WITH_IMAGE)
    && boundedText(preview.title, bounds.titleBytes)
    && (bounds.titleCodePoints === undefined
      || [...preview.title].length <= bounds.titleCodePoints)
    && (boundedText(preview.description, bounds.descriptionBytes)
      || (bounds.emptyDescription && preview.description === ''))
    && boundedText(preview.imageAlt, 1024)
    && typeof preview.locale === 'string'
    && (preview.locale === '' || PREVIEW_LOCALE.test(preview.locale));
};

export function normalizedOrigin(value: unknown): value is string {
  if (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > 512) {
    return false;
  }
  if ([...value].some((character) => character.codePointAt(0)! > 0x7f)) {
    return false;
  }
  try {
    const parsed = new URL(value);
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:')
      && parsed.username === ''
      && parsed.password === ''
      && parsed.pathname === '/'
      && parsed.search === ''
      && parsed.hash === ''
      && parsed.origin === value
    );
  } catch {
    return false;
  }
}

// The versioned card URL Go names (docs/design/link-previews.md, "Page
// head"): this page's own origin and slug, and a 16-hex-digit version.
export const validCardImageURL = (
  value: unknown,
  canonicalOrigin: string,
  slug: string,
): boolean => {
  if (typeof value !== 'string') return false;
  const prefix = `${canonicalOrigin}/api/v1/public/resumes/${slug}/og/`;
  return value.startsWith(prefix)
    && /^[0-9a-f]{16}\.png$/u.test(value.slice(prefix.length));
};
