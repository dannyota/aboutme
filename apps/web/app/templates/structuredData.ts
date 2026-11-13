import { siteName, siteOrigin } from '../i18n/meta';
import type { GalleryTemplate } from './catalog';

const LESS_THAN = `${'\\'}u003c`;

function serialize(value: unknown): string {
  return JSON.stringify(value).replace(/</g, LESS_THAN);
}

export function galleryStructuredData(
  name: string,
  description: string,
  templates: readonly GalleryTemplate[],
): string {
  const url = `${siteOrigin}/templates`;
  return serialize({
    '@context': 'https://schema.org',
    '@type': 'CollectionPage',
    name,
    description,
    url,
    'isPartOf': {
      '@type': 'WebSite',
      'name': siteName,
      'url': `${siteOrigin}/`,
    },
    'mainEntity': {
      '@type': 'ItemList',
      'numberOfItems': templates.length,
      'itemListElement': templates.map((template, index) => ({
        '@type': 'ListItem',
        'position': index + 1,
        'url': `${url}/${template.id}`,
        'name': template.name,
      })),
    },
  });
}

export function templateStructuredData(
  template: GalleryTemplate,
  description: string,
  inLanguage: string,
): string {
  return serialize({
    '@context': 'https://schema.org',
    '@type': 'CreativeWork',
    'name': template.name,
    description,
    'url': `${siteOrigin}/templates/${template.id}`,
    inLanguage,
    'isAccessibleForFree': true,
    'creator': { '@type': 'Organization', 'name': siteName },
  });
}
