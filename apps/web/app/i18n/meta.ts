// Page titles and search metadata for the site pages. Only the homepage, the
// Privacy Policy, and the Terms are indexable; every other route is noindex.
import type { Locale } from './locale';

export const siteName = 'aboutme';
export const siteOrigin = 'https://aboutme.vn';
export const ogImageUrl = `${siteOrigin}/og-image.png`;

export const indexablePaths: ReadonlySet<string> = new Set([
  '/',
  '/privacy',
  '/terms',
]);

/** "<page> · aboutme", the title pattern for every page but the homepage. */
export function pageTitle(name: string): string {
  return `${name} · ${siteName}`;
}

/** Titles for the English-only application pages. */
export const appTitles = {
  resumes: pageTitle('Resumes'),
  settings: pageTitle('Settings'),
  authorize: pageTitle('Authorize an agent'),
  /** The editor before its resume has loaded. */
  editor: pageTitle('Resume'),
} as const;

export const homeTitle: Record<Locale, string> = {
  vi: 'aboutme — CV miễn phí, riêng tư đến khi bạn muốn',
  en: 'aboutme — Free resumes, private until you choose',
};

export const ogLocales: Record<Locale, string> = {
  vi: 'vi_VN',
  en: 'en_US',
};
