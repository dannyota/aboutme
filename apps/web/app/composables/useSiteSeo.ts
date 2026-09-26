import { computed, type MaybeRefOrGetter, toValue } from 'vue';

import type { Locale } from '../i18n/locale';
import {
  ogImageUrl,
  ogLocales,
  siteName,
  siteOrigin,
} from '../i18n/meta';

export interface SiteSeo {
  readonly path:
    | '/' | '/privacy' | '/terms' | '/verify' | `/templates${string}`;
  readonly title: string;
  readonly description: string;
  readonly locale: Locale;
}

/**
 * Title, description, canonical link, Open Graph, and Twitter card for an
 * indexable site page. The language lives in a cookie, not the URL, so there
 * is one canonical URL per page and no hreflang.
 */
export function useSiteSeo(input: MaybeRefOrGetter<SiteSeo>): void {
  const seo = computed(() => toValue(input));
  useHead(computed(() => {
    const { path, title, description, locale } = seo.value;
    const url = `${siteOrigin}${path === '/' ? '/' : path}`;
    const alternate = locale === 'vi' ? ogLocales.en : ogLocales.vi;
    return {
      title,
      link: [{ rel: 'canonical', href: url }],
      meta: [
        { name: 'description', content: description },
        { property: 'og:type', content: 'website' },
        { property: 'og:site_name', content: siteName },
        { property: 'og:title', content: title },
        { property: 'og:description', content: description },
        { property: 'og:url', content: url },
        { property: 'og:locale', content: ogLocales[locale] },
        { property: 'og:locale:alternate', content: alternate },
        { property: 'og:image', content: ogImageUrl },
        { property: 'og:image:width', content: '1200' },
        { property: 'og:image:height', content: '630' },
        { property: 'og:image:alt', content: siteName },
        { name: 'twitter:card', content: 'summary_large_image' },
      ],
    };
  }));
}
