import { siteName, siteOrigin } from '../i18n/meta';
import type { GalleryTemplate } from './catalog';

// JSON-LD is data, not script; escaping "<" keeps "</script>" out of it.
const LESS_THAN = `${'\\'}u003c`;

function serialize(value: unknown): string {
  return JSON.stringify(value).replace(/</g, LESS_THAN);
}

/** The gallery as a schema.org collection listing every template page. */
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

/** One template page: a free creative work from aboutme. */
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
