import { repositoryUrl } from '../i18n/legal';
import { siteName, siteOrigin } from '../i18n/meta';

/**
 * The homepage's schema.org graph. Every value is a fact the site already
 * states: the name, the address, the open repository, and that it is free.
 * No ratings, reviews, or user counts.
 */
export function homeStructuredData(description: string): string {
  const url = `${siteOrigin}/`;
  const graph = {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'WebSite',
        '@id': `${url}#website`,
        'name': siteName,
        url,
        'inLanguage': ['vi', 'en'],
        'publisher': { '@id': `${url}#organization` },
      },
      {
        '@type': 'Organization',
        '@id': `${url}#organization`,
        'name': siteName,
        url,
        'logo': `${siteOrigin}/apple-touch-icon.png`,
        'sameAs': [repositoryUrl],
      },
      {
        '@type': 'WebApplication',
        'name': siteName,
        url,
        description,
        'applicationCategory': 'BusinessApplication',
        'operatingSystem': 'Web',
        'inLanguage': ['vi', 'en'],
        'offers': { '@type': 'Offer', 'price': '0', 'priceCurrency': 'VND' },
      },
    ],
  };
  // JSON-LD is data, not script; escaping "<" keeps "</script>" out of it.
  return JSON.stringify(graph).replace(/</g, '\\u003c');
}
