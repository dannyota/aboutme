import type {
  PublicRenderPreview,
} from '../../utils/public-render/envelope';

// HTML-escapes plain text for a text context (<title>). It is complete for
// RCDATA text but does not escape single quotes, so never reuse it for an HTML
// attribute value.
export const escapeText = (value: string): string => value
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;');

export const escapeAttribute = (value: string): string => escapeText(value)
  .replaceAll('\'', '&#39;');

const meta = (
  attribute: 'name' | 'property',
  key: string,
  value: string,
): string =>
  `<meta ${attribute}="${key}" content="${escapeAttribute(value)}">`;

/**
 * The link-preview tags in the order of docs/design/link-previews.md, "Page
 * head". The server's validator accepts each exactly once with the value it
 * computed, and rejects twitter:title, twitter:description and theme-color.
 * A request without preview text gets only the image tags. A resume page is
 * og:type profile; a project page is website. An empty description, which
 * only a project page sends, drops both description tags.
 */
export const previewHead = (
  pageURL: string,
  preview: PublicRenderPreview | undefined,
  imageURL: string | undefined,
  ogType: 'profile' | 'website' = 'profile',
): string => {
  // Without an image URL the head names no image and no twitter:card at all:
  // a gate has no card of its own to show, and the resume's PNG would reveal
  // the resume (docs/design/viewer-analytics/sign-in-to-view.md#gate). Go's
  // gate validator accepts twitter:card only as summary_large_image.
  const image = imageURL === undefined
    ? []
    : [
        meta('property', 'og:image', imageURL),
        ...(preview === undefined
          ? []
          : [meta('property', 'og:image:type', 'image/png')]),
        meta('property', 'og:image:width', '1200'),
        meta('property', 'og:image:height', '630'),
      ];
  const card = imageURL === undefined
    ? []
    : [
        meta('name', 'twitter:card', 'summary_large_image'),
        meta('name', 'twitter:image', imageURL),
      ];
  if (preview === undefined) return [...image, ...card].join('');
  const described = preview.description !== '';
  return [
    ...(described ? [meta('name', 'description', preview.description)] : []),
    meta('property', 'og:type', ogType),
    meta('property', 'og:site_name', 'aboutme.vn'),
    meta('property', 'og:title', preview.title),
    ...(described
      ? [meta('property', 'og:description', preview.description)]
      : []),
    meta('property', 'og:url', pageURL),
    ...(preview.locale === ''
      ? []
      : [meta('property', 'og:locale', preview.locale)]),
    ...image,
    ...(imageURL === undefined
      ? []
      : [meta('property', 'og:image:alt', preview.imageAlt)]),
    ...card,
    ...(imageURL === undefined
      ? []
      : [meta('name', 'twitter:image:alt', preview.imageAlt)]),
  ].join('');
};

/**
 * Content versions of the fixed-name, immutable assets the public page loads;
 * each is 16 lowercase hex characters.
 */
export interface PublicAssetVersions {
  readonly style: string;
  readonly script: string;
}

export const validAssetVersion = (version: string): boolean =>
  /^[0-9a-f]{16}$/u.test(version);
