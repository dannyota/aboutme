import type {
  PublicRenderPreview,
} from '../../utils/public-render/envelope';

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

export const previewHead = (
  pageURL: string,
  preview: PublicRenderPreview | undefined,
  imageURL: string | undefined,
  ogType: 'profile' | 'website' = 'profile',
): string => {
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

export interface PublicAssetVersions {
  readonly style: string;
  readonly script: string;
}

export const validAssetVersion = (version: string): boolean =>
  /^[0-9a-f]{16}$/u.test(version);
